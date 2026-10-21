package api_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A Marquee screen shows up when it asks, takes settings of its own, and gets
// the remote's buttons on its live stream.
func TestMarqueeScreensAndRemote(t *testing.T) {
	_, base, admin := loginNewServer(t)

	// No screens yet: an empty list, not null.
	if got := getJSON[map[string]any](t, admin, base+"/api/home/marquee/screens"); got["screens"] == nil || len(got["screens"].([]any)) != 0 {
		t.Fatalf("no screens: %v", got)
	}

	// The screen opens its stream.
	req, _ := http.NewRequest(http.MethodGet, base+"/api/home/marquee/events?screen=test-screen-1", nil)
	resp, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("stream type %q", ct)
	}
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

	getJSON[map[string]any](t, admin, base+"/api/home/marquee?screen=test-screen-1&w=1440&h=2560")
	screens := getJSON[map[string]any](t, admin, base+"/api/home/marquee/screens")["screens"].([]any)
	if len(screens) != 1 {
		t.Fatalf("screens: %v", screens)
	}
	sc := screens[0].(map[string]any)
	if sc["name"] != "Screen 1" || sc["online"] != true || sc["width"] != float64(1440) || sc["own"] != false {
		t.Fatalf("screen: %v", sc)
	}

	// Its own settings: it is told, and gets them.
	set := sc["settings"].(map[string]any)
	set["textScale"] = 120
	if st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/marquee/screens/test-screen-1", map[string]any{"name": "Hallway", "settings": set}); st != http.StatusOK {
		t.Fatalf("save: %d %v", st, body)
	}
	next("settings")
	got := getJSON[map[string]any](t, admin, base+"/api/home/marquee?screen=test-screen-1")["settings"].(map[string]any)
	if got["textScale"] != float64(120) {
		t.Fatalf("own settings not used: %v", got["textScale"])
	}
	if other := getJSON[map[string]any](t, admin, base+"/api/home/marquee")["settings"].(map[string]any); other["textScale"] != float64(100) {
		t.Fatalf("shared settings changed: %v", other["textScale"])
	}

	// The remote's buttons.
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/screens/test-screen-1/command", map[string]any{"cmd": "pause", "arg": "on"}); st != http.StatusOK {
		t.Fatalf("pause: %d", st)
	}
	if ev := next("command"); ev["cmd"] != "pause" || ev["arg"] != "on" {
		t.Fatalf("pause event: %v", ev)
	}
	for _, bad := range []map[string]any{{"cmd": "explode"}, {"cmd": "dim", "arg": "maybe"}, {"cmd": "scene", "arg": "nope"}} {
		if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/screens/test-screen-1/command", bad); st != http.StatusBadRequest {
			t.Fatalf("%v answered %d", bad, st)
		}
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/screens/another-one/command", map[string]any{"cmd": "next"}); st != http.StatusConflict {
		t.Fatalf("a screen that isn't on answered %d", st)
	}

	// What it shows, for the remote's preview.
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/showing?screen=test-screen-1", map[string]any{"scene": "new", "title": "Heat", "paused": true}); st != http.StatusNoContent {
		t.Fatalf("showing: %d", st)
	}
	sc = getJSON[map[string]any](t, admin, base+"/api/home/marquee/screens")["screens"].([]any)[0].(map[string]any)
	if sc["name"] != "Hallway" || sc["own"] != true || sc["showing"].(map[string]any)["title"] != "Heat" {
		t.Fatalf("after: %v", sc)
	}

	// Back to the shared settings, then forgotten.
	if st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/marquee/screens/test-screen-1", map[string]any{"settings": nil}); st != http.StatusOK {
		t.Fatalf("shared again: %d", st)
	}
	next("settings")
	if st, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/marquee/screens/test-screen-1", nil); st != http.StatusOK {
		t.Fatalf("delete: %d", st)
	}
	if n := len(getJSON[map[string]any](t, admin, base+"/api/home/marquee/screens")["screens"].([]any)); n != 0 {
		t.Fatalf("%d screens after delete", n)
	}
}

// The screens keep the order set on the Screens tab; a new one goes last.
func TestMarqueeScreenOrder(t *testing.T) {
	_, base, admin := loginNewServer(t)
	for _, id := range []string{"screen-a", "screen-b", "screen-c"} {
		getJSON[map[string]any](t, admin, base+"/api/home/marquee?screen="+id)
	}
	ids := func(got map[string]any) string {
		var out []string
		for _, sc := range got["screens"].([]any) {
			out = append(out, sc.(map[string]any)["id"].(string))
		}
		return strings.Join(out, ",")
	}
	if got := ids(getJSON[map[string]any](t, admin, base+"/api/home/marquee/screens")); got != "screen-a,screen-b,screen-c" {
		t.Fatalf("the order they came in: %s", got)
	}
	// Moved: the one named first; the others keep their place after it.
	st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/marquee/screens/order", map[string]any{"order": []string{"screen-c", "nope"}})
	if st != http.StatusOK {
		t.Fatalf("order: %d %v", st, body)
	}
	if got := ids(getJSON[map[string]any](t, admin, base+"/api/home/marquee/screens")); got != "screen-c,screen-a,screen-b" {
		t.Fatalf("after moving: %s", got)
	}
	getJSON[map[string]any](t, admin, base+"/api/home/marquee?screen=screen-d")
	if got := ids(getJSON[map[string]any](t, admin, base+"/api/home/marquee/screens")); got != "screen-c,screen-a,screen-b,screen-d" {
		t.Fatalf("a new screen goes last: %s", got)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/marquee/screens/order", map[string]any{}); st != http.StatusBadRequest {
		t.Fatalf("an empty order: %d", st)
	}
}
