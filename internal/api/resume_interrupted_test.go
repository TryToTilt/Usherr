package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/queue"
)

// fakeResume is the app around a resume run, with a clock that only moves
// when the run sleeps.
type fakeResume struct {
	now      time.Time
	items    map[int64]queue.Item
	enabled  bool
	safe     bool
	external bool // torrents run in an external client
	watching bool
	known    bool
	vpnUp    int // the VPN comes up after this many checks (0: always up)
	vpnTries int
	slots    int // free places; each resume uses one
	failOn   map[int64]bool

	resumed []int64
	held    []int64
	events  []string
	gaps    []time.Duration // time between two resumes
	last    time.Time
}

func (f *fakeResume) deps() resumeDeps {
	return resumeDeps{
		Enabled: func() bool { return f.enabled },
		Safe:    func() bool { return f.safe },
		Get: func(id int64) (queue.Item, error) {
			it, ok := f.items[id]
			if !ok {
				return queue.Item{}, errors.New("gone")
			}
			return it, nil
		},
		Builtin: func(it queue.Item) bool { return it.Protocol == queue.ProtocolUsenet || !f.external },
		Watch:   func() (bool, bool) { return f.watching, f.known },
		Hold:    func(ids []int64) { f.held = append(f.held, ids...) },
		VPN: func(queue.Item) error {
			f.vpnTries++
			if f.vpnTries <= f.vpnUp {
				return errors.New("waiting for the VPN")
			}
			return nil
		},
		Free: func() bool { return f.slots > 0 },
		Resume: func(it queue.Item) error {
			if f.failOn[it.ID] {
				return errors.New("not in the library")
			}
			f.slots--
			if !f.last.IsZero() {
				f.gaps = append(f.gaps, f.now.Sub(f.last))
			}
			f.last = f.now
			f.resumed = append(f.resumed, it.ID)
			return nil
		},
		Event: func(it queue.Item, msg string) { f.events = append(f.events, msg) },
		Sleep: func(ctx context.Context, d time.Duration) bool {
			f.now = f.now.Add(d)
			return ctx.Err() == nil
		},
		Now: func() time.Time { return f.now },
	}
}

func TestStillInterruptedCountsOnlyWhatIsLeftForThePerson(t *testing.T) {
	f := &fakeResume{items: map[int64]queue.Item{
		1: paused(1, queue.ProtocolUsenet, true),  // could not be resumed: waits for a person
		2: paused(2, queue.ProtocolUsenet, false), // the person paused it themselves
		3: {ID: 3, Status: queue.StatusDownloading},
		4: paused(4, queue.ProtocolUsenet, true), // held back while someone watches
	}}
	items := []queue.Item{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}
	if got := stillInterrupted(f.deps().Get, items, map[int64]bool{4: true}); got != 1 {
		t.Fatalf("stillInterrupted = %d, want 1", got)
	}
}

func paused(id int64, p queue.Protocol, interrupted bool) queue.Item {
	return queue.Item{ID: id, Protocol: p, Status: queue.StatusPaused, Interrupted: interrupted}
}

