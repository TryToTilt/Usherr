package api

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

// A season that keeps finding no pack is given up on after a few searches,
// until an episode is added to it or two months have passed.
func TestPlanSeasonPackGivesUpOnASeasonWithNoPack(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	eps := []library.Episode{
		{Season: 1, Episode: 1, AirDate: "2020-01-01", Status: library.StatusMissing, Monitored: true},
		{Season: 1, Episode: 2, AirDate: "2020-01-08", Status: library.StatusDownloaded, Quality: "WEBDL-1080p", Monitored: true},
		{Season: 1, Episode: 3, AirDate: "2020-01-15", Status: library.StatusDownloaded, Quality: "WEBDL-1080p", Monitored: true},
	}
	ago := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	fourth := append(append([]library.Episode(nil), eps...), library.Episode{Season: 1, Episode: 4, AirDate: "2020-01-22", Status: library.StatusMissing, Monitored: true})
	tests := []struct {
		name string
		eps  []library.Episode
		last packHistory
		want bool
	}{
		{"never searched", eps, packHistory{}, true},
		{"searched yesterday", eps, packHistory{at: ago(1), tries: 1, episodes: 3}, false},
		{"searched a week ago, once", eps, packHistory{at: ago(7), tries: 1, episodes: 3}, true},
		{"five searches, a week ago", eps, packHistory{at: ago(7), tries: 5, episodes: 3}, true},
		{"six searches, a week ago: given up", eps, packHistory{at: ago(7), tries: 6, episodes: 3}, false},
		{"six searches, 59 days ago: still given up", eps, packHistory{at: ago(59), tries: 6, episodes: 3}, false},
		{"six searches, 60 days ago: try again", eps, packHistory{at: ago(60), tries: 6, episodes: 3}, true},
		{"six searches, a new episode was added", fourth, packHistory{at: ago(7), tries: 6, episodes: 3}, true},
		{"six searches, nothing added since", eps, packHistory{at: ago(30), tries: 9, episodes: 3}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got := planSeasonPack(quality.Profile{}, library.Series{}, tc.eps, "Ended", tc.last, now)
			if got != tc.want {
				t.Fatalf("planSeasonPack = %v, want %v", got, tc.want)
			}
		})
	}
}
