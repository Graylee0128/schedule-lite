package store

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"schedule-lite/internal/platform/httpx"
)

// v2 排班端點(店長端,與其他 /api/* 一樣暫無身分驗證):
//   GET  /api/schedule?store_id=            取/建 draft + 候選員工 + 需求 + 指派 + 驗證 + 已發布版的問題標記
//   PUT  /api/schedule/assignments?store_id= 整批覆寫某員工在 draft 的指派格 {employee_id, slots:[{weekday,hour}]}
//   POST /api/schedule/publish?store_id=     發布 draft(有硬違反 → 409)
//   GET  /api/schedule/export?store_id=      匯出 CSV(最近發布版;無則匯出 draft)
//   GET  /api/employee-availability?store_id=&employee_id=  某員工在該店的可上格(排班時當底圖)
// 員工端(magic-link):
//   GET  /api/my-schedule?token=&store_id=          看自己在該店已發布班表 + 自己標的問題
//   POST /api/my-schedule/issues?token=&store_id=    標記自己某格有問題 {weekday,hour,note}

var weekdayNames = []string{"週日", "週一", "週二", "週三", "週四", "週五", "週六"}

// validateDraft 把 DB 資料組成 Rule Engine 輸入並跑驗證。
func (h *Handler) validateDraft(r *http.Request, storeID string, employees []ScheduleEmployee, assignments []ScheduleAssignment) (ValidationReport, error) {
	ctx := r.Context()

	availRows, err := h.repo.StoreAvailabilityRows(ctx, storeID)
	if err != nil {
		return ValidationReport{}, err
	}
	availability := map[string]map[int]bool{}
	for _, a := range availRows {
		m := availability[a.EmployeeID]
		if m == nil {
			m = map[int]bool{}
			availability[a.EmployeeID] = m
		}
		m[slotKey(a.Weekday, a.Hour)] = true
	}

	reqRows, err := h.repo.GetRequirements(ctx, storeID)
	if err != nil {
		return ValidationReport{}, err
	}
	requirements := map[int]int{}
	for _, rq := range reqRows {
		requirements[slotKey(rq.Weekday, rq.Hour)] = rq.Headcount
	}

	empIDs := make([]string, len(employees))
	for i, e := range employees {
		empIDs[i] = e.ID
	}
	crossRows, err := h.repo.CrossStoreBusy(ctx, storeID, empIDs)
	if err != nil {
		return ValidationReport{}, err
	}
	crossBusy := map[string]map[int]bool{}
	for _, a := range crossRows {
		m := crossBusy[a.EmployeeID]
		if m == nil {
			m = map[int]bool{}
			crossBusy[a.EmployeeID] = m
		}
		m[slotKey(a.Weekday, a.Hour)] = true
	}

	return ValidateSchedule(ScheduleInput{
		Assignments:    assignments,
		Employees:      employees,
		Availability:   availability,
		Requirements:   requirements,
		CrossStoreBusy: crossBusy,
	}), nil
}

func (h *Handler) getSchedule(w http.ResponseWriter, r *http.Request) {
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
	draft, err := h.repo.GetOrCreateDraft(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "取得排班草稿")
		return
	}
	employees, err := h.repo.ListStoreEmployees(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢店內員工")
		return
	}
	reqs, err := h.repo.GetRequirements(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢需求人數")
		return
	}
	assignments, err := h.repo.ListAssignments(r.Context(), draft.ID)
	if err != nil {
		h.writeDBError(w, err, "查詢指派")
		return
	}
	validation, err := h.validateDraft(r, storeID, employees, assignments)
	if err != nil {
		h.writeDBError(w, err, "驗證班表")
		return
	}

	// 最近發布/鎖定版:給確認面板(員工確認狀態 + 問題標記)。
	issues := []ScheduleIssue{}
	confirmations := []Confirmation{}
	var published *ScheduleVersion
	if pub, ok, err := h.repo.LatestPublishedVersion(r.Context(), storeID); err != nil {
		h.writeDBError(w, err, "查詢已發布版本")
		return
	} else if ok {
		pubCopy := pub
		published = &pubCopy
		if issues, err = h.repo.ListIssues(r.Context(), pub.ID); err != nil {
			h.writeDBError(w, err, "查詢問題標記")
			return
		}
		if confirmations, err = h.repo.ListConfirmations(r.Context(), pub.ID); err != nil {
			h.writeDBError(w, err, "查詢確認狀態")
			return
		}
	}

	httpx.JSON(w, http.StatusOK, ScheduleContext{
		Version:       draft,
		OpenHour:      hours.OpenHour,
		CloseHour:     hours.CloseHour,
		Employees:     employees,
		Requirements:  reqs,
		Assignments:   assignments,
		Validation:    validation,
		Published:     published,
		Confirmations: confirmations,
		Issues:        issues,
	})
}

