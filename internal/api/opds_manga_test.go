package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/manga"
)

func TestMangaInOPDS(t *testing.T) {
	s, repo, root := mangaTestServer(t)
	user := mangaUser(t, s, "reader")
	sr := addSeries(t, repo, "Feed", 3, 1)
	v := fileVolume(t, repo, root, sr, "2", 3)
	if _, err := s.db.Exec(`INSERT INTO home_positions (user_id, item, position, duration, updated_ms) VALUES (?, ?, 2, 3, 1)`, user, manga.PositionItem(v.ID)); err != nil {
		t.Fatal(err)
	}
	account := &auth.User{ID: user}
	view := opdsView{root: "/opds"}
	get := func(fn func(w http.ResponseWriter, r *http.Request)) string {
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	list := get(func(w http.ResponseWriter, r *http.Request) { s.opdsMangaSeriesList(w, r, view, account) })
	if !strings.Contains(list, "Feed") || !strings.Contains(list, fmt.Sprintf("/opds/manga/series/%d", sr.ID)) {
		t.Fatal(list)
	}
	feed := get(func(w http.ResponseWriter, r *http.Request) {
		s.opdsMangaVolumes(w, r, view, account, strconv.FormatInt(sr.ID, 10))
	})
	if !strings.Contains(feed, "Feed, Volume 2") || strings.Contains(feed, "Volume 1") || !strings.Contains(feed, fmt.Sprintf("/opds/manga/volumes/%d/pages/{pageNumber}", v.ID)) || !strings.Contains(feed, `pse:count="3"`) || !strings.Contains(feed, `pse:lastRead="1"`) {
		t.Fatal(feed)
	}
	file := func(parts ...string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.opdsMangaFile(rec, httptest.NewRequest(http.MethodGet, "/", nil), account, parts)
		return rec
	}
	id := strconv.FormatInt(v.ID, 10)
	if rec := file("manga", "volumes", id, "pages", "0"); rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("%d", rec.Code)
	}
	if rec := file("manga", "volumes", id, "pages", "3"); rec.Code != http.StatusNotFound {
		t.Fatalf("past the end: %d", rec.Code)
	}
	if rec := file("manga", "volumes", id, "download"); rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("%d", rec.Code)
	}
}
