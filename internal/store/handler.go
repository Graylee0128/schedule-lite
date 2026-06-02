package store

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"schedule-lite/internal/platform/httpx"
)

// Handler 是 store domain 的 HTTP 介面。
type Handler struct {
	repo *Repository
	log  *slog.Logger
}

// NewHandler 建立 Handler。
func NewHandler(repo *Repository, log *slog.Logger) *Handler {
	return &Handler{repo: repo, log: log}
}

// RegisterRoutes 把本 domain 的路由掛到 mux(Go 1.22 方法+路徑路由)。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/organizations", h.createOrganization)
	mux.HandleFunc("GET /api/organizations", h.listOrganizations)
	mux.HandleFunc("POST /api/stores", h.createStore)
	mux.HandleFunc("GET /api/stores", h.listStores)
	mux.HandleFunc("POST /api/employees", h.createEmployee)
	mux.HandleFunc("GET /api/employees", h.listEmployees)

	// v1.5 階段 A:員工 ↔ 門市 membership(店長端調整員工門市歸屬)。
	mux.HandleFunc("GET /api/memberships", h.listMemberships)
	mux.HandleFunc("POST /api/memberships", h.addMembership)
	mux.HandleFunc("DELETE /api/memberships", h.removeMembership)

	// v1.5 階段 B:老闆設營業時段 + 逐小時需求人數(取代固定 4 班別模板)。
	mux.HandleFunc("GET /api/store-hours", h.getStoreHours)
	mux.HandleFunc("PUT /api/store-hours", h.putStoreHours)
	mux.HandleFunc("GET /api/requirements", h.getRequirements)
	mux.HandleFunc("PUT /api/requirements", h.putRequirements)

	// Step 5 / v1.5:員工填班(magic-link,token 綁員工,多店)。
	mux.HandleFunc("POST /api/access-links", h.createAccessLink)
	mux.HandleFunc("GET /api/me", h.getMe)
	mux.HandleFunc("GET /api/availability", h.getAvailability)
	mux.HandleFunc("PUT /api/availability", h.putAvailability)

	// Step 6:缺口分析。
	mux.HandleFunc("GET /api/coverage", h.getCoverage)

	// v2:排班 grid + Rule Engine + 發布 + 匯出(店長端)。
	mux.HandleFunc("GET /api/schedule", h.getSchedule)
	mux.HandleFunc("PUT /api/schedule/assignments", h.putAssignments)
	mux.HandleFunc("POST /api/schedule/publish", h.publishSchedule)
	mux.HandleFunc("GET /api/schedule/export", h.exportSchedule)
	mux.HandleFunc("GET /api/employee-availability", h.getEmployeeAvailability)

	// v2:員工看自己班表 + 簡化雙階段(標記問題)。
	mux.HandleFunc("GET /api/my-schedule", h.getMySchedule)
	mux.HandleFunc("POST /api/my-schedule/issues", h.postMyIssue)
}

func (h *Handler) createOrganization(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name 不可為空")
		return
	}
	if strings.TrimSpace(req.Timezone) == "" {
		req.Timezone = "Asia/Taipei" // 預設時區
	}

	org, err := h.repo.CreateOrganization(r.Context(), req.Name, req.Timezone)
	if err != nil {
		h.writeDBError(w, err, "建立組織")
		return
	}
	httpx.JSON(w, http.StatusCreated, org)
}

func (h *Handler) listOrganizations(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.repo.ListOrganizations(r.Context())
	if err != nil {
		h.writeDBError(w, err, "查詢組織")
		return
	}
	httpx.JSON(w, http.StatusOK, orgs)
}

func (h *Handler) createStore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string `json:"organization_id"`
		Name           string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return
	}
	req.OrganizationID = strings.TrimSpace(req.OrganizationID)
	req.Name = strings.TrimSpace(req.Name)
	if req.OrganizationID == "" || req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "organization_id 與 name 皆必填")
		return
	}

	s, err := h.repo.CreateStore(r.Context(), req.OrganizationID, req.Name)
	if err != nil {
		h.writeDBError(w, err, "建立門市")
		return
	}
	httpx.JSON(w, http.StatusCreated, s)
}

