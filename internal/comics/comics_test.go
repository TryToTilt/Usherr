package comics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/store"
)

func testRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepo(db)
}

func TestRepoKeepsStateOnRefresh(t *testing.T) {
	r := testRepo(t)
	sr, err := r.UpsertSeries(Series{CVID: 3170, Name: "Groo the Wanderer", StartYear: 1982, MonitorNew: true, Folder: "/data/comics/Groo/Volume 01 (1982)"})
	if err != nil || sr.ID == 0 {
		t.Fatalf("add series: %+v %v", sr, err)
	}
	for _, n := range []string{"10", "2", "1", "1.5"} {
		if _, err := r.UpsertIssue(Issue{SeriesID: sr.ID, CVID: int64(1000 + len(n)*10 + int(SortKey(n))), Number: n}); err != nil {
			t.Fatal(err)
		}
	}
	issues, _ := r.Issues(sr.ID)
	var order []string
	for _, i := range issues {
		order = append(order, i.Number)
	}
	if strings.Join(order, ",") != "1,1.5,2,10" {
		t.Errorf("reading order %v", order)
	}
	first := issues[0]
	if _, err := r.SetWanted([]int64{first.ID}, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetState(first.ID, StatusDownloaded, "/data/comics/a.cbz", 123); err != nil {
		t.Fatal(err)
	}
	// A refresh renames the issue but keeps wanted, status and file.
	isNew, err := r.UpsertIssue(Issue{SeriesID: sr.ID, CVID: first.CVID, Number: "1", Title: "Renamed"})
	if err != nil || isNew {
		t.Fatalf("refresh: new=%v err=%v", isNew, err)
	}
	got, _ := r.GetIssue(first.ID)
	if got.Title != "Renamed" || !got.Wanted || got.Status != StatusDownloaded || got.FilePath != "/data/comics/a.cbz" {
		t.Errorf("after refresh: %+v", got)
	}
	// Refreshing the series keeps its folder.
	again, _ := r.UpsertSeries(Series{CVID: 3170, Name: "Groo the Wanderer", StartYear: 1982})
	if again.Folder != sr.Folder || !again.MonitorNew {
		t.Errorf("series refresh lost settings: %+v", again)
	}
	list, _ := r.ListSeries()
	if len(list) != 1 || list[0].Have != 1 {
		t.Errorf("list: %+v", list)
	}
}

func TestReleased(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	if !(Issue{StoreDate: "2026-10-01"}).Released(now) || (Issue{StoreDate: "2026-12-01"}).Released(now) || !(Issue{}).Released(now) {
		t.Error("released dates wrong")
	}
}

func TestComicVineErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") == "good" {
			_, _ = w.Write([]byte(`{"status_code":1,"number_of_total_results":1,"results":[{"id":3170,"name":"Groo the Wanderer","start_year":"1982","count_of_issues":120,"publisher":{"name":"Pacific Comics"},"image":{"medium_url":"https://comicvine.gamespot.com/a/x.jpg"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status_code":100,"error":"Invalid API Key","results":[]}`))
	}))
	defer srv.Close()
	key := "bad"
	cv := NewComicVine("test", func() string { return key })
	cv.Base, cv.Gap = srv.URL, 0
	if err := cv.Test(context.Background()); err != ErrKeyRefused {
		t.Errorf("bad key: %v", err)
	}
	key = "good"
	got, err := cv.SearchSeries(context.Background(), "groo", 5)
	if err != nil || len(got) != 1 || got[0].StartYear != 1982 || got[0].Publisher != "Pacific Comics" {
		t.Errorf("search: %+v %v", got, err)
	}
	key = ""
	if _, err := cv.SearchSeries(context.Background(), "groo", 5); err != ErrNoKey {
		t.Errorf("no key: %v", err)
	}
}
