package screening

// Linked screenings, step 4: chat and reactions over the link
// (docs/home/link-protocol.md, Chat and reactions). Guests are linked to the
// host, not to each other, so the host's Usherr is the hub: a guest sends
// what someone said (chat.send) or a reaction (react.send) to the host in
// their pair's own envelope; the host keeps it and passes it on to everyone
// at the screening in one message sealed with the screening key (chat.msg,
// react.moment), which the relay fans out. The host's own people's go out
// the same way.
//
// Chat is plain text only (no markup, no links that open by themselves),
// at most 500 characters; a message can be marked a spoiler. A reaction is
// one of five words, pinned to the second of the film it was sent at, so
// someone catching up sees it at the same moment. Each sender is held to a
// rate: a guest Usherr here (for its whole household), each person on
// their own Usherr.

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rdborg/mediarium/internal/link"
)

// The message types.
const (
	TypeChatSend    = "chat.send"    // guest → host: someone there said something
	TypeChatMsg     = "chat.msg"     // host → everyone (screening key, fanout): something said at the screening
	TypeReactSend   = "react.send"   // guest → host: someone there reacted
	TypeReactMoment = "react.moment" // host → everyone (screening key, fanout): a reaction, at its second of the film
)

// Limits.
const (
	MaxChatText = 500 // characters
	MaxBodyChat = 4 << 10
)

// Words are the reactions, in the order they're shown.
var Words = []string{"HA", "EEK", "WOW", "NOPE", "CLAP"}

// ValidWord reports whether w is one of the reactions.
func ValidWord(w string) bool {
	for _, x := range Words {
		if w == x {
			return true
		}
	}
	return false
}

// ChatSend is what someone at a guest Usherr said, to the host.
type ChatSend struct {
	V       int     `json:"v"`
	SID     string  `json:"sid"`
	ID      string  `json:"id"` // the message's id (a UUID the sender made)
	Name    string  `json:"name"`
	Text    string  `json:"text"`
	Spoiler bool    `json:"spoiler,omitempty"`
	Pos     float64 `json:"pos"` // seconds into the film when it was said, at the sender
}

// ChatMsg is something said at the screening, from the host to everyone.
type ChatMsg struct {
	V       int     `json:"v"`
	SID     string  `json:"sid"`
	ID      string  `json:"id"`
	Name    string  `json:"name"`            // the person
	House   string  `json:"house,omitempty"` // their Usherr
	Text    string  `json:"text"`
	Spoiler bool    `json:"spoiler,omitempty"`
	Pos     float64 `json:"pos"`
	At      int64   `json:"at"` // unix ms
}

// ReactSend is a reaction at a guest Usherr, to the host.
type ReactSend struct {
	V    int     `json:"v"`
	SID  string  `json:"sid"`
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Word string  `json:"word"`
	Pos  float64 `json:"pos"`
}

// ReactMoment is a reaction at the screening, from the host to everyone.
type ReactMoment struct {
	V     int     `json:"v"`
	SID   string  `json:"sid"`
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	House string  `json:"house,omitempty"`
	Word  string  `json:"word"`
	Pos   float64 `json:"pos"`
	At    int64   `json:"at"`
}

// Chat is what chat and reaction messages do on this Usherr. Every call is
// made only after the sender was found allowed and within its rate.
type Chat interface {
	// Host side: someone at an invited Usherr said something, or reacted.
	ChatFrom(ctx context.Context, guest link.Peer, m ChatSend) error
	ReactFrom(ctx context.Context, guest link.Peer, r ReactSend) error
	// Guest side: the host passes on what was said and reacted.
	ChatIn(ctx context.Context, host link.Peer, m ChatMsg) error
	ReactIn(ctx context.Context, host link.Peer, r ReactMoment) error
}

// ErrTooFast: the sender is over its rate; the message is dropped.
var ErrTooFast = errors.New("screening: too many at once")

// How fast a guest Usherr may send, for its whole household (each person
// there is held to their own Usherr's rate as well).
var (
	GuestChatEvery  = time.Second
	GuestChatBurst  = 12
	GuestReactEvery = 200 * time.Millisecond
	GuestReactBurst = 20
)

