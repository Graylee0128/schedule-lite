package store

import "context"

// 這個檔是 Step 6「缺口分析」的查詢:把員工填的 availability 依
// (班別 × 星期) 聚合成人數,handler 再跟班別需求人數相減算缺口。

// AvailabilityCounts 取某店各 (班別 × 星期) 的偏好人數統計(只算循環 weekday)。
func (r *Repository) AvailabilityCounts(ctx context.Context, storeID string) ([]AvailabilityCount, error) {
	const q = `
		SELECT shift_template_id::text, weekday,
		       count(*) FILTER (WHERE preference_level = 2) AS want,
		       count(*) FILTER (WHERE preference_level = 1) AS ok,
		       count(*) FILTER (WHERE preference_level = 0) AS no
		FROM availability_slots
		WHERE store_id = $1::uuid AND weekday IS NOT NULL AND shift_template_id IS NOT NULL
		GROUP BY shift_template_id, weekday`
	rows, err := r.pool.Query(ctx, q, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AvailabilityCount{}
	for rows.Next() {
		var c AvailabilityCount
		if err := rows.Scan(&c.ShiftTemplateID, &c.Weekday, &c.Want, &c.Ok, &c.No); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// NotFilledEmployees 列出「拿過這間店連結、但一格都還沒填」的員工。
// 沒有店長 auth / membership,所以用「發過 access token」當作「被期待要填」的依據。
func (r *Repository) NotFilledEmployees(ctx context.Context, storeID string) ([]IDName, error) {
	const q = `
		SELECT DISTINCT e.id::text, e.name
		FROM employees e
		JOIN employee_access_tokens t ON t.employee_id = e.id AND t.store_id = $1::uuid
		WHERE NOT EXISTS (
			SELECT 1 FROM availability_slots a
			WHERE a.employee_id = e.id AND a.store_id = $1::uuid AND a.weekday IS NOT NULL
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
