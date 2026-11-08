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
)

func (u linkUsherr) chat(t *testing.T, showtime int64) map[string]any {
	t.Helper()
	st, body := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/showtimes/%d/chat", showtime), nil)
	if st != http.StatusOK {
		t.Fatalf("chat %d: %d %v", showtime, st, body)
	}
	return body
}

func (u linkUsherr) chatTexts(t *testing.T, showtime int64) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range u.chat(t, showtime)["messages"].([]any) {
		out = append(out, m.(map[string]any))
	}
	return out
}

func (u linkUsherr) reactions(t *testing.T, showtime int64) map[string]any {
	t.Helper()
	st, body := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/showtimes/%d/reactions", showtime), nil)
	if st != http.StatusOK {
		t.Fatalf("reactions %d: %d %v", showtime, st, body)
	}
	return body
}

// Robin hosts; Sam and Alex are in. What anyone says reaches everyone at
// the screening (the host passes guests' messages on), with the house it
// was said at, as plain text, maybe a spoiler; reactions too, pinned to the
// second of the film, and the most-reacted moment is found. Each person is
// held to a rate; a showtime that isn't linked still has no chat here.
func TestLinkChatAndReactions(t *testing.T) {
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

	at := time.Now().Add(30 * time.Minute).Truncate(time.Second).UnixMilli()
	st, booked := robin.call(t, http.MethodPost, "/api/home/showtimes", map[string]any{
		"tmdbId": 1091, "title": "The Thing", "year": 1982, "runtime": 109, "atMs": at, "preshowMin": 0, "playOn": "none",
	})
	if st != http.StatusCreated {
		t.Fatalf("book: %d %v", st, booked)
	}
	showtime := int64(booked["id"].(float64))
	// Not linked yet: no chat (it isn't a Discord party either).
	if st, _ := robin.call(t, http.MethodGet, fmt.Sprintf("/api/home/showtimes/%d/chat", showtime), nil); st != http.StatusNotFound {
		t.Fatalf("a plain showtime's chat: %d", st)
	}
	if st, body := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/linked", showtime), map[string]any{"peers": []int64{samRow, alexRow}}); st != http.StatusOK {
		t.Fatalf("invite: %d %v", st, body)
	}
	booking := map[string]int64{}
	for name, g := range map[string]linkUsherr{"Sam": sam, "Alex": alex} {
		linkWait(t, name+": the screening arrives", func() bool { return len(g.screenings(t)) == 1 })
		id := int64(g.screenings(t)[0]["id"].(float64))
		if st, body := g.call(t, http.MethodPut, fmt.Sprintf("/api/home/screenings/%d/rsvp", id), map[string]any{"answer": "going"}); st != http.StatusOK {
			t.Fatalf("%s rsvp: %d %v", name, st, body)
		}
		booking[name] = int64(g.hostedBooking(t)["id"].(float64))
	}
	samShowtime, alexShowtime := booking["Sam"], booking["Alex"]

	// The film is on, 42 minutes in.
	if st, body := robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/room/playback", showtime), map[string]any{"state": "film", "position": 2520}); st != http.StatusOK {
		t.Fatalf("play: %d %v", st, body)
	}
	for name, g := range map[string]struct {
		u  linkUsherr
		id int64
	}{"Sam": {sam, samShowtime}, "Alex": {alex, alexShowtime}} {
		linkWait(t, name+" follows", func() bool { s, _, _ := roomState(g.u.room(t, g.id)); return s == "film" })
	}

	c := robin.chat(t, showtime)
	if c["link"] != true || c["role"] != "host" || c["canPost"] != true || len(c["messages"].([]any)) != 0 {
		t.Fatalf("the host's chat: %v", c)
	}

	// Robin says hello: everyone has it.
	if st, body := robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/chat", showtime), map[string]any{"text": "Lights off, <i>everyone</i>"}); st != http.StatusCreated {
		t.Fatalf("host says: %d %v", st, body)
	}
	for name, g := range map[string]struct {
		u  linkUsherr
		id int64
	}{"Sam": {sam, samShowtime}, "Alex": {alex, alexShowtime}} {
		linkWait(t, name+" hears the host", func() bool { return len(g.u.chatTexts(t, g.id)) == 1 })
		m := g.u.chatTexts(t, g.id)[0]
		if m["text"] != "Lights off, <i>everyone</i>" || m["house"] != "Robin's Usherr" || m["user"].(map[string]any)["name"] != "Ryan" || m["pos"].(float64) < 2520 {
			t.Fatalf("%s: %v", name, m)
		}
	}

	// Sam says a spoiler: through the host to Alex, once each.
	if st, body := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/chat", samShowtime), map[string]any{"text": "  wait till the\ndefib scene ", "spoiler": true}); st != http.StatusCreated {
		t.Fatalf("guest says: %d %v", st, body)
	}
	for name, g := range map[string]struct {
		u  linkUsherr
		id int64
	}{"Robin": {robin, showtime}, "Sam": {sam, samShowtime}, "Alex": {alex, alexShowtime}} {
		linkWait(t, name+" hears Sam", func() bool { return len(g.u.chatTexts(t, g.id)) == 2 })
		m := g.u.chatTexts(t, g.id)[1]
		if m["text"] != "wait till the defib scene" || m["spoiler"] != true || m["house"] != "Sam's Usherr" {
			t.Fatalf("%s: %v", name, m)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(sam.chatTexts(t, samShowtime)); n != 2 {
		t.Fatalf("Sam has %d messages, want 2 (its own isn't echoed twice)", n)
	}

	// Too long, empty: not sent.
	if st, _ := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/chat", samShowtime), map[string]any{"text": strings.Repeat("a", 600)}); st != http.StatusBadRequest {
		t.Fatalf("600 characters: %d", st)
	}
	if st, _ := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/chat", samShowtime), map[string]any{"text": " \n "}); st != http.StatusBadRequest {
		t.Fatalf("nothing: %d", st)
	}

	// Reactions: Alex and Sam scream around 1:12:40, Robin laughs earlier.
	react := func(u linkUsherr, id int64, word string, pos float64) int {
		st, _ := u.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/react", id), map[string]any{"word": word, "pos": pos})
		return st
	}
	cur := func() float64 { _, p, _ := roomState(robin.room(t, showtime)); return p }
	p := cur()
	if st := react(robin, showtime, "HA", p); st != http.StatusCreated {
		t.Fatalf("host reacts: %d", st)
	}
	// The film moves on to 1:12:30.
	robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/room/playback", showtime), map[string]any{"state": "film", "position": 4350})
	linkWait(t, "the guests at 1:12:30", func() bool {
		_, a, _ := roomState(alex.room(t, alexShowtime))
		_, b, _ := roomState(sam.room(t, samShowtime))
		return a >= 4350 && b >= 4350
	})
	for range 3 {
		if st := react(alex, alexShowtime, "EEK", 4360); st != http.StatusCreated {
			t.Fatalf("Alex reacts: %d", st)
		}
	}
	if st := react(sam, samShowtime, "EEK", 4361); st != http.StatusCreated {
		t.Fatalf("Sam reacts: %d", st)
	}
	if st := react(sam, samShowtime, "NOPE", 4362); st != http.StatusCreated {
		t.Fatalf("Sam reacts: %d", st)
	}
	// A position far from the room's while it plays: the room's instead.
	if st := react(sam, samShowtime, "WOW", 10); st != http.StatusCreated {
		t.Fatalf("Sam reacts: %d", st)
	}
	// Not a reaction.
	if st := react(sam, samShowtime, "LOL", 4362); st != http.StatusBadRequest {
		t.Fatalf("LOL: %d", st)
	}
	for name, g := range map[string]struct {
		u  linkUsherr
		id int64
	}{"Robin": {robin, showtime}, "Sam": {sam, samShowtime}, "Alex": {alex, alexShowtime}} {
		linkWait(t, name+" has every reaction", func() bool { return len(g.u.reactions(t, g.id)["reactions"].([]any)) == 7 })
		r := g.u.reactions(t, g.id)
		top := r["top"].(map[string]any)
		words := top["words"].(map[string]any)
		if top["from"] != float64(4350) || top["to"] != float64(4380) || top["top"] != "EEK" || words["EEK"] != float64(4) || words["NOPE"] != float64(1) || words["WOW"] != float64(1) || top["total"] != float64(6) {
			t.Fatalf("%s's most-reacted moment: %v", name, top)
		}
		if heat := r["heat"].([]any); len(heat) != 2 || r["bucket"] != float64(30) {
			t.Fatalf("%s's heat: %v", name, r["heat"])
		}
	}

	// The film's reactions, for anyone playing it later: without names.
	st, film := robin.call(t, http.MethodGet, "/api/home/reactions/film?tmdb=1091", nil)
	if st != http.StatusOK || len(film["reactions"].([]any)) != 7 || film["top"].(map[string]any)["top"] != "EEK" {
		t.Fatalf("the film's reactions: %d %v", st, film)
	}
	if first := film["reactions"].([]any)[0].(map[string]any); first["name"] != nil || first["house"] != nil {
		t.Fatalf("a film's reaction says who: %v", first)
	}

	// A person here is held to about one reaction a second.
	limited := false
	for range 8 {
		if react(alex, alexShowtime, "CLAP", 4370) == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("eight reactions at once all taken")
	}
}
