package musictidy

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Album statuses in a plan.
const (
	StatusEligible  = "eligible"
	StatusUnchanged = "unchanged"
	StatusBlocked   = "blocked"
	StatusStale     = "stale"
	StatusApplying  = "applying" // being tidied; stays so after a crash halfway
	StatusApplied   = "applied"
	StatusFailed    = "failed"
	StatusUndone    = "undone"
)

// Plan statuses.
const (
	PlanPreview = "preview"
	PlanApplied = "applied"
	PlanUndone  = "undone"
)

// Store keeps plans and the undo journal (migration 9110).
type Store struct {
	db *sql.DB
}

// NewStore returns a store on db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// PlanRow is a plan without its albums.
type PlanRow struct {
	ID        int64  `json:"id"`
	Root      string `json:"root"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	AppliedAt string `json:"appliedAt,omitempty"`
	UndoneAt  string `json:"undoneAt,omitempty"`
	Recycled  bool   `json:"recycled"`
	Applied   int    `json:"applied"` // albums applied (and since undone, for an undone plan)
}

// AlbumRow is one album folder of a plan with its files.
type AlbumRow struct {
	ID        int64           `json:"id"`
	PlanID    int64           `json:"planId"`
	AlbumID   int64           `json:"albumId,omitempty"`
	Artist    string          `json:"artist"`
	Title     string          `json:"title"`
	OldFolder string          `json:"oldFolder"`
	NewFolder string          `json:"newFolder"`
	Status    string          `json:"status"`
	Reason    string          `json:"reason,omitempty"`
	Detail    json.RawMessage `json:"detail"`
	Items     []ItemRow       `json:"items"`
}

// ItemRow is one file of an album row.
type ItemRow struct {
	ID      int64    `json:"id"`
	TrackID int64    `json:"trackId,omitempty"`
	OldPath string   `json:"oldPath"`
	NewPath string   `json:"newPath"`
	Plan    FilePlan `json:"plan"`
}

// NewPlan starts a plan for a check of root. Earlier plans that were only
// looked at (never applied) are removed: a new check replaces them.
func (s *Store) NewPlan(root string) (int64, error) {
	if _, err := s.db.Exec(`DELETE FROM music_tidy_plans WHERE status = ?`, PlanPreview); err != nil {
		return 0, fmt.Errorf("tidy: clear old checks: %w", err)
	}
	res, err := s.db.Exec(`INSERT INTO music_tidy_plans (root) VALUES (?)`, root)
	if err != nil {
		return 0, fmt.Errorf("tidy: new plan: %w", err)
	}
	return res.LastInsertId()
}

// AddAlbum stores one album folder of a plan with its files.
func (s *Store) AddAlbum(planID, albumID int64, artist, title string, p AlbumPlan, status, reason string, detail any) (int64, error) {
	d, err := json.Marshal(detail)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var aid any
	if albumID > 0 {
		aid = albumID
	}
	res, err := tx.Exec(`INSERT INTO music_tidy_albums (plan_id, album_id, artist, title, old_folder, new_folder, status, reason, detail) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		planID, aid, artist, title, p.OldFolder, p.NewFolder, status, reason, string(d))
	if err != nil {
		return 0, fmt.Errorf("tidy: store album: %w", err)
	}
	row, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, f := range p.Files {
		js, err := json.Marshal(f)
		if err != nil {
			return 0, err
		}
		var tid any
		if f.TrackID > 0 {
			tid = f.TrackID
		}
		if _, err := tx.Exec(`INSERT INTO music_tidy_items (album_row, track_id, old_path, new_path, plan, size, mtime_ns) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			row, tid, f.OldPath, f.NewPath, string(js), f.Size, f.ModNs); err != nil {
			return 0, fmt.Errorf("tidy: store file: %w", err)
		}
	}
	return row, tx.Commit()
}

// Plan returns one plan.
func (s *Store) Plan(id int64) (PlanRow, error) {
	var p PlanRow
	err := s.db.QueryRow(`SELECT id, root, status, created_at, applied_at, undone_at, recycled,
		(SELECT COUNT(*) FROM music_tidy_albums WHERE plan_id = p.id AND status IN ('applied', 'applying', 'undone'))
		FROM music_tidy_plans p WHERE id = ?`, id).Scan(&p.ID, &p.Root, &p.Status, &p.CreatedAt, &p.AppliedAt, &p.UndoneAt, &p.Recycled, &p.Applied)
	if err != nil {
		return PlanRow{}, fmt.Errorf("tidy: plan %d: %w", id, err)
	}
	return p, nil
}

// Plans lists the plans, newest first.
func (s *Store) Plans() ([]PlanRow, error) {
	rows, err := s.db.Query(`SELECT id, root, status, created_at, applied_at, undone_at, recycled,
		(SELECT COUNT(*) FROM music_tidy_albums WHERE plan_id = p.id AND status IN ('applied', 'applying', 'undone'))
		FROM music_tidy_plans p ORDER BY id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanRow
	for rows.Next() {
		var p PlanRow
		if err := rows.Scan(&p.ID, &p.Root, &p.Status, &p.CreatedAt, &p.AppliedAt, &p.UndoneAt, &p.Recycled, &p.Applied); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Albums lists a plan's album rows with their files. ids, when given, limits
// it to those rows.
func (s *Store) Albums(planID int64, ids ...int64) ([]AlbumRow, error) {
	q := `SELECT id, plan_id, COALESCE(album_id, 0), artist, title, old_folder, new_folder, status, reason, detail FROM music_tidy_albums WHERE plan_id = ?`
	args := []any{planID}
	if len(ids) > 0 {
		q += ` AND id IN (?` + strings.Repeat(`, ?`, len(ids)-1) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := s.db.Query(q+` ORDER BY artist COLLATE NOCASE, title COLLATE NOCASE, id`, args...)
	if err != nil {
		return nil, err
	}
	var out []AlbumRow
	for rows.Next() {
		var a AlbumRow
		var detail string
		if err := rows.Scan(&a.ID, &a.PlanID, &a.AlbumID, &a.Artist, &a.Title, &a.OldFolder, &a.NewFolder, &a.Status, &a.Reason, &detail); err != nil {
			rows.Close()
			return nil, err
		}
		a.Detail = json.RawMessage(detail)
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		items, err := s.items(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Items = items
	}
	return out, nil
}

func (s *Store) items(albumRow int64) ([]ItemRow, error) {
	rows, err := s.db.Query(`SELECT id, COALESCE(track_id, 0), old_path, new_path, plan FROM music_tidy_items WHERE album_row = ? ORDER BY new_path`, albumRow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemRow
	for rows.Next() {
		var it ItemRow
		var js string
		if err := rows.Scan(&it.ID, &it.TrackID, &it.OldPath, &it.NewPath, &js); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(js), &it.Plan); err != nil {
			return nil, fmt.Errorf("tidy: item %d: %w", it.ID, err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// SetAlbumStatus records what became of an album row.
func (s *Store) SetAlbumStatus(row int64, status, reason string) error {
	_, err := s.db.Exec(`UPDATE music_tidy_albums SET status = ?, reason = ? WHERE id = ?`, status, reason, row)
	return err
}

// UpdateAlbum changes an album row's status, reason and detail.
func (s *Store) UpdateAlbum(row int64, status, reason string, detail any) error {
	d, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE music_tidy_albums SET status = ?, reason = ?, detail = ? WHERE id = ?`, status, reason, string(d), row)
	return err
}

// markApplied notes that a plan had albums applied.
func (s *Store) markApplied(planID int64) error {
	_, err := s.db.Exec(`UPDATE music_tidy_plans SET status = ?, applied_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, PlanApplied, planID)
	return err
}

// settlePlan puts a plan back to a check when nothing of it stayed applied
// and nothing of it is left in the recycle folder.
func (s *Store) settlePlan(planID int64) error {
	_, err := s.db.Exec(`UPDATE music_tidy_plans SET status = ?, applied_at = '' WHERE id = ? AND status = ?
		AND NOT EXISTS (SELECT 1 FROM music_tidy_albums WHERE plan_id = ? AND status IN ('applied', 'applying'))
		AND NOT EXISTS (SELECT 1 FROM music_tidy_journal WHERE plan_id = ? AND undone = 2)`,
		PlanPreview, planID, PlanApplied, planID, planID)
	return err
}

func (s *Store) markUndone(planID int64) error {
	_, err := s.db.Exec(`UPDATE music_tidy_plans SET status = ?, undone_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, PlanUndone, planID)
	return err
}

// MarkRecycled notes that a plan's kept originals are gone.
func (s *Store) MarkRecycled(planID int64) error {
	_, err := s.db.Exec(`UPDATE music_tidy_plans SET recycled = 1 WHERE id = ?`, planID)
	return err
}

// step is one journal entry.
type step struct {
	ID       int64
	AlbumRow int64
	Step     string
	From, To string
	State    State
	Undone   int
}

// Journal steps.
const (
	stepFolder  = "folder"
	stepMkdir   = "mkdir"
	stepRecycle = "recycle"
	stepWrite   = "write"
	stepMove    = "move"
)

func (s *Store) record(planID, albumRow int64, kind, from, to string, st State) error {
	_, err := s.db.Exec(`INSERT INTO music_tidy_journal (plan_id, album_row, step, from_path, to_path, size, mtime_ns) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		planID, albumRow, kind, from, to, st.Size, st.ModNs)
	if err != nil {
		return fmt.Errorf("tidy: journal: %w", err)
	}
	return nil
}

// steps returns a plan's journal, newest first, that isn't taken back yet.
func (s *Store) steps(planID int64) ([]step, error) {
	rows, err := s.db.Query(`SELECT id, album_row, step, from_path, to_path, size, mtime_ns, undone FROM music_tidy_journal WHERE plan_id = ? AND undone = 0 ORDER BY id DESC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []step
	for rows.Next() {
		var st step
		if err := rows.Scan(&st.ID, &st.AlbumRow, &st.Step, &st.From, &st.To, &st.State.Size, &st.State.ModNs, &st.Undone); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) settle(id int64, undone int, note string) error {
	_, err := s.db.Exec(`UPDATE music_tidy_journal SET undone = ?, note = ? WHERE id = ?`, undone, note, id)
	return err
}

// recycledPlans lists the applied plans whose originals are still kept.
func (s *Store) recycledPlans() ([]PlanRow, error) {
	rows, err := s.db.Query(`SELECT id, root, applied_at FROM music_tidy_plans WHERE recycled = 0 AND applied_at != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanRow
	for rows.Next() {
		var p PlanRow
		if err := rows.Scan(&p.ID, &p.Root, &p.AppliedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
