package store

import (
	"net/http"
	"strings"

	"schedule-lite/internal/platform/httpx"
)

// 這個檔處理 v1.5 階段 B 老闆端的兩項店面設定:
//   - 營業時段 GET/PUT /api/store-hours
//   - 逐小時需求 GET/PUT /api/requirements

func (h *Handler) getStoreHours(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	hours, err := h.repo.GetStoreHours(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢營業時段")
		return
	}
	httpx.JSON(w, http.StatusOK, hours)
}

func (h *Handler) putStoreHours(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	var body struct {
		OpenHour  int `json:"open_hour"`
		CloseHour int `json:"close_hour"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return
	}
	if body.OpenHour < 0 || body.OpenHour > 24 || body.CloseHour < 0 || body.CloseHour > 24 {
		httpx.Error(w, http.StatusBadRequest, "營業時段需介於 0~24")
		return
	}
	if body.OpenHour >= body.CloseHour {
		httpx.Error(w, http.StatusBadRequest, "開店時間需早於關店時間")
		return
	}
	hours, err := h.repo.SetStoreHours(r.Context(), storeID, body.OpenHour, body.CloseHour)
	if err != nil {
		h.writeDBError(w, err, "更新營業時段")
		return
	}
	httpx.JSON(w, http.StatusOK, hours)
}

func (h *Handler) getRequirements(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	reqs, err := h.repo.GetRequirements(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢需求人數")
		return
	}
	httpx.JSON(w, http.StatusOK, reqs)
}

func (h *Handler) putRequirements(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	var body struct {
		Requirements []Requirement `json:"requirements"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return
	}
	for _, req := range body.Requirements {
		if req.Weekday < 0 || req.Weekday > 6 {
			httpx.Error(w, http.StatusBadRequest, "weekday 需介於 0~6")
			return
		}
		if req.Hour < 0 || req.Hour > 23 {
			httpx.Error(w, http.StatusBadRequest, "hour 需介於 0~23")
			return
		}
		if req.Headcount < 0 {
			httpx.Error(w, http.StatusBadRequest, "headcount 不可為負")
			return
		}
	}
	if err := h.repo.ReplaceRequirements(r.Context(), storeID, body.Requirements); err != nil {
		h.writeDBError(w, err, "儲存需求人數")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"saved": len(body.Requirements)})
}
