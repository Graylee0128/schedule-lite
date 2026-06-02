package store

import "testing"

// Rule Engine 是純函式,這裡用 table-driven 測四種檢查(design 決策 4)。
// 一名員工 emp1(週上限 40),除非個案另設。

func avail(keys ...int) map[string]map[int]bool {
	m := map[string]map[int]bool{"emp1": {}}
	for _, k := range keys {
		m["emp1"][k] = true
	}
	return m
}

func TestValidateSchedule(t *testing.T) {
	emp := []ScheduleEmployee{{ID: "emp1", Name: "小明", MaxWeeklyHours: 40}}

	tests := []struct {
		name           string
		in             ScheduleInput
		wantHard       int
		wantSoft       int
		wantUnder      int
		wantPublish    bool
	}{
		{
			name: "合法:可上 + 滿足需求",
			in: ScheduleInput{
				Assignments:  []ScheduleAssignment{{EmployeeID: "emp1", Weekday: 1, Hour: 12}},
				Employees:    emp,
				Availability: avail(slotKey(1, 12)),
				Requirements: map[int]int{slotKey(1, 12): 1},
			},
			wantHard: 0, wantSoft: 0, wantUnder: 0, wantPublish: true,
		},
		{
			name: "硬:指派到不可上時段",
			in: ScheduleInput{
				Assignments:  []ScheduleAssignment{{EmployeeID: "emp1", Weekday: 1, Hour: 12}},
				Employees:    emp,
				Availability: avail(), // 沒塗任何可上
				Requirements: map[int]int{slotKey(1, 12): 1},
			},
			wantHard: 1, wantSoft: 0, wantUnder: 0, wantPublish: false,
		},
		{
			name: "硬:跨店同時段雙排",
			in: ScheduleInput{
				Assignments:    []ScheduleAssignment{{EmployeeID: "emp1", Weekday: 2, Hour: 9}},
				Employees:      emp,
				Availability:   avail(slotKey(2, 9)),
				Requirements:   map[int]int{slotKey(2, 9): 1},
				CrossStoreBusy: map[string]map[int]bool{"emp1": {slotKey(2, 9): true}},
			},
			wantHard: 1, wantSoft: 0, wantUnder: 0, wantPublish: false,
		},
		{
			name: "缺口:需求 2 只排 1",
			in: ScheduleInput{
				Assignments:  []ScheduleAssignment{{EmployeeID: "emp1", Weekday: 3, Hour: 18}},
				Employees:    emp,
				Availability: avail(slotKey(3, 18)),
				Requirements: map[int]int{slotKey(3, 18): 2},
			},
			wantHard: 0, wantSoft: 0, wantUnder: 1, wantPublish: true, // 缺口不擋發布
		},
		{
			name: "軟:超週工時(上限 2,排 3 格)",
			in: ScheduleInput{
				Assignments: []ScheduleAssignment{
					{EmployeeID: "emp1", Weekday: 1, Hour: 9},
					{EmployeeID: "emp1", Weekday: 1, Hour: 10},
					{EmployeeID: "emp1", Weekday: 1, Hour: 11},
				},
				Employees:    []ScheduleEmployee{{ID: "emp1", Name: "小明", MaxWeeklyHours: 2}},
				Availability: avail(slotKey(1, 9), slotKey(1, 10), slotKey(1, 11)),
				Requirements: map[int]int{},
			},
			wantHard: 0, wantSoft: 1, wantUnder: 0, wantPublish: true, // 超時不擋
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateSchedule(tt.in)
			if len(got.Hard) != tt.wantHard {
				t.Errorf("Hard = %d, want %d (%+v)", len(got.Hard), tt.wantHard, got.Hard)
			}
			if len(got.Soft) != tt.wantSoft {
				t.Errorf("Soft = %d, want %d (%+v)", len(got.Soft), tt.wantSoft, got.Soft)
			}
			if len(got.Understaffed) != tt.wantUnder {
				t.Errorf("Understaffed = %d, want %d (%+v)", len(got.Understaffed), tt.wantUnder, got.Understaffed)
			}
			if got.Publishable != tt.wantPublish {
				t.Errorf("Publishable = %v, want %v", got.Publishable, tt.wantPublish)
			}
		})
	}
}
