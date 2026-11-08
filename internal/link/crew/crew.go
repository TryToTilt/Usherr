// Package crew is the crew space between parties: the messages linked
// Usherrs send about a crew (docs/home/link-protocol.md, Crews).
//
// A crew is a named group of linked Usherrs ("houses"). The Usherr that made
// it owns it and is the hub: it invites houses it is linked with, and every
// house talks to the owner alone (two houses in a crew needn't be linked with
// each other). The owner keeps the crew's chat, shared watchlist, votes and
// film clubs, and sends each house the latest whenever something changes.
//
// This package only reads and checks the messages and decides who may send
// what: only the owner invites, ends or tells the crew how things stand; only
// houses in the crew may answer, chat, add films, vote or join a club. It has
// no access to the library, files or settings, and no message can ask for
// any; what a message does is up to the Book it is given (Usherr's Crews, in
// internal/api).
package crew

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode"

	"github.com/rdborg/mediarium/internal/link"
)

// The message types.
const (
	TypeInvite   = "crew.invite"   // owner → house: the crew as it is now (the first one is the invite)
	TypeAnswer   = "crew.answer"   // house → owner: join (and who opted in), decline, leave
	TypeEnd      = "crew.end"      // owner → house: the crew ended, or this house is no longer in it
	TypeChat     = "crew.chat"     // house → owner, then owner → every other house: a chat line
	TypeList     = "crew.list"     // house → owner: add, remove, have; owner → house: the whole list
	TypeVote     = "crew.vote"     // house → owner: open, cast; owner → house: the ballot
	TypeClub     = "crew.club"     // house → owner: add, join, leave, pick, remove; owner → house: every club
	TypeActivity = "crew.activity" // house → owner, then owner → every other house: what people watched
)

// Types lists every crew message type.
var Types = []string{TypeInvite, TypeAnswer, TypeEnd, TypeChat, TypeList, TypeVote, TypeClub, TypeActivity}

// Version is the body version these messages carry.
const Version = 1

// Limits.
const (
	MaxBodySmall    = 1 << 10
	MaxBodyInvite   = 8 << 10
	MaxBodyChat     = 4 << 10
	MaxBodyList     = 14 << 10
	MaxBodyVote     = 10 << 10
	MaxBodyClub     = 14 << 10
	MaxBodyActivity = 4 << 10

	MaxHouses   = 12  // in a crew, the owner's own first
	MaxPeople   = 12  // people of one house who opted in
	MaxText     = 500 // characters of a chat line
	MaxList     = 40  // films on the shared watchlist
	MaxBallot   = 8   // films on a ballot
	MaxVoters   = 100 // names listed under one film of a ballot
	MaxClubs    = 6   // film clubs of a crew
	MaxRotation = 24  // seats in a club's rotation
	MaxLines    = 10  // activity lines in one message
	MaxName     = 60  // a crew's, a club's or a house's name
	MaxWho      = 40  // a person's name
	MaxTitle    = 200 // a film's title
	MaxPreshow  = 60  // minutes
)

// Answers of a house.
const (
	Join    = "join"
	Decline = "decline"
	Leave   = "leave"
)

// Has the film (as screening's "Have it?": a house says only this, never
// what else it has).
const (
	HasYes     = "yes"
	HasGetting = "getting"
	HasNo      = "no"
)

// List ops.
const (
	ListAdd    = "add"    // house: add a film
	ListRemove = "remove" // house: take off a film it added
	ListHave   = "have"   // house: whether it has the films on the list
	ListAll    = "all"    // owner: the whole list
)

// Vote ops.
const (
	VoteOpen   = "open"   // house: open a vote (its person hosts it)
	VoteCast   = "cast"   // house: one person's vote (tmdb 0 takes it back)
	VoteBallot = "ballot" // owner: the ballot as it is now
)

// States of a vote.
const (
	VoteIsOpen = "open"   // taking votes
	VoteClosed = "closed" // closed, the winner is being booked
	VoteBooked = "booked" // the winner is booked in the host's Showtime
	VoteNobody = "nobody" // closed with no votes
)

