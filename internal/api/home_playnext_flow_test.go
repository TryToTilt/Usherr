package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// "Up next": the next library episode, its file when it has one; across a
// season; streamed when it has aired but has no file; nothing when it
// hasn't aired.
func TestPlayNextEpisode(t *testing.T) {
	server, base, client := loginNewServer(t)
	future := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	show, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 9100, Title: "Night Desk", Year: 2020, Monitored: true}, []library.Episode{
		{Season: 0, Episode: 1, AirDate: "2020-01-01", Title: "Special"},
		{Season: 1, Episode: 1, AirDate: "2020-02-01", Title: "One"},
		{Season: 1, Episode: 2, AirDate: "2020-02-08", Title: "Two"},
		{Season: 2, Episode: 1, AirDate: "2021-02-01", Title: "Three"},
		{Season: 2, Episode: 2, AirDate: future, Title: "Not yet"},
	})
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := server.MovieRepo.ListEpisodes(show.ID)
	byName := map[string]library.Episode{}
	for _, e := range eps {
		byName[e.Title] = e
	}
	file := filepath.Join(t.TempDir(), "S01E02.mkv")
	if err := os.WriteFile(file, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetEpisodeStatus(byName["Two"].ID, library.StatusDownloaded, "WEBDL-1080p", file); err != nil {
		t.Fatal(err)
	}
	next := func(q string) map[string]any {
		t.Helper()
		got := getJSON[map[string]any](t, client, base+"/api/play/next?"+q)
		n, _ := got["next"].(map[string]any)
		return n
	}
	id := func(e library.Episode) string { return "episodeId=" + itoa(e.ID) }

	// S01E01 -> S01E02, which has a file.
	n := next(id(byName["One"]))
	if n == nil || n["episode"] != float64(2) || n["episodeId"] != float64(byName["Two"].ID) || n["show"] != "Night Desk" {
		t.Fatalf("after one: %v", n)
	}
	// S01E02 -> S02E01 across the season, no file: streamed.
	n = next(id(byName["Two"]))
	if n == nil || n["season"] != float64(2) || n["episode"] != float64(1) || n["episodeId"] != nil || n["tmdbId"] != float64(9100) {
		t.Fatalf("after two: %v", n)
	}
	// S02E01 -> S02E02 hasn't aired: nothing.
	if n := next(id(byName["Three"])); n != nil {
		t.Fatalf("not aired yet: %v", n)
	}
	// A streamed episode of a show in the library uses the library's list.
	if n := next("tmdbId=9100&season=1&episode=1"); n == nil || n["episodeId"] != float64(byName["Two"].ID) {
		t.Fatalf("by tmdb: %v", n)
	}
	if st, _ := doJSONStatus(t, client, http.MethodGet, base+"/api/play/next?tmdbId=9100", nil); st != http.StatusBadRequest {
		t.Fatalf("no episode said: %d", st)
	}
}
