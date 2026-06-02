package store

import "context"

// 這個檔是缺口分析的查詢:把員工填的逐小時 availability 依 (weekday × hour)
// 聚合成可上人數,handler 再跟逐小時需求人數相減算缺口(v1.5 階段 B)。

// AvailabilityCounts 取某店各 (weekday × hour) 的可上人數統計(只存正向,故只有 want/ok)。
func (r *Repository) AvailabilityCounts(ctx context.Context, storeID string) ([]HourCount, error) {
	const q = `
		SELECT weekday, hour,
		       count(*) FILTER (WHERE preference_level = 2) AS want,
		       count(*) FILTER (WHERE preference_level = 1) AS ok
		FROM availability_slots
		WHERE store_id = $1::uuid
		GROUP BY weekday, hour`
	rows, err := r.pool.Query(ctx, q, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []HourCount{}
	for rows.Next() {
		var c HourCount
		if err := rows.Scan(&c.Weekday, &c.Hour, &c.Want, &c.Ok); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// NotFilledEmployees 列出「是這間店的 membership、但還沒提交可上時段」的員工。
// 用 membership(誰被指派到這店)+ availability_submissions(提交標記)判斷,
// 這樣能分辨「沒回應」與「提交了但整週都不行」(後者有提交紀錄,不算未填)。
func (r *Repository) NotFilledEmployees(ctx context.Context, storeID string) ([]IDName, error) {
	const q = `
		SELECT e.id::text, e.name
		FROM employee_store_memberships m
		JOIN employees e ON e.id = m.employee_id
		WHERE m.store_id = $1::uuid AND m.is_active
		  AND NOT EXISTS (
			SELECT 1 FROM availability_submissions a
			WHERE a.employee_id = e.id AND a.store_id = $1::uuid
		)
		ORDER BY e.name`
	rows, err := r.pool.Query(ctx, q, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []IDName{}
	for rows.Next() {
		var p IDName
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
