package store

import "testing"

// 預排器是純函式,這裡測幾個關鍵性質:零硬衝突、偏好優先、尊重週上限、瓶頸優先。

func avPref(m map[string]map[int]int) map[string]map[int]int { return m }

func countFor(asgs []ScheduleAssignment, emp string) int {
	n := 0
	for _, a := range asgs {
		if a.EmployeeID == emp {
			n++
		}
	}
	return n
}
func hasAssign(asgs []ScheduleAssignment, emp string, wd, hr int) bool {
	for _, a := range asgs {
		if a.EmployeeID == emp && a.Weekday == wd && a.Hour == hr {
			return true
		}
	}
	return false
}

func TestSuggestSchedule_OnlyAvailable(t *testing.T) {
	// 需求 1 人,只有 emp1 標可上 → 只能排 emp1,且不排 emp2(沒標可上)。
	in := AutofillInput{
		Requirements: []Requirement{{Weekday: 1, Hour: 12, Headcount: 1}},
		Employees:    []ScheduleEmployee{{ID: "emp1", MaxWeeklyHours: 40}, {ID: "emp2", MaxWeeklyHours: 40}},
		Availability: avPref(map[string]map[int]int{"emp1": {slotKey(1, 12): 1}}),
	}
	got := SuggestSchedule(in)
	if len(got) != 1 || !hasAssign(got, "emp1", 1, 12) {
		t.Fatalf("應只排 emp1 於 (1,12),得到 %+v", got)
	}
}

func TestSuggestSchedule_PrefersWant(t *testing.T) {
	// 需求 1 人,emp1=可配合(1)、emp2=非常想上(2) → 應選 emp2。
	in := AutofillInput{
		Requirements: []Requirement{{Weekday: 2, Hour: 9, Headcount: 1}},
		Employees:    []ScheduleEmployee{{ID: "emp1", MaxWeeklyHours: 40}, {ID: "emp2", MaxWeeklyHours: 40}},
		Availability: avPref(map[string]map[int]int{
			"emp1": {slotKey(2, 9): 1},
			"emp2": {slotKey(2, 9): 2},
		}),
	}
	got := SuggestSchedule(in)
	if !hasAssign(got, "emp2", 2, 9) || hasAssign(got, "emp1", 2, 9) {
		t.Fatalf("應選非常想上的 emp2,得到 %+v", got)
	}
}

func TestSuggestSchedule_RespectsMaxHours(t *testing.T) {
	// emp1 上限 1 小時、兩個需求格都只有他可上 → 只排到 1 格,另一格留缺口。
	in := AutofillInput{
		Requirements: []Requirement{{Weekday: 1, Hour: 9, Headcount: 1}, {Weekday: 1, Hour: 10, Headcount: 1}},
		Employees:    []ScheduleEmployee{{ID: "emp1", MaxWeeklyHours: 1}},
		Availability: avPref(map[string]map[int]int{"emp1": {slotKey(1, 9): 2, slotKey(1, 10): 2}}),
	}
	got := SuggestSchedule(in)
	if countFor(got, "emp1") != 1 {
		t.Fatalf("emp1 上限 1,應只排 1 格,得到 %d (%+v)", countFor(got, "emp1"), got)
	}
}

func TestSuggestSchedule_NoCrossStoreDoubleBook(t *testing.T) {
	// emp1 在 (3,15) 已於別店被排 → 不應再排到這格。
	in := AutofillInput{
		Requirements:   []Requirement{{Weekday: 3, Hour: 15, Headcount: 1}},
		Employees:      []ScheduleEmployee{{ID: "emp1", MaxWeeklyHours: 40}},
		Availability:   avPref(map[string]map[int]int{"emp1": {slotKey(3, 15): 2}}),
		CrossStoreBusy: map[string]map[int]bool{"emp1": {slotKey(3, 15): true}},
	}
	got := SuggestSchedule(in)
	if len(got) != 0 {
		t.Fatalf("跨店已占用,不應排,得到 %+v", got)
	}
}

func TestSuggestSchedule_BottleneckFirst(t *testing.T) {
	// emp1 兩格都可上、emp2 只有 (1,9) 可上;(1,9)、(1,10) 各需 1 人。
	// 瓶頸格 (1,10) 只有 emp1 一個候選 → 先排,(1,9) 再用 emp2 → 兩格都填滿。
	in := AutofillInput{
		Requirements: []Requirement{{Weekday: 1, Hour: 9, Headcount: 1}, {Weekday: 1, Hour: 10, Headcount: 1}},
		Employees:    []ScheduleEmployee{{ID: "emp1", MaxWeeklyHours: 40}, {ID: "emp2", MaxWeeklyHours: 40}},
		Availability: avPref(map[string]map[int]int{
			"emp1": {slotKey(1, 9): 2, slotKey(1, 10): 2},
			"emp2": {slotKey(1, 9): 2},
		}),
	}
	got := SuggestSchedule(in)
	if len(got) != 2 || !hasAssign(got, "emp1", 1, 10) || !hasAssign(got, "emp2", 1, 9) {
		t.Fatalf("瓶頸優先應兩格填滿(emp1→(1,10), emp2→(1,9)),得到 %+v", got)
	}
}
