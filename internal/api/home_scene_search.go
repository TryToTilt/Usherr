package api

// Home fork: search many scenes in the background. A follow with "add all
// and search now", or Search on a selection in the library, puts scenes in
// one line that is worked through one at a time (a short pause between
// them), so the indexers see a steady trickle instead of a burst. Grabs go
// to the download line as manual grabs; scenes already downloaded or
// downloading are skipped.

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/scenes"
)

type sceneSearchLine struct {
	mu      sync.Mutex
	pending []int64
	queued  map[int64]bool
	running bool
	total   int // scenes in this run (since the line was last empty)
	done    int
	found   int
	now     bool // "Search now anyway": ignore the quiet hours for this run
}

var sceneSearches = &sceneSearchLine{queued: map[int64]bool{}}

// sceneSearchPause is the wait between two scene searches.
var sceneSearchPause = 1500 * time.Millisecond

// sceneWindowPoll is how often a held search line checks the schedule.
var sceneWindowPoll = 30 * time.Second

// queueSceneSearches adds scenes to the background search line and reports
// how many were added (scenes already waiting aren't added twice).
func (s *Server) queueSceneSearches(ids []int64) int {
	l := sceneSearches
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
		s.background(s.runSceneSearches)
	}
	return added
}

func (s *Server) runSceneSearches() {
	l := sceneSearches
	for {
		l.mu.Lock()
		if len(l.pending) == 0 || !s.scenesEnabled() {
			l.running, l.pending, l.queued, l.now = false, nil, map[int64]bool{}, false
			found, total := l.found, l.total
			l.total, l.done, l.found = 0, 0, 0
			l.mu.Unlock()
			if total > 0 {
				slog.Info("scenes: batch search finished", "searched", total, "grabbed", found)
			}
			return
		}
		if !l.now && !s.taskAllowed(taskSceneSearch) {
			// The schedule holds scene searches: wait (or "Search now anyway").
			l.mu.Unlock()
			time.Sleep(sceneWindowPoll)
			continue
		}
		id := l.pending[0]
		l.pending = l.pending[1:]
		l.mu.Unlock()

		grabbed := false
		if sc, err := s.sceneRepo().GetScene(id); err == nil && sc.Wanted && sc.Status == scenes.StatusMissing {
			if busy, _ := s.sceneRepo().HasActiveDownload(id); !busy {
				grabbed = s.searchScene(id, false, queue.PriorityManual)
			}
		}

		l.mu.Lock()
		delete(l.queued, id)
		l.done++
		if grabbed {
			l.found++
		}
		l.mu.Unlock()
		time.Sleep(sceneSearchPause)
	}
}

// searchableScenes keeps the ids of scenes worth searching: wanted, without
// a file and not downloading.
func (s *Server) searchableScenes(ids []int64) []int64 {
	repo := s.sceneRepo()
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		sc, err := repo.GetScene(id)
		if err != nil || !sc.Wanted || sc.Status != scenes.StatusMissing {
			continue
		}
		if busy, _ := repo.HasActiveDownload(id); busy {
			continue
		}
		out = append(out, id)
	}
	return out
}

// searchSourceScenes queues the wanted, missing scenes of a performer or
// studio (stash id) for searching and reports how many.
func (s *Server) searchSourceScenes(kind, stashID string) int {
	repo := s.sceneRepo()
	var list []scenes.Scene
	var err error
	if kind == scenes.KindStudio {
		list, err = repo.ScenesFromStudio(stashID)
	} else {
		list, err = repo.ScenesWithPerformer(stashID)
	}
	if err != nil {
		return 0
	}
	ids := make([]int64, 0, len(list))
	for _, sc := range list {
		ids = append(ids, sc.ID)
	}
	return s.queueSceneSearches(s.searchableScenes(ids))
}

// GET /api/scenes/search-status: how the background search line is doing.
func (s *Server) handleSceneSearchStatus(w http.ResponseWriter, r *http.Request) {
	l := sceneSearches
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]any{"running": l.running, "total": l.total, "done": l.done, "found": l.found, "waiting": len(l.pending)}
	if !l.now && len(l.pending) > 0 && !s.taskAllowed(taskSceneSearch) {
		out["held"] = true
		if next := s.loadSchedules().nextAllowed(taskSceneSearch, time.Now()); !next.IsZero() {
			out["heldUntil"] = next.Format(time.RFC3339)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/scenes/search-now: run the waiting batch now, ignoring the
// quiet hours until the line is empty.
func (s *Server) handleSceneSearchNow(w http.ResponseWriter, r *http.Request) {
	l := sceneSearches
	l.mu.Lock()
	l.now = true
	l.mu.Unlock()
	s.handleSceneSearchStatus(w, r)
}
