package store

import "fmt"

// Rule Engine(design §3.4 / 決策 4):純函式、不碰 DB,方便 table-driven 測試。
// handler 把 DB 資料整理成下列輸入,呼叫 ValidateSchedule 得到報告。
//
// v2 啟用的檢查(逐小時模型):
//   1. 不可用時段(硬,擋):指派到員工沒塗「可上」的格子。
//   2. 跨店同時段雙排(硬,擋):同員工同 (weekday,hour) 已在別店「已發布」版本被排。
//   3. 人數未滿(缺口,黃):某 (weekday,hour) 已排人數 < 需求人數。
//   4. 超週工時上限(軟,黃):員工本版總時數 > 其 max_weekly_hours。
// 技能未滿足(決策 4 第 4 項)在 v1.5 逐小時需求模型下無技能維度資料,**延後**到需求帶技能時再做。

// slotKey 把 (weekday, hour) 壓成單一整數鍵(hour 0~23,故 *100 不會碰撞)。
func slotKey(weekday, hour int) int { return weekday*100 + hour }

// ScheduleInput 是 Rule Engine 的輸入(全部已是記憶體資料)。
type ScheduleInput struct {
	Assignments    []ScheduleAssignment
	Employees      []ScheduleEmployee
	Availability   map[string]map[int]bool // empID -> slotKey -> 是否可上(preference>=1)
	Requirements   map[int]int             // slotKey -> 需求人數
	CrossStoreBusy map[string]map[int]bool // empID -> slotKey -> 是否已在別店已發布版本被排
}

// ValidateSchedule 跑所有檢查,回傳結構化報告(硬/軟/缺口 + 可否發布)。
func ValidateSchedule(in ScheduleInput) ValidationReport {
	rep := ValidationReport{Hard: []Violation{}, Soft: []Violation{}, Understaffed: []UnderStaffed{}}

	nameByID := make(map[string]string, len(in.Employees))
	maxByID := make(map[string]int, len(in.Employees))
	for _, e := range in.Employees {
		nameByID[e.ID] = e.Name
		maxByID[e.ID] = e.MaxWeeklyHours
	}

	assignedCount := make(map[int]int)        // slotKey -> 已排人數
	hoursByEmp := make(map[string]int)        // empID -> 本版總時數
	for _, a := range in.Assignments {
		key := slotKey(a.Weekday, a.Hour)
		assignedCount[key]++
		hoursByEmp[a.EmployeeID]++

		// 1. 不可用時段(硬)
		if av := in.Availability[a.EmployeeID]; av == nil || !av[key] {
			rep.Hard = append(rep.Hard, Violation{
				Kind: "unavailable", Severity: "hard",
				EmployeeID: a.EmployeeID, EmployeeName: nameByID[a.EmployeeID],
				Weekday: a.Weekday, Hour: a.Hour,
				Message: fmt.Sprintf("%s 沒把這格標為可上", nameByID[a.EmployeeID]),
			})
		}
		// 2. 跨店同時段雙排(硬)
		if busy := in.CrossStoreBusy[a.EmployeeID]; busy != nil && busy[key] {
			rep.Hard = append(rep.Hard, Violation{
				Kind: "double_booked", Severity: "hard",
				EmployeeID: a.EmployeeID, EmployeeName: nameByID[a.EmployeeID],
				Weekday: a.Weekday, Hour: a.Hour,
				Message: fmt.Sprintf("%s 同一時段已在別店被排班", nameByID[a.EmployeeID]),
			})
		}
	}

	// 3. 人數未滿(缺口,黃)
	for key, need := range in.Requirements {
		if got := assignedCount[key]; got < need {
			rep.Understaffed = append(rep.Understaffed, UnderStaffed{
				Weekday: key / 100, Hour: key % 100, Required: need, Assigned: got,
			})
		}
	}

	// 4. 超週工時上限(軟,黃)
	for _, e := range in.Employees {
		if h := hoursByEmp[e.ID]; e.MaxWeeklyHours > 0 && h > e.MaxWeeklyHours {
			rep.Soft = append(rep.Soft, Violation{
				Kind: "overtime", Severity: "soft",
				EmployeeID: e.ID, EmployeeName: e.Name,
				Weekday: -1, Hour: -1,
				Message: fmt.Sprintf("%s 本週排 %d 小時,超過上限 %d", e.Name, h, e.MaxWeeklyHours),
			})
		}
	}

	rep.Publishable = len(rep.Hard) == 0
	return rep
}
