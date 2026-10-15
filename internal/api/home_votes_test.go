package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/store"
)

func TestVotesArePerPersonAndCanBeTakenBack(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, u := range []string{"alex", "sam"} {
		if _, err := db.Exec(`INSERT INTO users (username, password_hash) VALUES (?, 'x')`, u); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{db: db}
	put := func(uid int64, body string) int {
		w := httptest.NewRecorder()
		s.handlePutVote(w, withUser(httptest.NewRequest(http.MethodPut, "/api/home/votes", strings.NewReader(body)), uid))
		return w.Code
	}
	if c := put(1, `{"kind":"movie","ref":"603","vote":1,"title":"The Matrix","year":1999}`); c != 200 {
		t.Fatalf("vote: %d", c)
	}
	_ = put(1, `{"kind":"album","ref":"b1a9c0e9-d987-4042-ae91-78d6a3267d69","vote":-1,"title":"Some album"}`)
	if c := put(1, `{"kind":"podcast","ref":"1","vote":1}`); c != 400 {
		t.Fatal("an unknown kind was accepted")
	}
	if got := s.votesOf(t.Context(), 1, "", 0); len(got) != 2 {
		t.Fatalf("alex has %d votes", len(got))
	}
	if got := s.votesOf(t.Context(), 2, "", 0); len(got) != 0 {
		t.Fatal("votes leaked to another person")
	}
	_ = put(1, `{"kind":"movie","ref":"603","vote":-1}`)
	if got := s.votesOf(t.Context(), 1, "movie", 0); len(got) != 1 || got[0].Vote != -1 {
		t.Fatalf("a changed vote: %+v", got)
	}
	_ = put(1, `{"kind":"movie","ref":"603","vote":0}`)
	if got := s.votesOf(t.Context(), 1, "movie", 0); len(got) != 0 {
		t.Fatal("a vote taken back is still there")
	}
}

// withUser makes r come from account uid, as the sign-in middleware would.
func withUser(r *http.Request, uid int64) *http.Request {
	return r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: uid}))
}
