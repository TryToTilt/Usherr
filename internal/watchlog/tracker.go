package watchlog

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Server names the media server a list of playbacks came from.
type Server struct {
	ID   int64
	Name string
	Kind string
}

// Tracker turns what the servers say is playing into the history. It is safe
// for concurrent use.
type Tracker struct {
	Repo *Repo

	mu     sync.Mutex
	open   map[string]*Session // server id + key -> the playback
	pos    map[string]float64  // the position at the last check, in seconds
	loaded bool
}

// NewTracker returns a Tracker that saves to repo.
func NewTracker(repo *Repo) *Tracker {
	return &Tracker{Repo: repo, open: map[string]*Session{}, pos: map[string]float64{}}
}

func openKey(serverID int64, key string) string {
	return strconv.FormatInt(serverID, 10) + "|" + key
}

// staleOpen is how long a playback left open (Usherr stopped while it played)
// may go unseen before it is closed on the next start.
const staleOpen = 10 * time.Minute

// load picks up the playbacks that were open when Usherr stopped: one that is
// still playing carries on; one that went unseen too long is closed where it
// was last seen.
func (t *Tracker) load(ctx context.Context, now time.Time) {
	if t.loaded {
		return
	}
	t.loaded = true
	list, err := t.Repo.Open(ctx)
	if err != nil {
		slog.Warn("watch history: couldn't read the open playbacks", "err", err)
		return
	}
	for i := range list {
		s := list[i]
		if now.Sub(s.LastSeen) > staleOpen {
			s.EndedAt = s.LastSeen
			if s.EndedAt.IsZero() {
				s.EndedAt = s.StartedAt
			}
			_ = t.Repo.Update(ctx, s)
			continue
		}
		t.open[openKey(s.ServerID, s.Key)] = &s
		t.pos[openKey(s.ServerID, s.Key)] = s.Progress * float64(s.RuntimeSec)
	}
}

// Observe records what one server plays at now. Playbacks of that server
// that are no longer listed have ended.
func (t *Tracker) Observe(ctx context.Context, srv Server, seen []Seen, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.load(ctx, now)
	here := map[string]bool{}
	for _, v := range seen {
		k := openKey(srv.ID, v.Key)
		here[k] = true
		s, ok := t.open[k]
		if !ok {
			s = &Session{ServerID: srv.ID, ServerName: srv.Name, ServerKind: srv.Kind, Key: v.Key, ServerUserID: v.ServerUserID,
				UserName: v.UserName, UserID: v.UserID, Kind: v.Kind, Title: v.Title, Series: v.Series, Season: v.Season,
				Episode: v.Episode, Year: v.Year, TMDBID: v.TMDBID, LibraryID: v.LibraryID, Client: v.Client, Device: v.Device,
				VideoIn: v.VideoIn, AudioIn: v.AudioIn, ResolutionIn: v.ResolutionIn, Location: v.Location, Address: v.Address,
				StartedAt: now, LastSeen: now}
			apply(s, v)
			if err := t.Repo.Insert(ctx, s); err != nil {
				slog.Warn("watch history: couldn't save a playback", "err", err)
				continue
			}
			t.open[k], t.pos[k] = s, v.PositionSec
			continue
		}
		step := min(now.Sub(s.LastSeen), maxStep)
		if step > 0 {
			s.WatchedSec += int(played(step, s.Paused, v.Paused, t.pos[k], v.PositionSec, v.RuntimeSec).Seconds() + 0.5)
		}
		s.LastSeen = now
		t.pos[k] = v.PositionSec
		apply(s, v)
		if err := t.Repo.Update(ctx, *s); err != nil {
			slog.Warn("watch history: couldn't update a playback", "err", err)
		}
	}
	for k, s := range t.open {
		if s.ServerID != srv.ID || here[k] {
			continue
		}
		s.EndedAt = s.LastSeen
		s.Paused = false
		if err := t.Repo.Update(ctx, *s); err != nil {
			slog.Warn("watch history: couldn't close a playback", "err", err)
		}
		delete(t.open, k)
		delete(t.pos, k)
	}
}

// played is how much of one step between two checks was spent playing. When
// the position is known it is how far it moved (never more than the time
// that passed, so a skip ahead isn't counted); otherwise the whole step if it
// was playing before. Paused at both checks counts nothing.
func played(step time.Duration, wasPaused, isPaused bool, from, to, runtime float64) time.Duration {
	if wasPaused && isPaused {
		return 0
	}
	if runtime > 0 && (from > 0 || to > 0) {
		moved := time.Duration((to - from) * float64(time.Second))
		return max(0, min(moved, step))
	}
	if wasPaused {
		return 0
	}
	return step
}

// apply copies what can change during a playback.
func apply(s *Session, v Seen) {
	s.Paused = v.Paused
	if v.PlayMethod != "" {
		s.PlayMethod = v.PlayMethod
	}
	if len(v.TranscodeReasons) > 0 || s.PlayMethod != "Transcode" {
		s.TranscodeReasons = v.TranscodeReasons
	}
	if v.VideoOut != "" {
		s.VideoOut = v.VideoOut
	}
	if v.AudioOut != "" {
		s.AudioOut = v.AudioOut
	}
	s.ResolutionOut = v.ResolutionOut
	if v.BandwidthKbps > 0 {
		s.BandwidthKbps = v.BandwidthKbps
	}
	if v.RuntimeSec > 0 {
		s.RuntimeSec = int(v.RuntimeSec + 0.5)
		if p := v.PositionSec / v.RuntimeSec; p > s.Progress {
			s.Progress = min(p, 1)
		}
	}
	s.Completed = s.Progress >= Completed
}

// Playing is what is open right now, newest first. Paused ones are included.
func (t *Tracker) Playing() []Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Session, 0, len(t.open))
	for k, s := range t.open {
		c := *s
		if c.RuntimeSec > 0 {
			c.Progress = min(t.pos[k]/float64(c.RuntimeSec), 1) // where it is now, not the furthest
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// Active says whether anything is playing (the checks come more often then).
func (t *Tracker) Active() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.open) > 0
}

// Forget drops a server's open playbacks from memory, closing them, for a
// server that was removed or switched off.
func (t *Tracker) Forget(ctx context.Context, keep map[int64]bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, s := range t.open {
		if keep[s.ServerID] {
			continue
		}
		s.EndedAt, s.Paused = s.LastSeen, false
		_ = t.Repo.Update(ctx, *s)
		delete(t.open, k)
		delete(t.pos, k)
	}
}
