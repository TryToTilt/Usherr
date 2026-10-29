package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// GitHub #94: nzb360's "Radarr Missing Movies" card said "No results found"
// although there were monitored movies without a file. Radarr lists every
// monitored movie without a file, released or not, so a title that is only
// "Waiting for a release" (no date yet, or a date still ahead) belongs there.
func TestArrWantedMissingListsEveryMonitoredMovieWithoutAFile(t *testing.T) {
	server, base, admin, _, _ := familyServer(t)
	key := postJSON[map[string]any](t, admin, base+"/api/auth/api-keys", map[string]any{"name": "nzb360"}, http.StatusCreated)["key"].(string)
	radarr := base + "/radarr/api/v3"
	dir := t.TempDir()

	add := func(tmdb int, title string, monitored bool, date string) library.Movie {
		m, err := server.MovieRepo.Add(library.Movie{TMDBID: tmdb, Title: title, Year: 2030, Monitored: monitored, ReleaseDate: date})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	add(801, "Alpha Released", true, "2020-01-01")
	add(802, "Bravo Future", true, "2999-01-01")
	add(803, "Charlie No Date", true, "")
	add(804, "Delta Unmonitored", false, "2020-01-01")
	add(805, "Echo Unmonitored Future", false, "2999-01-01")
	filed := add(806, "Foxtrot Has File", true, "2020-01-01")
	if err := os.WriteFile(filepath.Join(dir, "f.mkv"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetStatus(filed.ID, library.StatusDownloaded, "Bluray-1080p", filepath.Join(dir, "f.mkv")); err != nil {
		t.Fatal(err)
	}
	busy := add(807, "Golf Downloading", true, "2020-06-01")
	if err := server.MovieRepo.SetStatus(busy.ID, library.StatusDownloading, "", ""); err != nil {
		t.Fatal(err)
	}

	titles := func(page map[string]any) []string {
		var out []string
		for _, r := range list(page["records"]) {
			out = append(out, obj(r)["title"].(string))
		}
		return out
	}
	tests := []struct {
		name  string
		query string
		want  []string // titles in the order returned
		total int
	}{
		{"monitored, as the card asks", "page=1&pageSize=20&sortKey=movieMetadata.sortTitle&sortDirection=ascending&includeMovie=true&monitored=true", []string{"Alpha Released", "Bravo Future", "Charlie No Date", "Golf Downloading"}, 4},
		{"descending", "page=1&pageSize=20&sortKey=movieMetadata.sortTitle&sortDirection=descending&monitored=true", []string{"Golf Downloading", "Charlie No Date", "Bravo Future", "Alpha Released"}, 4},
		{"no monitored filter", "page=1&pageSize=20&sortKey=movieMetadata.sortTitle&sortDirection=ascending", []string{"Alpha Released", "Bravo Future", "Charlie No Date", "Golf Downloading"}, 4},
		{"no parameters at all", "", []string{"Alpha Released", "Bravo Future", "Charlie No Date", "Golf Downloading"}, 4},
		{"monitored=false adds the others", "page=1&pageSize=20&sortKey=movieMetadata.sortTitle&sortDirection=ascending&monitored=false", []string{"Alpha Released", "Bravo Future", "Charlie No Date", "Delta Unmonitored", "Echo Unmonitored Future", "Golf Downloading"}, 6},
		{"a sort key the app made up", "sortKey=whatever&sortDirection=ascending&monitored=true", []string{"Alpha Released", "Bravo Future", "Charlie No Date", "Golf Downloading"}, 4},
		{"page 2 of size 3 keeps the total", "page=2&pageSize=3&sortKey=movieMetadata.sortTitle&sortDirection=ascending&monitored=true", []string{"Golf Downloading"}, 4},
		{"page past the end", "page=5&pageSize=3&monitored=true", nil, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := obj(arrCall(t, "GET", radarr+"/wanted/missing?"+tc.query, key, nil, http.StatusOK))
			got := titles(page)
			if page["totalRecords"] != float64(tc.total) {
				t.Fatalf("totalRecords %v, want %d (%v)", page["totalRecords"], tc.total, got)
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("records %v, want %v", got, tc.want)
			}
			for _, r := range list(page["records"]) {
				rec := obj(r)
				if rec["hasFile"] != false || rec["movieFile"] != nil || rec["id"] == nil || rec["tmdbId"] == nil {
					t.Errorf("a missing movie should say hasFile=false, with the movie itself: %v", rec)
				}
				if rec["monitored"] != true && !strings.Contains(tc.query, "monitored=false") {
					t.Errorf("only monitored movies expected: %v", rec)
				}
			}
		})
	}

	// The same movies are found by the other way an app can ask.
	var viaMovie []string
	for _, r := range list(arrCall(t, "GET", radarr+"/movie?monitored=true&hasFile=false", key, nil, http.StatusOK)) {
		viaMovie = append(viaMovie, obj(r)["title"].(string))
	}
	sort.Strings(viaMovie)
	if want := "Alpha Released|Bravo Future|Charlie No Date|Golf Downloading"; strings.Join(viaMovie, "|") != want {
		t.Errorf("/movie?monitored=true&hasFile=false gave %v", viaMovie)
	}

	// An unreleased movie keeps its real status in the movie object.
	for _, r := range list(obj(arrCall(t, "GET", radarr+"/wanted/missing?monitored=true", key, nil, http.StatusOK))["records"]) {
		rec := obj(r)
		switch rec["title"] {
		case "Bravo Future":
			if rec["status"] != "announced" || rec["isAvailable"] != false {
				t.Errorf("future movie: %v", fmt.Sprint(rec["status"], rec["isAvailable"]))
			}
		case "Charlie No Date":
			if rec["status"] != "tba" {
				t.Errorf("movie without a date: %v", rec["status"])
			}
		case "Alpha Released":
			if rec["status"] != "released" || rec["isAvailable"] != true {
				t.Errorf("released movie: %v", fmt.Sprint(rec["status"], rec["isAvailable"]))
			}
		}
	}
}
