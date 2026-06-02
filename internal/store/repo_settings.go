package store

import "context"

// 這個檔是 v1.5 階段 B 老闆端的兩項設定:營業時段 + 逐小時需求人數。
// 兩者都是「整批覆寫」語意,讓管理台送出當下整張表即可(冪等)。

// GetStoreHours 取某店營業時段;店不存在回 pgx.ErrNoRows。
func (r *Repository) GetStoreHours(ctx context.Context, storeID string) (StoreHours, error) {
	var h StoreHours
	err := r.pool.QueryRow(ctx, `
		SELECT open_hour, close_hour FROM stores WHERE id = $1::uuid`, storeID).
		Scan(&h.OpenHour, &h.CloseHour)
	return h, err
}

// SetStoreHours 更新某店營業時段;店不存在回 pgx.ErrNoRows。
func (r *Repository) SetStoreHours(ctx context.Context, storeID string, open, close int) (StoreHours, error) {
	var h StoreHours
	err := r.pool.QueryRow(ctx, `
		UPDATE stores SET open_hour = $2, close_hour = $3
		WHERE id = $1::uuid
		RETURNING open_hour, close_hour`, storeID, open, close).
		Scan(&h.OpenHour, &h.CloseHour)
	return h, err
}

// GetRequirements 取某店所有逐小時需求(只回 headcount>0 的列)。
func (r *Repository) GetRequirements(ctx context.Context, storeID string) ([]Requirement, error) {
	const q = `
		SELECT weekday, hour, headcount
		FROM staffing_requirements
		WHERE store_id = $1::uuid AND headcount > 0
		ORDER BY weekday, hour`
	rows, err := r.pool.Query(ctx, q, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Requirement{}
	for rows.Next() {
		var req Requirement
		if err := rows.Scan(&req.Weekday, &req.Hour, &req.Headcount); err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, rows.Err()
}

// ReplaceRequirements 整批覆寫某店的逐小時需求:同一交易內先清空該店所有需求,
// 再插入這次送來的(只插 headcount>0,把設回 0 的格子當成刪除)。
func (r *Repository) ReplaceRequirements(ctx context.Context, storeID string, reqs []Requirement) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // 已 commit 時為 no-op

	if _, err := tx.Exec(ctx, `DELETE FROM staffing_requirements WHERE store_id = $1::uuid`, storeID); err != nil {
		return err
	}
	for _, req := range reqs {
		if req.Headcount <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO staffing_requirements (store_id, weekday, hour, headcount)
			VALUES ($1::uuid, $2, $3, $4)`,
			storeID, req.Weekday, req.Hour, req.Headcount); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
