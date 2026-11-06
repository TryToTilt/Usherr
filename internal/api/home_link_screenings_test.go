package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/link/relay"
	"github.com/rdborg/mediarium/internal/link/screening"
)

// linkUp switches linking on for each Usherr, on the relay at url, with a
// name.
func linkUp(t *testing.T, url string, names map[*linkUsherr]string) {
	t.Helper()
	for u, name := range names {
		if st, body := u.call(t, http.MethodPut, "/api/link/settings", map[string]any{"on": true, "relay": url, "name": name}); st != http.StatusOK || body["status"].(map[string]any)["state"] != "connected" {
			t.Fatalf("switch on %s: %d %v", name, st, body)
		}
	}
}

// linkPair links two Usherrs with a code.
func linkPair(t *testing.T, host, joiner *linkUsherr) {
	t.Helper()
	st, inv := host.call(t, http.MethodPost, "/api/link/invites", map[string]any{})
	if st != http.StatusCreated {
		t.Fatalf("invite: %d %v", st, inv)
	}
	if st, body := joiner.call(t, http.MethodPost, "/api/link/join", map[string]any{"code": inv["code"]}); st != http.StatusOK || body["state"] != "linked" {
		t.Fatalf("join: %d %v", st, body)
	}
}

func peerIDNamed(t *testing.T, u *linkUsherr, name string) int64 {
	t.Helper()
	for _, p := range u.peers(t) {
		if p["theirName"] == name && p["status"] == "linked" {
			return int64(p["id"].(float64))
		}
	}
	t.Fatalf("no link named %q: %v", name, u.peers(t))
	return 0
}

