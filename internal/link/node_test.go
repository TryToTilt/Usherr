package link_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/relay"
)

// goRelay starts the Go relay on this machine and gives its ws:// address.
func goRelay(t *testing.T, opt relay.Options) (string, *relay.Server) {
	t.Helper()
	r := relay.New(opt)
	ts := httptest.NewServer(r)
	t.Cleanup(func() {
		r.Close()
		ts.Close()
	})
	return "ws" + strings.TrimPrefix(ts.URL, "http"), r
}

type testNode struct {
	*link.Node
	store *link.MemStore
	mu    sync.Mutex
	got   []link.Message
}

func (n *testNode) received(typ string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	c := 0
	for _, m := range n.got {
		if m.Type == typ {
			c++
		}
	}
	return c
}

func startNode(t *testing.T, name, relayURL, defaultRelay string) *testNode {
	t.Helper()
	id, err := link.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	st := link.NewMemStore()
	tn := &testNode{store: st}
	tn.Node = link.NewNode(link.NodeOptions{
		Identity: id, Store: st, Relay: relayURL, DefaultRelay: defaultRelay,
		Name: func() string { return name }, Version: "2.52.0",
	})
	tn.OnReceive(func(_ string, m link.Message) {
		tn.mu.Lock()
		tn.got = append(tn.got, m)
		tn.mu.Unlock()
	})
	tn.Start(t.Context())
	t.Cleanup(tn.Close)
	if !tn.WaitConnected(t.Context(), 5*time.Second) {
		t.Fatalf("%s didn't connect to the relay: %+v", name, tn.Status())
	}
	return tn
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited 5 s for: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func linked(t *testing.T, n *testNode, id string) (link.Peer, bool) {
	p, ok, _ := n.store.Peer(t.Context(), id)
	return p, ok
}

func TestLinkTwoUsherrsThroughTheRelay(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		url, rl := goRelay(t, relay.Options{})
		linkFlow(t, url, rl)
	})
	// The same against another relay (the Worker under wrangler dev).
	if u := os.Getenv("ULR_TEST_URL"); u != "" {
		t.Run("ULR_TEST_URL", func(t *testing.T) { linkFlow(t, strings.TrimRight(u, "/"), nil) })
	}
}

