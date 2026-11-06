package screening_test

import (
	"context"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/link"
	"github.com/rdborg/mediarium/internal/link/relay"
	"github.com/rdborg/mediarium/internal/link/screening"
)

// room is a Room in memory that records what it was told.
type room struct {
	mu        sync.Mutex
	playbacks []screening.Playback
	recipes   []screening.Recipe
	pauses    []string // who asked
}

func (r *room) PlaybackIn(_ context.Context, _ link.Peer, p screening.Playback) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.playbacks = append(r.playbacks, p)
	return nil
}

func (r *room) RecipeIn(_ context.Context, _ link.Peer, rc screening.Recipe) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recipes = append(r.recipes, rc)
	return nil
}

func (r *room) PauseIn(_ context.Context, guest link.Peer, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pauses = append(r.pauses, guest.ID)
	return nil
}

func (r *room) count(f func(*room) int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return f(r)
}

// Only the host's key moves the film: a guest that has the screening key
// can't send playback to the others, a playback for another screening or in
// the pair's envelope is dropped, and only invited Usherrs may ask to pause.
func TestPlaybackOnlyFromHost(t *testing.T) {
	url := goRelay(t, relay.Options{})
	ctx := t.Context()
	host, g1, g2, other := start(t, url, "host"), start(t, url, "g1"), start(t, url, "g2"), start(t, url, "other")
	pair(t, url, host, g1)
	pair(t, url, host, g2)
	pair(t, url, host, other)
	pair(t, url, g1, g2)

	sid, otherSID := screening.NewSID(), screening.NewSID()
	key := link.NewGroupKey()
	host.book.hosted[sid] = map[string]bool{g1.id.ID(): true, g2.id.ID(): true}
	host.book.hosted[otherSID] = map[string]bool{}
	for _, g := range []*usherr{g1, g2} {
		g.book.guestOf[sid] = host.id.ID()
		g.book.keys[sid] = key
	}
	guests := []string{g1.id.ID(), g2.id.ID()}

	// The host plays: both follow.
	play := screening.Playback{V: 1, SID: sid, Seq: 10, State: screening.StateFilm, Position: 125.5, AtWallTime: time.Now().UnixMilli(), AnyonePause: true}
	if err := host.SendGroup(ctx, key, guests, screening.TypePlayback, play); err != nil {
		t.Fatal(err)
	}
	for _, g := range []*usherr{g1, g2} {
		eventually(t, "the playback arrives", func() bool { return g.room.count(func(r *room) int { return len(r.playbacks) }) == 1 })
		if p := g.room.playbacks[0]; p.Position != 125.5 || p.State != "film" || !p.AnyonePause {
			t.Fatalf("playback: %+v", p)
		}
	}

	// g2 has the key, but isn't the host: dropped at g1.
	before := g1.Dropped("sender")
	forged := play
	forged.Seq, forged.State = 11, screening.StatePaused
	env, _ := link.SealGroup(g2.id, key, link.Message{Type: screening.TypePlayback, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: mustJSON(forged)})
	_ = g2.SendRaw(url, g1.id.ID(), env)
	eventually(t, "a guest's playback dropped", func() bool { return g1.Dropped("sender") > before })
	// Sent through the relay's fanout too: still dropped.
	before = g1.Dropped("sender")
	_ = g2.SendGroup(ctx, key, []string{g1.id.ID()}, screening.TypePlayback, forged)
	eventually(t, "a guest's fanned-out playback dropped", func() bool { return g1.Dropped("sender") > before })

	// The host, but with another screening's id inside: refused.
	before = g1.Dropped("refused")
	wrong := play
	wrong.SID, wrong.Seq = otherSID, 12
	_ = host.SendGroup(ctx, key, []string{g1.id.ID()}, screening.TypePlayback, wrong)
	eventually(t, "a playback for another screening refused", func() bool { return g1.Dropped("refused") > before })

	// Playback never comes pairwise.
	if err := host.Send(ctx, g1.id.ID(), screening.TypePlayback, play); err != link.ErrUnknownType {
		t.Fatalf("playback sent pairwise: %v", err)
	}
	before = g1.Dropped("type")
	pairEnv, _ := host.Seal(ctx, g1.id.ID(), link.Message{Type: screening.TypePlayback, ID: link.NewMessageID(), At: time.Now().UnixMilli(), Body: mustJSON(play)})
	_ = host.SendRaw(url, g1.id.ID(), pairEnv)
	eventually(t, "a pairwise playback dropped", func() bool { return g1.Dropped("type") > before })

	// A state that isn't one, a position out of range: refused.
	before = g1.Dropped("refused")
	bad := play
	bad.State, bad.Seq = "rewinding", 13
	_ = host.SendGroup(ctx, key, []string{g1.id.ID()}, screening.TypePlayback, bad)
	bad.State, bad.Position, bad.Seq = screening.StateFilm, -1e7, 14
	_ = host.SendGroup(ctx, key, []string{g1.id.ID()}, screening.TypePlayback, bad)
	eventually(t, "bad playbacks refused", func() bool { return g1.Dropped("refused") > before+1 })
	if n := g1.room.count(func(r *room) int { return len(r.playbacks) }); n != 1 {
		t.Fatalf("g1 took %d playbacks, want only the host's first", n)
	}

	// The recipe arrives; one longer than an hour is refused.
	rc := screening.Recipe{V: 1, SID: sid, Rev: 1, Items: []screening.RecipeItem{
		{Kind: "card", Card: "welcome", Seconds: 8},
		{Kind: "trailer", TMDB: 550, Title: "Fight Club", YouTube: "SUXWAEX2jlg", Poster: "javascript:alert(1)", Seconds: 120},
		{Kind: "clip", Category: "Snipes", Seconds: 30},
	}}
	_ = host.SendGroup(ctx, key, guests, screening.TypeRecipe, rc)
	eventually(t, "the recipe arrives", func() bool { return g1.room.count(func(r *room) int { return len(r.recipes) }) == 1 })
	if got := g1.room.recipes[0]; len(got.Items) != 3 || got.Items[1].Poster != "" || got.Items[1].YouTube != "SUXWAEX2jlg" || screening.RecipeSeconds(got.Items) != 158 {
		t.Fatalf("recipe: %+v", got)
	}
	before = g1.Dropped("refused")
	long := rc
	long.Items = []screening.RecipeItem{}
	for range 7 {
		long.Items = append(long.Items, screening.RecipeItem{Kind: "clip", Seconds: 600})
	}
	_ = host.SendGroup(ctx, key, []string{g1.id.ID()}, screening.TypeRecipe, long)
	eventually(t, "a recipe over an hour refused", func() bool { return g1.Dropped("refused") > before })

	// Pause requests: from an invited guest, taken; from anyone else, refused.
	if err := g1.Send(ctx, host.id.ID(), screening.TypePause, screening.PauseRequest{V: 1, SID: sid}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the pause request arrives", func() bool { return host.room.count(func(r *room) int { return len(r.pauses) }) == 1 })
	before = host.Dropped("refused")
	_ = other.Send(ctx, host.id.ID(), screening.TypePause, screening.PauseRequest{V: 1, SID: sid})
	eventually(t, "an uninvited pause request refused", func() bool { return host.Dropped("refused") > before })
	if n := host.room.count(func(r *room) int { return len(r.pauses) }); n != 1 {
		t.Fatalf("pauses: %d", n)
	}
}

func TestProject(t *testing.T) {
	at := int64(1_000_000)
	for _, c := range []struct {
		state string
		pos   float64
		now   int64
		want  string
		at    float64
	}{
		{"film", 100, at + 2500, "film", 102.5},
		{"paused", 100, at + 60_000, "paused", 100},
		{"ended", 7000, at + 1000, "ended", 7000},
		{"preshow", -30, at + 10_000, "preshow", -20},
		{"preshow", -5, at + 7_000, "film", 2}, // the pre-show ran out: the film
	} {
		st, pos := screening.Project(c.state, c.pos, at, c.now)
		if st != c.want || math.Abs(pos-c.at) > 1e-9 {
			t.Errorf("%s at %v, %d ms later: %s %v, want %s %v", c.state, c.pos, c.now-at, st, pos, c.want, c.at)
		}
	}
}

func TestCorrect(t *testing.T) {
	for _, c := range []struct {
		drift float64
		rate  float64
		seek  bool
	}{
		{0, 1, false},
		{0.05, 1, false},    // in step
		{-0.09, 1, false},   // in step
		{0.25, 1.02, false}, // a little behind: a little faster
		{-0.5, 0.96, false}, // ahead: slower
		{0.9, 1.06, false},  // at most 6%
		{-0.95, 0.94, false},
		{1.5, 1, true}, // far off: seek
		{-12, 1, true},
		{math.NaN(), 1, false},
	} {
		rate, seek := screening.Correct(c.drift)
		if math.Abs(rate-c.rate) > 1e-9 || seek != c.seek {
			t.Errorf("drift %v: rate %v seek %v, want %v %v", c.drift, rate, seek, c.rate, c.seek)
		}
	}
}

func TestPlaybackCheck(t *testing.T) {
	ok := screening.Playback{V: 1, SID: screening.NewSID(), Seq: 1, State: "film", Position: 10, AtWallTime: 1}
	if !ok.Check() {
		t.Fatal("a good playback refused")
	}
	for name, mod := range map[string]func(*screening.Playback){
		"no seq":    func(p *screening.Playback) { p.Seq = 0 },
		"no time":   func(p *screening.Playback) { p.AtWallTime = 0 },
		"NaN":       func(p *screening.Playback) { p.Position = math.NaN() },
		"too far":   func(p *screening.Playback) { p.Position = 1e9 },
		"version":   func(p *screening.Playback) { p.V = 2 },
		"bad sid":   func(p *screening.Playback) { p.SID = strings.Repeat("x", 36) },
		"bad state": func(p *screening.Playback) { p.State = "" },
	} {
		p := ok
		mod(&p)
		if p.Check() {
			t.Errorf("%s: taken", name)
		}
	}
}
