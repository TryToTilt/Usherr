package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

// A friend's party joined by its code, when this Usherr's bot is in the
// friend's server: it's found by the code on its card, shows who hosts and
// who's going, its chat is the friend's thread, the friend's bot saying you
// stayed to the end stamps your stub here, and you can remove the stub (and
// bring it back).
func TestCommunityJoinedParty(t *testing.T) {
	fd := newFakeDiscord(t)
	server, base, admin := loginNewServer(t)
	server.TestSetDiscord(fd.srv.URL, "ws"+strings.TrimPrefix(fd.srv.URL, "http")+"/gateway", plainWebsocket)
	t.Cleanup(func() {
		doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": false})
	})
	if st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "token": communityToken, "server": "900000000000000001", "channel": "700000000000000004"}); st != http.StatusOK {
		t.Fatalf("connect: %d %v", st, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, a := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/community", nil)
		if s, _ := a["status"].(map[string]any); s != nil && s["state"] == "connected" && len(s["servers"].([]any)) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the bot didn't connect: %v", a)
		}
		time.Sleep(20 * time.Millisecond)
	}

	at := time.Now().Add(2 * time.Hour).Truncate(time.Minute)
	st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes", map[string]any{
		"tmdbId": 949, "title": "Heat", "year": 1995, "runtime": 170, "atMs": at.UnixMilli(), "preshowMin": 0, "playOn": "none"})
	if st != http.StatusCreated {
		t.Fatalf("book: %d %v", st, body)
	}
	id := int64(body["id"].(float64))
	db := server.TestDB()
	if _, err := db.Exec(`INSERT INTO community_joined (showtime_id, code, joined_ms) VALUES (?, 'x', 1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO community_links (user_id, backend, external_id, name, linked_at) VALUES (1, 'discord', '555000000000000002', 'Alex', 'now')`); err != nil {
		t.Fatal(err)
	}

	// Not found yet: the page says what it takes.
	_, list := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/showtimes", nil)
	if p := partyOf(list, id); p == nil || p["waiting"] != true || p["bot"] != "on" || !strings.Contains(fmt.Sprint(p["invite"]), "client_id=4242") {
		t.Fatalf("waiting party: %v", p)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPost, fmt.Sprintf("%s/api/home/showtimes/%d/share", base, id), nil); st != http.StatusBadRequest {
		t.Errorf("a joined party could be shared again: %d", st)
	}

	// The friend's card, by their bot, with this showtime's code.
	code := api.ShowtimeCodeForTest(949, at.UnixMilli(), 0)
	fd.mu.Lock()
	fd.hostCard = `{"id":"850000000000000002","channel_id":"700000000000000002","content":"","author":{"id":"77","username":"friendbot","bot":true},
		"embeds":[{"type":"rich","title":"Heat (1995)","description":"**Robin** is hosting a watch party.","fields":[
		{"name":"Feature","value":"x"},{"name":"Showtime code","value":"` + code + ` In your own Usherr: Showtime, Join a watch party."},
		{"name":"Going (3)","value":"Sam, Alex, Jo"}]}]}`
	fd.mu.Unlock()
	if !server.TestFindJoined(id) {
		t.Fatal("the party wasn't found by its code")
	}
	_, list = doJSONStatus(t, admin, http.MethodGet, base+"/api/home/showtimes", nil)
	if p := partyOf(list, id); p == nil || p["waiting"] == true || p["host"] != "Robin" || fmt.Sprint(p["going"]) != "[Sam Alex Jo]" {
		t.Fatalf("found party: %v", p)
	}
	if st, chat := doJSONStatus(t, admin, http.MethodGet, fmt.Sprintf("%s/api/home/showtimes/%d/chat", base, id), nil); st != http.StatusOK || chat["canPost"] != true ||
		chat["discord"] != "https://discord.com/channels/900000000000000001/850000000000000002" {
		t.Fatalf("chat: %d %v", st, chat)
	}

	// The film starts: the stub, with who hosted and who else came.
	if _, err := db.Exec(`UPDATE showtimes SET state = 'playing' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO community_stubs (backend, external_id, showtime_id, tmdb_id, title, seat, at_ms) VALUES ('here', '1', ?, 949, 'Heat', 1, ?)`, id, at.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	// A person typing the same words doesn't stamp it; the friend's bot does.
	fd.dispatch(t, "MESSAGE_CREATE", map[string]any{"id": "990000000000000001", "channel_id": "850000000000000002", "content": "🎟️ <@555000000000000002> stayed to the end: stub stamped.",
		"author": map[string]any{"id": "31", "username": "joker"}})
	time.Sleep(200 * time.Millisecond)
	if s := myStub(t, admin, base, id); s["stamped"] == true {
		t.Fatal("a person's message stamped the stub")
	}
	fd.dispatch(t, "MESSAGE_CREATE", map[string]any{"id": "990000000000000002", "channel_id": "850000000000000002", "content": "🎟️ <@555000000000000002> stayed to the end: stub stamped.",
		"author": map[string]any{"id": "77", "username": "friendbot", "bot": true}})
	deadline = time.Now().Add(5 * time.Second)
	for {
		s := myStub(t, admin, base, id)
		if s["stamped"] == true {
			if s["host"] != "Robin" || s["seat"] != float64(2) || fmt.Sprint(s["guests"]) != "[Robin Sam Jo]" {
				t.Errorf("stub: %v", s)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the friend's stamp didn't reach the stub: %v", s)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Remove it, then bring it back.
	if st, _ := doJSONStatus(t, admin, http.MethodDelete, fmt.Sprintf("%s/api/home/stubs/here/%d", base, id), nil); st != http.StatusNoContent {
		t.Fatalf("remove: %d", st)
	}
	if s := myStub(t, admin, base, id); s != nil {
		t.Fatalf("a removed stub still shows: %v", s)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodDelete, fmt.Sprintf("%s/api/home/stubs/discord/%d", base, id), nil); st != http.StatusNotFound {
		t.Errorf("removing a stub that isn't there: %d", st)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPost, fmt.Sprintf("%s/api/home/stubs/here/%d/restore", base, id), nil); st != http.StatusNoContent {
		t.Fatalf("restore: %d", st)
	}
	if s := myStub(t, admin, base, id); s == nil || s["stamped"] != true {
		t.Fatalf("the stub didn't come back: %v", s)
	}
}

func partyOf(list map[string]any, id int64) map[string]any {
	for _, x := range list["showtimes"].([]any) {
		s := x.(map[string]any)
		if s["id"] == float64(id) {
			p, _ := s["party"].(map[string]any)
			return p
		}
	}
	return nil
}

func myStub(t *testing.T, c *http.Client, base string, id int64) map[string]any {
	t.Helper()
	_, d := doJSONStatus(t, c, http.MethodGet, base+"/api/home/stubs", nil)
	list, _ := d["stubs"].([]any)
	for _, x := range list {
		s := x.(map[string]any)
		if s["showtimeId"] == float64(id) && s["here"] == true {
			return s
		}
	}
	return nil
}
