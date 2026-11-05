package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rdborg/mediarium/internal/community/discord"
)

// A made-up token in a bot token's shape, put together here so it doesn't
// look like a real one to secret scanners (GitHub refuses those).
var communityToken = strings.Join([]string{"MTIzNDU2Nzg5MDEyMzQ1Njc4", "GabcDE", "abcdefghijklmnopqrstuvwxyz0123"}, ".")

// fakeDiscord plays Discord: its gateway at /gateway and the few API calls
// Community makes.
type fakeDiscord struct {
	srv      *httptest.Server
	mu       sync.Mutex
	posted   []map[string]any
	forbid   bool
	identify map[string]any
	conn     *websocket.Conn
	wmu      sync.Mutex
	replies  []map[string]any
	calls    []fakeCall // every API call after connecting
	hostCard string     // a friend's Usherr's party card, under thread 850…2 (TestCommunityJoinedParty)
}

type fakeCall struct {
	Method, Path string
	Body         map[string]any
}

// called lists the calls made to paths starting with prefix.
func (f *fakeDiscord) called(method, prefix string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeCall
	for _, c := range f.calls {
		if c.Method == method && strings.HasPrefix(c.Path, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// interact sends an interaction (a command typed) down the gateway.
func (f *fakeDiscord) interact(t *testing.T, d map[string]any) {
	t.Helper()
	f.mu.Lock()
	c := f.conn
	f.mu.Unlock()
	if c == nil {
		t.Fatal("the bot isn't connected")
	}
	f.wmu.Lock()
	defer f.wmu.Unlock()
	_ = c.WriteJSON(map[string]any{"op": 0, "s": 9, "t": "INTERACTION_CREATE", "d": d})
}

func (f *fakeDiscord) lastReply() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.replies) == 0 {
		return ""
	}
	d, _ := f.replies[len(f.replies)-1]["data"].(map[string]any)
	s, _ := d["content"].(string)
	return s
}

func newFakeDiscord(t *testing.T) *fakeDiscord {
	f := &fakeDiscord{}
	up := websocket.Upgrader{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// A friend's watch party in their server: its open threads, and their cards.
		case r.Method == "GET" && r.URL.Path == "/guilds/900000000000000001/threads/active":
			_, _ = w.Write([]byte(`{"threads":[{"id":"850000000000000001","name":"Other film watch party","parent_id":"700000000000000002"},{"id":"850000000000000002","name":"Heat watch party","parent_id":"700000000000000002"}]}`))
		case r.Method == "GET" && r.URL.Path == "/channels/700000000000000002/messages/850000000000000002":
			f.mu.Lock()
			card := f.hostCard
			f.mu.Unlock()
			_, _ = w.Write([]byte(card))
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/channels/700000000000000002/messages/"):
			_, _ = w.Write([]byte(`{"id":"850000000000000001","channel_id":"700000000000000002","content":"","author":{"id":"77","username":"friendbot","bot":true},"embeds":[{"type":"rich","title":"Other","fields":[{"name":"Showtime code","value":"AAAA-AAAA-AAAAA"}]}]}`))
		case r.Method == "GET" && r.URL.Path == "/channels/850000000000000002/messages":
			_, _ = w.Write([]byte(`[]`))
		// Step 5: the party thread's history, the channel's webhook, and
		// posting through it.
		case r.Method == "GET" && r.URL.Path == "/channels/800000000000000005/messages":
			_, _ = w.Write([]byte(`[{"id":"900000000000000300","channel_id":"800000000000000005","content":"earlier","timestamp":"2026-11-01T20:00:00Z","author":{"id":"42","username":"usherr"}}]`))
		case r.URL.Path == "/channels/700000000000000004/webhooks":
			if r.Method == "GET" {
				_, _ = w.Write([]byte(`[]`))
			} else {
				_, _ = w.Write([]byte(`{"id":"960000000000000001","token":"hooktoken"}`))
			}
		case r.Method == "POST" && r.URL.Path == "/channels/700000000000000009/threads":
			// A film's post in the reviews forum.
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.mu.Lock()
			f.calls = append(f.calls, fakeCall{r.Method, r.URL.Path, m})
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"820000000000000007"}`))
		case strings.HasPrefix(r.URL.Path, "/webhooks/"):
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			m["_query"] = r.URL.RawQuery
			f.mu.Lock()
			f.calls = append(f.calls, fakeCall{r.Method, r.URL.Path, m})
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"900000000000000400"}`))
		case strings.HasPrefix(r.URL.Path, "/gateway"):
			c, err := up.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer c.Close()
			_ = c.WriteJSON(map[string]any{"op": 10, "d": map[string]any{"heartbeat_interval": 60000}})
			var id map[string]any
			if c.ReadJSON(&id) != nil {
				return
			}
			f.mu.Lock()
			f.identify = id
			f.conn = c
			f.mu.Unlock()
			f.wmu.Lock()
			_ = c.WriteJSON(map[string]any{"op": 0, "s": 1, "t": "READY", "d": map[string]any{
				"session_id": "s", "user": map[string]any{"id": "42", "username": "usherr"}, "application": map[string]any{"id": "4242"}}})
			_ = c.WriteJSON(map[string]any{"op": 0, "s": 2, "t": "GUILD_CREATE", "d": map[string]any{"id": "900000000000000001", "name": "Movie Club"}})
			f.wmu.Unlock()
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		case r.Method == "GET" && r.URL.Path == "/guilds/900000000000000001/channels":
			_, _ = w.Write([]byte(`[{"id":"700000000000000002","name":"general","type":0,"position":1},
				{"id":"700000000000000003","name":"Lounge","type":2,"position":2},
				{"id":"700000000000000004","name":"usherr-showtimes","type":0,"position":0},
				{"id":"700000000000000009","name":"usherr-reviews","type":15,"position":3}]`))
		case r.Method == "POST" && r.URL.Path == "/channels/700000000000000004/messages":
			if f.forbid {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"code":50013,"message":"Missing Permissions"}`))
				return
			}
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.mu.Lock()
			f.posted = append(f.posted, m)
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"1"}`))
		case strings.HasPrefix(r.URL.Path, "/channels/") && !(r.Method == "POST" && r.URL.Path == "/channels/700000000000000004/messages" && f.forbid),
			strings.HasPrefix(r.URL.Path, "/guilds/") && strings.Contains(r.URL.Path, "/scheduled-events"):
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.mu.Lock()
			f.calls = append(f.calls, fakeCall{r.Method, r.URL.Path, m})
			if r.URL.Path == "/channels/700000000000000004/messages" && r.Method == "POST" {
				f.posted = append(f.posted, m)
			}
			f.mu.Unlock()
			switch {
			case strings.HasSuffix(r.URL.Path, "/threads"):
				_, _ = w.Write([]byte(`{"id":"800000000000000005"}`))
			case strings.HasSuffix(r.URL.Path, "/scheduled-events"):
				_, _ = w.Write([]byte(`{"id":"810000000000000006"}`))
			case r.Method == "DELETE" || r.Method == "PUT":
				w.WriteHeader(http.StatusNoContent)
			default:
				_, _ = w.Write([]byte(`{"id":"900000000000000123"}`))
			}
		case strings.HasPrefix(r.URL.Path, "/applications/"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/interactions/"):
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.mu.Lock()
			f.replies = append(f.replies, m)
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func plainWebsocket(ctx context.Context, url string) (discord.Conn, error) {
	c, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Community, step 1: paste a bot token and Usherr connects as that bot, lists
// its servers and their channels, and posts a test card where it's told. The
// token never comes back out.
func TestCommunityConnectsTheBotAndPostsATest(t *testing.T) {
	fd := newFakeDiscord(t)
	server, base, admin := loginNewServer(t)
	server.TestSetDiscord(fd.srv.URL, "ws"+strings.TrimPrefix(fd.srv.URL, "http")+"/gateway", plainWebsocket)
	t.Cleanup(func() {
		doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": false})
	})

	st, body := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/community", nil)
	if status := body["status"].(map[string]any); st != http.StatusOK || status["state"] != "off" || body["tokenSet"] != false {
		t.Fatalf("before: %d %v", st, body)
	}
	if st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "token": "not a token"}); st != http.StatusBadRequest {
		t.Fatalf("a bad token: %d %v", st, body)
	}
	if st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true}); st != http.StatusBadRequest {
		t.Fatalf("no token: %d %v", st, body)
	}

	st, body = doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "token": "Bot " + communityToken})
	raw, _ := json.Marshal(body)
	if st != http.StatusOK || strings.Contains(string(raw), communityToken) {
		t.Fatalf("save: %d %s", st, raw)
	}
	status := body["status"].(map[string]any)
	servers, _ := status["servers"].([]any)
	if status["state"] != "connected" || len(servers) != 1 || servers[0].(map[string]any)["name"] != "Movie Club" {
		t.Fatalf("status: %v", status)
	}
	if !strings.Contains(body["inviteUrl"].(string), "client_id=4242") || body["tokenSet"] != true {
		t.Errorf("invite and token: %v", body)
	}
	fd.mu.Lock()
	sent := fd.identify["d"].(map[string]any)["token"]
	fd.mu.Unlock()
	if sent != communityToken {
		t.Errorf("identified with %v", sent)
	}

	st, body = doJSONStatus(t, admin, http.MethodGet, base+"/api/home/community/channels?server=900000000000000001", nil)
	chans, _ := body["channels"].([]any)
	if st != http.StatusOK || len(chans) != 3 || chans[0].(map[string]any)["name"] != "usherr-showtimes" || chans[2].(map[string]any)["kind"] != "forum" {
		t.Fatalf("channels (text and forums, not voice, in Discord's order): %d %v", st, body)
	}

	if st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/community/test", map[string]any{}); st != http.StatusBadRequest {
		t.Fatalf("a test with no channel: %d %v", st, body)
	}
	// Choosing where to post keeps the token and the connection.
	st, body = doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "server": "900000000000000001", "channel": "700000000000000004"})
	if st != http.StatusOK || body["status"].(map[string]any)["state"] != "connected" || body["tokenSet"] != true {
		t.Fatalf("choose the channel: %d %v", st, body)
	}
	st, body = doJSONStatus(t, admin, http.MethodPost, base+"/api/home/community/test", map[string]any{})
	if st != http.StatusOK || body["ok"] != true {
		t.Fatalf("test: %d %v", st, body)
	}
	fd.mu.Lock()
	if len(fd.posted) != 1 || !strings.Contains(fd.posted[0]["embeds"].([]any)[0].(map[string]any)["title"].(string), "is connected") {
		t.Errorf("posted: %v", fd.posted)
	}
	fd.forbid = true
	fd.mu.Unlock()
	st, body = doJSONStatus(t, admin, http.MethodPost, base+"/api/home/community/test", map[string]any{})
	if st != http.StatusOK || body["ok"] != false || !strings.Contains(body["message"].(string), "Send Messages") {
		t.Errorf("a channel the bot may not post in: %d %v", st, body)
	}

	st, body = doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": false})
	if st != http.StatusOK || body["status"].(map[string]any)["state"] != "off" {
		t.Errorf("off: %d %v", st, body)
	}
}

