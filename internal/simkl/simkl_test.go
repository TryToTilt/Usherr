package simkl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testClientID = "client-id-for-tests-0123456789"
const testToken = "token-for-tests-0123456789abcdef"

func fake(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewWithBaseURL(testClientID, srv.URL)
}

func TestNoClientID(t *testing.T) {
	c := NewWithBaseURL("", "http://127.0.0.1:1")
	if _, err := c.StartPIN(context.Background()); !errors.Is(err, ErrNoClientID) {
		t.Fatalf("StartPIN without an id: %v", err)
	}
	if _, err := c.Items(context.Background(), testToken, StatusPlanToWatch, 0); !errors.Is(err, ErrNoClientID) {
		t.Fatalf("Items without an id: %v", err)
	}
}

func TestStartPIN(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/pin" || r.URL.Query().Get("client_id") != testClientID {
			t.Errorf("unexpected request %s", r.URL)
		}
		if r.Header.Get("simkl-api-key") != testClientID {
			t.Errorf("missing simkl-api-key header")
		}
		_, _ = w.Write([]byte(`{"result":"OK","device_code":"x","user_code":"AB12C","verification_url":"https://evil.example/pin","expires_in":900,"interval":5}`))
	})
	p, err := c.StartPIN(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.UserCode != "AB12C" || p.ExpiresIn != 900*time.Second || p.Interval != 5*time.Second {
		t.Errorf("pin = %+v", p)
	}
	if p.VerificationURL != "https://simkl.com/pin" {
		t.Errorf("a verification address outside simkl.com must be ignored, got %q", p.VerificationURL)
	}
}

