package api_test

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/link/relay"
	"github.com/rdborg/mediarium/internal/link/screening"
)

func (u linkUsherr) room(t *testing.T, showtime int64) map[string]any {
	t.Helper()
	st, body := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/showtimes/%d/room", showtime), nil)
	if st != http.StatusOK {
		t.Fatalf("room %d: %d %v", showtime, st, body)
	}
	return body
}

func roomState(room map[string]any) (string, float64, float64) {
	now := room["now"].(map[string]any)
	return now["state"].(string), now["position"].(float64), now["seq"].(float64)
}

// hostedBooking is a guest's booking of a linked screening, in its own
// Showtime.
func (u linkUsherr) hostedBooking(t *testing.T) map[string]any {
	t.Helper()
	_, list := u.call(t, http.MethodGet, "/api/home/showtimes", nil)
	for _, x := range list["showtimes"].([]any) {
		if s := x.(map[string]any); s["hosted"] != nil {
			return s
		}
	}
	return nil
}

func (u linkUsherr) planSeconds(t *testing.T, showtime int64) (float64, int) {
	t.Helper()
	st, plan := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/preshow/plan?showtime=%d&dry=1", showtime), nil)
	if st != http.StatusOK {
		t.Fatalf("plan: %d %v", st, plan)
	}
	steps, _ := plan["steps"].([]any)
	return plan["seconds"].(float64), len(steps)
}

