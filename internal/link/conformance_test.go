package link_test

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/relay"
)

// The relay protocol, checked from the outside, the way an Usherr sees it.
// It runs against the Go relay always, and against any other relay (the
// Cloudflare Worker under `wrangler dev`, or the deployed one) when
// ULR_TEST_URL is set, for example ULR_TEST_URL=ws://127.0.0.1:8787.
func TestLinkRelayConformance(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		url, _ := goRelay(t, relay.Options{})
		conformance(t, url)
	})
	if u := os.Getenv("ULR_TEST_URL"); u != "" {
		t.Run("ULR_TEST_URL", func(t *testing.T) { conformance(t, strings.TrimRight(u, "/")) })
	} else {
		t.Log("ULR_TEST_URL is not set: only the Go relay was checked")
	}
}

// rawConn is a hand-driven relay connection.
type rawConn struct {
	t     *testing.T
	ws    *websocket.Conn
	id    *link.Identity
	ready link.Frame
	wait  time.Duration // how long a read waits (10 s when 0)
}

func dialRaw(t *testing.T, url string) (*websocket.Conn, link.Frame) {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(url+"/v1/connect", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { ws.Close() })
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, b, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("no challenge: %v", err)
	}
	f, err := link.DecodeFrame(b)
	if err != nil || f.T != link.FChallenge {
		t.Fatalf("challenge: %s %v", b, err)
	}
	return ws, f
}

func connectRaw(t *testing.T, url string, id *link.Identity) *rawConn {
	t.Helper()
	ws, ch := dialRaw(t, url)
	nonce, _ := base64.StdEncoding.DecodeString(ch.Nonce)
	sig := id.Sign(append([]byte(link.HelloPrefix), nonce...))
	c := &rawConn{t: t, ws: ws, id: id}
	c.write(link.Frame{T: link.FHello, ID: id.ID(), Sig: base64.StdEncoding.EncodeToString(sig), V: 1})
	if f := c.read(); f.T != link.FReady {
		t.Fatalf("after hello: %+v", f)
	} else {
		c.ready = f
	}
	return c
}

func (c *rawConn) write(f link.Frame) {
	c.t.Helper()
	if err := c.ws.WriteMessage(websocket.TextMessage, link.EncodeFrame(f)); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *rawConn) read() link.Frame {
	c.t.Helper()
	wait := c.wait
	if wait == 0 {
		wait = 10 * time.Second
	}
	_ = c.ws.SetReadDeadline(time.Now().Add(wait))
	_, b, err := c.ws.ReadMessage()
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	f, err := link.DecodeFrame(b)
	if err != nil {
		c.t.Fatalf("frame %s: %v", b, err)
	}
	return f
}

// until reads frames until one matches.
func (c *rawConn) until(match func(link.Frame) bool) link.Frame {
	c.t.Helper()
	for range 500 {
		if f := c.read(); match(f) {
			return f
		}
	}
	c.t.Fatal("no matching frame")
	return link.Frame{}
}

