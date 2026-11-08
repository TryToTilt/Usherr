package screening_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/relay"
	"github.com/rdborg/mediarium/internal/link/screening"
)

// chatRec is a Chat in memory that records what it was told.
type chatRec struct {
	mu     sync.Mutex
	from   []screening.ChatSend
	in     []screening.ChatMsg
	rfrom  []screening.ReactSend
	rin    []screening.ReactMoment
	fromBy []string
}

func (c *chatRec) ChatFrom(_ context.Context, guest link.Peer, m screening.ChatSend) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.from = append(c.from, m)
	c.fromBy = append(c.fromBy, guest.ID)
	return nil
}

func (c *chatRec) ChatIn(_ context.Context, _ link.Peer, m screening.ChatMsg) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.in = append(c.in, m)
	return nil
}

func (c *chatRec) ReactFrom(_ context.Context, _ link.Peer, r screening.ReactSend) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rfrom = append(c.rfrom, r)
	return nil
}

func (c *chatRec) ReactIn(_ context.Context, _ link.Peer, r screening.ReactMoment) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rin = append(c.rin, r)
	return nil
}

func (c *chatRec) count(f func(*chatRec) int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return f(c)
}

// Chat goes through the host: an invited guest's chat.send reaches the
// host (cleaned), the host's chat.msg reaches every guest, a guest can't
// pass a chat.msg off as the host's, an uninvited Usherr can't chat, and
// reactions are one of the five words.
func TestChatThroughHost(t *testing.T) {
	url := goRelay(t, relay.Options{})
	ctx := t.Context()
	host, g1, g2, other := start(t, url, "host"), start(t, url, "g1"), start(t, url, "g2"), start(t, url, "other")
	pair(t, url, host, g1)
	pair(t, url, host, g2)
	pair(t, url, host, other)
	pair(t, url, g1, g2)

	sid := screening.NewSID()
	key := link.NewGroupKey()
	host.book.hosted[sid] = map[string]bool{g1.id.ID(): true, g2.id.ID(): true}
	for _, g := range []*usherr{g1, g2} {
		g.book.guestOf[sid] = host.id.ID()
		g.book.keys[sid] = key
	}

	// A guest says something: to the host, cleaned.
	say := screening.ChatSend{V: 1, SID: sid, ID: link.NewMessageID(), Name: "Sam\u0000", Text: "  <b>that dog</b>\nis NOT a dog‮ ", Spoiler: true, Pos: 2290.5}
	if err := g1.Send(ctx, host.id.ID(), screening.TypeChatSend, say); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the chat reaches the host", func() bool { return host.chat.count(func(c *chatRec) int { return len(c.from) }) == 1 })
	if got := host.chat.from[0]; got.Text != "<b>that dog</b> is NOT a dog" || got.Name != "Sam" || !got.Spoiler || got.Pos != 2290.5 || host.chat.fromBy[0] != g1.id.ID() {
		t.Fatalf("chat at the host: %+v", got)
	}

	// Only by the pair's envelope: never as a group message.
	if err := g1.SendGroup(ctx, key, []string{host.id.ID()}, screening.TypeChatSend, say); err != link.ErrUnknownType {
		t.Fatalf("chat.send as a group message: %v", err)
	}

	// Not invited: refused.
	before := host.Dropped("refused")
	_ = other.Send(ctx, host.id.ID(), screening.TypeChatSend, screening.ChatSend{V: 1, SID: sid, ID: link.NewMessageID(), Name: "Eve", Text: "hi", Pos: 1})
	eventually(t, "an uninvited chat refused", func() bool { return host.Dropped("refused") > before })

	// Too long, empty, not a reaction: refused.
	before = host.Dropped("refused")
	_ = g1.Send(ctx, host.id.ID(), screening.TypeChatSend, screening.ChatSend{V: 1, SID: sid, ID: link.NewMessageID(), Name: "Sam", Text: strings.Repeat("a", 1200), Pos: 1})
	_ = g1.Send(ctx, host.id.ID(), screening.TypeChatSend, screening.ChatSend{V: 1, SID: sid, ID: link.NewMessageID(), Name: "Sam", Text: " ​ ", Pos: 1})
	_ = g1.Send(ctx, host.id.ID(), screening.TypeReactSend, screening.ReactSend{V: 1, SID: sid, ID: link.NewMessageID(), Name: "Sam", Word: "LOL", Pos: 1})
	eventually(t, "bad chat refused", func() bool { return host.Dropped("refused") >= before+3 })

	// The host passes it on to everyone.
	msg := screening.ChatMsg{V: 1, SID: sid, ID: say.ID, Name: "Sam", House: "Sam's Usherr", Text: "that dog is NOT a dog", Spoiler: true, Pos: 2290.5, At: time.Now().UnixMilli()}
	if err := host.SendGroup(ctx, key, []string{g1.id.ID(), g2.id.ID()}, screening.TypeChatMsg, msg); err != nil {
		t.Fatal(err)
	}
	for _, g := range []*usherr{g1, g2} {
		eventually(t, "the chat reaches the guests", func() bool { return g.chat.count(func(c *chatRec) int { return len(c.in) }) == 1 })
		if got := g.chat.in[0]; got.Text != msg.Text || got.House != "Sam's Usherr" || !got.Spoiler {
			t.Fatalf("chat at a guest: %+v", got)
		}
	}
	// A guest has the key, but isn't the host: dropped.
	before = g2.Dropped("sender")
	forged := msg
	forged.ID, forged.Text = link.NewMessageID(), "the host says: go to evil.example"
	_ = g1.SendGroup(ctx, key, []string{g2.id.ID()}, screening.TypeChatMsg, forged)
	eventually(t, "a guest's chat.msg dropped", func() bool { return g2.Dropped("sender") > before })

	// Reactions: to the host, then to everyone.
	if err := g2.Send(ctx, host.id.ID(), screening.TypeReactSend, screening.ReactSend{V: 1, SID: sid, ID: link.NewMessageID(), Name: "Alex", Word: "EEK", Pos: 4360}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the reaction reaches the host", func() bool { return host.chat.count(func(c *chatRec) int { return len(c.rfrom) }) == 1 })
	moment := screening.ReactMoment{V: 1, SID: sid, ID: link.NewMessageID(), Name: "Alex", House: "Alex's Usherr", Word: "EEK", Pos: 4360, At: time.Now().UnixMilli()}
	_ = host.SendGroup(ctx, key, []string{g1.id.ID()}, screening.TypeReactMoment, moment)
	eventually(t, "the reaction reaches a guest", func() bool { return g1.chat.count(func(c *chatRec) int { return len(c.rin) }) == 1 })
	before = g1.Dropped("refused")
	bad := moment
	bad.ID, bad.Word = link.NewMessageID(), "<script>"
	_ = host.SendGroup(ctx, key, []string{g1.id.ID()}, screening.TypeReactMoment, bad)
	eventually(t, "a reaction that isn't one refused", func() bool { return g1.Dropped("refused") > before })
}

// A guest Usherr is held to its rate: a burst, then dropped.
func TestChatRate(t *testing.T) {
	every := screening.GuestReactEvery
	screening.GuestReactEvery = time.Hour // nothing comes back during the test
	t.Cleanup(func() { screening.GuestReactEvery = every })
	url := goRelay(t, relay.Options{})
	ctx := t.Context()
	host, g1 := start(t, url, "host"), start(t, url, "g1")
	pair(t, url, host, g1)
	sid := screening.NewSID()
	host.book.hosted[sid] = map[string]bool{g1.id.ID(): true}
	n := screening.GuestReactBurst + 5
	for range n {
		_ = g1.Send(ctx, host.id.ID(), screening.TypeReactSend, screening.ReactSend{V: 1, SID: sid, ID: link.NewMessageID(), Name: "Sam", Word: "HA", Pos: 10})
	}
	eventually(t, "the reactions over the rate dropped", func() bool {
		return host.chat.count(func(c *chatRec) int { return len(c.rfrom) })+host.Dropped("refused") >= n
	})
	if got := host.chat.count(func(c *chatRec) int { return len(c.rfrom) }); got < screening.GuestReactBurst || got >= n {
		t.Fatalf("taken %d of %d (burst %d)", got, n, screening.GuestReactBurst)
	}
}

func TestLimiter(t *testing.T) {
	l := screening.NewLimiter(time.Second, 5)
	now := time.Unix(1_000_000, 0)
	for i := range 5 {
		if !l.Allow("a", now) {
			t.Fatalf("burst %d refused", i)
		}
	}
	if l.Allow("a", now) {
		t.Fatal("over the burst allowed")
	}
	if !l.Allow("b", now) {
		t.Fatal("another sender held back")
	}
	if l.Allow("a", now.Add(900*time.Millisecond)) {
		t.Fatal("allowed before a second")
	}
	if !l.Allow("a", now.Add(1100*time.Millisecond)) {
		t.Fatal("not allowed after a second")
	}
	if l.Allow("a", now.Add(1200*time.Millisecond)) {
		t.Fatal("two in a second")
	}
}

func TestChatChecks(t *testing.T) {
	sid, id := screening.NewSID(), link.NewMessageID()
	m := screening.ChatMsg{V: 1, SID: sid, ID: id, Name: "", Text: "hi", Pos: -30, At: 1}
	if !m.Check() || m.Name != "Usherr" {
		t.Fatalf("a message with no name: %+v", m)
	}
	for _, p := range []float64{-1e9, 1e9} {
		bad := m
		bad.Pos = p
		if bad.Check() {
			t.Fatalf("position %v taken", p)
		}
	}
	long := m
	long.Text = strings.Repeat("é", 700)
	if !long.Check() || len([]rune(long.Text)) != screening.MaxChatText {
		t.Fatalf("700 characters: kept %d", len([]rune(long.Text)))
	}
	for _, w := range screening.Words {
		if !screening.ValidWord(w) {
			t.Fatal(w)
		}
	}
	if screening.ValidWord("ha") || screening.ValidWord("") {
		t.Fatal("not a reaction taken")
	}
}
