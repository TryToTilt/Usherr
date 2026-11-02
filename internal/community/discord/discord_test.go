package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rdborg/mediarium/internal/community"
)

// fakeGateway is a pretend Discord gateway. Each connection gets hello, then
// script runs with what the bot sent first (identify or resume).
type fakeGateway struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	firsts []map[string]any // the first thing the bot sent on each connection
	script func(n int, c *websocket.Conn, first map[string]any)
}

func newFakeGateway(t *testing.T, script func(n int, c *websocket.Conn, first map[string]any)) *fakeGateway {
	g := &fakeGateway{t: t, script: script}
	up := websocket.Upgrader{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteJSON(map[string]any{"op": opHello, "d": map[string]any{"heartbeat_interval": 60000}})
		var first map[string]any
		for {
			if err := c.ReadJSON(&first); err != nil {
				return
			}
			if first["op"] != float64(opHeartbeat) {
				break
			}
		}
		g.mu.Lock()
		g.firsts = append(g.firsts, first)
		n := len(g.firsts)
		g.mu.Unlock()
		g.script(n, c, first)
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGateway) url() string { return "ws" + strings.TrimPrefix(g.srv.URL, "http") }

func (g *fakeGateway) first(i int) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	if i >= len(g.firsts) {
		return nil
	}
	return g.firsts[i]
}

func plainDial(ctx context.Context, url string) (Conn, error) {
	c, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func dispatchFrame(c *websocket.Conn, seq int, kind string, d any) {
	_ = c.WriteJSON(map[string]any{"op": opDispatch, "s": seq, "t": kind, "d": d})
}

func ready(c *websocket.Conn) {
	dispatchFrame(c, 1, "READY", map[string]any{
		"session_id": "sess-1", "resume_gateway_url": "",
		"user":        map[string]any{"id": "42", "username": "usherr", "global_name": "Usherr"},
		"application": map[string]any{"id": "4242"},
	})
	dispatchFrame(c, 2, "GUILD_CREATE", map[string]any{"id": "900", "name": "Movie Club", "icon": "abc"})
}

// hold keeps a connection open until the bot closes it.
func hold(c *websocket.Conn) {
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			return
		}
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A made-up token in a bot token's shape, put together here so it doesn't
// look like a real one to secret scanners (GitHub refuses those).
var testToken = strings.Join([]string{"MTIzNDU2Nzg5MDEyMzQ1Njc4", "GabcDE", "abcdefghijklmnopqrstuvwxyz0123"}, ".")

// The bot identifies with its token and intents, and once Discord says it's
// ready, it shows as connected under its name, with its servers.
func TestTheBotConnectsAndListsItsServers(t *testing.T) {
	var events []string
	var emu sync.Mutex
	g := newFakeGateway(t, func(n int, c *websocket.Conn, first map[string]any) {
		ready(c)
		dispatchFrame(c, 3, "MESSAGE_CREATE", map[string]any{"id": "1"})
		hold(c)
	})
	b := Start(Options{Token: "Bot " + testToken + " ", Gateway: g.url(), Dial: plainDial, OnEvent: func(kind string, _ json.RawMessage) {
		emu.Lock()
		events = append(events, kind)
		emu.Unlock()
	}})
	defer b.Close()
	waitFor(t, "connected", func() bool { st := b.Status(); return st.State == "connected" && len(st.Servers) == 1 })
	st := b.Status()
	if st.Name != "Usherr" || st.AppID != "4242" || st.Servers[0].Name != "Movie Club" || !strings.Contains(st.Servers[0].Icon, "/icons/900/abc.png") {
		t.Errorf("status: %+v", st)
	}
	id := g.first(0)
	d, _ := id["d"].(map[string]any)
	if id["op"] != float64(opIdentify) || d["token"] != testToken {
		t.Fatalf("identify: %v", id)
	}
	if in := int(d["intents"].(float64)); in&intentMessageContent == 0 || in&intentGuildMessages == 0 {
		t.Errorf("intents %b", in)
	}
	waitFor(t, "the message event", func() bool {
		emu.Lock()
		defer emu.Unlock()
		return strings.Contains(strings.Join(events, ","), "MESSAGE_CREATE")
	})
}

// A refused token stops the bot for good, with words that say what to do.
func TestARefusedTokenStopsTheBot(t *testing.T) {
	g := newFakeGateway(t, func(n int, c *websocket.Conn, first map[string]any) {
		_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(4004, "Authentication failed."))
	})
	b := Start(Options{Token: testToken, Gateway: g.url(), Dial: plainDial})
	defer b.Close()
	waitFor(t, "the error", func() bool { return b.Status().State == "error" })
	if !strings.Contains(b.Status().Error, "Reset Token") {
		t.Errorf("error: %q", b.Status().Error)
	}
	time.Sleep(1500 * time.Millisecond)
	if g.first(1) != nil {
		t.Error("the bot tried again with a refused token")
	}
}

