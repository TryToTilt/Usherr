package api

// Usherr: the NSFW module's tools for administrators: matching files by
// fingerprint, bringing in StashDB favorites, and renaming with a preview and
// undo. Identify and rename run only when an administrator presses a button,
// one job at a time, which the page polls. Modelled on music_tidy.go.
//
// Video names and paths stay out of the shared log: it reports counts only.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/nsfw"
	"github.com/rdborg/mediarium/internal/organizer"
)

const (
	nsfwJobIdentify = "identify"
	nsfwJobRegister = "register"
	nsfwJobCheck    = "check"
	nsfwJobApply    = "apply"
	nsfwJobUndo     = "undo"

	nsfwJobRunning = "running"
	nsfwJobDone    = "done"
	nsfwJobFailed  = "failed"
	nsfwJobStopped = "stopped"

	videoEventRenamed = "nsfw-renamed"
	videoEventMatched = "nsfw-matched"
	videoEventFollows = "nsfw-favorites"

	maxNsfwSelection = 20000
)

// nsfwToolState is the tools' in-memory part.
type nsfwToolState struct {
	mu        sync.Mutex
	job       *nsfwJob
	favorites sync.Mutex // one read of the favorites at a time
}

var nsfwTools sync.Map // *Server -> *nsfwToolState

func (s *Server) nsfwToolMod() *nsfwToolState {
	v, _ := nsfwTools.LoadOrStore(s, &nsfwToolState{})
	return v.(*nsfwToolState)
}

// nsfwJob is the running or last job.
type nsfwJob struct {
	mu      sync.Mutex
	kind    string
	phase   string
	planID  int64
	done    int
	total   int
	current string
	err     string
	undo    []nsfw.UndoResult
	ctx     context.Context
	cancel  context.CancelFunc
}

type nsfwJobPayload struct {
	Kind    string            `json:"kind"`
	Phase   string            `json:"phase"`
	PlanID  int64             `json:"planId"`
	Done    int               `json:"done"`
	Total   int               `json:"total"`
	Current string            `json:"current,omitempty"`
	Error   string            `json:"error,omitempty"`
	Undo    []nsfw.UndoResult `json:"undo,omitempty"`
}

func (j *nsfwJob) snapshot() nsfwJobPayload {
	j.mu.Lock()
	defer j.mu.Unlock()
	return nsfwJobPayload{Kind: j.kind, Phase: j.phase, PlanID: j.planID, Done: j.done, Total: j.total, Current: j.current, Error: j.err, Undo: j.undo}
}

func (j *nsfwJob) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.phase == nsfwJobRunning
}

func (j *nsfwJob) working(name string) {
	j.mu.Lock()
	j.current = name
	j.mu.Unlock()
}

func (j *nsfwJob) step() {
	j.mu.Lock()
	j.done++
	j.mu.Unlock()
}

func (j *nsfwJob) setTotal(n int) {
	j.mu.Lock()
	j.total = n
	j.mu.Unlock()
}

func (j *nsfwJob) setPlan(id int64) {
	j.mu.Lock()
	j.planID = id
	j.mu.Unlock()
}

// finish ends the job; a job stopped by hand stays stopped.
func (j *nsfwJob) finish(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		j.cancel()
	}
	j.current = ""
	switch {
	case j.phase != nsfwJobRunning:
	case err != nil:
		j.phase, j.err = nsfwJobFailed, err.Error()
	default:
		j.phase = nsfwJobDone
	}
}

func (j *nsfwJob) stop() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.phase == nsfwJobRunning {
		j.phase, j.current = nsfwJobStopped, ""
		if j.cancel != nil {
			j.cancel()
		}
	}
}

// startNsfwJob starts a job, unless one is running: only one at a time.
func (s *Server) startNsfwJob(kind string, planID int64) (*nsfwJob, bool) {
	t := s.nsfwToolMod()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != nil && t.job.running() {
		return t.job, false
	}
	job := &nsfwJob{kind: kind, phase: nsfwJobRunning, planID: planID}
	job.ctx, job.cancel = context.WithCancel(context.Background())
	t.job = job
	return job, true
}

