package api_test

import (
	"net/http"
	"testing"
	"time"
)

// Showtimes at home: booked, listed (and on the Marquee), moved, cancelled;
// times that can't be are refused.
func TestShowtimes(t *testing.T) {
	_, base, client := loginNewServer(t)
	at := time.Now().Add(2 * time.Hour).UnixMilli()
	st, body := doJSONStatus(t, client, http.MethodPost, base+"/api/home/showtimes", map[string]any{
		"tmdbId": 693134, "title": "Dune: Part Two", "year": 2024, "runtime": 166, "atMs": at, "preshowMin": 15, "playOn": "none",
	})
	if st != http.StatusCreated {
		t.Fatalf("book: %d %v", st, body)
	}
	id := body["id"].(float64)

	list := getJSON[map[string]any](t, client, base+"/api/home/showtimes")
	items := list["showtimes"].([]any)
	if len(items) != 1 || list["nowMs"] == nil {
		t.Fatalf("list: %v", list)
	}
	got := items[0].(map[string]any)
	if got["title"] != "Dune: Part Two" || got["preshowMin"] != float64(15) || got["state"] != "booked" || got["mine"] != true {
		t.Fatalf("booking: %v", got)
	}
	// On the Marquee too.
	mq := getJSON[map[string]any](t, client, base+"/api/home/marquee?only=playing")
	if s := mq["showtimes"].([]any); len(s) != 1 {
		t.Fatalf("marquee: %v", s)
	}

	// Refused: the past, too far ahead, a long pre-show, an unknown place.
	for _, bad := range []map[string]any{
		{"tmdbId": 1, "title": "X", "atMs": time.Now().Add(-time.Hour).UnixMilli()},
		{"tmdbId": 1, "title": "X", "atMs": time.Now().AddDate(0, 3, 0).UnixMilli()},
		{"tmdbId": 1, "title": "X", "atMs": at, "preshowMin": 90},
		{"tmdbId": 1, "title": "X", "atMs": at, "playOn": "garage"},
		{"tmdbId": 0, "title": "X", "atMs": at},
	} {
		if st, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/home/showtimes", bad); st != http.StatusBadRequest {
			t.Fatalf("%v: %d", bad, st)
		}
	}
	// The TV only when Play on TV is set up.
	if st, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/home/showtimes", map[string]any{"tmdbId": 1, "title": "X", "atMs": at, "playOn": "tv"}); st != http.StatusBadRequest {
		t.Fatalf("tv not set up: %d", st)
	}

	// Moved an hour later.
	later := at + int64(time.Hour/time.Millisecond)
	if st, body := doJSONStatus(t, client, http.MethodPut, base+"/api/home/showtimes/"+itoa(int64(id)), map[string]any{"atMs": later, "playOn": "here"}); st != http.StatusOK {
		t.Fatalf("move: %d %v", st, body)
	}
	got = getJSON[map[string]any](t, client, base+"/api/home/showtimes")["showtimes"].([]any)[0].(map[string]any)
	if got["atMs"] != float64(later) || got["playOn"] != "here" || got["preshowMin"] != float64(15) {
		t.Fatalf("moved: %v", got)
	}

	// Cancelled.
	if st, _ := doJSONStatus(t, client, http.MethodDelete, base+"/api/home/showtimes/"+itoa(int64(id)), nil); st != http.StatusNoContent {
		t.Fatalf("cancel: %d", st)
	}
	if n := len(getJSON[map[string]any](t, client, base+"/api/home/showtimes")["showtimes"].([]any)); n != 0 {
		t.Fatalf("after cancelling: %d", n)
	}
}
