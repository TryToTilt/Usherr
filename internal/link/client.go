package link

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
	mrand "math/rand/v2"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DialFunc opens the WebSocket to a relay's /v1/connect.
type DialFunc func(ctx context.Context, url string) (*websocket.Conn, error)

// DefaultDial connects to any address (a test's relay, or one on the home
// network); Usherr gives a dial that reaches only the public internet for
// the shared relay.
func DefaultDial(ctx context.Context, url string) (*websocket.Conn, error) {
	d := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	c, _, err := d.DialContext(ctx, url, nil)
	return c, err
}

// Status is a relay connection's state, for Settings.
type Status struct {
	State   string `json:"state"` // off, connecting, connected, error
	Error   string `json:"error,omitempty"`
	SkewMs  int64  `json:"skewMs"`  // the relay's clock minus ours, at the last connect
	Since   int64  `json:"since"`   // when the state last changed, unix ms
	Waiting int    `json:"waiting"` // messages the relay hasn't taken yet
}

// ClockWarning is how far off the clock may be before Settings warns.
const ClockWarning = 5 * time.Second

// Client limits.
const (
	outboxMax     = 1000
	pingEvery     = 30 * time.Second
	readLimit     = 64 << 10
	handshakeWait = 15 * time.Second
)

// ErrOutboxFull: the relay has been unreachable for long enough that many
// messages wait already.
var ErrOutboxFull = errors.New("too many messages are waiting for the relay")

// ErrNotConnected: the relay isn't connected right now.
var ErrNotConnected = errors.New("the relay isn't connected")

// ClientOptions sets up a relay client.
type ClientOptions struct {
	URL      string // ws(s)://host[:port]; /v1/connect is added
	Identity *Identity
	Dial     DialFunc
	Now      func() time.Time
	// OnFrame gets msg and invite.msg frames, one at a time, in order.
	OnFrame func(c *Client, f Frame)
	// OnReady runs after each successful connect.
	OnReady func(c *Client)
	// OnReject is told when the relay refused a message (rate, size, full…).
	OnReject func(mid, code string)
	// OnStatus is told when the state changes.
	OnStatus func()
	// PingEvery overrides the 30 s keepalive (tests).
	PingEvery time.Duration
}

type outItem struct {
	mid   string
	frame []byte
	sent  bool
	// A fanout: kept so it can be sent one by one to a relay that doesn't
	// know fanout.
	dests []string
	env   string
}

// cost is how many messages the item counts as at the relay.
func (it *outItem) cost() int {
	if len(it.dests) > 0 {
		return FanoutCost(len(it.dests))
	}
	return 1
}

// Client keeps one outgoing WebSocket to a relay: it reconnects with a
// growing, jittered wait, checks the clock against the relay's, keeps the
// invite mailboxes open, and holds messages until the relay takes them.
type Client struct {
	opt    ClientOptions
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	conn    *websocket.Conn
	status  Status
	outbox  []*outItem
	boxes   map[string]int64 // mailbox -> closes at (unix ms)
	waiters map[string]chan Frame
	kick    chan struct{}
	fanout  bool // the relay said it knows fanout (ready's v), at the last connect

	wmu sync.Mutex // one writer at a time
}

// NewClient makes a client; Start connects it.
func NewClient(opt ClientOptions) *Client {
	if opt.Dial == nil {
		opt.Dial = DefaultDial
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.PingEvery <= 0 {
		opt.PingEvery = pingEvery
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		opt: opt, ctx: ctx, cancel: cancel, done: make(chan struct{}),
		status:  Status{State: "off"},
		boxes:   map[string]int64{},
		waiters: map[string]chan Frame{},
		kick:    make(chan struct{}, 1),
	}
}

// URL is the relay's address.
func (c *Client) URL() string { return c.opt.URL }

// Start connects, and keeps reconnecting until Close.
func (c *Client) Start() { go c.run() }

// Close disconnects for good.
func (c *Client) Close() {
	c.cancel()
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	<-c.done
}

// Status is the connection's state now.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.status
	s.Waiting = len(c.outbox)
	return s
}

func (c *Client) setState(state, msg string) {
	c.mu.Lock()
	changed := c.status.State != state || c.status.Error != msg
	c.status.State, c.status.Error = state, msg
	if changed {
		c.status.Since = c.opt.Now().UnixMilli()
	}
	c.mu.Unlock()
	if changed && c.opt.OnStatus != nil {
		c.opt.OnStatus()
	}
}

func newMid() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// Send queues an envelope for a linked Usherr; it goes as soon as the relay
// is connected, and again after a reconnect until the relay acknowledges it.
func (c *Client) Send(to string, env []byte) (string, error) {
	if len(env) > MaxEnvelope {
		return "", errors.New("the message is too big")
	}
	mid := newMid()
	f := EncodeFrame(Frame{T: FSend, To: to, Env: base64.StdEncoding.EncodeToString(env), Mid: mid})
	c.mu.Lock()
	if len(c.outbox) >= outboxMax {
		c.mu.Unlock()
		return "", ErrOutboxFull
	}
	c.outbox = append(c.outbox, &outItem{mid: mid, frame: f})
	c.mu.Unlock()
	c.poke()
	return mid, nil
}

