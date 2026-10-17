// Package videos keeps the home fork's NSFW library: adult videos,
// the performers and studios they come from, and which of them are
// followed. Details come from a stash-box server such as StashDB (see
// stashdb.go). The whole module is for administrators only; nothing in this
// package decides that, the API layer does.
package nsfw

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status of a video in the library.
const (
	StatusMissing     = "missing"
	StatusDownloading = "downloading"
	StatusDownloaded  = "downloaded"
)

// Where a video came from.
const (
	AddedManual    = "manual"
	AddedPerformer = "performer"
	AddedStudio    = "studio"
	AddedImport    = "import"
)

// ErrNotFound is returned when a row doesn't exist.
var ErrNotFound = errors.New("not found")

// Credit is one performer in a video.
type Credit struct {
	StashID string `json:"id"`
	Name    string `json:"name"`
	As      string `json:"as,omitempty"` // the alias they appeared under
	// Gender as StashDB has it: MALE, FEMALE, TRANSGENDER_MALE,
	// TRANSGENDER_FEMALE, INTERSEX, NON_BINARY; "" when not known.
	Gender string `json:"gender,omitempty"`
}

// Video is one video in the library.
type Video struct {
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

// Name is how a video is shown in activity and notifications.
func (s Video) Name() string {
	parts := []string{}
	if s.StudioName != "" {
		parts = append(parts, s.StudioName)
	}
	if s.ReleaseDate != "" {
		parts = append(parts, s.ReleaseDate)
	}
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = "Untitled video"
	}
	parts = append(parts, title)
	return strings.Join(parts, " - ")
}

