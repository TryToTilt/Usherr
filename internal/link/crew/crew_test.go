package crew_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/crew"
	"github.com/rdborg/mediarium/internal/link/relay"
)

// book is a Book in memory that records what it was told.
type book struct {
	mu      sync.Mutex
	owned   map[string]map[string]house // cid -> peer -> its house (owner side)
	ownerOf map[string]string           // cid -> owner identity (house side)
	invites []crew.Invite
	ends    []string
	answers []crew.Answer
	chats   []fromHid
	lists   []fromHid
	votes   []fromHid
	clubs   []fromHid
	acts    []crew.Activity
}

type house struct {
	hid   int
	state string
}

type fromHid struct {
	hid int
	msg any
}

func newBook() *book {
	return &book{owned: map[string]map[string]house{}, ownerOf: map[string]string{}}
}

func (b *book) OwnerOf(_ context.Context, cid string) (string, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.owned[cid]; ok {
		return "", true, nil
	}
	return b.ownerOf[cid], false, nil
}

func (b *book) HouseOf(_ context.Context, cid, peer string) (int, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h := b.owned[cid][peer]
	return h.hid, h.state, nil
}

func (b *book) Invited(_ context.Context, owner link.Peer, inv crew.Invite) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ownerOf[inv.CID] = owner.ID
	b.invites = append(b.invites, inv)
	return nil
}

func (b *book) Ended(_ context.Context, _ link.Peer, cid string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.ownerOf, cid)
	b.ends = append(b.ends, cid)
	return nil
}

func (b *book) Answered(_ context.Context, p link.Peer, hid int, a crew.Answer) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if a.Answer == crew.Join {
		b.owned[a.CID][p.ID] = house{hid, "joined"}
	}
	b.answers = append(b.answers, a)
	return nil
}

func (b *book) ChatIn(_ context.Context, _ link.Peer, hid int, c crew.Chat) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.chats = append(b.chats, fromHid{hid, c})
	return nil
}

func (b *book) ListIn(_ context.Context, _ link.Peer, hid int, l crew.List) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lists = append(b.lists, fromHid{hid, l})
	return nil
}

func (b *book) VoteIn(_ context.Context, _ link.Peer, hid int, v crew.Vote) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.votes = append(b.votes, fromHid{hid, v})
	return nil
}

func (b *book) ClubIn(_ context.Context, _ link.Peer, hid int, c crew.Club) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clubs = append(b.clubs, fromHid{hid, c})
	return nil
}

func (b *book) ActivityIn(_ context.Context, _ link.Peer, _ int, a crew.Activity) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.acts = append(b.acts, a)
	return nil
}

func (b *book) count(f func(*book) int) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return f(b)
}

type usherr struct {
	*link.Node
	id    *link.Identity
	store *link.MemStore
	book  *book
}

func start(t *testing.T, url, name string) *usherr {
	t.Helper()
	id, err := link.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	u := &usherr{id: id, store: link.NewMemStore(), book: newBook()}
	u.Node = link.NewNode(link.NodeOptions{Identity: id, Store: u.store, Relay: url, DefaultRelay: url, Name: func() string { return name }})
	if err := crew.Register(u.Registry(), u.book); err != nil {
		t.Fatal(err)
	}
	u.Start(t.Context())
	t.Cleanup(u.Close)
	if !u.WaitConnected(t.Context(), 5*time.Second) {
		t.Fatalf("%s didn't connect", name)
	}
	return u
}

