package api_test

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/mqtt"
)

// broker plays an MQTT broker for one client: it records what is published
// and can send the client messages.
type broker struct {
	addr string
	mu   sync.Mutex
	got  map[string]string
	subs []string
	conn net.Conn
}

func newBroker(t *testing.T) *broker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	b := &broker{addr: ln.Addr().String(), got: map[string]string{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go b.serve(conn)
		}
	}()
	return b
}

func (b *broker) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	if _, _, err := mqtt.ReadPacket(r); err != nil {
		return
	}
	_, _ = conn.Write([]byte{0x20, 2, 0, 0})
	b.mu.Lock()
	b.conn = conn
	b.mu.Unlock()
	for {
		kind, body, err := mqtt.ReadPacket(r)
		if err != nil {
			return
		}
		switch kind >> 4 {
		case 3:
			n := int(binary.BigEndian.Uint16(body))
			b.mu.Lock()
			b.got[string(body[2:2+n])] = string(body[2+n:])
			b.mu.Unlock()
		case 8:
			b.mu.Lock()
			b.subs = append(b.subs, string(body))
			b.mu.Unlock()
		}
	}
}

func (b *broker) wait(t *testing.T, topic string) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		b.mu.Lock()
		v, ok := b.got[topic]
		b.mu.Unlock()
		if ok {
			return v
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("nothing published to %s", topic)
	return ""
}

func (b *broker) send(topic, payload string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, _ = b.conn.Write(mqtt.Packet(0x30, append(mqtt.AppendString(nil, topic), payload...)))
}

// Home Assistant through MQTT: Usherr connects, each screen appears by
// itself, and what Home Assistant sends reaches the screen. The API names
// screens by name and "all".
func TestMarqueeHomeAssistant(t *testing.T) {
	br := newBroker(t)
	_, base, admin := loginNewServer(t)
	host, port, _ := net.SplitHostPort(br.addr)
	if st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/marquee/mqtt", map[string]any{"on": true, "host": host, "port": mustAtoi(port), "username": "ha", "password": "secret"}); st != http.StatusOK {
		t.Fatalf("save: %d %v", st, body)
	}
	t.Cleanup(func() {
		doJSONStatus(t, admin, http.MethodPut, base+"/api/home/marquee/mqtt", map[string]any{"on": false})
	})
	if v := br.wait(t, "usherr/marquee/status"); v != "online" {
		t.Fatalf("status %q", v)
	}
	got := getJSON[map[string]any](t, admin, base+"/api/home/marquee/mqtt")
	if got["status"].(map[string]any)["state"] != "connected" || got["passwordSet"] != true {
		t.Fatalf("mqtt: %v", got)
	}
	if strings.Contains(strings.ToLower(mustJSON(t, got)), "secret") {
		t.Fatal("the password came back")
	}

	// A screen appears, with its buttons, switches and the rest.
	req, _ := http.NewRequest(http.MethodGet, base+"/api/home/marquee/events?screen=ha-screen-1", nil)
	resp, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	events := make(chan map[string]any, 8)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var ev map[string]any
				_ = json.Unmarshal([]byte(data), &ev)
				events <- ev
			}
		}
	}()
	next := func(want string) map[string]any {
		t.Helper()
		select {
		case ev := <-events:
			if ev["type"] != want {
				t.Fatalf("event %v, want %s", ev, want)
			}
			return ev
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s event", want)
		}
		return nil
	}
	cfg := br.wait(t, "homeassistant/button/usherr_marquee_ha-screen-1/next/config")
	if !strings.Contains(cfg, `"command_topic":"usherr/marquee/ha-screen-1/cmd/next"`) || !strings.Contains(cfg, `"name":"Marquee Screen 1"`) {
		t.Fatalf("button config: %s", cfg)
	}
	br.wait(t, "homeassistant/switch/usherr_marquee_ha-screen-1/dim/config")
	br.wait(t, "homeassistant/select/usherr_marquee_ha-screen-1/look/config")
	if st := br.wait(t, "usherr/marquee/ha-screen-1/state"); !strings.Contains(st, `"online":true`) || !strings.Contains(st, `"look":"usherr"`) {
		t.Fatalf("state: %s", st)
	}

	// Home Assistant presses and switches.
	br.send("usherr/marquee/ha-screen-1/cmd/next", "PRESS")
	if ev := next("command"); ev["cmd"] != "next" {
		t.Fatalf("next: %v", ev)
	}
	br.send("usherr/marquee/ha-screen-1/set/dim", "ON")
	if ev := next("command"); ev["cmd"] != "dim" || ev["arg"] != "on" {
		t.Fatalf("dim: %v", ev)
	}
	br.send("usherr/marquee/ha-screen-1/set/look", "cinema")
	next("settings")
	for i := 0; i < 50 && !strings.Contains(br.wait(t, "usherr/marquee/ha-screen-1/state"), `"look":"cinema"`); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if st := br.wait(t, "usherr/marquee/ha-screen-1/state"); !strings.Contains(st, `"look":"cinema"`) {
		t.Fatalf("look not told: %s", st)
	}

	// What the screen shows goes to Home Assistant.
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/showing?screen=ha-screen-1", map[string]any{"scene": "new", "title": "Heat", "posterUrl": "https://image.tmdb.org/t/p/w780/heat.jpg"}); st != http.StatusNoContent {
		t.Fatalf("showing: %d", st)
	}
	for i := 0; i < 50 && br.wait(t, "usherr/marquee/ha-screen-1/poster") == ""; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if p := br.wait(t, "usherr/marquee/ha-screen-1/poster"); p != "https://image.tmdb.org/t/p/w780/heat.jpg" {
		t.Fatalf("poster: %q", p)
	}

	// The API: by name, and every screen at once.
	if st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/screens/screen%201/command", map[string]any{"cmd": "pause", "arg": "on"}); st != http.StatusOK {
		t.Fatalf("by name: %d %v", st, body)
	}
	next("command")
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/screens/all/command", map[string]any{"cmd": "next"}); st != http.StatusOK {
		t.Fatalf("all: %d", st)
	}
	next("command")
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/screens/all/look", map[string]any{"style": "wall"}); st != http.StatusOK {
		t.Fatalf("look for all: %d", st)
	}
	next("settings")
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/screens/nobody/look", map[string]any{"style": "wall"}); st != http.StatusNotFound {
		t.Fatalf("no such screen answered %d", st)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/screens/all/look", map[string]any{"style": "hologram"}); st != http.StatusBadRequest {
		t.Fatalf("a bad look answered %d", st)
	}
	noFollow := *admin
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	pr, err := noFollow.Get(base + "/api/home/marquee/screens/screen%201/poster")
	if err != nil {
		t.Fatal(err)
	}
	pr.Body.Close()
	if pr.StatusCode != http.StatusFound || pr.Header.Get("Location") != "https://image.tmdb.org/t/p/w780/heat.jpg" {
		t.Fatalf("poster: %d %s", pr.StatusCode, pr.Header.Get("Location"))
	}
}

func mustAtoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
