// Adapted from DroppedNeedle (AGPL-3.0), backend/services/native/library_management_publisher.py
// and backend/services/native/library_management_undo_service.py

package musictidy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RecycleKeep is how long originals are kept after tidying.
const RecycleKeep = 30 * 24 * time.Hour

// Engine applies plans to the disk and takes them back. It only ever works
// inside Root, and never deletes an audio file it did not write itself: an
// original goes to the recycle folder until that is emptied.
type Engine struct {
	Store *Store
	DB    *sql.DB // the library's tracks and albums
	Root  string
}

// Outcome is what became of one album.
type Outcome struct {
	Status string   // StatusApplied, StatusStale, StatusBlocked, StatusFailed, StatusUndone
	Reason string   // for anything else than done
	Dests  []string // the album's files as they are now
}

// insideRoot checks a path that may not exist yet: its nearest existing
// folder, links resolved, must be inside the music root.
func (e *Engine) insideRoot(path string) error {
	root, err := filepath.EvalSymlinks(e.Root)
	if err != nil {
		return fmt.Errorf("resolve the music folder: %w", err)
	}
	if !inside(path, e.Root) {
		return fmt.Errorf("%s is outside the music folder", path)
	}
	at := path
	for {
		real, err := filepath.EvalSymlinks(at)
		if err == nil {
			if !inside(real, root) {
				return fmt.Errorf("%s is outside the music folder", path)
			}
			return nil
		}
		parent := filepath.Dir(at)
		if parent == at {
			return fmt.Errorf("%s: %w", path, err)
		}
		at = parent
	}
}

// ApplyAlbum tidies one album row. coverFile, when set, is a picture to embed
// in the files planned to get one. A file that changed since the check
// makes the whole album stale; anything that goes wrong halfway is taken back,
// so an album is either tidied or as it was.
func (e *Engine) ApplyAlbum(ctx context.Context, row AlbumRow, coverFile string) Outcome {
	// Marked first: after a crash halfway, undo still finds this album.
	if err := e.Store.SetAlbumStatus(row.ID, StatusApplying, ""); err != nil {
		return Outcome{Status: StatusFailed, Reason: err.Error()}
	}
	if err := e.Store.markApplied(row.PlanID); err != nil {
		return Outcome{Status: StatusFailed, Reason: err.Error()}
	}
	out := e.applyAlbum(ctx, row, coverFile)
	if err := e.Store.SetAlbumStatus(row.ID, out.Status, out.Reason); err != nil {
		slog.Warn("tidy: store an album's result", "album", row.Title, "err", err)
	}
	return out
}

func (e *Engine) applyAlbum(ctx context.Context, row AlbumRow, coverFile string) Outcome {
	// Everything is checked before anything is touched.
	if err := e.insideRoot(row.OldFolder); err != nil {
		return Outcome{Status: StatusBlocked, Reason: err.Error()}
	}
	if err := e.insideRoot(row.NewFolder); err != nil {
		return Outcome{Status: StatusBlocked, Reason: err.Error()}
	}
	for _, it := range row.Items {
		if err := e.insideRoot(it.OldPath); err != nil {
			return Outcome{Status: StatusBlocked, Reason: err.Error()}
		}
		if !inside(it.NewPath, e.Root) {
			return Outcome{Status: StatusBlocked, Reason: fmt.Sprintf("%s is outside the music folder", it.NewPath)}
		}
		st, link, err := StateOf(it.OldPath)
		switch {
		case err != nil:
			return Outcome{Status: StatusStale, Reason: fmt.Sprintf("%s isn't there any more. Check your music again.", filepath.Base(it.OldPath))}
		case link:
			return Outcome{Status: StatusBlocked, Reason: fmt.Sprintf("%s is a link to a file elsewhere.", filepath.Base(it.OldPath))}
		case st != (State{Size: it.Plan.Size, ModNs: it.Plan.ModNs}):
			return Outcome{Status: StatusStale, Reason: fmt.Sprintf("%s changed since the check. Check your music again.", filepath.Base(it.OldPath))}
		}
	}

	if err := e.doApply(ctx, row, coverFile); err != nil {
		// Back to how it was, as far as this album got.
		problems, uerr := e.takeBack(row.PlanID, row.ID)
		if uerr != nil {
			slog.Warn("tidy: take back an album that failed", "album", row.Title, "err", uerr)
			problems = append(problems, uerr.Error())
		}
		left := ""
		if len(problems) > 0 {
			left = " Not everything could be put back: " + strings.Join(problems, " ")
		}
		if errors.Is(err, ErrExists) {
			return Outcome{Status: StatusBlocked, Reason: fmt.Sprintf("A file is in the way: %v.%s", err, left)}
		}
		if ctx.Err() != nil {
			return Outcome{Status: StatusFailed, Reason: "Stopped before this album was done, so it was left as it was." + left}
		}
		return Outcome{Status: StatusFailed, Reason: err.Error() + left}
	}
	if err := e.updateLibrary(row, func(it ItemRow) string { return it.NewPath }, row.NewFolder); err != nil {
		slog.Warn("tidy: update the library's paths", "album", row.Title, "err", err)
		return Outcome{Status: StatusApplied, Reason: "The files were tidied, but the library couldn't be updated. Rescan the album."}
	}
	out := Outcome{Status: StatusApplied}
	for _, it := range row.Items {
		out.Dests = append(out.Dests, it.NewPath)
	}
	return out
}