func pair(t *testing.T, url string, a, b *usherr) {
	ctx := t.Context()
	_ = a.store.SavePeer(ctx, link.Peer{ID: b.id.ID(), XPub: b.id.XPublic(), Name: "b", Relay: url})
	_ = b.store.SavePeer(ctx, link.Peer{ID: a.id.ID(), XPub: a.id.XPublic(), Name: "a", Relay: url})
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

func goRelay(t *testing.T) string {
	r := relay.New(relay.Options{})
	ts := httptest.NewServer(r)
	t.Cleanup(func() { r.Close(); ts.Close() })
	return "ws" + strings.TrimPrefix(ts.URL, "http")
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// The owner invites two houses; they answer, chat, add films, vote and
// join clubs through the owner alone. Nobody else can, and only the owner
// tells the crew how things stand.
func TestCrewMessages(t *testing.T) {
	url := goRelay(t)
	ctx := t.Context()
	owner, a, b, other := start(t, url, "owner"), start(t, url, "a"), start(t, url, "b"), start(t, url, "other")
	pair(t, url, owner, a)
	pair(t, url, owner, b)
	pair(t, url, owner, other)
	pair(t, url, a, b)
	pair(t, url, a, other)

	cid := crew.NewID()
	owner.book.owned[cid] = map[string]house{a.id.ID(): {2, "invited"}, b.id.ID(): {3, "invited"}}
	inv := crew.Invite{V: 1, CID: cid, Rev: 1, Name: "The Friday Crew", By: "Robin", Houses: []crew.House{
		{H: 1, ID: owner.id.ID(), Name: "Robin's Usherr", State: "joined", People: []string{"Robin"}}, {H: 2, ID: a.id.ID(), Name: "A", State: "invited"}, {H: 3, ID: b.id.ID(), Name: "B", State: "invited"}}}
	for i, u := range []*usherr{a, b} {
		inv.You = i + 2
		if err := owner.Send(ctx, u.id.ID(), crew.TypeInvite, inv); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []*usherr{a, b} {
		eventually(t, "the invite arrives", func() bool { return u.book.count(func(b *book) int { return len(b.invites) }) == 1 })
	}
	if a.book.invites[0].Name != "The Friday Crew" || a.book.invites[0].You != 2 || b.book.invites[0].You != 3 {
		t.Fatalf("invites: %+v %+v", a.book.invites, b.book.invites)
	}

	refused := func(u *usherr, before int, what string) {
		t.Helper()
		eventually(t, what, func() bool { return u.Dropped("refused") > before })
	}

	// Before it joins, a house can't chat; an Usherr not invited can't even
	// answer.
	before := owner.Dropped("refused")
	chat := crew.Chat{V: 1, CID: cid, MID: crew.NewID(), Who: "Ann", Text: "hi", At: time.Now().UnixMilli()}
	_ = a.Send(ctx, owner.id.ID(), crew.TypeChat, chat)
	_ = other.Send(ctx, owner.id.ID(), crew.TypeAnswer, crew.Answer{V: 1, CID: cid, Answer: crew.Join, People: []string{"Eve"}})
	refused(owner, before+1, "a chat before joining and a stranger's answer refused")

	for _, u := range []*usherr{a, b} {
		if err := u.Send(ctx, owner.id.ID(), crew.TypeAnswer, crew.Answer{V: 1, CID: cid, Answer: crew.Join, People: []string{"Ann"}}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "both joined", func() bool { return owner.book.count(func(b *book) int { return len(b.answers) }) == 2 })

	// A house's chat reaches the owner with its house number, whatever it
	// said it was.
	chat.MID, chat.H = crew.NewID(), 3
	_ = a.Send(ctx, owner.id.ID(), crew.TypeChat, chat)
	eventually(t, "the chat arrives", func() bool { return owner.book.count(func(b *book) int { return len(b.chats) }) == 1 })
	if c := owner.book.chats[0]; c.hid != 2 || c.msg.(crew.Chat).H != 2 || c.msg.(crew.Chat).Text != "hi" {
		t.Fatalf("chat at the owner: %+v", c)
	}
	// The owner relays it; a house takes it from the owner (hid 0) only.
	relayed := chat
	relayed.H = 2
	_ = owner.Send(ctx, b.id.ID(), crew.TypeChat, relayed)
	eventually(t, "the relayed chat arrives", func() bool { return b.book.count(func(b *book) int { return len(b.chats) }) == 1 })
	if c := b.book.chats[0]; c.hid != 0 || c.msg.(crew.Chat).H != 2 {
		t.Fatalf("chat at b: %+v", c)
	}
	before = b.Dropped("refused")
	relayed.MID = crew.NewID()
	_ = a.Send(ctx, b.id.ID(), crew.TypeChat, relayed) // a and b are linked, but a doesn't own the crew
	_ = a.Send(ctx, b.id.ID(), crew.TypeInvite, inv)   // nor can it change it
	_ = a.Send(ctx, b.id.ID(), crew.TypeEnd, crew.End{V: 1, CID: cid})
	refused(b, before+2, "a non-owner's chat, invite and end refused")
	if n := b.book.count(func(b *book) int { return len(b.chats) + len(b.ends) + len(b.invites) }); n != 2 {
		t.Fatalf("a non-owner got through: %d", n)
	}

	// Ops go one way: only houses change the list, only the owner sends it all.
	film := crew.Film{TMDB: 9426, Title: "The Fly", Year: 1986, Poster: "/fly.jpg"}
	_ = a.Send(ctx, owner.id.ID(), crew.TypeList, crew.List{V: 1, CID: cid, Op: crew.ListAdd, Film: &film, Who: "Ann"})
	eventually(t, "the add arrives", func() bool { return owner.book.count(func(b *book) int { return len(b.lists) }) == 1 })
	before = owner.Dropped("refused")
	_ = a.Send(ctx, owner.id.ID(), crew.TypeList, crew.List{V: 1, CID: cid, Op: crew.ListAll, Items: []crew.Entry{{Film: film, By: "Ann", ByH: 2}}})
	_ = a.Send(ctx, owner.id.ID(), crew.TypeVote, crew.Vote{V: 1, CID: cid, Op: crew.VoteBallot, Ballot: &crew.Ballot{VID: crew.NewID(), HostH: 2, Closes: 1, At: 2, State: crew.VoteIsOpen}})
	refused(owner, before+1, "a house's whole list and ballot refused")
	before = a.Dropped("refused")
	_ = owner.Send(ctx, a.id.ID(), crew.TypeList, crew.List{V: 1, CID: cid, Op: crew.ListAdd, Film: &film, Who: "Robin"})
	refused(a, before, "an owner's add refused")
	_ = owner.Send(ctx, a.id.ID(), crew.TypeList, crew.List{V: 1, CID: cid, Op: crew.ListAll, Items: []crew.Entry{{Film: film, By: "Ann", ByH: 2, Have: []crew.Have{{H: 1, Has: crew.HasYes}}}}})
	eventually(t, "the list arrives", func() bool { return a.book.count(func(b *book) int { return len(b.lists) }) == 1 })

	// Votes, clubs and activity from a house.
	other2 := crew.Film{TMDB: 348, Title: "Alien", Year: 1979}
	_ = b.Send(ctx, owner.id.ID(), crew.TypeVote, crew.Vote{V: 1, CID: cid, Op: crew.VoteOpen, VID: crew.NewID(), Who: "Bo", Films: []crew.Film{film, other2},
		Closes: time.Now().Add(time.Hour).UnixMilli(), At: time.Now().Add(2 * time.Hour).UnixMilli(), Pre: 10})
	_ = b.Send(ctx, owner.id.ID(), crew.TypeClub, crew.Club{V: 1, CID: cid, Op: crew.ClubAdd, Who: "Bo", Def: &crew.ClubDef{Name: "Horror Night", Weekday: 5, Minute: 21*60 + 30, Every: 1, Mode: crew.ModeVote, Preshow: 10}})
	_ = b.Send(ctx, owner.id.ID(), crew.TypeActivity, crew.Activity{V: 1, CID: cid, Lines: []crew.Line{{H: 9, Who: "Bo", Kind: crew.DidRate, Title: "Arrival", Stars: 10, At: time.Now().UnixMilli()}}})
	eventually(t, "vote, club and activity arrive", func() bool {
		return owner.book.count(func(b *book) int { return len(b.votes) + len(b.clubs) + len(b.acts) }) == 3
	})
	if owner.book.votes[0].hid != 3 || owner.book.acts[0].Lines[0].H != 3 {
		t.Fatalf("the house number wasn't set by the owner: %+v %+v", owner.book.votes[0], owner.book.acts[0])
	}

	// Unknown types, oversized bodies and unknown fields are dropped.
	before = a.Dropped("type")
	unk, _ := owner.Seal(ctx, a.id.ID(), link.Message{Type: "crew.bogus", ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: json.RawMessage(`{}`)})
	_ = owner.SendRaw(url, a.id.ID(), unk)
	eventually(t, "an unknown type dropped", func() bool { return a.Dropped("type") > before })
	before = owner.Dropped("size")
	big := chat
	big.Text = strings.Repeat("x", crew.MaxBodyChat)
	bigEnv, _ := a.Seal(ctx, owner.id.ID(), link.Message{Type: crew.TypeChat, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: mustJSON(big)})
	_ = a.SendRaw(url, owner.id.ID(), bigEnv)
	eventually(t, "an oversized chat dropped", func() bool { return owner.Dropped("size") > before })
	before = a.Dropped("refused")
	odd, _ := owner.Seal(ctx, a.id.ID(), link.Message{Type: crew.TypeEnd, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: json.RawMessage(`{"v":1,"cid":"` + cid + `","why":"x"}`)})
	_ = owner.SendRaw(url, a.id.ID(), odd)
	refused(a, before, "a body with an unknown field refused")

	// The owner ends it.
	_ = owner.Send(ctx, a.id.ID(), crew.TypeEnd, crew.End{V: 1, CID: cid})
	eventually(t, "ended", func() bool { return a.book.count(func(b *book) int { return len(b.ends) }) == 1 })
}

// The checks: malformed bodies are refused, text is cleaned.
func TestCrewChecks(t *testing.T) {
	cid, now := crew.NewID(), time.Now().UnixMilli()
	me, _ := link.NewIdentity()
	x := me.ID()
	chat := func(text string) *crew.Chat {
		return &crew.Chat{V: 1, CID: cid, MID: crew.NewID(), Who: "Dana", Text: text, At: now}
	}
	if c := chat("  hello\u0000 <b>there</b>\n  "); !c.Check() || c.Text != "hello <b>there</b>" {
		t.Fatalf("cleaned: %q", c.Text) // shown as plain text: the page escapes it
	}
	if chat(strings.Repeat("é", crew.MaxText)).Check() != true || chat(strings.Repeat("é", crew.MaxText+1)).Check() {
		t.Fatal("500 characters is the limit")
	}
	if chat("   ").Check() {
		t.Fatal("an empty line")
	}
	c := chat("")
	c.Card = &crew.Card{Film: crew.Film{TMDB: 1, Title: "x", Poster: "https://evil/x.jpg"}, To: 2}
	if !c.Check() || c.Card.Poster != "" {
		t.Fatal("a card alone is a line; a poster that isn't a TMDB path is dropped")
	}
	c = chat("x")
	c.V = 2
	if c.Check() {
		t.Fatal("another version")
	}
	c = chat("x")
	c.CID = "not-a-uuid"
	if c.Check() {
		t.Fatal("a bad crew id")
	}

	bad := []interface{ Check() bool }{
		&crew.Invite{V: 1, CID: cid, Rev: 1, Name: "x", You: 1, Houses: []crew.House{{H: 1, ID: x, Name: "a", State: "joined"}}},                       // you can't be the owner
		&crew.Invite{V: 1, CID: cid, Rev: 1, Name: "x", You: 2, Houses: []crew.House{{H: 2, ID: x, Name: "a", State: "joined"}}},                       // no owner
		&crew.Invite{V: 1, CID: cid, Rev: 1, Name: " ", You: 2, Houses: []crew.House{{H: 1, ID: x, State: "joined"}, {H: 2, ID: x, State: "invited"}}}, // no name
		&crew.Invite{V: 1, CID: cid, Rev: 1, Name: "x", You: 2, Houses: []crew.House{{H: 1, ID: x, State: "joined"}, {H: 2, ID: x, State: "boss"}}},    // a state
		&crew.Answer{V: 1, CID: cid, Answer: crew.Leave, People: []string{"x"}},                                                                        // leave with people
		&crew.Answer{V: 1, CID: cid, Answer: "maybe"},                                                                                                                       // an answer
		&crew.List{V: 1, CID: cid, Op: crew.ListHave, Has: []crew.HasOf{{TMDB: 1, Has: "lots"}}},                                                                            // a has
		&crew.List{V: 1, CID: cid, Op: crew.ListAdd, Film: &crew.Film{TMDB: 0, Title: "x"}, Who: "x"},                                                                       // a tmdb id
		&crew.List{V: 1, CID: cid, Op: "drop"},                                                                                                                              // an op
		&crew.Vote{V: 1, CID: cid, Op: crew.VoteOpen, VID: cid, Who: "x", Films: []crew.Film{{TMDB: 1, Title: "a"}}, Closes: now, At: now + 1},                              // one film
		&crew.Vote{V: 1, CID: cid, Op: crew.VoteOpen, VID: cid, Who: "x", Films: []crew.Film{{TMDB: 1, Title: "a"}, {TMDB: 1, Title: "a"}}, Closes: now, At: now + 1},       // twice
		&crew.Vote{V: 1, CID: cid, Op: crew.VoteOpen, VID: cid, Who: "x", Films: []crew.Film{{TMDB: 1, Title: "a"}, {TMDB: 2, Title: "b"}}, Closes: now, At: now},           // plays before it closes
		&crew.Club{V: 1, CID: cid, Op: crew.ClubAdd, Who: "x", Def: &crew.ClubDef{Name: "x", Weekday: 7, Every: 1, Mode: crew.ModePick}},                                    // a weekday
		&crew.Club{V: 1, CID: cid, Op: crew.ClubAdd, Who: "x", Def: &crew.ClubDef{Name: "x", Weekday: 1, Every: 3, Mode: crew.ModePick}},                                    // every three weeks
		&crew.Club{V: 1, CID: cid, Op: crew.ClubAll, Clubs: []crew.ClubState{{KID: cid, ClubDef: crew.ClubDef{Name: "x", Every: 1, Mode: crew.ModeVote}, ByH: 1, Turn: 2}}}, // a turn past the rotation
		&crew.Activity{V: 1, CID: cid, Lines: []crew.Line{{Who: "x", Kind: crew.DidRate, Title: "x", At: now}}},                                                             // rated without stars
		&crew.Activity{V: 1, CID: cid, Lines: []crew.Line{{Who: "x", Kind: "searched", Title: "x", At: now}}},                                                               // a kind
		&crew.Activity{V: 1, CID: cid}, // no lines
		&crew.Invite{V: 1, CID: cid, Rev: 1, Name: "Crew", You: 2, Houses: []crew.House{{H: 1, ID: x, Name: "a", State: "joined"}, {H: 2, ID: "nope", Name: "b", State: "invited"}}}, // an identity
	}
	for i, m := range bad {
		if m.Check() {
			t.Errorf("%d: %T taken: %+v", i, m, m)
		}
	}
	good := []interface{ Check() bool }{
		&crew.Invite{V: 1, CID: cid, Rev: 1, Name: "Crew", You: 2, Houses: []crew.House{{H: 1, ID: x, Name: "a", State: "joined", People: []string{"Robin"}}, {H: 2, ID: x, Name: "b", State: "invited"}}},
		&crew.Answer{V: 1, CID: cid, Answer: crew.Join, People: []string{"Ann", "Bo"}},
		&crew.Vote{V: 1, CID: cid, Op: crew.VoteCast, VID: cid, Who: "Ann", TMDB: 0},
		&crew.Club{V: 1, CID: cid, Op: crew.ClubPick, KID: cid, Who: "Ann", Film: &crew.Film{TMDB: 5, Title: "Five"}},
		&crew.Activity{V: 1, CID: cid, Lines: []crew.Line{{Who: "Carl", Kind: crew.DidFinish, Title: "Severance", Part: "S2", At: now}}},
	}
	for i, m := range good {
		if !m.Check() {
			t.Errorf("%d: %T refused: %+v", i, m, m)
		}
	}
}
