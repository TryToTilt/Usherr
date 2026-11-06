// Package screening is linked screenings, step 2: the messages two linked
// Usherrs send about a screening (docs/home/link-protocol.md, Screenings).
//
// A host offers a screening to each Usherr it invites, in their own link,
// with a screening key; guests answer (rsvp, ready) to the host alone; the
// host tells everyone who's coming in one message sealed with the screening
// key, which the relay fans out.
//
// This package only reads and checks the messages and decides who may send
// what: only the Usherr that hosts a screening may offer, change or cancel
// it, and only Usherrs it invited may answer. It has no access to the
// library, files or settings; what a message does is up to the Book it is
// given (Usherr's Showtime, in internal/api).
package screening

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"unicode"

	"github.com/rdborg/mediarium/internal/link"
)

// The message types.
const (
	TypeOffer  = "screening.offer"  // host → guest: a screening, and its key
	TypeUpdate = "screening.update" // host → guest: it changed (and maybe a new key)
	TypeCancel = "screening.cancel" // host → guest: called off, or this guest is no longer invited
	TypeRoster = "screening.roster" // host → everyone (screening key, fanout): who's coming
	TypeRSVP   = "rsvp"             // guest → host: going, not going, maybe; how many
	TypeReady  = "ready"            // guest → host: has the film; its copy's runtime
)

// Version is the body version these messages carry.
const Version = 1

// Limits.
const (
	MaxBodyOffer  = 4 << 10
	MaxBodySmall  = 512
	MaxBodyRoster = 12 << 10
	MaxGuests     = link.MaxFanout // on a roster
	MaxHousehold  = 20             // people one Usherr brings
	MaxPreshow    = 60             // minutes
	MaxRuntime    = 600            // minutes
)

// Answers.
const (
	Going    = "going"
	NotGoing = "not"
	Maybe    = "maybe"
)

// Has the film.
const (
	HasYes     = "yes"
	HasGetting = "getting"
	HasNo      = "no"
)

// Film is the film of a screening.
type Film struct {
	TMDB    int    `json:"tmdb"`
	Title   string `json:"title"`
	Year    int    `json:"year,omitempty"`
	Runtime int    `json:"runtime,omitempty"` // minutes, from TMDB
	Copy    int    `json:"copy,omitempty"`    // minutes: the host's own copy, when known
	Edition string `json:"edition,omitempty"` // "Director's Cut", when known
	Poster  string `json:"poster,omitempty"`  // an image.tmdb.org address only
}

// Offer is screening.offer and screening.update: the screening as it is now.
type Offer struct {
	V       int    `json:"v"`
	SID     string `json:"sid"` // the screening's id (a UUID the host made)
	Rev     int    `json:"rev"` // goes up with every change
	Film    Film   `json:"film"`
	At      int64  `json:"at"`      // when the film starts, unix ms
	Preshow int    `json:"preshow"` // minutes of pre-show before it
	Host    string `json:"host"`    // who's hosting (a person's name)
	Key     string `json:"key"`     // base64 of the 32-byte screening key
}

// Cancel is screening.cancel.
type Cancel struct {
	V   int    `json:"v"`
	SID string `json:"sid"`
}

// RSVP is a guest Usherr's answer for its household.
type RSVP struct {
	V      int    `json:"v"`
	SID    string `json:"sid"`
	Answer string `json:"answer"` // going, not, maybe
	Count  int    `json:"count"`  // how many from this household (going, or maybe)
}

// Ready is whether a guest Usherr has the film, and its copy's runtime. It
// also says which revision of the screening it saw (so the host knows the
// offer arrived).
type Ready struct {
	V       int    `json:"v"`
	SID     string `json:"sid"`
	Rev     int    `json:"rev"`
	Has     string `json:"has"`               // yes, getting, no
	Runtime int    `json:"runtime,omitempty"` // minutes of its copy, when known
}

