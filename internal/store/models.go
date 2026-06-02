// Package store 管理組織 / 門市 / 員工(門市與人員的註冊資料)。
// 這是第一個有業務邏輯的 domain:對應 plan.md Step 3「建店 / 建員工」。
package store

import "time"

// Organization 組織 / 品牌:多租戶的根。
type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Timezone  string    `json:"timezone"`
	WeekStart int       `json:"week_start"`
	CreatedAt time.Time `json:"created_at"`
}

// Store 門市 / 分店。OpenHour/CloseHour 是營業時段(單一窗,套用 7 天),
// 決定填班 / 需求網格只顯示 [OpenHour, CloseHour) 內的小時(v1.5 階段 B)。
type Store struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	OpenHour       int       `json:"open_hour"`
	CloseHour      int       `json:"close_hour"`
	CreatedAt      time.Time `json:"created_at"`
}

// Employee 員工(綁在 org,跨店歸屬另由 membership 處理)。
type Employee struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	Phone          *string   `json:"phone,omitempty"` // 可為 NULL
	CreatedAt      time.Time `json:"created_at"`
}

// IDName 是只回 id + name 的精簡視圖(給員工填班頁用,不外洩多餘欄位)。
type IDName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// HourSlot 員工可上的「循環」逐小時時段(weekday × hour + 正向偏好)。
// v1.5 階段 B:只存「能上」的小時,未塗 = 絕對不行(不落 DB)。
// PreferenceLevel:1=可配合 / 2=非常想上。
type HourSlot struct {
	Weekday         int `json:"weekday"` // 0=週日 … 6=週六
	Hour            int `json:"hour"`    // 0~23(店本地牆鐘)
	PreferenceLevel int `json:"preference_level"`
}

// Requirement 某店某 (weekday × hour) 的需求人數(逐小時需求,取代固定 4 班)。
type Requirement struct {
	Weekday   int `json:"weekday"`
	Hour      int `json:"hour"`
	Headcount int `json:"headcount"`
}

// StoreHours 是門市營業時段([OpenHour, CloseHour))。
type StoreHours struct {
	OpenHour  int `json:"open_hour"`
	CloseHour int `json:"close_hour"`
}

// MeContext 員工開連結後的初始資料:我是誰 + 我能填哪些門市(membership)。
// 員工選定門市後,再用 AvailabilityContext 取該店的營業時段與已填時段。
type MeContext struct {
	Employee IDName  `json:"employee"`
	Stores   []Store `json:"stores"`
}

// AvailabilityContext 員工填班頁需要的一次性資料包:
// 我是誰、填哪間店、營業時段(網格列範圍)、我之前塗過哪些小時。
type AvailabilityContext struct {
	Employee  IDName     `json:"employee"`
	Store     IDName     `json:"store"`
	OpenHour  int        `json:"open_hour"`
	CloseHour int        `json:"close_hour"`
	Slots     []HourSlot `json:"slots"`
}

// HourCount 是某 (weekday × hour) 各偏好等級的可上人數(逐小時缺口分析用)。
type HourCount struct {
	Weekday int
	Hour    int
	Want    int // 非常想上(level 2)
	Ok      int // 可配合(level 1)
}

// CoverageCell 是一格(weekday × hour)的缺口結果:需求 vs 可上。
type CoverageCell struct {
	Weekday   int `json:"weekday"`
	Hour      int `json:"hour"`
	Required  int `json:"required"`  // 該時段需求人數
	Want      int `json:"want"`      // 非常想上
	Ok        int `json:"ok"`        // 可配合
	Available int `json:"available"` // 可上 = want + ok
	Gap       int `json:"gap"`       // required - available(>0 表示缺人)
}

// Coverage 是整間店一週的逐小時缺口分析包(小時 × 星期 heatmap + 未填名單)。
type Coverage struct {
	StoreID   string         `json:"store_id"`
	OpenHour  int            `json:"open_hour"`
	CloseHour int            `json:"close_hour"`
	Cells     []CoverageCell `json:"cells"`
	NotFilled []IDName       `json:"not_filled"` // 發了連結但還沒提交的員工
}

