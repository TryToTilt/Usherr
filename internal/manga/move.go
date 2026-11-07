package manga

// Bringing manga over from Comics: moving a file without ever overwriting one, and
// the record of a move, so it can be undone.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrFileThere means a move found a file where it would put one, and left both.
var ErrFileThere = errors.New("a file is already there")

// MoveNoOverwrite moves a file. It never replaces a file, and the original goes only
// once the new one is whole, also from one drive to another.
func MoveNoOverwrite(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return ErrFileThere
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	// A hard link fails if something took the name meanwhile, which a rename wouldn't.
	err := os.Link(from, to)
	if err == nil {
		return os.Remove(from)
	}
	if errors.Is(err, fs.ErrExist) {
		return ErrFileThere
	}
	return copyThenRemove(from, to)
}

func copyThenRemove(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		return err
	}
	tmp := to + ".moving"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, src)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != st.Size() {
		err = fmt.Errorf("copied %d of %d bytes", n, st.Size())
	}
	if err == nil {
		if _, lerr := os.Lstat(to); lerr == nil {
			err = ErrFileThere
		}
	}
	if err == nil {
		err = os.Rename(tmp, to)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	src.Close()
	return os.Remove(from)
}

// SameFile reports whether two files have the same bytes.
func SameFile(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	if err != nil || sa.Size() != sb.Size() {
		return false
	}
	ha, err := fileHash(a)
	if err != nil {
		return false
	}
	hb, err := fileHash(b)
	return err == nil && ha == hb
}

func fileHash(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return string(h.Sum(nil)), nil
}

var magazineWords = regexp.MustCompile(`(?i)\b(monthly|weekly|bi-?weekly|magazine|anthology|spirits|shonen jump|shounen jump|shonen champion|young jump)\b`)

// LooksLikeMagazine says a comic series is a magazine rather than one manga: its
// name, or more issues than any manga has volumes.
func LooksLikeMagazine(name string, issues int) bool {
	return magazineWords.MatchString(name) || issues >= 300
}

// Move statuses.
const (
	MovePreview  = "preview"
	MoveApplying = "applying"
	MoveApplied  = "applied"
	MoveUndoing  = "undoing"
	MoveUndone   = "undone"
	MoveStopped  = "stopped" // ended part way; what was done can still be undone
)

// Why a file stays where it is.
const (
	StayIdentical = "identical" // the same bytes as a file that moves
	StayCopy      = "copy"      // another copy of a volume that has one
	StayExists    = "exists"    // a file is already at the new name, or the volume has one
	StayUnread    = "unread"    // the name doesn't say which volume
	StayChanged   = "changed"   // it's gone or different since the preview
)

// MoveFile is one file in a move's plan.
type MoveFile struct {
	IssueID int64  `json:"issueId,omitempty"` // 0: found in the folder, on no issue
	Number  string `json:"number"`
	From    string `json:"from"`
	To      string `json:"to"`
	Size    int64  `json:"size"`
	Action  string `json:"action"` // move, stay
	Why     string `json:"why,omitempty"`
}

// MoveIssue is an issue with no file: a volume to add, with what it had.
type MoveIssue struct {
	IssueID int64  `json:"issueId"`
	Number  string `json:"number"`
	Wanted  bool   `json:"wanted"`
}

// MovePlan is what a move will do.
type MovePlan struct {
	ID            int64       `json:"id"`
	ComicSeriesID int64       `json:"comicSeriesId"`
	ComicName     string      `json:"comicName"`
	Title         string      `json:"title"`
	AniListID     int64       `json:"anilistId,omitempty"`
	MangaSeriesID int64       `json:"mangaSeriesId,omitempty"` // into a series already in the library
	Folder        string      `json:"folder"`
	Volumes       int         `json:"volumes"`
	Files         []MoveFile  `json:"files"`
	Issues        []MoveIssue `json:"issues"`
}