// SendMany queues one envelope for several linked Usherrs (at most
// MaxFanout): one fanout frame when the relay knows fanout, else one send
// each. Like Send, it goes again after a reconnect until the relay answers.
func (c *Client) SendMany(dests []string, env []byte) error {
	if len(env) > MaxEnvelope {
		return errors.New("the message is too big")
	}
	if !ValidDests(dests) {
		return errors.New("link: a fanout needs 1 to 100 different identities")
	}
	e := base64.StdEncoding.EncodeToString(env)
	c.mu.Lock()
	var items []*outItem
	if c.fanout {
		mid := newMid()
		items = append(items, &outItem{mid: mid, frame: EncodeFrame(Frame{T: FFanout, Dests: dests, Env: e, Mid: mid}), dests: dests, env: e})
	} else {
		items = singles(dests, e)
	}
	if len(c.outbox)+len(items) > outboxMax {
		c.mu.Unlock()
		return ErrOutboxFull
	}
	c.outbox = append(c.outbox, items...)
	c.mu.Unlock()
	c.poke()
	return nil
}

// singles is a fanout as one send per recipient.
func singles(dests []string, env string) []*outItem {
	out := make([]*outItem, 0, len(dests))
	for _, d := range dests {
		mid := newMid()
		out = append(out, &outItem{mid: mid, frame: EncodeFrame(Frame{T: FSend, To: d, Env: env, Mid: mid})})
	}
	return out
}

// FanoutKnown reports whether the relay said, at the last connect, that it
// knows fanout.
func (c *Client) FanoutKnown() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fanout
}

func (c *Client) poke() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// Request sends one frame now and waits for the relay's ack or err (an
// invite.post). It isn't kept for a reconnect.
func (c *Client) Request(ctx context.Context, f Frame) (Frame, error) {
	f.Mid = newMid()
	ch := make(chan Frame, 1)
	c.mu.Lock()
	conn := c.conn
	if conn == nil {
		c.mu.Unlock()
		return Frame{}, ErrNotConnected
	}
	c.waiters[f.Mid] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiters, f.Mid)
		c.mu.Unlock()
	}()
	if err := c.write(conn, EncodeFrame(f)); err != nil {
		return Frame{}, ErrNotConnected
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return Frame{}, ErrNotConnected
		}
		return r, nil
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	}
}

// OpenBox keeps an invite mailbox open (now, and after every reconnect)
// until it closes at exp.
func (c *Client) OpenBox(box string, exp time.Time) {
	c.mu.Lock()
	c.boxes[box] = exp.UnixMilli()
	conn := c.conn
	c.mu.Unlock()
	if conn != nil {
		_ = c.write(conn, EncodeFrame(Frame{T: FInviteOpen, Box: box, Exp: exp.UnixMilli()}))
	}
}

// OpenBoxWait opens an invite mailbox like OpenBox, and when the relay is
// connected waits until it says the mailbox is open.
func (c *Client) OpenBoxWait(ctx context.Context, box string, exp time.Time) error {
	c.mu.Lock()
	c.boxes[box] = exp.UnixMilli()
	c.mu.Unlock()
	r, err := c.Request(ctx, Frame{T: FInviteOpen, Box: box, Exp: exp.UnixMilli()})
	if err != nil {
		return err
	}
	if r.T == FErr {
		return errors.New("the relay refused the mailbox (" + r.Code + ")")
	}
	return nil
}

// CloseBox closes an invite mailbox.
func (c *Client) CloseBox(box string) {
	c.mu.Lock()
	_, had := c.boxes[box]
	delete(c.boxes, box)
	conn := c.conn
	c.mu.Unlock()
	if had && conn != nil {
		_ = c.write(conn, EncodeFrame(Frame{T: FInviteClose, Box: box}))
	}
}

func (c *Client) write(conn *websocket.Conn, b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, b)
}

func (c *Client) run() {
	defer close(c.done)
	wait := time.Second
	for c.ctx.Err() == nil {
		c.setState("connecting", c.Status().Error)
		ready, err := c.connect()
		if c.ctx.Err() != nil {
			break
		}
		if ready {
			wait = time.Second
		}
		msg := "Can't reach the relay."
		if errors.Is(err, errRefused) {
			msg = "The relay didn't let this Usherr in."
		}
		c.setState("error", msg)
		jitter, _ := rand.Int(rand.Reader, big.NewInt(int64(wait)))
		t := time.NewTimer(wait + time.Duration(jitter.Int64()))
		select {
		case <-c.ctx.Done():
			t.Stop()
		case <-t.C:
		}
		wait = min(wait*2, 2*time.Minute)
	}
	c.setState("off", "")
}

var errRefused = errors.New("the relay refused the hello")

