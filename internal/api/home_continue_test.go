package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rdborg/mediarium/internal/auth"
)

// Continue watching lists your own part-watched films and episodes, newest
// first, one episode per show, and skips what's gone from the library.
func TestContinueWatching(t *testing.T) {
	s, _ := prerollServer(t, &fakePrerollPlex{})
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO movies (id, tmdb_id, title, year, poster_path) VALUES (7, 603, 'The Matrix', 1999, '/m.jpg')`)
	exec(`INSERT INTO streamed_titles (id, kind, tmdb_id, title, year, poster_path) VALUES (1, 'series', 1396, 'Breaking Bad', 2008, '/b.jpg')`)
	put := func(user int64, item string, at int64) {
		exec(`INSERT INTO home_positions (user_id, item, position, duration, updated_ms) VALUES (?, ?, 600, 3600, ?)`, user, item, at)
	}
	put(1, "movie:7", 100)
	put(1, "title:tv:1396:1:2", 200)
	put(1, "title:tv:1396:1:3", 300) // the newer episode of the same show
	put(1, "movie:99", 400)          // no longer in the library
	put(1, "comic:5", 500)           // not a video
	put(2, "movie:7", 600)           // someone else's

	get := func(user int64) []continueItem {
		r := httptest.NewRequest(http.MethodGet, "/api/home/continue", nil)
		r = r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: user}))
		w := httptest.NewRecorder()
		s.handleContinue(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("continue: %d %s", w.Code, w.Body)
		}
		var out []continueItem
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := get(1)
	if len(got) != 2 {
		t.Fatalf("want 2 items, got %+v", got)
	}
	if got[0].Link != "/show/1396" || got[0].Episode != "S01E03" || got[0].Title != "Breaking Bad" {
		t.Errorf("the show: %+v", got[0])
	}
	if got[1].Link != "/title/603" || got[1].Title != "The Matrix" || got[1].Position != 600 || got[1].PosterURL == "" {
		t.Errorf("the film: %+v", got[1])
	}
	if other := get(3); len(other) != 0 {
		t.Errorf("someone with nothing started sees %+v", other)
	}
}
