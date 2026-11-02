package api_test

import (
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
