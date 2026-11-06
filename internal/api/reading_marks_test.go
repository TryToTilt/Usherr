package api_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/comics"
)

type readingMark struct {
	ID       int64   `json:"id"`
	Item     string  `json:"item"`
	Kind     string  `json:"kind"`
	Position string  `json:"position"`
	Label    string  `json:"label"`
	Excerpt  string  `json:"excerpt"`
	Note     string  `json:"note"`
	Color    string  `json:"color"`
	Percent  float64 `json:"percent"`
}

func TestReadingMarksArePerPersonAndJumpBack(t *testing.T) {
	f := newEReaderFixture(t)
	base, admin := f.base, f.admin
	marks := fmt.Sprintf("%s/api/books/%d/marks", base, f.hobbit.ID)

	// A bookmark and a highlight with a note, as the reader makes them.
	bm := postJSON[readingMark](t, admin, marks, map[string]any{"kind": "bookmark", "position": "epubcfi(/6/4!/4/2/1:0)", "label": "One", "percent": 12.5}, http.StatusCreated)
	if bm.ID == 0 || bm.Item != fmt.Sprintf("book:%d", f.hobbit.ID) || bm.Label != "One" || bm.Percent != 12.5 {
		t.Fatalf("bookmark: %+v", bm)
	}
	hl := postJSON[readingMark](t, admin, marks, map[string]any{"kind": "highlight", "position": "epubcfi(/6/4!/4/2,/1:5,/1:9)", "excerpt": "  hole ", "note": "cosy", "color": "yellow", "percent": 10}, http.StatusCreated)
	if hl.Excerpt != "hole" || hl.Note != "cosy" || hl.Color != "yellow" {
		t.Fatalf("highlight: %+v", hl)
	}
	// The same bookmark twice is kept once.
	again := postJSON[readingMark](t, admin, marks, map[string]any{"kind": "bookmark", "position": "epubcfi(/6/4!/4/2/1:0)"}, http.StatusCreated)
	if again.ID != bm.ID {
		t.Fatalf("a second bookmark at the same place: %+v", again)
	}

	// In reading order: the highlight (10%) comes before the bookmark (12.5%).
	list := getJSON[[]readingMark](t, admin, marks)
	if len(list) != 2 || list[0].ID != hl.ID || list[1].ID != bm.ID {
		t.Fatalf("list: %+v", list)
	}

	// Bad marks are turned away.
	for _, bad := range []map[string]any{
		{"kind": "highlight", "position": "epubcfi(/6/4)"},           // no words
		{"kind": "note", "position": "epubcfi(/6/4)"},                // no such kind
		{"kind": "bookmark", "position": ""},                         // nowhere
		{"kind": "bookmark", "position": "page:1", "color": "black"}, // not a choice
	} {
		postJSON[map[string]any](t, admin, marks, bad, http.StatusBadRequest)
	}
	postJSON[map[string]any](t, admin, base+"/api/books/99999/marks", map[string]any{"kind": "bookmark", "position": "page:1"}, http.StatusNotFound)

	// The note changes; only the fields sent do.
	upd := postJSONMethod[readingMark](t, admin, http.MethodPut, fmt.Sprintf("%s/api/reading-marks/%d", base, hl.ID), map[string]any{"note": "very cosy"}, http.StatusOK)
	if upd.Note != "very cosy" || upd.Color != "yellow" || upd.Excerpt != "hole" {
		t.Fatalf("update: %+v", upd)
	}

	// Someone else sees none of them and can't change or remove them.
	postJSON[map[string]any](t, admin, base+"/api/users", map[string]any{"username": "sam", "password": "kids-password", "name": "Sam", "role": "member"}, http.StatusCreated)
	jar, _ := cookiejar.New(nil)
	member := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	postJSON[map[string]any](t, member, base+"/api/auth/login", map[string]string{"username": "sam", "password": "kids-password"}, http.StatusOK)
	if theirs := getJSON[[]readingMark](t, member, marks); len(theirs) != 0 {
		t.Fatalf("another person sees: %+v", theirs)
	}
	postJSONMethod[map[string]any](t, member, http.MethodPut, fmt.Sprintf("%s/api/reading-marks/%d", base, hl.ID), map[string]any{"note": "mine now"}, http.StatusNotFound)
	postJSONMethod[map[string]any](t, member, http.MethodDelete, fmt.Sprintf("%s/api/reading-marks/%d", base, hl.ID), nil, http.StatusNotFound)
	mine := postJSON[readingMark](t, member, marks, map[string]any{"kind": "bookmark", "position": "page:3"}, http.StatusCreated)
	if mine.ID == bm.ID {
		t.Fatal("two people share a bookmark")
	}

	// Removing one leaves the rest.
	postJSONMethod[map[string]any](t, admin, http.MethodDelete, fmt.Sprintf("%s/api/reading-marks/%d", base, bm.ID), nil, http.StatusNoContent)
	if list := getJSON[[]readingMark](t, admin, marks); len(list) != 1 || list[0].ID != hl.ID {
		t.Fatalf("after removing: %+v", list)
	}
}

func TestComicBookmarks(t *testing.T) {
	f := newEReaderFixture(t)
	base, admin := f.base, f.admin
	f.server.TestComicsOn(true)
	repo := f.server.TestComicRepo()
	sr, err := repo.UpsertSeries(comics.Series{CVID: 9201, Name: "Harbour Lights", Folder: filepath.Join(t.TempDir(), "hl")})
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 2; n++ {
		if _, err := repo.UpsertIssue(comics.Issue{SeriesID: sr.ID, CVID: int64(9300 + n), Number: fmt.Sprint(n)}); err != nil {
			t.Fatal(err)
		}
	}
	issues, _ := repo.Issues(sr.ID)
	for i, is := range issues {
		postJSON[readingMark](t, admin, fmt.Sprintf("%s/api/comics/issues/%d/marks", base, is.ID), map[string]any{"position": fmt.Sprintf("page:%d", 4+i), "label": fmt.Sprintf("Page %d", 4+i)}, http.StatusCreated)
	}
	// Comics have bookmarks only.
	postJSON[map[string]any](t, admin, fmt.Sprintf("%s/api/comics/issues/%d/marks", base, issues[0].ID), map[string]any{"kind": "highlight", "position": "page:2", "excerpt": "words"}, http.StatusBadRequest)

	one := getJSON[[]readingMark](t, admin, fmt.Sprintf("%s/api/comics/issues/%d/marks", base, issues[0].ID))
	if len(one) != 1 || one[0].Kind != "bookmark" || one[0].Position != "page:4" {
		t.Fatalf("issue marks: %+v", one)
	}
	// The series page lists every issue's.
	all := getJSON[[]readingMark](t, admin, fmt.Sprintf("%s/api/comics/series/%d/marks", base, sr.ID))
	if len(all) != 2 {
		t.Fatalf("series marks: %+v", all)
	}

	f.server.TestComicsOn(false)
	if status, _ := doStatus(t, admin, http.MethodGet, fmt.Sprintf("%s/api/comics/series/%d/marks", base, sr.ID)); status != http.StatusNotFound {
		t.Fatalf("comics off: %d", status)
	}
}
