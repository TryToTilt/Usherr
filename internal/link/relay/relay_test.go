package relay_test

import (
	"crypto/rand"
	"encoding/base64"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/relay"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func start(t *testing.T, opt relay.Options) (string, *relay.Server) {
	t.Helper()
	r := relay.New(opt)
	ts := httptest.NewServer(r)
	t.Cleanup(func() {
		r.Close()
		ts.Close()
	})
	return "ws" + strings.TrimPrefix(ts.URL, "http"), r
}

func connect(t *testing.T, url string, id *link.Identity) *websocket.Conn {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(url+"/v1/connect", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	ch := read(t, ws)
	nonce, _ := base64.StdEncoding.DecodeString(ch.Nonce)
	write(t, ws, link.Frame{T: link.FHello, ID: id.ID(), Sig: base64.StdEncoding.EncodeToString(id.Sign(append([]byte(link.HelloPrefix), nonce...))), V: 1})
	if f := read(t, ws); f.T != link.FReady {
		t.Fatalf("not ready: %+v", f)
	}
	return ws
}

func read(t *testing.T, ws *websocket.Conn) link.Frame {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, b, err := ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	f, err := link.DecodeFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func write(t *testing.T, ws *websocket.Conn, f link.Frame) {
	t.Helper()
	if err := ws.WriteMessage(websocket.TextMessage, link.EncodeFrame(f)); err != nil {
		t.Fatal(err)
	}
}

func env(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func ident(t *testing.T) *link.Identity {
	id, err := link.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// The queue for an Usherr that's away: at most 200 messages and 1 MB, kept
// 24 hours, and nothing once it's handed over.
func TestLinkRelayQueueLimits(t *testing.T) {
	c := &clock{t: time.Now()}
	url, r := start(t, relay.Options{Now: c.now, Rate: 1000, Burst: 1000})
	a, b := ident(t), ident(t)
	ws := connect(t, url, a)
	codes := map[string]int{}
	for i := range 210 {
		write(t, ws, link.Frame{T: link.FSend, To: b.ID(), Env: env(32), Mid: "m" + strconv.Itoa(i)})
		f := read(t, ws)
		codes[f.T+f.Code]++
	}
	if codes["ack"] != 200 || codes["errfull"] != 10 {
		t.Fatalf("answers: %v", codes)
	}
	if st := r.Stats(); st.Queued != 200 {
		t.Fatalf("queued %d", st.Queued)
	}
	// A day later they're gone, and the queue takes new ones.
	c.add(24*time.Hour + time.Second)
	r.Sweep()
	if st := r.Stats(); st.Queued != 0 || st.QueuedB != 0 {
		t.Fatalf("after 24 h: %+v", st)
	}
	// 1 MB: 16 KB envelopes fill it well before 200 messages.
	n := 0
	for i := range 100 {
		write(t, ws, link.Frame{T: link.FSend, To: b.ID(), Env: env(link.MaxEnvelope), Mid: "b" + strconv.Itoa(i)})
		if f := read(t, ws); f.T == link.FAck {
			n++
		} else if f.Code != link.CodeFull {
			t.Fatalf("answer %+v", f)
		}
	}
	if n < 40 || n > 64 {
		t.Fatalf("%d messages of 16 KB fit in 1 MB", n)
	}
	// Delivered on connect, then the relay holds nothing.
	wb := connect(t, url, b)
	for range n {
		if f := read(t, wb); f.T != link.FMsg {
			t.Fatalf("delivery: %+v", f)
		}
	}
	if st := r.Stats(); st.Queued != 0 || st.QueuedB != 0 {
		t.Fatalf("kept after delivery: %+v", st)
	}
}

// A mailbox closes after 24 hours, and opening it again after a reconnect
// can't keep it open longer.
func TestLinkRelayMailboxExpires(t *testing.T) {
	c := &clock{t: time.Now()}
	url, _ := start(t, relay.Options{Now: c.now})
	host, joiner := ident(t), ident(t)
	wh := connect(t, url, host)
	wj := connect(t, url, joiner)
	box := link.BoxOf(link.NewSecret())
	write(t, wh, link.Frame{T: link.FInviteOpen, Box: box, Exp: c.now().Add(48 * time.Hour).UnixMilli(), Mid: "o"})
	if f := read(t, wh); f.T != link.FAck {
		t.Fatal(f)
	}
	// Someone else can't take it over.
	write(t, wj, link.Frame{T: link.FInviteOpen, Box: box, Mid: "x"})
	if f := read(t, wj); f.T != link.FErr {
		t.Fatalf("taken over: %+v", f)
	}
	c.add(23 * time.Hour)
	write(t, wh, link.Frame{T: link.FInviteOpen, Box: box, Exp: c.now().Add(24 * time.Hour).UnixMilli(), Mid: "o2"})
	read(t, wh)
	c.add(time.Hour + time.Second)
	write(t, wj, link.Frame{T: link.FInvitePost, Box: box, Env: env(40), Mid: "p"})
	if f := read(t, wj); f.T != link.FErr || f.Code != link.CodeUnknown {
		t.Fatalf("a mailbox past 24 h: %+v", f)
	}
}

// Frames that can't be read are answered, not obeyed.
func TestLinkRelayRefusesOddFrames(t *testing.T) {
	url, _ := start(t, relay.Options{})
	ws := connect(t, url, ident(t))
	for _, raw := range []string{`{"t":"send","to":"x","env":"AAAA","mid":"1","extra":1}`, `not json`, `{"t":"send","mid":"bad mid!"}`} {
		if err := ws.WriteMessage(websocket.TextMessage, []byte(raw)); err != nil {
			t.Fatal(err)
		}
		if f := read(t, ws); f.T != link.FErr || f.Code != link.CodeBad {
			t.Fatalf("%s: %+v", raw, f)
		}
	}
	write(t, ws, link.Frame{T: "steal", Mid: "s"})
	if f := read(t, ws); f.T != link.FErr || f.Code != link.CodeUnknown {
		t.Fatalf("an unknown frame: %+v", f)
	}
	write(t, ws, link.Frame{T: link.FSend, To: "not-a-key", Env: env(10), Mid: "k"})
	if f := read(t, ws); f.T != link.FErr || f.Code != link.CodeUnknown {
		t.Fatalf("a bad recipient: %+v", f)
	}
}
