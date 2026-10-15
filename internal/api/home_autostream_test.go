package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestAutostreamWeekAndOwnership(t *testing.T) {
	s := diaryServer(t)
	ctx := t.Context()
	if _, err := s.db.Exec(`INSERT INTO users (username, password_hash) VALUES ('sam', 'x')`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	for _, at := range []string{old, now.UTC().Format(time.RFC3339), now.Add(-time.Hour).UTC().Format(time.RFC3339)} {
		if _, err := s.db.Exec(`INSERT INTO home_autostream_added (user_id, kind, tmdb_id, title, added_at) VALUES (1, 'movie', 1, 'x', ?)`, at); err != nil {
			t.Fatal(err)
		}
	}
	if n := s.autostreamThisWeek(ctx, 1, now); n != 2 {
		t.Fatalf("this week: %d, want 2 (one is older than a week)", n)
	}
	if n := s.autostreamThisWeek(ctx, 2, now); n != 0 {
		t.Fatalf("someone else's count: %d", n)
	}
	// Only the person it was added for can take it out.
	w := httptest.NewRecorder()
	req := withUser(httptest.NewRequest(http.MethodDelete, "/", nil), 2)
	req.SetPathValue("id", strconv.Itoa(2))
	s.handleAutostreamRemove(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("someone else took it out: %d", w.Code)
	}
	w = httptest.NewRecorder()
	req = withUser(httptest.NewRequest(http.MethodDelete, "/", nil), 1)
	req.SetPathValue("id", strconv.Itoa(2))
	s.handleAutostreamRemove(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("taking it out: %d %s", w.Code, w.Body)
	}
	list := s.autostreamList(ctx, 1)
	removed := 0
	for _, a := range list {
		if a.Removed {
			removed++
		}
	}
	if len(list) != 3 || removed != 1 {
		t.Fatalf("list: %d entries, %d taken out", len(list), removed)
	}
	// Taken out still counts for the week: the limit is on adding.
	if n := s.autostreamThisWeek(ctx, 1, now); n != 2 {
		t.Fatalf("after taking one out: %d", n)
	}
}
