package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// magic-link token 的設計:
//   - 產生 32 bytes 亂數 → base64url 當作「原始 token」(給店長/員工的連結用)。
//   - DB 只存 SHA-256 hash(token_hash),原始 token 不落地——就算 DB 外洩也無法反推連結。
//   - 驗證時把帶進來的 token 重新 hash,比對 token_hash。

// newToken 產生一組(原始 token, 其 hash)。
func newToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashToken(raw), nil
}

// hashToken 把原始 token 轉成存 DB 的 hash(hex 編碼的 SHA-256)。
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// CreateAccessToken 為**員工**產生一條 magic-link(v1.5 起不綁門市),回傳原始 token。
// 原始 token 只在這裡回傳一次(之後 DB 只剩 hash)。此版不設過期。
func (r *Repository) CreateAccessToken(ctx context.Context, employeeID string) (string, error) {
	raw, hash, err := newToken()
	if err != nil {
		return "", err
	}
	const q = `
		INSERT INTO employee_access_tokens (employee_id, token_hash)
		VALUES ($1::uuid, $2)`
	if _, err := r.pool.Exec(ctx, q, employeeID, hash); err != nil {
		return "", err
	}
	return raw, nil
}

// ResolveToken 用原始 token 解出**員工**;連結無效 / 已撤銷 / 已過期都回 pgx.ErrNoRows。
func (r *Repository) ResolveToken(ctx context.Context, raw string) (emp IDName, err error) {
	const q = `
		SELECT e.id::text, e.name
		FROM employee_access_tokens t
		JOIN employees e ON e.id = t.employee_id
		WHERE t.token_hash = $1
		  AND t.revoked_at IS NULL
		  AND (t.expires_at IS NULL OR t.expires_at > now())`
	err = r.pool.QueryRow(ctx, q, hashToken(raw)).Scan(&emp.ID, &emp.Name)
	return emp, err
}

// ResolveTokenForStore 解出 token 對應的員工 + 指定門市,並驗證該員工是該店 membership。
// token 無效或員工不屬於該店,都回 pgx.ErrNoRows。
func (r *Repository) ResolveTokenForStore(ctx context.Context, raw, storeID string) (emp, store IDName, err error) {
	const q = `
		SELECT e.id::text, e.name, s.id::text, s.name
		FROM employee_access_tokens t
		JOIN employees e ON e.id = t.employee_id
		JOIN employee_store_memberships m ON m.employee_id = e.id AND m.store_id = $2::uuid AND m.is_active
		JOIN stores s ON s.id = m.store_id
		WHERE t.token_hash = $1
		  AND t.revoked_at IS NULL
		  AND (t.expires_at IS NULL OR t.expires_at > now())`
	err = r.pool.QueryRow(ctx, q, hashToken(raw), storeID).
		Scan(&emp.ID, &emp.Name, &store.ID, &store.Name)
	return emp, store, err
}

// MarkSubmitted 記錄某員工已對某店送出可上時段(提交標記;重送就更新時間)。
func (r *Repository) MarkSubmitted(ctx context.Context, employeeID, storeID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO availability_submissions (employee_id, store_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (employee_id, store_id) DO UPDATE SET submitted_at = now()`,
		employeeID, storeID)
	return err
}

// GetAvailability 取某員工在某店「循環(weekday × hour)」塗過的可上時段(只存正向)。
func (r *Repository) GetAvailability(ctx context.Context, employeeID, storeID string) ([]HourSlot, error) {
	const q = `
		SELECT weekday, hour, preference_level
		FROM availability_slots
		WHERE employee_id = $1::uuid AND store_id = $2::uuid
		ORDER BY weekday, hour`
	rows, err := r.pool.Query(ctx, q, employeeID, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []HourSlot{}
	for rows.Next() {
		var s HourSlot
		if err := rows.Scan(&s.Weekday, &s.Hour, &s.PreferenceLevel); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ReplaceAvailability 以「整批覆寫」方式存員工某店的逐小時可上時段:
// 在同一交易內先刪掉該員工該店所有時段,再插入這次塗的(只存 preference 1/2)。
// 整批覆寫讓提交具冪等性,前端只要送出當下整張網格塗到的格子即可。
func (r *Repository) ReplaceAvailability(ctx context.Context, employeeID, storeID string, slots []HourSlot) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // 已 commit 時為 no-op

	if _, err := tx.Exec(ctx, `
		DELETE FROM availability_slots
		WHERE employee_id = $1::uuid AND store_id = $2::uuid`,
		employeeID, storeID); err != nil {
		return err
	}

	for _, s := range slots {
		if _, err := tx.Exec(ctx, `
			INSERT INTO availability_slots (employee_id, store_id, weekday, hour, preference_level)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5)`,
			employeeID, storeID, s.Weekday, s.Hour, s.PreferenceLevel); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
