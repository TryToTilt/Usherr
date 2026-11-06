// Package relay is the Usherr Link Relay (ULR) in Go: the same blind
// postman as the Cloudflare Worker in relay/, with its queues in memory. It
// is used by the tests, for trying two Usherrs on one machine, and by anyone
// who runs their own relay (cmd/usherr-relay). It only ever sees who sends
// how many bytes to whom: envelopes are sealed end to end, and it never
// logs them.
package relay

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/rdborg/mediarium/internal/link"
)

// Options are the relay's limits; zero values take the protocol's.
type Options struct {
	MaxConnsPerKey int           // live connections per identity (3)
	QueueTTL       time.Duration // how long a message waits for an offline Usherr (24 h)
	QueueMsgs      int           // messages waiting per recipient (200)
	QueueBytes     int           // bytes waiting per recipient (1 MB)
	MaxTotalQueue  int64         // bytes waiting in all (256 MB)
	Rate           float64       // messages per second per sender (20)
	Burst          int           // burst per sender (60)
	PostEvery      time.Duration // one invite.post per connection this often (2 s)
	PostBurst      int           // burst of invite.post per connection (5)
	BoxLife        time.Duration // an invite mailbox stays open at most (24 h)
	BoxesPerKey    int           // open mailboxes per identity (20)
	HelloWait      time.Duration // the hello must come this soon (10 s)
	IdleWait       time.Duration // a connection silent this long is closed (90 s)
	Now            func() time.Time
	// NoFanout makes it a step-1 relay: no fanout, and nothing said about it
	// in ready (tests: how Usherrs cope with an older relay).
	NoFanout bool
}

func (o *Options) defaults() {
	set := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	setD := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	set(&o.MaxConnsPerKey, 3)
	setD(&o.QueueTTL, 24*time.Hour)
	set(&o.QueueMsgs, 200)
	set(&o.QueueBytes, 1<<20)
	if o.MaxTotalQueue <= 0 {
		o.MaxTotalQueue = 256 << 20
	}
	if o.Rate <= 0 {
		o.Rate = 20
	}
	set(&o.Burst, 60)
	setD(&o.PostEvery, 2*time.Second)
	set(&o.PostBurst, 5)
	setD(&o.BoxLife, 24*time.Hour)
	set(&o.BoxesPerKey, 20)
	setD(&o.HelloWait, 10*time.Second)
	setD(&o.IdleWait, 90*time.Second)
	if o.Now == nil {
		o.Now = time.Now
	}
}

// Server is a relay.
type Server struct {
	opt      Options
	upgrader websocket.Upgrader
	stop     chan struct{}
	stopOnce sync.Once

	mu     sync.Mutex
	conns  map[string]map[*conn]struct{}
	queues map[string]*queue
	boxes  map[string]box
	limits map[string]*rate.Limiter
	total  int64
}

type conn struct {
	ws     *websocket.Conn
	id     string
	out    chan []byte
	closed chan struct{}
	once   sync.Once
}

func (c *conn) close() {
	c.once.Do(func() {
		close(c.closed)
		_ = c.ws.Close()
	})
}

type queued struct {
	frame []byte
	exp   time.Time
}

type queue struct {
	items []queued
	bytes int
}

type box struct {
	owner string
	exp   time.Time
}

// New makes a relay. Close stops its clean-up.
func New(opt Options) *Server {
	opt.defaults()
	s := &Server{
		opt:    opt,
		stop:   make(chan struct{}),
		conns:  map[string]map[*conn]struct{}{},
		queues: map[string]*queue{},
		boxes:  map[string]box{},
		limits: map[string]*rate.Limiter{},
		upgrader: websocket.Upgrader{
			ReadBufferSize: 4 << 10, WriteBufferSize: 4 << 10,
			// Usherrs aren't browsers; any page that tries still has to sign
			// the challenge with a key.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
	go s.janitor()
	return s
}

// Close stops the clean-up and closes every connection.
func (s *Server) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.mu.Lock()
	var all []*conn
	for _, set := range s.conns {
		for c := range set {
			all = append(all, c)
		}
	}
	s.mu.Unlock()
	for _, c := range all {
		c.close()
	}
}

// ServeHTTP answers /v1/connect (the WebSocket) and / (a health check).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/connect":
		ws, err := s.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		s.serve(ws)
	case "/", "/healthz":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Usherr Link Relay\n"))
	default:
		http.NotFound(w, r)
	}
}

