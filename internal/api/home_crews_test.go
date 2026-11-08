package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/link/relay"
)

func (u linkUsherr) crews(t *testing.T) []map[string]any {
	t.Helper()
	_, body := u.call(t, http.MethodGet, "/api/home/crews", nil)
	var out []map[string]any
	for _, x := range body["crews"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

func (u linkUsherr) crewSpace(t *testing.T, id int64) map[string]any {
	t.Helper()
	st, body := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/crews/%d", id), nil)
	if st != http.StatusOK {
		t.Fatalf("crew %d: %d %v", id, st, body)
	}
	return body
}

func crewItems(v any) []map[string]any {
	var out []map[string]any
	list, _ := v.([]any)
	for _, x := range list {
		out = append(out, x.(map[string]any))
	}
	return out
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func (u linkUsherr) onlyCrew(t *testing.T) int64 {
	t.Helper()
	cs := u.crews(t)
	if len(cs) != 1 {
		t.Fatalf("crews: %v", cs)
	}
	return int64(cs[0]["id"].(float64))
}

// Robin makes a crew with Sam and Alex (both linked with Robin, not with
// each other). They join; chat, the shared watchlist, a vote and a film
// club go through Robin's Usherr; the vote's winner is booked in its host's
// Showtime as a linked screening; sharing what you watch is off until you
// turn it on; unlinking takes the crew away.
func TestCrews(t *testing.T) {
	rl := relay.New(relay.Options{})
	ts := httptest.NewServer(rl)
	t.Cleanup(func() { rl.Close(); ts.Close() })
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	robin, sam, alex := newLinkUsherr(t), newLinkUsherr(t), newLinkUsherr(t)
	linkUp(t, url, map[*linkUsherr]string{&robin: "Robin's Usherr", &sam: "Sam's Usherr", &alex: "Alex's Usherr"})
	linkPair(t, &robin, &sam)
	linkPair(t, &robin, &alex)
	samRow, alexRow := peerIDNamed(t, &robin, "Sam's Usherr"), peerIDNamed(t, &robin, "Alex's Usherr")

	if st, _ := robin.call(t, http.MethodPost, "/api/home/crews", map[string]any{"name": "  ", "peers": []int64{samRow}}); st != http.StatusBadRequest {
		t.Fatalf("a crew without a name: %d", st)
	}
	if st, _ := robin.call(t, http.MethodPost, "/api/home/crews", map[string]any{"name": "X", "peers": []int64{99999}}); st != http.StatusBadRequest {
		t.Fatalf("a crew with an Usherr that isn't linked: %d", st)
	}
	st, made := robin.call(t, http.MethodPost, "/api/home/crews", map[string]any{"name": "The Friday Crew", "peers": []int64{samRow, alexRow}})
	if st != http.StatusCreated || made["joined"] != true || made["mine"] != true {
		t.Fatalf("make: %d %v", st, made)
	}
	robinCrew := int64(made["id"].(float64))

	// The invites arrive; nothing of the crew shows before joining.
	linkWait(t, "Sam and Alex are invited", func() bool {
		s, a := sam.crews(t), alex.crews(t)
		return len(s) == 1 && len(a) == 1 && s[0]["state"] == "invited" && a[0]["name"] == "The Friday Crew"
	})
	samCrew, alexCrew := sam.onlyCrew(t), alex.onlyCrew(t)
	if sp := alex.crewSpace(t, alexCrew); sp["chat"] != nil {
		t.Fatalf("the crew shows before joining: %v", sp)
	}
	if st, _ := alex.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/chat", alexCrew), map[string]any{"text": "hi"}); st != http.StatusForbidden {
		t.Fatalf("chat before joining: %d", st)
	}
	for _, x := range []struct {
		u  linkUsherr
		id int64
	}{{sam, samCrew}, {alex, alexCrew}} {
		if st, body := x.u.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/join", x.id), nil); st != http.StatusOK || body["joined"] != true {
			t.Fatalf("join: %d %v", st, body)
		}
	}
	linkWait(t, "Robin sees both houses joined", func() bool {
		hs := crewItems(robin.crewSpace(t, robinCrew)["crew"].(map[string]any)["houses"])
		return len(hs) == 3 && hs[1]["state"] == "joined" && hs[2]["state"] == "joined" && len(hs[1]["people"].([]any)) == 1
	})
	linkWait(t, "Alex hears Sam joined", func() bool {
		hs := crewItems(alex.crewSpace(t, alexCrew)["crew"].(map[string]any)["houses"])
		return len(hs) == 3 && hs[0]["state"] == "joined" && hs[1]["state"] == "joined" && hs[2]["state"] == "joined"
	})

	// Chat: Sam's line reaches Alex through Robin's Usherr; too long is refused.
	if st, _ := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/chat", samCrew), map[string]any{"text": strings.Repeat("x", 501)}); st != http.StatusBadRequest {
		t.Fatalf("a long line: %d", st)
	}
	if st, body := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/chat", samCrew), map[string]any{"text": "<b>Friday?</b>"}); st != http.StatusOK {
		t.Fatalf("chat: %d %v", st, body)
	}
	linkWait(t, "Alex and Robin get Sam's line", func() bool {
		a, r := crewItems(alex.crewSpace(t, alexCrew)["chat"]), crewItems(robin.crewSpace(t, robinCrew)["chat"])
		return len(a) == 1 && len(r) == 1 && a[0]["text"] == "<b>Friday?</b>" && a[0]["house"] == "Sam's Usherr"
	})
	if c := crewItems(sam.crewSpace(t, samCrew)["chat"]); len(c) != 1 {
		t.Fatalf("Sam's own line: %v", c)
	}

	// The shared watchlist: two films, each with who added it and Have it?
	// for every house (yes, getting or no: here nobody has them).
	if st, body := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/list", samCrew), map[string]any{"tmdbId": 603, "title": "The Matrix", "year": 1999, "poster": "https://image.tmdb.org/t/p/w500/m.jpg"}); st != http.StatusOK {
		t.Fatalf("add: %d %v", st, body)
	}
	if st, body := alex.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/list", alexCrew), map[string]any{"tmdbId": 348, "title": "Alien", "year": 1979}); st != http.StatusOK {
		t.Fatalf("add: %d %v", st, body)
	}
	linkWait(t, "everyone has both films on the list", func() bool {
		return len(crewItems(sam.crewSpace(t, samCrew)["list"])) == 2 && len(crewItems(alex.crewSpace(t, alexCrew)["list"])) == 2
	})
	robin.server.CrewJobForTest(time.Now()) // Robin says it has neither
	for _, u := range []linkUsherr{sam, alex} {
		u.server.CrewJobForTest(time.Now())
	}
	linkWait(t, "every house said whether it has them", func() bool {
		for _, e := range crewItems(alex.crewSpace(t, alexCrew)["list"]) {
			if len(e["have"].([]any)) != 3 || e["can"] != "none" {
				return false
			}
		}
		return true
	})
	l := crewItems(robin.crewSpace(t, robinCrew)["list"])
	if l[0]["title"] != "Alien" || l[0]["by"] != "Ryan" || l[1]["poster"] != "/m.jpg" {
		t.Fatalf("list: %v", l)
	}

	// Send to a friend: a card in the chat, for Alex's house.
	if st, body := robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/send", robinCrew), map[string]any{"tmdbId": 348, "title": "Alien", "year": 1979, "to": 3, "toWho": "Ryan"}); st != http.StatusOK {
		t.Fatalf("send: %d %v", st, body)
	}
	if st, _ := robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/send", robinCrew), map[string]any{"tmdbId": 348, "title": "Alien", "to": 9}); st != http.StatusBadRequest {
		t.Fatalf("send to nobody: %d", st)
	}
	linkWait(t, "Alex gets the film", func() bool {
		c := crewItems(alex.crewSpace(t, alexCrew)["chat"])
		return len(c) == 2 && c[1]["card"] != nil && c[1]["card"].(map[string]any)["to"].(float64) == 3
	})

	// A vote, hosted by Sam: one vote each, the winner booked in Sam's
	// Showtime and shared with the houses Sam is linked with (Robin).
	at := time.Now().Add(2 * time.Hour).Truncate(time.Minute).UnixMilli()
	closes := time.Now().Add(20 * time.Minute).UnixMilli()
	if st, _ := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/votes", samCrew), map[string]any{"films": []int{603, 999}, "closesMs": closes, "atMs": at}); st != http.StatusBadRequest {
		t.Fatalf("a film not on the list: %d", st)
	}
	st, opened := sam.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/votes", samCrew), map[string]any{"films": []int{603, 348}, "closesMs": closes, "atMs": at, "preshowMin": 15})
	if st != http.StatusOK {
		t.Fatalf("open: %d %v", st, opened)
	}
	vid := opened["vid"].(string)
	linkWait(t, "the ballot reaches Alex", func() bool {
		v := crewItems(alex.crewSpace(t, alexCrew)["votes"])
		return len(v) == 1 && v[0]["host"] == "Ryan"
	})
	for _, x := range []struct {
		u    linkUsherr
		id   int64
		tmdb int
	}{{alex, alexCrew, 348}, {alex, alexCrew, 603}, {robin, robinCrew, 603}} {
		if st, body := x.u.call(t, http.MethodPut, fmt.Sprintf("/api/home/crews/%d/votes/%s", x.id, vid), map[string]any{"tmdbId": x.tmdb}); st != http.StatusOK {
			t.Fatalf("vote: %d %v", st, body)
		}
	}
	linkWait(t, "Sam sees two votes for The Matrix, Alex's changed", func() bool {
		v := crewItems(sam.crewSpace(t, samCrew)["votes"])
		if len(v) != 1 {
			return false
		}
		ch := crewItems(v[0]["choices"])
		return len(ch) == 2 && len(ch[0]["voters"].([]any)) == 2 && len(ch[1]["voters"].([]any)) == 0
	})
	linkWait(t, "Alex's own vote shows", func() bool {
		v := crewItems(alex.crewSpace(t, alexCrew)["votes"])
		return len(v) == 1 && num(v[0]["myVote"]) == 603
	})
	if err := robin.server.TestExec(`UPDATE crew_votes SET closes_ms = ? WHERE vid = ?`, time.Now().Add(-time.Second).UnixMilli(), vid); err != nil {
		t.Fatal(err)
	}
	robin.server.CrewJobForTest(time.Now())
	linkWait(t, "Sam books the winner and shares it with Robin", func() bool {
		v := crewItems(sam.crewSpace(t, samCrew)["votes"])
		return len(v) == 1 && num(v[0]["winner"]) == 603 && v[0]["state"] == "booked" && num(v[0]["showtimeId"]) > 0 && len(robin.screenings(t)) == 1
	})
	if x := robin.screenings(t)[0]; x["title"] != "The Matrix" || x["atMs"].(float64) != float64(at) || x["preshowMin"].(float64) != 15 {
		t.Fatalf("Robin's screening: %v", x)
	}
	if len(alex.screenings(t)) != 0 {
		t.Fatal("Alex isn't linked with Sam, so isn't sent the screening")
	}

	// A film club: Robin's pick for a night tomorrow is booked ahead.
	tomorrow := time.Now().Add(24 * time.Hour)
	if st, body := robin.call(t, http.MethodPost, fmt.Sprintf("/api/home/crews/%d/clubs", robinCrew), map[string]any{
		"name": "Horror Night", "weekday": int(tomorrow.Weekday()), "minute": tomorrow.Hour()*60 + tomorrow.Minute(), "every": 1, "mode": "pick", "preshowMin": 5}); st != http.StatusOK {
		t.Fatalf("club: %d %v", st, body)
	}
	linkWait(t, "Sam sees the club", func() bool { return len(crewItems(sam.crewSpace(t, samCrew)["clubs"])) == 1 })
	kid := crewItems(sam.crewSpace(t, samCrew)["clubs"])[0]["kid"].(string)
	if st, body := sam.call(t, http.MethodPut, fmt.Sprintf("/api/home/crews/%d/clubs/%s", samCrew, kid), map[string]any{"op": "join"}); st != http.StatusOK {
		t.Fatalf("join club: %d %v", st, body)
	}
	if st, _ := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/crews/%d/clubs/%s", robinCrew, kid), map[string]any{"op": "dance"}); st != http.StatusBadRequest {
		t.Fatalf("an op: %d", st)
	}
	if st, body := robin.call(t, http.MethodPut, fmt.Sprintf("/api/home/crews/%d/clubs/%s", robinCrew, kid), map[string]any{"op": "pick", "tmdbId": 348, "title": "Alien", "year": 1979}); st != http.StatusOK {
		t.Fatalf("pick: %d %v", st, body)
	}
	linkWait(t, "Sam is in the rotation", func() bool {
		k := crewItems(sam.crewSpace(t, samCrew)["clubs"])[0]
		return k["in"] == true && len(k["rotation"].([]any)) == 2 && k["pick"] != nil
	})
	robin.server.CrewJobForTest(time.Now())
	linkWait(t, "the club night is booked and Sam is invited", func() bool {
		k := crewItems(sam.crewSpace(t, samCrew)["clubs"])[0]
		return num(k["nights"]) == 1 && num(k["streak"]) == 1 && k["myTurn"] == true && len(sam.screenings(t)) == 1
	})
	if x := sam.screenings(t)[0]; x["title"] != "Alien" || x["preshowMin"].(float64) != 5 {
		t.Fatalf("Sam's club screening: %v", x)
	}

	// What the crew is watching: off until Sam turns it on; NSFW never goes.
	db := sam.server.TestDB()
	var uid int64
	_ = db.QueryRow(`SELECT id FROM users WHERE is_admin = 1 ORDER BY id LIMIT 1`).Scan(&uid)
	today := time.Now().Format("2006-01-02")
	logged := func(tmdb int, title string, stars int) {
		if _, err := db.Exec(`INSERT INTO home_diary (user_id, kind, tmdb_id, title, watched_on, stars) VALUES (?, 'movie', ?, ?, ?, ?)`, uid, tmdb, title, today, stars); err != nil {
			t.Fatal(err)
		}
	}
	logged(329865, "Arrival", 10) // before sharing is on: never shared
	if _, body := sam.call(t, http.MethodGet, "/api/home/crews", nil); body["sharing"] != false {
		t.Fatalf("sharing is on by default: %v", body)
	}
	sam.server.CrewJobForTest(time.Now())
	if st, _ := sam.call(t, http.MethodPut, "/api/home/crews/sharing", map[string]any{"on": true}); st != http.StatusOK {
		t.Fatalf("sharing: %d", st)
	}
	logged(78, "Blade Runner", 0)
	logged(1, "Something NSFW", 0)
	sam.server.CrewJobForTest(time.Now())
	linkWait(t, "Alex sees what Sam watched", func() bool { return len(crewItems(alex.crewSpace(t, alexCrew)["activity"])) == 1 })
	a := crewItems(alex.crewSpace(t, alexCrew)["activity"])[0]
	if a["title"] != "Blade Runner" || a["kind"] != "finished" || a["house"] != "Sam's Usherr" {
		t.Fatalf("activity: %v", a)
	}

	// Unlinking: Sam's crew goes; Robin's crew loses Sam.
	if st, _ := robin.call(t, http.MethodDelete, fmt.Sprintf("/api/link/peers/%d", samRow), nil); st != http.StatusOK {
		t.Fatal("unlink")
	}
	linkWait(t, "the crew is gone at Sam", func() bool { return len(sam.crews(t)) == 0 })
	linkWait(t, "Alex hears Sam left", func() bool {
		return len(crewItems(alex.crewSpace(t, alexCrew)["crew"].(map[string]any)["houses"])) == 2
	})
}
