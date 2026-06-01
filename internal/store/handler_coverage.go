package store

import (
	"net/http"
	"strings"

	"schedule-lite/internal/platform/httpx"
)

// getCoverage 回傳某店一週的缺口分析:每個 (班別 × 星期) 的 需求 vs 可上,
// 加上「發了連結但還沒填」的員工名單。對應 design §3.9 的 7×4 heatmap。
func (h *Handler) getCoverage(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}

	templates, err := h.repo.ListShiftTemplates(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢班別")
		return
	}
	counts, err := h.repo.AvailabilityCounts(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "統計可上人數")
		return
	}
	notFilled, err := h.repo.NotFilledEmployees(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢未填名單")
		return
	}

	// 把統計結果索引成 map[班別_星期] 方便查;沒填的格子預設 0。
	type key struct {
		tid string
		wd  int
	}
	byCell := make(map[key]AvailabilityCount, len(counts))
	for _, c := range counts {
		byCell[key{c.ShiftTemplateID, c.Weekday}] = c
	}

	// 對每個班別 × 7 天展開,算出 available / gap。
	cells := make([]CoverageCell, 0, len(templates)*7)
	for _, t := range templates {
		for wd := 0; wd < 7; wd++ {
			c := byCell[key{t.ID, wd}] // 不存在時為零值(want/ok/no=0)
			available := c.Want + c.Ok
			cells = append(cells, CoverageCell{
				ShiftTemplateID: t.ID,
				Weekday:         wd,
				Required:        t.RequiredHeadcount,
				Want:            c.Want,
				Ok:              c.Ok,
				Available:       available,
				Gap:             t.RequiredHeadcount - available,
			})
		}
	}

	httpx.JSON(w, http.StatusOK, Coverage{
		StoreID:   storeID,
		Templates: templates,
		Cells:     cells,
		NotFilled: notFilled,
	})
}
