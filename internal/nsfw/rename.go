package nsfw

// Renaming and filing downloaded videos, with a preview and undo. A plan says
// what would happen and changes nothing; an apply renames the chosen files. A
// file is only ever renamed: never copied, overwritten or deleted. Every step
// goes in a journal before it is taken, so undo can put it back, even after a
// crash halfway.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rdborg/mediarium/internal/fileperms"
	"github.com/rdborg/mediarium/internal/organizer"
)

// Statuses of a plan.
const (
	RenamePreview = "preview"
	RenameApplied = "applied"
	RenameUndone  = "undone"
)

// Statuses of a video in a plan.
const (
	ItemEligible = "eligible"
	ItemBlocked  = "blocked"
	ItemStale    = "stale"
	ItemApplying = "applying"
	ItemApplied  = "applied"
	ItemFailed   = "failed"
	ItemUndone   = "undone"
)

// Journal steps and states.
const (
	stepMkdir = "mkdir"
	stepMove  = "move"

	statePending = 0
	stateDone    = 1
	stateUndone  = 2
	stateStuck   = 3
)

// maxStemBytes is the longest new file name without its extension. File
// systems allow 255 bytes in a name; the rest is room for the extension and a
// sidecar's ".en.forced.srt".
const maxStemBytes = 200

// sidecarExts are the files that move along with a video of the same name.
var sidecarExts = map[string]bool{".srt": true, ".sub": true, ".idx": true, ".ass": true, ".ssa": true, ".vtt": true, ".smi": true, ".sup": true, ".nfo": true}

// Sidecar is one subtitle or .nfo file that moves along.
type Sidecar struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// RenameItem is one video of a plan.
type RenameItem struct {
	ID       int64     `json:"id"`
	PlanID   int64     `json:"planId"`
	VideoID  int64     `json:"videoId"`
	Name     string    `json:"name"`
	OldPath  string    `json:"oldPath"`
	NewPath  string    `json:"newPath"`
	Size     int64     `json:"-"`
	ModNs    int64     `json:"-"`
	Sidecars []Sidecar `json:"sidecars"`
	Outside  bool      `json:"outside"` // the file isn't in the NSFW folder
	Status   string    `json:"status"`
	Reason   string    `json:"reason,omitempty"`
}

// RenamePlan is a plan without its videos.
type RenamePlan struct {
	ID        int64  `json:"id"`
	Root      string `json:"root"`
	Status    string `json:"status"`
	Unchanged int    `json:"unchanged"`
	CreatedAt string `json:"createdAt"`
	AppliedAt string `json:"appliedAt,omitempty"`
	UndoneAt  string `json:"undoneAt,omitempty"`
	Applied   int    `json:"applied"` // videos renamed (and since put back, for an undone plan)
}

func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimRight(strings.ToValidUTF8(s[:n], ""), ". ")
}

// NewFileStem is a video's new file name without the extension:
// "<Studio> - YYYY-MM-DD - <Title> [<quality>]". The title is shortened to fit,
// so the date and quality stay. clean makes a piece safe for a file name.
func NewFileStem(v Video, clean func(string) string) string {
	studio := cutBytes(clean(strings.TrimSpace(v.StudioName)), 80)
	head := studio + " - "
	if len(v.ReleaseDate) == 10 {
		head += clean(v.ReleaseDate) + " - "
	}
	tail := ""
	if q := clean(strings.TrimSpace(v.Quality)); q != "" {
		tail = " [" + q + "]"
	}
	title := cutBytes(clean(strings.TrimSpace(v.Title)), maxStemBytes-len(head)-len(tail))
	return head + title + tail
}

// isUnder reports whether p is inside root (both already resolved).
func isUnder(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolved follows links in a path's folders; the name itself stays.
func resolved(p string) string {
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, filepath.Base(p))
}