func (u linkUsherr) screenings(t *testing.T) []map[string]any {
	t.Helper()
	_, body := u.call(t, http.MethodGet, "/api/home/screenings", nil)
	var out []map[string]any
	for _, x := range body["screenings"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

func (u linkUsherr) linkedGuests(t *testing.T, showtime int64) []map[string]any {
	t.Helper()
	_, body := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/showtimes/%d/linked", showtime), nil)
	var out []map[string]any
	for _, x := range body["peers"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

// Robin hosts; Sam is invited; Alex is linked to Robin but not invited.
// The invite picker shares the showtime, Sam sees it with Robin's name,
// answers, and Robin sees the answer and whether Sam has the film. Moves
// and cancels reach Sam; Alex can't answer; unlinking takes it all away.
func TestLinkScreenings(t *testing.T) {
	api.SetScreeningRosterWaitForTest(50 * time.Millisecond)
	rl := relay.New(relay.Options{})
	ts := httptest.NewServer(rl)
	t.Cleanup(func() { rl.Close(); ts.Close() })
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	robin, sam, alex := newLinkUsherr(t), newLinkUsherr(t), newLinkUsherr(t)
	linkUp(t, url, map[*linkUsherr]string{&robin: "Robin's Usherr", &sam: "Sam's Usherr", &alex: "Alex's Usherr"})
	linkPair(t, &robin, &sam)
	linkPair(t, &robin, &alex)
	samRow, alexRow := peerIDNamed(t, &robin, "Sam's Usherr"), peerIDNamed(t, &robin, "Alex's Usherr")

	at := time.Now().Add(3 * time.Hour).Truncate(time.Minute).UnixMilli()
	st, booked := robin.call(t, http.MethodPost, "/api/home/showtimes", map[string]any{
		"tmdbId": 603, "title": "The Matrix", "year": 1999, "runtime": 136, "posterUrl": "https://image.tmdb.org/t/p/w500/m.jpg",
		"atMs": at, "preshowMin": 10, "playOn": "none",
	})
	if st != http.StatusCreated {
		t.Fatalf("book: %d %v", st, booked)
	}
	showtime := int64(booked["id"].(float64))
	if _, list := robin.call(t, http.MethodGet, "/api/home/showtimes", nil); list["linkOn"] != true {
		t.Fatalf("the picker isn't offered: %v", list)
	}

	// The picker: both links, nobody invited yet.
	if g := robin.linkedGuests(t, showtime); len(g) != 2 || g[0]["invited"] != false || g[1]["invited"] != false {
		t.Fatalf("picker before: %v", g)
	}
	st, body := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/linked", showtime), map[string]any{"peers": []int64{samRow}})
	if st != http.StatusOK {
		t.Fatalf("invite: %d %v", st, body)
	}
	if st, _ := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/linked", showtime), map[string]any{"peers": []int64{99999}}); st != http.StatusBadRequest {
		t.Fatalf("an Usherr that isn't linked: %d", st)
	}

	// Sam's Showtime has it, with Robin's name; Alex's doesn't (only
	// screenings it's invited to).
	linkWait(t, "Sam gets the screening", func() bool { x := sam.screenings(t); return len(x) == 1 && x[0]["has"] != nil }) // looked for in the library just after
	x := sam.screenings(t)[0]
	if x["host"] != "Robin's Usherr" || x["hostName"] != "Ryan" || x["title"] != "The Matrix" || x["atMs"].(float64) != float64(at) || x["preshowMin"].(float64) != 10 ||
		x["posterUrl"] != "https://image.tmdb.org/t/p/w500/m.jpg" || x["has"] != "no" {
		t.Fatalf("Sam's screening: %v", x)
	}
	if len(alex.screenings(t)) != 0 {
		t.Fatal("Alex sees a screening it isn't invited to")
	}
	// Sam said it doesn't have the film, and that the screening arrived.
	samGuest := func() map[string]any {
		for _, g := range robin.linkedGuests(t, showtime) {
			if int64(g["id"].(float64)) == samRow {
				return g
			}
		}
		return nil
	}
	linkWait(t, "Robin hears Sam has no copy", func() bool { g := samGuest(); return g["has"] == "no" && g["seen"] == true })

	// Sam's people answer; Robin sees the household's answer.
	screeningID := int64(x["id"].(float64))
	if st, body := sam.call(t, http.MethodPut, fmt.Sprintf("/api/home/screenings/%d/rsvp", screeningID), map[string]any{"answer": "sure"}); st != http.StatusBadRequest {
		t.Fatalf("a made-up answer: %d %v", st, body)
	}
	st, body = sam.call(t, http.MethodPut, fmt.Sprintf("/api/home/screenings/%d/rsvp", screeningID), map[string]any{"answer": "going"})
	if st != http.StatusOK || body["screenings"].([]any)[0].(map[string]any)["mine"] != "going" {
		t.Fatalf("rsvp: %d %v", st, body)
	}
	linkWait(t, "Robin sees Sam is coming", func() bool { g := samGuest(); return g["answer"] == "going" && g["count"] == float64(1) })
	_, list := robin.call(t, http.MethodGet, "/api/home/showtimes", nil)
	if l := list["showtimes"].([]any)[0].(map[string]any)["linked"].(map[string]any); l["invited"] != float64(1) || l["going"] != float64(1) || l["people"] != float64(1) {
		t.Fatalf("the showtime's summary: %v", l)
	}
	// Who's coming reaches everyone at the screening (fanned out), the
	// host's own household first.
	linkWait(t, "the roster reaches Sam", func() bool {
		r := sam.screenings(t)[0]["roster"].([]any)
		return len(r) == 2 && r[0].(map[string]any)["name"] == "Robin's Usherr" && r[1].(map[string]any)["answer"] == "going" && r[1].(map[string]any)["name"] == "Sam's Usherr"
	})

	// Alex isn't invited: its answer is refused.
	robinNode := robin.server.TestLinkNode()
	sid := robin.server.TestScreeningSID(showtime)
	before := robinNode.Dropped("refused")
	if err := alex.server.TestLinkNode().Send(t.Context(), robinNode.Identity().ID(), screening.TypeRSVP, screening.RSVP{V: 1, SID: sid, Answer: "going", Count: 4}); err != nil {
		t.Fatal(err)
	}
	linkWait(t, "Alex's answer refused", func() bool { return robinNode.Dropped("refused") > before })
	for _, g := range robin.linkedGuests(t, showtime) {
		if int64(g["id"].(float64)) == alexRow && (g["invited"] != false || g["answer"] != nil) {
			t.Fatalf("Alex's answer was taken: %v", g)
		}
	}

	// Moved: Sam's Showtime follows.
	if st, _ := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d", showtime), map[string]any{"atMs": at + 15*60_000, "preshowMin": 5, "playOn": "none"}); st != http.StatusOK {
		t.Fatalf("move: %d", st)
	}
	linkWait(t, "the move reaches Sam", func() bool {
		x := sam.screenings(t)[0]
		return x["atMs"].(float64) == float64(at+15*60_000) && x["preshowMin"].(float64) == 5
	})
	// Sam says it saw the change, so the job doesn't send it again.
	linkWait(t, "Sam saw the latest", func() bool { return samGuest()["seen"] == true })

	// Invited, then taken off the list: Sam is told it's off.
	if st, _ := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/linked", showtime), map[string]any{"peers": []int64{samRow, alexRow}}); st != http.StatusOK {
		t.Fatal("invite both")
	}
	linkWait(t, "Alex gets it", func() bool { return len(alex.screenings(t)) == 1 })
	robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/linked", showtime), map[string]any{"peers": []int64{samRow}})
	linkWait(t, "taken off Alex's", func() bool { return len(alex.screenings(t)) == 0 })
	if len(sam.screenings(t)) != 1 {
		t.Fatal("Sam lost the screening when Alex was taken off")
	}

	// A second showtime, cancelled: gone at Sam's.
	_, second := robin.call(t, http.MethodPost, "/api/home/showtimes", map[string]any{"tmdbId": 78, "title": "Blade Runner", "year": 1982, "runtime": 117, "atMs": at + 24*3600_000, "preshowMin": 0, "playOn": "none"})
	secondID := int64(second["id"].(float64))
	robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/linked", secondID), map[string]any{"peers": []int64{samRow}})
	linkWait(t, "Sam gets the second", func() bool { return len(sam.screenings(t)) == 2 })
	if st, _ := robin.call(t, http.MethodDelete, fmt.Sprintf("/api/home/showtimes/%d", secondID), nil); st != http.StatusNoContent {
		t.Fatalf("cancel: %d", st)
	}
	linkWait(t, "the cancel reaches Sam", func() bool { return len(sam.screenings(t)) == 1 })

	// Unlinked: the screening goes on both sides.
	samSide := peerIDNamed(t, &sam, "Robin's Usherr")
	if st, _ := sam.call(t, http.MethodDelete, fmt.Sprintf("/api/link/peers/%d", samSide), nil); st != http.StatusOK {
		t.Fatalf("unlink: %d", st)
	}
	if len(sam.screenings(t)) != 0 {
		t.Fatal("Sam still has the screening after unlinking")
	}
	linkWait(t, "gone at Robin's", func() bool {
		_, list := robin.call(t, http.MethodGet, "/api/home/showtimes", nil)
		return list["showtimes"].([]any)[0].(map[string]any)["linked"] == nil
	})
}

func TestScreeningDifferentCut(t *testing.T) {
	for _, c := range []struct {
		a, b int
		want bool
	}{{136, 136, false}, {136, 138, false}, {136, 139, true}, {140, 136, true}, {0, 136, false}, {136, 0, false}} {
		if got := api.DifferentCutForTest(c.a, c.b); got != c.want {
			t.Errorf("%d vs %d: %v", c.a, c.b, got)
		}
	}
}
