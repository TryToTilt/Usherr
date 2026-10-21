package api

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

func TestSeasonEnded(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	eps := func(dates ...string) []library.Episode {
		var out []library.Episode
		for i, d := range dates {
			out = append(out, library.Episode{Season: 1, Episode: i + 1, AirDate: d})
		}
		return out
	}
	tests := []struct {
		name   string
		eps    []library.Episode
		status string
		want   bool
	}{
		{"every episode aired long ago", eps("2026-09-01", "2026-09-08"), "Returning Series", true},
		{"finale three days ago", eps("2026-10-01", "2026-10-15"), "Returning Series", true},
		{"finale two days ago", eps("2026-10-01", "2026-10-16"), "Returning Series", true},
		{"finale yesterday", eps("2026-10-01", "2026-10-17"), "Returning Series", false},
		{"an episode still to air", eps("2026-10-01", "2026-11-01"), "Returning Series", false},
		{"an episode with no date, show running", eps("2026-09-01", ""), "Returning Series", false},
		{"an episode with no date, show ended", eps("2026-09-01", ""), "Ended", true},
		{"an episode with no date, show cancelled", eps("2026-09-01", ""), "Canceled", true},
		{"no date at all, show ended", eps("", ""), "Ended", false},
		{"one episode is not a pack", eps("2026-09-01"), "Ended", false},
		{"no episodes", nil, "Ended", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := seasonEnded(tc.eps, tc.status, now); got != tc.want {
				t.Fatalf("seasonEnded = %v, want %v", got, tc.want)
			}
		})
	}
}
