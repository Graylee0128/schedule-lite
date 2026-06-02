package store

import (
	"net/http"
	"strings"

	"schedule-lite/internal/platform/httpx"
)

// getCoverage 回傳某店一週的逐小時缺口分析:每個 (weekday × hour) 的 需求 vs 可上,
// 加上「發了連結但還沒提交」的員工名單。對應 design 決策 8.3 的「小時 × 星期」heatmap。
//
// 格子範圍 = 營業時段內的每個小時 × 7 天;只列出「有需求(headcount>0)或有人可上」的時段,
// 全空的時段不回(前端只畫有意義的列),避免營業時段大時 heatmap 過長。
func (h *Handler) getCoverage(w http.ResponseWriter, r *http.Request) {
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
	counts, err := h.repo.AvailabilityCounts(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "統計可上人數")
		return
	}
	reqs, err := h.repo.GetRequirements(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢需求人數")
		return
	}
	notFilled, err := h.repo.NotFilledEmployees(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢未填名單")
		return
	}

	type key struct{ wd, hr int }
	countBy := make(map[key]HourCount, len(counts))
	for _, c := range counts {
		countBy[key{c.Weekday, c.Hour}] = c
	}
	reqBy := make(map[key]int, len(reqs))
	for _, req := range reqs {
		reqBy[key{req.Weekday, req.Hour}] = req.Headcount
	}

	// 對營業時段內的每個小時 × 7 天展開;只保留有需求或有人可上的格子。
	cells := []CoverageCell{}
	for hr := hours.OpenHour; hr < hours.CloseHour; hr++ {
		for wd := 0; wd < 7; wd++ {
			c := countBy[key{wd, hr}] // 不存在時為零值
			required := reqBy[key{wd, hr}]
			available := c.Want + c.Ok
			if required == 0 && available == 0 {
				continue
			}
			cells = append(cells, CoverageCell{
				Weekday:   wd,
				Hour:      hr,
				Required:  required,
				Want:      c.Want,
				Ok:        c.Ok,
				Available: available,
				Gap:       required - available,
			})
		}
	}

	httpx.JSON(w, http.StatusOK, Coverage{
		StoreID:   storeID,
		OpenHour:  hours.OpenHour,
		CloseHour: hours.CloseHour,
		Cells:     cells,
		NotFilled: notFilled,
	})
}
