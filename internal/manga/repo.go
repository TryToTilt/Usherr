package manga

// The Manga section's tables: series, their other titles, volumes, which
// download is for which volume, the review list and the search log.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Status of a volume, as stored.
const (
	StatusMissing    = "missing"
	StatusDownloaded = "downloaded"
)

// ErrNotFound is returned when a row doesn't exist.
var ErrNotFound = errors.New("not found")

// Series is one manga series in the library.
type Series struct {
	ID          int64  `json:"id"`
	AniListID   int64  `json:"anilistId,omitempty"`
	Title       string `json:"title"`
	SortTitle   string `json:"-"`
	Format      string `json:"format"`
	Status      string `json:"status"`
	StartYear   int    `json:"startYear"`
	Author      string `json:"author"`
	Publisher   string `json:"publisher"`
	BuyURL      string `json:"buyUrl,omitempty"`
	SiteURL     string `json:"siteUrl,omitempty"`
	LicensedEN  int    `json:"licensedEn"` // 1 known, -1 unknown
	IsAdult     bool   `json:"isAdult,omitempty"`
	Description string `json:"description,omitempty"`
	CoverURL    string `json:"coverUrl,omitempty"`
	VolumeCount int    `json:"volumeCount"`
	MonitorNew  bool   `json:"monitorNew"`
	WantAll     bool   `json:"wantAll"`
	SourceMode  string `json:"sourceMode"`
	Folder      string `json:"folder,omitempty"`
	CheckedAt   string `json:"checkedAt,omitempty"`
	AddedAt     string `json:"addedAt"`
	// Counts, filled by ListSeries and GetSeries.
	Total       int `json:"total"`       // volumes known
	Have        int `json:"have"`        // on disk
	Wanted      int `json:"wanted"`      // wanted and not on disk
	Downloading int `json:"downloading"` // in the download line
	Waiting     int `json:"waiting"`     // releases waiting for a person
	// Chapters, from MangaDex.
	Following    bool `json:"following,omitempty"`    // this series follows chapters
	ChaptersHave int  `json:"chaptersHave,omitempty"` // chapters on disk
}

// Volume is one volume of a series. State is what the page shows: downloaded,
// downloading, waiting (on the review list) or missing.
type Volume struct {
	ID          int64   `json:"id"`
	SeriesID    int64   `json:"seriesId"`
	Number      string  `json:"number"`
	SortKey     float64 `json:"-"`
	Title       string  `json:"title,omitempty"`
	Wanted      bool    `json:"wanted"`
	Status      string  `json:"status"`
	State       string  `json:"state"`
	FilePath    string  `json:"filePath,omitempty"`
	FileSize    int64   `json:"fileSize,omitempty"`
	ReleaseSize int64   `json:"-"` // as it came, before ComicInfo.xml went in
	SourceClass string  `json:"sourceClass,omitempty"`
	Reviews     int     `json:"reviews,omitempty"`
	// This person, filled by FillReading.
	Read  bool `json:"read,omitempty"`
	Page  int  `json:"page,omitempty"`  // where they left it
	Pages int  `json:"pages,omitempty"` // of how many
}

// Alias is another title of a series.
type Alias struct {
	Alias string `json:"alias"`
	Kind  string `json:"kind"`
}

// Repo keeps the Manga tables.
type Repo struct{ db *sql.DB }

// NewRepo returns a Repo on db.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const activeQueue = `('queued','downloading','importing','paused')`

const seriesCols = `s.id, COALESCE(s.anilist_id, 0), s.title, s.sort_title, s.format, s.status, s.start_year, s.author, s.publisher_en, s.buy_url, s.site_url,
	s.licensed_en, s.is_adult, s.description, s.cover_url, s.volume_count, s.monitor_new, s.want_all, s.source_mode, s.folder, s.checked_at, s.added_at,
	(SELECT COUNT(*) FROM manga_volumes v WHERE v.series_id = s.id),
	(SELECT COUNT(*) FROM manga_volumes v WHERE v.series_id = s.id AND v.status = 'downloaded'),
	(SELECT COUNT(*) FROM manga_volumes v WHERE v.series_id = s.id AND v.wanted = 1 AND v.status != 'downloaded'),
	(SELECT COUNT(*) FROM manga_downloads d JOIN download_queue q ON q.id = d.queue_id WHERE d.series_id = s.id AND q.status IN ` + activeQueue + `),
	(SELECT COUNT(*) FROM manga_reviews r WHERE r.series_id = s.id),
	EXISTS (SELECT 1 FROM manga_dex_series x WHERE x.series_id = s.id AND x.follow != ''),
	(SELECT COUNT(*) FROM manga_chapters c WHERE c.series_id = s.id AND c.status = 'downloaded')`