// doApply does the moves and writes, recording each in the journal.
func (e *Engine) doApply(ctx context.Context, row AlbumRow, coverFile string) error {
	current := func(old string) string { return old }
	if row.NewFolder != row.OldFolder {
		if err := e.makeParents(row, row.NewFolder); err != nil {
			return err
		}
		if err := Move(row.OldFolder, row.NewFolder); err != nil {
			return err
		}
		if err := e.Store.record(row.PlanID, row.ID, stepFolder, row.OldFolder, row.NewFolder, State{}); err != nil {
			return err
		}
		current = func(old string) string {
			rel, err := filepath.Rel(row.OldFolder, old)
			if err != nil {
				return old
			}
			return filepath.Join(row.NewFolder, rel)
		}
	}

	// First everything to a hidden name, then to its final one, so two files
	// can trade names.
	type pending struct {
		from, to string
	}
	var moves, sidecars []pending
	for _, it := range row.Items {
		if err := ctx.Err(); err != nil {
			return err
		}
		cur := current(it.OldPath)
		for _, sc := range it.Plan.Sidecars {
			sidecars = append(sidecars, pending{current(sc.OldPath), sc.NewPath})
		}
		switch {
		case it.Plan.Rewrites():
			tmp := TempName(filepath.Join(row.NewFolder, filepath.Base(cur)))
			cover := ""
			if it.Plan.Cover {
				cover = coverFile
			}
			if err := Rewrite(ctx, cur, tmp, it.Plan.Set, cover); err != nil {
				// A cover is a nicety: a picture ffmpeg can't take never
				// holds up the names and tags.
				if cover == "" || ctx.Err() != nil {
					return err
				}
				slog.Info("tidy: tidying without the cover", "file", cur, "err", err)
				if err := Rewrite(ctx, cur, tmp, it.Plan.Set, ""); err != nil {
					return err
				}
			}
			st, _, err := StateOf(tmp)
			if err != nil {
				return err
			}
			if err := e.Store.record(row.PlanID, row.ID, stepWrite, "", tmp, st); err != nil {
				_ = os.Remove(tmp)
				return err
			}
			rel, err := filepath.Rel(e.Root, it.OldPath)
			if err != nil {
				return err
			}
			kept := filepath.Join(RecycleDir(e.Root, row.PlanID), rel)
			orig, _, err := StateOf(cur)
			if err != nil {
				return err
			}
			if err := Move(cur, kept); err != nil {
				return err
			}
			if err := e.Store.record(row.PlanID, row.ID, stepRecycle, cur, kept, orig); err != nil {
				return err
			}
			moves = append(moves, pending{tmp, it.NewPath})
		case cur != it.NewPath:
			tmp := TempName(filepath.Join(row.NewFolder, filepath.Base(cur)))
			if err := Move(cur, tmp); err != nil {
				return err
			}
			st, _, err := StateOf(tmp)
			if err != nil {
				return err
			}
			if err := e.Store.record(row.PlanID, row.ID, stepMove, cur, tmp, st); err != nil {
				return err
			}
			moves = append(moves, pending{tmp, it.NewPath})
		}
	}
	for _, m := range moves {
		if err := ctx.Err(); err != nil {
			return err
		}
		st, _, err := StateOf(m.from)
		if err != nil {
			return err
		}
		if err := Move(m.from, m.to); err != nil {
			return err
		}
		if err := e.Store.record(row.PlanID, row.ID, stepMove, m.from, m.to, st); err != nil {
			return err
		}
	}
	for _, m := range sidecars {
		st, _, err := StateOf(m.from)
		if err != nil {
			continue // it went away since the check; it's not the music
		}
		if err := Move(m.from, m.to); err != nil {
			return err
		}
		if err := e.Store.record(row.PlanID, row.ID, stepMove, m.from, m.to, st); err != nil {
			return err
		}
	}
	removeEmptySubfolders(row.NewFolder)
	if row.NewFolder != row.OldFolder {
		if parent := filepath.Dir(row.OldFolder); parent != e.Root && inside(parent, e.Root) {
			_ = os.Remove(parent) // an artist folder left empty; refused otherwise
		}
	}
	return nil
}

