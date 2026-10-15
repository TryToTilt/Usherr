// Package comics keeps the home fork's Comics library: series (ComicVine
// "volumes") and their issues, which are wanted, and where their files are.
// Details come from ComicVine (see comicvine.go).
package comics

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Status of an issue.
const (
	StatusMissing     = "missing"
	StatusDownloading = "downloading"
	StatusDownloaded  = "downloaded"
)

// ErrNotFound is returned when a row doesn't exist.
var ErrNotFound = errors.New("not found")

// Series is one comic series.
type Series struct {
	ID          int64  `json:"id"`
	CVID        int64  `json:"cvId"`
	Name        string `json:"name"`
	StartYear   int    `json:"startYear"`
	Publisher   string `json:"publisher"`
	IssueCount  int    `json:"issueCount"`
	Description string `json:"description,omitempty"`
	ImageURL    string `json:"imageUrl"`
	Folder      string `json:"folder"`
	MonitorNew  bool   `json:"monitorNew"`
	CheckedAt   string `json:"checkedAt,omitempty"`
	AddedAt     string `json:"addedAt"`
	// Counts, filled by List.
	Have   int `json:"have"`
	Wanted int `json:"wanted"`
}

// Issue is one issue of a series.
type Issue struct {
	ID        int64   `json:"id"`
	SeriesID  int64   `json:"seriesId"`
	CVID      int64   `json:"cvId"`
	Number    string  `json:"number"`
	SortKey   float64 `json:"-"`
	Title     string  `json:"title"`
	CoverDate string  `json:"coverDate"`
	StoreDate string  `json:"storeDate"`
	ImageURL  string  `json:"imageUrl"`
	Wanted    bool    `json:"wanted"`
	Status    string  `json:"status"`
	FilePath  string  `json:"filePath,omitempty"`
	FileSize  int64   `json:"fileSize,omitempty"`
}

// Released reports whether the issue is out on day now (a missing date
// counts as out).
func (i Issue) Released(now time.Time) bool {
	d := i.StoreDate
	if d == "" {
		d = i.CoverDate
	}
	if len(d) < 10 {
		return true
	}
	t, err := time.Parse("2006-01-02", d[:10])
	return err != nil || !t.After(now)
}

var leadingNum = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// SortKey turns an issue number into something to order by: "1" < "1.1" <
// "2" < "10"; numbers without digits ("½") sort first, "Annual 1" after.
func SortKey(number string) float64 {
	n := strings.TrimSpace(number)
	if n == "½" || n == "1/2" {
		return 0.5
	}
	m := leadingNum.FindString(n)
	if m == "" {
		return -1
	}
	f, _ := strconv.ParseFloat(m, 64)
	if strings.Contains(strings.ToLower(n), "annual") {
		f += 100000
	}
	return f
}

