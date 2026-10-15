package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
)

func TestFreshFirstOrdersComingThenNewest(t *testing.T) {
	list := []metadata.Movie{
		{TMDBID: 1, ReleaseDate: "2020-01-01"}, // too old
		{TMDBID: 2, ReleaseDate: "2026-03-01"},
		{TMDBID: 3, ReleaseDate: "2027-05-01"},
		{TMDBID: 4, ReleaseDate: "2026-09-01"},
		{TMDBID: 5, ReleaseDate: "2026-12-01"},
		{TMDBID: 6, ReleaseDate: ""}, // undated
	}
	got := freshFirst(list, movieDate, "2025-10-13", "2026-10-13")
	want := []int{5, 3, 4, 2}
	if len(got) != len(want) {
		t.Fatalf("got %d titles, want %d", len(got), len(want))
	}
	for i, m := range got {
		if m.TMDBID != want[i] {
			t.Fatalf("position %d: got %d, want %d", i, m.TMDBID, want[i])
		}
	}
}