func randEnv(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func newIdentity(t *testing.T) *link.Identity {
	id, err := link.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func conformance(t *testing.T, url string) {
	t.Run("challenge", func(t *testing.T) {
		_, ch := dialRaw(t, url)
		nonce, err := base64.StdEncoding.DecodeString(ch.Nonce)
		if err != nil || len(nonce) != 32 {
			t.Fatalf("nonce: %q", ch.Nonce)
		}
		if d := time.Since(time.UnixMilli(ch.Time)); d > time.Minute || d < -time.Minute {
			t.Fatalf("the relay's time is %v off", d)
		}
	})

	t.Run("a wrong signature is shut out", func(t *testing.T) {
		ws, ch := dialRaw(t, url)
		a, b := newIdentity(t), newIdentity(t)
		nonce, _ := base64.StdEncoding.DecodeString(ch.Nonce)
		sig := b.Sign(append([]byte(link.HelloPrefix), nonce...))
		_ = ws.WriteMessage(websocket.TextMessage, link.EncodeFrame(link.Frame{T: link.FHello, ID: a.ID(), Sig: base64.StdEncoding.EncodeToString(sig), V: 1}))
		_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return // closed: right
			}
			if f, _ := link.DecodeFrame(msg); f.T == link.FReady {
				t.Fatal("let in with someone else's signature")
			}
		}
	})

	t.Run("send, ack and deliver", func(t *testing.T) {
		a, b := newIdentity(t), newIdentity(t)
		ca, cb := connectRaw(t, url, a), connectRaw(t, url, b)
		env := randEnv(100)
		ca.write(link.Frame{T: link.FSend, To: b.ID(), Env: env, Mid: "m1"})
		if f := ca.until(func(f link.Frame) bool { return f.Mid == "m1" }); f.T != link.FAck {
			t.Fatalf("answer: %+v", f)
		}
		f := cb.until(func(f link.Frame) bool { return f.T == link.FMsg })
		if f.From != a.ID() || f.Env != env || f.Mid != "m1" {
			t.Fatalf("delivered: %+v", f)
		}
		// Ping, pong.
		ca.write(link.Frame{T: link.FPing})
		ca.until(func(f link.Frame) bool { return f.T == link.FPong })
	})

	t.Run("queued for an offline Usherr", func(t *testing.T) {
		a, b := newIdentity(t), newIdentity(t)
		ca := connectRaw(t, url, a)
		for i, mid := range []string{"q1", "q2"} {
			ca.write(link.Frame{T: link.FSend, To: b.ID(), Env: randEnv(50 + i), Mid: mid})
			if f := ca.until(func(f link.Frame) bool { return f.Mid == mid }); f.T != link.FAck {
				t.Fatalf("queue: %+v", f)
			}
		}
		cb := connectRaw(t, url, b)
		for _, mid := range []string{"q1", "q2"} {
			if f := cb.until(func(f link.Frame) bool { return f.T == link.FMsg }); f.Mid != mid || f.From != a.ID() {
				t.Fatalf("queued message: %+v", f)
			}
		}
		// Delivered once: a second connection gets nothing more.
		cb2 := connectRaw(t, url, b)
		ca.write(link.Frame{T: link.FSend, To: b.ID(), Env: randEnv(10), Mid: "q3"})
		if f := cb2.until(func(f link.Frame) bool { return f.T == link.FMsg }); f.Mid != "q3" {
			t.Fatalf("an old message came again: %+v", f)
		}
	})

	t.Run("envelopes over 16 KB are refused", func(t *testing.T) {
		a, b := newIdentity(t), newIdentity(t)
		ca := connectRaw(t, url, a)
		ca.write(link.Frame{T: link.FSend, To: b.ID(), Env: randEnv(link.MaxEnvelope + 1), Mid: "big"})
		if f := ca.until(func(f link.Frame) bool { return f.Mid == "big" }); f.T != link.FErr || f.Code != link.CodeSize {
			t.Fatalf("a big envelope: %+v", f)
		}
		ca.write(link.Frame{T: link.FSend, To: b.ID(), Env: randEnv(link.MaxEnvelope), Mid: "max"})
		if f := ca.until(func(f link.Frame) bool { return f.Mid == "max" }); f.T != link.FAck {
			t.Fatalf("an envelope of exactly 16 KB: %+v", f)
		}
	})

	t.Run("rate limit per sender", func(t *testing.T) {
		a, b := newIdentity(t), newIdentity(t)
		ca := connectRaw(t, url, a)
		connectRaw(t, url, b) // online, so nothing fills a queue
		const n = 90
		for i := range n {
			ca.write(link.Frame{T: link.FSend, To: b.ID(), Env: randEnv(16), Mid: "r" + strconv.Itoa(i)})
		}
		limited := 0
		for range n {
			f := ca.until(func(f link.Frame) bool { return f.T == link.FAck || f.T == link.FErr })
			if f.T == link.FErr && f.Code == link.CodeRate {
				limited++
			}
		}
		if limited == 0 {
			t.Fatal("90 messages at once weren't rate limited (burst 60)")
		}
	})

	t.Run("invite mailbox", func(t *testing.T) {
		host, joiner := newIdentity(t), newIdentity(t)
		ch := connectRaw(t, url, host)
		cj := connectRaw(t, url, joiner)
		box := link.BoxOf(link.NewSecret())
		// Nobody opened it yet.
		cj.write(link.Frame{T: link.FInvitePost, Box: box, Env: randEnv(40), Mid: "p0"})
		if f := cj.until(func(f link.Frame) bool { return f.Mid == "p0" }); f.T != link.FErr || f.Code != link.CodeUnknown {
			t.Fatalf("an unknown mailbox: %+v", f)
		}
		ch.write(link.Frame{T: link.FInviteOpen, Box: box, Mid: "o1"})
		ch.until(func(f link.Frame) bool { return f.Mid == "o1" && f.T == link.FAck })
		env := randEnv(40)
		cj.write(link.Frame{T: link.FInvitePost, Box: box, Env: env, Mid: "p1"})
		if f := cj.until(func(f link.Frame) bool { return f.Mid == "p1" }); f.T != link.FAck {
			t.Fatalf("post: %+v", f)
		}
		f := ch.until(func(f link.Frame) bool { return f.T == link.FInviteMsg })
		if f.Box != box || f.From != joiner.ID() || f.Env != env {
			t.Fatalf("invite.msg: %+v", f)
		}
		// Only its owner closes it; then it's unknown.
		cj.write(link.Frame{T: link.FInviteClose, Box: box, Mid: "c0"})
		cj.until(func(f link.Frame) bool { return f.Mid == "c0" })
		ch.write(link.Frame{T: link.FInviteClose, Box: box, Mid: "c1"})
		ch.until(func(f link.Frame) bool { return f.Mid == "c1" })
		cj.write(link.Frame{T: link.FInvitePost, Box: box, Env: env, Mid: "p2"})
		if f := cj.until(func(f link.Frame) bool { return f.Mid == "p2" }); f.T != link.FErr || f.Code != link.CodeUnknown {
			t.Fatalf("a closed mailbox: %+v", f)
		}
		// invite.post is rate limited per connection.
		limited := false
		for i := range 12 {
			mid := "pp" + strconv.Itoa(i)
			cj.write(link.Frame{T: link.FInvitePost, Box: box, Env: env, Mid: mid})
			if f := cj.until(func(f link.Frame) bool { return f.Mid == mid }); f.Code == link.CodeRate {
				limited = true
				break
			}
		}
		if !limited {
			t.Fatal("12 invite.posts in a row weren't rate limited")
		}
	})

	t.Run("fanout", func(t *testing.T) {
		host := newIdentity(t)
		ch := connectRaw(t, url, host)
		if ch.ready.V != link.RelayLevel {
			t.Fatalf("ready doesn't say the relay knows fanout: %+v", ch.ready)
		}
		g1, g2, away := newIdentity(t), newIdentity(t), newIdentity(t)
		c1, c2 := connectRaw(t, url, g1), connectRaw(t, url, g2)
		env := randEnv(300)
		ch.write(link.Frame{T: link.FFanout, Dests: []string{g1.ID(), g2.ID(), away.ID()}, Env: env, Mid: "f1"})
		if f := ch.until(func(f link.Frame) bool { return f.Mid == "f1" }); f.T != link.FAck {
			t.Fatalf("fanout answer: %+v", f)
		}
		// Each gets an ordinary msg frame (a client that knows nothing of
		// fanout reads it as it always did); the one offline gets it queued.
		for _, c := range []*rawConn{c1, c2} {
			if f := c.until(func(f link.Frame) bool { return f.T == link.FMsg }); f.From != host.ID() || f.Env != env || f.Mid != "f1" {
				t.Fatalf("fanned out: %+v", f)
			}
		}
		ca := connectRaw(t, url, away)
		if ca.ready.Queued != 1 {
			t.Fatalf("queued for the one offline: %+v", ca.ready)
		}
		if f := ca.until(func(f link.Frame) bool { return f.T == link.FMsg }); f.From != host.ID() || f.Env != env {
			t.Fatalf("queued fanout: %+v", f)
		}

		// A list that's too long, empty, has someone twice or isn't
		// identities is refused; so is a big envelope.
		many := func(n int) []string {
			out := make([]string, n)
			for i := range out {
				out[i] = newIdentity(t).ID()
			}
			return out
		}
		for mid, c := range map[string]struct {
			dests []string
			env   string
			code  string
		}{
			"long":  {many(link.MaxFanout + 1), env, link.CodeSize},
			"empty": {nil, env, link.CodeBad},
			"twice": {[]string{g1.ID(), g1.ID()}, env, link.CodeBad},
			"notid": {[]string{g1.ID(), "nope"}, env, link.CodeBad},
			"big":   {[]string{g1.ID()}, randEnv(link.MaxEnvelope + 1), link.CodeSize},
		} {
			ch.write(link.Frame{T: link.FFanout, Dests: c.dests, Env: c.env, Mid: mid})
			if f := ch.until(func(f link.Frame) bool { return f.Mid == mid }); f.T != link.FErr || f.Code != c.code {
				t.Fatalf("%s: %+v, want err %s", mid, f, c.code)
			}
		}

		// A screening of 60 Usherrs is one frame; each one gets it. (Under
		// wrangler dev, making 58 new inboxes at once takes a while.)
		for _, c := range []*rawConn{ch, c1, c2} {
			c.wait = 2 * time.Minute
		}
		sixty := append(many(58), g1.ID(), g2.ID())
		env2 := randEnv(1000)
		ch.write(link.Frame{T: link.FFanout, Dests: sixty, Env: env2, Mid: "f60"})
		if f := ch.until(func(f link.Frame) bool { return f.Mid == "f60" }); f.T != link.FAck {
			t.Fatalf("a fanout to 60: %+v", f)
		}
		for _, c := range []*rawConn{c1, c2} {
			if f := c.until(func(f link.Frame) bool { return f.T == link.FMsg && f.Mid == "f60" }); f.Env != env2 {
				t.Fatalf("fanned out to 60: %+v", f)
			}
		}

		// The rate limit counts a fanout as one for every ten recipients:
		// after 65 quick sends (burst 60), one more send may still get
		// through as tokens come back, but a fanout to 100 (ten) doesn't.
		other := newIdentity(t)
		co := connectRaw(t, url, other)
		co.wait = 2 * time.Minute
		const n = 65
		for i := range n {
			co.write(link.Frame{T: link.FSend, To: g1.ID(), Env: randEnv(16), Mid: "fs" + strconv.Itoa(i)})
		}
		co.write(link.Frame{T: link.FFanout, Dests: append(sixty[:58:58], many(link.MaxFanout-58)...), Env: randEnv(16), Mid: "fr"})
		var fr link.Frame
		for got := 0; got < n || fr.T == ""; {
			f := co.until(func(f link.Frame) bool { return f.T == link.FAck || f.T == link.FErr })
			if f.Mid == "fr" {
				fr = f
			} else {
				got++
			}
		}
		if fr.T != link.FErr || fr.Code != link.CodeRate {
			t.Fatalf("a fanout to 100 right after the burst: %+v", fr)
		}
	})

	t.Run("at most three connections per key", func(t *testing.T) {
		a := newIdentity(t)
		for range 3 {
			connectRaw(t, url, a)
		}
		ws, ch := dialRaw(t, url)
		nonce, _ := base64.StdEncoding.DecodeString(ch.Nonce)
		sig := a.Sign(append([]byte(link.HelloPrefix), nonce...))
		_ = ws.WriteMessage(websocket.TextMessage, link.EncodeFrame(link.Frame{T: link.FHello, ID: a.ID(), Sig: base64.StdEncoding.EncodeToString(sig), V: 1}))
		_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if f, _ := link.DecodeFrame(msg); f.T == link.FReady {
				t.Fatal("a fourth connection was let in")
			}
		}
	})
}
