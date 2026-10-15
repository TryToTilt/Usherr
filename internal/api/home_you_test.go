package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/store"
)

func TestYouSharingAndVoteLinks(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO users (username, password_hash) VALUES ('alex', 'x')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db}
	ctx := t.Context()

	// Not shared until chosen; another person's taste stays out of sight.
	if s.sharesTaste(ctx, 1) {
		t.Fatal("taste is shared before anyone chose to")
	}
	w := httptest.NewRecorder()
	s.handleYouHousehold(w, withUser(httptest.NewRequest(http.MethodGet, "/api/home/you/household", nil), 1))
	if !strings.Contains(w.Body.String(), `"share":false`) || !strings.Contains(w.Body.String(), `"people":[]`) {
		t.Fatalf("the household showed to someone not sharing: %s", w.Body)
	}
	w = httptest.NewRecorder()
	s.handleYouShare(w, withUser(httptest.NewRequest(http.MethodPut, "/api/home/you/share", strings.NewReader(`{"share":true}`)), 1))
	if w.Code != 200 || !s.sharesTaste(ctx, 1) {
		t.Fatalf("sharing wasn't saved: %d", w.Code)
	}

	// Each vote opens where it can.
	for _, v := range []string{
		`{"kind":"movie","ref":"603","vote":1,"title":"The Matrix"}`,
		`{"kind":"book","ref":"OL45883W","vote":1,"title":"A book"}`,
		`{"kind":"artist","ref":"a74b1b7f-71a5-4011-9441-d0b5e4122711","vote":-1,"title":"An artist"}`,
	} {
		rec := httptest.NewRecorder()
		s.handlePutVote(rec, withUser(httptest.NewRequest(http.MethodPut, "/api/home/votes", strings.NewReader(v)), 1))
	}
	w = httptest.NewRecorder()
	s.handleYouVotes(w, withUser(httptest.NewRequest(http.MethodGet, "/api/home/you/votes", nil), 1))
	var got struct {
		Votes []youVote `json:"votes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Votes) != 3 {
		t.Fatalf("votes: %s", w.Body)
	}
	to := map[string]string{}
	for _, v := range got.Votes {
		to[v.Kind] = v.To
	}
	if to["movie"] != "/title/603" || to["book"] != "/books/work/OL45883W" || to["artist"] != "" {
		t.Fatalf("links: %v", to)
	}
}

func TestTasteCosine(t *testing.T) {
	a := map[string]float64{"k:heist": 2, "m:Steven Soderbergh": 1, "l:en": 5}
	if c := tasteCosine(a, a); c < 0.999 {
		t.Fatalf("a taste against itself: %v", c)
	}
	if c := tasteCosine(a, map[string]float64{"k:haunted house": 2, "l:en": 5}); c != 0 {
		t.Fatalf("nothing in common but the language: %v", c)
	}
}