// Robin hosts; Sam and Alex are invited and say they're in. Each gets the
// screening in its own Showtime, hosted by Robin's Usherr; Robin's
// household leads the roster; the pre-show's recipe makes a pre-show of the
// same length everywhere. Robin plays, pauses and seeks, and both follow;
// Sam can't move the film (its own key isn't the host's), may pause only
// once Robin lets anyone pause, and a guest that missed a message is put
// right by the next heartbeat. Moves and cancels follow.
func TestLinkScreeningRoom(t *testing.T) {
	api.SetScreeningRosterWaitForTest(50 * time.Millisecond)
	api.SetRoomBeatForTest(300*time.Millisecond, 50*time.Millisecond, 24*time.Hour)
	t.Cleanup(func() { api.SetRoomBeatForTest(5*time.Second, time.Second, 15*time.Minute) })
	rl := relay.New(relay.Options{})
	ts := httptest.NewServer(rl)
	t.Cleanup(func() { rl.Close(); ts.Close() })
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	robin, sam, alex := newLinkUsherr(t), newLinkUsherr(t), newLinkUsherr(t)
	linkUp(t, url, map[*linkUsherr]string{&robin: "Robin's Usherr", &sam: "Sam's Usherr", &alex: "Alex's Usherr"})
	linkPair(t, &robin, &sam)
	linkPair(t, &robin, &alex)
	linkPair(t, &sam, &alex) // guests linked to each other too: still only the host moves the film
	samRow, alexRow := peerIDNamed(t, &robin, "Sam's Usherr"), peerIDNamed(t, &robin, "Alex's Usherr")

	at := time.Now().Add(30 * time.Minute).Truncate(time.Second).UnixMilli()
	st, booked := robin.call(t, http.MethodPost, "/api/home/showtimes", map[string]any{
		"tmdbId": 603, "title": "The Matrix", "year": 1999, "runtime": 136, "atMs": at, "preshowMin": 10, "playOn": "none",
	})
	if st != http.StatusCreated {
		t.Fatalf("book: %d %v", st, booked)
	}
	showtime := int64(booked["id"].(float64))
	if st, body := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/linked", showtime), map[string]any{"peers": []int64{samRow, alexRow}}); st != http.StatusOK {
		t.Fatalf("invite: %d %v", st, body)
	}
	for _, g := range []linkUsherr{sam, alex} {
		linkWait(t, "the screening arrives", func() bool { return len(g.screenings(t)) == 1 })
		if g.hostedBooking(t) != nil {
			t.Fatal("booked before anyone said they're in")
		}
	}

	// I'm in: the screening is booked in each guest's own Showtime.
	guestShowtime := map[string]int64{}
	for name, g := range map[string]linkUsherr{"Sam": sam, "Alex": alex} {
		id := int64(g.screenings(t)[0]["id"].(float64))
		if st, body := g.call(t, http.MethodPut, fmt.Sprintf("/api/home/screenings/%d/rsvp", id), map[string]any{"answer": "going"}); st != http.StatusOK {
			t.Fatalf("%s rsvp: %d %v", name, st, body)
		}
		b := g.hostedBooking(t)
		if b == nil {
			t.Fatalf("%s: no booking after I'm in", name)
		}
		h := b["hosted"].(map[string]any)
		if b["atMs"].(float64) != float64(at) || b["preshowMin"].(float64) != 10 || b["title"] != "The Matrix" || h["host"] != "Robin's Usherr" || h["hostName"] != "Ryan" || int64(h["screening"].(float64)) != id {
			t.Fatalf("%s's booking: %v", name, b)
		}
		guestShowtime[name] = int64(b["id"].(float64))
		if g.screenings(t)[0]["showtimeId"].(float64) != b["id"].(float64) {
			t.Fatalf("%s: the screening doesn't say its booking", name)
		}
	}
	samShowtime, alexShowtime := guestShowtime["Sam"], guestShowtime["Alex"]
	// The host's to move or cancel, not the guest's.
	if st, body := sam.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d", samShowtime), map[string]any{"atMs": at + 60_000, "preshowMin": 10, "playOn": "here"}); st != http.StatusConflict || !strings.Contains(body["error"].(string), "Robin's Usherr hosts") {
		t.Fatalf("a guest moves the host's screening: %d %v", st, body)
	}
	if st, _ := sam.call(t, http.MethodDelete, fmt.Sprintf("/api/home/showtimes/%d", samShowtime), nil); st != http.StatusConflict {
		t.Fatalf("a guest cancels the host's screening: %d", st)
	}

	// The roster: Robin's household first, then the guests.
	linkWait(t, "the roster with the host", func() bool {
		x := sam.screenings(t)[0]
		r := x["roster"].([]any)
		if len(r) != 3 || x["rosterHost"] != true {
			return false
		}
		first := r[0].(map[string]any)
		return first["name"] == "Robin's Usherr" && first["answer"] == "going" && first["count"] == float64(1)
	})
	// Robin can't make it after all: the roster says so; then Robin's back.
	if st, body := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/linked/rsvp", showtime), map[string]any{"answer": "not"}); st != http.StatusOK || body["hostGoing"] != float64(0) {
		t.Fatalf("host rsvp: %d %v", st, body)
	}
	linkWait(t, "the host's household not going", func() bool {
		r := alex.screenings(t)[0]["roster"].([]any)
		return len(r) == 3 && r[0].(map[string]any)["answer"] == "not"
	})
	robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/linked/rsvp", showtime), map[string]any{"answer": "going"})
	_, list := robin.call(t, http.MethodGet, "/api/home/showtimes", nil)
	if l := list["showtimes"].([]any)[0].(map[string]any)["linked"].(map[string]any); l["hostGoing"] != float64(1) || l["mine"] != "going" || l["going"] != float64(2) {
		t.Fatalf("the host's summary: %v", l)
	}

	// The pre-show: Robin's is fixed and its recipe sent; each guest builds
	// one just as long.
	robin.server.TestScreeningJob()
	hostSecs, hostSteps := robin.planSeconds(t, showtime)
	if hostSteps == 0 {
		t.Fatal("the host's pre-show is empty")
	}
	if again, _ := robin.planSeconds(t, showtime); again != hostSecs {
		t.Fatalf("the host's pre-show changed: %v then %v", hostSecs, again)
	}
	for name, g := range map[string]struct {
		u  linkUsherr
		id int64
	}{"Sam": {sam, samShowtime}, "Alex": {alex, alexShowtime}} {
		linkWait(t, name+" builds the same pre-show", func() bool {
			secs, steps := g.u.planSeconds(t, g.id)
			return math.Abs(secs-hostSecs) < 0.01 && steps == hostSteps
		})
	}

	// The room, before anything's said: by the schedule, the pre-show isn't on yet.
	hostRoom := robin.room(t, showtime)
	if hostRoom["role"] != "host" || hostRoom["canControl"] != true || hostRoom["following"] != float64(2) || hostRoom["relay"] != "connected" {
		t.Fatalf("the host's room: %v", hostRoom)
	}
	if s, _, _ := roomState(hostRoom); s != "waiting" {
		t.Fatalf("the host's room before: %s", s)
	}
	samRoom := sam.room(t, samShowtime)
	if samRoom["role"] != "guest" || samRoom["canControl"] != false || samRoom["canPause"] != false || samRoom["host"] != "Robin's Usherr" {
		t.Fatalf("Sam's room: %v", samRoom)
	}

	follows := func(what, state string, pos float64) {
		t.Helper()
		for name, g := range map[string]struct {
			u  linkUsherr
			id int64
		}{"Sam": {sam, samShowtime}, "Alex": {alex, alexShowtime}} {
			linkWait(t, name+" follows: "+what, func() bool {
				s, p, _ := roomState(g.u.room(t, g.id))
				if s != state {
					return false
				}
				if state == "film" {
					return p >= pos && p < pos+3
				}
				return math.Abs(p-pos) < 0.01
			})
		}
	}
	play := func(state string, pos float64) {
		t.Helper()
		if st, body := robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/room/playback", showtime), map[string]any{"state": state, "position": pos}); st != http.StatusOK {
			t.Fatalf("%s at %v: %d %v", state, pos, st, body)
		}
	}

	// Robin starts the film at 10:00, then pauses at 11:40; both follow.
	play("film", 600)
	follows("playing from 10:00", "film", 600)
	play("paused", 700)
	follows("paused at 11:40", "paused", 700)

	// Sam isn't the host: its playback, sealed with the screening key it
	// has, is dropped at Alex's; its own room won't let it play.
	sid := robin.server.TestScreeningSID(showtime)
	alexNode := alex.server.TestLinkNode()
	before := alexNode.Dropped("sender")
	forged := screening.Playback{V: 1, SID: sid, Seq: time.Now().Add(time.Hour).UnixMilli(), State: "film", Position: 5000, AtWallTime: time.Now().UnixMilli()}
	if err := sam.server.TestLinkNode().SendGroup(t.Context(), sam.server.TestScreeningKeyIn(sid), []string{alexNode.Identity().ID()}, screening.TypePlayback, forged); err != nil {
		t.Fatal(err)
	}
	linkWait(t, "Sam's playback dropped at Alex's", func() bool { return alexNode.Dropped("sender") > before })
	if s, p, _ := roomState(alex.room(t, alexShowtime)); s != "paused" || p != 700 {
		t.Fatalf("Alex took Sam's playback: %s %v", s, p)
	}
	if st, _ := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/room/playback", samShowtime), map[string]any{"state": "film"}); st != http.StatusForbidden {
		t.Fatalf("a guest moves the film: %d", st)
	}

	// Anyone can pause: off, Sam can't; on, Sam asks and Robin's Usherr
	// pauses for everyone.
	play("film", 800)
	follows("playing from 13:20", "film", 800)
	if st, _ := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/room/pause", samShowtime), nil); st != http.StatusForbidden {
		t.Fatalf("a guest pauses with Anyone can pause off: %d", st)
	}
	if st, body := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/room/settings", showtime), map[string]any{"anyonePause": true}); st != http.StatusOK || body["anyonePause"] != true {
		t.Fatalf("anyone can pause: %d %v", st, body)
	}
	linkWait(t, "Sam hears anyone can pause", func() bool { return sam.room(t, samShowtime)["canPause"] == true })
	if st, body := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/room/pause", samShowtime), nil); st != http.StatusAccepted {
		t.Fatalf("Sam asks to pause: %d %v", st, body)
	}
	linkWait(t, "Robin pauses for Sam", func() bool { s, _, _ := roomState(robin.room(t, showtime)); return s == "paused" })
	_, hostPos, _ := roomState(robin.room(t, showtime))
	follows("paused for Sam", "paused", hostPos)

	// Alex misses a message: the next heartbeat puts it right.
	play("film", 900)
	follows("playing from 15:00", "film", 900)
	alex.server.TestRoomForget(sid)
	if s, _, _ := roomState(alex.room(t, alexShowtime)); s != "waiting" {
		t.Fatalf("Alex forgot, so the schedule holds: %s", s)
	}
	linkWait(t, "the heartbeat reaches Alex", func() bool {
		s, p, _ := roomState(alex.room(t, alexShowtime))
		return s == "film" && p >= 900
	})
	// Robin and Alex agree on where the film is, to a fraction of a second.
	_, hp, _ := roomState(robin.room(t, showtime))
	_, ap, _ := roomState(alex.room(t, alexShowtime))
	if math.Abs(hp-ap) > 0.5 {
		t.Fatalf("Robin at %v, Alex at %v", hp, ap)
	}

	// Moved: the guests' bookings follow, and the room starts over.
	if st, _ := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d", showtime), map[string]any{"atMs": at + 15*60_000, "preshowMin": 10, "playOn": "none"}); st != http.StatusOK {
		t.Fatalf("move: %d", st)
	}
	linkWait(t, "the move reaches Sam's booking", func() bool {
		b := sam.hostedBooking(t)
		return b != nil && b["atMs"].(float64) == float64(at+15*60_000)
	})
	linkWait(t, "Sam's room starts over", func() bool { s, _, _ := roomState(sam.room(t, samShowtime)); return s == "waiting" })

	// Alex can't make it after all: its booking goes.
	alexScreening := int64(alex.screenings(t)[0]["id"].(float64))
	alex.call(t, http.MethodPut, fmt.Sprintf("/api/home/screenings/%d/rsvp", alexScreening), map[string]any{"answer": "not"})
	if alex.hostedBooking(t) != nil {
		t.Fatal("Alex's booking stayed after Can't make it")
	}

	// Called off: gone from Sam's Showtime.
	if st, _ := robin.call(t, http.MethodDelete, fmt.Sprintf("/api/home/showtimes/%d", showtime), nil); st != http.StatusNoContent {
		t.Fatalf("cancel: %d", st)
	}
	linkWait(t, "the cancel takes Sam's booking", func() bool { return sam.hostedBooking(t) == nil && len(sam.screenings(t)) == 0 })
}