// RegisterChat adds the chat and reaction messages to a node's registry; b
// says who hosts and who is invited, as for the screening messages.
func RegisterChat(reg *link.Registry, b Book, c Chat) error {
	says := NewLimiter(GuestChatEvery, GuestChatBurst)
	reacts := NewLimiter(GuestReactEvery, GuestReactBurst)
	kinds := []link.Kind{
		{Type: TypeChatSend, MaxBody: MaxBodyChat, Handle: func(ctx context.Context, in link.Incoming) error {
			var m ChatSend
			if err := decode(in.Msg.Body, &m); err != nil || !m.check() {
				return errBody
			}
			if err := mustInvited(ctx, b, m.SID, in.Peer.ID); err != nil {
				return err
			}
			if !says.Allow(in.Peer.ID, time.Now()) {
				return ErrTooFast
			}
			return c.ChatFrom(ctx, in.Peer, m)
		}},
		{Type: TypeChatMsg, MaxBody: MaxBodyChat, Group: true, Handle: func(ctx context.Context, in link.Incoming) error {
			var m ChatMsg
			if err := decode(in.Msg.Body, &m); err != nil || !m.Check() {
				return errBody
			}
			if m.SID != in.Group {
				return ErrNotAllowed // sealed with another screening's key
			}
			if err := mustHost(ctx, b, m.SID, in.Peer.ID, false); err != nil {
				return err
			}
			return c.ChatIn(ctx, in.Peer, m)
		}},
		{Type: TypeReactSend, MaxBody: MaxBodySmall, Handle: func(ctx context.Context, in link.Incoming) error {
			var r ReactSend
			if err := decode(in.Msg.Body, &r); err != nil || !r.check() {
				return errBody
			}
			if err := mustInvited(ctx, b, r.SID, in.Peer.ID); err != nil {
				return err
			}
			if !reacts.Allow(in.Peer.ID, time.Now()) {
				return ErrTooFast
			}
			return c.ReactFrom(ctx, in.Peer, r)
		}},
		{Type: TypeReactMoment, MaxBody: MaxBodySmall, Group: true, Handle: func(ctx context.Context, in link.Incoming) error {
			var r ReactMoment
			if err := decode(in.Msg.Body, &r); err != nil || !r.Check() {
				return errBody
			}
			if r.SID != in.Group {
				return ErrNotAllowed
			}
			if err := mustHost(ctx, b, r.SID, in.Peer.ID, false); err != nil {
				return err
			}
			return c.ReactIn(ctx, in.Peer, r)
		}},
	}
	for _, k := range kinds {
		if err := reg.Register(k); err != nil {
			return err
		}
	}
	return nil
}

// CleanChat is chat text as it's kept and shown: control and format
// characters out, spaces joined, at most MaxChatText characters. It's
// shown as plain text everywhere.
func CleanChat(s string) string {
	// A line break or tab is a space; any other control character goes.
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s)
	return cleanText(s, MaxChatText)
}

// ValidPos reports whether a film position (seconds) is one a screening
// can be at.
func ValidPos(p float64) bool {
	return !math.IsNaN(p) && !math.IsInf(p, 0) && p >= -float64(MaxPreshow*60+60) && p <= float64(MaxRuntime*60+3600)
}

func chatFields(v int, sid, id string, name *string, pos float64) bool {
	if v != Version || !validSID(sid) || !link.ValidMessageID(id) || !ValidPos(pos) {
		return false
	}
	*name = link.CleanName(*name)
	return true
}

func (m *ChatSend) check() bool {
	if !chatFields(m.V, m.SID, m.ID, &m.Name, m.Pos) || len([]rune(m.Text)) > MaxChatText*2 {
		return false
	}
	m.Text = CleanChat(m.Text)
	return m.Text != ""
}

// Check reads a chat message: its fields in range, its text cleaned.
func (m *ChatMsg) Check() bool {
	if !chatFields(m.V, m.SID, m.ID, &m.Name, m.Pos) || m.At <= 0 || len([]rune(m.Text)) > MaxChatText*2 {
		return false
	}
	m.Text = CleanChat(m.Text)
	m.House = cleanText(m.House, 60)
	return m.Text != ""
}

func (r *ReactSend) check() bool {
	return chatFields(r.V, r.SID, r.ID, &r.Name, r.Pos) && ValidWord(r.Word)
}

// Check reads a reaction: one of the words, at a second of the film.
func (r *ReactMoment) Check() bool {
	if !chatFields(r.V, r.SID, r.ID, &r.Name, r.Pos) || r.At <= 0 || !ValidWord(r.Word) {
		return false
	}
	r.House = cleanText(r.House, 60)
	return true
}

// Limiter holds each sender (by key) to a rate: one every so often, with a
// burst. Its zero value isn't usable; make one with NewLimiter.
type Limiter struct {
	mu      sync.Mutex
	every   time.Duration
	burst   float64
	buckets map[string]limitBucket
}

type limitBucket struct {
	tokens float64
	at     time.Time
}

// NewLimiter lets each key send burst at once, then one every every.
func NewLimiter(every time.Duration, burst int) *Limiter {
	return &Limiter{every: every, burst: float64(burst), buckets: map[string]limitBucket{}}
}

// Allow takes one from key's allowance, and reports whether there was one.
func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = limitBucket{tokens: l.burst, at: now}
	} else if d := now.Sub(b.at); d > 0 {
		b.tokens = math.Min(l.burst, b.tokens+float64(d)/float64(l.every))
		b.at = now
	}
	if b.tokens < 1 {
		l.buckets[key] = b
		return false
	}
	b.tokens--
	if len(l.buckets) >= 10_000 && !ok {
		// Forget the senders that are back to a full allowance.
		full := time.Duration(l.burst * float64(l.every))
		for k, x := range l.buckets {
			if now.Sub(x.at) >= full {
				delete(l.buckets, k)
			}
		}
	}
	l.buckets[key] = b
	return true
}
