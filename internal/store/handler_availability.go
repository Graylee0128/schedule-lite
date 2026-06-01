package store

import (
	"net/http"
	"strings"

	"schedule-lite/internal/platform/httpx"
)

// 這個檔處理 Step 5「員工填班」的兩端:
//   - 店長端:POST /api/access-links 產生 magic-link。
//   - 員工端:GET/PUT /api/availability(用連結裡的 token 認證,免註冊登入)。

// createAccessLink 店長為某員工 + 某店產生一條填班 magic-link。
func (h *Handler) createAccessLink(w http.ResponseWriter, r *http.Request) {
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

	raw, err := h.repo.CreateAccessToken(r.Context(), req.EmployeeID, req.StoreID)
	if err != nil {
		h.writeDBError(w, err, "建立填班連結")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]string{
		"token": raw,
		"url":   "/a/" + raw, // 前端自行接上 origin 組成完整連結
	})
}

// getAvailability 員工開啟填班頁時拉的資料包(token 在 query)。
func (h *Handler) getAvailability(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		httpx.Error(w, http.StatusUnauthorized, "缺少連結權杖")
		return
	}
	emp, store, err := h.repo.ResolveToken(r.Context(), token)
	if err != nil {
		h.writeTokenError(w, err)
		return
	}

	templates, err := h.repo.ListShiftTemplates(r.Context(), store.ID)
	if err != nil {
		h.writeDBError(w, err, "查詢班別")
		return
	}
	slots, err := h.repo.GetAvailability(r.Context(), emp.ID, store.ID)
	if err != nil {
		h.writeDBError(w, err, "查詢可上時段")
		return
	}
	httpx.JSON(w, http.StatusOK, AvailabilityContext{
		Employee:  emp,
		Store:     store,
		Templates: templates,
		Slots:     slots,
	})
}

// putAvailability 員工送出整張可上時段表(整批覆寫)。
func (h *Handler) putAvailability(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		httpx.Error(w, http.StatusUnauthorized, "缺少連結權杖")
		return
	}
	emp, store, err := h.repo.ResolveToken(r.Context(), token)
	if err != nil {
		h.writeTokenError(w, err)
		return
	}

	var body struct {
		Slots []AvailabilitySlot `json:"slots"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return
	}
	for _, s := range body.Slots {
		if s.Weekday < 0 || s.Weekday > 6 {
			httpx.Error(w, http.StatusBadRequest, "weekday 需介於 0~6")
			return
		}
		if s.PreferenceLevel < 0 || s.PreferenceLevel > 2 {
			httpx.Error(w, http.StatusBadRequest, "preference_level 需為 0/1/2")
			return
		}
		if strings.TrimSpace(s.ShiftTemplateID) == "" {
			httpx.Error(w, http.StatusBadRequest, "每筆時段需有 shift_template_id")
			return
		}
	}

	if err := h.repo.ReplaceAvailability(r.Context(), emp.ID, store.ID, body.Slots); err != nil {
		h.writeDBError(w, err, "儲存可上時段")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"saved": len(body.Slots)})
}

// writeTokenError 把 token 解析失敗統一翻成 401(連結無效或過期)。
func (h *Handler) writeTokenError(w http.ResponseWriter, err error) {
	// ResolveToken 查無資料 → 連結無效/已撤銷/過期;格式錯誤(壞 token)也當無效。
	httpx.Error(w, http.StatusUnauthorized, "連結無效或已過期,請向店長索取新連結")
	h.log.Warn("解析填班連結失敗", "err", err)
}