func (h *Handler) putAssignments(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	var body struct {
		EmployeeID string     `json:"employee_id"`
		Slots      []HourCell `json:"slots"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return
	}
	body.EmployeeID = strings.TrimSpace(body.EmployeeID)
	if body.EmployeeID == "" {
		httpx.Error(w, http.StatusBadRequest, "employee_id 必填")
		return
	}
	for _, c := range body.Slots {
		if c.Weekday < 0 || c.Weekday > 6 || c.Hour < 0 || c.Hour > 23 {
			httpx.Error(w, http.StatusBadRequest, "weekday 需 0~6、hour 需 0~23")
			return
		}
	}

	draft, err := h.repo.GetOrCreateDraft(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "取得排班草稿")
		return
	}
	if err := h.repo.ReplaceEmployeeAssignments(r.Context(), draft.ID, body.EmployeeID, body.Slots); err != nil {
		h.writeDBError(w, err, "儲存指派")
		return
	}

	// 回傳更新後的整張指派 + 重新驗證,讓前端即時刷新紅黃標。
	employees, err := h.repo.ListStoreEmployees(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢店內員工")
		return
	}
	assignments, err := h.repo.ListAssignments(r.Context(), draft.ID)
	if err != nil {
		h.writeDBError(w, err, "查詢指派")
		return
	}
	validation, err := h.validateDraft(r, storeID, employees, assignments)
	if err != nil {
		h.writeDBError(w, err, "驗證班表")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"assignments": assignments,
		"validation":  validation,
	})
}

// autofillSchedule v3 階段 A:一鍵建議排班。用現有需求 + 員工可上時段跑貪婪預排,
// **整批覆寫**目前 draft(老闆手動排的會被取代),回傳建議結果 + 驗證(保證無硬衝突)。
func (h *Handler) autofillSchedule(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	ctx := r.Context()
	draft, err := h.repo.GetOrCreateDraft(ctx, storeID)
	if err != nil {
		h.writeDBError(w, err, "取得排班草稿")
		return
	}
	employees, err := h.repo.ListStoreEmployees(ctx, storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢店內員工")
		return
	}
	reqs, err := h.repo.GetRequirements(ctx, storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢需求人數")
		return
	}
	availRows, err := h.repo.StoreAvailabilityRows(ctx, storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢可上時段")
		return
	}
	availability := map[string]map[int]int{}
	for _, a := range availRows {
		m := availability[a.EmployeeID]
		if m == nil {
			m = map[int]int{}
			availability[a.EmployeeID] = m
		}
		m[slotKey(a.Weekday, a.Hour)] = a.PreferenceLevel
	}
	empIDs := make([]string, len(employees))
	for i, e := range employees {
		empIDs[i] = e.ID
	}
	crossRows, err := h.repo.CrossStoreBusy(ctx, storeID, empIDs)
	if err != nil {
		h.writeDBError(w, err, "查詢跨店占用")
		return
	}
	crossBusy := map[string]map[int]bool{}
	for _, a := range crossRows {
		m := crossBusy[a.EmployeeID]
		if m == nil {
			m = map[int]bool{}
			crossBusy[a.EmployeeID] = m
		}
		m[slotKey(a.Weekday, a.Hour)] = true
	}

	suggested := SuggestSchedule(AutofillInput{
		Requirements:   reqs,
		Employees:      employees,
		Availability:   availability,
		CrossStoreBusy: crossBusy,
	})
	if err := h.repo.ReplaceAllAssignments(ctx, draft.ID, suggested); err != nil {
		h.writeDBError(w, err, "寫入建議排班")
		return
	}
	validation, err := h.validateDraft(r, storeID, employees, suggested)
	if err != nil {
		h.writeDBError(w, err, "驗證班表")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"suggested":   len(suggested),
		"assignments": suggested,
		"validation":  validation,
	})
}

func (h *Handler) publishSchedule(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	draft, err := h.repo.GetOrCreateDraft(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "取得排班草稿")
		return
	}
	employees, err := h.repo.ListStoreEmployees(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢店內員工")
		return
	}
	assignments, err := h.repo.ListAssignments(r.Context(), draft.ID)
	if err != nil {
		h.writeDBError(w, err, "查詢指派")
		return
	}
	validation, err := h.validateDraft(r, storeID, employees, assignments)
	if err != nil {
		h.writeDBError(w, err, "驗證班表")
		return
	}
	// 有硬違反 → 擋下發布(回報告讓前端標紅)。
	if !validation.Publishable {
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"error":      "有硬性衝突,請先排除後再發布",
			"validation": validation,
		})
		return
	}
	version, err := h.repo.PublishDraft(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "發布班表")
		return
	}
	// v3-B:對這版有班的員工 seed pending 確認(進入兩階段)。
	if err := h.repo.SeedConfirmations(r.Context(), version.ID); err != nil {
		h.writeDBError(w, err, "建立確認紀錄")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"version":    version,
		"validation": validation,
	})
}

// lockSchedule v3-B:老闆把最近一版 published 設為 locked(定案)。
// 採「軟截止 + 手動鎖」,不強制全員確認(店長說了算);前端會在未全確認時提醒。
func (h *Handler) lockSchedule(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	version, err := h.repo.LockVersion(r.Context(), storeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Error(w, http.StatusBadRequest, "沒有已發布的班表可鎖定")
			return
		}
		h.writeDBError(w, err, "鎖定班表")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"version": version})
}

// exportSchedule 匯出 CSV:把每位員工每天的連續小時併成班段。
func (h *Handler) exportSchedule(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	if storeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 查詢參數")
		return
	}
	// 優先匯出最近發布版;沒有就匯出目前 draft。
	versionID := ""
	if pub, ok, err := h.repo.LatestPublishedVersion(r.Context(), storeID); err != nil {
		h.writeDBError(w, err, "查詢已發布版本")
		return
	} else if ok {
		versionID = pub.ID
	} else {
		draft, err := h.repo.GetOrCreateDraft(r.Context(), storeID)
		if err != nil {
			h.writeDBError(w, err, "取得排班草稿")
			return
		}
		versionID = draft.ID
	}
	assignments, err := h.repo.ListAssignments(r.Context(), versionID)
	if err != nil {
		h.writeDBError(w, err, "查詢指派")
		return
	}
	employees, err := h.repo.ListStoreEmployees(r.Context(), storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢店內員工")
		return
	}
	nameByID := map[string]string{}
	for _, e := range employees {
		nameByID[e.ID] = e.Name
	}

	blocks := coalesceBlocks(assignments)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="schedule.csv"`)
	// BOM 讓 Excel 正確辨識 UTF-8。
	w.Write([]byte("\xEF\xBB\xBF員工,星期,開始,結束,時數\n"))
	for _, b := range blocks {
		name := nameByID[b.EmployeeID]
		if name == "" {
			name = b.EmployeeID
		}
		fmt.Fprintf(w, "%s,%s,%02d:00,%02d:00,%d\n", name, weekdayNames[b.Weekday], b.Start, b.End, b.End-b.Start)
	}
}