func resolvedDir(p string) string {
	dir, err := filepath.EvalSymlinks(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return dir
}

// FindSidecars lists the subtitle and .nfo files that belong to a video: same
// folder, its name followed by a dot. One that fits another video's longer name
// better ("Night.part2.srt" next to "Night.mkv" and "Night.part2.mkv") is that
// video's. Links are left alone.
func FindSidecars(video string) ([]string, error) {
	dir := filepath.Dir(video)
	stem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var stems []string
	for _, e := range entries {
		if !e.IsDir() && isVideoName(e.Name()) {
			stems = append(stems, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		}
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !sidecarExts[strings.ToLower(filepath.Ext(name))] {
			continue
		}
		owner := ""
		for _, s := range stems {
			if strings.HasPrefix(name, s+".") && len(s) > len(owner) {
				owner = s
			}
		}
		if owner == stem {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out, nil
}

// PlanRename works out the new name of each downloaded video. Videos already
// named right are only counted. Anything that can't be renamed safely is in the
// list as blocked, with the reason. clean makes a piece safe for a file name.
func (r *Repo) PlanRename(ctx context.Context, root string, clean func(string) string, working func(string), step func()) ([]RenameItem, int, error) {
	videos, err := r.listNsfw(`WHERE status = ? AND file_path <> '' ORDER BY id`, StatusDownloaded)
	if err != nil {
		return nil, 0, err
	}
	realRoot := resolvedDir(root)
	taken := map[string]bool{}
	claim := func(p string) bool {
		key := strings.ToLower(filepath.Clean(p))
		if taken[key] {
			return false
		}
		taken[key] = true
		return true
	}
	items := []RenameItem{}
	unchanged := 0
	for _, v := range videos {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if working != nil {
			working(v.Name())
		}
		it, same := r.planOne(v, root, realRoot, clean, claim)
		if same {
			unchanged++
		} else {
			items = append(items, it)
		}
		if step != nil {
			step()
		}
	}
	return items, unchanged, nil
}

func (r *Repo) planOne(v Video, root, realRoot string, clean func(string) string, claim func(string) bool) (RenameItem, bool) {
	it := RenameItem{VideoID: v.ID, Name: v.Name(), OldPath: v.FilePath, Sidecars: []Sidecar{}, Status: ItemBlocked}
	it.Outside = !isUnder(resolved(v.FilePath), realRoot)
	st, err := os.Lstat(v.FilePath)
	if err != nil {
		it.Reason = "The file isn't there any more."
		return it, false
	}
	if !st.Mode().IsRegular() {
		it.Reason = "This is a link, not a file, so it's left alone."
		return it, false
	}
	it.Size, it.ModNs = st.Size(), st.ModTime().UnixNano()
	if strings.TrimSpace(v.StudioName) == "" || strings.TrimSpace(v.Title) == "" {
		it.Reason = "Its studio or title isn't known yet."
		return it, false
	}
	if busy, err := r.HasActiveDownload(v.ID); err != nil || busy {
		it.Reason = "This video is downloading right now."
		return it, false
	}
	folder := clean(strings.TrimSpace(v.StudioName))
	it.NewPath = filepath.Join(root, folder, NewFileStem(v, clean)+strings.ToLower(filepath.Ext(v.FilePath)))
	if !isUnder(filepath.Clean(it.NewPath), filepath.Clean(root)) {
		it.Reason = "Its new name would be outside the NSFW folder."
		return it, false
	}
	if it.NewPath == v.FilePath {
		return it, true
	}
	if it.Outside {
		same, supported, err := organizer.SameFilesystem(v.FilePath, root)
		if err != nil {
			it.Reason = "Couldn't tell which drive this file is on."
			return it, false
		}
		if supported && !same {
			it.Reason = "This file is on another drive. It would have to be copied, so it's left alone."
			return it, false
		}
	}
	sides, err := FindSidecars(v.FilePath)
	if err != nil {
		it.Reason = "Its folder couldn't be read."
		return it, false
	}
	oldStem := strings.TrimSuffix(filepath.Base(v.FilePath), filepath.Ext(v.FilePath))
	newStem := strings.TrimSuffix(filepath.Base(it.NewPath), filepath.Ext(it.NewPath))
	for _, side := range sides {
		suffix := strings.TrimPrefix(filepath.Base(side), oldStem)
		it.Sidecars = append(it.Sidecars, Sidecar{Old: side, New: filepath.Join(filepath.Dir(it.NewPath), newStem+suffix)})
	}
	if reason := blockedTargets(it); reason != "" {
		it.Reason = reason
		return it, false
	}
	if !claim(it.NewPath) {
		it.Reason = "Another video would get this same name."
		return it, false
	}
	for _, side := range it.Sidecars {
		if !claim(side.New) {
			it.Reason = "Another video's subtitle would get this same name."
			return it, false
		}
	}
	it.Status = ItemEligible
	return it, false
}

// blockedTargets says why a video's new names can't be used: something else is
// there. The same file under another letter case doesn't count.
func blockedTargets(it RenameItem) string {
	if targetTaken(it.OldPath, it.NewPath) {
		return "A file with the new name is already there."
	}
	for _, side := range it.Sidecars {
		if targetTaken(side.Old, side.New) {
			return "A file with the new name of its subtitle or .nfo is already there."
		}
	}
	return ""
}

func targetTaken(from, to string) bool {
	dst, err := os.Lstat(to)
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	src, err := os.Lstat(from)
	return err != nil || !os.SameFile(src, dst)
}

// errTaken is returned when a rename would replace another file.
var errTaken = errors.New("a file is already there")

// moveNoOverwrite renames from to to without ever replacing another file. A
// change of letter case only (the same file on a case-insensitive disk) goes
// through a temporary name. It never copies: across drives it fails.
func moveNoOverwrite(from, to string) error {
	if from == to {
		return nil
	}
	if dst, err := os.Lstat(to); err == nil {
		src, err := os.Lstat(from)
		if err != nil {
			return err
		}
		if !os.SameFile(src, dst) {
			return errTaken
		}
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		mid := filepath.Join(filepath.Dir(to), ".usherr-case-"+hex.EncodeToString(b))
		if err := os.Rename(from, mid); err != nil {
			return err
		}
		if err := os.Rename(mid, to); err != nil {
			_ = os.Rename(mid, from)
			return err
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(from, to)
}

// ---- store ----

// NewRenamePlan stores a plan. Earlier plans that were only looked at are
// removed: a new check replaces them.
func (r *Repo) NewRenamePlan(root string, unchanged int, items []RenameItem) (int64, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM nsfw_rename_plans WHERE status = ?`, RenamePreview); err != nil {
		return 0, fmt.Errorf("clear old checks: %w", err)
	}
	res, err := tx.Exec(`INSERT INTO nsfw_rename_plans (root, unchanged) VALUES (?, ?)`, root, unchanged)
	if err != nil {
		return 0, fmt.Errorf("new plan: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, it := range items {
		sides, err := json.Marshal(it.Sidecars)
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`INSERT INTO nsfw_rename_items (plan_id, video_id, name, old_path, new_path, size, mtime_ns, sidecars, outside, status, reason) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, it.VideoID, it.Name, it.OldPath, it.NewPath, it.Size, it.ModNs, string(sides), it.Outside, it.Status, it.Reason); err != nil {
			return 0, fmt.Errorf("store a video: %w", err)
		}
	}
	return id, tx.Commit()
}

const renamePlanCols = `id, root, status, unchanged, created_at, applied_at, undone_at,
	(SELECT COUNT(*) FROM nsfw_rename_items WHERE plan_id = p.id AND status IN ('applied', 'applying', 'undone'))`

func scanRenamePlan(sc interface{ Scan(...any) error }) (RenamePlan, error) {
	var p RenamePlan
	err := sc.Scan(&p.ID, &p.Root, &p.Status, &p.Unchanged, &p.CreatedAt, &p.AppliedAt, &p.UndoneAt, &p.Applied)
	return p, err
}

// GetRenamePlan returns one plan.
func (r *Repo) GetRenamePlan(id int64) (RenamePlan, error) {
	p, err := scanRenamePlan(r.db.QueryRow(`SELECT `+renamePlanCols+` FROM nsfw_rename_plans p WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// RenamePlans lists the plans, newest first.
func (r *Repo) RenamePlans() ([]RenamePlan, error) {
	rows, err := r.db.Query(`SELECT ` + renamePlanCols + ` FROM nsfw_rename_plans p ORDER BY id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RenamePlan{}
	for rows.Next() {
		p, err := scanRenamePlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RenameItems lists a plan's videos; ids, when given, limits it.
func (r *Repo) RenameItems(planID int64, ids ...int64) ([]RenameItem, error) {
	q := `SELECT id, plan_id, COALESCE(video_id, 0), name, old_path, new_path, size, mtime_ns, sidecars, outside, status, reason FROM nsfw_rename_items WHERE plan_id = ?`
	args := []any{planID}
	if len(ids) > 0 {
		q += ` AND id IN (?` + strings.Repeat(`, ?`, len(ids)-1) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := r.db.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RenameItem{}
	for rows.Next() {
		var it RenameItem
		var sides string
		if err := rows.Scan(&it.ID, &it.PlanID, &it.VideoID, &it.Name, &it.OldPath, &it.NewPath, &it.Size, &it.ModNs, &sides, &it.Outside, &it.Status, &it.Reason); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(sides), &it.Sidecars)
		if it.Sidecars == nil {
			it.Sidecars = []Sidecar{}
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *Repo) setItemStatus(id int64, status, reason string) error {
	_, err := r.db.Exec(`UPDATE nsfw_rename_items SET status = ?, reason = ? WHERE id = ?`, status, reason, id)
	return err
}

func (r *Repo) journal(planID, itemID int64, step string, primary bool, from, to string, size, modNs int64) (int64, error) {
	res, err := r.db.Exec(`INSERT INTO nsfw_rename_journal (plan_id, item_id, step, primary_file, from_path, to_path, size, mtime_ns) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		planID, itemID, step, primary, from, to, size, modNs)
	if err != nil {
		return 0, fmt.Errorf("journal: %w", err)
	}
	return res.LastInsertId()
}

func (r *Repo) settleStep(id int64, state int, note string) error {
	_, err := r.db.Exec(`UPDATE nsfw_rename_journal SET state = ?, note = ? WHERE id = ?`, state, note, id)
	return err
}

// repoint changes a video's file from one path to another, only if it is still
// the path expected. It reports whether it did.
func (r *Repo) repoint(videoID int64, from, to string) (bool, error) {
	res, err := r.db.Exec(`UPDATE nsfw_videos SET file_path = ?, updated_at = ? WHERE id = ? AND file_path = ?`, to, now(), videoID, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ---- apply ----

// MarkRenameApplied notes that a plan has begun to be applied. It is done
// before the first file moves, so a crash halfway still offers undo.
func (r *Repo) MarkRenameApplied(planID int64) error {
	_, err := r.db.Exec(`UPDATE nsfw_rename_plans SET status = ?, applied_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, RenameApplied, planID)
	return err
}

type movedFile struct {
	step     int64
	from, to string
}

// ApplyRename renames one video and its sidecars, all of them or none. It
// reports what became of it; a video not renamed has its reason on its row.
func (r *Repo) ApplyRename(ctx context.Context, plan RenamePlan, it RenameItem) (string, error) {
	leave := func(status, reason string) (string, error) {
		return status, r.setItemStatus(it.ID, status, reason)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	v, err := r.GetVideo(it.VideoID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return leave(ItemStale, "This video isn't in your library any more.")
		}
		return "", err
	}
	if v.FilePath != it.OldPath || v.Status != StatusDownloaded {
		return leave(ItemStale, "This video's file changed since the check. Check names again.")
	}
	if busy, err := r.HasActiveDownload(v.ID); err != nil {
		return "", err
	} else if busy {
		return leave(ItemBlocked, "This video is downloading right now.")
	}
	st, err := os.Lstat(it.OldPath)
	if err != nil || !st.Mode().IsRegular() || st.Size() != it.Size || st.ModTime().UnixNano() != it.ModNs {
		return leave(ItemStale, "This file changed since the check. Check names again.")
	}
	if reason := blockedTargets(it); reason != "" {
		return leave(ItemBlocked, reason)
	}
	if err := r.setItemStatus(it.ID, ItemApplying, ""); err != nil {
		return "", err
	}

	// The folders that aren't there yet are made, and noted for undo.
	var made []string
	stop := filepath.Clean(plan.Root)
	for dir := filepath.Dir(it.NewPath); dir != stop && len(dir) > len(stop); dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil {
			break
		}
		made = append(made, dir)
	}
	var moved []movedFile
	var madeSteps []int64
	rollback := func() bool {
		clean := true
		for i := len(moved) - 1; i >= 0; i-- {
			if err := os.Rename(moved[i].to, moved[i].from); err == nil {
				_ = r.settleStep(moved[i].step, stateUndone, "")
			} else {
				clean = false
				_ = r.settleStep(moved[i].step, stateStuck, "couldn't be put back after a failed rename")
			}
		}
		for i, dir := range made {
			if i >= len(madeSteps) {
				continue
			}
			if removeEmptyDir(dir) {
				_ = r.settleStep(madeSteps[len(madeSteps)-1-i], stateUndone, "")
			}
		}
		return clean
	}
	for i := len(made) - 1; i >= 0; i-- {
		id, err := r.journal(plan.ID, it.ID, stepMkdir, false, "", made[i], 0, 0)
		if err != nil {
			return "", err
		}
		madeSteps = append(madeSteps, id)
	}
	fail := func(err error) (string, error) {
		if !rollback() {
			// Some of it stayed at the new name: left as it is, so undo can reach it.
			return leave(ItemApplying, "The rename failed halfway and couldn't be put back. Try Undo.")
		}
		if organizer.IsCrossDevice(err) {
			return leave(ItemBlocked, "This file is on another drive. It would have to be copied, so it's left alone.")
		}
		if errors.Is(err, errTaken) {
			return leave(ItemBlocked, "A file with the new name is already there.")
		}
		return leave(ItemFailed, "The file couldn't be renamed: "+err.Error())
	}
	if err := fileperms.MkdirAll(filepath.Dir(it.NewPath)); err != nil {
		return fail(err)
	}
	for _, id := range madeSteps {
		if err := r.settleStep(id, stateDone, ""); err != nil {
			return fail(err)
		}
	}
	files := append([]Sidecar{{Old: it.OldPath, New: it.NewPath}}, it.Sidecars...)
	for i, f := range files {
		fst, err := os.Lstat(f.Old)
		if err != nil {
			if i > 0 && errors.Is(err, os.ErrNotExist) {
				continue // a subtitle removed since the check
			}
			return fail(err)
		}
		id, err := r.journal(plan.ID, it.ID, stepMove, i == 0, f.Old, f.New, fst.Size(), fst.ModTime().UnixNano())
		if err != nil {
			return fail(err)
		}
		if err := moveNoOverwrite(f.Old, f.New); err != nil {
			_ = r.settleStep(id, stateUndone, "not moved")
			return fail(err)
		}
		moved = append(moved, movedFile{id, f.Old, f.New})
		if err := r.settleStep(id, stateDone, ""); err != nil {
			return fail(err)
		}
	}
	ok, err := r.repoint(v.ID, it.OldPath, it.NewPath)
	if err == nil && !ok {
		err = errors.New("the video's file changed meanwhile")
	}
	if err != nil {
		return fail(err)
	}
	return ItemApplied, r.setItemStatus(it.ID, ItemApplied, "")
}

// SettleRenamePlan puts a plan back to a preview when nothing of it stayed
// renamed, so its videos can be chosen again and there is nothing to undo.
func (r *Repo) SettleRenamePlan(planID int64) error {
	_, err := r.db.Exec(`UPDATE nsfw_rename_plans SET status = ?, applied_at = '' WHERE id = ? AND status = ?
		AND NOT EXISTS (SELECT 1 FROM nsfw_rename_items WHERE plan_id = ? AND status IN (?, ?, ?))`,
		RenamePreview, planID, RenameApplied, planID, ItemApplied, ItemApplying, ItemUndone)
	return err
}

// ---- undo ----

// UndoResult says what became of one video in an undo.
type UndoResult struct {
	Name string `json:"name"`
	Done bool   `json:"done"`
	Note string `json:"note,omitempty"`
}

type journalStep struct {
	id       int64
	itemID   int64
	step     string
	primary  bool
	from, to string
	size     int64
	modNs    int64
	state    int
}

func (r *Repo) journalSteps(planID int64) ([]journalStep, error) {
	rows, err := r.db.Query(`SELECT id, item_id, step, primary_file, from_path, to_path, size, mtime_ns, state FROM nsfw_rename_journal WHERE plan_id = ? AND state <> ? ORDER BY primary_file DESC, id`, planID, stateUndone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []journalStep
	for rows.Next() {
		var st journalStep
		if err := rows.Scan(&st.id, &st.itemID, &st.step, &st.primary, &st.from, &st.to, &st.size, &st.modNs, &st.state); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// undoMove takes back one renamed file. note says why it couldn't be.
func undoMove(st journalStep) (note string, moved bool) {
	got, toErr := os.Lstat(st.to)
	_, fromErr := os.Lstat(st.from)
	switch {
	case toErr != nil && fromErr == nil:
		return "", false // it never moved, or is back already
	case toErr != nil:
		return "The file isn't at its new name any more.", false
	case got.Size() != st.size || got.ModTime().UnixNano() != st.modNs:
		return "The file changed after it was renamed, so it stays.", false
	case fromErr == nil && !sameEntry(st.from, st.to):
		return "Something else now has the old name, so it stays.", false
	}
	if err := fileperms.MkdirAll(filepath.Dir(st.from)); err != nil {
		return "Its old folder couldn't be made again.", false
	}
	if err := moveNoOverwrite(st.to, st.from); err != nil {
		return "The file couldn't be renamed back: " + err.Error(), false
	}
	return "", true
}

// removeEmptyDir removes a folder Usherr made, if it is still an empty folder,
// and reports whether it is gone. Whatever else is at that path by now stays.
func removeEmptyDir(dir string) bool {
	st, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil || !st.IsDir() {
		return false
	}
	return os.Remove(dir) == nil
}

func sameEntry(a, b string) bool {
	sa, err := os.Lstat(a)
	if err != nil {
		return false
	}
	sb, err := os.Lstat(b)
	return err == nil && os.SameFile(sa, sb)
}

// UndoRename puts a plan's files back and restores each video's file. A file
// that changed, or whose old name was taken since, stays where it is and is
// reported. Folders the apply made go only when they are empty. The second
// return lists the old and new paths moved back, for the media servers.
func (r *Repo) UndoRename(ctx context.Context, planID int64, working func(string), step func()) ([]UndoResult, []string, error) {
	items, err := r.RenameItems(planID)
	if err != nil {
		return nil, nil, err
	}
	steps, err := r.journalSteps(planID)
	if err != nil {
		return nil, nil, err
	}
	byItem := map[int64][]journalStep{}
	for _, st := range steps {
		if st.step == stepMove {
			byItem[st.itemID] = append(byItem[st.itemID], st)
		}
	}
	results := []UndoResult{}
	var paths []string
	for _, it := range items {
		if it.Status != ItemApplied && it.Status != ItemApplying {
			continue
		}
		if err := ctx.Err(); err != nil {
			break
		}
		if working != nil {
			working(it.Name)
		}
		res := r.undoItem(it, byItem[it.ID], &paths)
		results = append(results, res)
		if step != nil {
			step()
		}
	}
	if ctx.Err() == nil {
		// The newest folders first, so a folder inside another goes before it.
		for i := len(steps) - 1; i >= 0; i-- {
			if st := steps[i]; st.step == stepMkdir {
				if removeEmptyDir(st.to) {
					_ = r.settleStep(st.id, stateUndone, "")
				}
			}
		}
	}
	var left int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM nsfw_rename_items WHERE plan_id = ? AND status IN (?, ?)`, planID, ItemApplied, ItemApplying).Scan(&left); err != nil {
		return results, paths, err
	}
	if left == 0 {
		if _, err := r.db.Exec(`UPDATE nsfw_rename_plans SET status = ?, undone_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, RenameUndone, planID); err != nil {
			return results, paths, err
		}
	}
	return results, paths, nil
}

func (r *Repo) undoItem(it RenameItem, moves []journalStep, paths *[]string) UndoResult {
	res := UndoResult{Name: it.Name}
	stuck := func(note string) UndoResult {
		for _, st := range moves {
			_ = r.settleStep(st.id, stateStuck, note)
		}
		_ = r.setItemStatus(it.ID, ItemApplied, note)
		res.Note = note
		return res
	}
	// A video re-downloaded or changed since: its file is no longer this one.
	hasVideo := true
	v, err := r.GetVideo(it.VideoID)
	switch {
	case errors.Is(err, ErrNotFound):
		hasVideo = false
	case err != nil:
		return stuck("Its library entry couldn't be read.")
	case v.FilePath != it.NewPath && v.FilePath != it.OldPath:
		return stuck("This video has another file now, so it stays.")
	}
	note := ""
	for _, st := range moves { // the video itself comes first
		n, moved := undoMove(st)
		if n != "" {
			_ = r.settleStep(st.id, stateStuck, n)
			if st.primary {
				rest := moves[1:]
				for _, side := range rest {
					_ = r.settleStep(side.id, stateStuck, n)
				}
				_ = r.setItemStatus(it.ID, ItemApplied, n)
				res.Note = n
				return res
			}
			note = n
			continue
		}
		_ = r.settleStep(st.id, stateUndone, "")
		if moved {
			*paths = append(*paths, st.from, st.to)
		}
	}
	if hasVideo {
		if _, err := r.repoint(v.ID, it.NewPath, it.OldPath); err != nil {
			_ = r.setItemStatus(it.ID, ItemApplied, "Its library entry couldn't be changed back.")
			res.Note = "Its library entry couldn't be changed back."
			return res
		}
	}
	if err := r.setItemStatus(it.ID, ItemUndone, note); err != nil {
		res.Note = "Its library entry couldn't be changed back."
		return res
	}
	res.Done, res.Note = true, note
	return res
}