func (h *Handler) listStores(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(r.URL.Query().Get("organization_id"))
	if orgID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 organization_id 查詢參數")
		return
	}
	stores, err := h.repo.ListStores(r.Context(), orgID)
	if err != nil {
		h.writeDBError(w, err, "查詢門市")
		return
	}
	if stores == nil {
		stores = []Store{} // 回 [] 而非 null
	}
	httpx.JSON(w, http.StatusOK, stores)
}

func (h *Handler) createEmployee(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrganizationID string  `json:"organization_id"`
		Name           string  `json:"name"`
		Phone          *string `json:"phone"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return
	}
	req.OrganizationID = strings.TrimSpace(req.OrganizationID)
	req.Name = strings.TrimSpace(req.Name)
	if req.OrganizationID == "" || req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "organization_id 與 name 皆必填")
		return
	}

	e, err := h.repo.CreateEmployee(r.Context(), req.OrganizationID, req.Name, req.Phone)
	if err != nil {
		h.writeDBError(w, err, "建立員工")
		return
	}
	httpx.JSON(w, http.StatusCreated, e)
}

func (h *Handler) listEmployees(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(r.URL.Query().Get("organization_id"))
	if orgID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 organization_id 查詢參數")
		return
	}
	employees, err := h.repo.ListEmployees(r.Context(), orgID)
	if err != nil {
		h.writeDBError(w, err, "查詢員工")
		return
	}
	if employees == nil {
		employees = []Employee{}
	}
	httpx.JSON(w, http.StatusOK, employees)
}

// --- 員工 ↔ 門市 membership ---

func (h *Handler) listMemberships(w http.ResponseWriter, r *http.Request) {
	empID := strings.TrimSpace(r.URL.Query().Get("employee_id"))
	if empID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 employee_id 查詢參數")
		return
	}
	stores, err := h.repo.ListMembershipStores(r.Context(), empID)
	if err != nil {
		h.writeDBError(w, err, "查詢門市歸屬")
		return
	}
	httpx.JSON(w, http.StatusOK, stores)
}

func (h *Handler) addMembership(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EmployeeID string `json:"employee_id"`
		StoreID    string `json:"store_id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return
	}
	req.EmployeeID = strings.TrimSpace(req.EmployeeID)
	req.StoreID = strings.TrimSpace(req.StoreID)
	if req.EmployeeID == "" || req.StoreID == "" {
		httpx.Error(w, http.StatusBadRequest, "employee_id 與 store_id 皆必填")
		return
	}
	if err := h.repo.AddMembership(r.Context(), req.EmployeeID, req.StoreID); err != nil {
		h.writeDBError(w, err, "加入門市")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removeMembership(w http.ResponseWriter, r *http.Request) {
	empID := strings.TrimSpace(r.URL.Query().Get("employee_id"))
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if empID == "" || storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 employee_id 與 store_id 查詢參數")
		return
	}
	if err := h.repo.RemoveMembership(r.Context(), empID, storeID); err != nil {
		h.writeDBError(w, err, "移出門市")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeDBError 把常見的 Postgres 錯誤翻成對使用者友善的 4xx,其餘當 500。
func (h *Handler) writeDBError(w http.ResponseWriter, err error, action string) {
	// 查無資料(更新/查詢單筆找不到)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, action+":找不到資料")
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "22P02": // invalid_text_representation:UUID 格式不對
			httpx.Error(w, http.StatusBadRequest, "ID 格式錯誤(需為 UUID)")
			return
		case "22007", "22008": // invalid_datetime_format:時間格式不對
			httpx.Error(w, http.StatusBadRequest, "時間格式錯誤(需為 HH:MM)")
			return
		case "23503": // foreign_key_violation:關聯對象不存在
			httpx.Error(w, http.StatusBadRequest, "關聯的資料不存在(id 是否正確?)")
			return
		case "23514": // check_violation:違反 CHECK 限制(如 weekday / preference 超出範圍)
			httpx.Error(w, http.StatusBadRequest, "欄位數值超出允許範圍")
			return
		}
	}
	h.log.Error(action+"失敗", "err", err)
	httpx.Error(w, http.StatusInternalServerError, action+"失敗")
}
