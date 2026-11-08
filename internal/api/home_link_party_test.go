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

func (u linkUsherr) party(t *testing.T, showtime int64) map[string]any {
	t.Helper()
	st, body := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/showtimes/%d/party", showtime), nil)
	if st != http.StatusOK {
		t.Fatalf("party %d: %d %v", showtime, st, body)
	}
	return body
}

func partySeats(p map[string]any) []map[string]any {
	var out []map[string]any
	for _, x := range p["lobby"].(map[string]any)["seats"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

// Robin hosts; Sam is in. The lobby's seats follow the stubs' order (the
// host Row A Seat 1, then Sam's block) and Take my seat reaches everyone.
// Robin calls an intermission: the film pauses for everyone, Sam sets a
// status everyone sees, Robin adds 5 minutes and resumes. At the credits
// both give their stars: they land in each one's own Diary, and the crew's
// card shows the average and how many voted, with a name only for whoever
// shares. A guest can't call the intermission or roll the credits.
func TestLinkPartyLobbyIntermissionVerdict(t *testing.T) {
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
	linkWait(t, "the screening arrives", func() bool { return len(sam.screenings(t)) == 1 })
	sid := int64(sam.screenings(t)[0]["id"].(float64))
	if st, body := sam.call(t, http.MethodPut, fmt.Sprintf("/api/home/screenings/%d/rsvp", sid), map[string]any{"answer": "going"}); st != http.StatusOK {
		t.Fatalf("rsvp: %d %v", st, body)
	}
	samShowtime := int64(sam.hostedBooking(t)["id"].(float64))

	// The lobby: Robin in seat 0; Sam's seat follows once Sam's Usherr says who's coming.
	p := robin.party(t, showtime)
	if p["role"] != "host" || p["canHost"] != true {
		t.Fatalf("host's party: %v", p)
	}
	linkWait(t, "Sam's seat is held", func() bool { return len(partySeats(robin.party(t, showtime))) == 2 })
	seats := partySeats(robin.party(t, showtime))
	if seats[1]["seat"].(float64) != 1 || seats[1]["house"] != "Sam's Usherr" || seats[1]["seated"] != false || seats[0]["seat"].(float64) != 0 || seats[0]["me"] != true || seats[0]["seated"] != false {
		t.Fatalf("host's seats: %v", seats)
	}
	if st, body := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/seat", samShowtime), map[string]any{"seated": true}); st != http.StatusOK {
		t.Fatalf("Sam takes a seat: %d %v", st, body)
	}
	linkWait(t, "Sam seated at the host", func() bool {
		for _, x := range partySeats(robin.party(t, showtime)) {
			if x["seat"].(float64) == 1 && x["house"] == "Sam's Usherr" && x["seated"] == true && x["name"] != "" {
				return true
			}
		}
		return false
	})
	if st, body := robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/seat", showtime), map[string]any{"seated": true}); st != http.StatusOK {
		t.Fatalf("Robin takes a seat: %d %v", st, body)
	}
	linkWait(t, "the seat map at Sam's", func() bool {
		s := partySeats(sam.party(t, samShowtime))
		return len(s) == 2 && s[0]["seated"] == true && s[1]["seated"] == true && s[1]["me"] == true && s[0]["me"] != true
	})

	// The intermission: only the host, only once the film is on.
	path := fmt.Sprintf("/api/home/showtimes/%d/party/intermission", showtime)
	if st, _ := robin.call(t, http.MethodPost, path, map[string]any{"action": "start"}); st != http.StatusConflict {
		t.Fatalf("an intermission before the film: %d", st)
	}
	if st, body := robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/room/playback", showtime), map[string]any{"state": "film", "position": 600}); st != http.StatusOK {
		t.Fatalf("play: %d %v", st, body)
	}
	if st, _ := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/intermission", samShowtime), map[string]any{"action": "start"}); st != http.StatusConflict {
		t.Fatalf("a guest calls an intermission: %d", st)
	}
	if st, _ := robin.call(t, http.MethodPost, path, map[string]any{"action": "nap"}); st != http.StatusBadRequest {
		t.Fatalf("an unknown action: %d", st)
	}
	st, p = robin.call(t, http.MethodPost, path, map[string]any{"action": "start"})
	if st != http.StatusOK || p["intermission"].(map[string]any)["on"] != true {
		t.Fatalf("intermission: %d %v", st, p)
	}
	until := p["intermission"].(map[string]any)["untilMs"].(float64)
	if d := until - float64(time.Now().UnixMilli()); d < 9*60_000 || d > 10*60_000+1000 {
		t.Fatalf("10 minutes: %v", d)
	}
	if s, _, _ := roomState(robin.room(t, showtime)); s != "paused" {
		t.Fatalf("the film isn't paused: %s", s)
	}
	linkWait(t, "the intermission at Sam's", func() bool {
		in := sam.party(t, samShowtime)["intermission"].(map[string]any)
		s, _, _ := roomState(sam.room(t, samShowtime))
		return in["on"] == true && s == "paused"
	})
	if st, _ := sam.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/party/status", samShowtime), map[string]any{"status": "dancing"}); st != http.StatusBadRequest {
		t.Fatalf("a status not offered: %d", st)
	}
	if st, body := sam.call(t, http.MethodPut, fmt.Sprintf("/api/home/showtimes/%d/party/status", samShowtime), map[string]any{"status": "snacks"}); st != http.StatusOK {
		t.Fatalf("Sam's status: %d %v", st, body)
	}
	linkWait(t, "Sam's status everywhere", func() bool {
		for _, u := range []struct {
			u  linkUsherr
			id int64
		}{{robin, showtime}, {sam, samShowtime}} {
			list := u.u.party(t, u.id)["intermission"].(map[string]any)["statuses"].([]any)
			if len(list) != 1 || list[0].(map[string]any)["status"] != "snacks" || list[0].(map[string]any)["house"] != "Sam's Usherr" {
				return false
			}
		}
		return true
	})
	if sam.party(t, samShowtime)["intermission"].(map[string]any)["mine"] != "snacks" {
		t.Fatal("Sam's own status isn't marked")
	}
	_, p = robin.call(t, http.MethodPost, path, map[string]any{"action": "more"})
	if more := p["intermission"].(map[string]any)["untilMs"].(float64); more-until != 5*60_000 {
		t.Fatalf("5 more minutes: %v", more-until)
	}
	_, p = robin.call(t, http.MethodPost, path, map[string]any{"action": "end"})
	if p["intermission"].(map[string]any)["on"] != false {
		t.Fatalf("still on: %v", p)
	}
	linkWait(t, "back on at Sam's", func() bool {
		s, pos, _ := roomState(sam.room(t, samShowtime))
		in := sam.party(t, samShowtime)["intermission"].(map[string]any)
		return s == "film" && pos >= 600 && in["on"] == false && len(in["statuses"].([]any)) == 0
	})

	// The verdict: not before the credits; a guest doesn't roll them.
	vpath := fmt.Sprintf("/api/home/showtimes/%d/party/verdict", samShowtime)
	if st, _ := sam.call(t, http.MethodPost, vpath, map[string]any{"stars": 8}); st != http.StatusConflict {
		t.Fatalf("a verdict mid-film: %d", st)
	}
	if st, _ := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/credits", samShowtime), nil); st != http.StatusForbidden {
		t.Fatalf("a guest rolls the credits: %d", st)
	}
	if st, body := robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/credits", showtime), nil); st != http.StatusOK || body["verdict"].(map[string]any)["open"] != true {
		t.Fatalf("roll credits: %d %v", st, body)
	}
	linkWait(t, "the credits at Sam's", func() bool { return sam.party(t, samShowtime)["verdict"].(map[string]any)["open"] == true })
	if st, _ := sam.call(t, http.MethodPost, vpath, map[string]any{"stars": 11}); st != http.StatusBadRequest {
		t.Fatalf("11 stars: %d", st)
	}
	// Sam shares; Robin doesn't.
	if st, body := sam.call(t, http.MethodPost, vpath, map[string]any{"stars": 9, "line": "Still the best.", "share": true}); st != http.StatusOK {
		t.Fatalf("Sam's verdict: %d %v", st, body)
	}
	if st, body := robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/verdict", showtime), map[string]any{"stars": 6}); st != http.StatusOK {
		t.Fatalf("Robin's verdict: %d %v", st, body)
	}
	for name, u := range map[string]struct {
		u  linkUsherr
		id int64
	}{"Robin": {robin, showtime}, "Sam": {sam, samShowtime}} {
		linkWait(t, name+": the crew's verdict", func() bool {
			v := u.u.party(t, u.id)["verdict"].(map[string]any)
			sh := v["shared"].([]any)
			return v["count"].(float64) == 2 && v["average"].(float64) == 3.8 && len(sh) == 1 && sh[0].(map[string]any)["stars"].(float64) == 9 && sh[0].(map[string]any)["house"] == "Sam's Usherr"
		})
	}
	v := sam.party(t, samShowtime)["verdict"].(map[string]any)
	if mine := v["mine"].(map[string]any); mine["stars"].(float64) != 9 || mine["line"] != "Still the best." || mine["share"] != true {
		t.Fatalf("Sam's own verdict: %v", mine)
	}
	// In each one's own Diary, with the line.
	_, d := sam.call(t, http.MethodGet, "/api/home/diary", nil)
	found := false
	for _, x := range d["entries"].([]any) {
		e := x.(map[string]any)
		if e["tmdbId"].(float64) == 1091 && e["stars"].(float64) == 9 && e["note"] == "Still the best." {
			found = true
		}
	}
	if !found {
		t.Fatalf("not in Sam's Diary: %v", d["entries"])
	}
	_, d = robin.call(t, http.MethodGet, "/api/home/diary", nil)
	found = false
	for _, x := range d["entries"].([]any) {
		if e := x.(map[string]any); e["tmdbId"].(float64) == 1091 && e["stars"].(float64) == 6 {
			found = true
		}
	}
	if !found {
		t.Fatalf("not in Robin's Diary: %v", d["entries"])
	}
}

