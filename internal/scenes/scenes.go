// Package scenes keeps the home fork's Scenes library: adult scenes,
// the performers and studios they come from, and which of them are
// followed. Details come from a stash-box server such as StashDB (see
// stashdb.go). The whole module is for administrators only; nothing in this
// package decides that, the API layer does.
package scenes

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status of a scene in the library.
const (
	StatusMissing     = "missing"
	StatusDownloading = "downloading"
	StatusDownloaded  = "downloaded"
)

// Where a scene came from.
const (
	AddedManual    = "manual"
	AddedPerformer = "performer"
	AddedStudio    = "studio"
	AddedImport    = "import"
)

// ErrNotFound is returned when a row doesn't exist.
var ErrNotFound = errors.New("not found")

// Credit is one performer in a scene.
type Credit struct {
	StashID string `json:"id"`
	Name    string `json:"name"`
	As      string `json:"as,omitempty"` // the alias they appeared under
	// Gender as StashDB has it: MALE, FEMALE, TRANSGENDER_MALE,
	// TRANSGENDER_FEMALE, INTERSEX, NON_BINARY; "" when not known.
	Gender string `json:"gender,omitempty"`
}

// Scene is one scene in the library.
type Scene struct {
	ID            int64    `json:"id"`
	StashID       string   `json:"stashId"`
	Title         string   `json:"title"`
	StudioStashID string   `json:"studioStashId"`
	StudioName    string   `json:"studioName"`
	StudioAliases []string `json:"studioAliases"`
	ReleaseDate   string   `json:"releaseDate"` // YYYY-MM-DD
	Duration      int      `json:"duration"`    // seconds
	Code          string   `json:"code"`
	Details       string   `json:"details"`
	ImageURL      string   `json:"imageUrl"`
	Performers    []Credit `json:"performers"`
	Tags          []string `json:"tags"`
	DetailsAt     string   `json:"detailsAt"` // when details were last read from StashDB
	Wanted        bool     `json:"wanted"`
	Status        string   `json:"status"`
	FilePath      string   `json:"filePath"`
	Quality       string   `json:"quality"`
	AddedBy       string   `json:"addedBy"`
	AddedAt       string   `json:"addedAt"`
}

// Name is how a scene is shown in activity and notifications.
func (s Scene) Name() string {
	parts := []string{}
	if s.StudioName != "" {
		parts = append(parts, s.StudioName)
	}
	if s.ReleaseDate != "" {
		parts = append(parts, s.ReleaseDate)
	}
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = "Untitled scene"
	}
	parts = append(parts, title)
	return strings.Join(parts, " - ")
}

// Released reports whether the scene is out (an unknown date counts as out).
func (s Scene) Released(now time.Time) bool {
	if s.ReleaseDate == "" {
		return true
	}
	d, err := time.Parse("2006-01-02", s.ReleaseDate)
	if err != nil {
		return true
	}
	return !d.After(now)
}

// Performer is a performer known to the library.
type Performer struct {
	ID             int64    `json:"id"`
	StashID        string   `json:"stashId"`
	Name           string   `json:"name"`
	Disambiguation string   `json:"disambiguation"`
	Aliases        []string `json:"aliases"`
	ImageURL       string   `json:"imageUrl"`
	Followed       bool     `json:"followed"`
	CheckedAt      string   `json:"checkedAt"`
}

// Studio is a studio known to the library.
type Studio struct {
	ID         int64    `json:"id"`
	StashID    string   `json:"stashId"`
	Name       string   `json:"name"`
	Aliases    []string `json:"aliases"`
	ParentName string   `json:"parentName"`
	ImageURL   string   `json:"imageUrl"`
	Followed   bool     `json:"followed"`
	CheckedAt  string   `json:"checkedAt"`
}

// Repo stores the Scenes library.
type Repo struct{ db *sql.DB }

// NewRepo returns a repo on db.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

const sceneCols = `id, stash_id, title, studio_stash_id, studio_name, studio_aliases, release_date, duration, code, details, image_url, performers, wanted, status, file_path, quality, added_by, added_at, tags, details_at`