// scheduleBlock 是 CSV 用的連續班段:某員工某天 [Start, End)。
type scheduleBlock struct {
	EmployeeID string
	Weekday    int
	Start, End int
}

// coalesceBlocks 把逐小時指派依 (員工,星期) 排序後,把連續小時併成班段。
func coalesceBlocks(assignments []ScheduleAssignment) []scheduleBlock {
	sorted := make([]ScheduleAssignment, len(assignments))
	copy(sorted, assignments)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].EmployeeID != sorted[j].EmployeeID {
			return sorted[i].EmployeeID < sorted[j].EmployeeID
		}
		if sorted[i].Weekday != sorted[j].Weekday {
			return sorted[i].Weekday < sorted[j].Weekday
		}
		return sorted[i].Hour < sorted[j].Hour
	})

	blocks := []scheduleBlock{}
	for _, a := range sorted {
		n := len(blocks)
		if n > 0 && blocks[n-1].EmployeeID == a.EmployeeID && blocks[n-1].Weekday == a.Weekday && blocks[n-1].End == a.Hour {
			blocks[n-1].End = a.Hour + 1 // 接續上一段
			continue
		}
		blocks = append(blocks, scheduleBlock{EmployeeID: a.EmployeeID, Weekday: a.Weekday, Start: a.Hour, End: a.Hour + 1})
	}
	return blocks
}