// MoveStep is one thing a move did: enough to undo it.
type MoveStep struct {
	IssueID     int64  `json:"issueId,omitempty"`
	VolumeID    int64  `json:"volumeId"`
	NewVolume   bool   `json:"newVolume,omitempty"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	Size        int64  `json:"size,omitempty"`
	IssueWanted bool   `json:"issueWanted,omitempty"`
	IssueStatus string `json:"issueStatus,omitempty"`
	IssueSize   int64  `json:"issueSize,omitempty"`
}

// MoveJournal is everything a move has done.
type MoveJournal struct {
	Steps      []MoveStep `json:"steps"`
	MonitorWas bool       `json:"monitorWas,omitempty"`
	Hidden     bool       `json:"hidden,omitempty"`
	Left       []string   `json:"left,omitempty"` // files an undo had to leave, in words
}

// Move is a row of manga_moves.
type Move struct {
	ID            int64       `json:"id"`
	ComicSeriesID int64       `json:"comicSeriesId"`
	MangaSeriesID int64       `json:"mangaSeriesId"`
	MadeSeries    bool        `json:"-"`
	ComicName     string      `json:"comicName"`
	Title         string      `json:"title"`
	Status        string      `json:"status"`
	Done          int         `json:"done"`
	Total         int         `json:"total"`
	Moved         int         `json:"moved"`
	Stayed        int         `json:"stayed"`
	Problem       string      `json:"problem,omitempty"`
	UndoLeft      []string    `json:"undoLeft,omitempty"`
	At            string      `json:"at"`
	Plan          MovePlan    `json:"-"`
	Journal       MoveJournal `json:"-"`
}

const moveCols = `id, comic_series_id, manga_series_id, made_series, comic_name, title, status, plan, journal, done, total, problem, created_at`

func scanMove(r rowScanner) (Move, error) {
	var m Move
	var plan, journal string
	err := r.Scan(&m.ID, &m.ComicSeriesID, &m.MangaSeriesID, &m.MadeSeries, &m.ComicName, &m.Title, &m.Status, &plan, &journal, &m.Done, &m.Total, &m.Problem, &m.At)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	_ = json.Unmarshal([]byte(plan), &m.Plan)
	_ = json.Unmarshal([]byte(journal), &m.Journal)
	m.UndoLeft = m.Journal.Left
	for _, s := range m.Journal.Steps {
		if s.To != "" {
			m.Moved++
		}
	}
	for _, f := range m.Plan.Files {
		if f.Action != "move" {
			m.Stayed++
		}
	}
	return m, nil
}

// SaveMovePlan keeps a new plan; an earlier one for this comic series, never
// applied, is replaced.
func (r *Repo) SaveMovePlan(p *MovePlan) error {
	if _, err := r.db.Exec(`DELETE FROM manga_moves WHERE comic_series_id = ? AND status = ?`, p.ComicSeriesID, MovePreview); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	res, err := r.db.Exec(`INSERT INTO manga_moves (comic_series_id, manga_series_id, comic_name, title, plan, total) VALUES (?, ?, ?, ?, ?, ?)`,
		p.ComicSeriesID, p.MangaSeriesID, p.ComicName, p.Title, string(b), len(p.Files)+len(p.Issues))
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// GetMove reads one move.
func (r *Repo) GetMove(id int64) (Move, error) {
	return scanMove(r.db.QueryRow(`SELECT `+moveCols+` FROM manga_moves WHERE id = ?`, id))
}

// Moves are the moves that went ahead, newest first.
func (r *Repo) Moves() ([]Move, error) {
	rows, err := r.db.Query(`SELECT ` + moveCols + ` FROM manga_moves WHERE status != 'preview' ORDER BY id DESC LIMIT 30`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Move{}
	for rows.Next() {
		m, err := scanMove(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SaveMove writes where a move has got to. It's called after each step, so a move
// cut off part way can still be undone.
func (r *Repo) SaveMove(m Move) error {
	plan, err := json.Marshal(m.Plan)
	if err != nil {
		return err
	}
	journal, err := json.Marshal(m.Journal)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`UPDATE manga_moves SET manga_series_id = ?, made_series = ?, status = ?, plan = ?, journal = ?, done = ?, total = ?, problem = ? WHERE id = ?`,
		m.MangaSeriesID, m.MadeSeries, m.Status, string(plan), string(journal), m.Done, m.Total, m.Problem, m.ID)
	return err
}

// CarryReading gives a volume what people had of a comic issue: their place and
// bookmarks move to it, and what they finished is copied.
func (r *Repo) CarryReading(issueID, volumeID int64) error {
	comic, item := fmt.Sprintf("comic:%d", issueID), PositionItem(volumeID)
	if _, err := r.db.Exec(`UPDATE OR IGNORE home_positions SET item = ? WHERE item = ?`, item, comic); err != nil {
		return err
	}
	if _, err := r.db.Exec(`UPDATE home_reading_marks SET item = ? WHERE item = ?`, item, comic); err != nil {
		return err
	}
	_, err := r.db.Exec(`INSERT OR IGNORE INTO manga_read (user_id, volume_id, read_ms) SELECT user_id, ?, read_ms FROM comic_read WHERE issue_id = ?`, volumeID, issueID)
	return err
}

// CarryReaderPrefs copies how people read a comic series. A comic's page-turn view
// is this reader's choice of its own, so only what means the same here comes along.
func (r *Repo) CarryReaderPrefs(comicSeriesID, mangaSeriesID int64) error {
	_, err := r.db.Exec(`INSERT OR IGNORE INTO manga_reader_prefs (user_id, series_id, direction, layout)
		SELECT user_id, ?, direction, CASE layout WHEN 'slide' THEN 'single' WHEN 'double' THEN 'double' WHEN 'webtoon' THEN 'webtoon' ELSE '' END
		FROM comic_reader_prefs WHERE series_id = ?`, mangaSeriesID, comicSeriesID)
	return err
}

// UncarryReading takes a volume's reading back to the comic issue.
func (r *Repo) UncarryReading(issueID, volumeID int64) error {
	comic, item := fmt.Sprintf("comic:%d", issueID), PositionItem(volumeID)
	if _, err := r.db.Exec(`UPDATE OR IGNORE home_positions SET item = ? WHERE item = ?`, comic, item); err != nil {
		return err
	}
	if _, err := r.db.Exec(`UPDATE home_reading_marks SET item = ? WHERE item = ?`, comic, item); err != nil {
		return err
	}
	_, err := r.db.Exec(`DELETE FROM manga_read WHERE volume_id = ?`, volumeID)
	return err
}

// DeleteVolume removes a volume row a move added.
func (r *Repo) DeleteVolume(id int64) error {
	_, err := r.db.Exec(`DELETE FROM manga_volumes WHERE id = ?`, id)
	return err
}

// PlainNumber reports whether a comic's issue number can be a volume's.
func PlainNumber(s string) bool { return plainNum.MatchString(strings.TrimSpace(s)) }
