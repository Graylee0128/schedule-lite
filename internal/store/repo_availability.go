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

// CreateAccessToken 為 (員工, 門市) 產生一個 magic-link,回傳原始 token。
// 原始 token 只在這裡回傳一次(之後 DB 只剩 hash)。此版不設過期。
func (r *Repository) CreateAccessToken(ctx context.Context, employeeID, storeID string) (string, error) {
	raw, hash, err := newToken()
	if err != nil {
		return "", err
	}
	const q = `
		INSERT INTO employee_access_tokens (employee_id, store_id, token_hash)
		VALUES ($1::uuid, $2::uuid, $3)`
	if _, err := r.pool.Exec(ctx, q, employeeID, storeID, hash); err != nil {
		return "", err
	}
	return raw, nil
}

// ResolveToken 用原始 token 解出 (員工, 門市) 精簡資料;
// 連結無效 / 已撤銷 / 已過期都會回 pgx.ErrNoRows。
func (r *Repository) ResolveToken(ctx context.Context, raw string) (emp, store IDName, err error) {
	const q = `
		SELECT e.id::text, e.name, s.id::text, s.name
		FROM employee_access_tokens t
		JOIN employees e ON e.id = t.employee_id
		JOIN stores    s ON s.id = t.store_id
		WHERE t.token_hash = $1
		  AND t.revoked_at IS NULL
		  AND (t.expires_at IS NULL OR t.expires_at > now())`
	err = r.pool.QueryRow(ctx, q, hashToken(raw)).
		Scan(&emp.ID, &emp.Name, &store.ID, &store.Name)
	return emp, store, err
}

// GetAvailability 取某員工在某店「循環(weekday)」的可上時段。
func (r *Repository) GetAvailability(ctx context.Context, employeeID, storeID string) ([]AvailabilitySlot, error) {
	const q = `
		SELECT shift_template_id::text, weekday, preference_level
		FROM availability_slots
		WHERE employee_id = $1::uuid AND store_id = $2::uuid AND weekday IS NOT NULL
		ORDER BY weekday`
	rows, err := r.pool.Query(ctx, q, employeeID, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AvailabilitySlot{}
	for rows.Next() {
		var s AvailabilitySlot
		if err := rows.Scan(&s.ShiftTemplateID, &s.Weekday, &s.PreferenceLevel); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ReplaceAvailability 以「整批覆寫」方式存員工某店的循環可上時段:
// 在同一交易內先刪掉該員工該店所有 weekday 時段,再插入這次提交的。
// 整批覆寫讓提交具冪等性,前端只要送出當下整張表即可。
func (r *Repository) ReplaceAvailability(ctx context.Context, employeeID, storeID string, slots []AvailabilitySlot) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // 已 commit 時為 no-op

	if _, err := tx.Exec(ctx, `
		DELETE FROM availability_slots
		WHERE employee_id = $1::uuid AND store_id = $2::uuid AND weekday IS NOT NULL`,
		employeeID, storeID); err != nil {
		return err
	}

	for _, s := range slots {
		if _, err := tx.Exec(ctx, `
			INSERT INTO availability_slots (employee_id, store_id, shift_template_id, weekday, preference_level)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5)`,
			employeeID, storeID, s.ShiftTemplateID, s.Weekday, s.PreferenceLevel); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
