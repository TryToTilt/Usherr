package screening

// Linked screenings, the party: the lobby, the intermission and the verdict
// (docs/home/link-protocol.md, Lobby, intermission and verdict). As with
// chat, the host's Usherr is the hub: a guest says what its own people did
// (took their seats, set a status at the intermission, gave their stars) to
// the host alone, in their pair's own envelope; the host works out the whole
// picture and sends it to everyone at the screening in one message sealed
// with the screening key, which the relay fans out.
//
//   - lobby.seat (guest → host): the guest's people who said I'm in, in the
//     order they said so, and whether each has taken their seat.
//   - lobby.roster (host → everyone): the seat map, seat by seat.
//   - room.status (guest → host): someone's status at the intermission
//     ("getting snacks"), one of a few presets.
//   - room.intermission (host → everyone): the intermission is on (until
//     when) or over, with everyone's status.
//   - verdict.vote (guest → host): someone's stars for the film (1 to 10,
//     half stars out of five); their name only when they chose to share it.
//   - verdict.tally (host → everyone): how many voted, their stars added up,
//     and the names and stars of those who share.
//
// Nothing else travels: no one-line verdicts, no user ids (a person is a
// token their own Usherr makes for that screening).

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/link"
)

// The message types.
const (
	TypeLobbySeat    = "lobby.seat"        // guest → host: its people, and who has taken their seat
	TypeLobbyRoster  = "lobby.roster"      // host → everyone (screening key, fanout): the seat map
	TypeRoomStatus   = "room.status"       // guest → host: someone's status at the intermission
	TypeIntermission = "room.intermission" // host → everyone (screening key, fanout): the intermission, on or over
	TypeVerdictVote  = "verdict.vote"      // guest → host: someone's stars for the film
	TypeVerdictTally = "verdict.tally"     // host → everyone (screening key, fanout): the crew's verdict
)

// Limits.
const (
	MaxBodyLobby     = 4 << 10
	MaxBodySeatMap   = 12 << 10
	MaxBodyInter     = 12 << 10
	MaxBodyTally     = 12 << 10
	MaxSeats         = 120 // on a seat map (fewer when the names are long: it must fit MaxBodySeatMap)
	MaxSeatNo        = (link.MaxFanout + 2) * 20 * 4
	MaxStatuses      = 100
	MaxShared        = 100 // names on a verdict
	MaxVoters        = 2000
	MaxInterMinutes  = 60 // an intermission runs at most this long from now
	DefaultInterMins = 10
	MoreInterMins    = 5
)

// Statuses someone can set at the intermission.
var Statuses = []string{"snacks", "drinks", "break", "back"}

// ValidStatus reports whether s is one of the statuses ("" clears it).
func ValidStatus(s string) bool {
	if s == "" {
		return true
	}
	for _, x := range Statuses {
		if s == x {
			return true
		}
	}
	return false
}

// A person's token: hex, made by their own Usherr for one screening.
var tokenRe = regexp.MustCompile(`^[0-9a-f]{8,32}$`)

// ValidToken reports whether s is a person's token.
func ValidToken(s string) bool { return tokenRe.MatchString(s) }

// LobbyPerson is one of a guest Usherr's people at the lobby.
type LobbyPerson struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Seated bool   `json:"seated,omitempty"`
}

// LobbySeat is lobby.seat: a guest Usherr's people who said I'm in, in the
// order they said so (the order their stubs' seats go in).
type LobbySeat struct {
	V      int           `json:"v"`
	SID    string        `json:"sid"`
	People []LobbyPerson `json:"people"`
}

// Seat is one seat on the map: seat 0 is the host's (Row A, Seat 1), eight
// a row. An empty Name is a seat held for someone at House not named yet.
type Seat struct {
	Seat   int    `json:"seat"`
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	House  string `json:"house,omitempty"`
	Seated bool   `json:"seated,omitempty"`
}

// LobbyRoster is lobby.roster: the seat map.
type LobbyRoster struct {
	V     int    `json:"v"`
	SID   string `json:"sid"`
	Seq   int64  `json:"seq"`
	Seats []Seat `json:"seats"`
}