// Without Message Content switched on in the Developer Portal, Discord closes
// with 4014: the bot connects again without it, and says what's missing.
func TestWithoutMessageContentTheBotStillConnects(t *testing.T) {
	g := newFakeGateway(t, func(n int, c *websocket.Conn, first map[string]any) {
		if n == 1 {
			_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(4014, "Disallowed intent(s)."))
			return
		}
		ready(c)
		hold(c)
	})
	b := Start(Options{Token: testToken, Gateway: g.url(), Dial: plainDial})
	defer b.Close()
	waitFor(t, "connected", func() bool { return b.Status().State == "connected" })
	d, _ := g.first(1)["d"].(map[string]any)
	if int(d["intents"].(float64))&intentMessageContent != 0 {
		t.Error("asked for Message Content again")
	}
	if w := b.Status().Warnings; len(w) != 1 || !strings.Contains(w[0], "Message Content") {
		t.Errorf("warnings: %v", w)
	}
}

// When Discord asks for a reconnect, the bot resumes its session (keeping the
// events it missed) instead of starting over.
func TestTheBotResumesAfterAReconnect(t *testing.T) {
	g := newFakeGateway(t, func(n int, c *websocket.Conn, first map[string]any) {
		if n == 1 {
			ready(c)
			_ = c.WriteJSON(map[string]any{"op": opReconnect, "d": nil})
			hold(c)
			return
		}
		dispatchFrame(c, 3, "RESUMED", map[string]any{})
		hold(c)
	})
	b := Start(Options{Token: testToken, Gateway: g.url(), Dial: plainDial})
	defer b.Close()
	waitFor(t, "a second connection", func() bool { return g.first(1) != nil })
	r := g.first(1)
	d, _ := r["d"].(map[string]any)
	if r["op"] != float64(opResume) || d["session_id"] != "sess-1" || d["seq"] != float64(2) {
		t.Fatalf("resume: %v", r)
	}
	waitFor(t, "connected", func() bool { return b.Status().State == "connected" })
}

// Posts go to the API with the bot's token, never ping anyone, and become a
// card and buttons.
func TestAPostBecomesACardWithButtons(t *testing.T) {
	var got map[string]any
	var auth string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.Method != "POST" || r.URL.Path != "/channels/77/messages" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"555"}`))
	}))
	defer api.Close()
	b := &Bot{rest: &rest{base: api.URL, token: testToken, client: api.Client()}}
	ref, err := b.Post(context.Background(), "77", community.Post{
		Text: "@everyone movie night",
		Card: &community.Card{Title: "Heat (1995)", URL: "https://www.themoviedb.org/movie/949", Color: 0xD85A30, Thumbnail: "https://image.tmdb.org/p.jpg",
			Fields: []community.Field{{Name: "Feature", Value: "<t:1700000000:F>", Inline: true}}},
		Buttons: []community.Button{{Label: "I'm in", ID: "party:1:in", Style: "success"}, {Label: "TMDB", URL: "https://www.themoviedb.org/movie/949"}, {Label: "skip me"}},
	})
	if err != nil || ref.ID != "555" || ref.Channel != "77" {
		t.Fatalf("post: %v %+v", err, ref)
	}
	if auth != "Bot "+testToken {
		t.Errorf("auth %q", auth)
	}
	if am, _ := got["allowed_mentions"].(map[string]any); am == nil || len(am["parse"].([]any)) != 0 {
		t.Errorf("mentions not blocked: %v", got["allowed_mentions"])
	}
	e := got["embeds"].([]any)[0].(map[string]any)
	if e["title"] != "Heat (1995)" || e["color"] != float64(0xD85A30) || e["thumbnail"].(map[string]any)["url"] == "" {
		t.Errorf("embed: %v", e)
	}
	row := got["components"].([]any)[0].(map[string]any)["components"].([]any)
	if len(row) != 2 || row[0].(map[string]any)["custom_id"] != "party:1:in" || row[0].(map[string]any)["style"] != float64(3) || row[1].(map[string]any)["style"] != float64(5) {
		t.Errorf("buttons: %v", row)
	}
}

// After a 429 the call waits as long as Discord says, then goes through; a
// 401 stops every call after it.
func TestRateLimitsAreWaitedOutAndARefusedTokenStopsCalls(t *testing.T) {
	var calls int
	var mu sync.Mutex
	status := http.StatusTooManyRequests
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"retry_after":0.2,"global":false}`))
			return
		}
		if n == 3 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer api.Close()
	refused := 0
	r := &rest{base: api.URL, token: testToken, client: api.Client(), onRefused: func() { refused++ }}
	start := time.Now()
	if err := r.do(context.Background(), "POST", "/channels/1/messages", map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 200*time.Millisecond {
		t.Error("didn't wait out the 429")
	}
	if err := r.do(context.Background(), "GET", "/x", nil, nil); !errors.Is(err, community.ErrBadToken) {
		t.Fatalf("401: %v", err)
	}
	if err := r.do(context.Background(), "GET", "/x", nil, nil); !errors.Is(err, community.ErrBadToken) || calls != 3 || refused != 1 {
		t.Errorf("called again after a 401: calls %d refused %d err %v", calls, refused, err)
	}
}