func scanScene(sc interface{ Scan(...any) error }) (Scene, error) {
	var s Scene
	var aliases, performers, tags string
	err := sc.Scan(&s.ID, &s.StashID, &s.Title, &s.StudioStashID, &s.StudioName, &aliases, &s.ReleaseDate, &s.Duration, &s.Code, &s.Details, &s.ImageURL, &performers, &s.Wanted, &s.Status, &s.FilePath, &s.Quality, &s.AddedBy, &s.AddedAt, &tags, &s.DetailsAt)
	if err != nil {
		return s, err
	}
	_ = json.Unmarshal([]byte(aliases), &s.StudioAliases)
	_ = json.Unmarshal([]byte(performers), &s.Performers)
	_ = json.Unmarshal([]byte(tags), &s.Tags)
	if s.Tags == nil {
		s.Tags = []string{}
	}
	if s.StudioAliases == nil {
		s.StudioAliases = []string{}
	}
	if s.Performers == nil {
		s.Performers = []Credit{}
	}
	return s, nil
}

// AddScene adds a scene, or refreshes the details of one already there
// (keeping its wanted flag, status and file). added reports a new row.
func (r *Repo) AddScene(s Scene) (out Scene, added bool, err error) {
	if s.StashID == "" {
		return s, false, errors.New("a scene needs its stash-box id")
	}
	if s.AddedBy == "" {
		s.AddedBy = AddedManual
	}
	_, existsErr := r.GetSceneByStashID(s.StashID)
	if s.Tags == nil {
		s.Tags = []string{}
	}
	// Details from an import (no StashDB read) never overwrite tags or
	// genders that StashDB already gave.
	_, err = r.db.Exec(`INSERT INTO scenes (stash_id, title, studio_stash_id, studio_name, studio_aliases, release_date, duration, code, details, image_url, performers, wanted, added_by, tags, details_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(stash_id) DO UPDATE SET title = excluded.title, studio_stash_id = excluded.studio_stash_id, studio_name = excluded.studio_name,
			studio_aliases = excluded.studio_aliases, release_date = excluded.release_date, duration = excluded.duration, code = excluded.code,
			details = excluded.details, image_url = excluded.image_url,
			performers = CASE WHEN excluded.details_at <> '' OR scenes.details_at = '' THEN excluded.performers ELSE scenes.performers END,
			tags = CASE WHEN excluded.details_at <> '' THEN excluded.tags ELSE scenes.tags END,
			details_at = CASE WHEN excluded.details_at <> '' THEN excluded.details_at ELSE scenes.details_at END,
			updated_at = ?`,
		s.StashID, s.Title, s.StudioStashID, s.StudioName, toJSON(s.StudioAliases), s.ReleaseDate, s.Duration, s.Code, s.Details, s.ImageURL, toJSON(s.Performers), s.Wanted, s.AddedBy, toJSON(s.Tags), s.DetailsAt, now())
	if err != nil {
		return s, false, fmt.Errorf("add scene: %w", err)
	}
	got, err := r.GetSceneByStashID(s.StashID)
	if err != nil {
		return s, false, err
	}
	return got, errors.Is(existsErr, ErrNotFound), nil
}