// A showtime of this Usherr's own has a lobby (whoever booked it in Row A
// Seat 1) and a verdict once its host rolls the credits; no intermission.
func TestLocalPartyLobbyVerdict(t *testing.T) {
	u := newLinkUsherr(t)
	at := time.Now().Add(20 * time.Minute).Truncate(time.Second).UnixMilli()
	st, booked := u.call(t, http.MethodPost, "/api/home/showtimes", map[string]any{
		"tmdbId": 603, "title": "The Matrix", "year": 1999, "runtime": 136, "atMs": at, "preshowMin": 0, "playOn": "none",
	})
	if st != http.StatusCreated {
		t.Fatalf("book: %d %v", st, booked)
	}
	id := int64(booked["id"].(float64))
	p := u.party(t, id)
	if p["role"] != "local" || len(partySeats(p)) != 1 || partySeats(p)[0]["me"] != true {
		t.Fatalf("local party: %v", p)
	}
	_, p = u.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/seat", id), map[string]any{"seated": true})
	if partySeats(p)[0]["seated"] != true || p["lobby"].(map[string]any)["seated"] != true {
		t.Fatalf("seated: %v", p)
	}
	if st, _ := u.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/intermission", id), map[string]any{"action": "start"}); st != http.StatusConflict {
		t.Fatalf("a local intermission: %d", st)
	}
	if st, _ := u.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/verdict", id), map[string]any{"stars": 7}); st != http.StatusConflict {
		t.Fatalf("a verdict before the credits: %d", st)
	}
	if st, _ := u.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/credits", id), nil); st != http.StatusOK {
		t.Fatalf("credits: %d", st)
	}
	st, body := u.call(t, http.MethodPost, fmt.Sprintf("/api/home/showtimes/%d/party/verdict", id), map[string]any{"stars": 7})
	if st != http.StatusOK {
		t.Fatalf("verdict: %d %v", st, body)
	}
	v := body["party"].(map[string]any)["verdict"].(map[string]any)
	if v["count"].(float64) != 1 || v["average"].(float64) != 3.5 || len(v["shared"].([]any)) != 0 {
		t.Fatalf("verdict: %v", v)
	}
}