func TestRouteKeysAndTokens(t *testing.T) {
	if got := route("PATCH", "/channels/123/messages/456"); got != "PATCH /channels/123/messages/{id}" {
		t.Error(got)
	}
	if !LooksLikeToken(testToken) || LooksLikeToken("hello") || LooksLikeToken("a.b.c") {
		t.Error("token shapes")
	}
	if u := InviteURL("4242"); !strings.Contains(u, "client_id=4242") || !strings.Contains(u, "applications.commands") {
		t.Error(u)
	}
}

// A /link typed in a server reaches OnInteraction with who typed it (their
// server nickname first), and Reply answers privately; commands are
// registered only when Discord's list differs.
func TestCommandsAreRegisteredAndAnswered(t *testing.T) {
	var mu sync.Mutex
	var puts int
	var reply map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == "GET" && r.URL.Path == "/applications/4242/commands":
			if puts == 0 {
				_, _ = w.Write([]byte(`[]`))
			} else {
				_, _ = w.Write([]byte(`[{"name":"link","description":"Link your account"}]`))
			}
		case r.Method == "PUT" && r.URL.Path == "/applications/4242/commands":
			puts++
			_, _ = w.Write([]byte(`[]`))
		case r.Method == "POST" && r.URL.Path == "/interactions/i1/tok/callback":
			_ = json.NewDecoder(r.Body).Decode(&reply)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	got := make(chan community.Interaction, 1)
	g := newFakeGateway(t, func(n int, c *websocket.Conn, first map[string]any) {
		ready(c)
		dispatchFrame(c, 3, "INTERACTION_CREATE", map[string]any{
			"id": "i1", "token": "tok", "type": 2, "guild_id": "900", "channel_id": "77",
			"data":   map[string]any{"name": "link"},
			"member": map[string]any{"nick": "Movie Fan", "user": map[string]any{"id": "555", "username": "fan", "avatar": "av"}},
		})
		hold(c)
	})
	var b *Bot
	b = Start(Options{Token: testToken, Gateway: g.url(), API: api.URL, Client: api.Client(), Dial: plainDial,
		Commands:      []community.Command{{Name: "link", Description: "Link your account"}},
		OnInteraction: func(in community.Interaction) { got <- in },
	})
	defer b.Close()
	var in community.Interaction
	select {
	case in = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("no interaction")
	}
	if in.Kind != "command" || in.Name != "link" || in.User.ID != "555" || in.User.Name != "Movie Fan" || !strings.Contains(in.User.Avatar, "/avatars/555/av.png") {
		t.Fatalf("interaction: %+v", in)
	}
	if err := b.Reply(context.Background(), in, "Your code is 123 456", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the commands", func() bool { mu.Lock(); defer mu.Unlock(); return puts == 1 })
	mu.Lock()
	d, _ := reply["data"].(map[string]any)
	if reply["type"] != float64(4) || d["flags"] != float64(64) || d["content"] != "Your code is 123 456" {
		t.Errorf("reply: %v", reply)
	}
	mu.Unlock()
	// Already registered: nothing is sent again.
	b.syncCommands(context.Background(), "4242")
	mu.Lock()
	if puts != 1 {
		t.Errorf("registered again: %d", puts)
	}
	mu.Unlock()
}
