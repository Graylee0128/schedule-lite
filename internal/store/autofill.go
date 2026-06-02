package store

import "sort"

// v3 階段 A:一鍵建議班表(預排推薦)。純函式、不碰 DB,方便 table-driven 測試。
// 演算法 = 貪婪 + 計分(design §3.4:自己寫 Rule Engine + scoring,先不用 OR-Tools):
//
//  1. 對每個有需求的 (weekday, hour) 格,算出「候選人」= 在這格標可上(preference≥1)、
//     且沒在別店同時段被排的店內成員。**只用候選人 → 產出的草稿保證零硬衝突**(不會排到不可上)。
//  2. 瓶頸優先:候選人愈少的格愈先排(難填的先搶人),把稀缺供給用在刀口上。
//  3. 每格挑分數最高的前 N 人:偏好高(非常想上>可配合)> 本週已排時數少(公平)> id 穩定。
//     超過週工時上限(max_weekly_hours)的人不排,避免製造超時軟警告。
//  4. 排不滿就留缺口(黃標,老闆自己補)。
//
// 產出交給 handler 寫進 draft,老闆在排班頁微調後發布 —— 把「空白格自己拖」變成「審閱+微調」。

// AutofillInput 是預排的輸入(已是記憶體資料)。
type AutofillInput struct {
	Requirements   []Requirement
	Employees      []ScheduleEmployee
	Availability   map[string]map[int]int  // empID -> slotKey -> preference(1/2)
	CrossStoreBusy map[string]map[int]bool // empID -> slotKey -> 是否已在別店已發布版本被排
}

// SuggestSchedule 跑貪婪預排,回傳建議指派(保證無硬衝突)。
func SuggestSchedule(in AutofillInput) []ScheduleAssignment {
	maxByID := make(map[string]int, len(in.Employees))
	for _, e := range in.Employees {
		maxByID[e.ID] = e.MaxWeeklyHours
	}

	// 每個有需求的格,算候選人。
	type slot struct {
		wd, hr, need int
		cands        []string
	}
	slots := make([]slot, 0, len(in.Requirements))
	for _, r := range in.Requirements {
		if r.Headcount <= 0 {
			continue
		}
		key := slotKey(r.Weekday, r.Hour)
		cands := []string{}
		for _, e := range in.Employees {
			av := in.Availability[e.ID]
			if av == nil || av[key] < 1 { // 沒標可上 → 不列入(保證零硬衝突)
				continue
			}
			if b := in.CrossStoreBusy[e.ID]; b != nil && b[key] { // 別店同時段已排 → 跳過
				continue
			}
			cands = append(cands, e.ID)
		}
		slots = append(slots, slot{r.Weekday, r.Hour, r.Headcount, cands})
	}

	// 瓶頸優先:候選人少的先排;tie → 需求多的先;再 tie → (wd,hr) 穩定。
	sort.Slice(slots, func(i, j int) bool {
		if len(slots[i].cands) != len(slots[j].cands) {
			return len(slots[i].cands) < len(slots[j].cands)
		}
		if slots[i].need != slots[j].need {
			return slots[i].need > slots[j].need
		}
		if slots[i].wd != slots[j].wd {
			return slots[i].wd < slots[j].wd
		}
		return slots[i].hr < slots[j].hr
	})

	hours := make(map[string]int) // 本版已排時數(動態)
	out := []ScheduleAssignment{}
	for _, s := range slots {
		key := slotKey(s.wd, s.hr)
		cands := append([]string(nil), s.cands...)
		// 計分排序:偏好高優先、已排時數少優先、id 穩定(可重現)。
		sort.Slice(cands, func(i, j int) bool {
			pi, pj := in.Availability[cands[i]][key], in.Availability[cands[j]][key]
			if pi != pj {
				return pi > pj
			}
			hi, hj := hours[cands[i]], hours[cands[j]]
			if hi != hj {
				return hi < hj
			}
			return cands[i] < cands[j]
		})
		filled := 0
		for _, emp := range cands {
			if filled >= s.need {
				break
			}
			if max := maxByID[emp]; max > 0 && hours[emp] >= max { // 超過週上限不排
				continue
			}
			out = append(out, ScheduleAssignment{EmployeeID: emp, Weekday: s.wd, Hour: s.hr})
			hours[emp]++
			filled++
		}
	}
	return out
}
