package watchlog

import (
	"testing"
	"time"
)

// The time watched between two checks: how far it moved when the position is
// known (never more than the time that passed), nothing while paused.
func TestPlayed(t *testing.T) {
	step := 30 * time.Second
	for _, c := range []struct {
		name              string
		wasPaused, paused bool
		from, to, runtime float64
		want              time.Duration
	}{
		{"playing", false, false, 100, 130, 6000, 30 * time.Second},
		{"paused throughout", true, true, 100, 130, 6000, 0},
		{"paused part way", false, true, 100, 110, 6000, 10 * time.Second},
		{"skipped ahead", false, false, 100, 4000, 6000, 30 * time.Second},
		{"went back", false, false, 400, 100, 6000, 0},
		{"no position, playing", false, false, 0, 0, 0, 30 * time.Second},
		{"no position, was paused", true, false, 0, 0, 0, 0},
	} {
		if got := played(step, c.wasPaused, c.paused, c.from, c.to, c.runtime); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// The numbers for a period: hours per day, per person, top titles, how things
// played and why they were transcoded, busiest hour and day.
func TestSum(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 11, 4, 22, 0, 0, 0, loc) // a Wednesday
	at := func(daysAgo, hour int) time.Time { return time.Date(2026, 11, 4-daysAgo, hour, 0, 0, 0, loc) }
	list := []Session{
		{UserName: "Alice", Kind: "movie", Title: "Arrival", TMDBID: 329865, LibraryID: 4, StartedAt: at(0, 20), WatchedSec: 7200, PlayMethod: "DirectPlay", Completed: true, Client: "Jellyfin Web", Device: "Firefox"},
		{UserName: "Bob", Kind: "movie", Title: "Arrival", TMDBID: 329865, StartedAt: at(1, 21), WatchedSec: 3600, PlayMethod: "Transcode", TranscodeReasons: []string{"Audio codec not supported"}, Device: "Phone"},
		{UserName: "Bob", Kind: "episode", Series: "Severance", Title: "Pilot", Season: 1, Episode: 1, TMDBID: 95396, StartedAt: at(1, 21), WatchedSec: 1800, PlayMethod: "DirectStream"},
		{UserName: "Bob", Kind: "episode", Series: "Severance", Title: "Pilot", Season: 1, Episode: 1, TMDBID: 95396, StartedAt: at(2, 9), WatchedSec: 1800, PlayMethod: "DirectPlay"},
	}
	st := Sum(list, 7, now, loc)
	if st.Plays != 4 || st.Hours != 4 || st.People != 2 || st.Titles != 2 || st.Finished != 1 {
		t.Fatalf("totals: %+v", st)
	}
	if len(st.PerDay) != 7 || st.PerDay[6].Day != "2026-11-04" || st.PerDay[6].Hours != 2 || st.PerDay[5].Hours != 1.5 {
		t.Fatalf("per day: %+v", st.PerDay)
	}
	if st.Users[0].Name != "Bob" || st.Users[0].Hours != 2 || st.Users[0].Plays != 3 {
		t.Fatalf("users: %+v", st.Users)
	}
	if len(st.Movies) != 1 || st.Movies[0].Plays != 2 || st.Movies[0].LibraryID != 4 {
		t.Fatalf("movies: %+v", st.Movies)
	}
	if len(st.Shows) != 1 || st.Shows[0].Episodes != 1 || st.Shows[0].Plays != 2 {
		t.Fatalf("shows: %+v", st.Shows)
	}
	if st.Methods != (Methods{Direct: 2, Stream: 1, Transcode: 1}) || len(st.Reasons) != 1 || st.Reasons[0].Name != "Audio codec not supported" {
		t.Fatalf("methods: %+v %+v", st.Methods, st.Reasons)
	}
	if st.HourOfDay[21] != 1.5 || st.HourOfDay[20] != 2 || st.DayOfWeek[2] != 2 || st.DayOfWeek[1] != 1.5 {
		t.Fatalf("busiest: %v %v", st.HourOfDay, st.DayOfWeek)
	}
}
