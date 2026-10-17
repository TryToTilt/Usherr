package api

// Home fork: search many videos in the background. A follow with "add all
// and search now", or Search on a selection in the library, puts videos in
// one line that is worked through one at a time (a short pause between
// them), so the indexers see a steady trickle instead of a burst. Grabs go
// to the download line as manual grabs; videos already downloaded or
// downloading are skipped.

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/nsfw"
	"github.com/rdborg/mediarium/internal/queue"
)

type videoSearchLine struct {
	mu      sync.Mutex
	pending []int64
	queued  map[int64]bool
	running bool
	total   int // videos in this run (since the line was last empty)
	done    int
	found   int
	now     bool // "Search now anyway": ignore the quiet hours for this run
}

var videoSearches = &videoSearchLine{queued: map[int64]bool{}}

// videoSearchPause is the wait between two video searches.
var videoSearchPause = 1500 * time.Millisecond

// videoWindowPoll is how often a held search line checks the schedule.
var videoWindowPoll = 30 * time.Second

// queueVideoSearches adds videos to the background search line and reports
// how many were added (videos already waiting aren't added twice).
func (s *Server) queueVideoSearches(ids []int64) int {
	l := videoSearches
	l.mu.Lock()
	added := 0
	for _, id := range ids {
		if l.queued[id] {
			continue
		}
		l.queued[id] = true
		l.pending = append(l.pending, id)
		added++
	}
	l.total += added
	start := !l.running && len(l.pending) > 0
	if start {
		l.running = true
	}
	l.mu.Unlock()
	if start {
		s.background(s.runVideoSearches)
	}
	return added
}

func (s *Server) runVideoSearches() {
	l := videoSearches
	for {
		l.mu.Lock()
		if len(l.pending) == 0 || !s.nsfwEnabled() {
			l.running, l.pending, l.queued, l.now = false, nil, map[int64]bool{}, false
			found, total := l.found, l.total
			l.total, l.done, l.found = 0, 0, 0
			l.mu.Unlock()
			if total > 0 {
				slog.Info("videos: batch search finished", "searched", total, "grabbed", found)
			}
			return
		}
		if !l.now && !s.taskAllowed(taskVideoSearch) {
			// The schedule holds video searches: wait (or "Search now anyway").
			l.mu.Unlock()
			time.Sleep(videoWindowPoll)
			continue
		}
		id := l.pending[0]
		l.pending = l.pending[1:]
		l.mu.Unlock()

		grabbed := false
		if sc, err := s.videoRepo().GetVideo(id); err == nil && sc.Wanted && sc.Status == nsfw.StatusMissing {
			if busy, _ := s.videoRepo().HasActiveDownload(id); !busy {
				grabbed = s.searchVideo(id, false, queue.PriorityManual)
			}
		}

		l.mu.Lock()
		delete(l.queued, id)
		l.done++
		if grabbed {
			l.found++
		}
		l.mu.Unlock()
		time.Sleep(videoSearchPause)
	}
}

// searchableNsfw keeps the ids of videos worth searching: wanted, without
// a file and not downloading.
func (s *Server) searchableNsfw(ids []int64) []int64 {
	repo := s.videoRepo()
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		sc, err := repo.GetVideo(id)
		if err != nil || !sc.Wanted || sc.Status != nsfw.StatusMissing {
			continue
		}
		if busy, _ := repo.HasActiveDownload(id); busy {
			continue
		}
		out = append(out, id)
	}
	return out
}

// searchSourceNsfw queues the wanted, missing videos of a performer or
// studio (stash id) for searching and reports how many.
func (s *Server) searchSourceNsfw(kind, stashID string) int {
	repo := s.videoRepo()
	var list []nsfw.Video
	var err error
	if kind == nsfw.KindStudio {
		list, err = repo.NsfwFromStudio(stashID)
	} else {
		list, err = repo.NsfwWithPerformer(stashID)
	}
	if err != nil {
		return 0
	}
	ids := make([]int64, 0, len(list))
	for _, sc := range list {
		ids = append(ids, sc.ID)
	}
	return s.queueVideoSearches(s.searchableNsfw(ids))
}

// GET /api/nsfw/search-status: how the background search line is doing.
func (s *Server) handleVideoSearchStatus(w http.ResponseWriter, r *http.Request) {
	l := videoSearches
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]any{"running": l.running, "total": l.total, "done": l.done, "found": l.found, "waiting": len(l.pending)}
	if !l.now && len(l.pending) > 0 && !s.taskAllowed(taskVideoSearch) {
		out["held"] = true
		if next := s.loadSchedules().nextAllowed(taskVideoSearch, time.Now()); !next.IsZero() {
			out["heldUntil"] = next.Format(time.RFC3339)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/nsfw/search-now: run the waiting batch now, ignoring the
// quiet hours until the line is empty.
func (s *Server) handleVideoSearchNow(w http.ResponseWriter, r *http.Request) {
	l := videoSearches
	l.mu.Lock()
	l.now = true
	l.mu.Unlock()
	s.handleVideoSearchStatus(w, r)
}