// GetScene returns the scene with id.
func (r *Repo) GetScene(id int64) (Scene, error) {
	s, err := scanScene(r.db.QueryRow(`SELECT `+sceneCols+` FROM scenes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

// GetSceneByStashID returns the scene with a stash-box id.
func (r *Repo) GetSceneByStashID(stashID string) (Scene, error) {
	s, err := scanScene(r.db.QueryRow(`SELECT `+sceneCols+` FROM scenes WHERE stash_id = ?`, stashID))
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

func (r *Repo) listScenes(where string, args ...any) ([]Scene, error) {
	rows, err := r.db.Query(`SELECT `+sceneCols+` FROM scenes `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Scene{}
	for rows.Next() {
		s, err := scanScene(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListScenes returns every scene, newest release first.
func (r *Repo) ListScenes() ([]Scene, error) {
	return r.listScenes(`ORDER BY release_date DESC, id DESC`)
}

// MissingDetails returns up to n scenes whose details were never read from
// StashDB (tags and performer genders unknown), for filling them in.
func (r *Repo) MissingDetails(n int) ([]Scene, error) {
	return r.listScenes(`WHERE details_at = '' ORDER BY id LIMIT ?`, n)
}

// Wanted returns the wanted scenes without a file, newest release first.
func (r *Repo) Wanted() ([]Scene, error) {
	return r.listScenes(`WHERE wanted = 1 AND status = ? ORDER BY release_date DESC, id DESC`, StatusMissing)
}

// ScenesWithPerformer returns the scenes a performer (stash id) is in.
func (r *Repo) ScenesWithPerformer(stashID string) ([]Scene, error) {
	return r.listScenes(`WHERE EXISTS (SELECT 1 FROM json_each(scenes.performers) WHERE json_extract(value, '$.id') = ?) ORDER BY release_date DESC`, stashID)
}

// ScenesFromStudio returns the scenes of a studio (stash id).
func (r *Repo) ScenesFromStudio(stashID string) ([]Scene, error) {
	return r.listScenes(`WHERE studio_stash_id = ? ORDER BY release_date DESC`, stashID)
}

// SetWanted marks a scene wanted or not.
func (r *Repo) SetWanted(id int64, wanted bool) error {
	_, err := r.db.Exec(`UPDATE scenes SET wanted = ?, updated_at = ? WHERE id = ?`, wanted, now(), id)
	return err
}

// SetState records a scene's status, and its file and quality when it has one.
func (r *Repo) SetState(id int64, status, path, quality string) error {
	if status == StatusDownloaded {
		_, err := r.db.Exec(`UPDATE scenes SET status = ?, file_path = ?, quality = ?, updated_at = ? WHERE id = ?`, status, path, quality, now(), id)
		return err
	}
	_, err := r.db.Exec(`UPDATE scenes SET status = ?, updated_at = ? WHERE id = ?`, status, now(), id)
	return err
}

// BulkResult says what a bulk change did.
type BulkResult struct {
	Changed int `json:"changed"`
	Skipped int `json:"skipped"` // removals refused: the scene has a file or a running download
}

// SetWantedMany marks many scenes wanted or not, in one transaction.
func (r *Repo) SetWantedMany(ids []int64, wanted bool) (BulkResult, error) {
	var res BulkResult
	tx, err := r.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	ts := now()
	for _, id := range ids {
		out, err := tx.Exec(`UPDATE scenes SET wanted = ?, updated_at = ? WHERE id = ?`, wanted, ts, id)
		if err != nil {
			return res, err
		}
		if n, _ := out.RowsAffected(); n > 0 {
			res.Changed++
		}
	}
	return res, tx.Commit()
}

// RemoveEmpty removes scenes that have no file from the library, in one
// transaction. A scene with a file, or with a download still running or
// waiting, is never removed (it counts as skipped). No file is touched.
func (r *Repo) RemoveEmpty(ids []int64) (BulkResult, error) {
	var res BulkResult
	tx, err := r.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		out, err := tx.Exec(`DELETE FROM scenes WHERE id = ? AND status <> 'downloaded' AND file_path = ''
			AND NOT EXISTS (SELECT 1 FROM scene_downloads d JOIN download_queue q ON q.id = d.queue_id
				WHERE d.scene_id = scenes.id AND q.status IN ('queued', 'downloading', 'importing', 'paused', 'conflict'))`, id)
		if err != nil {
			return res, err
		}
		if n, _ := out.RowsAffected(); n > 0 {
			res.Changed++
		} else {
			res.Skipped++
		}
	}
	return res, tx.Commit()
}

// DeleteScene removes a scene from the library (not its file).
func (r *Repo) DeleteScene(id int64) error {
	_, err := r.db.Exec(`DELETE FROM scenes WHERE id = ?`, id)
	return err
}

// Counts is the size of the library.
type Counts struct {
	Scenes     int `json:"scenes"`
	Wanted     int `json:"wanted"`
	Downloaded int `json:"downloaded"`
	Performers int `json:"performers"`
	Studios    int `json:"studios"`
}

// Count returns the library's size.
func (r *Repo) Count() (Counts, error) {
	var c Counts
	err := r.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM scenes),
		(SELECT COUNT(*) FROM scenes WHERE wanted = 1 AND status <> 'downloaded'),
		(SELECT COUNT(*) FROM scenes WHERE status = 'downloaded'),
		(SELECT COUNT(*) FROM scene_performers WHERE followed = 1),
		(SELECT COUNT(*) FROM scene_studios WHERE followed = 1)`).Scan(&c.Scenes, &c.Wanted, &c.Downloaded, &c.Performers, &c.Studios)
	return c, err
}

// ---- performers ----

const performerCols = `id, stash_id, name, disambiguation, aliases, image_url, followed, checked_at`

func scanPerformer(sc interface{ Scan(...any) error }) (Performer, error) {
	var p Performer
	var aliases string
	err := sc.Scan(&p.ID, &p.StashID, &p.Name, &p.Disambiguation, &aliases, &p.ImageURL, &p.Followed, &p.CheckedAt)
	_ = json.Unmarshal([]byte(aliases), &p.Aliases)
	if p.Aliases == nil {
		p.Aliases = []string{}
	}
	return p, err
}

// SavePerformer adds a performer or refreshes its details, keeping whether
// it is followed unless follow is set.
func (r *Repo) SavePerformer(p Performer, follow bool) (Performer, error) {
	_, err := r.db.Exec(`INSERT INTO scene_performers (stash_id, name, disambiguation, aliases, image_url, followed) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(stash_id) DO UPDATE SET name = excluded.name, disambiguation = excluded.disambiguation, aliases = excluded.aliases,
			image_url = excluded.image_url, followed = MAX(scene_performers.followed, excluded.followed)`,
		p.StashID, p.Name, p.Disambiguation, toJSON(p.Aliases), p.ImageURL, follow)
	if err != nil {
		return p, fmt.Errorf("save performer: %w", err)
	}
	return scanPerformer(r.db.QueryRow(`SELECT `+performerCols+` FROM scene_performers WHERE stash_id = ?`, p.StashID))
}

// GetPerformer returns the performer with id.
func (r *Repo) GetPerformer(id int64) (Performer, error) {
	p, err := scanPerformer(r.db.QueryRow(`SELECT `+performerCols+` FROM scene_performers WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// Performers returns the known performers, followed ones first.
func (r *Repo) Performers(followedOnly bool) ([]Performer, error) {
	where := ""
	if followedOnly {
		where = "WHERE followed = 1"
	}
	rows, err := r.db.Query(`SELECT ` + performerCols + ` FROM scene_performers ` + where + ` ORDER BY followed DESC, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Performer{}
	for rows.Next() {
		p, err := scanPerformer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetPerformerFollowed follows or unfollows a performer.
func (r *Repo) SetPerformerFollowed(id int64, followed bool) error {
	_, err := r.db.Exec(`UPDATE scene_performers SET followed = ? WHERE id = ?`, followed, id)
	return err
}

// PerformerChecked records when a followed performer's scenes were looked up.
func (r *Repo) PerformerChecked(id int64) error {
	_, err := r.db.Exec(`UPDATE scene_performers SET checked_at = ? WHERE id = ?`, now(), id)
	return err
}

// ---- studios ----

const studioCols = `id, stash_id, name, aliases, parent_name, image_url, followed, checked_at`

func scanStudio(sc interface{ Scan(...any) error }) (Studio, error) {
	var s Studio
	var aliases string
	err := sc.Scan(&s.ID, &s.StashID, &s.Name, &aliases, &s.ParentName, &s.ImageURL, &s.Followed, &s.CheckedAt)
	_ = json.Unmarshal([]byte(aliases), &s.Aliases)
	if s.Aliases == nil {
		s.Aliases = []string{}
	}
	return s, err
}

// SaveStudio adds a studio or refreshes its details, keeping whether it is
// followed unless follow is set.
func (r *Repo) SaveStudio(s Studio, follow bool) (Studio, error) {
	_, err := r.db.Exec(`INSERT INTO scene_studios (stash_id, name, aliases, parent_name, image_url, followed) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(stash_id) DO UPDATE SET name = excluded.name, aliases = excluded.aliases, parent_name = excluded.parent_name,
			image_url = excluded.image_url, followed = MAX(scene_studios.followed, excluded.followed)`,
		s.StashID, s.Name, toJSON(s.Aliases), s.ParentName, s.ImageURL, follow)
	if err != nil {
		return s, fmt.Errorf("save studio: %w", err)
	}
	return scanStudio(r.db.QueryRow(`SELECT `+studioCols+` FROM scene_studios WHERE stash_id = ?`, s.StashID))
}

// GetStudio returns the studio with id.
func (r *Repo) GetStudio(id int64) (Studio, error) {
	s, err := scanStudio(r.db.QueryRow(`SELECT `+studioCols+` FROM scene_studios WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

// Studios returns the known studios, followed ones first.
func (r *Repo) Studios(followedOnly bool) ([]Studio, error) {
	where := ""
	if followedOnly {
		where = "WHERE followed = 1"
	}
	rows, err := r.db.Query(`SELECT ` + studioCols + ` FROM scene_studios ` + where + ` ORDER BY followed DESC, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Studio{}
	for rows.Next() {
		s, err := scanStudio(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetStudioFollowed follows or unfollows a studio.
func (r *Repo) SetStudioFollowed(id int64, followed bool) error {
	_, err := r.db.Exec(`UPDATE scene_studios SET followed = ? WHERE id = ?`, followed, id)
	return err
}

// StudioChecked records when a followed studio's scenes were looked up.
func (r *Repo) StudioChecked(id int64) error {
	_, err := r.db.Exec(`UPDATE scene_studios SET checked_at = ? WHERE id = ?`, now(), id)
	return err
}

// ---- downloads ----

// LinkDownload records that a download is for a scene.
func (r *Repo) LinkDownload(queueID, sceneID int64) error {
	_, err := r.db.Exec(`INSERT OR REPLACE INTO scene_downloads (queue_id, scene_id) VALUES (?, ?)`, queueID, sceneID)
	return err
}

// SceneForDownload returns the scene a download is for, or ErrNotFound when
// the download isn't a scene download.
func (r *Repo) SceneForDownload(queueID int64) (Scene, error) {
	var id int64
	err := r.db.QueryRow(`SELECT scene_id FROM scene_downloads WHERE queue_id = ?`, queueID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Scene{}, ErrNotFound
	}
	if err != nil {
		return Scene{}, err
	}
	return r.GetScene(id)
}

// IsSceneDownload reports whether a download belongs to the Scenes module.
func (r *Repo) IsSceneDownload(queueID int64) bool {
	var n int
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM scene_downloads WHERE queue_id = ?`, queueID).Scan(&n)
	return n > 0
}

// SceneDownloadIDs returns every download id that belongs to a scene, for
// hiding them from people who aren't administrators.
func (r *Repo) SceneDownloadIDs() (map[int64]bool, error) {
	rows, err := r.db.Query(`SELECT queue_id FROM scene_downloads`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// HasActiveDownload reports whether a download for the scene is still
// running or waiting.
func (r *Repo) HasActiveDownload(sceneID int64) (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM scene_downloads d JOIN download_queue q ON q.id = d.queue_id
		WHERE d.scene_id = ? AND q.status IN ('queued', 'downloading', 'importing', 'paused', 'conflict')`, sceneID).Scan(&n)
	return n > 0, err
}