const frameLimit = 32 << 10 // a 16 KB envelope in base64, and the rest of the frame

func (s *Server) writeNow(ws *websocket.Conn, f link.Frame) {
	_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_ = ws.WriteMessage(websocket.TextMessage, link.EncodeFrame(f))
}

func (s *Server) serve(ws *websocket.Conn) {
	ws.SetReadLimit(frameLimit)
	nonce := make([]byte, 32)
	_, _ = rand.Read(nonce)
	s.writeNow(ws, link.Frame{T: link.FChallenge, Nonce: base64.StdEncoding.EncodeToString(nonce), Time: s.opt.Now().UnixMilli()})
	_ = ws.SetReadDeadline(time.Now().Add(s.opt.HelloWait))
	kind, b, err := ws.ReadMessage()
	if err != nil || kind != websocket.TextMessage {
		_ = ws.Close()
		return
	}
	f, err := link.DecodeFrame(b)
	if err != nil || f.T != link.FHello || f.V != 1 || !link.VerifyHello(f.ID, f.Sig, nonce) {
		_ = ws.Close()
		return
	}
	c := &conn{ws: ws, id: f.ID, out: make(chan []byte, s.opt.QueueMsgs+64), closed: make(chan struct{})}

	s.mu.Lock()
	if len(s.conns[c.id]) >= s.opt.MaxConnsPerKey {
		s.mu.Unlock()
		s.writeNow(ws, link.Frame{T: link.FErr, Code: link.CodeBusy})
		_ = ws.Close()
		return
	}
	if s.conns[c.id] == nil {
		s.conns[c.id] = map[*conn]struct{}{}
	}
	s.conns[c.id][c] = struct{}{}
	// Whatever waited is handed over, and the relay keeps nothing of it.
	var waiting [][]byte
	if q := s.queues[c.id]; q != nil {
		now := s.opt.Now()
		for _, it := range q.items {
			if now.Before(it.exp) {
				waiting = append(waiting, it.frame)
			}
		}
		s.total -= int64(q.bytes)
		delete(s.queues, c.id)
	}
	// Ready goes first, before anything another sender delivers.
	level := link.RelayLevel
	if s.opt.NoFanout {
		level = 0
	}
	c.send(link.EncodeFrame(link.Frame{T: link.FReady, Queued: len(waiting), V: level}))
	for _, w := range waiting {
		c.send(w)
	}
	s.mu.Unlock()
	defer s.drop(c)

	go s.writer(c)

	post := rate.NewLimiter(rate.Every(s.opt.PostEvery), s.opt.PostBurst)
	for {
		_ = ws.SetReadDeadline(time.Now().Add(s.opt.IdleWait))
		kind, b, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if kind != websocket.TextMessage {
			continue
		}
		f, err := link.DecodeFrame(b)
		if err != nil {
			c.send(link.EncodeFrame(link.Frame{T: link.FErr, Code: link.CodeBad}))
			continue
		}
		s.handle(c, f, post)
	}
}

// send hands a frame to the connection's writer; a connection that can't
// keep up is closed.
func (c *conn) send(b []byte) {
	select {
	case <-c.closed:
	case c.out <- b:
	default:
		c.close()
	}
}

func (s *Server) writer(c *conn) {
	for {
		select {
		case <-c.closed:
			return
		case b := <-c.out:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.TextMessage, b); err != nil {
				c.close()
				return
			}
		}
	}
}

func (s *Server) drop(c *conn) {
	c.close()
	s.mu.Lock()
	delete(s.conns[c.id], c)
	if len(s.conns[c.id]) == 0 {
		delete(s.conns, c.id)
	}
	s.mu.Unlock()
}