// connect runs one connection, from the challenge until it drops. ready says
// whether it got as far as the relay's ready.
func (c *Client) connect() (ready bool, err error) {
	dctx, cancel := context.WithTimeout(c.ctx, 20*time.Second)
	conn, err := c.opt.Dial(dctx, c.opt.URL+"/v1/connect")
	cancel()
	if err != nil {
		return false, err
	}
	defer conn.Close()
	conn.SetReadLimit(readLimit)
	stop := context.AfterFunc(c.ctx, func() { _ = conn.Close() })
	defer stop()

	_ = conn.SetReadDeadline(time.Now().Add(handshakeWait))
	f, err := readFrame(conn)
	if err != nil || f.T != FChallenge {
		return false, errors.New("no challenge")
	}
	nonce, err := base64.StdEncoding.DecodeString(f.Nonce)
	if err != nil || len(nonce) != 32 {
		return false, errors.New("a bad challenge")
	}
	skew := f.Time - c.opt.Now().UnixMilli()
	sig := c.opt.Identity.Sign(append([]byte(HelloPrefix), nonce...))
	if err := c.write(conn, EncodeFrame(Frame{T: FHello, ID: c.opt.Identity.ID(), Sig: base64.StdEncoding.EncodeToString(sig), V: 1})); err != nil {
		return false, err
	}
	f, err = readFrame(conn)
	if err != nil || f.T != FReady {
		return false, errRefused
	}

	c.mu.Lock()
	c.conn = conn
	c.status.SkewMs = skew
	c.fanout = f.V >= RelayLevel
	if !c.fanout {
		// A relay that doesn't know fanout (or no longer does): what waits
		// goes one by one.
		var out []*outItem
		for _, it := range c.outbox {
			if len(it.dests) > 0 {
				out = append(out, singles(it.dests, it.env)...)
			} else {
				out = append(out, it)
			}
		}
		c.outbox = out
	}
	for _, it := range c.outbox {
		it.sent = false
	}
	now := c.opt.Now().UnixMilli()
	var boxes []Frame
	for b, exp := range c.boxes {
		if exp <= now {
			delete(c.boxes, b)
			continue
		}
		boxes = append(boxes, Frame{T: FInviteOpen, Box: b, Exp: exp})
	}
	c.mu.Unlock()
	c.setState("connected", "")
	defer func() {
		c.mu.Lock()
		c.conn = nil
		for mid, ch := range c.waiters {
			close(ch)
			delete(c.waiters, mid)
		}
		c.mu.Unlock()
	}()
	for _, b := range boxes {
		if err := c.write(conn, EncodeFrame(b)); err != nil {
			return true, err
		}
	}

	connCtx, connDone := context.WithCancel(c.ctx)
	defer connDone()
	go c.writer(connCtx, conn)
	go c.pinger(connCtx, conn)
	if c.opt.OnReady != nil {
		c.opt.OnReady(c)
	}
	c.poke()

	for {
		_ = conn.SetReadDeadline(time.Now().Add(c.opt.PingEvery*2 + 15*time.Second))
		f, err := readFrame(conn)
		if err != nil {
			return true, err
		}
		switch f.T {
		case FPong:
		case FAck, FErr:
			c.answered(f)
		case FMsg, FInviteMsg:
			if c.opt.OnFrame != nil {
				c.opt.OnFrame(c, f)
			}
		}
	}
}

func readFrame(conn *websocket.Conn) (Frame, error) {
	kind, b, err := conn.ReadMessage()
	if err != nil {
		return Frame{}, err
	}
	if kind != websocket.TextMessage {
		return Frame{}, errors.New("not a text frame")
	}
	return DecodeFrame(b)
}

// answered takes an ack or err: the message it answers leaves the outbox.
func (c *Client) answered(f Frame) {
	c.mu.Lock()
	if ch, ok := c.waiters[f.Mid]; ok {
		ch <- f
		delete(c.waiters, f.Mid)
		c.mu.Unlock()
		return
	}
	for i, it := range c.outbox {
		if it.mid == f.Mid {
			c.outbox = append(c.outbox[:i], c.outbox[i+1:]...)
			break
		}
	}
	c.mu.Unlock()
	if f.T == FErr && f.Mid != "" && c.opt.OnReject != nil {
		c.opt.OnReject(f.Mid, f.Code)
	}
}

// writer sends what waits in the outbox, a few at a time (the relay allows
// 20 a second; a fanout counts as one for every ten recipients).
func (c *Client) writer(ctx context.Context, conn *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.kick:
		}
		for {
			c.mu.Lock()
			var next *outItem
			for _, it := range c.outbox {
				if !it.sent {
					next = it
					break
				}
			}
			if next != nil {
				next.sent = true
			}
			c.mu.Unlock()
			if next == nil {
				break
			}
			if err := c.write(conn, next.frame); err != nil {
				_ = conn.Close()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(sendGap * time.Duration(next.cost())):
			}
		}
	}
}

// sendGap keeps a burst of messages under the relay's rate limit.
var sendGap = 55 * time.Millisecond

func (c *Client) pinger(ctx context.Context, conn *websocket.Conn) {
	// Spread the pings of many Usherrs a little.
	every := c.opt.PingEvery
	t := time.NewTicker(every + time.Duration(mrand.Int64N(int64(every)/10+1)))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.write(conn, EncodeFrame(Frame{T: FPing})); err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}
