// Package rewards is linked screenings, step 5: ticket stubs and stamps over
// the link (docs/home/link-protocol.md, Rewards).
//
// At the door, the Usherr hosting a screening hands each linked Usherr that's
// coming a block of seats: one ticket stub for each of its people, numbered
// by the host (reward.stub). The message is signed with the host's key, like
// every message over the link, so a stub can't be made up by anyone else.
// When someone at a guest's house stays to the end, the guest's Usherr says
// which of its seats were stamped (reward.stamp, guest to host), and the host
// answers with the stamps it now holds for that Usherr (reward.stamp, host to
// guest), so both sides agree.
//
// Like internal/link/screening, this package only reads and checks the
// messages and decides who may send what: only the host of a screening hands
// out its stubs and confirms stamps, and a guest can only stamp the seats it
// was given. What a message does is up to the Book (Usherr's stubs, in
// internal/api). Nothing here can reach the library, files or settings.
package rewards

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/rdborg/mediarium/internal/link"
)

// The message types.
const (
	TypeStub  = "reward.stub"  // host → guest: its people's stubs, at the door
	TypeStamp = "reward.stamp" // guest → host: seats stamped; host → guest: the stamps the host holds
)

// Version is the body version these messages carry.
const Version = 1

// Limits.
const (
	MaxBodyStub  = 4 << 10
	MaxBodyStamp = 1 << 10
	MaxBlock     = 20                            // seats in one block (people one Usherr brings)
	MaxSeat      = (link.MaxFanout + 2) * 20 * 4 // the highest seat number taken
	MaxWith      = 40                            // names on a stub's "with" line
	MaxRuntime   = 600                           // minutes
	MaxNo        = 1 << 30                       // stub numbers
)

// Film is the film on a stub.
type Film struct {
	TMDB    int    `json:"tmdb"`
	Title   string `json:"title"`
	Year    int    `json:"year,omitempty"`
	Runtime int    `json:"runtime,omitempty"` // minutes
	Poster  string `json:"poster,omitempty"`  // an image.tmdb.org address only
}

// Stub is reward.stub: a block of seats for one linked Usherr's people.
// Seats Seat … Seat+Count-1 carry stub numbers No … No+Count-1.
type Stub struct {
	V     int      `json:"v"`
	SID   string   `json:"sid"`
	Film  Film     `json:"film"`
	At    int64    `json:"at"`   // when the film started, unix ms
	Host  string   `json:"host"` // who hosted (a person's name)
	Seat  int      `json:"seat"` // the first seat (1 and up; 0 is the host)
	Count int      `json:"count"`
	No    int      `json:"no"`             // the first stub's number
	With  []string `json:"with,omitempty"` // who else was there (names of people and Usherrs)
}

// Stamp is reward.stamp: the seats stamped (they stayed to the end). From a
// guest, the ones it stamped; from the host, every one it holds for that
// guest.
type Stamp struct {
	V     int    `json:"v"`
	SID   string `json:"sid"`
	Seats []int  `json:"seats"`
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
	// Holds says whether every one of seats was handed to that guest at a
	// screening hosted here.
	Holds(ctx context.Context, sid, guest string, seats []int) (bool, error)

	// Guest side: stubs from the host, and the stamps it holds.
	StubsIn(ctx context.Context, host link.Peer, s Stub) error
	StampsAgreed(ctx context.Context, host link.Peer, s Stamp) error

	// Host side: seats a guest stamped.
	StampsIn(ctx context.Context, guest link.Peer, s Stamp) error
}

// ErrNotAllowed: the sender may not send that about that screening.
var ErrNotAllowed = errors.New("rewards: not allowed")

var errBody = errors.New("rewards: the body can't be read")

// Register adds the reward messages to a node's registry. Both come only in
// the pair's own envelope: a guest's stubs and stamps are for it alone.
func Register(reg *link.Registry, b Book) error {
	kinds := []link.Kind{
		{Type: TypeStub, MaxBody: MaxBodyStub, Handle: func(ctx context.Context, in link.Incoming) error {
			var s Stub
			if err := link.StrictJSON(in.Msg.Body, &s); err != nil || !s.check() {
				return errBody
			}
			host, mine, err := b.HostOf(ctx, s.SID)
			if err != nil {
				return err
			}
			if mine || host == "" || host != in.Peer.ID {
				return ErrNotAllowed
			}
			return b.StubsIn(ctx, in.Peer, s)
		}},
		{Type: TypeStamp, MaxBody: MaxBodyStamp, Handle: func(ctx context.Context, in link.Incoming) error {
			var s Stamp
			if err := link.StrictJSON(in.Msg.Body, &s); err != nil || !s.check() {
				return errBody
			}
			host, mine, err := b.HostOf(ctx, s.SID)
			if err != nil {
				return err
			}
			if !mine {
				// The host's record, back to a guest.
				if host == "" || host != in.Peer.ID {
					return ErrNotAllowed
				}
				return b.StampsAgreed(ctx, in.Peer, s)
			}
			// A guest's stamps, at a screening hosted here: only its own seats.
			if ok, err := b.Invited(ctx, s.SID, in.Peer.ID); err != nil || !ok {
				return errors.Join(ErrNotAllowed, err)
			}
			if len(s.Seats) == 0 {
				return errBody
			}
			if ok, err := b.Holds(ctx, s.SID, in.Peer.ID, s.Seats); err != nil || !ok {
				return errors.Join(ErrNotAllowed, err)
			}
			return b.StampsIn(ctx, in.Peer, s)
		}},
	}
	for _, k := range kinds {
		if err := reg.Register(k); err != nil {
			return err
		}
	}
	return nil
}

// check reads a stub: every field in its range. Text is cleaned (no control
// characters, cut to length).
func (s *Stub) check() bool {
	if s.V != Version || !link.ValidMessageID(s.SID) || s.At <= 0 || s.Seat < 1 || s.Count < 1 || s.Count > MaxBlock || s.Seat+s.Count-1 > MaxSeat ||
		s.No < 1 || s.No > MaxNo || len(s.With) > MaxWith {
		return false
	}
	f := &s.Film
	if f.TMDB <= 0 || f.TMDB >= 1<<24 || f.Year < 0 || f.Year > 3000 || f.Runtime < 0 || f.Runtime > MaxRuntime {
		return false
	}
	f.Title = CleanText(f.Title, 200)
	if f.Title == "" {
		return false
	}
	if f.Poster != "" && (len(f.Poster) > 300 || !strings.HasPrefix(f.Poster, "https://image.tmdb.org/") || strings.ContainsAny(f.Poster, "\"'<> \\")) {
		f.Poster = ""
	}
	s.Host = CleanText(s.Host, 60)
	with := s.With[:0]
	for _, n := range s.With {
		if n = CleanText(n, 60); n != "" {
			with = append(with, n)
		}
	}
	s.With = with
	return true
}

// check reads a stamp: seats in range, none twice.
func (s *Stamp) check() bool {
	if s.V != Version || !link.ValidMessageID(s.SID) || s.Seats == nil || len(s.Seats) > MaxSeatsStamp {
		return false
	}
	seen := map[int]bool{}
	for _, n := range s.Seats {
		if n < 1 || n > MaxSeat || seen[n] {
			return false
		}
		seen[n] = true
	}
	return true
}

// MaxSeatsStamp is the most seats one stamp names (a guest's whole share at
// a screening, in a few blocks).
const MaxSeatsStamp = 4 * MaxBlock

// CleanText takes control and format characters out, joins spaces and cuts
// to n characters.
func CleanText(s string, n int) string {
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
