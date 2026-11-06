package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/relay"
)

func linkWait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited 8 s for: %s", what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

type linkUsherr struct {
	server *api.Server
	base   string
	admin  *http.Client
}

func (u linkUsherr) call(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	return doJSONStatus(t, u.admin, method, u.base+path, body)
}

func (u linkUsherr) peers(t *testing.T) []map[string]any {
	t.Helper()
	_, body := u.call(t, http.MethodGet, "/api/link/peers", nil)
	var out []map[string]any
	for _, p := range body["peers"].([]any) {
		out = append(out, p.(map[string]any))
	}
	return out
}

func newLinkUsherr(t *testing.T) linkUsherr {
	server, base, admin := loginNewServer(t)
	u := linkUsherr{server, base, admin}
	t.Cleanup(func() { u.call(t, http.MethodPut, "/api/link/settings", map[string]any{"on": false}) })
	return u
}

// Two Usherrs on this machine, the Go relay between them: linked with a
// code, each sees the other, pings go both ways, and after Unlink nothing
// from the other is taken in.
func TestLinkTwoUsherrs(t *testing.T) {
	rl := relay.New(relay.Options{})
	ts := httptest.NewServer(rl)
	t.Cleanup(func() { rl.Close(); ts.Close() })
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	robin, sam := newLinkUsherr(t), newLinkUsherr(t)

	// Off by default: no connection, no codes.
	st, body := robin.call(t, http.MethodGet, "/api/link", nil)
	if st != http.StatusOK || body["settings"].(map[string]any)["on"] != false || body["status"].(map[string]any)["state"] != "off" {
		t.Fatalf("before: %d %v", st, body)
	}
	if fp, _ := body["fingerprint"].(string); len(fp) != 19 || body["relay"] != link.DefaultRelay || !strings.HasPrefix(body["name"].(string), "Ryan’s ") {
		t.Fatalf("identity: %v", body)
	}
	if st, _ := robin.call(t, http.MethodPost, "/api/link/invites", map[string]any{}); st != http.StatusConflict {
		t.Fatalf("a code while off: %d", st)
	}
	if st, _ := robin.call(t, http.MethodPut, "/api/link/settings", map[string]any{"on": true, "relay": "https://relay.example.com"}); st != http.StatusBadRequest {
		t.Fatalf("a relay that isn't ws(s): %d", st)
	}

	for u, name := range map[linkUsherr]string{robin: "Robin's Usherr", sam: "Sam's Usherr"} {
		st, body := u.call(t, http.MethodPut, "/api/link/settings", map[string]any{"on": true, "relay": url + "/", "name": name})
		if st != http.StatusOK || body["status"].(map[string]any)["state"] != "connected" || body["relay"] != url || body["name"] != name {
			t.Fatalf("switch on: %d %v", st, body)
		}
		if body["status"].(map[string]any)["clockWarning"] != false {
			t.Fatalf("a clock warning against a relay on this machine: %v", body["status"])
		}
	}

	st, inv := robin.call(t, http.MethodPost, "/api/link/invites", map[string]any{})
	code, _ := inv["code"].(string)
	if st != http.StatusCreated || !strings.HasPrefix(code, "USH@ws://127.0.0.1:") || inv["expiresAt"].(float64) < float64(time.Now().Add(23*time.Hour).UnixMilli()) {
		t.Fatalf("invite: %d %v", st, inv)
	}
	_, body = robin.call(t, http.MethodGet, "/api/link", nil)
	if list := body["invites"].([]any); len(list) != 1 || list[0].(map[string]any)["code"] != code {
		t.Fatalf("the code waiting: %v", body["invites"])
	}

	// A mistyped code is caught here; the right one links.
	typo := []byte(code)
	last := len(typo) - 3
	typo[last] = map[bool]byte{true: 'A', false: 'B'}[typo[last] != 'A']
	if st, body := sam.call(t, http.MethodPost, "/api/link/join", map[string]any{"code": string(typo)}); st != http.StatusBadRequest || !strings.Contains(body["error"].(string), "typo") {
		t.Fatalf("a typo: %d %v", st, body)
	}
	st, body = sam.call(t, http.MethodPost, "/api/link/join", map[string]any{"code": " " + strings.ToLower(code) + " "})
	if st != http.StatusOK || body["state"] != "linked" || body["peer"].(map[string]any)["name"] != "Robin's Usherr" {
		t.Fatalf("join: %d %v", st, body)
	}
	_, robinMe := robin.call(t, http.MethodGet, "/api/link", nil)
	_, samMe := sam.call(t, http.MethodGet, "/api/link", nil)
	if len(robinMe["invites"].([]any)) != 0 {
		t.Fatalf("the code is still waiting after use: %v", robinMe["invites"])
	}
	linkWait(t, "Robin sees Sam", func() bool {
		p := robin.peers(t)
		return len(p) == 1 && p[0]["name"] == "Sam's Usherr" && p[0]["status"] == "linked" && p[0]["fingerprint"] == samMe["fingerprint"]
	})
	if p := sam.peers(t); len(p) != 1 || p[0]["fingerprint"] != robinMe["fingerprint"] || p[0]["relay"] != url {
		t.Fatalf("Sam's links: %v", p)
	}
	// Used once.
	if st, body := sam.call(t, http.MethodPost, "/api/link/join", map[string]any{"code": code}); st != http.StatusGone {
		t.Fatalf("a used code: %d %v", st, body)
	}

	// link.ping both ways.
	robinID, samID := robinMe["id"].(string), samMe["id"].(string)
	if err := robin.server.TestLinkNode().Ping(t.Context(), samID); err != nil {
		t.Fatal(err)
	}
	if err := sam.server.TestLinkNode().Ping(t.Context(), robinID); err != nil {
		t.Fatal(err)
	}
	linkWait(t, "pings both ways", func() bool {
		g1, e1 := robin.server.TestLinkNode().LastPing(samID)
		g2, e2 := sam.server.TestLinkNode().LastPing(robinID)
		return !g1.IsZero() && !e1.IsZero() && !g2.IsZero() && !e2.IsZero()
	})
	linkWait(t, "last seen", func() bool { return sam.peers(t)[0]["lastSeen"].(float64) > 0 })

	// A local name, and the other Usherr's own rename.
	samPeerID := int64(sam.peers(t)[0]["id"].(float64))
	st, body = sam.call(t, http.MethodPut, fmt.Sprintf("/api/link/peers/%d", samPeerID), map[string]any{"name": "  Robin  "})
	if p := body["peers"].([]any)[0].(map[string]any); st != http.StatusOK || p["name"] != "Robin" || p["theirName"] != "Robin's Usherr" {
		t.Fatalf("rename here: %d %v", st, body)
	}
	robin.call(t, http.MethodPut, "/api/link/settings", map[string]any{"on": true, "relay": url, "name": "Robin & Alex"})
	linkWait(t, "the rename reaches Sam", func() bool { return sam.peers(t)[0]["theirName"] == "Robin & Alex" })

	// A cancelled code doesn't work.
	_, inv = robin.call(t, http.MethodPost, "/api/link/invites", map[string]any{})
	if st, _ := robin.call(t, http.MethodDelete, fmt.Sprintf("/api/link/invites/%d", int64(inv["id"].(float64))), nil); st != http.StatusOK {
		t.Fatalf("cancel: %d", st)
	}
	time.Sleep(100 * time.Millisecond)
	if st, body := sam.call(t, http.MethodPost, "/api/link/join", map[string]any{"code": inv["code"]}); st != http.StatusGone {
		t.Fatalf("a cancelled code: %d %v", st, body)
	}

	// Sam unlinks; Robin is told at once, and a message from Robin's key is
	// dropped at Sam's afterwards.
	samNode, robinNode := sam.server.TestLinkNode(), robin.server.TestLinkNode()
	st, body = sam.call(t, http.MethodDelete, fmt.Sprintf("/api/link/peers/%d", samPeerID), nil)
	if p := body["peers"].([]any)[0].(map[string]any); st != http.StatusOK || body["told"] != true || p["status"] != "unlinked" || p["unlinkedBy"] != "me" || p["fingerprint"] != nil {
		t.Fatalf("unlink: %d %v", st, body)
	}
	linkWait(t, "Robin is told", func() bool {
		p := robin.peers(t)
		return p[0]["status"] == "unlinked" && p[0]["unlinkedBy"] == "them"
	})
	env, err := link.SealMessage(robinNode.Identity(), samNode.Identity().SignPublic(), samNode.Identity().XPublic(),
		link.Message{Type: link.TypePing, ID: link.NewMessageID(), At: time.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	before := samNode.Dropped("sender")
	pinged, _ := samNode.LastPing(robinID)
	if err := robinNode.SendRaw(url, samID, env); err != nil {
		t.Fatal(err)
	}
	linkWait(t, "a message from the unlinked key dropped", func() bool { return samNode.Dropped("sender") > before })
	if g, _ := samNode.LastPing(robinID); !g.Equal(pinged) {
		t.Fatal("a ping from the unlinked Usherr was taken in")
	}
	// Taken off the list.
	if st, body := sam.call(t, http.MethodDelete, fmt.Sprintf("/api/link/peers/%d", samPeerID), nil); st != http.StatusOK || len(body["peers"].([]any)) != 0 {
		t.Fatalf("remove: %d %v", st, body)
	}

	// Switched off: disconnected.
	_, body = sam.call(t, http.MethodPut, "/api/link/settings", map[string]any{"on": false})
	if body["status"].(map[string]any)["state"] != "off" || sam.server.TestLinkNode() != nil {
		t.Fatalf("off: %v", body)
	}
}
