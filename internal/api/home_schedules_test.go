package api

import (
	"testing"
	"time"
)

// 2026-10-07 is a Wednesday (weekday 3).
func wed(h, m int) time.Time { return time.Date(2026, 10, 7, h, m, 0, 0, time.UTC) }

func TestScheduleContains(t *testing.T) {
	weeknights := schedule{ID: "a", Name: "TV", Ranges: []schedRange{{Days: []int{1, 2, 3, 4, 5}, Start: "18:30", End: "22:45"}}}
	overnight := schedule{ID: "b", Name: "Night", Ranges: []schedRange{{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "23:00", End: "07:00"}}}
	weekend := schedule{ID: "c", Name: "Weekend", Ranges: []schedRange{{Days: []int{0, 6}, Start: "00:00", End: "00:00"}}}
	friNight := schedule{ID: "d", Name: "Fri late", Ranges: []schedRange{{Days: []int{5}, Start: "22:00", End: "02:00"}}}
	cases := []struct {
		name string
		sc   schedule
		t    time.Time
		want bool
	}{
		{"weeknight inside", weeknights, wed(19, 0), true},
		{"weeknight start minute", weeknights, wed(18, 30), true},
		{"weeknight end minute is outside", weeknights, wed(22, 45), false},
		{"weeknight before", weeknights, wed(18, 29), false},
		{"weeknight on a Saturday", weeknights, wed(19, 0).AddDate(0, 0, 3), false},
		{"overnight late", overnight, wed(23, 30), true},
		{"overnight early", overnight, wed(6, 59), true},
		{"overnight daytime", overnight, wed(12, 0), false},
		{"whole-day range", weekend, wed(12, 0).AddDate(0, 0, 4), true},
		{"whole-day range other day", weekend, wed(12, 0), false},
		{"past midnight belongs to the start day", friNight, wed(1, 0).AddDate(0, 0, 3), true},
		{"past midnight not after Thursday", friNight, wed(1, 0).AddDate(0, 0, 2), false},
	}
	for _, c := range cases {
		if got := c.sc.contains(c.t); got != c.want {
			t.Errorf("%s: contains(%s) = %v, want %v", c.name, c.t.Format("Mon 15:04"), got, c.want)
		}
	}
}

func TestTaskRules(t *testing.T) {
	tv := schedule{ID: "tv", Name: "TV time", Ranges: []schedRange{{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "18:00", End: "23:00"}}}
	night := schedule{ID: "night", Name: "Overnight", Ranges: []schedRange{{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "01:00", End: "09:00"}}}
	h := homeSchedules{Schedules: []schedule{tv, night}, Tasks: map[string]taskRule{
		taskDownloads:   {Schedule: "tv", Mode: "never", Action: dlPause, When: "finish"},
		taskVideoSearch: {Schedule: "night", Mode: "only"},
		taskSearch:      {Schedule: "gone", Mode: "only"}, // a deleted schedule never holds anything
	}}
	if h.allowedAt(taskDownloads, wed(19, 0)) || !h.allowedAt(taskDownloads, wed(12, 0)) {
		t.Error("downloads should pause only during TV time")
	}
	if !h.allowedAt(taskVideoSearch, wed(3, 0)) || h.allowedAt(taskVideoSearch, wed(19, 0)) {
		t.Error("video searches should run only overnight")
	}
	if !h.allowedAt(taskSearch, wed(19, 0)) || !h.allowedAt(taskPostprocess, wed(19, 0)) {
		t.Error("tasks without a (valid) schedule always run")
	}
	if next := h.nextAllowed(taskVideoSearch, wed(19, 0)); !next.Equal(time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)) {
		t.Errorf("next video search window = %v, want Thu 01:00", next)
	}
	if next := h.nextAllowed(taskDownloads, wed(19, 0)); !next.Equal(wed(23, 0)) {
		t.Errorf("downloads resume at %v, want 23:00", next)
	}

	// Quiet holds everything heavy, but not the light StashDB checks.
	h.QuietUntil = wed(20, 0).Format(time.RFC3339)
	for _, task := range []string{taskSearch, taskDownloads, taskPostprocess, taskMediaRefresh, taskVideoSearch} {
		if h.allowedAt(task, wed(12, 0)) {
			t.Errorf("%s should be held while quiet", task)
		}
	}
	if !h.allowedAt(taskVideoFollow, wed(12, 0)) {
		t.Error("followed checks are not held by quiet")
	}
	if !h.allowedAt(taskSearch, wed(20, 1)) {
		t.Error("quiet should end at its time")
	}
}

func TestScheduleValidation(t *testing.T) {
	good := schedule{Name: "TV", Ranges: []schedRange{{Days: []int{1}, Start: "18:00", End: "22:00"}}}
	if err := validSchedule(good); err != nil {
		t.Fatalf("good schedule refused: %v", err)
	}
	for name, sc := range map[string]schedule{
		"no name":   {Ranges: good.Ranges},
		"no ranges": {Name: "x"},
		"no days":   {Name: "x", Ranges: []schedRange{{Start: "01:00", End: "02:00"}}},
		"bad day":   {Name: "x", Ranges: []schedRange{{Days: []int{7}, Start: "01:00", End: "02:00"}}},
		"bad time":  {Name: "x", Ranges: []schedRange{{Days: []int{1}, Start: "25:00", End: "02:00"}}},
	} {
		if validSchedule(sc) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	h := homeSchedules{Schedules: []schedule{{ID: "s", Name: "S", Ranges: good.Ranges}}}
	for name, r := range map[string]taskRule{
		"unknown schedule": {Schedule: "nope", Mode: "only"},
		"bad mode":         {Schedule: "s", Mode: "sometimes"},
		"limit without MB": {Schedule: "s", Mode: "never", Action: dlLimit},
		"no connections":   {Schedule: "s", Mode: "never", Action: dlConnections},
		"bad when":         {Schedule: "s", Mode: "never", Action: dlPause, When: "later"},
	} {
		if validRule(taskDownloads, r, h) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := validRule(taskDownloads, taskRule{Schedule: "s", Mode: "never", Action: dlLimit, LimitMB: 10}, h); err != nil {
		t.Errorf("limit rule refused: %v", err)
	}
}

func TestClockMinutes(t *testing.T) {
	for v, want := range map[string]int{"01:00": 60, "9:30": 570, "23:59": 1439} {
		if got, ok := clockMinutes(v); !ok || got != want {
			t.Errorf("clockMinutes(%q) = %d, %v", v, got, ok)
		}
	}
	for _, v := range []string{"", "24:00", "ab", "12:60"} {
		if _, ok := clockMinutes(v); ok {
			t.Errorf("clockMinutes(%q) accepted", v)
		}
	}
}
