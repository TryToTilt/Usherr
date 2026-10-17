package api_test

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library/librarytest"
)

type wantedPage struct {
	Rows []struct {
		Kind     string `json:"kind"`
		ID       int64  `json:"id"`
		SeriesID int64  `json:"seriesId"`
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		Episodes int    `json:"episodes"`
	} `json:"rows"`
	Total    int `json:"total"`
	Movies   int `json:"movies"`
	Episodes int `json:"episodes"`
}

// Grouped, the Wanted list is one row per movie and per show, a page at a
// time, with totals that match the full list; a show's episodes come on
// their own. Without paging the old full list is unchanged.
func TestWantedGroupedPages(t *testing.T) {
	db, base, client := newServerWithDB(t)
	// 200 movies (every 10th has its file) and 20 shows of 50 episodes.
	if err := librarytest.Fill(db, librarytest.Small); err != nil {
		t.Fatal(err)
	}
	// One show has a single episode left to get, so it is listed as that episode.
	if _, err := db.Exec(`UPDATE episodes SET status = 'downloaded' WHERE series_id = 20 AND NOT (season = 1 AND episode = 1)`); err != nil {
		t.Fatal(err)
	}
	flat := getJSON[[]map[string]any](t, client, base+"/api/wanted?kind=missing")
	var flatMovies, flatEpisodes int
	for _, it := range flat {
		if it["kind"] == "movie" {
			flatMovies++
		} else {
			flatEpisodes++
		}
	}

	all := getJSON[wantedPage](t, client, base+"/api/wanted?kind=missing&group=1&limit=500")
	if all.Movies != flatMovies || all.Episodes != flatEpisodes || all.Total != flatMovies+20 || len(all.Rows) != all.Total {
		t.Fatalf("grouped totals %d movies, %d episodes, %d rows; the full list has %d movies and %d episodes",
			all.Movies, all.Episodes, all.Total, flatMovies, flatEpisodes)
	}
	last := all.Rows[len(all.Rows)-1] // the shows come after the movies, newest added first: show 1 is last
	if last.Kind != "show" || last.SeriesID != 1 || last.Episodes == 0 {
		t.Fatalf("last row = %+v", last)
	}
	var single bool
	for _, r := range all.Rows {
		if r.SeriesID == 20 {
			single = r.Kind == "episode" && r.Subtitle != ""
		}
	}
	if !single {
		t.Fatal("a show with one wanted episode should be listed as that episode")
	}

	page := getJSON[wantedPage](t, client, base+"/api/wanted?kind=missing&group=1&limit=10&offset=175")
	if len(page.Rows) != 10 || page.Total != all.Total || page.Rows[0].ID != all.Rows[175].ID {
		t.Fatalf("page = %d rows of %d, first %+v", len(page.Rows), page.Total, page.Rows)
	}
	if past := getJSON[wantedPage](t, client, base+"/api/wanted?kind=missing&group=1&offset=5000"); len(past.Rows) != 0 || past.Total != all.Total {
		t.Fatalf("past the end: %+v", past)
	}
	if counts := getJSON[wantedPage](t, client, base+"/api/wanted?kind=missing&group=1&limit=0"); len(counts.Rows) != 0 || counts.Movies != all.Movies || counts.Episodes != all.Episodes {
		t.Fatalf("limit=0 = %+v", counts)
	}
	tv := getJSON[wantedPage](t, client, base+"/api/wanted?kind=missing&group=1&media=tv")
	if tv.Total != 20 || tv.Movies != 0 || tv.Episodes != all.Episodes {
		t.Fatalf("media=tv = %+v", tv)
	}
	found := getJSON[wantedPage](t, client, base+"/api/wanted?kind=missing&group=1&q=show+7")
	if found.Total != 1 || found.Rows[0].Title != "Show 7" {
		t.Fatalf("q=show 7 = %+v", found)
	}

	one := getJSON[[]map[string]any](t, client, fmt.Sprintf("%s/api/wanted?kind=missing&series=%d", base, last.SeriesID))
	if len(one) != last.Episodes {
		t.Fatalf("the show's episodes: %d, the row says %d", len(one), last.Episodes)
	}
	for _, it := range one {
		if it["kind"] != "episode" || int64(it["seriesId"].(float64)) != last.SeriesID {
			t.Fatalf("another title in the show's list: %+v", it)
		}
	}

	for _, bad := range []string{"group=1&limit=x", "group=1&offset=-1", "group=1&media=books", "series=abc"} {
		resp, err := client.Get(base + "/api/wanted?kind=missing&" + bad)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d", bad, resp.StatusCode)
		}
	}
}

// An episode marked missing that still names a file on disk is not wanted, in
// the grouped list as in the full one.
func TestWantedGroupedLeavesOutFilesOnDisk(t *testing.T) {
	db, base, client := newServerWithDB(t)
	if err := librarytest.Fill(db, librarytest.Size{Shows: 1, EpisodesPerShow: 3, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	file := t.TempDir() + "/S01E01.mkv"
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE episodes SET file_path = ? WHERE episode = 1`, file); err != nil {
		t.Fatal(err)
	}
	page := getJSON[wantedPage](t, client, base+"/api/wanted?kind=missing&group=1")
	if page.Episodes != 2 || len(page.Rows) != 1 || page.Rows[0].Episodes != 2 {
		t.Fatalf("page = %+v", page)
	}
	dash := getJSON[map[string]any](t, client, base+"/api/dashboard")
	wanted := dash["library"].(map[string]any)["wanted"].(map[string]any)
	if wanted["episodes"].(float64) != 2 {
		t.Fatalf("the dashboard counts %v wanted episodes", wanted["episodes"])
	}
}

// With nothing monitored (what a library import leaves), Wanted answers at
// once, however big the library.
func TestWantedNothingMonitoredIsQuick(t *testing.T) {
	db, base, client := newServerWithDB(t)
	sz := librarytest.Size{Movies: 2000, Shows: 100, EpisodesPerShow: 200}
	if err := librarytest.Fill(db, sz); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/wanted?kind=missing", "/api/wanted?kind=missing&group=1", "/api/dashboard"} {
		start := time.Now()
		getJSON[any](t, client, base+path)
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("%s took %s with nothing monitored", path, took)
		}
	}
	counts := getJSON[map[string]int](t, client, base+"/api/library/counts")
	if counts["movies"] != 2000 || counts["shows"] != 100 {
		t.Fatalf("library counts = %v", counts)
	}
}
