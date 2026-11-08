package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Step 4: a watch party's code books the same film for the same moment, with
// its pre-show, looked up on TMDB; the same code again gives that booking
// back; a mistyped code, or one for a party already started, is refused.
func TestJoinAShowtimeByItsCode(t *testing.T) {
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/movie/68726") {
			_, _ = w.Write([]byte(`{"id":68726,"title":"Pacific Rim","release_date":"2013-07-11","runtime":131,"poster_path":"/p.jpg"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer tmdb.Close()
	server, base, admin := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	at := time.Now().Add(2 * time.Hour).Truncate(time.Minute)
	code := server.TestShowtimeCode(68726, at.UnixMilli(), 5)
	st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes/join", map[string]any{"code": "Showtime code: `" + strings.ToLower(code) + "`", "playOn": "none"})
	if st != http.StatusCreated || body["title"] != "Pacific Rim" || int64(body["atMs"].(float64)) != at.UnixMilli() {
		t.Fatalf("join: %d %v", st, body)
	}
	_, list := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/showtimes", nil)
	got := list["showtimes"].([]any)[0].(map[string]any)
	if got["preshowMin"] != float64(5) || got["runtime"] != float64(131) || got["playOn"] != "none" {
		t.Errorf("booked: %v", got)
	}
	if st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes/join", map[string]any{"code": code}); st != http.StatusOK || body["already"] != true {
		t.Errorf("the same code again: %d %v", st, body)
	}
	bad := []byte(code)
	bad[0] ^= 1
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes/join", map[string]any{"code": string(bad)}); st != http.StatusBadRequest {
		t.Errorf("a mistyped code: %d", st)
	}
	past := server.TestShowtimeCode(68726, time.Now().Add(-time.Hour).UnixMilli(), 5)
	if st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes/join", map[string]any{"code": past}); st != http.StatusBadRequest || !strings.Contains(body["error"].(string), "already started") {
		t.Errorf("a party already started: %d %v", st, body)
	}
}

// A party joined by its code gives a ticket stub in this Usherr too, with no
// Discord link: at the door when the film starts; stamped by hand from the
// film's start until half an hour after its end, or by the player at 90%.
func TestJoinedPartyGivesAStubHere(t *testing.T) {
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/movie/68726") {
			_, _ = w.Write([]byte(`{"id":68726,"title":"Pacific Rim","release_date":"2013-07-11","runtime":131,"poster_path":"/p.jpg"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer tmdb.Close()
	server, base, admin := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)
	stubs := func() []map[string]any {
		_, d := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/stubs", nil)
		var out []map[string]any
		for _, s := range d["stubs"].([]any) {
			out = append(out, s.(map[string]any))
		}
		return out
	}
	join := func(in time.Duration) int64 {
		code := server.TestShowtimeCode(68726, time.Now().Add(in).Truncate(time.Minute).UnixMilli(), 0)
		st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes/join", map[string]any{"code": code, "playOn": "none"})
		if st != http.StatusCreated {
			t.Fatalf("join: %d %v", st, body)
		}
		return int64(body["id"].(float64))
	}

	// Stamped by hand, after the credits.
	first := join(2 * time.Hour)
	if got := stubs(); len(got) != 0 {
		t.Fatalf("a stub before the film started: %v", got)
	}
	server.TestShowtimeAt(first, time.Now().Add(-time.Minute).UnixMilli())
	server.TestShowtimeJob(context.Background())
	got := stubs()
	if len(got) != 1 || got[0]["here"] != true || got[0]["title"] != "Pacific Rim" || got[0]["stamped"] != false || got[0]["stampUntil"] == nil || got[0]["no"].(float64) < 1 {
		t.Fatalf("the stub at the door: %v", got)
	}
	// Stamp it works from the film's start (not before) until half an hour
	// after its end.
	stamp := fmt.Sprintf("%s/api/home/stubs/%d/stamp", base, first)
	server.TestStubsAt(first, time.Now().Add(time.Minute).UnixMilli())
	if st, body := doJSONStatus(t, admin, http.MethodPost, stamp, nil); st != http.StatusConflict || !strings.Contains(body["error"].(string), "opens when the film starts") {
		t.Errorf("stamping before the film: %d %v", st, body)
	}
	server.TestStubsAt(first, time.Now().Add(-3*time.Hour).UnixMilli())
	server.TestStubsUntil(first, time.Now().Add(-time.Minute).UnixMilli())
	if st, body := doJSONStatus(t, admin, http.MethodPost, stamp, nil); st != http.StatusConflict || !strings.Contains(body["error"].(string), "is over") {
		t.Errorf("stamping long after: %d %v", st, body)
	}
	server.TestStubsAt(first, time.Now().Add(-time.Minute).UnixMilli())
	server.TestStubsUntil(first, time.Now().Add(time.Hour).UnixMilli())
	if st, _ := doJSONStatus(t, admin, http.MethodPost, stamp, nil); st != http.StatusNoContent {
		t.Errorf("stamp while the film plays: %d", st)
	}
	if got := stubs(); got[0]["stamped"] != true || got[0]["stampUntil"] != nil {
		t.Errorf("stamped: %v", got)
	}

	// Stamped by the player reaching the credits.
	second := join(3 * time.Hour)
	server.TestShowtimeAt(second, time.Now().Add(-time.Minute).UnixMilli())
	server.TestShowtimeJob(context.Background())
	put := func(pos float64) {
		if st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/position", map[string]any{"item": "title:movie:68726", "position": pos, "duration": 7860}); st != http.StatusNoContent {
			t.Fatalf("position: %d", st)
		}
	}
	put(5) // barely begun: not the credits
	put(3600)
	if got := stubs(); got[0]["stamped"] != false {
		t.Fatalf("stamped before the credits: %v", got)
	}
	put(7080) // 90% of 7860 s
	if got := stubs(); len(got) != 2 || got[0]["stamped"] != true {
		t.Errorf("stamped at 90%%: %v", got)
	}
}