// --- v2:排班 / Rule Engine / 發布 ---

// ScheduleVersion 一張範本週班表的版本(draft 編輯中 / published 已凍結)。
type ScheduleVersion struct {
	ID          string     `json:"id"`
	StoreID     string     `json:"store_id"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// ScheduleAssignment 逐小時指派:某員工被排在 (weekday, hour) 這一格。
type ScheduleAssignment struct {
	EmployeeID string `json:"employee_id"`
	Weekday    int    `json:"weekday"`
	Hour       int    `json:"hour"`
}

// AvailabilityRow 是某店一筆員工可上格(含偏好);供 Rule Engine 與預排取用,不外露 JSON。
type AvailabilityRow struct {
	EmployeeID      string
	Weekday         int
	Hour            int
	PreferenceLevel int
}

// ScheduleEmployee 排班用的員工視圖(含週工時上限,給 Rule Engine 算超時)。
type ScheduleEmployee struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	MaxWeeklyHours int    `json:"max_weekly_hours"`
}

// Violation 一筆規則違反(硬=擋發布、軟=黃標不擋)。
type Violation struct {
	Kind         string `json:"kind"`     // unavailable / double_booked / overtime
	Severity     string `json:"severity"` // hard / soft
	EmployeeID   string `json:"employee_id"`
	EmployeeName string `json:"employee_name"`
	Weekday      int    `json:"weekday"`
	Hour         int    `json:"hour"` // overtime 類為 -1(非單一格)
	Message      string `json:"message"`
}

// UnderStaffed 一個 (weekday, hour) 缺口:已排 < 需求(黃標,不擋)。
type UnderStaffed struct {
	Weekday  int `json:"weekday"`
	Hour     int `json:"hour"`
	Required int `json:"required"`
	Assigned int `json:"assigned"`
}

// ValidationReport Rule Engine 的輸出。Publishable=無硬違反才能發布。
type ValidationReport struct {
	Hard         []Violation    `json:"hard"`
	Soft         []Violation    `json:"soft"`
	Understaffed []UnderStaffed `json:"understaffed"`
	Publishable  bool           `json:"publishable"`
}

// ScheduleIssue 員工在已發布班表上標記的問題格。
type ScheduleIssue struct {
	EmployeeID   string `json:"employee_id"`
	EmployeeName string `json:"employee_name"`
	Weekday      int    `json:"weekday"`
	Hour         int    `json:"hour"`
	Note         string `json:"note"`
}

// ScheduleContext 老闆排班頁的一次性資料包。
type ScheduleContext struct {
	Version      ScheduleVersion      `json:"version"`
	OpenHour     int                  `json:"open_hour"`
	CloseHour    int                  `json:"close_hour"`
	Employees    []ScheduleEmployee   `json:"employees"`
	Requirements []Requirement        `json:"requirements"`
	Assignments  []ScheduleAssignment `json:"assignments"`
	Validation   ValidationReport     `json:"validation"`
	Issues       []ScheduleIssue      `json:"issues"` // 最近一版 published 的員工標記
}

// HourCell 是 (weekday, hour) 的精簡格(員工看自己班表 / 標問題用)。
type HourCell struct {
	Weekday int    `json:"weekday"`
	Hour    int    `json:"hour"`
	Note    string `json:"note,omitempty"`
}

// MyScheduleContext 員工看自己在某店「已發布」班表 + 自己標的問題。
type MyScheduleContext struct {
	Store       IDName     `json:"store"`
	OpenHour    int        `json:"open_hour"`
	CloseHour   int        `json:"close_hour"`
	Published   bool       `json:"published"` // 該店是否已有發布版本
	Assignments []HourCell `json:"assignments"`
	Issues      []HourCell `json:"issues"`
}