// Step 2: /link in Discord answers privately with a code; typed into Usherr,
// it links that Discord account to the account typing it, once. The
// administrator sees who is linked.
func TestCommunityLinkWithACode(t *testing.T) {
	fd := newFakeDiscord(t)
	server, base, admin := loginNewServer(t)
	server.TestSetDiscord(fd.srv.URL, "ws"+strings.TrimPrefix(fd.srv.URL, "http")+"/gateway", plainWebsocket)
	t.Cleanup(func() {
		doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": false})
	})
	if st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "token": communityToken}); st != http.StatusOK {
		t.Fatalf("connect: %d %v", st, body)
	}
	_, me := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/community/me", nil)
	if me["on"] != true || me["link"] != nil {
		t.Fatalf("before: %v", me)
	}

	link := func() string {
		before := fd.lastReply()
		fd.interact(t, map[string]any{"id": "i1", "token": "tok", "type": 2, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
			"data": map[string]any{"name": "link"}, "member": map[string]any{"user": map[string]any{"id": "555000000000000001", "username": "fan", "global_name": "Movie Fan"}}})
		deadline := time.Now().Add(5 * time.Second)
		for fd.lastReply() == before {
			if time.Now().After(deadline) {
				t.Fatal("no answer to /link")
			}
			time.Sleep(20 * time.Millisecond)
		}
		return fd.lastReply()
	}
	text := link()
	fd.mu.Lock()
	flags := fd.replies[len(fd.replies)-1]["data"].(map[string]any)["flags"]
	fd.mu.Unlock()
	m := regexp.MustCompile(`\*\*(\d{3}) (\d{3})\*\*`).FindStringSubmatch(text)
	if m == nil || flags != float64(64) || !strings.Contains(text, "Linked services") {
		t.Fatalf("the answer: %v %q", flags, text)
	}
	code := m[1] + m[2]

	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/community/link", map[string]any{"code": "000000"}); st != http.StatusBadRequest {
		t.Errorf("a wrong code: %d", st)
	}
	st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/community/link", map[string]any{"code": m[1] + " " + m[2]})
	if l, _ := body["link"].(map[string]any); st != http.StatusOK || l["name"] != "Movie Fan" {
		t.Fatalf("link: %d %v", st, body)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/community/link", map[string]any{"code": code}); st != http.StatusBadRequest {
		t.Errorf("a code used twice: %d", st)
	}
	_, cfg := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/community", nil)
	if l, _ := cfg["linked"].([]any); len(l) != 1 || l[0].(map[string]any)["username"] != "ryan" {
		t.Errorf("linked: %v", cfg["linked"])
	}
	// Asking again says who it's linked to.
	if text := link(); !strings.Contains(text, "linked to **ryan**") {
		t.Errorf("second /link: %q", text)
	}

	if st, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/community/link", nil); st != http.StatusNoContent {
		t.Errorf("unlink: %d", st)
	}
	if _, me := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/community/me", nil); me["link"] != nil {
		t.Errorf("after unlinking: %v", me)
	}
}

