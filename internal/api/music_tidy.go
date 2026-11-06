package api

// Adapted from DroppedNeedle (AGPL-3.0), backend/services/native/library_management_preview_service.py

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/musictidy"
	"github.com/rdborg/mediarium/internal/queue"
)

// "Tidy up your music": an administrator starts a check, which works out new
// names and tags for the music folder and changes nothing. They then pick the
// albums to apply. Originals are kept in a recycle folder for 30 days, and a
// plan can be undone. The check and the apply run as one background job at a
// time, which the page polls (see music_tidy_check.go for the check).

const (
	keyAcoustID = "music.acoustid_key" // encrypted; the server's AcoustID application key

	tidyCheck = "check"
	tidyApply = "apply"
	tidyUndo  = "undo"

	tidyRunning = "running"
	tidyDone    = "done"
	tidyFailed  = "failed"
	tidyStopped = "stopped"
)

// tidyState is the tidy feature's in-memory state.
type tidyState struct {
	mu       sync.Mutex
	job      *tidyJob
	acoustID *musictidy.AcoustID // created on first use; tests point it at a fake
}

// tidyJob is the running or last check, apply or undo.
type tidyJob struct {
	mu      sync.Mutex
	kind    string
	phase   string
	planID  int64
	done    int
	total   int
	current string
	err     string
	undo    []musictidy.UndoResult
	ctx     context.Context
	cancel  context.CancelFunc
}

