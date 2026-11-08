package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/link/relay"
)

func (u linkUsherr) stubs(t *testing.T) []map[string]any {
	t.Helper()
	_, body := u.call(t, http.MethodGet, "/api/home/stubs", nil)
	var out []map[string]any
	for _, x := range body["stubs"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

func (u linkUsherr) badge(t *testing.T, id string) map[string]any {
	t.Helper()
	_, body := u.call(t, http.MethodGet, "/api/home/party-badges", nil)
	for _, x := range body["badges"].([]any) {
		if b := x.(map[string]any); b["id"] == id {
			return b
		}
	}
	t.Fatalf("no badge %q: %v", id, body)
	return nil
}

// Robin hosts a screening Sam's Usherr is coming to. At the door Robin gets
// the host's stub (seat 0) and Sam's Usherr gets a stub for its person over
// the link, numbered by Robin's Usherr. Sam stamps it at home; Robin's
// Usherr takes the stamp and confirms it, so both sides agree. Both earn
// their first badge.
func TestLinkRewards(t *testing.T) {
	api.SetScreeningRosterWaitForTest(50 * time.Millisecond)
	rl := relay.New(relay.Options{})
	ts := httptest.NewServer(rl)
	t.Cleanup(func() { rl.Close(); ts.Close() })
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	robin, sam := newLinkUsherr(t), newLinkUsherr(t)
	linkUp(t, url, map[*linkUsherr]string{&robin: "Robin's Usherr", &sam: "Sam's Usherr"})
	linkPair(t, &robin, &sam)
	samRow := peerIDNamed(t, &robin, "Sam's Usherr")

	at := time.Now().Add(30 * time.Minute).Truncate(time.Second).UnixMilli()
	st, booked := robin.call(t, http.MethodPost, "/api/home/showtimes", map[string]any{
		"tmdbId": 1091, "title": "The Thing", "year": 1982, "runtime": 109, "atMs": at, "preshowMin": 0, "playOn": "none",
	})
	if st != http.StatusCreated {
		t.Fatalf("book: %d %v", st, booked)
	}
	showtime := int64(booked["id"].(float64))
	if st, body := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/linked", showtime), map[string]any{"peers": []int64{samRow}}); st != http.StatusOK {
		t.Fatalf("invite: %d %v", st, body)
	}
	linkWait(t, "the screening reaches Sam", func() bool { return len(sam.screenings(t)) == 1 })
	id := int64(sam.screenings(t)[0]["id"].(float64))
	if st, body := sam.call(t, http.MethodPut, fmt.Sprintf("/api/home/screenings/%d/rsvp", id), map[string]any{"answer": "going"}); st != http.StatusOK {
		t.Fatalf("rsvp: %d %v", st, body)
	}
	linkWait(t, "Robin hears Sam is coming", func() bool {
		for _, g := range robin.linkedGuests(t, showtime) {
			if int64(g["id"].(float64)) == samRow && g["answer"] == "going" {
				return true
			}
		}
		return false
	})

	// The door.
	robin.server.TestShowtimeAt(showtime, time.Now().Add(-time.Minute).UnixMilli())
	robin.server.TestShowtimeJob(context.Background())
	mine := robin.stubs(t)
	if len(mine) != 1 || mine[0]["seat"] != float64(0) || mine[0]["hosted"] != true || mine[0]["title"] != "The Thing" || mine[0]["stampUntil"] == nil {
		t.Fatalf("the host's stub: %v", mine)
	}
	if g, _ := mine[0]["guests"].([]any); len(g) != 1 || g[0] != "Sam's Usherr" {
		t.Fatalf("the host's stub, with: %v", mine[0])
	}
	linkWait(t, "Sam's stub arrives", func() bool { return len(sam.stubs(t)) == 1 })
	theirs := sam.stubs(t)[0]
	if theirs["linked"] != "Robin's Usherr" || theirs["seat"] != float64(1) || theirs["no"] != mine[0]["no"].(float64)+1 || theirs["host"] != "Ryan" || theirs["title"] != "The Thing" ||
		theirs["hosted"] == true || theirs["stamped"] != false || theirs["stampUntil"] == nil {
		t.Fatalf("Sam's stub: %v", theirs)
	}
	// Running the door again hands out nothing new.
	robin.server.TestShowtimeJob(context.Background())
	robin.server.TestRewardJob(context.Background())
	time.Sleep(200 * time.Millisecond)
	if len(robin.stubs(t)) != 1 || len(sam.stubs(t)) != 1 {
		t.Fatal("the door ran twice")
	}

	// Sam stamps it at home: Robin's Usherr confirms.
	stamp := fmt.Sprintf("/api/home/stubs/here/%d/stamp", int64(theirs["showtimeId"].(float64)))
	if st, body := sam.call(t, http.MethodPost, stamp, nil); st != http.StatusNoContent {
		t.Fatalf("stamp: %d %v", st, body)
	}
	linkWait(t, "the host confirms the stamp", func() bool { s := sam.stubs(t)[0]; return s["stamped"] == true && s["agreed"] == true })

	// Badges: a first party each.
	for name, u := range map[string]linkUsherr{"Robin": robin, "Sam": sam} {
		if b := u.badge(t, "first"); b["earned"] == nil || b["have"] != float64(1) {
			t.Errorf("%s's first badge: %v", name, b)
		}
		if b := u.badge(t, "five"); b["earned"] != nil || b["have"] != float64(1) || b["need"] != float64(5) {
			t.Errorf("%s's five nights: %v", name, b)
		}
	}
	if b := robin.badge(t, "host"); b["have"] != float64(1) {
		t.Errorf("Robin hosted: %v", b)
	}
	if b := sam.badge(t, "host"); b["have"] != float64(0) {
		t.Errorf("Sam didn't host: %v", b)
	}
}