// Roster is who's coming, from the host to everyone at the screening. The
// first is the host's own household (step 3), then each invited Usherr.
type Roster struct {
	V      int     `json:"v"`
	SID    string  `json:"sid"`
	Rev    int     `json:"rev"`
	Guests []Guest `json:"guests"`
}

// Guest is one Usherr on a roster.
type Guest struct {
	Name   string `json:"name"`
	Answer string `json:"answer,omitempty"` // "" for not answered yet
	Count  int    `json:"count,omitempty"`
}

// Book is what the messages do, on this Usherr. Every call is made only
// after the sender was found allowed.
type Book interface {
	// HostOf says who hosts a screening known here: "" and false when it's
	// unknown, mine when this Usherr hosts it, else the host's identity.
	HostOf(ctx context.Context, sid string) (host string, mine bool, err error)
	// Invited says whether a linked Usherr is invited to a screening this
	// Usherr hosts.
	Invited(ctx context.Context, sid, guest string) (bool, error)

	// Guest side.
	Offered(ctx context.Context, host link.Peer, o Offer, key []byte) error
	Cancelled(ctx context.Context, host link.Peer, sid string) error
	RosterIn(ctx context.Context, host link.Peer, r Roster) error

	// Host side.
	RSVPIn(ctx context.Context, guest link.Peer, r RSVP) error
	ReadyIn(ctx context.Context, guest link.Peer, r Ready) error
}

// ErrNotAllowed: the sender may not send that about that screening.
var ErrNotAllowed = errors.New("screening: not allowed")

var errBody = errors.New("screening: the body can't be read")

// Register adds the screening messages to a node's registry.
func Register(reg *link.Registry, b Book) error {
	kinds := []link.Kind{
		{Type: TypeOffer, MaxBody: MaxBodyOffer, Handle: func(ctx context.Context, in link.Incoming) error { return offer(ctx, b, in) }},
		{Type: TypeUpdate, MaxBody: MaxBodyOffer, Handle: func(ctx context.Context, in link.Incoming) error { return offer(ctx, b, in) }},
		{Type: TypeCancel, MaxBody: MaxBodySmall, Handle: func(ctx context.Context, in link.Incoming) error {
			var c Cancel
			if err := decode(in.Msg.Body, &c); err != nil || !validSID(c.SID) {
				return errBody
			}
			if err := mustHost(ctx, b, c.SID, in.Peer.ID, false); err != nil {
				return err
			}
			return b.Cancelled(ctx, in.Peer, c.SID)
		}},
		{Type: TypeRoster, MaxBody: MaxBodyRoster, Group: true, Handle: func(ctx context.Context, in link.Incoming) error {
			var r Roster
			if err := decode(in.Msg.Body, &r); err != nil || !r.check() {
				return errBody
			}
			if r.SID != in.Group {
				return ErrNotAllowed // sealed with another screening's key
			}
			if err := mustHost(ctx, b, r.SID, in.Peer.ID, false); err != nil {
				return err
			}
			return b.RosterIn(ctx, in.Peer, r)
		}},
		{Type: TypeRSVP, MaxBody: MaxBodySmall, Handle: func(ctx context.Context, in link.Incoming) error {
			var r RSVP
			if err := decode(in.Msg.Body, &r); err != nil || !r.check() {
				return errBody
			}
			if err := mustInvited(ctx, b, r.SID, in.Peer.ID); err != nil {
				return err
			}
			return b.RSVPIn(ctx, in.Peer, r)
		}},
		{Type: TypeReady, MaxBody: MaxBodySmall, Handle: func(ctx context.Context, in link.Incoming) error {
			var r Ready
			if err := decode(in.Msg.Body, &r); err != nil || !r.check() {
				return errBody
			}
			if err := mustInvited(ctx, b, r.SID, in.Peer.ID); err != nil {
				return err
			}
			return b.ReadyIn(ctx, in.Peer, r)
		}},
	}
	for _, k := range kinds {
		if err := reg.Register(k); err != nil {
			return err
		}
	}
	return nil
}