// makeParents makes the folders above dir that are missing, journalling each
// so undo can remove them again.
func (e *Engine) makeParents(row AlbumRow, dir string) error {
	var missing []string
	for at := filepath.Dir(dir); inside(at, e.Root) && at != e.Root; at = filepath.Dir(at) {
		if _, err := os.Stat(at); err == nil {
			break
		}
		missing = append(missing, at)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := e.Store.record(row.PlanID, row.ID, stepMkdir, "", missing[i], State{}); err != nil {
			return err
		}
	}
	return nil
}

// removeEmptySubfolders drops the CD1/CD2 folders left empty inside an album
// folder. The album folder itself stays.
func removeEmptySubfolders(dir string) {
	var subs []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != dir {
			subs = append(subs, p)
		}
		return nil
	})
	sort.Slice(subs, func(i, j int) bool { return len(subs[i]) > len(subs[j]) })
	for _, s := range subs {
		_ = os.Remove(s) // refuses a folder with anything in it
	}
}

// updateLibrary points the library's tracks and album at their files.
func (e *Engine) updateLibrary(row AlbumRow, pathOf func(ItemRow) string, folder string) error {
	if e.DB == nil {
		return nil
	}
	tx, err := e.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, it := range row.Items {
		if it.TrackID == 0 {
			continue
		}
		if p := pathOf(it); p != "" {
			if _, err := tx.Exec(`UPDATE tracks SET file_path = ? WHERE id = ?`, p, it.TrackID); err != nil {
				return err
			}
		}
	}
	if row.AlbumID > 0 {
		if _, err := tx.Exec(`UPDATE albums SET path = ? WHERE id = ?`, folder, row.AlbumID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FinishApply settles a plan after an apply: one that changed nothing goes
// back to being only a check.
func (e *Engine) FinishApply(planID int64) error { return e.Store.settlePlan(planID) }

// UndoResult says what undoing an album did.
type UndoResult struct {
	AlbumRow int64    `json:"albumRow"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Problems []string `json:"problems,omitempty"`
}

// Undo takes a plan's applied albums back: each original returns from the
// recycle folder to its old path, and the tidied copy is removed only when it
// is unchanged since it was written. What can't be done (an original already
// purged, a file changed since) is reported, not forced.
func (e *Engine) Undo(ctx context.Context, planID int64) ([]UndoResult, error) {
	albums, err := e.Store.Albums(planID)
	if err != nil {
		return nil, err
	}
	var out []UndoResult
	for _, row := range albums {
		if row.Status != StatusApplied && row.Status != StatusApplying {
			continue
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		problems, err := e.takeBack(planID, row.ID)
		if err != nil {
			problems = append(problems, err.Error())
		}
		res := UndoResult{AlbumRow: row.ID, Title: row.Title, Status: StatusUndone, Problems: problems}
		reason := ""
		if len(problems) > 0 {
			reason = "Not everything could be put back: " + strings.Join(problems, " ")
		}
		// Each track points at where its file ended up.
		pathOf := func(it ItemRow) string {
			if _, _, err := StateOf(it.OldPath); err == nil {
				return it.OldPath
			}
			if rel, err := filepath.Rel(row.NewFolder, it.NewPath); err == nil {
				if p := filepath.Join(row.OldFolder, rel); fileThere(p) {
					return p
				}
			}
			if fileThere(it.NewPath) {
				return it.NewPath
			}
			return ""
		}
		folder := row.OldFolder
		if !dirThere(row.OldFolder) {
			folder = row.NewFolder
		}
		if err := e.updateLibrary(row, pathOf, folder); err != nil {
			slog.Warn("tidy: restore the library's paths", "album", row.Title, "err", err)
		}
		if err := e.Store.SetAlbumStatus(row.ID, StatusUndone, reason); err != nil {
			slog.Warn("tidy: store an album's result", "album", row.Title, "err", err)
		}
		out = append(out, res)
	}
	if err := e.Store.markUndone(planID); err != nil {
		return out, err
	}
	return out, nil
}

func fileThere(p string) bool {
	_, _, err := StateOf(p)
	return err == nil
}

func dirThere(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// takeBack reverses the journal of one album row, newest step first, and
// returns what could not be taken back.
func (e *Engine) takeBack(planID, albumRow int64) ([]string, error) {
	all, err := e.Store.steps(planID)
	if err != nil {
		return nil, err
	}
	var steps []step
	for _, st := range all {
		if st.AlbumRow == albumRow {
			steps = append(steps, st)
		}
	}
	// A rewritten file whose original can't come back keeps its tidied copy:
	// there would be nothing left otherwise. Its three steps are left be.
	// The journal holds [write tmp, recycle original, move tmp to final].
	keep := map[int64]string{}
	for i, st := range steps {
		if st.Step != stepRecycle {
			continue
		}
		if _, _, err := StateOf(st.To); err == nil {
			continue
		}
		name := filepath.Base(st.From)
		note := fmt.Sprintf("The original of %s is no longer kept, so the tidied file stays.", name)
		keep[st.ID] = note
		if i+1 < len(steps) && steps[i+1].Step == stepWrite {
			tmp := steps[i+1].To
			keep[steps[i+1].ID] = note
			for _, m := range steps {
				if m.Step == stepMove && m.From == tmp {
					keep[m.ID] = note
				}
			}
		}
	}

	var problems []string
	seen := map[string]bool{}
	for _, st := range steps {
		note, kept := keep[st.ID]
		if kept {
			if err := e.Store.settle(st.ID, 2, note); err != nil {
				return problems, err
			}
			if !seen[note] {
				seen[note] = true
				problems = append(problems, note)
			}
			continue
		}
		note = takeBackStep(st)
		undone := 1
		if note != "" {
			undone = 2
			problems = append(problems, note)
		}
		if err := e.Store.settle(st.ID, undone, note); err != nil {
			return problems, err
		}
	}
	return problems, nil
}

// takeBackStep reverses one step; the answer says why it couldn't.
func takeBackStep(st step) string {
	switch st.Step {
	case stepMove:
		now, _, err := StateOf(st.To)
		if err != nil {
			return fmt.Sprintf("%s isn't there any more.", filepath.Base(st.To))
		}
		if now != st.State {
			return fmt.Sprintf("%s changed after it was tidied, so it stays as it is.", filepath.Base(st.To))
		}
		if err := Move(st.To, st.From); err != nil {
			return fmt.Sprintf("%s couldn't be put back: %v", filepath.Base(st.From), err)
		}
	case stepWrite:
		if !fileThere(st.To) {
			return "" // it was kept at its new name, already reported
		}
		removed, err := RemoveIfUnchanged(st.To, st.State)
		if err != nil {
			return fmt.Sprintf("%s couldn't be removed: %v", filepath.Base(st.To), err)
		}
		if !removed {
			return fmt.Sprintf("%s changed after it was tidied, so it stays as it is.", filepath.Base(st.To))
		}
	case stepRecycle:
		if err := Move(st.To, st.From); err != nil {
			return fmt.Sprintf("The original of %s couldn't be put back: %v", filepath.Base(st.From), err)
		}
	case stepFolder:
		if err := Move(st.To, st.From); err != nil {
			return fmt.Sprintf("The folder %s couldn't be put back: %v", filepath.Base(st.From), err)
		}
	case stepMkdir:
		_ = os.Remove(st.To) // only when empty
	}
	return ""
}

// RecycleSize is the space the kept originals take.
func RecycleSize(root string) int64 {
	if root == "" {
		return 0
	}
	return dirSize(filepath.Join(root, RecycleFolder))
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// Purge removes the kept originals of plans applied before cutoff (a zero
// cutoff means all of them) and returns the space freed. Only the recycle
// folders of known plans are touched.
func (e *Engine) Purge(cutoff time.Time) (int64, error) {
	plans, err := e.Store.recycledPlans()
	if err != nil {
		return 0, err
	}
	var freed int64
	roots := map[string]bool{e.Root: true}
	for _, p := range plans {
		if !cutoff.IsZero() {
			when, err := time.Parse(time.RFC3339, p.AppliedAt)
			if err != nil || when.After(cutoff) {
				continue
			}
		}
		// A plan's originals are under the music folder it was made for.
		pe := &Engine{Store: e.Store, DB: e.DB, Root: p.Root}
		roots[p.Root] = true
		dir := RecycleDir(p.Root, p.ID)
		if err := pe.insideRoot(dir); err != nil {
			slog.Warn("tidy: kept originals left in place", "plan", p.ID, "err", err)
			continue
		}
		freed += dirSize(dir)
		if err := os.RemoveAll(dir); err != nil {
			return freed, fmt.Errorf("empty %s: %w", dir, err)
		}
		if err := e.Store.MarkRecycled(p.ID); err != nil {
			return freed, err
		}
		slog.Info("tidy: kept originals removed", "plan", p.ID, "applied", p.AppliedAt)
	}
	for root := range roots {
		if root != "" {
			_ = os.Remove(filepath.Join(root, RecycleFolder)) // only when empty
		}
	}
	return freed, nil
}
