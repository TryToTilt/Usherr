package screening_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/relay"
	"github.com/rdborg/mediarium/internal/link/screening"
)

// book is a Book in memory that records what it was told.
type book struct {
	mu      sync.Mutex
	me      string
	hosted  map[string]map[string]bool // sid -> invited identities
	guestOf map[string]string          // sid -> host identity
	keys    map[string][]byte          // sid -> screening key (guest side)
	offers  []screening.Offer
	cancels []string
	rosters []screening.Roster
	rsvps   map[string]screening.RSVP // from -> last rsvp
	readies map[string]screening.Ready
}

func newBook() *book {
	return &book{hosted: map[string]map[string]bool{}, guestOf: map[string]string{}, keys: map[string][]byte{}, rsvps: map[string]screening.RSVP{}, readies: map[string]screening.Ready{}}
}

func (b *book) HostOf(_ context.Context, sid string) (string, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.hosted[sid]; ok {
		return b.me, true, nil
	}
	return b.guestOf[sid], false, nil
}

func (b *book) Invited(_ context.Context, sid, guest string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hosted[sid][guest], nil
}

func (b *book) Offered(_ context.Context, host link.Peer, o screening.Offer, key []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.guestOf[o.SID] = host.ID
	b.keys[o.SID] = key
	b.offers = append(b.offers, o)
	return nil
}

func (b *book) Cancelled(_ context.Context, _ link.Peer, sid string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.guestOf, sid)
	delete(b.keys, sid)
	b.cancels = append(b.cancels, sid)
	return nil
}

func (b *book) RosterIn(_ context.Context, _ link.Peer, r screening.Roster) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rosters = append(b.rosters, r)
	return nil
}

func (b *book) RSVPIn(_ context.Context, guest link.Peer, r screening.RSVP) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rsvps[guest.ID] = r
	return nil
}

func (b *book) ReadyIn(_ context.Context, guest link.Peer, r screening.Ready) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.readies[guest.ID] = r
	return nil
}

func (b *book) groupKey(_ context.Context, kid []byte) (link.GroupKey, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sid, k := range b.keys {
		if bytes.Equal(link.GroupKID(k), kid) {
			return link.GroupKey{Key: k, Owner: b.guestOf[sid], Scope: sid}, true
		}
	}
	return link.GroupKey{}, false
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
	room  *room
}

func start(t *testing.T, url, name string) *usherr {
	t.Helper()
	id, err := link.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	u := &usherr{id: id, store: link.NewMemStore(), book: newBook(), room: &room{}}
	u.book.me = id.ID()
	u.Node = link.NewNode(link.NodeOptions{Identity: id, Store: u.store, Relay: url, DefaultRelay: url, Name: func() string { return name }, GroupKey: u.book.groupKey})
	if err := screening.Register(u.Registry(), u.book); err != nil {
		t.Fatal(err)
	}
	if err := screening.RegisterRoom(u.Registry(), u.book, u.room); err != nil {
		t.Fatal(err)
	}
	u.Start(t.Context())
	t.Cleanup(u.Close)
	if !u.WaitConnected(t.Context(), 5*time.Second) {
		t.Fatalf("%s didn't connect", name)
	}
	return u
}

// link two Usherrs directly (the handshake is tested in internal/link).
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

func goRelay(t *testing.T, opt relay.Options) string {
	r := relay.New(opt)
	ts := httptest.NewServer(r)
	t.Cleanup(func() { r.Close(); ts.Close() })
	return "ws" + strings.TrimPrefix(ts.URL, "http")
}

func TestScreeningMessages(t *testing.T) {
	for name, opt := range map[string]relay.Options{"fanout": {}, "an older relay": {NoFanout: true}} {
		t.Run(name, func(t *testing.T) { screeningFlow(t, goRelay(t, opt)) })
	}
	// The same through another relay (the Worker under wrangler dev).
	if u := os.Getenv("ULR_TEST_URL"); u != "" {
		t.Run("ULR_TEST_URL", func(t *testing.T) { screeningFlow(t, strings.TrimRight(u, "/")) })
	}
}