// waitCalls waits until at least n calls of a kind were made.
func (f *fakeDiscord) waitCalls(t *testing.T, method, prefix string, n int) []fakeCall {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := f.called(method, prefix)
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited for %d %s %s, got %d", n, method, prefix, len(got))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Step 3: a Showtime shared as a watch party posts its card (times in each
// reader's time zone, I'm in / Can't make it), opens a thread and puts an
// event on the calendar. I'm in is answered privately, adds the person to
// the thread and shows on the card and in Usherr; moving the Showtime moves
// the event and says so in the thread; cancelling it calls the party off.
func TestCommunityWatchParty(t *testing.T) {
	fd := newFakeDiscord(t)
	server, base, admin := loginNewServer(t)
	server.TestSetDiscord(fd.srv.URL, "ws"+strings.TrimPrefix(fd.srv.URL, "http")+"/gateway", plainWebsocket)
	t.Cleanup(func() {
		doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": false})
	})
	if st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "token": communityToken, "server": "900000000000000001", "channel": "700000000000000004"}); st != http.StatusOK {
		t.Fatalf("connect: %d %v", st, body)
	}
	_, list := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/showtimes", nil)
	if list["community"] != true {
		t.Fatalf("sharing isn't offered: %v", list)
	}

	at := time.Now().Add(3 * time.Hour).Truncate(time.Minute)
	st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes", map[string]any{
		"tmdbId": 949, "title": "Heat", "year": 1995, "posterUrl": "https://image.tmdb.org/t/p/w500/heat.jpg", "runtime": 170,
		"atMs": at.UnixMilli(), "preshowMin": 10, "playOn": "none", "share": true})
	if st != http.StatusCreated || body["shareError"] != nil {
		t.Fatalf("book and share: %d %v", st, body)
	}
	id := int64(body["id"].(float64))
	fd.mu.Lock()
	card := fd.posted[len(fd.posted)-1]
	fd.mu.Unlock()
	e := card["embeds"].([]any)[0].(map[string]any)
	raw, _ := json.Marshal(card)
	if e["title"] != "Heat (1995)" || !strings.Contains(string(raw), fmt.Sprintf("t:%d:F", at.Unix())) || !strings.Contains(string(raw), fmt.Sprintf("party:%d:in", id)) {
		t.Fatalf("the card: %s", raw)
	}
	thread := fd.waitCalls(t, "POST", "/channels/700000000000000004/messages/", 1)[0]
	if !strings.HasSuffix(thread.Path, "/threads") || thread.Body["name"] != "Heat watch party" {
		t.Errorf("thread: %+v", thread)
	}
	ev := fd.waitCalls(t, "POST", "/guilds/900000000000000001/scheduled-events", 1)[0]
	if ev.Body["entity_type"] != float64(3) || ev.Body["scheduled_start_time"] != at.Add(-10*time.Minute).UTC().Format(time.RFC3339) {
		t.Errorf("event: %+v", ev.Body)
	}

	// I'm in, from Discord.
	fd.interact(t, map[string]any{"id": "b1", "token": "tok", "type": 3, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
		"data": map[string]any{"custom_id": fmt.Sprintf("party:%d:in", id)}, "member": map[string]any{"user": map[string]any{"id": "555000000000000001", "username": "fan", "global_name": "Movie Fan"}}})
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(fd.lastReply(), "You're in for **Heat**") {
		if time.Now().After(deadline) {
			t.Fatalf("no answer to I'm in: %q", fd.lastReply())
		}
		time.Sleep(20 * time.Millisecond)
	}
	fd.waitCalls(t, "PUT", "/channels/800000000000000005/thread-members/555000000000000001", 1)
	edit := fd.waitCalls(t, "PATCH", "/channels/700000000000000004/messages/1", 1)
	if raw, _ := json.Marshal(edit[len(edit)-1].Body); !strings.Contains(string(raw), "Going (1)") || !strings.Contains(string(raw), "Movie Fan") {
		t.Errorf("the card after I'm in: %s", raw)
	}
	_, list = doJSONStatus(t, admin, http.MethodGet, base+"/api/home/showtimes", nil)
	if p, _ := list["showtimes"].([]any)[0].(map[string]any)["party"].(map[string]any); p == nil || p["going"].([]any)[0] != "Movie Fan" {
		t.Errorf("who's going, in Usherr: %v", list["showtimes"])
	}

	// Moved half an hour later: the event follows, the thread is told.
	later := at.Add(30 * time.Minute)
	if st, body := doJSONStatus(t, admin, http.MethodPut, fmt.Sprintf("%s/api/home/showtimes/%d", base, id), map[string]any{"atMs": later.UnixMilli(), "preshowMin": 10, "playOn": "none"}); st != http.StatusOK {
		t.Fatalf("move: %d %v", st, body)
	}
	moved := fd.waitCalls(t, "PATCH", "/guilds/900000000000000001/scheduled-events/810000000000000006", 1)
	if moved[0].Body["scheduled_start_time"] != later.Add(-10*time.Minute).UTC().Format(time.RFC3339) {
		t.Errorf("the event moved to %v", moved[0].Body["scheduled_start_time"])
	}
	said := fd.waitCalls(t, "POST", "/channels/800000000000000005/messages", 1)
	if !strings.Contains(said[0].Body["content"].(string), "moved") {
		t.Errorf("thread: %v", said[0].Body)
	}

	// Cancelled: the event goes, the card says so, the thread is told.
	if st, _ := doJSONStatus(t, admin, http.MethodDelete, fmt.Sprintf("%s/api/home/showtimes/%d", base, id), nil); st != http.StatusNoContent {
		t.Fatalf("cancel: %d", st)
	}
	fd.waitCalls(t, "DELETE", "/guilds/900000000000000001/scheduled-events/810000000000000006", 1)
	said = fd.waitCalls(t, "POST", "/channels/800000000000000005/messages", 2)
	if !strings.Contains(said[1].Body["content"].(string), "is off") {
		t.Errorf("thread: %v", said[1].Body)
	}
	edits := fd.called("PATCH", "/channels/700000000000000004/messages/")
	if raw, _ := json.Marshal(edits[len(edits)-1].Body); !strings.Contains(string(raw), "called this watch party off") || strings.Contains(string(raw), "party:") {
		t.Errorf("the cancelled card: %s", raw)
	}
}

