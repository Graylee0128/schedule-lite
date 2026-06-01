package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 用 pgx 連線池存取資料。
//
// 註:SQL 裡刻意用 `id::text`(輸出)與 `$1::uuid` / `$3::time`(輸入)做轉型,
// 這樣 Go 端一律用 string 處理 UUID 與時間,不必引入額外型別。
// 之後這層會用 sqlc 產生的型別安全程式碼取代(plan.md 下一步)。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 建立 Repository。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// defaultShiftTemplates 是每間店建立時自動帶的 4 個預設班別(早/中/晚/大夜)。
// 對應 design §3.9 的 7×4 格子「列」。店家之後可改。
var defaultShiftTemplates = []struct {
	Name, Start, End string
	Headcount        int
}{
	{"早班", "06:00", "12:00", 1},
	{"中班", "12:00", "18:00", 1},
	{"晚班", "18:00", "24:00", 1},
	{"大夜", "00:00", "06:00", 1},
}

// CreateOrganization 建立組織,回傳含 DB 產生的 id 的完整資料。
func (r *Repository) CreateOrganization(ctx context.Context, name, timezone string) (Organization, error) {
	const q = `
		INSERT INTO organizations (name, timezone)
		VALUES ($1, $2)
		RETURNING id::text, name, timezone, week_start, created_at`
	var o Organization
	err := r.pool.QueryRow(ctx, q, name, timezone).
		Scan(&o.ID, &o.Name, &o.Timezone, &o.WeekStart, &o.CreatedAt)
	return o, err
}

// ListOrganizations 列出所有組織(目前無店長身分,先全列;有 auth 後再依擁有者過濾)。
func (r *Repository) ListOrganizations(ctx context.Context) ([]Organization, error) {
	const q = `
		SELECT id::text, name, timezone, week_start, created_at
		FROM organizations
		ORDER BY created_at`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Organization{}
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Timezone, &o.WeekStart, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CreateStore 在指定組織底下建立門市,並在同一交易內 seed 4 個預設班別。
func (r *Repository) CreateStore(ctx context.Context, orgID, name string) (Store, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Store{}, err
	}
	defer tx.Rollback(ctx) // 已 commit 時為 no-op

	var s Store
	err = tx.QueryRow(ctx, `
		INSERT INTO stores (organization_id, name)
		VALUES ($1::uuid, $2)
		RETURNING id::text, organization_id::text, name, created_at`, orgID, name).
		Scan(&s.ID, &s.OrganizationID, &s.Name, &s.CreatedAt)
	if err != nil {
		return Store{}, err
	}

	for _, t := range defaultShiftTemplates {
		if _, err = tx.Exec(ctx, `
			INSERT INTO shift_templates (store_id, name, start_local, end_local, required_headcount, required_skills)
			VALUES ($1::uuid, $2, $3::time, $4::time, $5, $6)`,
			s.ID, t.Name, t.Start, t.End, t.Headcount, []byte("[]")); err != nil {
			return Store{}, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return Store{}, err
	}
	return s, nil
}

// ListStores 列出某組織的所有門市。
func (r *Repository) ListStores(ctx context.Context, orgID string) ([]Store, error) {
	const q = `
		SELECT id::text, organization_id::text, name, created_at
		FROM stores
		WHERE organization_id = $1::uuid
		ORDER BY created_at`
	rows, err := r.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Store
	for rows.Next() {
		var s Store
		if err := rows.Scan(&s.ID, &s.OrganizationID, &s.Name, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CreateEmployee 在指定組織底下建立員工。phone 可為 nil(NULL)。
func (r *Repository) CreateEmployee(ctx context.Context, orgID, name string, phone *string) (Employee, error) {
	const q = `
		INSERT INTO employees (organization_id, name, phone)
		VALUES ($1::uuid, $2, $3)
		RETURNING id::text, organization_id::text, name, phone, created_at`
	var e Employee
	err := r.pool.QueryRow(ctx, q, orgID, name, phone).
		Scan(&e.ID, &e.OrganizationID, &e.Name, &e.Phone, &e.CreatedAt)
	return e, err
}

// ListEmployees 列出某組織的所有員工。
func (r *Repository) ListEmployees(ctx context.Context, orgID string) ([]Employee, error) {
	const q = `
		SELECT id::text, organization_id::text, name, phone, created_at
		FROM employees
		WHERE organization_id = $1::uuid
		ORDER BY created_at`
	rows, err := r.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Employee
	for rows.Next() {
		var e Employee
		if err := rows.Scan(&e.ID, &e.OrganizationID, &e.Name, &e.Phone, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- 班別模板(shift_templates)---

const shiftTemplateCols = `id::text, store_id::text, name, start_local::text, end_local::text, required_headcount, required_skills, created_at`

// ListShiftTemplates 列出某門市的班別模板(依開始時間排序)。
func (r *Repository) ListShiftTemplates(ctx context.Context, storeID string) ([]ShiftTemplate, error) {
	const q = `SELECT ` + shiftTemplateCols + `
		FROM shift_templates WHERE store_id = $1::uuid ORDER BY start_local`
	rows, err := r.pool.Query(ctx, q, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ShiftTemplate
	for rows.Next() {
		t, err := scanShiftTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateShiftTemplate 新增一個班別模板。
func (r *Repository) CreateShiftTemplate(ctx context.Context, storeID, name, start, end string, headcount int, skills []string) (ShiftTemplate, error) {
	const q = `
		INSERT INTO shift_templates (store_id, name, start_local, end_local, required_headcount, required_skills)
		VALUES ($1::uuid, $2, $3::time, $4::time, $5, $6)
		RETURNING ` + shiftTemplateCols
	row := r.pool.QueryRow(ctx, q, storeID, name, start, end, headcount, marshalSkills(skills))
	return scanShiftTemplate(row)
}

// UpdateShiftTemplate 全欄更新一個班別模板;找不到 id 會回 pgx.ErrNoRows。
func (r *Repository) UpdateShiftTemplate(ctx context.Context, id, name, start, end string, headcount int, skills []string) (ShiftTemplate, error) {
	const q = `
		UPDATE shift_templates
		SET name = $2, start_local = $3::time, end_local = $4::time,
		    required_headcount = $5, required_skills = $6
		WHERE id = $1::uuid
		RETURNING ` + shiftTemplateCols
	row := r.pool.QueryRow(ctx, q, id, name, start, end, headcount, marshalSkills(skills))
	return scanShiftTemplate(row)
}

// DeleteShiftTemplate 刪除一個班別模板,回傳是否真的刪到。
func (r *Repository) DeleteShiftTemplate(ctx context.Context, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM shift_templates WHERE id = $1::uuid`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// rowScanner 同時相容 pgx 的 Row 與 Rows。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanShiftTemplate(row rowScanner) (ShiftTemplate, error) {
	var t ShiftTemplate
	var rawSkills []byte
	if err := row.Scan(&t.ID, &t.StoreID, &t.Name, &t.StartLocal, &t.EndLocal, &t.RequiredHeadcount, &rawSkills, &t.CreatedAt); err != nil {
		return ShiftTemplate{}, err
	}
	t.RequiredSkills = unmarshalSkills(rawSkills)
	return t, nil
}

// marshalSkills 把技能清單轉成 jsonb 用的 bytes(nil → "[]")。
func marshalSkills(skills []string) []byte {
	if skills == nil {
		skills = []string{}
	}
	b, _ := json.Marshal(skills)
	return b
}

// unmarshalSkills 把 jsonb bytes 轉回 []string(空 → 空切片)。
func unmarshalSkills(raw []byte) []string {
	out := []string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = []string{}
	}
	return out
}