type tidyJobPayload struct {
	Kind    string                 `json:"kind"`
	Phase   string                 `json:"phase"`
	PlanID  int64                  `json:"planId"`
	Done    int                    `json:"done"`
	Total   int                    `json:"total"`
	Current string                 `json:"current,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Undo    []musictidy.UndoResult `json:"undo,omitempty"`
}

func (j *tidyJob) snapshot() tidyJobPayload {
	j.mu.Lock()
	defer j.mu.Unlock()
	return tidyJobPayload{Kind: j.kind, Phase: j.phase, PlanID: j.planID, Done: j.done, Total: j.total, Current: j.current, Error: j.err, Undo: j.undo}
}

func (j *tidyJob) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.phase == tidyRunning
}

func (j *tidyJob) working(name string) {
	j.mu.Lock()
	j.current = name
	j.mu.Unlock()
}

func (j *tidyJob) step() {
	j.mu.Lock()
	j.done++
	j.mu.Unlock()
}

func (j *tidyJob) setTotal(n int) {
	j.mu.Lock()
	j.total = n
	j.mu.Unlock()
}

func (j *tidyJob) setPlan(id int64) {
	j.mu.Lock()
	j.planID = id
	j.mu.Unlock()
}

// finish ends the job; a job stopped by hand stays stopped.
func (j *tidyJob) finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		j.cancel()
	}
	j.current = ""
	switch {
	case j.phase != tidyRunning:
	case err != nil:
		j.phase, j.err = tidyFailed, err.Error()
	default:
		j.phase = tidyDone
	}
}

func (j *tidyJob) stop() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.phase == tidyRunning {
		j.phase, j.current = tidyStopped, ""
		if j.cancel != nil {
			j.cancel()
		}
	}
}

// startTidy starts a job of a kind, unless one is running: only one check,
// apply or undo at a time.
func (s *Server) startTidy(kind string, planID int64) (*tidyJob, bool) {
	t := &s.music.tidy
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != nil && t.job.running() {
		return t.job, false
	}
	job := &tidyJob{kind: kind, phase: tidyRunning, planID: planID}
	job.ctx, job.cancel = context.WithCancel(context.Background())
	t.job = job
	return job, true
}

func (s *Server) tidyStore() *musictidy.Store { return musictidy.NewStore(s.db) }

func (s *Server) tidyEngine(root string) *musictidy.Engine {
	return &musictidy.Engine{Store: s.tidyStore(), DB: s.db, Root: root}
}

// tidyAcoustID is the AcoustID client, shared so its rate limit holds across
// jobs. The key is read on every lookup.
func (s *Server) tidyAcoustID() *musictidy.AcoustID {
	t := &s.music.tidy
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.acoustID == nil {
		t.acoustID = &musictidy.AcoustID{Key: s.acoustIDKey}
	}
	return t.acoustID
}

func (s *Server) acoustIDKey() string {
	v, _ := s.Settings.Get(keyAcoustID)
	return strings.TrimSpace(v)
}

// GET /api/music/tidy: the page: the running or last job, the plans so far
// and the space the kept originals take.
func (s *Server) handleTidyOverview(w http.ResponseWriter, r *http.Request) {
	root := s.musicRoot()
	plans, err := s.tidyStore().Plans()
	if err != nil {
		slog.Warn("tidy: list plans", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the tidy history. Check the log for details.")
		return
	}
	if plans == nil {
		plans = []musictidy.PlanRow{}
	}
	out := map[string]any{
		"root": root, "hasKey": s.acoustIDKey() != "", "toolsReady": musictidy.Available(), "plans": plans,
		"recycleBytes": musictidy.RecycleSize(root), "recycleDays": int(musictidy.RecycleKeep / (24 * time.Hour)),
	}
	t := &s.music.tidy
	t.mu.Lock()
	job := t.job
	t.mu.Unlock()
	if job != nil {
		out["job"] = job.snapshot()
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/music/tidy/check: starts a check of the music folder. Nothing is
// changed. A check already running is returned instead of starting another.
func (s *Server) handleTidyCheck(w http.ResponseWriter, r *http.Request) {
	root := s.musicRoot()
	if root == "" {
		writeError(w, http.StatusPreconditionFailed, "The music folder isn't set. Choose one in Settings > Library > Folders.")
		return
	}
	if !musictidy.Available() {
		writeError(w, http.StatusPreconditionFailed, "This needs ffmpeg and ffprobe, which aren't installed here.")
		return
	}
	job, created := s.startTidy(tidyCheck, 0)
	if !created {
		if job.kind == tidyCheck {
			writeJSON(w, http.StatusOK, job.snapshot())
			return
		}
		writeError(w, http.StatusConflict, "Usherr is still changing your music files. Try again when that has finished.")
		return
	}
	s.background(func() { s.runTidyCheck(job, root) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

// POST /api/music/tidy/stop: stops the running job. What it did so far stays
// (and an apply never leaves an album half done).
func (s *Server) handleTidyStop(w http.ResponseWriter, r *http.Request) {
	t := &s.music.tidy
	t.mu.Lock()
	job := t.job
	t.mu.Unlock()
	if job == nil {
		writeError(w, http.StatusNotFound, "Nothing is running.")
		return
	}
	job.stop()
	writeJSON(w, http.StatusOK, job.snapshot())
}

// tidyItemView is one file as the page shows it: names relative to the music
// folder, no tags it won't change.
type tidyItemView struct {
	OldName string             `json:"oldName"`
	NewName string             `json:"newName"`
	Changes []musictidy.Change `json:"changes,omitempty"`
	Cover   bool               `json:"cover,omitempty"`
}

type tidyAlbumView struct {
	ID        int64           `json:"id"`
	AlbumID   int64           `json:"albumId,omitempty"`
	Artist    string          `json:"artist"`
	Title     string          `json:"title"`
	OldFolder string          `json:"oldFolder"`
	NewFolder string          `json:"newFolder"`
	Status    string          `json:"status"`
	Reason    string          `json:"reason,omitempty"`
	Detail    json.RawMessage `json:"detail"`
	Items     []tidyItemView  `json:"items"`
}

// GET /api/music/tidy/plans/{id}: a plan's albums. Albums that need nothing
// are only counted, unless they have another copy to report.
func (s *Server) handleTidyPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := tidyPlanID(w, r)
	if !ok {
		return
	}
	store := s.tidyStore()
	plan, err := store.Plan(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That check isn't available any more.")
		return
	}
	if err == nil {
		var rows []musictidy.AlbumRow
		if rows, err = store.Albums(id); err == nil {
			rel := func(p string) string {
				if r, err := filepath.Rel(plan.Root, p); err == nil {
					return r
				}
				return p
			}
			views := []tidyAlbumView{}
			unchanged := 0
			for _, a := range rows {
				var d tidyDetail
				_ = json.Unmarshal(a.Detail, &d)
				if a.Status == musictidy.StatusUnchanged {
					unchanged++
					if len(d.Copies) == 0 {
						continue
					}
				}
				v := tidyAlbumView{ID: a.ID, AlbumID: a.AlbumID, Artist: a.Artist, Title: a.Title, OldFolder: rel(a.OldFolder), NewFolder: rel(a.NewFolder),
					Status: a.Status, Reason: a.Reason, Detail: a.Detail, Items: []tidyItemView{}}
				for _, it := range a.Items {
					if it.Plan.Moves() || it.Plan.Rewrites() {
						v.Items = append(v.Items, tidyItemView{OldName: rel(it.OldPath), NewName: rel(it.NewPath), Changes: it.Plan.Changes, Cover: it.Plan.Cover})
					}
				}
				views = append(views, v)
			}
			writeJSON(w, http.StatusOK, map[string]any{"plan": plan, "albums": views, "unchanged": unchanged})
			return
		}
	}
	slog.Warn("tidy: read a plan", "plan", id, "err", err)
	writeError(w, http.StatusInternalServerError, "Couldn't read that check. Check the log for details.")
}

func tidyPlanID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't a check.")
		return 0, false
	}
	return id, true
}

// POST /api/music/tidy/plans/{id}/apply {albums: [ids]}: tidies the chosen
// albums of a check.
func (s *Server) handleTidyApply(w http.ResponseWriter, r *http.Request) {
	id, ok := tidyPlanID(w, r)
	if !ok {
		return
	}
	var req struct {
		Albums []int64 `json:"albums"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Albums) == 0 {
		writeError(w, http.StatusBadRequest, "Choose the albums to tidy.")
		return
	}
	store := s.tidyStore()
	plan, err := store.Plan(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That check isn't available any more.")
		return
	}
	if plan.Root != s.musicRoot() {
		writeError(w, http.StatusConflict, "The music folder changed since this check. Check your music again.")
		return
	}
	if plan.Status == musictidy.PlanUndone {
		writeError(w, http.StatusConflict, "This check was undone. Check your music again.")
		return
	}
	rows, err := store.Albums(id, req.Albums...)
	if err != nil {
		slog.Warn("tidy: read albums to apply", "plan", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read that check. Check the log for details.")
		return
	}
	var todo []musictidy.AlbumRow
	for _, a := range rows {
		if a.Status == musictidy.StatusEligible || a.Status == musictidy.StatusFailed {
			todo = append(todo, a)
		}
	}
	if len(todo) == 0 {
		writeError(w, http.StatusBadRequest, "None of those albums can be tidied.")
		return
	}
	job, created := s.startTidy(tidyApply, id)
	if !created {
		writeError(w, http.StatusConflict, "Usherr is still working on your music. Try again when that has finished.")
		return
	}
	s.background(func() { s.runTidyApply(job, plan, todo) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

// runTidyApply tidies the albums one by one.
func (s *Server) runTidyApply(job *tidyJob, plan musictidy.PlanRow, albums []musictidy.AlbumRow) {
	ctx := job.ctx
	job.setTotal(len(albums))
	slog.Info("tidy: applying", "plan", plan.ID, "albums", len(albums))
	engine := s.tidyEngine(plan.Root)
	for _, a := range albums {
		if ctx.Err() != nil {
			break
		}
		job.working(a.Artist + " – " + a.Title)
		if a.AlbumID > 0 {
			if running, err := s.albumDownloadInProgress(a.AlbumID); err == nil && running != "" {
				if err := s.tidyStore().SetAlbumStatus(a.ID, musictidy.StatusBlocked, "This album is downloading right now. Check your music again when it has finished."); err != nil {
					slog.Warn("tidy: store an album's result", "album", a.Title, "err", err)
				}
				job.step()
				continue
			}
		}
		cover, cleanup := s.tidyCoverFile(ctx, a)
		out := engine.ApplyAlbum(ctx, a, cover)
		cleanup()
		if out.Status == musictidy.StatusApplied {
			s.musicImported(out.Dests...)
			if a.AlbumID > 0 {
				s.albumEvent(a.AlbumID, "tidied", queue.LevelInfo, fmt.Sprintf("Tidied up: the album is now in %s", a.NewFolder))
			}
		} else {
			slog.Info("tidy: album not tidied", "album", a.Title, "status", out.Status, "reason", out.Reason)
		}
		job.step()
	}
	if err := engine.FinishApply(plan.ID); err != nil {
		slog.Warn("tidy: settle a plan", "plan", plan.ID, "err", err)
	}
	job.finish(nil)
	slog.Info("tidy: apply finished", "plan", plan.ID)
}

// tidyCoverFile fetches the album's front cover to a temporary file when a
// file of the album is to get one. No cover (or no network) tidies without.
func (s *Server) tidyCoverFile(ctx context.Context, a musictidy.AlbumRow) (string, func()) {
	nothing := func() {}
	wanted := false
	for _, it := range a.Items {
		wanted = wanted || it.Plan.Cover
	}
	if !wanted {
		return "", nothing
	}
	var d tidyDetail
	if err := json.Unmarshal(a.Detail, &d); err != nil || d.ReleaseGroupID == "" {
		return "", nothing
	}
	data, contentType, err := s.MusicBrainz().FrontCover(ctx, d.ReleaseGroupID)
	if err != nil {
		slog.Info("tidy: no cover to embed", "album", a.Title, "err", err)
		return "", nothing
	}
	pattern := "usherr-cover-*.jpg"
	if contentType == "image/png" {
		pattern = "usherr-cover-*.png"
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		slog.Warn("tidy: keep a cover for embedding", "err", err)
		return "", nothing
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		cleanup()
		return "", nothing
	}
	return f.Name(), cleanup
}

// POST /api/music/tidy/plans/{id}/undo: puts a plan's albums back.
func (s *Server) handleTidyUndo(w http.ResponseWriter, r *http.Request) {
	id, ok := tidyPlanID(w, r)
	if !ok {
		return
	}
	plan, err := s.tidyStore().Plan(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That check isn't available any more.")
		return
	}
	if plan.Status != musictidy.PlanApplied {
		writeError(w, http.StatusBadRequest, "There is nothing to undo in that check.")
		return
	}
	job, created := s.startTidy(tidyUndo, id)
	if !created {
		writeError(w, http.StatusConflict, "Usherr is still working on your music. Try again when that has finished.")
		return
	}
	s.background(func() { s.runTidyUndo(job, plan) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *Server) runTidyUndo(job *tidyJob, plan musictidy.PlanRow) {
	slog.Info("tidy: undoing", "plan", plan.ID)
	res, err := s.tidyEngine(plan.Root).Undo(job.ctx, plan.ID)
	job.mu.Lock()
	job.undo = res
	job.total, job.done = len(res), len(res)
	job.mu.Unlock()
	if err != nil {
		slog.Warn("tidy: undo", "plan", plan.ID, "err", err)
	}
	// The media servers read the folders back.
	if rows, aerr := s.tidyStore().Albums(plan.ID); aerr == nil {
		for _, a := range rows {
			if a.Status != musictidy.StatusUndone {
				continue
			}
			var files []string
			for _, it := range a.Items {
				files = append(files, it.OldPath)
			}
			s.musicImported(files...)
			if a.AlbumID > 0 {
				s.albumEvent(a.AlbumID, "tidied", queue.LevelInfo, fmt.Sprintf("Tidying undone: the album is back in %s", a.OldFolder))
			}
		}
	}
	job.finish(err)
}

// POST /api/music/tidy/recycle/empty: removes the kept originals now.
func (s *Server) handleTidyRecycleEmpty(w http.ResponseWriter, r *http.Request) {
	t := &s.music.tidy
	t.mu.Lock()
	job := t.job
	t.mu.Unlock()
	if job != nil && job.running() && job.kind != tidyCheck {
		writeError(w, http.StatusConflict, "Usherr is still changing your music files. Try again when that has finished.")
		return
	}
	freed, err := s.tidyEngine(s.musicRoot()).Purge(time.Time{})
	if err != nil {
		slog.Warn("tidy: empty the recycle folder", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't empty it all. Check the log for details.")
		return
	}
	slog.Info("tidy: recycle folder emptied by hand", "bytes", freed)
	writeJSON(w, http.StatusOK, map[string]any{"freedBytes": freed, "recycleBytes": musictidy.RecycleSize(s.musicRoot())})
}

// tidyRecycleJob removes originals kept longer than 30 days, daily. It does
// nothing until an album was tidied.
func (s *Server) tidyRecycleJob(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	t := &s.music.tidy
	t.mu.Lock()
	job := t.job
	t.mu.Unlock()
	if job != nil && job.running() && job.kind != tidyCheck {
		return // tomorrow
	}
	freed, err := s.tidyEngine(s.musicRoot()).Purge(time.Now().Add(-musictidy.RecycleKeep))
	if err != nil {
		slog.Warn("tidy: remove old originals", "err", err)
	} else if freed > 0 {
		slog.Info("tidy: old originals removed", "bytes", freed)
	}
}

// GET /api/music/tidy/acoustid: whether the AcoustID key is set (the key
// itself is never sent back).
func (s *Server) handleTidyAcoustIDGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"hasKey": s.acoustIDKey() != ""})
}

// PUT /api/music/tidy/acoustid {key}: saves the AcoustID application key; an
// empty one removes it.
func (s *Server) handleTidyAcoustIDSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key *string `json:"key"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Key == nil {
		writeError(w, http.StatusBadRequest, "Send the AcoustID application key.")
		return
	}
	key := strings.TrimSpace(*req.Key)
	if len(key) > 64 || strings.ContainsAny(key, " \t\r\n") {
		writeError(w, http.StatusBadRequest, "That doesn't look like an AcoustID application key.")
		return
	}
	if err := s.Settings.Set(keyAcoustID, key, true); err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't save the key. Check the log for details.")
		return
	}
	s.handleTidyAcoustIDGet(w, r)
}

// musicTidyRoutes adds the administrator's tidy routes.
func (s *Server) musicTidyRoutes(admin routeGroup) {
	admin.HandleFunc("GET /api/music/tidy", s.handleTidyOverview)
	admin.HandleFunc("POST /api/music/tidy/check", s.handleTidyCheck)
	admin.HandleFunc("POST /api/music/tidy/stop", s.handleTidyStop)
	admin.HandleFunc("GET /api/music/tidy/plans/{id}", s.handleTidyPlan)
	admin.HandleFunc("POST /api/music/tidy/plans/{id}/apply", s.handleTidyApply)
	admin.HandleFunc("POST /api/music/tidy/plans/{id}/undo", s.handleTidyUndo)
	admin.HandleFunc("POST /api/music/tidy/recycle/empty", s.handleTidyRecycleEmpty)
	admin.HandleFunc("GET /api/music/tidy/acoustid", s.handleTidyAcoustIDGet)
	admin.HandleFunc("PUT /api/music/tidy/acoustid", s.handleTidyAcoustIDSave)
}