// Released reports whether the video is out (an unknown date counts as out).
func (s Video) Released(now time.Time) bool {
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

// Repo stores the NSFW library.
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

const videoCols = `id, stash_id, title, studio_stash_id, studio_name, studio_aliases, release_date, duration, code, details, image_url, performers, wanted, status, file_path, quality, added_by, added_at, tags, details_at`

func scanVideo(sc interface{ Scan(...any) error }) (Video, error) {
	var s Video
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

// AddVideo adds a video, or refreshes the details of one already there
// (keeping its wanted flag, status and file). added reports a new row.
func (r *Repo) AddVideo(s Video) (out Video, added bool, err error) {
	if s.StashID == "" {
		return s, false, errors.New("a video needs its stash-box id")
	}
	if s.AddedBy == "" {
		s.AddedBy = AddedManual
	}
	_, existsErr := r.GetVideoByStashID(s.StashID)
	if s.Tags == nil {
		s.Tags = []string{}
	}
	// Details from an import (no StashDB read) never overwrite tags or
	// genders that StashDB already gave.
	_, err = r.db.Exec(`INSERT INTO nsfw_videos (stash_id, title, studio_stash_id, studio_name, studio_aliases, release_date, duration, code, details, image_url, performers, wanted, added_by, tags, details_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(stash_id) DO UPDATE SET title = excluded.title, studio_stash_id = excluded.studio_stash_id, studio_name = excluded.studio_name,
			studio_aliases = excluded.studio_aliases, release_date = excluded.release_date, duration = excluded.duration, code = excluded.code,
			details = excluded.details, image_url = excluded.image_url,
			performers = CASE WHEN excluded.details_at <> '' OR nsfw_videos.details_at = '' THEN excluded.performers ELSE nsfw_videos.performers END,
			tags = CASE WHEN excluded.details_at <> '' THEN excluded.tags ELSE nsfw_videos.tags END,
			details_at = CASE WHEN excluded.details_at <> '' THEN excluded.details_at ELSE nsfw_videos.details_at END,
			updated_at = ?`,
		s.StashID, s.Title, s.StudioStashID, s.StudioName, toJSON(s.StudioAliases), s.ReleaseDate, s.Duration, s.Code, s.Details, s.ImageURL, toJSON(s.Performers), s.Wanted, s.AddedBy, toJSON(s.Tags), s.DetailsAt, now())
	if err != nil {
		return s, false, fmt.Errorf("add video: %w", err)
	}
	got, err := r.GetVideoByStashID(s.StashID)
	if err != nil {
		return s, false, err
	}
	return got, errors.Is(existsErr, ErrNotFound), nil
}

// GetVideo returns the video with id.
func (r *Repo) GetVideo(id int64) (Video, error) {
	s, err := scanVideo(r.db.QueryRow(`SELECT `+videoCols+` FROM nsfw_videos WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

// GetVideoByStashID returns the video with a stash-box id.
func (r *Repo) GetVideoByStashID(stashID string) (Video, error) {
	s, err := scanVideo(r.db.QueryRow(`SELECT `+videoCols+` FROM nsfw_videos WHERE stash_id = ?`, stashID))
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

func (r *Repo) listNsfw(where string, args ...any) ([]Video, error) {
	rows, err := r.db.Query(`SELECT `+videoCols+` FROM nsfw_videos `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Video{}
	for rows.Next() {
		s, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListNsfw returns every video, newest release first.
func (r *Repo) ListNsfw() ([]Video, error) {
	return r.listNsfw(`ORDER BY release_date DESC, id DESC`)
}

// MissingDetails returns up to n videos whose details were never read from
// StashDB (tags and performer genders unknown), for filling them in.
func (r *Repo) MissingDetails(n int) ([]Video, error) {
	return r.listNsfw(`WHERE details_at = '' ORDER BY id LIMIT ?`, n)
}

// Wanted returns the wanted videos without a file, newest release first.
func (r *Repo) Wanted() ([]Video, error) {
	return r.listNsfw(`WHERE wanted = 1 AND status = ? ORDER BY release_date DESC, id DESC`, StatusMissing)
}

// NsfwWithPerformer returns the videos a performer (stash id) is in.
func (r *Repo) NsfwWithPerformer(stashID string) ([]Video, error) {
	return r.listNsfw(`WHERE EXISTS (SELECT 1 FROM json_each(nsfw_videos.performers) WHERE json_extract(value, '$.id') = ?) ORDER BY release_date DESC`, stashID)
}

// NsfwFromStudio returns the videos of a studio (stash id).
func (r *Repo) NsfwFromStudio(stashID string) ([]Video, error) {
	return r.listNsfw(`WHERE studio_stash_id = ? ORDER BY release_date DESC`, stashID)
}

// SetWanted marks a video wanted or not.
func (r *Repo) SetWanted(id int64, wanted bool) error {
	_, err := r.db.Exec(`UPDATE nsfw_videos SET wanted = ?, updated_at = ? WHERE id = ?`, wanted, now(), id)
	return err
}

// SetState records a video's status, and its file and quality when it has one.
func (r *Repo) SetState(id int64, status, path, quality string) error {
	if status == StatusDownloaded {
		_, err := r.db.Exec(`UPDATE nsfw_videos SET status = ?, file_path = ?, quality = ?, updated_at = ? WHERE id = ?`, status, path, quality, now(), id)
		return err
	}
	_, err := r.db.Exec(`UPDATE nsfw_videos SET status = ?, updated_at = ? WHERE id = ?`, status, now(), id)
	return err
}

// BulkResult says what a bulk change did.
type BulkResult struct {
	Changed int `json:"changed"`
	Skipped int `json:"skipped"` // removals refused: the video has a file or a running download
}

// SetWantedMany marks many videos wanted or not, in one transaction.
func (r *Repo) SetWantedMany(ids []int64, wanted bool) (BulkResult, error) {
	var res BulkResult
	tx, err := r.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	ts := now()
	for _, id := range ids {
		out, err := tx.Exec(`UPDATE nsfw_videos SET wanted = ?, updated_at = ? WHERE id = ?`, wanted, ts, id)
		if err != nil {
			return res, err
		}
		if n, _ := out.RowsAffected(); n > 0 {
			res.Changed++
		}
	}
	return res, tx.Commit()
}

// RemoveEmpty removes videos that have no file from the library, in one
// transaction. A video with a file, or with a download still running or
// waiting, is never removed (it counts as skipped). No file is touched.
func (r *Repo) RemoveEmpty(ids []int64) (BulkResult, error) {
	var res BulkResult
	tx, err := r.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		out, err := tx.Exec(`DELETE FROM nsfw_videos WHERE id = ? AND status <> 'downloaded' AND file_path = ''
			AND NOT EXISTS (SELECT 1 FROM nsfw_downloads d JOIN download_queue q ON q.id = d.queue_id
				WHERE d.video_id = nsfw_videos.id AND q.status IN ('queued', 'downloading', 'importing', 'paused', 'conflict'))`, id)
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

// DeleteVideo removes a video from the library (not its file).
func (r *Repo) DeleteVideo(id int64) error {
	_, err := r.db.Exec(`DELETE FROM nsfw_videos WHERE id = ?`, id)
	return err
}

// Counts is the size of the library.
type Counts struct {
	Nsfw       int `json:"nsfw"`
	Wanted     int `json:"wanted"`
	Downloaded int `json:"downloaded"`
	Performers int `json:"performers"`
	Studios    int `json:"studios"`
}

// Count returns the library's size.
func (r *Repo) Count() (Counts, error) {
	var c Counts
	err := r.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM nsfw_videos),
		(SELECT COUNT(*) FROM nsfw_videos WHERE wanted = 1 AND status <> 'downloaded'),
		(SELECT COUNT(*) FROM nsfw_videos WHERE status = 'downloaded'),
		(SELECT COUNT(*) FROM nsfw_performers WHERE followed = 1),
		(SELECT COUNT(*) FROM nsfw_studios WHERE followed = 1)`).Scan(&c.Nsfw, &c.Wanted, &c.Downloaded, &c.Performers, &c.Studios)
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
	_, err := r.db.Exec(`INSERT INTO nsfw_performers (stash_id, name, disambiguation, aliases, image_url, followed) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(stash_id) DO UPDATE SET name = excluded.name, disambiguation = excluded.disambiguation, aliases = excluded.aliases,
			image_url = excluded.image_url, followed = MAX(nsfw_performers.followed, excluded.followed)`,
		p.StashID, p.Name, p.Disambiguation, toJSON(p.Aliases), p.ImageURL, follow)
	if err != nil {
		return p, fmt.Errorf("save performer: %w", err)
	}
	return scanPerformer(r.db.QueryRow(`SELECT `+performerCols+` FROM nsfw_performers WHERE stash_id = ?`, p.StashID))
}

// GetPerformer returns the performer with id.
func (r *Repo) GetPerformer(id int64) (Performer, error) {
	p, err := scanPerformer(r.db.QueryRow(`SELECT `+performerCols+` FROM nsfw_performers WHERE id = ?`, id))
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
	rows, err := r.db.Query(`SELECT ` + performerCols + ` FROM nsfw_performers ` + where + ` ORDER BY followed DESC, name COLLATE NOCASE`)
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
	_, err := r.db.Exec(`UPDATE nsfw_performers SET followed = ? WHERE id = ?`, followed, id)
	return err
}

// PerformerChecked records when a followed performer's videos were looked up.
func (r *Repo) PerformerChecked(id int64) error {
	_, err := r.db.Exec(`UPDATE nsfw_performers SET checked_at = ? WHERE id = ?`, now(), id)
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
	_, err := r.db.Exec(`INSERT INTO nsfw_studios (stash_id, name, aliases, parent_name, image_url, followed) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(stash_id) DO UPDATE SET name = excluded.name, aliases = excluded.aliases, parent_name = excluded.parent_name,
			image_url = excluded.image_url, followed = MAX(nsfw_studios.followed, excluded.followed)`,
		s.StashID, s.Name, toJSON(s.Aliases), s.ParentName, s.ImageURL, follow)
	if err != nil {
		return s, fmt.Errorf("save studio: %w", err)
	}
	return scanStudio(r.db.QueryRow(`SELECT `+studioCols+` FROM nsfw_studios WHERE stash_id = ?`, s.StashID))
}

// GetStudio returns the studio with id.
func (r *Repo) GetStudio(id int64) (Studio, error) {
	s, err := scanStudio(r.db.QueryRow(`SELECT `+studioCols+` FROM nsfw_studios WHERE id = ?`, id))
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
	rows, err := r.db.Query(`SELECT ` + studioCols + ` FROM nsfw_studios ` + where + ` ORDER BY followed DESC, name COLLATE NOCASE`)
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
	_, err := r.db.Exec(`UPDATE nsfw_studios SET followed = ? WHERE id = ?`, followed, id)
	return err
}

// StudioChecked records when a followed studio's videos were looked up.
func (r *Repo) StudioChecked(id int64) error {
	_, err := r.db.Exec(`UPDATE nsfw_studios SET checked_at = ? WHERE id = ?`, now(), id)
	return err
}

// ---- downloads ----

// LinkDownload records that a download is for a video.
func (r *Repo) LinkDownload(queueID, videoID int64) error {
	_, err := r.db.Exec(`INSERT OR REPLACE INTO nsfw_downloads (queue_id, video_id) VALUES (?, ?)`, queueID, videoID)
	return err
}

// VideoForDownload returns the video a download is for, or ErrNotFound when
// the download isn't a video download.
func (r *Repo) VideoForDownload(queueID int64) (Video, error) {
	var id int64
	err := r.db.QueryRow(`SELECT video_id FROM nsfw_downloads WHERE queue_id = ?`, queueID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Video{}, ErrNotFound
	}
	if err != nil {
		return Video{}, err
	}
	return r.GetVideo(id)
}

// IsVideoDownload reports whether a download belongs to the NSFW module.
func (r *Repo) IsVideoDownload(queueID int64) bool {
	var n int
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM nsfw_downloads WHERE queue_id = ?`, queueID).Scan(&n)
	return n > 0
}

// VideoDownloadIDs returns every download id that belongs to a video, for
// hiding them from people who aren't administrators.
func (r *Repo) VideoDownloadIDs() (map[int64]bool, error) {
	rows, err := r.db.Query(`SELECT queue_id FROM nsfw_downloads`)
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

// HasActiveDownload reports whether a download for the video is still
// running or waiting.
func (r *Repo) HasActiveDownload(videoID int64) (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM nsfw_downloads d JOIN download_queue q ON q.id = d.queue_id
		WHERE d.video_id = ? AND q.status IN ('queued', 'downloading', 'importing', 'paused', 'conflict')`, videoID).Scan(&n)
	return n > 0, err
}