type rowScanner interface{ Scan(dest ...any) error }

func scanSeries(r rowScanner) (Series, error) {
	var s Series
	err := r.Scan(&s.ID, &s.AniListID, &s.Title, &s.SortTitle, &s.Format, &s.Status, &s.StartYear, &s.Author, &s.Publisher, &s.BuyURL, &s.SiteURL,
		&s.LicensedEN, &s.IsAdult, &s.Description, &s.CoverURL, &s.VolumeCount, &s.MonitorNew, &s.WantAll, &s.SourceMode, &s.Folder, &s.CheckedAt, &s.AddedAt,
		&s.Total, &s.Have, &s.Wanted, &s.Downloading, &s.Waiting, &s.Following, &s.ChaptersHave)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

var leadingThe = regexp.MustCompile(`(?i)^(the|a|an)\s+`)

func sortTitle(title string) string {
	return strings.ToLower(leadingThe.ReplaceAllString(strings.TrimSpace(title), ""))
}

// ListSeries is the library, by title.
func (r *Repo) ListSeries() ([]Series, error) {
	rows, err := r.db.Query(`SELECT ` + seriesCols + ` FROM manga_series s ORDER BY s.sort_title, s.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Series{}
	for rows.Next() {
		s, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSeries reads one series.
func (r *Repo) GetSeries(id int64) (Series, error) {
	return scanSeries(r.db.QueryRow(`SELECT `+seriesCols+` FROM manga_series s WHERE s.id = ?`, id))
}

// SeriesByAniList finds a series by its AniList id.
func (r *Repo) SeriesByAniList(id int64) (Series, error) {
	return scanSeries(r.db.QueryRow(`SELECT `+seriesCols+` FROM manga_series s WHERE s.anilist_id = ?`, id))
}

// AddSeries saves a new series, with its other titles.
func (r *Repo) AddSeries(s Series, aliases []Alias) (Series, error) {
	var anilist any
	if s.AniListID > 0 {
		anilist = s.AniListID
	}
	res, err := r.db.Exec(`INSERT INTO manga_series (anilist_id, title, sort_title, format, status, start_year, author, publisher_en, buy_url, site_url,
			licensed_en, is_adult, description, cover_url, volume_count, monitor_new, want_all, source_mode, folder, checked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		anilist, s.Title, sortTitle(s.Title), orDefault(s.Format, "manga"), s.Status, s.StartYear, s.Author, s.Publisher, s.BuyURL, s.SiteURL,
		s.LicensedEN, s.IsAdult, s.Description, s.CoverURL, s.VolumeCount, s.MonitorNew, s.WantAll, s.SourceMode, s.Folder, s.CheckedAt)
	if err != nil {
		return Series{}, err
	}
	id, _ := res.LastInsertId()
	if err := r.SetAliases(id, aliases); err != nil {
		return Series{}, err
	}
	return r.GetSeries(id)
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// UpdateDetails saves what AniList says of a series now; what a person chose
// (monitoring, sources, folder) stays.
func (r *Repo) UpdateDetails(id int64, s Series, aliases []Alias) error {
	_, err := r.db.Exec(`UPDATE manga_series SET title = ?, sort_title = ?, format = ?, status = ?, start_year = ?, author = ?, publisher_en = ?, buy_url = ?, site_url = ?,
			licensed_en = ?, is_adult = ?, description = ?, cover_url = ?, volume_count = ?, checked_at = ? WHERE id = ?`,
		s.Title, sortTitle(s.Title), orDefault(s.Format, "manga"), s.Status, s.StartYear, s.Author, s.Publisher, s.BuyURL, s.SiteURL,
		s.LicensedEN, s.IsAdult, s.Description, s.CoverURL, s.VolumeCount, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	return r.SetAliases(id, aliases)
}

// MarkChecked notes that AniList was asked just now.
func (r *Repo) MarkChecked(id int64) error {
	_, err := r.db.Exec(`UPDATE manga_series SET checked_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// SetAliases replaces a series' other titles.
func (r *Repo) SetAliases(id int64, aliases []Alias) error {
	if _, err := r.db.Exec(`DELETE FROM manga_aliases WHERE series_id = ?`, id); err != nil {
		return err
	}
	for i, a := range aliases {
		if strings.TrimSpace(a.Alias) == "" {
			continue
		}
		if _, err := r.db.Exec(`INSERT INTO manga_aliases (series_id, alias, kind, position) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			id, strings.TrimSpace(a.Alias), orDefault(a.Kind, "synonym"), i); err != nil {
			return err
		}
	}
	return nil
}

// Aliases are a series' other titles, in the order to try them.
func (r *Repo) Aliases(id int64) ([]Alias, error) {
	rows, err := r.db.Query(`SELECT alias, kind FROM manga_aliases WHERE series_id = ? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alias{}
	for rows.Next() {
		var a Alias
		if err := rows.Scan(&a.Alias, &a.Kind); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Names are the names to match a series by: its title, then the other titles.
func (r *Repo) Names(s Series) []string {
	out := []string{s.Title}
	if list, err := r.Aliases(s.ID); err == nil {
		for _, a := range list {
			if a.Alias != s.Title {
				out = append(out, a.Alias)
			}
		}
	}
	return out
}

// DeleteSeries removes a series from the library; its files stay.
func (r *Repo) DeleteSeries(id int64) error {
	_, err := r.db.Exec(`DELETE FROM manga_series WHERE id = ?`, id)
	return err
}

// SetFolder notes where a series' volumes are.
func (r *Repo) SetFolder(id int64, folder string) error {
	_, err := r.db.Exec(`UPDATE manga_series SET folder = ? WHERE id = ?`, folder, id)
	return err
}

// SetMonitorNew says whether new volumes are wanted as they turn up.
func (r *Repo) SetMonitorNew(id int64, on bool) error {
	_, err := r.db.Exec(`UPDATE manga_series SET monitor_new = ? WHERE id = ?`, on, id)
	return err
}

// SetSourceMode sets a series' own choice of sources.
func (r *Repo) SetSourceMode(id int64, mode string) error {
	_, err := r.db.Exec(`UPDATE manga_series SET source_mode = ? WHERE id = ?`, mode, id)
	return err
}

const volumeCols = `v.id, v.series_id, v.number, v.sort_key, v.title, v.wanted, v.status, v.file_path, v.file_size, v.release_size, v.source_class,
	(SELECT COUNT(*) FROM manga_downloads d JOIN download_queue q ON q.id = d.queue_id WHERE d.volume_id = v.id AND q.status IN ` + activeQueue + `),
	(SELECT COUNT(*) FROM manga_reviews r WHERE r.volume_id = v.id)`

func scanVolume(r rowScanner) (Volume, error) {
	var v Volume
	var active int
	err := r.Scan(&v.ID, &v.SeriesID, &v.Number, &v.SortKey, &v.Title, &v.Wanted, &v.Status, &v.FilePath, &v.FileSize, &v.ReleaseSize, &v.SourceClass, &active, &v.Reviews)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	switch {
	case v.Status == StatusDownloaded:
		v.State = StatusDownloaded
	case active > 0:
		v.State = "downloading"
	case v.Reviews > 0:
		v.State = "waiting"
	default:
		v.State = StatusMissing
	}
	return v, err
}

// Volumes are a series' volumes, in order.
func (r *Repo) Volumes(seriesID int64) ([]Volume, error) {
	rows, err := r.db.Query(`SELECT `+volumeCols+` FROM manga_volumes v WHERE v.series_id = ? ORDER BY v.sort_key, v.number`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Volume{}
	for rows.Next() {
		v, err := scanVolume(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetVolume reads one volume.
func (r *Repo) GetVolume(id int64) (Volume, error) {
	return scanVolume(r.db.QueryRow(`SELECT `+volumeCols+` FROM manga_volumes v WHERE v.id = ?`, id))
}

// EnsureVolume is a series' volume with this number, added when it isn't there.
func (r *Repo) EnsureVolume(seriesID int64, number string, wanted bool) (Volume, bool, error) {
	number = numOf(number)
	res, err := r.db.Exec(`INSERT INTO manga_volumes (series_id, number, sort_key, wanted) VALUES (?, ?, ?, ?) ON CONFLICT (series_id, number) DO NOTHING`,
		seriesID, number, SortKey(number), wanted)
	if err != nil {
		return Volume{}, false, err
	}
	n, _ := res.RowsAffected()
	v, err := scanVolume(r.db.QueryRow(`SELECT `+volumeCols+` FROM manga_volumes v WHERE v.series_id = ? AND v.number = ?`, seriesID, number))
	return v, n > 0, err
}

// SetVolumesWanted changes whether volumes are wanted.
func (r *Repo) SetVolumesWanted(ids []int64, wanted bool) (int, error) {
	n := 0
	for _, id := range ids {
		res, err := r.db.Exec(`UPDATE manga_volumes SET wanted = ? WHERE id = ?`, wanted, id)
		if err != nil {
			return n, err
		}
		c, _ := res.RowsAffected()
		n += int(c)
	}
	return n, nil
}

// SetVolumeFile records a volume's file, and takes it off the review list.
func (r *Repo) SetVolumeFile(id int64, path string, size int64, class string) error {
	if _, err := r.db.Exec(`UPDATE manga_volumes SET status = 'downloaded', file_path = ?, file_size = ?, source_class = ? WHERE id = ?`, path, size, class, id); err != nil {
		return err
	}
	if _, err := r.db.Exec(`DELETE FROM manga_reviews WHERE volume_id = ? AND kind != 'copy'`, id); err != nil {
		return err
	}
	return r.CarryToVolume(id) // what was read in its chapters comes along
}

// SetReleaseSize notes how big a volume's file was as it came.
func (r *Repo) SetReleaseSize(id, size int64) error {
	_, err := r.db.Exec(`UPDATE manga_volumes SET release_size = ? WHERE id = ?`, size, id)
	return err
}

// ClearVolumeFile forgets a file that has gone. The volume isn't wanted after
// that, so a drive that's away for an hour can't start a round of downloads.
func (r *Repo) ClearVolumeFile(id int64) error {
	_, err := r.db.Exec(`UPDATE manga_volumes SET status = 'missing', file_path = '', file_size = 0, release_size = 0, source_class = '', wanted = 0 WHERE id = ?`, id)
	return err
}

// Downloads.

// Download is a queue item's link to the library.
type Download struct {
	QueueID  int64
	SeriesID int64
	VolumeID int64 // 0: a pack
	Class    string
	Replace  bool
	Source   string // "" a release from an indexer; "mangadex" a batch of chapters
}

// LinkDownload ties a queue item to a volume, or to a whole series for a pack.
func (r *Repo) LinkDownload(d Download) error {
	_, err := r.db.Exec(`INSERT INTO manga_downloads (queue_id, series_id, volume_id, class, replace_file, source) VALUES (?, ?, ?, ?, ?, ?)`,
		d.QueueID, d.SeriesID, d.VolumeID, d.Class, d.Replace, d.Source)
	return err
}

// DownloadFor is the link of a queue item.
func (r *Repo) DownloadFor(queueID int64) (Download, error) {
	var d Download
	err := r.db.QueryRow(`SELECT queue_id, series_id, volume_id, class, replace_file, source FROM manga_downloads WHERE queue_id = ?`, queueID).
		Scan(&d.QueueID, &d.SeriesID, &d.VolumeID, &d.Class, &d.Replace, &d.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// ActiveForVolume reports whether a volume has a download running or waiting.
func (r *Repo) ActiveForVolume(volumeID int64) (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM manga_downloads d JOIN download_queue q ON q.id = d.queue_id
		WHERE d.volume_id = ? AND q.status IN `+activeQueue, volumeID).Scan(&n)
	return n > 0, err
}

// ActiveRelease reports whether this release is already downloading for the
// series: one release that turns up for several volumes is grabbed once.
func (r *Repo) ActiveRelease(seriesID int64, title string) (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM manga_downloads d JOIN download_queue q ON q.id = d.queue_id
		WHERE d.series_id = ? AND q.release_title = ? AND q.status IN `+activeQueue, seriesID, title).Scan(&n)
	return n > 0, err
}

// NoteRelease remembers what a release turned out to hold.
func (r *Repo) NoteRelease(seriesID int64, title, note string) error {
	_, err := r.db.Exec(`INSERT INTO manga_release_notes (series_id, title, note) VALUES (?, ?, ?)
		ON CONFLICT (series_id, title) DO UPDATE SET note = excluded.note`, seriesID, title, note)
	return err
}

// ReleaseNotes is what's remembered for a series' releases, by their names.
func (r *Repo) ReleaseNotes(seriesID int64) map[string]string {
	out := map[string]string{}
	rows, err := r.db.Query(`SELECT title, note FROM manga_release_notes WHERE series_id = ?`, seriesID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var title, note string
		if rows.Scan(&title, &note) == nil {
			out[title] = note
		}
	}
	return out
}

// Reviews.

// Review is a release waiting for a person.
type Review struct {
	ID        int64    `json:"id"`
	SeriesID  int64    `json:"seriesId"`
	Series    string   `json:"series"`
	VolumeID  int64    `json:"volumeId,omitempty"`
	Number    string   `json:"number,omitempty"`
	Kind      string   `json:"kind"` // single, pack, copy
	Title     string   `json:"title"`
	URL       string   `json:"-"`
	SizeBytes int64    `json:"sizeBytes"`
	Protocol  string   `json:"protocol"`
	Indexer   string   `json:"indexer"`
	Score     int      `json:"score"`
	Class     string   `json:"class"`
	Notes     []string `json:"notes"`
}

// AddReview puts a release on the list, or updates it when it's there.
func (r *Repo) AddReview(v Review) error {
	notes, _ := json.Marshal(v.Notes)
	_, err := r.db.Exec(`INSERT INTO manga_reviews (series_id, volume_id, kind, title, download_url, size_bytes, protocol, indexer, score, class, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (series_id, volume_id, download_url) DO UPDATE SET score = excluded.score, notes = excluded.notes, class = excluded.class`,
		v.SeriesID, v.VolumeID, orDefault(v.Kind, "single"), v.Title, v.URL, v.SizeBytes, v.Protocol, v.Indexer, v.Score, string(v.Class), string(notes))
	return err
}

const reviewSelect = `SELECT r.id, r.series_id, s.title, r.volume_id, COALESCE(v.number, ''), r.kind, r.title, r.download_url, r.size_bytes, r.protocol, r.indexer, r.score, r.class, r.notes
	FROM manga_reviews r JOIN manga_series s ON s.id = r.series_id LEFT JOIN manga_volumes v ON v.id = r.volume_id `

func scanReview(row rowScanner) (Review, error) {
	var v Review
	var notes string
	err := row.Scan(&v.ID, &v.SeriesID, &v.Series, &v.VolumeID, &v.Number, &v.Kind, &v.Title, &v.URL, &v.SizeBytes, &v.Protocol, &v.Indexer, &v.Score, &v.Class, &notes)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	_ = json.Unmarshal([]byte(notes), &v.Notes)
	return v, err
}

// Reviews is the list, for one series, or for all with seriesID 0.
func (r *Repo) Reviews(seriesID int64) ([]Review, error) {
	q, args := reviewSelect, []any{}
	if seriesID > 0 {
		q += `WHERE r.series_id = ? `
		args = append(args, seriesID)
	}
	rows, err := r.db.Query(q+`ORDER BY s.sort_title, COALESCE(v.sort_key, 0), r.score DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Review{}
	for rows.Next() {
		v, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetReview reads one entry.
func (r *Repo) GetReview(id int64) (Review, error) {
	return scanReview(r.db.QueryRow(reviewSelect+`WHERE r.id = ?`, id))
}

// DeleteReview takes one entry off the list.
func (r *Repo) DeleteReview(id int64) error {
	_, err := r.db.Exec(`DELETE FROM manga_reviews WHERE id = ?`, id)
	return err
}

// ClearReviews takes what a new search replaces off the list: a volume's
// entries, or for volumeID 0 the series' packs. Copies stay until a person answers.
func (r *Repo) ClearReviews(seriesID, volumeID int64) error {
	_, err := r.db.Exec(`DELETE FROM manga_reviews WHERE series_id = ? AND volume_id = ? AND kind != 'copy'`, seriesID, volumeID)
	return err
}

// The search log.

// SearchSummary is what one search found, for the "Why nothing yet?" sentence.
type SearchSummary struct {
	Query    string         `json:"query,omitempty"`    // the last words asked
	Asked    int            `json:"asked"`              // indexers asked
	Failed   int            `json:"failed,omitempty"`   // that didn't answer
	Skipped  int            `json:"skipped,omitempty"`  // left out: paused, or out of searches for today
	NoIndex  bool           `json:"noIndex,omitempty"`  // no indexers at all
	Found    int            `json:"found"`              // releases seen
	Rejected map[string]int `json:"rejected,omitempty"` // Reject* code, or "language:French", to how many
	Doubtful int            `json:"doubtful,omitempty"` // put on the review list
	Taken    int            `json:"taken,omitempty"`
	Number   string         `json:"number,omitempty"`
}

// Note counts one judged release.
func (s *SearchSummary) Note(v Verdict) {
	s.Found++
	switch {
	case v.Why == RejectLanguage:
		s.addReject("language:" + v.Language)
	case v.Why != "":
		s.addReject(v.Why)
	}
}

func (s *SearchSummary) addReject(key string) {
	if s.Rejected == nil {
		s.Rejected = map[string]int{}
	}
	s.Rejected[key]++
}

// Named reports whether any release was of this series at all.
func (s SearchSummary) Named() bool {
	return s.Found > s.Rejected[RejectSeries]+s.Rejected[RejectFormat]+s.Rejected[RejectCategory]
}

// SearchEntry is a search log row.
type SearchEntry struct {
	SeriesID   int64         `json:"-"`
	VolumeID   int64         `json:"volumeId,omitempty"`
	SearchedAt string        `json:"searchedAt"`
	Outcome    string        `json:"outcome"`
	Summary    SearchSummary `json:"summary"`
	NextAt     string        `json:"nextAt,omitempty"`
	Empties    int           `json:"empties,omitempty"`
}

// BackoffAfter is how long to wait after so many searches in a row found
// nothing: a day, three days, a week, then a month.
func BackoffAfter(empties int) time.Duration {
	switch {
	case empties <= 1:
		return 24 * time.Hour
	case empties == 2:
		return 3 * 24 * time.Hour
	case empties == 3:
		return 7 * 24 * time.Hour
	}
	return 30 * 24 * time.Hour
}

// LogSearch keeps the last search of a volume (or the series, with volumeID 0),
// and says when to look again.
func (r *Repo) LogSearch(seriesID, volumeID int64, outcome string, sum SearchSummary, now time.Time) error {
	var empties int
	_ = r.db.QueryRow(`SELECT empties FROM manga_search_log WHERE series_id = ? AND volume_id = ?`, seriesID, volumeID).Scan(&empties)
	wait := 24 * time.Hour
	switch outcome {
	case "unasked": // no indexer could be asked: it says nothing of the series
		wait = 6 * time.Hour
	case "nothing":
		empties++
		wait = BackoffAfter(empties)
	default:
		empties = 0
	}
	d, _ := json.Marshal(sum)
	_, err := r.db.Exec(`INSERT INTO manga_search_log (series_id, volume_id, searched_at, outcome, details, next_at, empties) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (series_id, volume_id) DO UPDATE SET searched_at = excluded.searched_at, outcome = excluded.outcome, details = excluded.details,
			next_at = excluded.next_at, empties = excluded.empties`,
		seriesID, volumeID, now.UTC().Format(time.RFC3339), outcome, string(d), now.Add(wait).UTC().Format(time.RFC3339), empties)
	return err
}

// ResetBackoff makes a series due now: something changed, or a person asked.
func (r *Repo) ResetBackoff(seriesID int64) error {
	_, err := r.db.Exec(`UPDATE manga_search_log SET next_at = '', empties = 0 WHERE series_id = ?`, seriesID)
	return err
}

// SearchLog is a series' search log, newest first.
func (r *Repo) SearchLog(seriesID int64) ([]SearchEntry, error) {
	rows, err := r.db.Query(`SELECT series_id, volume_id, searched_at, outcome, details, next_at, empties FROM manga_search_log WHERE series_id = ?`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SearchEntry{}
	for rows.Next() {
		var e SearchEntry
		var details string
		if err := rows.Scan(&e.SeriesID, &e.VolumeID, &e.SearchedAt, &e.Outcome, &details, &e.NextAt, &e.Empties); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(details), &e.Summary)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SearchedAt > out[j].SearchedAt })
	return out, rows.Err()
}

// DueAt is when a series or volume may be searched again: zero when it's due
// now (never searched, or reset).
func DueAt(log []SearchEntry, volumeID int64) time.Time {
	for _, e := range log {
		if e.VolumeID == volumeID {
			t, err := time.Parse(time.RFC3339, e.NextAt)
			if err == nil {
				return t
			}
			return time.Time{}
		}
	}
	return time.Time{}
}