// RoomStatus is room.status: someone at a guest's house sets their status.
type RoomStatus struct {
	V      int    `json:"v"`
	SID    string `json:"sid"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // one of Statuses, or "" to clear it
}

// Status is someone's status at the intermission.
type Status struct {
	Name   string `json:"name"`
	House  string `json:"house,omitempty"`
	Status string `json:"status"`
}

// Intermission is room.intermission. Until is when it's over (unix ms by the
// relay's clock); Seq goes up with every change.
type Intermission struct {
	V        int      `json:"v"`
	SID      string   `json:"sid"`
	Seq      int64    `json:"seq"`
	On       bool     `json:"on"`
	Until    int64    `json:"until,omitempty"`
	Statuses []Status `json:"statuses,omitempty"`
}

// VerdictVote is verdict.vote. Name is sent only when the person shares
// their stars with the crew.
type VerdictVote struct {
	V     int    `json:"v"`
	SID   string `json:"sid"`
	ID    string `json:"id"`
	Stars int    `json:"stars"` // 1 to 10 (half stars out of five)
	Share bool   `json:"share,omitempty"`
	Name  string `json:"name,omitempty"`
}

// Shared is a voter who shares their stars.
type Shared struct {
	Name  string `json:"name"`
	House string `json:"house,omitempty"`
	Stars int    `json:"stars"`
}

// VerdictTally is verdict.tally: Count voted, Total is their stars added up.
type VerdictTally struct {
	V      int      `json:"v"`
	SID    string   `json:"sid"`
	Seq    int64    `json:"seq"`
	Count  int      `json:"count"`
	Total  int      `json:"total"`
	Shared []Shared `json:"shared,omitempty"`
}

// Party is what the lobby, intermission and verdict messages do on this
// Usherr. Every call is made only after the sender was found allowed and
// within its rate.
type Party interface {
	// Host side: a guest Usherr's people.
	LobbySeatIn(ctx context.Context, guest link.Peer, m LobbySeat) error
	StatusIn(ctx context.Context, guest link.Peer, m RoomStatus) error
	VoteIn(ctx context.Context, guest link.Peer, m VerdictVote) error
	// Guest side: the host's picture.
	LobbyRosterIn(ctx context.Context, host link.Peer, m LobbyRoster) error
	IntermissionIn(ctx context.Context, host link.Peer, m Intermission) error
	TallyIn(ctx context.Context, host link.Peer, m VerdictTally) error
}

// How fast a guest Usherr may send lobby, status and vote messages.
var (
	GuestPartyEvery = 2 * time.Second
	GuestPartyBurst = 10
)

// RegisterParty adds the lobby, intermission and verdict messages to a
// node's registry; b says who hosts and who is invited.
func RegisterParty(reg *link.Registry, b Book, p Party) error {
	limit := NewLimiter(GuestPartyEvery, GuestPartyBurst)
	fromGuest := func(ctx context.Context, in link.Incoming, sid string) error {
		if err := mustInvited(ctx, b, sid, in.Peer.ID); err != nil {
			return err
		}
		if !limit.Allow(in.Peer.ID, time.Now()) {
			return ErrTooFast
		}
		return nil
	}
	fromHost := func(ctx context.Context, in link.Incoming, sid string) error {
		if sid != in.Group {
			return ErrNotAllowed // sealed with another screening's key
		}
		return mustHost(ctx, b, sid, in.Peer.ID, false)
	}
	kinds := []link.Kind{
		{Type: TypeLobbySeat, MaxBody: MaxBodyLobby, Handle: func(ctx context.Context, in link.Incoming) error {
			var m LobbySeat
			if err := decode(in.Msg.Body, &m); err != nil || !m.Check() {
				return errBody
			}
			if err := fromGuest(ctx, in, m.SID); err != nil {
				return err
			}
			return p.LobbySeatIn(ctx, in.Peer, m)
		}},
		{Type: TypeRoomStatus, MaxBody: MaxBodySmall, Handle: func(ctx context.Context, in link.Incoming) error {
			var m RoomStatus
			if err := decode(in.Msg.Body, &m); err != nil || !m.Check() {
				return errBody
			}
			if err := fromGuest(ctx, in, m.SID); err != nil {
				return err
			}
			return p.StatusIn(ctx, in.Peer, m)
		}},
		{Type: TypeVerdictVote, MaxBody: MaxBodySmall, Handle: func(ctx context.Context, in link.Incoming) error {
			var m VerdictVote
			if err := decode(in.Msg.Body, &m); err != nil || !m.Check() {
				return errBody
			}
			if err := fromGuest(ctx, in, m.SID); err != nil {
				return err
			}
			return p.VoteIn(ctx, in.Peer, m)
		}},
		{Type: TypeLobbyRoster, MaxBody: MaxBodySeatMap, Group: true, Handle: func(ctx context.Context, in link.Incoming) error {
			var m LobbyRoster
			if err := decode(in.Msg.Body, &m); err != nil || !m.Check() {
				return errBody
			}
			if err := fromHost(ctx, in, m.SID); err != nil {
				return err
			}
			return p.LobbyRosterIn(ctx, in.Peer, m)
		}},
		{Type: TypeIntermission, MaxBody: MaxBodyInter, Group: true, Handle: func(ctx context.Context, in link.Incoming) error {
			var m Intermission
			if err := decode(in.Msg.Body, &m); err != nil || !m.Check() {
				return errBody
			}
			if err := fromHost(ctx, in, m.SID); err != nil {
				return err
			}
			return p.IntermissionIn(ctx, in.Peer, m)
		}},
		{Type: TypeVerdictTally, MaxBody: MaxBodyTally, Group: true, Handle: func(ctx context.Context, in link.Incoming) error {
			var m VerdictTally
			if err := decode(in.Msg.Body, &m); err != nil || !m.Check() {
				return errBody
			}
			if err := fromHost(ctx, in, m.SID); err != nil {
				return err
			}
			return p.TallyIn(ctx, in.Peer, m)
		}},
	}
	for _, k := range kinds {
		if err := reg.Register(k); err != nil {
			return err
		}
	}
	return nil
}

// Check reads lobby.seat: at most a household, each a token and a name,
// no token twice.
func (m *LobbySeat) Check() bool {
	if m.V != Version || !validSID(m.SID) || len(m.People) > MaxHousehold {
		return false
	}
	seen := map[string]bool{}
	for i := range m.People {
		pp := &m.People[i]
		if !ValidToken(pp.ID) || seen[pp.ID] {
			return false
		}
		seen[pp.ID] = true
		pp.Name = link.CleanName(pp.Name)
		if pp.Name == "" {
			return false
		}
	}
	return true
}

// Check reads lobby.roster: seats in range, none twice.
func (m *LobbyRoster) Check() bool {
	if m.V != Version || !validSID(m.SID) || m.Seq <= 0 || len(m.Seats) > MaxSeats {
		return false
	}
	seen := map[int]bool{}
	for i := range m.Seats {
		st := &m.Seats[i]
		if st.Seat < 0 || st.Seat > MaxSeatNo || seen[st.Seat] || (st.ID != "" && !ValidToken(st.ID)) {
			return false
		}
		seen[st.Seat] = true
		st.Name = nameOrEmpty(st.Name)
		st.House = cleanText(st.House, 60)
	}
	return true
}

// Check reads room.status.
func (m *RoomStatus) Check() bool {
	if m.V != Version || !validSID(m.SID) || !ValidToken(m.ID) || !ValidStatus(m.Status) {
		return false
	}
	m.Name = link.CleanName(m.Name)
	return m.Name != ""
}

// Check reads room.intermission: on with an end, or over.
func (m *Intermission) Check() bool {
	if m.V != Version || !validSID(m.SID) || m.Seq <= 0 || len(m.Statuses) > MaxStatuses {
		return false
	}
	if m.On != (m.Until > 0) {
		return false
	}
	for i := range m.Statuses {
		st := &m.Statuses[i]
		if st.Status == "" || !ValidStatus(st.Status) {
			return false
		}
		st.Name = link.CleanName(st.Name)
		st.House = cleanText(st.House, 60)
		if st.Name == "" {
			return false
		}
	}
	return true
}

// Check reads verdict.vote: stars 1 to 10; a name only when shared.
func (m *VerdictVote) Check() bool {
	if m.V != Version || !validSID(m.SID) || !ValidToken(m.ID) || m.Stars < 1 || m.Stars > 10 {
		return false
	}
	m.Name = nameOrEmpty(m.Name)
	if !m.Share {
		m.Name = ""
	}
	return !m.Share || m.Name != ""
}

// nameOrEmpty is a name cleaned to show, or "" when there's none.
func nameOrEmpty(s string) string {
	if strings.TrimSpace(cleanText(s, 60)) == "" {
		return ""
	}
	return link.CleanName(s)
}

// Check reads verdict.tally: the sums add up.
func (m *VerdictTally) Check() bool {
	if m.V != Version || !validSID(m.SID) || m.Seq <= 0 || m.Count < 0 || m.Count > MaxVoters || m.Total < m.Count || m.Total > m.Count*10 || len(m.Shared) > MaxShared || len(m.Shared) > m.Count {
		return false
	}
	if m.Count == 0 && m.Total != 0 {
		return false
	}
	for i := range m.Shared {
		sh := &m.Shared[i]
		if sh.Stars < 1 || sh.Stars > 10 {
			return false
		}
		sh.Name = link.CleanName(sh.Name)
		sh.House = cleanText(sh.House, 60)
		if sh.Name == "" {
			return false
		}
	}
	return true
}
