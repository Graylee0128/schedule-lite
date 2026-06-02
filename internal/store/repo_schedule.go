package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// v2 排班存取層:版本(draft/published)、逐小時指派、跨店占用、發布、員工標記。
// Rule Engine(rules.go)是純函式;這裡只負責把資料撈出/寫入,計算交給 handler 組裝後呼叫。

// isUniqueViolation 判斷是否為 Postgres unique 衝突(23505),用於 draft 並發建立的退讓。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func scanVersion(row rowScanner) (ScheduleVersion, error) {
	var v ScheduleVersion
	err := row.Scan(&v.ID, &v.StoreID, &v.Status, &v.CreatedAt, &v.PublishedAt, &v.ConfirmDeadline)
	return v, err
}

const versionCols = `id::text, store_id::text, status, created_at, published_at, confirm_deadline`

// GetOrCreateDraft 取某店「目前可編輯的 draft」;沒有就建。
// 規則:若最近一版是 published,新 draft **複製** 該 published 的指派(延續編輯,舊版留存)。
func (r *Repository) GetOrCreateDraft(ctx context.Context, storeID string) (ScheduleVersion, error) {
	// 快路徑:已有 draft 直接回。
	draft, err := scanVersion(r.pool.QueryRow(ctx, `SELECT `+versionCols+`
		FROM schedule_versions WHERE store_id = $1::uuid AND status = 'draft' LIMIT 1`, storeID))
	if err == nil {
		return draft, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ScheduleVersion{}, err
	}

	// 沒有 draft:在交易內建一張,若有最近 published 則複製其指派。
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ScheduleVersion{}, err
	}
	defer tx.Rollback(ctx)

	var pubID *string
	if err := tx.QueryRow(ctx, `
		SELECT id::text FROM schedule_versions
		WHERE store_id = $1::uuid AND status IN ('published', 'locked')
		ORDER BY published_at DESC NULLS LAST LIMIT 1`, storeID).Scan(&pubID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ScheduleVersion{}, err
	}

	newDraft, err := scanVersion(tx.QueryRow(ctx, `
		INSERT INTO schedule_versions (store_id, status) VALUES ($1::uuid, 'draft')
		RETURNING `+versionCols, storeID))
	if err != nil {
		// 並發下可能已有別的 draft 被建;退回去抓那一張。
		if isUniqueViolation(err) {
			tx.Rollback(ctx)
			return scanVersion(r.pool.QueryRow(ctx, `SELECT `+versionCols+`
				FROM schedule_versions WHERE store_id = $1::uuid AND status = 'draft' LIMIT 1`, storeID))
		}
		return ScheduleVersion{}, err
	}

	if pubID != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO shift_assignments (version_id, employee_id, weekday, hour)
			SELECT $1::uuid, employee_id, weekday, hour
			FROM shift_assignments WHERE version_id = $2::uuid`, newDraft.ID, *pubID); err != nil {
			return ScheduleVersion{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return ScheduleVersion{}, err
	}
	return newDraft, nil
}

// LatestPublishedVersion 取某店最近一版「已發布或已鎖定」班表(員工確認/看班表/匯出都看這版);沒有則 ok=false。
func (r *Repository) LatestPublishedVersion(ctx context.Context, storeID string) (ScheduleVersion, bool, error) {
	v, err := scanVersion(r.pool.QueryRow(ctx, `SELECT `+versionCols+`
		FROM schedule_versions WHERE store_id = $1::uuid AND status IN ('published', 'locked')
		ORDER BY published_at DESC NULLS LAST LIMIT 1`, storeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ScheduleVersion{}, false, nil
	}
	return v, err == nil, err
}

// PublishDraft 把某店目前 draft 設為 published 並設 24h 軟截止。沒有 draft 回 pgx.ErrNoRows。
func (r *Repository) PublishDraft(ctx context.Context, storeID string) (ScheduleVersion, error) {
	return scanVersion(r.pool.QueryRow(ctx, `
		UPDATE schedule_versions
		SET status = 'published', published_at = now(), confirm_deadline = now() + interval '24 hours'
		WHERE store_id = $1::uuid AND status = 'draft'
		RETURNING `+versionCols, storeID))
}

// SeedConfirmations 對某版本「有班的員工」各建一筆 pending 確認(發布時呼叫)。
func (r *Repository) SeedConfirmations(ctx context.Context, versionID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO shift_confirmations (version_id, employee_id)
		SELECT DISTINCT version_id, employee_id FROM shift_assignments WHERE version_id = $1::uuid
		ON CONFLICT DO NOTHING`, versionID)
	return err
}