func offer(ctx context.Context, b Book, in link.Incoming) error {
	var o Offer
	if err := decode(in.Msg.Body, &o); err != nil {
		return errBody
	}
	key, ok := o.check()
	if !ok {
		return errBody
	}
	// A new screening, or one this Usherr already has from the same host. An
	// update for one not known here is taken as an offer (the offer got
	// lost); one hosted here, or by someone else, never.
	if err := mustHost(ctx, b, o.SID, in.Peer.ID, true); err != nil {
		return err
	}
	return b.Offered(ctx, in.Peer, o, key)
}

// mustHost: the screening is hosted by from (or, when newOK, not known here
// at all).
func mustHost(ctx context.Context, b Book, sid, from string, newOK bool) error {
	host, mine, err := b.HostOf(ctx, sid)
	if err != nil {
		return err
	}
	switch {
	case mine:
		return ErrNotAllowed
	case host == "" && newOK:
		return nil
	case host == "" || host != from:
		return ErrNotAllowed
	}
	return nil
}

func mustInvited(ctx context.Context, b Book, sid, from string) error {
	ok, err := b.Invited(ctx, sid, from)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotAllowed
	}
	return nil
}

func decode(body []byte, v any) error {
	return link.StrictJSON(body, v)
}

// check reads an offer: every field in its range, the key 32 bytes. Text is
// cleaned (no control characters, cut to length).
func (o *Offer) check() ([]byte, bool) {
	if o.V != Version || !validSID(o.SID) || o.Rev < 1 || o.At <= 0 || o.Preshow < 0 || o.Preshow > MaxPreshow {
		return nil, false
	}
	f := &o.Film
	if f.TMDB <= 0 || f.TMDB >= 1<<24 || f.Year < 0 || f.Year > 3000 || f.Runtime < 0 || f.Runtime > MaxRuntime || f.Copy < 0 || f.Copy > MaxRuntime {
		return nil, false
	}
	f.Title = cleanText(f.Title, 200)
	if f.Title == "" {
		return nil, false
	}
	f.Edition = cleanText(f.Edition, 60)
	if f.Poster != "" && (len(f.Poster) > 300 || !strings.HasPrefix(f.Poster, "https://image.tmdb.org/") || strings.ContainsAny(f.Poster, "\"'<> \\")) {
		f.Poster = ""
	}
	o.Host = cleanText(o.Host, 60)
	key, err := base64.StdEncoding.DecodeString(o.Key)
	if err != nil || len(key) != link.GroupKeySize {
		return nil, false
	}
	return key, true
}

func (r *RSVP) check() bool {
	if r.V != Version || !validSID(r.SID) || r.Count < 0 || r.Count > MaxHousehold {
		return false
	}
	switch r.Answer {
	case Going, Maybe:
		return true
	case NotGoing:
		return r.Count == 0
	}
	return false
}

func (r *Ready) check() bool {
	if r.V != Version || !validSID(r.SID) || r.Rev < 0 || r.Runtime < 0 || r.Runtime > MaxRuntime {
		return false
	}
	return r.Has == HasYes || r.Has == HasGetting || r.Has == HasNo
}

func (r *Roster) check() bool {
	if r.V != Version || !validSID(r.SID) || r.Rev < 1 || len(r.Guests) > MaxGuests+1 { // the guests, and the host's own household first
		return false
	}
	for i := range r.Guests {
		g := &r.Guests[i]
		g.Name = link.CleanName(g.Name)
		if g.Count < 0 || g.Count > MaxHousehold {
			return false
		}
		switch g.Answer {
		case "", Going, NotGoing, Maybe:
		default:
			return false
		}
	}
	return true
}

// cleanText takes control and format characters out, joins spaces and cuts
// to n characters.
func cleanText(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n]))
	}
	return s
}

// validSID: a screening id is a UUID (lower-case hex).
func validSID(s string) bool {
	return link.ValidMessageID(s)
}

// NewSID makes a screening id.
func NewSID() string { return link.NewMessageID() }
