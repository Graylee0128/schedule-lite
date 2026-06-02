package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 用 pgx 連線池存取資料。
//
// 註:SQL 裡刻意用 `id::text`(輸出)與 `$1::uuid`(輸入)做轉型,
// 這樣 Go 端一律用 string 處理 UUID,不必引入額外型別。
// 之後這層會用 sqlc 產生的型別安全程式碼取代(plan.md 下一步)。
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository 建立 Repository。
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// storeCols 是 Store 的共用查詢欄位(含 v1.5 階段 B 的營業時段)。
const storeCols = `id::text, organization_id::text, name, open_hour, close_hour, created_at`

func scanStore(row rowScanner) (Store, error) {
	var s Store
	err := row.Scan(&s.ID, &s.OrganizationID, &s.Name, &s.OpenHour, &s.CloseHour, &s.CreatedAt)
	return s, err
}

// rowScanner 同時相容 pgx 的 Row 與 Rows。
type rowScanner interface {
	Scan(dest ...any) error
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

// CreateStore 在指定組織底下建立門市。
// v1.5 階段 B 起不再 seed 4 班別;營業時段用 DB 預設(09–22),老闆之後在管理台調整。
func (r *Repository) CreateStore(ctx context.Context, orgID, name string) (Store, error) {
	const q = `
		INSERT INTO stores (organization_id, name)
		VALUES ($1::uuid, $2)
		RETURNING ` + storeCols
	return scanStore(r.pool.QueryRow(ctx, q, orgID, name))
}

// ListStores 列出某組織的所有門市。
func (r *Repository) ListStores(ctx context.Context, orgID string) ([]Store, error) {
	const q = `SELECT ` + storeCols + `
		FROM stores WHERE organization_id = $1::uuid ORDER BY created_at`
	rows, err := r.pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Store
	for rows.Next() {
		s, err := scanStore(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CreateEmployee 在指定組織底下建立員工,並在同一交易內把他加入該組織**所有現有門市**
//(membership 預設全店,老闆之後可增減)。phone 可為 nil(NULL)。
func (r *Repository) CreateEmployee(ctx context.Context, orgID, name string, phone *string) (Employee, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Employee{}, err
	}
	defer tx.Rollback(ctx) // 已 commit 時為 no-op

	var e Employee
	err = tx.QueryRow(ctx, `
		INSERT INTO employees (organization_id, name, phone)
		VALUES ($1::uuid, $2, $3)
		RETURNING id::text, organization_id::text, name, phone, created_at`, orgID, name, phone).
		Scan(&e.ID, &e.OrganizationID, &e.Name, &e.Phone, &e.CreatedAt)
	if err != nil {
		return Employee{}, err
	}

	// 預設加入該組織所有門市。
	if _, err = tx.Exec(ctx, `
		INSERT INTO employee_store_memberships (employee_id, store_id)
		SELECT $1::uuid, s.id FROM stores s WHERE s.organization_id = $2::uuid
		ON CONFLICT DO NOTHING`, e.ID, orgID); err != nil {
		return Employee{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return Employee{}, err
	}
	return e, nil
}

// --- 員工 ↔ 門市 membership(v1.5 階段 A)---

// ListMembershipStores 列出某員工目前隸屬(可填班)的門市。
func (r *Repository) ListMembershipStores(ctx context.Context, employeeID string) ([]Store, error) {
	const q = `
		SELECT s.id::text, s.organization_id::text, s.name, s.open_hour, s.close_hour, s.created_at
		FROM employee_store_memberships m
		JOIN stores s ON s.id = m.store_id
		WHERE m.employee_id = $1::uuid AND m.is_active
		ORDER BY s.created_at`
	rows, err := r.pool.Query(ctx, q, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Store{}
	for rows.Next() {
		s, err := scanStore(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// AddMembership 把員工加入門市(已存在則重新啟用)。
func (r *Repository) AddMembership(ctx context.Context, employeeID, storeID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO employee_store_memberships (employee_id, store_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (employee_id, store_id) DO UPDATE SET is_active = true`,
		employeeID, storeID)
	return err
}

// RemoveMembership 把員工移出門市。
func (r *Repository) RemoveMembership(ctx context.Context, employeeID, storeID string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM employee_store_memberships
		WHERE employee_id = $1::uuid AND store_id = $2::uuid`,
		employeeID, storeID)
	return err
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
