package rewards_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/relay"
	"github.com/rdborg/mediarium/internal/link/rewards"
)

// book is a Book in memory that records what it was told.
type book struct {
	mu      sync.Mutex
	me      string
	hosted  map[string]map[string][]int // sid -> invited identity -> seats handed to it
	guestOf map[string]string           // sid -> host identity
	stubs   []rewards.Stub
	agreed  []rewards.Stamp
	stamps  map[string][]int // from -> seats stamped
}

func newBook() *book {
	return &book{hosted: map[string]map[string][]int{}, guestOf: map[string]string{}, stamps: map[string][]int{}}
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
	_, ok := b.hosted[sid][guest]
	return ok, nil
}

func (b *book) Holds(_ context.Context, sid, guest string, seats []int) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	have := map[int]bool{}
	for _, n := range b.hosted[sid][guest] {
		have[n] = true
	}
	for _, n := range seats {
		if !have[n] {
			return false, nil
		}
	}
	return true, nil
}

func (b *book) StubsIn(_ context.Context, _ link.Peer, s rewards.Stub) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stubs = append(b.stubs, s)
	return nil
}

func (b *book) StampsAgreed(_ context.Context, _ link.Peer, s rewards.Stamp) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.agreed = append(b.agreed, s)
	return nil
}

func (b *book) StampsIn(_ context.Context, guest link.Peer, s rewards.Stamp) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stamps[guest.ID] = append(b.stamps[guest.ID], s.Seats...)
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
	u.book.me = id.ID()
	u.Node = link.NewNode(link.NodeOptions{Identity: id, Store: u.store, Relay: url, DefaultRelay: url, Name: func() string { return name }})
	if err := rewards.Register(u.Registry(), u.book); err != nil {
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

func TestRewardMessages(t *testing.T) {
	url := goRelay(t)
	ctx := t.Context()
	host, g1, g2 := start(t, url, "host"), start(t, url, "g1"), start(t, url, "g2")
	pair(t, url, host, g1)
	pair(t, url, host, g2)
	pair(t, url, g1, g2)

	sid := link.NewMessageID()
	host.book.hosted[sid] = map[string][]int{g1.id.ID(): {3, 4}, g2.id.ID(): {5}}
	g1.book.guestOf[sid] = host.id.ID()
	g2.book.guestOf[sid] = host.id.ID()

	// The host hands out the stubs at the door.
	stub := rewards.Stub{V: 1, SID: sid, Film: rewards.Film{TMDB: 1091, Title: "The\u0000 Thing ", Year: 1982, Runtime: 109, Poster: "javascript:alert(1)"},
		At: time.Now().UnixMilli(), Host: "Robin", Seat: 3, Count: 2, No: 47, With: []string{"Dana", " ", "g2"}}
	if err := host.Send(ctx, g1.id.ID(), rewards.TypeStub, stub); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the stubs arrive", func() bool { return g1.book.count(func(b *book) int { return len(b.stubs) }) == 1 })
	if s := g1.book.stubs[0]; s.Film.Title != "The Thing" || s.Film.Poster != "" || s.No != 47 || s.Count != 2 || len(s.With) != 2 {
		t.Fatalf("stub: %+v", s)
	}

	refused := func(u *usherr, reason string, before int, what string) {
		t.Helper()
		eventually(t, what, func() bool { return u.Dropped(reason) > before })
	}

	// Only the host hands out stubs: g2 can't give g1 one for the host's
	// screening.
	before := g1.Dropped("refused")
	_ = g2.Send(ctx, g1.id.ID(), rewards.TypeStub, stub)
	refused(g1, "refused", before, "a stub from a non-host refused")

	// A guest stamps its own seats; the host takes them and says back what
	// it holds.
	if err := g1.Send(ctx, host.id.ID(), rewards.TypeStamp, rewards.Stamp{V: 1, SID: sid, Seats: []int{4}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the stamp reaches the host", func() bool { return host.book.count(func(b *book) int { return len(b.stamps[g1.id.ID()]) }) == 1 })
	if err := host.Send(ctx, g1.id.ID(), rewards.TypeStamp, rewards.Stamp{V: 1, SID: sid, Seats: []int{4}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the host's record reaches the guest", func() bool { return g1.book.count(func(b *book) int { return len(b.agreed) }) == 1 })

	// A guest can't stamp another guest's seat, nor can an Usherr that
	// isn't invited; a guest can't confirm stamps to another guest.
	before = host.Dropped("refused")
	_ = g1.Send(ctx, host.id.ID(), rewards.TypeStamp, rewards.Stamp{V: 1, SID: sid, Seats: []int{5}})
	refused(host, "refused", before, "a stamp on someone else's seat refused")
	before = g1.Dropped("refused")
	_ = g2.Send(ctx, g1.id.ID(), rewards.TypeStamp, rewards.Stamp{V: 1, SID: sid, Seats: []int{3}})
	refused(g1, "refused", before, "a confirmation from a non-host refused")
	if n := host.book.count(func(b *book) int { return len(b.stamps[g1.id.ID()]) }); n != 1 {
		t.Fatalf("stamps at the host: %d", n)
	}

	// Malformed: seats twice, out of range, empty from a guest, an unknown
	// field, an old version, an oversized body, a reward sealed with a
	// screening key.
	for _, body := range []json.RawMessage{
		mustJSON(rewards.Stamp{V: 1, SID: sid, Seats: []int{3, 3}}),
		mustJSON(rewards.Stamp{V: 1, SID: sid, Seats: []int{0}}),
		mustJSON(rewards.Stamp{V: 1, SID: sid, Seats: []int{}}),
		mustJSON(rewards.Stamp{V: 2, SID: sid, Seats: []int{3}}),
		json.RawMessage(`{"v":1,"sid":"` + sid + `","seats":[3],"stars":5}`),
	} {
		before = host.Dropped("refused")
		env, _ := g1.Seal(ctx, host.id.ID(), link.Message{Type: rewards.TypeStamp, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: body})
		_ = g1.SendRaw(url, host.id.ID(), env)
		refused(host, "refused", before, "a malformed stamp refused: "+string(body))
	}
	for _, bad := range []rewards.Stub{
		{V: 1, SID: sid, Film: rewards.Film{TMDB: 1, Title: "x"}, At: 1, Seat: 0, Count: 1, No: 1},
		{V: 1, SID: sid, Film: rewards.Film{TMDB: 1, Title: "x"}, At: 1, Seat: 1, Count: rewards.MaxBlock + 1, No: 1},
		{V: 1, SID: sid, Film: rewards.Film{TMDB: 1, Title: " "}, At: 1, Seat: 1, Count: 1, No: 1},
		{V: 1, SID: "not-a-sid", Film: rewards.Film{TMDB: 1, Title: "x"}, At: 1, Seat: 1, Count: 1, No: 1},
	} {
		before = g1.Dropped("refused")
		_ = host.Send(ctx, g1.id.ID(), rewards.TypeStub, bad)
		refused(g1, "refused", before, "a malformed stub refused")
	}
	before = g1.Dropped("size")
	big := stub
	big.Host = strings.Repeat("x", rewards.MaxBodyStub)
	env, _ := host.Seal(ctx, g1.id.ID(), link.Message{Type: rewards.TypeStub, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: mustJSON(big)})
	_ = host.SendRaw(url, g1.id.ID(), env)
	refused(g1, "size", before, "an oversized stub dropped")
	before = g1.Dropped("type")
	key := link.NewGroupKey()
	genv, _ := link.SealGroup(host.id, key, link.Message{Type: rewards.TypeStub, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: mustJSON(stub)})
	_ = host.SendRaw(url, g1.id.ID(), genv)
	eventually(t, "a stub sealed with a screening key dropped", func() bool {
		return g1.Dropped("type")+g1.Dropped("group") > before
	})
	if n := g1.book.count(func(b *book) int { return len(b.stubs) }); n != 1 {
		t.Fatalf("stubs at g1: %d", n)
	}
}

func TestRegisterOnce(t *testing.T) {
	reg := link.NewRegistry()
	if err := rewards.Register(reg, newBook()); err != nil {
		t.Fatal(err)
	}
	if err := rewards.Register(reg, newBook()); err == nil {
		t.Fatal("registered twice")
	}
	for _, typ := range []string{rewards.TypeStub, rewards.TypeStamp} {
		if k, ok := reg.Lookup(typ); !ok || k.Group {
			t.Fatalf("%s: %+v", typ, k)
		}
	}
}
