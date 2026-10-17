package api_test

import (
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// Movies in the calendar start 30 days back like episodes do: an old release
// of a big library is left out, a coming one (even a year ahead) stays.
func TestCalendarLeavesOutOldMovieReleases(t *testing.T) {
	server, base, client := loginNewServer(t)
	day := func(d int) string { return time.Now().AddDate(0, 0, d).Format("2006-01-02") }
	for i, m := range []library.Movie{
		{TMDBID: 1, Title: "Old", ReleaseDate: day(-400), Monitored: true},
		{TMDBID: 2, Title: "Recent", ReleaseDate: day(-5), Monitored: true},
		{TMDBID: 3, Title: "Far Away", ReleaseDate: day(300), Monitored: true},
	} {
		if _, err := server.MovieRepo.Add(m); err != nil {
			t.Fatalf("add movie %d: %v", i, err)
		}
	}
	got := map[string]bool{}
	for _, e := range getJSON[[]map[string]any](t, client, base+"/api/calendar") {
		got[e["title"].(string)] = true
	}
	if got["Old"] || !got["Recent"] || !got["Far Away"] {
		t.Fatalf("calendar titles: %v", got)
	}
}