// Club ops.
const (
	ClubAdd    = "add"    // house: start a club
	ClubJoin   = "join"   // house: a person joins (takes a seat in the rotation)
	ClubLeave  = "leave"  // house: a person leaves
	ClubPick   = "pick"   // house: the picker's pick for the next night
	ClubRemove = "remove" // house: end a club it started
	ClubAll    = "all"    // owner: every club
)

// How a club's film is chosen.
const (
	ModePick = "pick" // the person whose turn it is picks
	ModeVote = "vote" // the crew votes
)

// Activity kinds.
const (
	DidFinish = "finished" // finished a film or a season
	DidRate   = "rated"    // gave it stars
	DidWatch  = "watched"  // watched an episode or a film
)

// Film is a film by its TMDB id, as the crew sees it.
type Film struct {
	TMDB   int    `json:"tmdb"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
	Poster string `json:"poster,omitempty"` // a TMDB image path only ("/abc.jpg")
}

// House is one Usherr in a crew, by its number in the crew (the owner is 1).
type House struct {
	H      int      `json:"h"`
	ID     string   `json:"id"` // its link identity (so a house that hosts a vote can invite the houses it is linked with)
	Name   string   `json:"name"`
	State  string   `json:"state"`            // invited, joined
	People []string `json:"people,omitempty"` // who opted in there
}

// Invite is crew.invite: the crew as it is now.
type Invite struct {
	V      int     `json:"v"`
	CID    string  `json:"cid"`
	Rev    int     `json:"rev"`
	Name   string  `json:"name"`
	By     string  `json:"by"`  // the person who made it
	You    int     `json:"you"` // the receiving house's number
	Houses []House `json:"houses"`
}

// Answer is crew.answer.
type Answer struct {
	V      int      `json:"v"`
	CID    string   `json:"cid"`
	Answer string   `json:"answer"`           // join, decline, leave
	People []string `json:"people,omitempty"` // join: who opted in
}

// End is crew.end.
type End struct {
	V   int    `json:"v"`
	CID string `json:"cid"`
}

// Card is a film sent in a chat line: "sent you a film". To is the house it
// was sent to (0: the whole crew) and ToWho the person there.
type Card struct {
	Film
	Note  string `json:"note,omitempty"`
	To    int    `json:"to,omitempty"`
	ToWho string `json:"toWho,omitempty"`
}

// Chat is crew.chat.
type Chat struct {
	V    int    `json:"v"`
	CID  string `json:"cid"`
	MID  string `json:"mid"`         // the line's id (a UUID its writer's Usherr made)
	H    int    `json:"h,omitempty"` // the writer's house (set by the owner)
	Who  string `json:"who"`
	Text string `json:"text"`
	At   int64  `json:"at"`
	Card *Card  `json:"card,omitempty"`
}

// Have is whether one house has a film.
type Have struct {
	H   int    `json:"h"`
	Has string `json:"has"`
}

// Entry is one film on the shared watchlist.
type Entry struct {
	Film
	By    string `json:"by"`  // who added it
	ByH   int    `json:"byH"` // and their house
	Added int64  `json:"added"`
	Have  []Have `json:"have,omitempty"`
}

// HasOf is whether a house has a film on the list ("" when not told yet).
type HasOf struct {
	TMDB int    `json:"tmdb"`
	Has  string `json:"has"`
}

// List is crew.list.
type List struct {
	V     int     `json:"v"`
	CID   string  `json:"cid"`
	Op    string  `json:"op"`
	Film  *Film   `json:"film,omitempty"`  // add, remove
	Who   string  `json:"who,omitempty"`   // add
	Has   []HasOf `json:"has,omitempty"`   // have
	Items []Entry `json:"items,omitempty"` // all
	Seq   int64   `json:"seq,omitempty"`   // all: goes up with every list sent (an older one that comes late is ignored)
}

// Choice is one film on a ballot, and who voted for it.
type Choice struct {
	Film
	Voters []Seat `json:"voters,omitempty"` // who voted for it
	Have   []Have `json:"have,omitempty"`
}

// Ballot is a vote as it stands.
type Ballot struct {
	VID     string   `json:"vid"`
	HostH   int      `json:"hostH"`
	Host    string   `json:"host"`             // the person who opened it
	Closes  int64    `json:"closes"`           // unix ms
	At      int64    `json:"at"`               // when the winner plays, unix ms
	Preshow int      `json:"preshow"`          // minutes
	Club    string   `json:"club,omitempty"`   // the club night it's for
	State   string   `json:"state"`            // open, closed, booked, nobody
	Winner  int      `json:"winner,omitempty"` // the winning film's tmdb
	Choices []Choice `json:"choices"`
	Seq     int64    `json:"seq,omitempty"` // goes up with every ballot sent
}

// Vote is crew.vote.
type Vote struct {
	V      int     `json:"v"`
	CID    string  `json:"cid"`
	Op     string  `json:"op"`
	VID    string  `json:"vid,omitempty"`
	Who    string  `json:"who,omitempty"`    // open: the host; cast: the voter
	TMDB   int     `json:"tmdb,omitempty"`   // cast: the film (0 takes the vote back)
	Films  []Film  `json:"films,omitempty"`  // open
	Closes int64   `json:"closes,omitempty"` // open
	At     int64   `json:"at,omitempty"`     // open
	Pre    int     `json:"pre,omitempty"`    // open: the pre-show's minutes
	Ballot *Ballot `json:"ballot,omitempty"` // ballot
}

// Seat is one person in a club's rotation.
type Seat struct {
	H   int    `json:"h"`
	Who string `json:"who"`
}

// ClubDef is what a club is: its name, night and how its film is chosen.
type ClubDef struct {
	Name    string `json:"name"`
	Weekday int    `json:"weekday"` // 0 Sunday … 6 Saturday, the owner's local time
	Minute  int    `json:"minute"`  // of the day, the owner's local time
	Every   int    `json:"every"`   // weeks: 1 or 2
	Mode    string `json:"mode"`    // pick, vote
	Preshow int    `json:"preshow"` // minutes
}

// ClubState is a club as it stands.
type ClubState struct {
	KID string `json:"kid"`
	ClubDef
	By       string `json:"by"`  // who started it
	ByH      int    `json:"byH"` // and their house
	Next     int64  `json:"next"`
	Rotation []Seat `json:"rotation,omitempty"`
	Turn     int    `json:"turn"`           // index into the rotation: whose pick the next night is
	Pick     *Film  `json:"pick,omitempty"` // the picker's pick for the next night
	Streak   int    `json:"streak"`         // nights in a row that happened
	Nights   int    `json:"nights"`         // nights held
}

// Club is crew.club.
type Club struct {
	V     int         `json:"v"`
	CID   string      `json:"cid"`
	Op    string      `json:"op"`
	KID   string      `json:"kid,omitempty"`
	Who   string      `json:"who,omitempty"`  // add, join, leave, pick
	Def   *ClubDef    `json:"def,omitempty"`  // add
	Film  *Film       `json:"film,omitempty"` // pick
	Clubs []ClubState `json:"clubs,omitempty"`
	Seq   int64       `json:"seq,omitempty"` // all: goes up with every list of clubs sent
}

// Line is one thing a person watched.
type Line struct {
	H     int    `json:"h,omitempty"` // set by the owner
	Who   string `json:"who"`
	Kind  string `json:"kind"`            // finished, rated, watched
	Title string `json:"title"`           // "Severance", "Arrival"
	Part  string `json:"part,omitempty"`  // "S2", "S2E3"
	Stars int    `json:"stars,omitempty"` // rated: 1 to 10 (half stars out of five)
	At    int64  `json:"at"`
}

// Activity is crew.activity.
type Activity struct {
	V     int    `json:"v"`
	CID   string `json:"cid"`
	Lines []Line `json:"lines"`
}

// Book is what the messages do, on this Usherr. Every call is made only
// after the sender was found allowed. hid is the sending house's number in a
// crew this Usherr owns; 0 when the message came from the crew's owner.
type Book interface {
	// OwnerOf says who owns a crew known here: "" and false when it's unknown,
	// mine when this Usherr owns it, else the owner's identity.
	OwnerOf(ctx context.Context, cid string) (owner string, mine bool, err error)
	// HouseOf is a linked Usherr's number and state (invited, joined) in a
	// crew this Usherr owns; 0 and "" when it isn't in it.
	HouseOf(ctx context.Context, cid, peer string) (hid int, state string, err error)

	Invited(ctx context.Context, owner link.Peer, inv Invite) error
	Ended(ctx context.Context, owner link.Peer, cid string) error
	Answered(ctx context.Context, house link.Peer, hid int, a Answer) error
	ChatIn(ctx context.Context, from link.Peer, hid int, c Chat) error
	ListIn(ctx context.Context, from link.Peer, hid int, l List) error
	VoteIn(ctx context.Context, from link.Peer, hid int, v Vote) error
	ClubIn(ctx context.Context, from link.Peer, hid int, c Club) error
	ActivityIn(ctx context.Context, from link.Peer, hid int, a Activity) error
}

// ErrNotAllowed: the sender may not send that about that crew.
var ErrNotAllowed = errors.New("crew: not allowed")

var errBody = errors.New("crew: the body can't be read")

// Register adds the crew messages to a node's registry.
func Register(reg *link.Registry, b Book) error {
	kinds := []link.Kind{
		{Type: TypeInvite, MaxBody: MaxBodyInvite, Handle: func(ctx context.Context, in link.Incoming) error {
			var v Invite
			if decode(in.Msg.Body, &v) != nil || !v.Check() {
				return errBody
			}
			if err := mustOwner(ctx, b, v.CID, in.Peer.ID, true); err != nil {
				return err
			}
			return b.Invited(ctx, in.Peer, v)
		}},
		{Type: TypeEnd, MaxBody: MaxBodySmall, Handle: func(ctx context.Context, in link.Incoming) error {
			var v End
			if decode(in.Msg.Body, &v) != nil || v.V != Version || !ValidID(v.CID) {
				return errBody
			}
			if err := mustOwner(ctx, b, v.CID, in.Peer.ID, false); err != nil {
				return err
			}
			return b.Ended(ctx, in.Peer, v.CID)
		}},
		{Type: TypeAnswer, MaxBody: MaxBodySmall, Handle: func(ctx context.Context, in link.Incoming) error {
			var v Answer
			if decode(in.Msg.Body, &v) != nil || !v.Check() {
				return errBody
			}
			hid, state, err := b.HouseOf(ctx, v.CID, in.Peer.ID)
			if err != nil {
				return err
			}
			if hid <= 1 || state == "" {
				return ErrNotAllowed
			}
			return b.Answered(ctx, in.Peer, hid, v)
		}},
		{Type: TypeChat, MaxBody: MaxBodyChat, Handle: func(ctx context.Context, in link.Incoming) error {
			var v Chat
			if decode(in.Msg.Body, &v) != nil || !v.Check() {
				return errBody
			}
			hid, err := who(ctx, b, v.CID, in.Peer.ID)
			if err != nil {
				return err
			}
			if hid > 0 {
				v.H = hid // the owner says whose house it came from
			}
			return b.ChatIn(ctx, in.Peer, hid, v)
		}},
		{Type: TypeList, MaxBody: MaxBodyList, Handle: func(ctx context.Context, in link.Incoming) error {
			var v List
			if decode(in.Msg.Body, &v) != nil || !v.Check() {
				return errBody
			}
			hid, err := who(ctx, b, v.CID, in.Peer.ID)
			if err != nil {
				return err
			}
			if (hid == 0) != (v.Op == ListAll) {
				return ErrNotAllowed // only the owner sends the whole list; only houses change it
			}
			return b.ListIn(ctx, in.Peer, hid, v)
		}},
		{Type: TypeVote, MaxBody: MaxBodyVote, Handle: func(ctx context.Context, in link.Incoming) error {
			var v Vote
			if decode(in.Msg.Body, &v) != nil || !v.Check() {
				return errBody
			}
			hid, err := who(ctx, b, v.CID, in.Peer.ID)
			if err != nil {
				return err
			}
			if (hid == 0) != (v.Op == VoteBallot) {
				return ErrNotAllowed
			}
			return b.VoteIn(ctx, in.Peer, hid, v)
		}},
		{Type: TypeClub, MaxBody: MaxBodyClub, Handle: func(ctx context.Context, in link.Incoming) error {
			var v Club
			if decode(in.Msg.Body, &v) != nil || !v.Check() {
				return errBody
			}
			hid, err := who(ctx, b, v.CID, in.Peer.ID)
			if err != nil {
				return err
			}
			if (hid == 0) != (v.Op == ClubAll) {
				return ErrNotAllowed
			}
			return b.ClubIn(ctx, in.Peer, hid, v)
		}},
		{Type: TypeActivity, MaxBody: MaxBodyActivity, Handle: func(ctx context.Context, in link.Incoming) error {
			var v Activity
			if decode(in.Msg.Body, &v) != nil || !v.Check() {
				return errBody
			}
			hid, err := who(ctx, b, v.CID, in.Peer.ID)
			if err != nil {
				return err
			}
			for i := range v.Lines {
				if hid > 0 {
					v.Lines[i].H = hid
				}
			}
			return b.ActivityIn(ctx, in.Peer, hid, v)
		}},
	}
	for _, k := range kinds {
		if err := reg.Register(k); err != nil {
			return err
		}
	}
	return nil
}

// who decides who's talking about a crew: its owner (0), or a house that
// joined a crew this Usherr owns (its number). Anyone else is refused.
func who(ctx context.Context, b Book, cid, from string) (int, error) {
	owner, mine, err := b.OwnerOf(ctx, cid)
	if err != nil {
		return 0, err
	}
	if mine {
		hid, state, err := b.HouseOf(ctx, cid, from)
		if err != nil {
			return 0, err
		}
		if hid <= 1 || state != "joined" {
			return 0, ErrNotAllowed
		}
		return hid, nil
	}
	if owner == "" || owner != from {
		return 0, ErrNotAllowed
	}
	return 0, nil
}

// mustOwner: the crew is owned by from (or, when newOK, not known here).
func mustOwner(ctx context.Context, b Book, cid, from string, newOK bool) error {
	owner, mine, err := b.OwnerOf(ctx, cid)
	if err != nil {
		return err
	}
	switch {
	case mine:
		return ErrNotAllowed
	case owner == "" && newOK:
		return nil
	case owner == "" || owner != from:
		return ErrNotAllowed
	}
	return nil
}

func decode(body []byte, v any) error { return link.StrictJSON(body, v) }

// ---- checks: every field in its range; text cleaned (no control
// characters, cut to length). Each returns false for anything malformed. ----

// Check reads an invite.
func (v *Invite) Check() bool {
	if v.V != Version || !ValidID(v.CID) || v.Rev < 1 || v.You < 2 || v.You > MaxHouses || len(v.Houses) == 0 || len(v.Houses) > MaxHouses {
		return false
	}
	if v.Name = CleanText(v.Name, MaxName); v.Name == "" {
		return false
	}
	v.By = CleanText(v.By, MaxWho)
	seen := map[int]bool{}
	you := false
	for i := range v.Houses {
		h := &v.Houses[i]
		if h.H < 1 || h.H > MaxHouses || seen[h.H] || (h.State != "invited" && h.State != "joined") || len(h.People) > MaxPeople {
			return false
		}
		if pub, err := link.DecodeID(h.ID); err != nil || len(pub) != 32 {
			return false
		}
		seen[h.H] = true
		you = you || h.H == v.You
		if h.Name = CleanText(h.Name, MaxName); h.Name == "" {
			h.Name = "Usherr"
		}
		if !cleanNames(h.People) {
			return false
		}
	}
	return you && seen[1]
}

// Check reads an answer.
func (v *Answer) Check() bool {
	if v.V != Version || !ValidID(v.CID) || len(v.People) > MaxPeople {
		return false
	}
	switch v.Answer {
	case Join:
		return cleanNames(v.People)
	case Decline, Leave:
		return len(v.People) == 0
	}
	return false
}

// Check reads a chat line.
func (v *Chat) Check() bool {
	if v.V != Version || !ValidID(v.CID) || !ValidID(v.MID) || v.H < 0 || v.H > MaxHouses || v.At <= 0 {
		return false
	}
	if v.Who = CleanText(v.Who, MaxWho); v.Who == "" {
		return false
	}
	if len([]rune(v.Text)) > MaxText {
		return false
	}
	v.Text = CleanText(v.Text, MaxText)
	if v.Card != nil {
		c := v.Card
		if !c.Film.check() || c.To < 0 || c.To > MaxHouses {
			return false
		}
		c.Note = CleanText(c.Note, 120)
		c.ToWho = CleanText(c.ToWho, MaxWho)
	}
	return v.Text != "" || v.Card != nil
}

// Check reads a list message.
func (v *List) Check() bool {
	if v.V != Version || !ValidID(v.CID) || v.Seq < 0 || (v.Seq != 0 && v.Op != ListAll) {
		return false
	}
	switch v.Op {
	case ListAdd:
		v.Who = CleanText(v.Who, MaxWho)
		return v.Film != nil && v.Film.check() && v.Who != "" && v.Has == nil && v.Items == nil
	case ListRemove:
		return v.Film != nil && v.Film.check() && v.Has == nil && v.Items == nil
	case ListHave:
		if v.Film != nil || len(v.Has) == 0 || len(v.Has) > MaxList || v.Items != nil {
			return false
		}
		for _, h := range v.Has {
			if !validTMDB(h.TMDB) || !validHas(h.Has) {
				return false
			}
		}
		return true
	case ListAll:
		if v.Film != nil || v.Has != nil || len(v.Items) > MaxList {
			return false
		}
		for i := range v.Items {
			e := &v.Items[i]
			if !e.Film.check() || e.ByH < 1 || e.ByH > MaxHouses || e.Added < 0 || !checkHave(e.Have) {
				return false
			}
			e.By = CleanText(e.By, MaxWho)
		}
		return true
	}
	return false
}

// Check reads a vote message.
func (v *Vote) Check() bool {
	if v.V != Version || !ValidID(v.CID) {
		return false
	}
	switch v.Op {
	case VoteOpen:
		if !ValidID(v.VID) || len(v.Films) < 2 || len(v.Films) > MaxBallot || v.Closes <= 0 || v.At <= v.Closes || v.Pre < 0 || v.Pre > MaxPreshow || v.TMDB != 0 || v.Ballot != nil {
			return false
		}
		if v.Who = CleanText(v.Who, MaxWho); v.Who == "" {
			return false
		}
		seen := map[int]bool{}
		for i := range v.Films {
			if !v.Films[i].check() || seen[v.Films[i].TMDB] {
				return false
			}
			seen[v.Films[i].TMDB] = true
		}
		return true
	case VoteCast:
		v.Who = CleanText(v.Who, MaxWho)
		return ValidID(v.VID) && v.Who != "" && (v.TMDB == 0 || validTMDB(v.TMDB)) && v.Films == nil && v.Ballot == nil
	case VoteBallot:
		return v.VID == "" && v.Films == nil && v.Ballot != nil && v.Ballot.check()
	}
	return false
}

func (b *Ballot) check() bool {
	if !ValidID(b.VID) || b.HostH < 1 || b.HostH > MaxHouses || b.Closes <= 0 || b.At <= 0 || b.Preshow < 0 || b.Preshow > MaxPreshow || len(b.Choices) > MaxBallot || b.Seq < 0 {
		return false
	}
	if b.Club != "" && !ValidID(b.Club) {
		return false
	}
	switch b.State {
	case VoteIsOpen, VoteClosed, VoteBooked, VoteNobody:
	default:
		return false
	}
	if b.Winner != 0 && !validTMDB(b.Winner) {
		return false
	}
	b.Host = CleanText(b.Host, MaxWho)
	for i := range b.Choices {
		c := &b.Choices[i]
		if !c.Film.check() || len(c.Voters) > MaxVoters || !checkHave(c.Have) {
			return false
		}
		if !checkSeats(c.Voters) {
			return false
		}
	}
	return true
}

// Check reads a club message.
func (v *Club) Check() bool {
	if v.V != Version || !ValidID(v.CID) || v.Seq < 0 || (v.Seq != 0 && v.Op != ClubAll) {
		return false
	}
	v.Who = CleanText(v.Who, MaxWho)
	switch v.Op {
	case ClubAdd:
		return v.KID == "" && v.Who != "" && v.Def != nil && v.Def.Check() && v.Film == nil && v.Clubs == nil
	case ClubJoin, ClubLeave:
		return ValidID(v.KID) && v.Who != "" && v.Def == nil && v.Film == nil && v.Clubs == nil
	case ClubPick:
		return ValidID(v.KID) && v.Who != "" && v.Def == nil && v.Film != nil && v.Film.check() && v.Clubs == nil
	case ClubRemove:
		return ValidID(v.KID) && v.Def == nil && v.Film == nil && v.Clubs == nil
	case ClubAll:
		if v.KID != "" || v.Def != nil || v.Film != nil || len(v.Clubs) > MaxClubs {
			return false
		}
		for i := range v.Clubs {
			c := &v.Clubs[i]
			if !ValidID(c.KID) || !c.ClubDef.Check() || c.ByH < 1 || c.ByH > MaxHouses || c.Next < 0 || len(c.Rotation) > MaxRotation ||
				c.Turn < 0 || (c.Turn > 0 && c.Turn >= len(c.Rotation)) || c.Streak < 0 || c.Nights < 0 || (c.Pick != nil && !c.Pick.check()) {
				return false
			}
			c.By = CleanText(c.By, MaxWho)
			if !checkSeats(c.Rotation) {
				return false
			}
		}
		return true
	}
	return false
}

// Check reads what a club is.
func (d *ClubDef) Check() bool {
	if d.Weekday < 0 || d.Weekday > 6 || d.Minute < 0 || d.Minute >= 24*60 || (d.Every != 1 && d.Every != 2) || d.Preshow < 0 || d.Preshow > MaxPreshow {
		return false
	}
	if d.Mode != ModePick && d.Mode != ModeVote {
		return false
	}
	d.Name = CleanText(d.Name, MaxName)
	return d.Name != ""
}

// Check reads an activity message.
func (v *Activity) Check() bool {
	if v.V != Version || !ValidID(v.CID) || len(v.Lines) == 0 || len(v.Lines) > MaxLines {
		return false
	}
	for i := range v.Lines {
		l := &v.Lines[i]
		if l.H < 0 || l.H > MaxHouses || l.At <= 0 || l.Stars < 0 || l.Stars > 10 {
			return false
		}
		switch l.Kind {
		case DidFinish, DidWatch:
			if l.Stars != 0 {
				return false
			}
		case DidRate:
			if l.Stars == 0 {
				return false
			}
		default:
			return false
		}
		l.Who, l.Title, l.Part = CleanText(l.Who, MaxWho), CleanText(l.Title, MaxTitle), CleanText(l.Part, 20)
		if l.Who == "" || l.Title == "" {
			return false
		}
	}
	return true
}

var posterPath = regexp.MustCompile(`^/[A-Za-z0-9_-]{1,64}\.(jpg|png)$`)

func (f *Film) check() bool {
	if !validTMDB(f.TMDB) || f.Year < 0 || f.Year > 3000 {
		return false
	}
	if f.Poster != "" && !posterPath.MatchString(f.Poster) {
		f.Poster = ""
	}
	f.Title = CleanText(f.Title, MaxTitle)
	return f.Title != ""
}

func checkHave(hs []Have) bool {
	if len(hs) > MaxHouses {
		return false
	}
	for _, h := range hs {
		if h.H < 1 || h.H > MaxHouses || !validHas(h.Has) {
			return false
		}
	}
	return true
}

func cleanNames(names []string) bool {
	for i := range names {
		if names[i] = CleanText(names[i], MaxWho); names[i] == "" {
			return false
		}
	}
	return true
}

func checkSeats(seats []Seat) bool {
	for i := range seats {
		s := &seats[i]
		if s.H < 1 || s.H > MaxHouses {
			return false
		}
		if s.Who = CleanText(s.Who, MaxWho); s.Who == "" {
			return false
		}
	}
	return true
}

func validTMDB(id int) bool { return id > 0 && id < 1<<24 }

func validHas(s string) bool { return s == HasYes || s == HasGetting || s == HasNo }

// CleanText takes control and format characters out, joins spaces and cuts
// to n characters.
func CleanText(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ' {
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

// ValidID: a crew's, a vote's, a club's or a line's id is a UUID.
func ValidID(s string) bool { return link.ValidMessageID(s) }

// NewID makes an id.
func NewID() string { return link.NewMessageID() }
