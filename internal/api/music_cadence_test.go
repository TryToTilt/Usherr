package api

import (
	"testing"
	"time"
)

func TestAlbumSearchInterval(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	for _, c := range []struct {
		date string
		want time.Duration
	}{
		{"2026-11-01", 2 * day},
		{"2026-10", 4 * day}, // a month alone counts from its first day: 36 days
		{"2026-09-01", 4 * day},
		{"2026-03-15", 7 * day},
		{"2026", 7 * day},
		{"1997-05-21", 14 * day},
		{"", 14 * day},
		{"soon", 14 * day},
		{"2027-02-01", 2 * day}, // not out yet: looked for often
	} {
		if got := albumSearchInterval(c.date, now); got != c.want {
			t.Errorf("%q: %v, want %v", c.date, got, c.want)
		}
	}
}

func TestAlbumSearchDue(t *testing.T) {
	s := &Server{}
	now := time.Now()
	if !s.albumSearchDue(7, now) {
		t.Fatal("an album never searched is due")
	}
	s.albumSearched(7, "1997-05-21", now)
	if s.albumSearchDue(7, now.Add(10*24*time.Hour)) {
		t.Fatal("an old album is due again within ten days")
	}
	if !s.albumSearchDue(7, now.Add(17*24*time.Hour)) {
		t.Fatal("an old album is not due after seventeen days")
	}
}