// A guest's pre-show from the host's recipe: what it has plays in its slot,
// what it hasn't is a card of the same length, so the whole is as long as
// the host's.
func TestRecipeFill(t *testing.T) {
	items := []screening.RecipeItem{
		{Kind: "card", Card: "welcome", Seconds: 8},
		{Kind: "clip", Category: "Snipes", Seconds: 31.5},                                         // not here
		{Kind: "trailer", TMDB: 550, Title: "Fight Club", Seconds: 140},                           // not here
		{Kind: "card", Card: "rating", Seconds: 7},                                                // not here (no rating)
		{Kind: "trailer", TMDB: 680, Title: "Pulp Fiction", YouTube: "s7EdQ4FqbhY", Seconds: 150}, // here
	}
	have := map[int]bool{0: true, 4: true}
	i := -1
	steps := api.RecipeFillForTest(items, func(screening.RecipeItem) bool { i++; return have[i] })
	if len(steps) != len(items) {
		t.Fatalf("steps: %v", steps)
	}
	total := 0.0
	for k, st := range steps {
		total += st["seconds"].(float64)
		if st["seconds"].(float64) != items[k].Seconds {
			t.Errorf("step %d runs %v, the host's %v", k, st["seconds"], items[k].Seconds)
		}
	}
	if total != screening.RecipeSeconds(items) {
		t.Fatalf("the pre-show runs %v, the host's %v", total, screening.RecipeSeconds(items))
	}
	want := []struct {
		kind, card string
		filled     bool
	}{{"card", "welcome", false}, {"card", "welcome", true}, {"card", "soon", true}, {"card", "welcome", true}, {"trailer", "", false}}
	for k, w := range want {
		if steps[k]["kind"] != w.kind || steps[k]["card"] != w.card || steps[k]["filled"] != w.filled {
			t.Errorf("step %d: %v, want %+v", k, steps[k], w)
		}
	}
}
