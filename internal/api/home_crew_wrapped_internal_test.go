package api

import (
	"testing"
	"time"
)

// The year the Crews page offers, and a "most of anyone" badge: a tie
// counts, a month that isn't over doesn't.
func TestCrewWrappedFeaturedAndMostOfBadge(t *testing.T) {
	for _, c := range []struct {
		m    time.Month
		want int
	}{{time.December, 2026}, {time.January, 2025}, {time.June, 0}} {
		if got := crewWrappedFeatured(time.Date(2026, c.m, 15, 12, 0, 0, 0, time.Local)); got != c.want {
			t.Fatalf("%v: %d", c.m, got)
		}
	}
	tallies := []badgeTally{
		{Group: "2026-09", Who: "u1", N: 10, At: 5},
		{Group: "2026-09", Who: "nSam/Sam", N: 10, At: 6},
		{Group: "2026-11", Who: "u1", N: 12, At: 9},
	}
	b := mostOfBadge("clown", tallies, "u1", 10, func(m string) bool { return m < "2026-11" })
	if b.Earned != 5 || b.Have != 10 {
		t.Fatalf("a tie: %+v", b)
	}
	b = mostOfBadge("clown", tallies[2:], "u1", 10, func(m string) bool { return m < "2026-11" })
	if b.Earned != 0 || b.Have != 9 {
		t.Fatalf("this month: %+v", b)
	}
	if c := countBadge("taste", []int64{9, 3, 7, 1}, 3); c.Earned != 7 || c.Have != 3 {
		t.Fatalf("count: %+v", c)
	}
}