func TestStartPINBadClientID(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"ko": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"result":"KO","message":"nope"}`))
		},
		"forbidden": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"precond":   func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusPreconditionFailed) },
		"401":       func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fake(t, h).StartPIN(context.Background()); !errors.Is(err, ErrInvalidClientID) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestCheckPINStates(t *testing.T) {
	answers := map[string]string{
		"WAIT1": `{"result":"KO","message":"Authorization pending"}`,
		"DONE1": `{"result":"OK","access_token":"` + testToken + `"}`,
		"GONE1": `{"result":"KO","message":"Code expired"}`,
	}
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Path, "/oauth/pin/")
		body, ok := answers[code]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	ctx := context.Background()
	if tok, err := c.CheckPIN(ctx, "WAIT1"); tok != "" || err != nil {
		t.Errorf("pending: %q %v", tok, err)
	}
	if tok, err := c.CheckPIN(ctx, "DONE1"); tok != testToken || err != nil {
		t.Errorf("ok: %q %v", tok, err)
	}
	if _, err := c.CheckPIN(ctx, "GONE1"); !errors.Is(err, ErrPINExpired) {
		t.Errorf("expired: %v", err)
	}
	if _, err := c.CheckPIN(ctx, "../etc"); !errors.Is(err, ErrPINExpired) {
		t.Errorf("a code that is not a code must not reach Simkl: %v", err)
	}
}

const moviesBody = `{"movies":[
 {"status":"plantowatch","movie":{"title":"The Matrix","year":1999,"ids":{"simkl":1,"imdb":"tt0133093","tmdb":"603"}}},
 {"status":"plantowatch","movie":{"title":"Only IMDb","year":2001,"ids":{"simkl":2,"imdb":"tt0000002"}}},
 {"status":"plantowatch","movie":{"title":"","year":null,"ids":{"simkl":3}}}]}`

const showsBody = `{"shows":[
 {"status":"plantowatch","show":{"title":"Severance","year":2022,"ids":{"simkl":10,"imdb":"tt11280740","tmdb":95396,"tvdb":371980}}},
 {"status":"plantowatch","show":{"title":"Only TVDB","year":2010,"ids":{"simkl":11,"tvdb":"12345"}}}]}`

const animeBody = `{"anime":[
 {"anime_type":"tv","status":"plantowatch","show":{"title":"Frieren","year":2023,"ids":{"simkl":20,"mal":52991,"tvdb":424536}}},
 {"anime_type":"movie","status":"plantowatch","show":{"title":"Your Name","year":2016,"ids":{"simkl":21,"tmdb":372058}}}]}`

func listServer(t *testing.T) *Client {
	return fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("simkl-api-key") != testClientID {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("extended") != "full" {
			t.Errorf("extended=full missing on %s", r.URL)
		}
		switch r.URL.Path {
		case "/sync/all-items/movies/plantowatch":
			_, _ = w.Write([]byte(moviesBody))
		case "/sync/all-items/shows/plantowatch":
			_, _ = w.Write([]byte(showsBody))
		case "/sync/all-items/anime/plantowatch":
			_, _ = w.Write([]byte(animeBody))
		default:
			_, _ = w.Write([]byte(`null`))
		}
	})
}

func TestItemsParsesMoviesShowsAndAnime(t *testing.T) {
	got, err := listServer(t).Items(context.Background(), testToken, StatusPlanToWatch, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []Media{
		{Kind: "movie", Title: "The Matrix", Year: 1999, TMDBID: 603, IMDBID: "tt0133093"},
		{Kind: "movie", Title: "Only IMDb", Year: 2001, IMDBID: "tt0000002"},
		{Kind: "tv", Title: "Severance", Year: 2022, TMDBID: 95396, IMDBID: "tt11280740", TVDBID: 371980},
		{Kind: "tv", Title: "Only TVDB", Year: 2010, TVDBID: 12345},
		{Kind: "tv", Title: "Frieren", Year: 2023, TVDBID: 424536},
		{Kind: "movie", Title: "Your Name", Year: 2016, TMDBID: 372058},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestItemsEmptyList(t *testing.T) {
	got, err := listServer(t).Items(context.Background(), testToken, StatusCompleted, 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty list: %v %v", got, err)
	}
}

func TestItemsErrors(t *testing.T) {
	status := func(code int, hdr map[string]string) *Client {
		return fake(t, func(w http.ResponseWriter, r *http.Request) {
			for k, v := range hdr {
				w.Header().Set(k, v)
			}
			w.WriteHeader(code)
		})
	}
	ctx := context.Background()
	if _, err := status(401, nil).Items(ctx, testToken, StatusPlanToWatch, 0); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("401: %v", err)
	}
	var rl *RateLimitError
	if _, err := status(429, map[string]string{"Retry-After": "30"}).Items(ctx, testToken, StatusPlanToWatch, 0); !errors.As(err, &rl) || rl.RetryAfter != 30*time.Second {
		t.Errorf("429: %v", err)
	}
	if _, err := status(503, nil).Items(ctx, testToken, StatusPlanToWatch, 0); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("503: %v", err)
	}
	if _, err := status(200, nil).Items(ctx, "", StatusPlanToWatch, 0); !errors.Is(err, ErrNotConnected) {
		t.Errorf("no token: %v", err)
	}
	if _, err := status(200, nil).Items(ctx, testToken, "../x", 0); err == nil {
		t.Errorf("a status Simkl does not use must be refused")
	}
	garbage := fake(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`<html>`)) })
	if _, err := garbage.Items(ctx, testToken, StatusPlanToWatch, 0); err == nil {
		t.Errorf("an answer that is not JSON must fail")
	}
}

func TestErrorsNeverCarryTheClientIDOrToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // nothing listens any more: the request fails with a *url.Error that holds the address
	c := NewWithBaseURL(testClientID, base)
	_, err := c.StartPIN(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), testClientID) {
		t.Errorf("the client id leaked into the error: %v", err)
	}
	_, err = c.CheckPIN(context.Background(), "AB12C")
	if err == nil || strings.Contains(err.Error(), testClientID) {
		t.Errorf("the client id leaked into the error: %v", err)
	}
	_, err = c.Items(context.Background(), testToken, StatusPlanToWatch, 0)
	if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), testClientID) {
		t.Errorf("a secret leaked into the error: %v", err)
	}
}

func TestAnswerSizeIsLimited(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		chunk := []byte(strings.Repeat(" ", 1<<20))
		for i := 0; i < 34; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	if _, err := c.Items(context.Background(), testToken, StatusPlanToWatch, 0); err == nil || !strings.Contains(err.Error(), "too big") {
		t.Errorf("an answer over the limit must be refused, got %v", err)
	}
}
