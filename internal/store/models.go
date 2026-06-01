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

// Store 門市 / 分店。
type Store struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
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

// ShiftTemplate 班別模板:7×4 格子的「列」。
// 每店建立時自動帶 4 個預設(早/中/晚/大夜),之後可增刪改。
type ShiftTemplate struct {
	ID                string    `json:"id"`
	StoreID           string    `json:"store_id"`
	Name              string    `json:"name"`
	StartLocal        string    `json:"start_local"` // 店本地牆鐘 "HH:MM:SS"
	EndLocal          string    `json:"end_local"`
	RequiredHeadcount int       `json:"required_headcount"`
	RequiredSkills    []string  `json:"required_skills"`
	CreatedAt         time.Time `json:"created_at"`
}

// IDName 是只回 id + name 的精簡視圖(給員工填班頁用,不外洩多餘欄位)。
type IDName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AvailabilitySlot 員工可上的「循環」時段(weekday × 班別 + 三元偏好)。
// 此版只做每週循環;單週覆寫(specific_date)留待後續。
// PreferenceLevel:0=絕對不行 / 1=可配合 / 2=非常想上。
type AvailabilitySlot struct {
	ShiftTemplateID string `json:"shift_template_id"`
	Weekday         int    `json:"weekday"` // 0=週日 … 6=週六
	PreferenceLevel int    `json:"preference_level"`
}

// AvailabilityContext 員工填班頁需要的一次性資料包:
// 我是誰、填哪間店、有哪些班別、我之前填過什麼。
type AvailabilityContext struct {
	Employee  IDName             `json:"employee"`
	Store     IDName             `json:"store"`
	Templates []ShiftTemplate    `json:"templates"`
	Slots     []AvailabilitySlot `json:"slots"`
}

// AvailabilityCount 是某 (班別 × 星期) 各偏好等級的人數(Step 6 缺口分析用)。
type AvailabilityCount struct {
	ShiftTemplateID string
	Weekday         int
	Want            int // 非常想上(level 2)
	Ok              int // 可配合(level 1)
	No              int // 絕對不行(level 0)
}

// CoverageCell 是一格的缺口結果:需求 vs 可上。
type CoverageCell struct {
	ShiftTemplateID string `json:"shift_template_id"`
	Weekday         int    `json:"weekday"`
	Required        int    `json:"required"`  // 該班別需求人數
	Want            int    `json:"want"`      // 非常想上
	Ok              int    `json:"ok"`        // 可配合
	Available       int    `json:"available"` // 可上 = want + ok
	Gap             int    `json:"gap"`       // required - available(>0 表示缺人)
}

// Coverage 是整間店一週的缺口分析包(7×4 heatmap + 未填名單)。
type Coverage struct {
	StoreID   string          `json:"store_id"`
	Templates []ShiftTemplate `json:"templates"`
	Cells     []CoverageCell  `json:"cells"`
	NotFilled []IDName        `json:"not_filled"` // 發了連結但還沒填的員工
}