func (s *Server) currentNsfwJob() *nsfwJob {
	t := s.nsfwToolMod()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.job
}

func (s *Server) writeNsfwBusy(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "Usherr is still working on your NSFW files. Try again when that has finished.")
}

// POST /api/nsfw/tools/stop: stops the running job. What it did so far stays,
// and a rename never leaves a video half done.
func (s *Server) handleNsfwToolStop(w http.ResponseWriter, r *http.Request) {
	job := s.currentNsfwJob()
	if job == nil {
		writeError(w, http.StatusNotFound, "Nothing is running.")
		return
	}
	job.stop()
	writeJSON(w, http.StatusOK, job.snapshot())
}

// ---- identify ----

// nsfwRelPath shows a path relative to the folder when it is inside it.
func nsfwRelPath(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return p
}

// GET /api/nsfw/identify: the running or last job and what the last run found.
func (s *Server) handleIdentifyOverview(w http.ResponseWriter, r *http.Request) {
	root := s.nsfwRoot()
	repo := s.videoRepo()
	items, err := repo.IdentifyItems()
	if err != nil {
		slog.Warn("nsfw: read the matches", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the matches. Check the log for details.")
		return
	}
	matched := items[:0]
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Status]++
		if it.Status != nsfw.IdentifyNone && it.Status != nsfw.IdentifyUnreadable {
			it.Path = nsfwRelPath(root, it.Path)
			matched = append(matched, it)
		}
	}
	out := map[string]any{"root": root, "items": matched, "counts": counts}
	if job := s.currentNsfwJob(); job != nil && (job.kind == nsfwJobIdentify || job.kind == nsfwJobRegister) {
		out["job"] = job.snapshot()
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/nsfw/identify/start: looks for files Usherr doesn't know and asks
// StashDB about their fingerprints. Nothing is added to the library. A run
// already going is returned instead of starting another.
func (s *Server) handleIdentifyStart(w http.ResponseWriter, r *http.Request) {
	root := s.nsfwRoot()
	if root == "" {
		writeError(w, http.StatusPreconditionFailed, "Choose the NSFW folder first.")
		return
	}
	if !s.stashKeySet() {
		writeError(w, http.StatusPreconditionFailed, nsfw.ErrNoKey.Error()+".")
		return
	}
	job, created := s.startNsfwJob(nsfwJobIdentify, 0)
	if !created {
		if job.kind == nsfwJobIdentify {
			writeJSON(w, http.StatusOK, job.snapshot())
			return
		}
		s.writeNsfwBusy(w)
		return
	}
	s.background(func() { s.runIdentify(job, root) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *Server) runIdentify(job *nsfwJob, root string) {
	err := s.identifyFiles(job, root)
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("nsfw: find matches", "err", err)
	}
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	job.finish(err)
}

func (s *Server) identifyFiles(job *nsfwJob, root string) error {
	repo := s.videoRepo()
	known, err := repo.KnownFilePaths()
	if err != nil {
		return err
	}
	files, err := nsfw.ScanFiles(job.ctx, root, known)
	if err != nil {
		return err
	}
	noID, err := repo.FilesWithoutStashID()
	if err != nil {
		return err
	}
	files = append(files, noID...)
	job.setTotal(len(files))
	if err := repo.ClearIdentify(); err != nil {
		return err
	}
	id := nsfw.Identifier{Repo: repo, Stash: s.stashBox(), Working: job.working, Step: job.step}
	return id.Run(job.ctx, files)
}

// POST /api/nsfw/identify/confirm {items:[{id,stashId}]}: registers the chosen
// files as downloaded, where they are. No file is moved.
func (s *Server) handleIdentifyConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			ID      int64  `json:"id"`
			StashID string `json:"stashId"`
		} `json:"items"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "Choose the files to add.")
		return
	}
	if len(req.Items) > maxNsfwSelection {
		writeError(w, http.StatusBadRequest, "That is too many at once.")
		return
	}
	ids := make([]int64, 0, len(req.Items))
	choice := map[int64]string{}
	for _, it := range req.Items {
		ids = append(ids, it.ID)
		choice[it.ID] = it.StashID
	}
	items, err := s.videoRepo().IdentifyItems(ids...)
	if err != nil {
		slog.Warn("nsfw: read matches to confirm", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the matches. Check the log for details.")
		return
	}
	if len(items) == 0 {
		writeError(w, http.StatusBadRequest, "None of those files can be added. Find matches again.")
		return
	}
	job, created := s.startNsfwJob(nsfwJobRegister, 0)
	if !created {
		s.writeNsfwBusy(w)
		return
	}
	s.background(func() { s.runIdentifyConfirm(job, items, choice) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *Server) runIdentifyConfirm(job *nsfwJob, items []nsfw.IdentifyItem, choice map[int64]string) {
	repo := s.videoRepo()
	job.setTotal(len(items))
	added := 0
	var firstErr error
	for _, it := range items {
		if job.ctx.Err() != nil {
			break
		}
		job.working(filepath.Base(it.Path))
		ok, err := repo.ConfirmIdentify(it, choice[it.ID])
		if err != nil {
			slog.Warn("nsfw: add a matched file", "item", it.ID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
		if ok {
			added++
		}
		job.step()
	}
	if added > 0 {
		_ = s.QueueRepo.LogActivity(0, videoEventMatched, fmt.Sprintf("%d files added to NSFW 18+ by their fingerprint, where they are", added))
	}
	slog.Info("nsfw: matched files added", "added", added)
	job.finish(firstErr)
}

// ---- favorites ----

const (
	keyNsfwFavoritesAuto = "nsfw.favorites_auto" // "1" repeats the favorites daily
	keyNsfwFavoritesLast = "nsfw.favorites_last" // when they were last brought in by the daily job
)

// favorites run at night: the household's heavy work keeps to these hours, and
// this keeps to them although it only reads a little from StashDB.
const (
	favoritesFromHour = 2
	favoritesToHour   = 9
	favoritesEvery    = 20 * time.Hour // less than a day, so the time of day can drift
)

// favoritesDue reports whether the daily job should run at now: it's switched
// on, it's between 2 and 9 in the morning, and it hasn't run in the last while.
func favoritesDue(on bool, last string, now time.Time) bool {
	if !on || now.Hour() < favoritesFromHour || now.Hour() >= favoritesToHour {
		return false
	}
	if t, err := time.Parse(time.RFC3339, last); err == nil && now.Sub(t) < favoritesEvery {
		return false
	}
	return true
}

type favoritesRead struct {
	performers []nsfw.Performer
	studios    []nsfw.Studio
}

// readFavorites reads the account's favorites, with a pause before the second
// question, and works out what would change.
func (s *Server) readFavorites(ctx context.Context) (favoritesRead, nsfw.FavoriteSide, nsfw.FavoriteSide, error) {
	var read favoritesRead
	var ps, ss nsfw.FavoriteSide
	var err error
	sb := s.stashBox()
	if read.performers, err = sb.FavoritePerformers(ctx); err != nil {
		return read, ps, ss, err
	}
	select {
	case <-ctx.Done():
		return read, ps, ss, ctx.Err()
	case <-time.After(nsfw.RequestGap):
	}
	if read.studios, err = sb.FavoriteStudios(ctx); err != nil {
		return read, ps, ss, err
	}
	repo := s.videoRepo()
	if ps, err = repo.DiffPerformerFavorites(read.performers); err != nil {
		return read, ps, ss, err
	}
	ss, err = repo.DiffStudioFavorites(read.studios)
	return read, ps, ss, err
}

// followFavorites follows the favorites named in the two sides, from now on.
// It adds no video and starts no search: the usual follow check picks up what
// comes out after today. It never unfollows anything.
func (s *Server) followFavorites(read favoritesRead, ps, ss nsfw.FavoriteSide, wantP, wantS func(string) bool) (int, int, error) {
	repo := s.videoRepo()
	follow := func(side nsfw.FavoriteSide, want func(string) bool) map[string]bool {
		out := map[string]bool{}
		for _, f := range side.ToFollow {
			if want(f.StashID) {
				out[f.StashID] = true
			}
		}
		return out
	}
	addP, addS := follow(ps, wantP), follow(ss, wantS)
	performers, studios := 0, 0
	for _, p := range read.performers {
		if addP[p.StashID] {
			if err := repo.FollowPerformerFromNow(p); err != nil {
				return performers, studios, err
			}
			performers++
		}
	}
	for _, st := range read.studios {
		if addS[st.StashID] {
			if err := repo.FollowStudioFromNow(st); err != nil {
				return performers, studios, err
			}
			studios++
		}
	}
	if performers+studios > 0 {
		_ = s.QueueRepo.LogActivity(0, videoEventFollows, fmt.Sprintf("Now following %d performers and %d studios from your StashDB favorites", performers, studios))
	}
	return performers, studios, nil
}

func (s *Server) favoritesAuto() bool {
	v, _ := s.Settings.Get(keyNsfwFavoritesAuto)
	return v == "1"
}

func (s *Server) writeFavoritesError(w http.ResponseWriter, err error) {
	if nsfw.IsFavoriteSchemaError(err) {
		writeError(w, http.StatusBadGateway, "This stash-box server can't share favorites. It may be an older version than StashDB's.")
		return
	}
	s.writeStashError(w, err)
}

// GET /api/nsfw/favorites: whether the daily repeat is on, and when it last ran.
func (s *Server) handleFavoritesGet(w http.ResponseWriter, r *http.Request) {
	last, _ := s.Settings.Get(keyNsfwFavoritesLast)
	writeJSON(w, http.StatusOK, map[string]any{"auto": s.favoritesAuto(), "last": last})
}

// PUT /api/nsfw/favorites/auto {on}: repeats bringing in favorites every night.
func (s *Server) handleFavoritesAuto(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On *bool `json:"on"`
	}
	if err := decodeJSON(r, &req); err != nil || req.On == nil {
		writeError(w, http.StatusBadRequest, "Say whether to switch it on or off.")
		return
	}
	v := ""
	if *req.On {
		v = "1"
	}
	if err := s.Settings.Set(keyNsfwFavoritesAuto, v, false); err != nil {
		writeError(w, http.StatusInternalServerError, "The switch couldn't be saved.")
		return
	}
	s.handleFavoritesGet(w, r)
}

// POST /api/nsfw/favorites/preview: reads the favorites of the account behind
// the saved key and shows what would be added. Nothing changes.
func (s *Server) handleFavoritesPreview(w http.ResponseWriter, r *http.Request) {
	t := s.nsfwToolMod()
	if !t.favorites.TryLock() {
		writeError(w, http.StatusConflict, "Usherr is already reading your favorites. Try again in a moment.")
		return
	}
	defer t.favorites.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	_, ps, ss, err := s.readFavorites(ctx)
	if err != nil {
		s.writeFavoritesError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"performers": ps, "studios": ss})
}

// POST /api/nsfw/favorites/apply {performers:[ids],studios:[ids]}: follows the
// favorites chosen from the preview, which must still be favorites. Only adds
// follows, and never writes anything to StashDB.
func (s *Server) handleFavoritesApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Performers []string `json:"performers"`
		Studios    []string `json:"studios"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Performers)+len(req.Studios) == 0 {
		writeError(w, http.StatusBadRequest, "Choose what to follow.")
		return
	}
	t := s.nsfwToolMod()
	if !t.favorites.TryLock() {
		writeError(w, http.StatusConflict, "Usherr is already reading your favorites. Try again in a moment.")
		return
	}
	defer t.favorites.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	read, ps, ss, err := s.readFavorites(ctx)
	if err != nil {
		s.writeFavoritesError(w, err)
		return
	}
	in := func(list []string) func(string) bool {
		set := map[string]bool{}
		for _, id := range list {
			set[id] = true
		}
		return func(id string) bool { return set[id] }
	}
	performers, studios, err := s.followFavorites(read, ps, ss, in(req.Performers), in(req.Studios))
	if err != nil {
		slog.Warn("nsfw: follow favorites", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't follow them all. Check the log for details.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"performers": performers, "studios": studios})
}

// favoritesJob brings in new favorites once a night, when switched on. It only
// reads from StashDB, adds follows, and leaves searching to the usual flow.
func (s *Server) favoritesJob(ctx context.Context) {
	if !s.nsfwEnabled() || !s.stashKeySet() {
		return
	}
	last, _ := s.Settings.Get(keyNsfwFavoritesLast)
	if !favoritesDue(s.favoritesAuto(), last, time.Now()) || !s.taskAllowed(taskVideoFollow) {
		return
	}
	t := s.nsfwToolMod()
	if !t.favorites.TryLock() {
		return
	}
	defer t.favorites.Unlock()
	read, ps, ss, err := s.readFavorites(ctx)
	if err != nil {
		slog.Info("nsfw: daily favorites", "err", err)
		return
	}
	all := func(string) bool { return true }
	performers, studios, err := s.followFavorites(read, ps, ss, all, all)
	if err != nil {
		slog.Warn("nsfw: daily favorites", "err", err)
		return
	}
	_ = s.Settings.Set(keyNsfwFavoritesLast, time.Now().UTC().Format(time.RFC3339), false)
	slog.Info("nsfw: daily favorites", "performers", performers, "studios", studios)
}

// ---- rename ----

func (s *Server) nsfwCleanName() func(string) string {
	mode, repl := s.illegalCharSettings()
	return func(name string) string { return organizer.Sanitize(name, mode, repl) }
}

// GET /api/nsfw/rename: the running or last job and the plans so far.
func (s *Server) handleRenameOverview(w http.ResponseWriter, r *http.Request) {
	plans, err := s.videoRepo().RenamePlans()
	if err != nil {
		slog.Warn("nsfw: list rename plans", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the history. Check the log for details.")
		return
	}
	out := map[string]any{"root": s.nsfwRoot(), "plans": plans}
	if job := s.currentNsfwJob(); job != nil && job.kind != nsfwJobIdentify && job.kind != nsfwJobRegister {
		out["job"] = job.snapshot()
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/nsfw/rename/check: works out new names for downloaded videos and
// changes nothing. A check already running is returned instead.
func (s *Server) handleRenameCheck(w http.ResponseWriter, r *http.Request) {
	root := s.nsfwRoot()
	if root == "" {
		writeError(w, http.StatusPreconditionFailed, "Choose the NSFW folder first.")
		return
	}
	job, created := s.startNsfwJob(nsfwJobCheck, 0)
	if !created {
		if job.kind == nsfwJobCheck {
			writeJSON(w, http.StatusOK, job.snapshot())
			return
		}
		s.writeNsfwBusy(w)
		return
	}
	s.background(func() { s.runRenameCheck(job, root) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *Server) runRenameCheck(job *nsfwJob, root string) {
	repo := s.videoRepo()
	counts, _ := repo.Count()
	job.setTotal(counts.Downloaded)
	items, unchanged, err := repo.PlanRename(job.ctx, root, s.nsfwCleanName(), job.working, job.step)
	if err == nil {
		var id int64
		if id, err = repo.NewRenamePlan(root, unchanged, items); err == nil {
			job.setPlan(id)
		}
	}
	if errors.Is(err, context.Canceled) {
		err = nil
	} else if err != nil {
		slog.Warn("nsfw: check names", "err", err)
	}
	job.finish(err)
}

func nsfwPlanID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "That isn't a check.")
		return 0, false
	}
	return id, true
}

type renameItemView struct {
	nsfw.RenameItem
	OldName string `json:"oldName"`
	NewName string `json:"newName"`
}

// GET /api/nsfw/rename/plans/{id}: a plan's videos, old and new names.
func (s *Server) handleRenamePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := nsfwPlanID(w, r)
	if !ok {
		return
	}
	repo := s.videoRepo()
	plan, err := repo.GetRenamePlan(id)
	if errors.Is(err, nsfw.ErrNotFound) {
		writeError(w, http.StatusNotFound, "That check isn't available any more.")
		return
	}
	var items []nsfw.RenameItem
	if err == nil {
		items, err = repo.RenameItems(id)
	}
	if err != nil {
		slog.Warn("nsfw: read a rename plan", "plan", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read that check. Check the log for details.")
		return
	}
	views := make([]renameItemView, 0, len(items))
	for _, it := range items {
		v := renameItemView{RenameItem: it, OldName: nsfwRelPath(plan.Root, it.OldPath)}
		if it.NewPath != "" {
			v.NewName = nsfwRelPath(plan.Root, it.NewPath)
		}
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan": plan, "items": views})
}

// POST /api/nsfw/rename/plans/{id}/apply {items:[ids]}: renames the chosen
// videos of a check. Nothing is overwritten, copied or deleted.
func (s *Server) handleRenameApply(w http.ResponseWriter, r *http.Request) {
	id, ok := nsfwPlanID(w, r)
	if !ok {
		return
	}
	var req struct {
		Items []int64 `json:"items"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "Choose the videos to rename.")
		return
	}
	if len(req.Items) > maxNsfwSelection {
		writeError(w, http.StatusBadRequest, "That is too many at once.")
		return
	}
	repo := s.videoRepo()
	plan, err := repo.GetRenamePlan(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That check isn't available any more.")
		return
	}
	if plan.Root != s.nsfwRoot() {
		writeError(w, http.StatusConflict, "The NSFW folder changed since this check. Check names again.")
		return
	}
	if plan.Status == nsfw.RenameUndone {
		writeError(w, http.StatusConflict, "This check was undone. Check names again.")
		return
	}
	rows, err := repo.RenameItems(id, req.Items...)
	if err != nil {
		slog.Warn("nsfw: read videos to rename", "plan", id, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read that check. Check the log for details.")
		return
	}
	var todo []nsfw.RenameItem
	for _, it := range rows {
		if it.Status == nsfw.ItemEligible || it.Status == nsfw.ItemFailed {
			todo = append(todo, it)
		}
	}
	if len(todo) == 0 {
		writeError(w, http.StatusBadRequest, "None of those videos can be renamed.")
		return
	}
	job, created := s.startNsfwJob(nsfwJobApply, id)
	if !created {
		s.writeNsfwBusy(w)
		return
	}
	s.background(func() { s.runRenameApply(job, plan, todo) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *Server) runRenameApply(job *nsfwJob, plan nsfw.RenamePlan, items []nsfw.RenameItem) {
	repo := s.videoRepo()
	job.setTotal(len(items))
	if err := repo.MarkRenameApplied(plan.ID); err != nil {
		slog.Warn("nsfw: mark a plan applied", "plan", plan.ID, "err", err)
		job.finish(err)
		return
	}
	renamed := 0
	var paths []string
	var firstErr error
	for _, it := range items {
		if job.ctx.Err() != nil {
			break
		}
		job.working(it.Name)
		status, err := repo.ApplyRename(job.ctx, plan, it)
		switch {
		case err != nil && errors.Is(err, context.Canceled):
		case err != nil:
			slog.Warn("nsfw: rename a video", "item", it.ID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		case status == nsfw.ItemApplied:
			renamed++
			// Both folders: the new name is found, and the old one is forgotten.
			paths = append(paths, it.NewPath, it.OldPath)
		}
		job.step()
	}
	if err := repo.SettleRenamePlan(plan.ID); err != nil {
		slog.Warn("nsfw: settle a plan", "plan", plan.ID, "err", err)
	}
	s.mediaServersImported(mediaservers.MediaMovie, paths...)
	if renamed > 0 {
		_ = s.QueueRepo.LogActivity(0, videoEventRenamed, fmt.Sprintf("%d videos renamed and filed", renamed))
	}
	slog.Info("nsfw: renamed", "plan", plan.ID, "videos", renamed)
	job.finish(firstErr)
}

// POST /api/nsfw/rename/plans/{id}/undo: puts a plan's files back and restores
// where the library says they are.
func (s *Server) handleRenameUndo(w http.ResponseWriter, r *http.Request) {
	id, ok := nsfwPlanID(w, r)
	if !ok {
		return
	}
	plan, err := s.videoRepo().GetRenamePlan(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That check isn't available any more.")
		return
	}
	if plan.Status != nsfw.RenameApplied {
		writeError(w, http.StatusBadRequest, "There is nothing to undo in that check.")
		return
	}
	job, created := s.startNsfwJob(nsfwJobUndo, id)
	if !created {
		s.writeNsfwBusy(w)
		return
	}
	s.background(func() { s.runRenameUndo(job, plan) })
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *Server) runRenameUndo(job *nsfwJob, plan nsfw.RenamePlan) {
	repo := s.videoRepo()
	items, _ := repo.RenameItems(plan.ID)
	n := 0
	for _, it := range items {
		if it.Status == nsfw.ItemApplied || it.Status == nsfw.ItemApplying {
			n++
		}
	}
	job.setTotal(n)
	res, paths, err := repo.UndoRename(job.ctx, plan.ID, job.working, job.step)
	job.mu.Lock()
	job.undo = res
	job.mu.Unlock()
	if err != nil {
		slog.Warn("nsfw: undo renames", "plan", plan.ID, "err", err)
	}
	s.mediaServersImported(mediaservers.MediaMovie, paths...)
	back := 0
	for _, u := range res {
		if u.Done {
			back++
		}
	}
	if back > 0 {
		_ = s.QueueRepo.LogActivity(0, videoEventRenamed, fmt.Sprintf("%d videos put back under their old names", back))
	}
	slog.Info("nsfw: renames undone", "plan", plan.ID, "videos", back)
	job.finish(err)
}

// registerNsfwToolRoutes adds the tools' routes, all for administrators: they
// change files and the library, or read the account's StashDB favorites.
func (s *Server) registerNsfwToolRoutes(admin routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	admin.HandleFunc("POST /api/nsfw/tools/stop", on(s.handleNsfwToolStop))
	admin.HandleFunc("GET /api/nsfw/identify", on(s.handleIdentifyOverview))
	admin.HandleFunc("POST /api/nsfw/identify/start", on(s.handleIdentifyStart))
	admin.HandleFunc("POST /api/nsfw/identify/confirm", on(s.handleIdentifyConfirm))
	admin.HandleFunc("GET /api/nsfw/favorites", on(s.handleFavoritesGet))
	admin.HandleFunc("PUT /api/nsfw/favorites/auto", on(s.handleFavoritesAuto))
	admin.HandleFunc("POST /api/nsfw/favorites/preview", on(s.handleFavoritesPreview))
	admin.HandleFunc("POST /api/nsfw/favorites/apply", on(s.handleFavoritesApply))
	admin.HandleFunc("GET /api/nsfw/rename", on(s.handleRenameOverview))
	admin.HandleFunc("POST /api/nsfw/rename/check", on(s.handleRenameCheck))
	admin.HandleFunc("GET /api/nsfw/rename/plans/{id}", on(s.handleRenamePlan))
	admin.HandleFunc("POST /api/nsfw/rename/plans/{id}/apply", on(s.handleRenameApply))
	admin.HandleFunc("POST /api/nsfw/rename/plans/{id}/undo", on(s.handleRenameUndo))
}