func (s *Server) limiter(id string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.limits[id]
	if l == nil {
		l = rate.NewLimiter(rate.Limit(s.opt.Rate), s.opt.Burst)
		s.limits[id] = l
	}
	return l
}

func (s *Server) reply(c *conn, mid, code string) {
	if code == "" {
		if mid != "" {
			c.send(link.EncodeFrame(link.Frame{T: link.FAck, Mid: mid}))
		}
		return
	}
	c.send(link.EncodeFrame(link.Frame{T: link.FErr, Mid: mid, Code: code}))
}

// envelope checks a frame's envelope: base64, at most 16 KB.
func (s *Server) envelope(env string) bool {
	if env == "" || len(env) > base64.StdEncoding.EncodedLen(link.MaxEnvelope) {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(env)
	return err == nil && len(b) > 0 && len(b) <= link.MaxEnvelope
}

func (s *Server) handle(c *conn, f link.Frame, post *rate.Limiter) {
	if f.Mid != "" && !link.ValidMid(f.Mid) {
		s.reply(c, "", link.CodeBad)
		return
	}
	switch f.T {
	case link.FPing:
		c.send(link.EncodeFrame(link.Frame{T: link.FPong}))
	case link.FSend:
		if _, err := link.DecodeID(f.To); err != nil || f.Mid == "" {
			s.reply(c, f.Mid, link.CodeUnknown)
			return
		}
		if !s.envelope(f.Env) {
			s.reply(c, f.Mid, link.CodeSize)
			return
		}
		if !s.limiter(c.id).Allow() {
			s.reply(c, f.Mid, link.CodeRate)
			return
		}
		s.reply(c, f.Mid, s.deliver(f.To, link.EncodeFrame(link.Frame{T: link.FMsg, From: c.id, Env: f.Env, Mid: f.Mid})))
	case link.FFanout:
		// One envelope (sealed with a screening key) for up to MaxFanout
		// identities: each gets an ordinary msg frame, live or queued.
		if s.opt.NoFanout {
			s.reply(c, f.Mid, link.CodeUnknown)
			return
		}
		if f.Mid == "" {
			s.reply(c, "", link.CodeBad)
			return
		}
		if len(f.Dests) > link.MaxFanout {
			s.reply(c, f.Mid, link.CodeSize)
			return
		}
		if !link.ValidDests(f.Dests) {
			s.reply(c, f.Mid, link.CodeBad)
			return
		}
		if !s.envelope(f.Env) {
			s.reply(c, f.Mid, link.CodeSize)
			return
		}
		if !s.limiter(c.id).AllowN(time.Now(), link.FanoutCost(len(f.Dests))) {
			s.reply(c, f.Mid, link.CodeRate)
			return
		}
		frame := link.EncodeFrame(link.Frame{T: link.FMsg, From: c.id, Env: f.Env, Mid: f.Mid})
		code := ""
		for _, to := range f.Dests {
			if r := s.deliver(to, frame); r != "" {
				code = r // the others still got it
			}
		}
		s.reply(c, f.Mid, code)
	case link.FInviteOpen:
		if !link.ValidBox(f.Box) {
			s.reply(c, f.Mid, link.CodeBad)
			return
		}
		if !s.limiter(c.id).Allow() {
			s.reply(c, f.Mid, link.CodeRate)
			return
		}
		s.reply(c, f.Mid, s.openBox(c.id, f.Box, f.Exp))
	case link.FInviteClose:
		if !link.ValidBox(f.Box) {
			s.reply(c, f.Mid, link.CodeBad)
			return
		}
		s.mu.Lock()
		if b, ok := s.boxes[f.Box]; ok && b.owner == c.id {
			delete(s.boxes, f.Box)
		}
		s.mu.Unlock()
		s.reply(c, f.Mid, "")
	case link.FInvitePost:
		if !post.Allow() {
			s.reply(c, f.Mid, link.CodeRate)
			return
		}
		if !link.ValidBox(f.Box) {
			s.reply(c, f.Mid, link.CodeUnknown)
			return
		}
		if !s.envelope(f.Env) {
			s.reply(c, f.Mid, link.CodeSize)
			return
		}
		s.mu.Lock()
		b, ok := s.boxes[f.Box]
		if ok && !s.opt.Now().Before(b.exp) {
			delete(s.boxes, f.Box)
			ok = false
		}
		s.mu.Unlock()
		if !ok {
			s.reply(c, f.Mid, link.CodeUnknown)
			return
		}
		s.reply(c, f.Mid, s.deliver(b.owner, link.EncodeFrame(link.Frame{T: link.FInviteMsg, Box: f.Box, From: c.id, Env: f.Env})))
	default:
		s.reply(c, f.Mid, link.CodeUnknown)
	}
}

func (s *Server) openBox(owner, name string, exp int64) string {
	now := s.opt.Now()
	until := now.Add(s.opt.BoxLife)
	if exp > 0 && time.UnixMilli(exp).Before(until) {
		until = time.UnixMilli(exp)
	}
	if !now.Before(until) {
		return link.CodeBad
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.boxes[name]; ok && now.Before(b.exp) {
		if b.owner != owner {
			return link.CodeBad
		}
		// Opened again after a reconnect: never longer than first asked.
		if until.After(b.exp) {
			until = b.exp
		}
	} else {
		n := 0
		for _, b := range s.boxes {
			if b.owner == owner && now.Before(b.exp) {
				n++
			}
		}
		if n >= s.opt.BoxesPerKey {
			return link.CodeFull
		}
	}
	s.boxes[name] = box{owner: owner, exp: until}
	return ""
}

// deliver hands a frame to every live connection of to, or queues it.
func (s *Server) deliver(to string, frame []byte) string {
	s.mu.Lock()
	var live []*conn
	for c := range s.conns[to] {
		live = append(live, c)
	}
	if len(live) > 0 {
		s.mu.Unlock()
		for _, c := range live {
			c.send(frame)
		}
		return ""
	}
	defer s.mu.Unlock()
	now := s.opt.Now()
	q := s.queues[to]
	if q == nil {
		q = &queue{}
		s.queues[to] = q
	}
	s.pruneLocked(to, q, now)
	if len(q.items) >= s.opt.QueueMsgs || q.bytes+len(frame) > s.opt.QueueBytes || s.total+int64(len(frame)) > s.opt.MaxTotalQueue {
		if len(q.items) == 0 {
			delete(s.queues, to)
		}
		return link.CodeFull
	}
	q.items = append(q.items, queued{frame: frame, exp: now.Add(s.opt.QueueTTL)})
	q.bytes += len(frame)
	s.total += int64(len(frame))
	return ""
}

func (s *Server) pruneLocked(to string, q *queue, now time.Time) {
	keep := q.items[:0]
	for _, it := range q.items {
		if now.Before(it.exp) {
			keep = append(keep, it)
		} else {
			q.bytes -= len(it.frame)
			s.total -= int64(len(it.frame))
		}
	}
	clear(q.items[len(keep):])
	q.items = keep
}

func (s *Server) janitor() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.sweep()
		}
	}
}

// sweep forgets expired messages and mailboxes, and idle rate limits.
func (s *Server) sweep() {
	now := s.opt.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for to, q := range s.queues {
		s.pruneLocked(to, q, now)
		if len(q.items) == 0 {
			delete(s.queues, to)
		}
	}
	for name, b := range s.boxes {
		if !now.Before(b.exp) {
			delete(s.boxes, name)
		}
	}
	for id, l := range s.limits {
		if _, live := s.conns[id]; !live && l.Tokens() >= float64(s.opt.Burst) {
			delete(s.limits, id)
		}
	}
}

// Stats is what the relay holds now (tests, and a status line).
type Stats struct {
	Identities int   // with a live connection
	Queued     int   // messages waiting
	QueuedB    int64 // bytes waiting
	Boxes      int   // open invite mailboxes
}

// Stats counts what the relay holds now.
func (s *Server) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Stats{Identities: len(s.conns), QueuedB: s.total, Boxes: len(s.boxes)}
	for _, q := range s.queues {
		st.Queued += len(q.items)
	}
	return st
}

// Sweep runs the clean-up now (tests).
func (s *Server) Sweep() { s.sweep() }
