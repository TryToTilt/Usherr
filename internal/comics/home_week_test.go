package comics

import (
	"testing"
	"time"
)

func TestWeekOf(t *testing.T) {
	for day, want := range map[string][2]string{
		"2026-10-14": {"2026-10-12", "2026-10-18"}, // a Wednesday
		"2026-10-12": {"2026-10-12", "2026-10-18"}, // Monday
		"2026-10-18": {"2026-10-12", "2026-10-18"}, // Sunday
		"2026-01-01": {"2025-12-29", "2026-01-04"},
	} {
		d, _ := time.Parse("2006-01-02", day)
		if f, to := WeekOf(d); f != want[0] || to != want[1] {
			t.Errorf("%s: %s..%s", day, f, to)
		}
	}
}
