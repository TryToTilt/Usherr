package trakt

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUserHistoryReadsEveryPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("trakt-api-key") != "key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/users/sam/history":
			w.Header().Set("X-Pagination-Page-Count", "2")
			if r.URL.Query().Get("page") == "1" {
				_, _ = w.Write([]byte(`[{"watched_at":"2026-01-02T21:00:00.000Z","type":"movie","movie":{"title":"Heat","year":1995,"ids":{"tmdb":949}}}]`))
				return
			}
			_, _ = w.Write([]byte(`[{"watched_at":"2026-01-01T20:00:00.000Z","type":"episode","episode":{"season":1,"number":2},"show":{"title":"Severance","year":2022,"ids":{"tmdb":95396}}},
				{"watched_at":"2026-01-01T20:00:00.000Z","type":"episode","episode":{"season":1,"number":3},"show":{"title":"No ids","ids":{}}}]`))
		case "/users/private/history":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := NewWithBaseURL("key", srv.URL)
	got, err := c.UserHistory(t.Context(), "sam")
	if err != nil || len(got) != 2 {
		t.Fatalf("history: %v %+v", err, got)
	}
	if got[0].Kind != "movie" || got[0].TMDBID != 949 || got[1].Kind != "tv" || got[1].TMDBID != 95396 || got[1].Episode != 2 {
		t.Fatalf("items: %+v", got)
	}
	if _, err := c.UserHistory(t.Context(), "private"); !errors.Is(err, ErrPrivate) {
		t.Fatalf("a private profile: %v", err)
	}
}