// dispatch sends any gateway event down to the bot.
func (f *fakeDiscord) dispatch(t *testing.T, kind string, d map[string]any) {
	t.Helper()
	f.mu.Lock()
	c := f.conn
	f.mu.Unlock()
	f.wmu.Lock()
	defer f.wmu.Unlock()
	_ = c.WriteJSON(map[string]any{"op": 0, "s": 10, "t": kind, "d": d})
}

// Step 5: the party's chat in Usherr. It starts with what was said before
// (read from Discord), new messages in the thread arrive live, what you type
// is posted under your linked Discord name in the thread, and a message
// taken back in Discord goes away in Usherr too.
func TestCommunityPartyChat(t *testing.T) {
	fd := newFakeDiscord(t)
	server, base, admin := loginNewServer(t)
	server.TestSetDiscord(fd.srv.URL, "ws"+strings.TrimPrefix(fd.srv.URL, "http")+"/gateway", plainWebsocket)
	t.Cleanup(func() {
		doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": false})
	})
	if st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "token": communityToken, "server": "900000000000000001", "channel": "700000000000000004"}); st != http.StatusOK {
		t.Fatalf("connect: %d %v", st, body)
	}
	st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes", map[string]any{
		"tmdbId": 949, "title": "Heat", "year": 1995, "runtime": 170, "atMs": time.Now().Add(time.Hour).UnixMilli(), "preshowMin": 0, "playOn": "none", "share": true})
	if st != http.StatusCreated {
		t.Fatalf("book: %d %v", st, body)
	}
	chat := fmt.Sprintf("%s/api/home/showtimes/%d/chat", base, int64(body["id"].(float64)))

	_, c := doJSONStatus(t, admin, http.MethodGet, chat, nil)
	msgs, _ := c["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["text"] != "earlier" || msgs[0].(map[string]any)["fromBot"] != true || c["canPost"] != false {
		t.Fatalf("the chat before linking: %v", c)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPost, chat, map[string]any{"text": "hi"}); st != http.StatusForbidden {
		t.Errorf("posting without a linked account: %d", st)
	}

	// Link, the way people do: /link, then the code.
	fd.interact(t, map[string]any{"id": "i1", "token": "tok", "type": 2, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
		"data": map[string]any{"name": "link"}, "member": map[string]any{"user": map[string]any{"id": "555000000000000001", "username": "fan", "global_name": "Movie Fan", "avatar": "av"}}})
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(fd.lastReply(), "Your code") {
		if time.Now().After(deadline) {
			t.Fatal("no code")
		}
		time.Sleep(20 * time.Millisecond)
	}
	m := regexp.MustCompile(`\*\*(\d{3}) (\d{3})\*\*`).FindStringSubmatch(fd.lastReply())
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/community/link", map[string]any{"code": m[1] + m[2]}); st != http.StatusOK {
		t.Fatalf("link: %d", st)
	}

	// The live stream.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, chat+"/events", nil)
	resp, err := admin.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("events: %v %v", err, resp)
	}
	defer resp.Body.Close()
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				lines <- strings.TrimPrefix(sc.Text(), "data: ")
			}
		}
	}()
	next := func(want string) string {
		t.Helper()
		select {
		case l := <-lines:
			if !strings.Contains(l, want) {
				t.Fatalf("pushed %s, want %q in it", l, want)
			}
			return l
		case <-time.After(5 * time.Second):
			t.Fatalf("nothing pushed (wanted %q)", want)
		}
		return ""
	}

	fd.dispatch(t, "MESSAGE_CREATE", map[string]any{"id": "900000000000000301", "channel_id": "800000000000000005", "content": "who brought snacks?",
		"timestamp": time.Now().UTC().Format(time.RFC3339), "author": map[string]any{"id": "777", "username": "pal", "global_name": "Pal"}})
	next("who brought snacks?")
	// Something said in another channel isn't the party's.
	fd.dispatch(t, "MESSAGE_CREATE", map[string]any{"id": "900000000000000302", "channel_id": "123456789012345678", "content": "elsewhere", "author": map[string]any{"id": "777"}})

	st, sent := doJSONStatus(t, admin, http.MethodPost, chat, map[string]any{"text": "  popcorn @everyone  "})
	if st != http.StatusCreated {
		t.Fatalf("send: %d %v", st, sent)
	}
	next("popcorn")
	hook := fd.waitCalls(t, "POST", "/webhooks/960000000000000001/hooktoken", 1)[0]
	if hook.Body["username"] != "Movie Fan" || !strings.Contains(hook.Body["avatar_url"].(string), "/avatars/555000000000000001/av.png") ||
		!strings.Contains(hook.Body["_query"].(string), "thread_id=800000000000000005") || len(hook.Body["allowed_mentions"].(map[string]any)["parse"].([]any)) != 0 {
		t.Errorf("posted as: %v", hook.Body)
	}

	fd.dispatch(t, "MESSAGE_DELETE", map[string]any{"id": "900000000000000301", "channel_id": "800000000000000005"})
	next(`"kind":"delete"`)
	_, c = doJSONStatus(t, admin, http.MethodGet, chat, nil)
	raw, _ := json.Marshal(c["messages"])
	if strings.Contains(string(raw), "snacks") || strings.Contains(string(raw), "elsewhere") || !strings.Contains(string(raw), "popcorn") || c["canPost"] != true {
		t.Errorf("the chat after: %s %v", raw, c["canPost"])
	}
}