func TestRunResumeInterrupted(t *testing.T) {
	use, tor := queue.ProtocolUsenet, queue.ProtocolTorrent
	tests := []struct {
		name        string
		set         func(f *fakeResume)
		items       []queue.Item
		want        []int64
		wantHeld    []int64
		wantLeft    int
		minGap      time.Duration
		maxVPNTries int
	}{
		{name: "usenet and torrent both carry on, oldest first", items: []queue.Item{paused(1, use, true), paused(2, tor, true)}, want: []int64{1, 2}, minGap: resumeStagger},
		{name: "switched off", set: func(f *fakeResume) { f.enabled = false }, items: []queue.Item{paused(1, use, true)}, wantLeft: 1},
		{name: "safe mode", set: func(f *fakeResume) { f.safe = true }, items: []queue.Item{paused(1, use, true)}, wantLeft: 1},
		{name: "paused by a person is left alone", items: []queue.Item{paused(1, use, false), paused(2, use, true)}, want: []int64{2}, wantLeft: 1},
		{name: "failed, conflict and seeding items are not touched", set: func(f *fakeResume) {
			f.items[1] = queue.Item{ID: 1, Protocol: use, Status: queue.StatusFailed, Interrupted: true}
			f.items[2] = queue.Item{ID: 2, Protocol: tor, Status: queue.StatusConflict}
			f.items[3] = queue.Item{ID: 3, Protocol: tor, Status: queue.StatusCompleted}
		}, items: []queue.Item{paused(1, use, true), paused(2, tor, true), paused(3, tor, true)}, wantLeft: 3},
		{name: "removed meanwhile", set: func(f *fakeResume) { delete(f.items, 1) }, items: []queue.Item{paused(1, use, true), paused(2, use, true)}, want: []int64{2}, wantLeft: 1},
		{name: "external client torrents stay put", set: func(f *fakeResume) { f.external = true }, items: []queue.Item{paused(1, tor, true), paused(2, use, true)}, want: []int64{2}, wantLeft: 1},
		{name: "someone is watching: handed to the stream list", set: func(f *fakeResume) { f.watching = true }, items: []queue.Item{paused(1, use, true), paused(2, tor, true)}, wantHeld: []int64{1, 2}},
		{name: "torrent waits for the VPN, then starts", set: func(f *fakeResume) { f.vpnUp = 3 }, items: []queue.Item{paused(1, tor, true)}, want: []int64{1}},
		{name: "VPN never comes: torrent stays paused, usenet goes on", set: func(f *fakeResume) { f.vpnUp = 1 << 30 }, items: []queue.Item{paused(1, tor, true), paused(2, use, true)}, want: []int64{2}, wantLeft: 1},
		{name: "one place free: the line keeps its order", set: func(f *fakeResume) { f.slots = 1 }, items: []queue.Item{paused(1, use, true), paused(2, use, true)}, want: []int64{1}, wantLeft: 1},
		{name: "a refused item does not stop the rest", set: func(f *fakeResume) { f.failOn[1] = true }, items: []queue.Item{paused(1, use, true), paused(2, use, true)}, want: []int64{2}, wantLeft: 1},
		{name: "nothing to do", items: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeResume{now: time.Unix(1_800_000_000, 0), items: map[int64]queue.Item{}, enabled: true, known: true, slots: 10, failOn: map[int64]bool{}}
			for _, it := range tc.items {
				f.items[it.ID] = it
			}
			if tc.set != nil {
				tc.set(f)
			}
			res := runResumeInterrupted(context.Background(), f.deps(), tc.items)
			if !equalIDs(f.resumed, tc.want) {
				t.Fatalf("resumed %v, want %v", f.resumed, tc.want)
			}
			if !equalIDs(f.held, tc.wantHeld) {
				t.Fatalf("held %v, want %v", f.held, tc.wantHeld)
			}
			if res.Resumed != len(tc.want) || res.Held != len(tc.wantHeld) || res.Left != tc.wantLeft {
				t.Fatalf("result %+v, want resumed %d held %d left %d", res, len(tc.want), len(tc.wantHeld), tc.wantLeft)
			}
			if len(f.events) != len(tc.want) {
				t.Fatalf("%d activity lines for %d resumed items", len(f.events), len(tc.want))
			}
			for _, e := range f.events {
				if e != "Resumed after a restart" {
					t.Fatalf("activity line %q", e)
				}
			}
			for _, g := range f.gaps {
				if g < tc.minGap {
					t.Fatalf("two starts only %v apart, want at least %v", g, tc.minGap)
				}
			}
		})
	}
}

func TestRunResumeInterruptedWaitsForTheWatchCheck(t *testing.T) {
	f := &fakeResume{now: time.Unix(1_800_000_000, 0), items: map[int64]queue.Item{1: paused(1, queue.ProtocolUsenet, true)}, enabled: true, slots: 1, failOn: map[int64]bool{}}
	d := f.deps()
	checks := 0
	d.Watch = func() (bool, bool) {
		checks++
		return false, checks > 3 // the first media-server check comes after a few polls
	}
	res := runResumeInterrupted(context.Background(), d, []queue.Item{f.items[1]})
	if res.Resumed != 1 || checks < 4 {
		t.Fatalf("result %+v after %d checks", res, checks)
	}
}

func TestRunResumeInterruptedStopsWhenTheAppStops(t *testing.T) {
	f := &fakeResume{now: time.Unix(1_800_000_000, 0), items: map[int64]queue.Item{1: paused(1, queue.ProtocolUsenet, true)}, enabled: true, known: true, slots: 1, failOn: map[int64]bool{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := runResumeInterrupted(ctx, f.deps(), []queue.Item{f.items[1]})
	if res.Resumed != 0 || len(f.resumed) != 0 {
		t.Fatalf("a stopped app must resume nothing: %+v", res)
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
