package indexers_test

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
)

func TestRSSBudget(t *testing.T) {
	tests := []struct{ limit, want int }{
		{0, 0}, {-3, 0}, {1, 0}, {2, 1}, {3, 1}, {4, 1}, {5, 2}, {10, 3}, {25, 8}, {100, 33}, {1000, 333},
	}
	for _, tc := range tests {
		if got := indexers.RSSBudget(tc.limit); got != tc.want {
			t.Errorf("RSSBudget(%d) = %d, want %d", tc.limit, got, tc.want)
		}
	}
}

func TestRSSAdvice(t *testing.T) {
	const id = 7
	tests := []struct {
		name      string
		setLimit  int
		calls     int
		steps     func(c *fakeClock, tr *indexers.Tracker) // what happened before the question
		wantLimit bool
		wantAllow bool
		wantEvery time.Duration
	}{
		{name: "no limit is left alone", wantAllow: true, calls: 1},
		{name: "limit 25: first run goes, every 3 hours", setLimit: 25, calls: 1, wantLimit: true, wantAllow: true, wantEvery: 3 * time.Hour},
		{name: "limit 25 with movies and TV: 4 runs a day", setLimit: 25, calls: 2, wantLimit: true, wantAllow: true, wantEvery: 6 * time.Hour},
		{name: "too soon after the last run", setLimit: 25, calls: 1, wantLimit: true, wantEvery: 3 * time.Hour,
			steps: func(c *fakeClock, tr *indexers.Tracker) {
				tr.NoteRSS(id, 1)
				c.t = c.t.Add(2*time.Hour + 59*time.Minute)
			}},
		{name: "after the gap it goes again", setLimit: 25, calls: 1, wantLimit: true, wantAllow: true, wantEvery: 3 * time.Hour,
			steps: func(c *fakeClock, tr *indexers.Tracker) { tr.NoteRSS(id, 1); c.t = c.t.Add(3 * time.Hour) }},
		{name: "the day's RSS budget is spent", setLimit: 25, calls: 1, wantLimit: true, wantEvery: 3 * time.Hour,
			steps: func(c *fakeClock, tr *indexers.Tracker) {
				for i := 0; i < 8; i++ {
					tr.NoteRSS(id, 1)
					c.t = c.t.Add(3 * time.Hour)
				}
				c.t = time.Date(2026, 10, 9, 23, 59, 0, 0, time.UTC)
			}},
		{name: "a new day starts a new budget", setLimit: 25, calls: 1, wantLimit: true, wantAllow: true, wantEvery: 3 * time.Hour,
			steps: func(c *fakeClock, tr *indexers.Tracker) {
				for i := 0; i < 8; i++ {
					tr.NoteRSS(id, 1)
				}
				c.t = time.Date(2026, 10, 10, 0, 1, 0, 0, time.UTC)
			}},
		{name: "searches used the day: the last 40 percent is kept for them", setLimit: 25, calls: 1, wantLimit: true, wantEvery: 3 * time.Hour,
			steps: func(c *fakeClock, tr *indexers.Tracker) { tr.CountRequest(id, 15) }},
		{name: "just under the reserve still goes", setLimit: 25, calls: 1, wantLimit: true, wantAllow: true, wantEvery: 3 * time.Hour,
			steps: func(c *fakeClock, tr *indexers.Tracker) { tr.CountRequest(id, 14) }},
		{name: "a limit of 1 leaves everything to the searches", setLimit: 1, calls: 1, wantLimit: true},
		{name: "limit 2: one RSS read a day", setLimit: 2, calls: 1, wantLimit: true, wantAllow: true, wantEvery: 24 * time.Hour},
		{name: "limit 3 cannot fit a two-call run", setLimit: 3, calls: 2, wantLimit: true},
		{name: "indexer said it is used up", setLimit: 25, calls: 1, wantLimit: true, wantEvery: 3 * time.Hour,
			steps: func(c *fakeClock, tr *indexers.Tracker) {
				tr.LearnLimit(id, "Request limit reached (26/25)")
			}},
		{name: "a learned limit counts too", calls: 1, wantLimit: true, wantAllow: true, wantEvery: 3 * time.Hour,
			steps: func(c *fakeClock, tr *indexers.Tracker) {
				tr.LearnLimit(id, "Request limit reached (26/25)")
				c.t = time.Date(2026, 10, 10, 0, 1, 0, 0, time.UTC) // back after midnight, limit still known
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newClock()
			tr := indexers.NewTracker(nil, c.now)
			if tc.steps != nil {
				tc.steps(c, tr)
			}
			p := tr.RSSAdvice(id, tc.setLimit, tc.calls)
			if p.Limited != tc.wantLimit || p.Allowed != tc.wantAllow || p.Every != tc.wantEvery {
				t.Fatalf("got %+v, want limited %v allowed %v every %v", p, tc.wantLimit, tc.wantAllow, tc.wantEvery)
			}
		})
	}
}

func TestRSSAdviceOnANilTracker(t *testing.T) {
	var tr *indexers.Tracker
	if p := tr.RSSAdvice(1, 25, 1); !p.Allowed || p.Limited {
		t.Fatalf("an instance without a tracker is not limited: %+v", p)
	}
}