// getEmployeeAvailability 店長端:看某員工在該店塗的可上格(排班時當底圖,避免排到不可上)。
func (h *Handler) getEmployeeAvailability(w http.ResponseWriter, r *http.Request) {
	storeID := strings.TrimSpace(r.URL.Query().Get("store_id"))
	employeeID := strings.TrimSpace(r.URL.Query().Get("employee_id"))
	if storeID == "" || employeeID == "" {
		httpx.Error(w, http.StatusBadRequest, "需要 store_id 與 employee_id 查詢參數")
		return
	}
	slots, err := h.repo.GetAvailability(r.Context(), employeeID, storeID)
	if err != nil {
		h.writeDBError(w, err, "查詢員工可上時段")
		return
	}
	httpx.JSON(w, http.StatusOK, slots)
}

func (h *Handler) getMySchedule(w http.ResponseWriter, r *http.Request) {
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
	out := MyScheduleContext{
		Store: store, OpenHour: hours.OpenHour, CloseHour: hours.CloseHour,
		Assignments: []HourCell{}, Issues: []HourCell{},
	}
	pub, ok, err := h.repo.LatestPublishedVersion(r.Context(), store.ID)
	if err != nil {
		h.writeDBError(w, err, "查詢已發布班表")
		return
	}
	out.MyStatus = "pending"
	if ok {
		out.Published = true
		out.Locked = pub.Status == "locked"
		out.Deadline = pub.ConfirmDeadline
		if out.MyStatus, err = h.repo.EmployeeConfirmationStatus(r.Context(), pub.ID, emp.ID); err != nil {
			h.writeDBError(w, err, "查詢確認狀態")
			return
		}
		if out.Assignments, err = h.repo.EmployeeCells(r.Context(), pub.ID, emp.ID); err != nil {
			h.writeDBError(w, err, "查詢我的班表")
			return
		}
		if out.Issues, err = h.repo.EmployeeIssueCells(r.Context(), pub.ID, emp.ID); err != nil {
			h.writeDBError(w, err, "查詢我的問題標記")
			return
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// confirmMySchedule v3-B:員工「接受整週班表」→ 把自己對最近發布版的確認設為 confirmed。
func (h *Handler) confirmMySchedule(w http.ResponseWriter, r *http.Request) {
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
	pub, ok, err := h.repo.LatestPublishedVersion(r.Context(), store.ID)
	if err != nil {
		h.writeDBError(w, err, "查詢已發布班表")
		return
	}
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "這間店還沒有發布班表")
		return
	}
	if pub.Status == "locked" {
		httpx.Error(w, http.StatusBadRequest, "班表已鎖定,無法再變更確認")
		return
	}
	if err := h.repo.SetConfirmation(r.Context(), pub.ID, emp.ID, "confirmed", ""); err != nil {
		h.writeDBError(w, err, "確認班表")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) postMyIssue(w http.ResponseWriter, r *http.Request) {
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
		Weekday int    `json:"weekday"`
		Hour    int    `json:"hour"`
		Note    string `json:"note"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "請求格式錯誤: "+err.Error())
		return
	}
	if body.Weekday < 0 || body.Weekday > 6 || body.Hour < 0 || body.Hour > 23 {
		httpx.Error(w, http.StatusBadRequest, "weekday 需 0~6、hour 需 0~23")
		return
	}
	pub, ok, err := h.repo.LatestPublishedVersion(r.Context(), store.ID)
	if err != nil {
		h.writeDBError(w, err, "查詢已發布班表")
		return
	}
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "這間店還沒有發布班表")
		return
	}
	if pub.Status == "locked" {
		httpx.Error(w, http.StatusBadRequest, "班表已鎖定,無法再回報")
		return
	}
	note := strings.TrimSpace(body.Note)
	marked, err := h.repo.MarkIssue(r.Context(), pub.ID, emp.ID, body.Weekday, body.Hour, note)
	if err != nil {
		h.writeDBError(w, err, "標記問題")
		return
	}
	if !marked {
		httpx.Error(w, http.StatusBadRequest, "這格不是你的班,無法標記")
		return
	}
	// v3-B:回報問題 = 回絕,把這位員工對此版的確認設為 declined。
	if err := h.repo.SetConfirmation(r.Context(), pub.ID, emp.ID, "declined", note); err != nil {
		h.writeDBError(w, err, "更新確認狀態")
		return
	}
	w.WriteHeader(http.StatusCreated)
}