// ListConfirmations 取某版本的員工確認狀態(含姓名)。
func (r *Repository) ListConfirmations(ctx context.Context, versionID string) ([]Confirmation, error) {
	const q = `
		SELECT c.employee_id::text, e.name, c.status, c.reason, c.responded_at
		FROM shift_confirmations c
		JOIN employees e ON e.id = c.employee_id
		WHERE c.version_id = $1::uuid
		ORDER BY e.name`
	rows, err := r.pool.Query(ctx, q, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Confirmation{}
	for rows.Next() {
		var c Confirmation
		if err := rows.Scan(&c.EmployeeID, &c.EmployeeName, &c.Status, &c.Reason, &c.RespondedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetConfirmation upsert 某員工對某版本的確認狀態(confirmed / declined + 理由)。
func (r *Repository) SetConfirmation(ctx context.Context, versionID, employeeID, status, reason string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO shift_confirmations (version_id, employee_id, status, reason, responded_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, now())
		ON CONFLICT (version_id, employee_id)
		DO UPDATE SET status = EXCLUDED.status, reason = EXCLUDED.reason, responded_at = now()`,
		versionID, employeeID, status, reason)
	return err
}

// EmployeeConfirmationStatus 取某員工對某版本的確認狀態(沒紀錄當 pending)。
func (r *Repository) EmployeeConfirmationStatus(ctx context.Context, versionID, employeeID string) (string, error) {
	var status string
	err := r.pool.QueryRow(ctx, `
		SELECT status FROM shift_confirmations WHERE version_id = $1::uuid AND employee_id = $2::uuid`,
		versionID, employeeID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "pending", nil
	}
	return status, err
}

// LockVersion 把某店最近一版 published 設為 locked(定案)。沒有 published 回 pgx.ErrNoRows。
func (r *Repository) LockVersion(ctx context.Context, storeID string) (ScheduleVersion, error) {
	return scanVersion(r.pool.QueryRow(ctx, `
		UPDATE schedule_versions SET status = 'locked'
		WHERE id = (
			SELECT id FROM schedule_versions
			WHERE store_id = $1::uuid AND status = 'published'
			ORDER BY published_at DESC NULLS LAST LIMIT 1
		)
		RETURNING `+versionCols, storeID))
}

// ListStoreEmployees 列出某店在職成員(含週工時上限),給排班候選與 Rule Engine 用。
func (r *Repository) ListStoreEmployees(ctx context.Context, storeID string) ([]ScheduleEmployee, error) {
	const q = `
		SELECT e.id::text, e.name, e.max_weekly_hours
		FROM employee_store_memberships m
		JOIN employees e ON e.id = m.employee_id
		WHERE m.store_id = $1::uuid AND m.is_active
		ORDER BY e.name`
	rows, err := r.pool.Query(ctx, q, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleEmployee{}
	for rows.Next() {
		var e ScheduleEmployee
		if err := rows.Scan(&e.ID, &e.Name, &e.MaxWeeklyHours); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListAssignments 取某版本的所有指派。
func (r *Repository) ListAssignments(ctx context.Context, versionID string) ([]ScheduleAssignment, error) {
	const q = `
		SELECT employee_id::text, weekday, hour
		FROM shift_assignments WHERE version_id = $1::uuid
		ORDER BY weekday, hour`
	rows, err := r.pool.Query(ctx, q, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleAssignment{}
	for rows.Next() {
		var a ScheduleAssignment
		if err := rows.Scan(&a.EmployeeID, &a.Weekday, &a.Hour); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReplaceEmployeeAssignments 整批覆寫某版本裡「某員工」的指派格(只動這個員工)。
func (r *Repository) ReplaceEmployeeAssignments(ctx context.Context, versionID, employeeID string, cells []HourCell) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM shift_assignments WHERE version_id = $1::uuid AND employee_id = $2::uuid`,
		versionID, employeeID); err != nil {
		return err
	}
	for _, c := range cells {
		if _, err := tx.Exec(ctx, `
			INSERT INTO shift_assignments (version_id, employee_id, weekday, hour)
			VALUES ($1::uuid, $2::uuid, $3, $4)`, versionID, employeeID, c.Weekday, c.Hour); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// StoreAvailabilityRows 取某店所有員工塗的可上格(含偏好);Rule Engine 判「不可用」、預排算分共用。
func (r *Repository) StoreAvailabilityRows(ctx context.Context, storeID string) ([]AvailabilityRow, error) {
	const q = `
		SELECT employee_id::text, weekday, hour, preference_level
		FROM availability_slots WHERE store_id = $1::uuid`
	rows, err := r.pool.Query(ctx, q, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AvailabilityRow{}
	for rows.Next() {
		var a AvailabilityRow
		if err := rows.Scan(&a.EmployeeID, &a.Weekday, &a.Hour, &a.PreferenceLevel); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReplaceAllAssignments 整批覆寫某版本的「所有」指派(預排一鍵建議用:先清空整張 draft 再寫入)。
func (r *Repository) ReplaceAllAssignments(ctx context.Context, versionID string, asgs []ScheduleAssignment) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM shift_assignments WHERE version_id = $1::uuid`, versionID); err != nil {
		return err
	}
	for _, a := range asgs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO shift_assignments (version_id, employee_id, weekday, hour)
			VALUES ($1::uuid, $2::uuid, $3, $4)`, versionID, a.EmployeeID, a.Weekday, a.Hour); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CrossStoreBusy 取這些員工在「其他店最近一版已發布班表」裡被排的格(跨店雙排判斷用)。
func (r *Repository) CrossStoreBusy(ctx context.Context, storeID string, employeeIDs []string) ([]ScheduleAssignment, error) {
	if len(employeeIDs) == 0 {
		return []ScheduleAssignment{}, nil
	}
	const q = `
		WITH latest AS (
			SELECT DISTINCT ON (store_id) id
			FROM schedule_versions
			WHERE status = 'published' AND store_id <> $1::uuid
			ORDER BY store_id, published_at DESC NULLS LAST
		)
		SELECT a.employee_id::text, a.weekday, a.hour
		FROM shift_assignments a
		JOIN latest l ON l.id = a.version_id
		WHERE a.employee_id::text = ANY($2::text[])`
	rows, err := r.pool.Query(ctx, q, storeID, employeeIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleAssignment{}
	for rows.Next() {
		var a ScheduleAssignment
		if err := rows.Scan(&a.EmployeeID, &a.Weekday, &a.Hour); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListIssues 取某版本員工標記的問題格(含員工姓名)。
func (r *Repository) ListIssues(ctx context.Context, versionID string) ([]ScheduleIssue, error) {
	const q = `
		SELECT i.employee_id::text, e.name, i.weekday, i.hour, i.note
		FROM assignment_issues i
		JOIN employees e ON e.id = i.employee_id
		WHERE i.version_id = $1::uuid
		ORDER BY i.weekday, i.hour`
	rows, err := r.pool.Query(ctx, q, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleIssue{}
	for rows.Next() {
		var s ScheduleIssue
		if err := rows.Scan(&s.EmployeeID, &s.EmployeeName, &s.Weekday, &s.Hour, &s.Note); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// EmployeeCells 取某版本裡某員工的指派格(員工看自己班表用)。
func (r *Repository) EmployeeCells(ctx context.Context, versionID, employeeID string) ([]HourCell, error) {
	const q = `
		SELECT weekday, hour FROM shift_assignments
		WHERE version_id = $1::uuid AND employee_id = $2::uuid
		ORDER BY weekday, hour`
	rows, err := r.pool.Query(ctx, q, versionID, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HourCell{}
	for rows.Next() {
		var c HourCell
		if err := rows.Scan(&c.Weekday, &c.Hour); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// EmployeeIssueCells 取某版本裡某員工自己標的問題格(含 note)。
func (r *Repository) EmployeeIssueCells(ctx context.Context, versionID, employeeID string) ([]HourCell, error) {
	const q = `
		SELECT weekday, hour, note FROM assignment_issues
		WHERE version_id = $1::uuid AND employee_id = $2::uuid
		ORDER BY weekday, hour`
	rows, err := r.pool.Query(ctx, q, versionID, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HourCell{}
	for rows.Next() {
		var c HourCell
		if err := rows.Scan(&c.Weekday, &c.Hour, &c.Note); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkIssue 員工對「自己被排到的格」標記問題(upsert)。
// 用 INSERT...SELECT WHERE EXISTS 確保只能標自己有被排的格;回傳是否真的標到。
func (r *Repository) MarkIssue(ctx context.Context, versionID, employeeID string, weekday, hour int, note string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO assignment_issues (version_id, employee_id, weekday, hour, note)
		SELECT $1::uuid, $2::uuid, $3, $4, $5
		WHERE EXISTS (
			SELECT 1 FROM shift_assignments
			WHERE version_id = $1::uuid AND employee_id = $2::uuid AND weekday = $3 AND hour = $4
		)
		ON CONFLICT (version_id, employee_id, weekday, hour) DO UPDATE SET note = EXCLUDED.note, created_at = now()`,
		versionID, employeeID, weekday, hour, note)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
