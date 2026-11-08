package api

import (
	"testing"
	"time"
)

// Badges from stubs: counts, the night owl, a double feature, House lights
// and Perfect attendance (a month that's over, three parties or more, a stub
// from each).
func TestBadgeEarning(t *testing.T) {
	loc := time.Local
	day := func(m time.Month, d, h, min int) int64 { return time.Date(2026, m, d, h, min, 0, 0, loc).UnixMilli() }
	now := time.Date(2026, time.November, 7, 12, 0, 0, 0, loc)
	var stubs []badgeStub
	add := func(id int64, tmdb int, at int64, runtime int, hosted bool) {
		stubs = append(stubs, badgeStub{Showtime: id, TMDB: tmdb, At: at, Runtime: runtime, Hosted: hosted})
	}
	// September: three parties, all attended.
	add(1, 10, day(time.September, 4, 20, 0), 100, true)
	add(2, 11, day(time.September, 11, 20, 0), 100, true)
	add(3, 12, day(time.September, 18, 20, 0), 100, false)
	// October: a double feature, the second ending at 2:10 AM; one party missed.
	add(4, 13, day(time.October, 2, 21, 0), 120, true)
	add(5, 14, day(time.October, 2, 23, 50), 140, true)
	invites := map[int64]int64{1: day(time.September, 4, 20, 0), 2: day(time.September, 11, 20, 0), 3: day(time.September, 18, 20, 0),
		4: day(time.October, 2, 21, 0), 5: day(time.October, 2, 23, 50), 99: day(time.October, 9, 20, 0)}
	got := map[string]badgeOut{}
	for _, b := range earnBadges(stubs, invites, now) {
		got[b.ID] = b
	}
	want := map[string]int64{
		"first": day(time.September, 4, 20, 0), "five": day(time.October, 2, 23, 50), "regular": 0, "season": 0,
		"perfect": day(time.September, 18, 20, 0), "nightowl": day(time.October, 3, 2, 10), "host": 0, "double": day(time.October, 2, 23, 50),
	}
	for id, at := range want {
		if got[id].Earned != at {
			t.Errorf("%s: earned %v, want %v (%+v)", id, time.UnixMilli(got[id].Earned), time.UnixMilli(at), got[id])
		}
	}
	if got["host"].Have != 4 || got["regular"].Have != 5 || got["regular"].Need != 10 {
		t.Errorf("progress: %+v %+v", got["host"], got["regular"])
	}

	// A month still going doesn't count yet; nor one with a party missed.
	got = map[string]badgeOut{}
	for _, b := range earnBadges(stubs[3:], map[int64]int64{4: invites[4], 5: invites[5], 99: invites[99]}, now) {
		got[b.ID] = b
	}
	if got["perfect"].Earned != 0 || got["first"].Earned != day(time.October, 2, 21, 0) {
		t.Errorf("perfect without a whole month: %+v", got["perfect"])
	}
	if got := earnBadges(nil, nil, now); len(got) != len(badgeOrder) || got[0].Earned != 0 {
		t.Errorf("nothing: %+v", got)
	}
}