// NormalNumber is an issue number for comparing: "001" -> "1", "1.0" -> "1".
func NormalNumber(n string) string {
	n = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(n), "#"))
	if l := strings.ToLower(n); strings.HasSuffix(l, ".now") || strings.HasSuffix(l, "·now") {
		// Marvel's ".NOW" issues ("15.NOW"): the number before it, normalised.
		return NormalNumber(strings.TrimSuffix(strings.TrimSuffix(l, ".now"), "·now")) + ".now"
	}
	if f, err := strconv.ParseFloat(n, 64); err == nil {
		if f == math.Trunc(f) {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strings.ToLower(n)
}

// Repo stores the comics library.
type Repo struct{ db *sql.DB }

// NewRepo returns a Repo over db.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const seriesCols = `id, cv_id, name, start_year, publisher, issue_count, description, image_url, folder, monitor_new, checked_at, added_at`

func scanSeries(sc interface{ Scan(...any) error }) (Series, error) {
	var s Series
	var mon int
	err := sc.Scan(&s.ID, &s.CVID, &s.Name, &s.StartYear, &s.Publisher, &s.IssueCount, &s.Description, &s.ImageURL, &s.Folder, &mon, &s.CheckedAt, &s.AddedAt)
	s.MonitorNew = mon == 1
	return s, err
}

const issueCols = `id, series_id, cv_id, number, sort_key, title, cover_date, store_date, image_url, wanted, status, file_path, file_size`

func scanIssue(sc interface{ Scan(...any) error }) (Issue, error) {
	var i Issue
	var w int
	err := sc.Scan(&i.ID, &i.SeriesID, &i.CVID, &i.Number, &i.SortKey, &i.Title, &i.CoverDate, &i.StoreDate, &i.ImageURL, &w, &i.Status, &i.FilePath, &i.FileSize)
	i.Wanted = w == 1
	return i, err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpsertSeries adds a series or refreshes its details (keeping the folder
// and monitoring choice of one already there). It returns the stored row.
func (r *Repo) UpsertSeries(s Series) (Series, error) {
	_, err := r.db.Exec(`INSERT INTO comic_series (cv_id, name, start_year, publisher, issue_count, description, image_url, folder, monitor_new)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cv_id) DO UPDATE SET name = excluded.name, start_year = excluded.start_year, publisher = excluded.publisher,
			issue_count = excluded.issue_count, description = CASE WHEN excluded.description != '' THEN excluded.description ELSE comic_series.description END,
			image_url = CASE WHEN excluded.image_url != '' THEN excluded.image_url ELSE comic_series.image_url END`,
		s.CVID, s.Name, s.StartYear, s.Publisher, s.IssueCount, s.Description, s.ImageURL, s.Folder, b2i(s.MonitorNew))
	if err != nil {
		return Series{}, fmt.Errorf("save series: %w", err)
	}
	return r.SeriesByCV(s.CVID)
}

// SeriesByCV returns a series by its ComicVine id.
func (r *Repo) SeriesByCV(cvID int64) (Series, error) {
	s, err := scanSeries(r.db.QueryRow(`SELECT `+seriesCols+` FROM comic_series WHERE cv_id = ?`, cvID))
	if errors.Is(err, sql.ErrNoRows) {
		return Series{}, ErrNotFound
	}
	return s, err
}

// GetSeries returns a series by id.
func (r *Repo) GetSeries(id int64) (Series, error) {
	s, err := scanSeries(r.db.QueryRow(`SELECT `+seriesCols+` FROM comic_series WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Series{}, ErrNotFound
	}
	return s, err
}

// ListSeries returns every series with how many issues it has and wants.
func (r *Repo) ListSeries() ([]Series, error) {
	rows, err := r.db.Query(`SELECT ` + seriesCols + `,
		(SELECT COUNT(*) FROM comic_issues i WHERE i.series_id = comic_series.id AND i.status = 'downloaded'),
		(SELECT COUNT(*) FROM comic_issues i WHERE i.series_id = comic_series.id AND i.wanted = 1 AND i.status != 'downloaded')
		FROM comic_series ORDER BY name COLLATE NOCASE, start_year`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Series{}
	for rows.Next() {
		var s Series
		var mon int
		if err := rows.Scan(&s.ID, &s.CVID, &s.Name, &s.StartYear, &s.Publisher, &s.IssueCount, &s.Description, &s.ImageURL, &s.Folder, &mon, &s.CheckedAt, &s.AddedAt, &s.Have, &s.Wanted); err != nil {
			return nil, err
		}
		s.MonitorNew = mon == 1
		s.Description = ""
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetSeriesFolder records where a series' issues go.
func (r *Repo) SetSeriesFolder(id int64, folder string) error {
	_, err := r.db.Exec(`UPDATE comic_series SET folder = ? WHERE id = ?`, folder, id)
	return err
}

// SetMonitorNew sets whether new issues of a series are wanted.
func (r *Repo) SetMonitorNew(id int64, on bool) error {
	_, err := r.db.Exec(`UPDATE comic_series SET monitor_new = ? WHERE id = ?`, b2i(on), id)
	return err
}

// MarkChecked records that a series' issues were just fetched.
func (r *Repo) MarkChecked(id int64) error {
	_, err := r.db.Exec(`UPDATE comic_series SET checked_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// DeleteSeries removes a series and its issues from the library (files are
// never touched).
func (r *Repo) DeleteSeries(id int64) error {
	_, err := r.db.Exec(`DELETE FROM comic_series WHERE id = ?`, id)
	return err
}

// UpsertIssue adds an issue or refreshes its details, keeping its wanted
// flag, status and file. New issues get wanted as given. It reports whether
// the issue was new.
func (r *Repo) UpsertIssue(i Issue) (bool, error) {
	res, err := r.db.Exec(`INSERT INTO comic_issues (series_id, cv_id, number, sort_key, title, cover_date, store_date, image_url, wanted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(cv_id) DO NOTHING`,
		i.SeriesID, i.CVID, i.Number, SortKey(i.Number), i.Title, i.CoverDate, i.StoreDate, i.ImageURL, b2i(i.Wanted))
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return true, nil
	}
	_, err = r.db.Exec(`UPDATE comic_issues SET number = ?, sort_key = ?, title = ?, cover_date = ?, store_date = ?,
		image_url = CASE WHEN ? != '' THEN ? ELSE image_url END WHERE cv_id = ?`,
		i.Number, SortKey(i.Number), i.Title, i.CoverDate, i.StoreDate, i.ImageURL, i.ImageURL, i.CVID)
	return false, err
}

// Issues returns the issues of a series in reading order.
func (r *Repo) Issues(seriesID int64) ([]Issue, error) {
	return r.queryIssues(`SELECT `+issueCols+` FROM comic_issues WHERE series_id = ? ORDER BY sort_key, number`, seriesID)
}

// GetIssue returns an issue by id.
func (r *Repo) GetIssue(id int64) (Issue, error) {
	i, err := scanIssue(r.db.QueryRow(`SELECT `+issueCols+` FROM comic_issues WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	return i, err
}

// WantedMissing returns wanted issues without a file, oldest first.
func (r *Repo) WantedMissing(limit int) ([]Issue, error) {
	return r.queryIssues(`SELECT `+issueCols+` FROM comic_issues WHERE wanted = 1 AND status = 'missing'
		ORDER BY CASE WHEN store_date != '' THEN store_date ELSE cover_date END DESC LIMIT ?`, limit)
}

func (r *Repo) queryIssues(q string, args ...any) ([]Issue, error) {
	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Issue{}
	for rows.Next() {
		i, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// SetWanted sets whether issues are wanted.
func (r *Repo) SetWanted(ids []int64, wanted bool) (int, error) {
	n := 0
	for _, id := range ids {
		res, err := r.db.Exec(`UPDATE comic_issues SET wanted = ? WHERE id = ?`, b2i(wanted), id)
		if err != nil {
			return n, err
		}
		if c, _ := res.RowsAffected(); c > 0 {
			n++
		}
	}
	return n, nil
}

// SetState sets an issue's status and file.
func (r *Repo) SetState(id int64, status, path string, size int64) error {
	if status == StatusDownloading {
		_, err := r.db.Exec(`UPDATE comic_issues SET status = ? WHERE id = ?`, status, id)
		return err
	}
	_, err := r.db.Exec(`UPDATE comic_issues SET status = ?, file_path = ?, file_size = ? WHERE id = ?`, status, path, size, id)
	return err
}

// LinkDownload records that a queue item downloads an issue.
func (r *Repo) LinkDownload(queueID, issueID int64) error {
	_, err := r.db.Exec(`INSERT OR REPLACE INTO comic_downloads (queue_id, issue_id) VALUES (?, ?)`, queueID, issueID)
	return err
}

// IssueForDownload returns the issue a queue item downloads.
func (r *Repo) IssueForDownload(queueID int64) (Issue, error) {
	i, err := scanIssue(r.db.QueryRow(`SELECT `+prefixed("i", issueCols)+` FROM comic_issues i JOIN comic_downloads d ON d.issue_id = i.id WHERE d.queue_id = ?`, queueID))
	if errors.Is(err, sql.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	return i, err
}

// HasActiveDownload reports whether an issue has a download running or waiting.
func (r *Repo) HasActiveDownload(issueID int64) (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM comic_downloads d JOIN download_queue q ON q.id = d.queue_id
		WHERE d.issue_id = ? AND q.status IN ('queued','downloading','importing','paused')`, issueID).Scan(&n)
	return n > 0, err
}

func prefixed(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
