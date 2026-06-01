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

	mux.HandleFunc("GET /api/shift-templates", h.listShiftTemplates)
	mux.HandleFunc("POST /api/shift-templates", h.createShiftTemplate)
	mux.HandleFunc("PUT /api/shift-templates/{id}", h.updateShiftTemplate)
	mux.HandleFunc("DELETE /api/shift-templates/{id}", h.deleteShiftTemplate)

	// Step 5:員工填班(magic-link)。
	mux.HandleFunc("POST /api/access-links", h.createAccessLink)
	mux.HandleFunc("GET /api/availability", h.getAvailability)
	mux.HandleFunc("PUT /api/availability", h.putAvailability)

	// Step 6:缺口分析。
	mux.HandleFunc("GET /api/coverage", h.getCoverage)
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

// --- 班別模板 ---

func (h *Handler) listShiftTemplates(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	templates, err := h.repo.ListShiftTemplates(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢班別模板")
		return
	}
	if templates == nil {
		templates = []ShiftTemplate{}
	}
	httpx.JSON(w, http.StatusOK, templates)
}

func (h *Handler) createShiftTemplate(w http.ResponseWriter, r *http.Request) {
	req, ok := h.decodeShiftTemplate(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.StoreID) == "" {
		httpx.Error(w, http.StatusBadRequest, "store_id 必填")
		return
	}
	t, err := h.repo.CreateShiftTemplate(r.Context(), req.StoreID, req.Name, req.StartLocal, req.EndLocal, req.Headcount, req.Skills)
	if err != nil {
		h.writeDBError(w, err, "建立班別模板")
		return
	}
	httpx.JSON(w, http.StatusCreated, t)
}

func (h *Handler) updateShiftTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, ok := h.decodeShiftTemplate(w, r)
	if !ok {
		return
	}
	t, err := h.repo.UpdateShiftTemplate(r.Context(), id, req.Name, req.StartLocal, req.EndLocal, req.Headcount, req.Skills)
	if err != nil {
		h.writeDBError(w, err, "更新班別模板")
		return
	}
	httpx.JSON(w, http.StatusOK, t)
}

func (h *Handler) deleteShiftTemplate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deleted, err := h.repo.DeleteShiftTemplate(r.Context(), id)
	if err != nil {
		h.writeDBError(w, err, "刪除班別模板")
		return
	}
	if !deleted {
		httpx.Error(w, http.StatusNotFound, "班別模板不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// shiftTemplateReq 是建立/更新班別模板的共用請求格式與驗證結果。
type shiftTemplateReq struct {
	StoreID    string
	Name       string
	StartLocal string
	EndLocal   string
	Headcount  int
	Skills     []string
}

// decodeShiftTemplate 解析並驗證 body;失敗時已寫好回應、回傳 ok=false。
func (h *Handler) decodeShiftTemplate(w http.ResponseWriter, r *http.Request) (shiftTemplateReq, bool) {
	var body struct {
		StoreID           string   `json:"store_id"`
		Name              string   `json:"name"`
		StartLocal        string   `json:"start_local"`
		EndLocal          string   `json:"end_local"`
		RequiredHeadcount *int     `json:"required_headcount"`
		RequiredSkills    []string `json:"required_skills"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return shiftTemplateReq{}, false
	}

	req := shiftTemplateReq{
		StoreID:    strings.TrimSpace(body.StoreID),
		Name:       strings.TrimSpace(body.Name),
		StartLocal: strings.TrimSpace(body.StartLocal),
		EndLocal:   strings.TrimSpace(body.EndLocal),
		Headcount:  1, // 預設 1 人
		Skills:     body.RequiredSkills,
	}
	if body.RequiredHeadcount != nil {
		req.Headcount = *body.RequiredHeadcount
	}

	if req.Name == "" || req.StartLocal == "" || req.EndLocal == "" {
		httpx.Error(w, http.StatusBadRequest, "name、start_local、end_local 皆必填")
		return shiftTemplateReq{}, false
	}
	if req.Headcount < 0 {
		httpx.Error(w, http.StatusBadRequest, "required_headcount 不可為負")
		return shiftTemplateReq{}, false
	}
	return req, true
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