// linkFlow links two Usherrs through the relay at url (rl: the Go relay, to
// look inside; nil for another one).
func linkFlow(t *testing.T, url string, rl *relay.Server) {
	boxes := func(n int) func() bool {
		return func() bool { return rl == nil || rl.Stats().Boxes == n }
	}
	host := startNode(t, "Robin's Usherr", url, url)
	joiner := startNode(t, "Sam's Usherr", url, url)
	ctx := t.Context()

	if st := host.Status(); st.SkewMs > 5000 || st.SkewMs < -5000 {
		t.Fatalf("clock skew against a relay on this machine: %d ms", st.SkewMs)
	}
	in, code, err := host.CreateInvite(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(code, "USH-") || in.Role != "host" {
		t.Fatalf("code %q", code)
	}
	eventually(t, "the mailbox is open", boxes(1))

	// The host can't use its own code.
	if _, err := host.Join(ctx, code, time.Second); !errors.Is(err, link.ErrOwnCode) {
		t.Fatalf("own code: %v", err)
	}
	// A typo is caught before anything is sent.
	typo := map[bool]string{true: "Z", false: "Y"}[code[6] != 'Z']
	if _, err := joiner.Join(ctx, code[:6]+typo+code[7:], time.Second); !errors.Is(err, link.ErrCodeTypo) {
		t.Fatalf("typo: %v", err)
	}

	res, err := joiner.Join(ctx, code, 5*time.Second)
	if err != nil || res.State != "linked" || res.Peer.ID != host.Identity().ID() || res.Peer.Name != "Robin's Usherr" {
		t.Fatalf("join: %+v %v", res, err)
	}
	eventually(t, "the host sees the joiner", func() bool {
		p, ok := linked(t, host, joiner.Identity().ID())
		return ok && p.Name == "Sam's Usherr" && bytes.Equal(p.XPub, joiner.Identity().XPublic())
	})
	eventually(t, "both said hello", func() bool { return host.received(link.TypeHello) == 1 && joiner.received(link.TypeHello) == 1 })
	eventually(t, "the mailbox closed", boxes(0))

	// The code worked once.
	other := startNode(t, "Pat's Usherr", url, url)
	if _, err := other.Join(ctx, code, 2*time.Second); !errors.Is(err, link.ErrCodeGone) {
		t.Fatalf("a used code: %v", err)
	}
	if _, err := joiner.Join(ctx, code, 2*time.Second); err == nil {
		t.Fatal("a used code linked again")
	}

	// Pings both ways, each answered.
	if err := host.Ping(ctx, joiner.Identity().ID()); err != nil {
		t.Fatal(err)
	}
	if err := joiner.Ping(ctx, host.Identity().ID()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "pings both ways", func() bool {
		g1, e1 := host.LastPing(joiner.Identity().ID())
		g2, e2 := joiner.LastPing(host.Identity().ID())
		return !g1.IsZero() && !e1.IsZero() && !g2.IsZero() && !e2.IsZero()
	})

	hostID, joinID := host.Identity().ID(), joiner.Identity().ID()
	hostPeer, _ := linked(t, joiner, hostID)
	before := joiner.received(link.TypePing)

	// A tampered envelope is dropped.
	good, err := host.Seal(ctx, joinID, link.Message{Type: link.TypePing, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	bad := bytes.Clone(good)
	bad[len(bad)-3] ^= 0x40
	_ = host.SendRaw(url, joinID, bad)
	eventually(t, "a tampered envelope dropped", func() bool { return joiner.Dropped("open") == 1 })

	// The same envelope twice: the second is a replay.
	_ = host.SendRaw(url, joinID, good)
	_ = host.SendRaw(url, joinID, good)
	eventually(t, "a replay dropped", func() bool { return joiner.Dropped("replay") == 1 })
	if joiner.received(link.TypePing) != before+1 {
		t.Fatalf("pings taken in: %d, want %d", joiner.received(link.TypePing), before+1)
	}

	// Signed by a key that isn't the host's.
	stranger, _ := link.NewIdentity()
	forged, err := link.ForgeMessage(host.Identity(), stranger, joiner.Identity().SignPublic(), joiner.Identity().XPublic(),
		link.Message{Type: link.TypePing, ID: link.NewMessageID(), At: time.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	_ = host.SendRaw(url, joinID, forged)
	eventually(t, "a wrong signature dropped", func() bool { return joiner.Dropped("open") == 2 })

	// Too old, and from the future.
	for _, at := range []time.Time{time.Now().Add(-11 * time.Minute), time.Now().Add(11 * time.Minute)} {
		env, _ := host.Seal(ctx, joinID, link.Message{Type: link.TypePing, ID: link.NewMessageID(), At: at.UnixMilli(), Body: json.RawMessage(`{}`)})
		_ = host.SendRaw(url, joinID, env)
	}
	eventually(t, "stale messages dropped", func() bool { return joiner.Dropped("stale") == 2 })

	// A type nobody registered (yet): dropped before any handler.
	env, _ := host.Seal(ctx, joinID, link.Message{Type: "screening.offer", ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: json.RawMessage(`{"tmdb":603}`)})
	_ = host.SendRaw(url, joinID, env)
	eventually(t, "an unknown type dropped", func() bool { return joiner.Dropped("type") == 1 })
	if joiner.received("screening.offer") != 0 {
		t.Fatal("an unknown type reached the handlers")
	}
	// A later step's type, once registered, gets through.
	var offers sync.WaitGroup
	offers.Add(1)
	if err := joiner.Registry().Register(link.Kind{Type: "screening.offer", MaxBody: 4096, Handle: func(context.Context, link.Incoming) error { offers.Done(); return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := host.Registry().Register(link.Kind{Type: "screening.offer", MaxBody: 4096, Handle: func(context.Context, link.Incoming) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := host.Send(ctx, joinID, "screening.offer", map[string]int{"tmdb": 603}); err != nil {
		t.Fatal(err)
	}
	offers.Wait()

	// Renaming reaches the other side.
	// (The name comes from the options' function; the test node's is fixed,
	// so send the rename body by hand through the registry type.)
	if err := host.Send(ctx, joinID, link.TypeRename, map[string]string{"name": "Robin & Alex"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "renamed", func() bool { p, _ := linked(t, joiner, hostID); return p.Name == "Robin & Alex" })

	// Unlink: the joiner forgets the host, and the host is told.
	if err := joiner.Unlink(ctx, hostID); err != nil {
		t.Fatal(err)
	}
	if _, ok := linked(t, joiner, hostID); ok {
		t.Fatal("still linked after unlinking")
	}
	eventually(t, "the host is told", func() bool { _, ok := linked(t, host, joinID); return !ok })
	if host.store.Forgot[joinID] != "them" || joiner.store.Forgot[hostID] != "me" {
		t.Fatalf("who unlinked: %v %v", host.store.Forgot, joiner.store.Forgot)
	}
	// A message from the unlinked key is dropped.
	stale, err := link.SealMessage(host.Identity(), joiner.Identity().SignPublic(), joiner.Identity().XPublic(),
		link.Message{Type: link.TypePing, ID: link.NewMessageID(), At: time.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	pings := joiner.received(link.TypePing)
	_ = host.SendRaw(url, joinID, stale)
	eventually(t, "a message from an unlinked key dropped", func() bool { return joiner.Dropped("sender") >= 1 })
	if joiner.received(link.TypePing) != pings {
		t.Fatal("a message from an unlinked Usherr was taken in")
	}
	_ = hostPeer
}

func TestLinkInviteRefusals(t *testing.T) {
	url, _ := goRelay(t, relay.Options{})
	host := startNode(t, "Host", url, url)
	ctx := t.Context()

	// Cancelled: the mailbox is closed, so the relay doesn't know it.
	_, code, _ := host.CreateInvite(ctx)
	in, _ := host.store.Invites(ctx)
	if err := host.CancelInvite(ctx, in[0].Box); err != nil {
		t.Fatal(err)
	}
	j1 := startNode(t, "J1", url, url)
	time.Sleep(100 * time.Millisecond)
	if _, err := j1.Join(ctx, code, 2*time.Second); !errors.Is(err, link.ErrCodeGone) {
		t.Fatalf("a cancelled code: %v", err)
	}

	// Expired at the host while the relay still had the mailbox: the host
	// answers no.
	clock := &fakeClock{t: time.Now()}
	id, _ := link.NewIdentity()
	st := link.NewMemStore()
	late := link.NewNode(link.NodeOptions{Identity: id, Store: st, Relay: url, DefaultRelay: url, Now: clock.now})
	late.Start(ctx)
	t.Cleanup(late.Close)
	if !late.WaitConnected(ctx, 5*time.Second) {
		t.Fatal("not connected")
	}
	_, code2, _ := late.CreateInvite(ctx)
	time.Sleep(100 * time.Millisecond)
	clock.add(25 * time.Hour)
	j2 := startNode(t, "J2", url, url)
	if _, err := j2.Join(ctx, code2, 3*time.Second); !errors.As(err, new(link.RefusedError)) || !strings.Contains(err.Error(), "24 hours") {
		t.Fatalf("an expired code: %v", err)
	}

	// The relay in a custom-relay code is used by the joiner even when its
	// own relay is another one.
	url2, _ := goRelay(t, relay.Options{})
	custom := startNode(t, "Custom", url2, link.DefaultRelay)
	_, code3, _ := custom.CreateInvite(ctx)
	if !strings.HasPrefix(code3, "USH@ws://127.0.0.1:") {
		t.Fatalf("a custom relay's code: %q", code3)
	}
	j3 := startNode(t, "J3", url, url)
	res, err := j3.Join(ctx, code3, 5*time.Second)
	if err != nil || res.State != "linked" || res.Peer.Relay != url2 {
		t.Fatalf("join through the code's relay: %+v %v", res, err)
	}
}

// The host is offline when the joiner posts: the relay keeps the request,
// and the link is made when the host comes back.
func TestLinkJoinWhileTheHostIsAway(t *testing.T) {
	url, _ := goRelay(t, relay.Options{})
	ctx := t.Context()
	hid, _ := link.NewIdentity()
	hst := link.NewMemStore()
	host := link.NewNode(link.NodeOptions{Identity: hid, Store: hst, Relay: url, DefaultRelay: url})
	host.Start(ctx)
	if !host.WaitConnected(ctx, 5*time.Second) {
		t.Fatal("not connected")
	}
	_, code, _ := host.CreateInvite(ctx)
	time.Sleep(100 * time.Millisecond)
	host.Close()

	joiner := startNode(t, "Joiner", url, url)
	res, err := joiner.Join(ctx, code, 300*time.Millisecond)
	if err != nil || res.State != "waiting" {
		t.Fatalf("join while away: %+v %v", res, err)
	}
	// The host comes back (same keys, same store).
	host2 := link.NewNode(link.NodeOptions{Identity: hid, Store: hst, Relay: url, DefaultRelay: url})
	host2.Start(ctx)
	t.Cleanup(host2.Close)
	eventually(t, "linked after the host came back", func() bool {
		_, a := linked(t, joiner, hid.ID())
		_, b, _ := hst.Peer(ctx, joiner.Identity().ID())
		return a && b
	})
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}
