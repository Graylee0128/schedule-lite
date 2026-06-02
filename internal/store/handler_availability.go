package store

import (
	"net/http"
	"strings"

	"schedule-lite/internal/platform/httpx"
)

// 這個檔處理 Step 5「員工填班」的兩端:
//   - 店長端:POST /api/access-links 產生 magic-link。
//   - 員工端:GET/PUT /api/availability(用連結裡的 token 認證,免註冊登入)。

// createAccessLink 店長為某**員工**產生一條填班 magic-link(v1.5 起不綁門市)。
func (h *Handler) createAccessLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EmployeeID string `json:"employee_id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return
	}
	req.EmployeeID = strings.TrimSpace(req.EmployeeID)
	if req.EmployeeID == "" {
		httpx.Error(w, http.StatusBadRequest, "employee_id 必填")
		return
	}

	raw, err := h.repo.CreateAccessToken(r.Context(), req.EmployeeID)
	if err != nil {
		h.writeDBError(w, err, "建立填班連結")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]string{
		"token": raw,
		"url":   "/a/" + raw, // 前端自行接上 origin 組成完整連結
	})
}

// getMe 員工開連結後的初始資料:我是誰 + 我能填哪些門市(token 在 query)。
func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		httpx.Error(w, http.StatusUnauthorized, "缺少連結權杖")
		return
	}
	emp, err := h.repo.ResolveToken(r.Context(), token)
	if err != nil {
		h.writeTokenError(w, err)
		return
	}
	stores, err := h.repo.ListMembershipStores(r.Context(), emp.ID)
	if err != nil {
		h.writeDBError(w, err, "查詢可填門市")
		return
	}
	httpx.JSON(w, http.StatusOK, MeContext{Employee: emp, Stores: stores})
}

// getAvailability 員工選定門市後拉該店的營業時段與已塗時段(token + store_id 在 query)。
func (h *Handler) getAvailability(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if token == "" {
		httpx.Error(w, http.StatusUnauthorized, "缺少連結權杖")
		return
	}
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	emp, store, err := h.repo.ResolveTokenForStore(r.Context(), token, storeID)
	if err != nil {
		h.writeTokenError(w, err)
		return
	}

	hours, err := h.repo.GetStoreHours(r.Context(), store.ID)
	if err != nil {
		h.writeDBError(w, err, "查詢營業時段")
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
		OpenHour:  hours.OpenHour,
		CloseHour: hours.CloseHour,
		Slots:     slots,
	})
}

// putAvailability 員工送出某門市整張可上時段表(整批覆寫),並記提交標記。
func (h *Handler) putAvailability(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if token == "" {
		httpx.Error(w, http.StatusUnauthorized, "缺少連結權杖")
		return
	}
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	emp, store, err := h.repo.ResolveTokenForStore(r.Context(), token, storeID)
	if err != nil {
		h.writeTokenError(w, err)
		return
	}

	var body struct {
		Slots []HourSlot `json:"slots"`
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
		if s.Hour < 0 || s.Hour > 23 {
			httpx.Error(w, http.StatusBadRequest, "hour 需介於 0~23")
			return
		}
		// 只接受正向偏好(1/2);未塗 = 絕對不行不落 DB,不該出現在送出清單。
		if s.PreferenceLevel < 1 || s.PreferenceLevel > 2 {
			httpx.Error(w, http.StatusBadRequest, "preference_level 需為 1(可配合)或 2(非常想上)")
			return
		}
	}

	if err := h.repo.ReplaceAvailability(r.Context(), emp.ID, store.ID, body.Slots); err != nil {
		h.writeDBError(w, err, "儲存可上時段")
		return
	}
	// 記提交標記(即使整週都不選,也代表「已回應」)。
	if err := h.repo.MarkSubmitted(r.Context(), emp.ID, store.ID); err != nil {
		h.writeDBError(w, err, "記錄提交")
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