func screeningFlow(t *testing.T, url string) {
	ctx := t.Context()
	host, g1, g2, other := start(t, url, "host"), start(t, url, "g1"), start(t, url, "g2"), start(t, url, "other")
	pair(t, url, host, g1)
	pair(t, url, host, g2)
	pair(t, url, host, other)
	pair(t, url, g1, g2)

	sid := screening.NewSID()
	key := link.NewGroupKey()
	host.book.hosted[sid] = map[string]bool{g1.id.ID(): true, g2.id.ID(): true}
	offer := screening.Offer{V: 1, SID: sid, Rev: 1, Film: screening.Film{TMDB: 603, Title: "The Matrix", Year: 1999, Runtime: 136, Poster: "https://image.tmdb.org/t/p/w500/x.jpg"},
		At: time.Now().Add(2 * time.Hour).UnixMilli(), Preshow: 10, Host: "Robin", Key: base64.StdEncoding.EncodeToString(key)}
	for _, g := range []*usherr{g1, g2} {
		if err := host.Send(ctx, g.id.ID(), screening.TypeOffer, offer); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []*usherr{g1, g2} {
		eventually(t, "the offer arrives", func() bool { return g.book.count(func(b *book) int { return len(b.offers) }) == 1 })
		if o := g.book.offers[0]; o.Film.Title != "The Matrix" || !bytes.Equal(g.book.keys[sid], key) || g.book.guestOf[sid] != host.id.ID() {
			t.Fatalf("offer: %+v", o)
		}
	}

	// Guests answer the host; the host tells everyone at once.
	if err := g1.Send(ctx, host.id.ID(), screening.TypeRSVP, screening.RSVP{V: 1, SID: sid, Answer: screening.Going, Count: 3}); err != nil {
		t.Fatal(err)
	}
	if err := g2.Send(ctx, host.id.ID(), screening.TypeReady, screening.Ready{V: 1, SID: sid, Rev: 1, Has: screening.HasYes, Runtime: 140}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the host has both answers", func() bool {
		return host.book.count(func(b *book) int { return len(b.rsvps) + len(b.readies) }) == 2
	})
	if r := host.book.rsvps[g1.id.ID()]; r.Answer != screening.Going || r.Count != 3 {
		t.Fatalf("rsvp: %+v", r)
	}
	roster := screening.Roster{V: 1, SID: sid, Rev: 1, Guests: []screening.Guest{{Name: "g1", Answer: screening.Going, Count: 3}, {Name: "g2"}}}
	if err := host.SendGroup(ctx, key, []string{g1.id.ID(), g2.id.ID()}, screening.TypeRoster, roster); err != nil {
		t.Fatal(err)
	}
	for _, g := range []*usherr{g1, g2} {
		eventually(t, "the roster arrives", func() bool { return g.book.count(func(b *book) int { return len(b.rosters) }) == 1 })
		if r := g.book.rosters[0]; len(r.Guests) != 2 || r.Guests[0].Count != 3 {
			t.Fatalf("roster: %+v", r)
		}
	}

	refused := func(u *usherr, reason string, before int, what string) {
		t.Helper()
		eventually(t, what, func() bool { return u.Dropped(reason) > before })
	}

	// An Usherr that isn't invited can't answer.
	before := host.Dropped("refused")
	if err := other.Send(ctx, host.id.ID(), screening.TypeRSVP, screening.RSVP{V: 1, SID: sid, Answer: screening.Going, Count: 1}); err != nil {
		t.Fatal(err)
	}
	if err := other.Send(ctx, host.id.ID(), screening.TypeReady, screening.Ready{V: 1, SID: sid, Has: screening.HasNo}); err != nil {
		t.Fatal(err)
	}
	refused(host, "refused", before+1, "an uninvited Usherr's rsvp and ready refused")
	if _, ok := host.book.rsvps[other.id.ID()]; ok {
		t.Fatal("an uninvited Usherr's rsvp was taken")
	}

	// Only the host offers, changes or cancels: g2 can't change or cancel
	// the host's screening at g1, and the host takes no offer of its own.
	before = g1.Dropped("refused")
	fake := offer
	fake.Film.Title = "Something else"
	_ = g2.Send(ctx, g1.id.ID(), screening.TypeUpdate, fake)
	_ = g2.Send(ctx, g1.id.ID(), screening.TypeCancel, screening.Cancel{V: 1, SID: sid})
	refused(g1, "refused", before+1, "a non-host's update and cancel refused")
	if n := g1.book.count(func(b *book) int { return len(b.offers) + len(b.cancels) }); n != 1 || g1.book.guestOf[sid] != host.id.ID() {
		t.Fatalf("a non-host changed the screening: %d", n)
	}
	before = host.Dropped("refused")
	_ = g1.Send(ctx, host.id.ID(), screening.TypeOffer, offer)
	refused(host, "refused", before, "an offer of the host's own screening refused")

	// g2 has the screening key, but a roster sealed with it by anyone but
	// the host is dropped.
	before = g1.Dropped("sender")
	env, _ := link.SealGroup(g2.id, key, link.Message{Type: screening.TypeRoster, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: mustJSON(roster)})
	_ = g2.SendRaw(url, g1.id.ID(), env)
	refused(g1, "sender", before, "a roster sealed by a guest dropped")

	// A roster can't come in the pair's envelope; an offer can't come sealed
	// with a screening key.
	if err := host.Send(ctx, g1.id.ID(), screening.TypeRoster, roster); err != link.ErrUnknownType {
		t.Fatalf("a roster sent pairwise: %v", err)
	}
	before = g1.Dropped("type")
	pairEnv, _ := host.Seal(ctx, g1.id.ID(), link.Message{Type: screening.TypeRoster, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: mustJSON(roster)})
	_ = host.SendRaw(url, g1.id.ID(), pairEnv)
	groupOffer, _ := link.SealGroup(host.id, key, link.Message{Type: screening.TypeOffer, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: mustJSON(offer)})
	_ = host.SendRaw(url, g1.id.ID(), groupOffer)
	refused(g1, "type", before+1, "both dropped by type")

	// Unknown types and oversized bodies are dropped before any handler;
	// a body with an unknown field is refused.
	before = g1.Dropped("type")
	unk, _ := host.Seal(ctx, g1.id.ID(), link.Message{Type: "screening.bogus", ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: json.RawMessage(`{}`)})
	_ = host.SendRaw(url, g1.id.ID(), unk)
	refused(g1, "type", before, "an unknown type dropped")
	before = g1.Dropped("size")
	big := offer
	big.Host = strings.Repeat("x", screening.MaxBodyOffer)
	bigEnv, _ := host.Seal(ctx, g1.id.ID(), link.Message{Type: screening.TypeUpdate, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: mustJSON(big)})
	_ = host.SendRaw(url, g1.id.ID(), bigEnv)
	refused(g1, "size", before, "an oversized offer dropped")
	before = g1.Dropped("refused")
	odd, _ := host.Seal(ctx, g1.id.ID(), link.Message{Type: screening.TypeCancel, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: json.RawMessage(`{"v":1,"sid":"` + sid + `","why":"x"}`)})
	_ = host.SendRaw(url, g1.id.ID(), odd)
	refused(g1, "refused", before, "a body with an unknown field refused")
	if len(g1.book.cancels) != 0 {
		t.Fatal("a cancel with an unknown field was taken")
	}

	// The host cancels: gone at the guest.
	_ = host.Send(ctx, g1.id.ID(), screening.TypeCancel, screening.Cancel{V: 1, SID: sid})
	eventually(t, "cancelled", func() bool { return g1.book.count(func(b *book) int { return len(b.cancels) }) == 1 })

}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
