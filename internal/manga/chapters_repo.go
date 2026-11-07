package manga

// The chapter tables: what a series' link to MangaDex found, its chapters, and who
// has read what. See db/migrations/9185_home_manga_chapters.sql.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Follow says whether a series follows chapters, and which.
const (
	FollowOff = ""
	FollowNew = "new" // chapters from now on
	FollowAll = "all" // every chapter
)

// DexStats is what the last look at a series' chapters found, for the plain
// sentence that says why nothing is coming.
type DexStats struct {
	Listed       int      `json:"listed"`      // chapter numbers in this language
	Readable     int      `json:"readable"`    // of which can be downloaded
	External     int      `json:"external"`    // hosted elsewhere only
	Unavailable  int      `json:"unavailable"` // taken down
	Languages    []string `json:"languages,omitempty"`
	LicensedLink string   `json:"licensedLink,omitempty"`
}

// DexCandidate is an entry a person can choose when the match isn't sure.
type DexCandidate struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
}

// DexState is a series' link to MangaDex.
type DexState struct {
	SeriesID    int64          `json:"-"`
	DexID       string         `json:"dexId,omitempty"`
	Link        string         `json:"link"`
	LinkHow     string         `json:"linkHow,omitempty"`
	Candidates  []DexCandidate `json:"candidates,omitempty"`
	Follow      string         `json:"follow"`
	PreferGroup string         `json:"preferGroup,omitempty"`
	CheckedAt   string         `json:"checkedAt,omitempty"`
	NextAt      string         `json:"nextAt,omitempty"`
	Empties     int            `json:"empties,omitempty"`
	Trouble     string         `json:"trouble,omitempty"`
	Stats       DexStats       `json:"stats"`
}

func scanDexState(r rowScanner) (DexState, error) {
	var d DexState
	var cands, stats string
	err := r.Scan(&d.SeriesID, &d.DexID, &d.Link, &d.LinkHow, &cands, &d.Follow, &d.PreferGroup, &d.CheckedAt, &d.NextAt, &d.Empties, &d.Trouble, &stats)
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal([]byte(cands), &d.Candidates)
	_ = json.Unmarshal([]byte(stats), &d.Stats)
	return d, nil
}

const dexStateCols = `series_id, dex_id, link, link_how, candidates, follow, prefer_group, checked_at, next_at, empties, trouble, stats`

// DexStateOf is a series' link, or an empty one.
func (r *Repo) DexStateOf(seriesID int64) DexState {
	d, err := scanDexState(r.db.QueryRow(`SELECT `+dexStateCols+` FROM manga_dex_series WHERE series_id = ?`, seriesID))
	if err != nil {
		return DexState{SeriesID: seriesID}
	}
	return d
}

// SaveDexState keeps a series' link.
func (r *Repo) SaveDexState(d DexState) error {
	cands, _ := json.Marshal(d.Candidates)
	stats, _ := json.Marshal(d.Stats)
	_, err := r.db.Exec(`INSERT INTO manga_dex_series (`+dexStateCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (series_id) DO UPDATE SET dex_id = excluded.dex_id, link = excluded.link, link_how = excluded.link_how, candidates = excluded.candidates,
			follow = excluded.follow, prefer_group = excluded.prefer_group, checked_at = excluded.checked_at, next_at = excluded.next_at,
			empties = excluded.empties, trouble = excluded.trouble, stats = excluded.stats`,
		d.SeriesID, d.DexID, d.Link, d.LinkHow, string(cands), d.Follow, d.PreferGroup, d.CheckedAt, d.NextAt, d.Empties, d.Trouble, string(stats))
	return err
}

// DexDue are the followed series due a look, the longest waiting first.
func (r *Repo) DexDue(now time.Time, limit int) ([]DexState, error) {
	rows, err := r.db.Query(`SELECT `+dexStateCols+` FROM manga_dex_series WHERE follow != '' AND link != 'unsure' AND next_at <= ? ORDER BY next_at LIMIT ?`, now.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DexState
	for rows.Next() {
		d, err := scanDexState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Chapter is one chapter of a series. State is what the page shows.
type Chapter struct {
	ID          int64   `json:"id"`
	SeriesID    int64   `json:"seriesId"`
	Number      string  `json:"number"`
	SortKey     float64 `json:"-"`
	Volume      string  `json:"volume,omitempty"`
	Title       string  `json:"title,omitempty"`
	GroupKey    string  `json:"-"`
	Group       string  `json:"group,omitempty"`
	Language    string  `json:"language,omitempty"`
	Source      string  `json:"source"`
	DexID       string  `json:"-"`
	Pages       int     `json:"pages,omitempty"`
	ExternalURL string  `json:"externalUrl,omitempty"`
	Unavailable bool    `json:"unavailable,omitempty"`
	PublishedAt string  `json:"publishedAt,omitempty"`
	Wanted      bool    `json:"wanted"`
	Status      string  `json:"status"`
	State       string  `json:"state"` // downloaded, downloading, in_volume, external, unavailable, missing
	FilePath    string  `json:"filePath,omitempty"`
	FileSize    int64   `json:"fileSize,omitempty"`
	SetAside    bool    `json:"setAside,omitempty"`
	QueueID     int64   `json:"-"`
	Tries       int     `json:"-"`
	Problem     string  `json:"problem,omitempty"`
	InVolumeID  int64   `json:"inVolumeId,omitempty"` // a volume that's here holds it
	InVolume    string  `json:"inVolume,omitempty"`
	Copies      int     `json:"copies,omitempty"`
	// This person, filled by FillChapterReading.
	Read     bool `json:"read,omitempty"`
	Page     int  `json:"page,omitempty"`
	PagesFor int  `json:"pagesFor,omitempty"` // of how many, as the reader last said
}

const chapterCols = `c.id, c.series_id, c.number, c.sort_key, c.volume, c.title, c.group_key, c.group_name, c.language, c.source, c.dex_id, c.pages, c.external_url, c.unavailable, c.published_at,
	c.wanted, c.status, c.file_path, c.file_size, c.set_aside, c.queue_id, c.tries, c.problem,
	COALESCE((SELECT v.id FROM manga_volumes v WHERE v.series_id = c.series_id AND c.volume != '' AND v.number = c.volume AND v.status = 'downloaded'), 0),
	(SELECT COUNT(*) FROM download_queue q WHERE q.id = c.queue_id AND q.status IN ` + activeQueue + `),
	(SELECT COUNT(*) FROM manga_chapter_copies k WHERE k.chapter_id = c.id)`

func scanChapter(r rowScanner) (Chapter, error) {
	var c Chapter
	var active int
	err := r.Scan(&c.ID, &c.SeriesID, &c.Number, &c.SortKey, &c.Volume, &c.Title, &c.GroupKey, &c.Group, &c.Language, &c.Source, &c.DexID, &c.Pages, &c.ExternalURL, &c.Unavailable, &c.PublishedAt,
		&c.Wanted, &c.Status, &c.FilePath, &c.FileSize, &c.SetAside, &c.QueueID, &c.Tries, &c.Problem, &c.InVolumeID, &active, &c.Copies)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if c.InVolumeID > 0 {
		c.InVolume = c.Volume
	}
	switch {
	case c.InVolumeID > 0:
		c.State = "in_volume"
	case c.Status == StatusDownloaded:
		c.State = StatusDownloaded
	case active > 0:
		c.State = "downloading"
	case c.ExternalURL != "":
		c.State = "external"
	case c.Unavailable:
		c.State = "unavailable"
	default:
		c.State = StatusMissing
	}
	return c, nil
}

// Chapters are a series' chapters, in order.
func (r *Repo) Chapters(seriesID int64) ([]Chapter, error) {
	rows, err := r.db.Query(`SELECT `+chapterCols+` FROM manga_chapters c WHERE c.series_id = ? ORDER BY c.sort_key, c.number`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chapter{}
	for rows.Next() {
		c, err := scanChapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetChapter reads one chapter.
func (r *Repo) GetChapter(id int64) (Chapter, error) {
	return scanChapter(r.db.QueryRow(`SELECT `+chapterCols+` FROM manga_chapters c WHERE c.id = ?`, id))
}

// ChapterSort is a chapter number's place in order: a one-shot goes first.
func ChapterSort(number string) float64 {
	if number == OneshotNumber {
		return -1
	}
	return SortKey(number)
}

// SaveFeedResult is what a look at the feed changed.
type SaveFeedResult struct {
	New   int
	Stats DexStats
}

// SaveFeed brings a series' chapters up to date with the feed: one row a number,
// with the chosen upload, and every upload kept for the choice. A chapter whose file
// is here keeps its group and upload, whatever turns up later. wantNew says whether
// chapters new to the list are wanted.
func (r *Repo) SaveFeed(seriesID int64, feed []DexChapter, prefer, language string, wantNew bool) (SaveFeedResult, error) {
	var res SaveFeedResult
	byNumber := map[string][]ChapterCopy{}
	var order []string
	counts := map[string]int{}
	countedIn := map[string]bool{}
	for _, d := range feed {
		number := OneshotNumber
		if d.Number != "" {
			number = NormalNumber(d.Number)
		}
		if _, ok := byNumber[number]; !ok {
			order = append(order, number)
		}
		c := CopyOf(d)
		if c.Volume != "" {
			c.Volume = NormalNumber(c.Volume)
		}
		byNumber[number] = append(byNumber[number], c)
		if k := number + "|" + c.GroupKey; c.Readable() && !countedIn[k] {
			countedIn[k] = true
			counts[c.GroupKey]++
		}
	}
	tx, err := r.db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	seen := map[string]bool{}
	for _, number := range order {
		seen[number] = true
		copies := byNumber[number]
		best := ChooseCopy(copies, prefer, counts)
		volume := best.Volume
		for _, c := range copies { // any upload that knows its volume will do
			if volume == "" {
				volume = c.Volume
			}
		}
		res.Stats.Listed++
		switch {
		case best.Readable():
			res.Stats.Readable++
		case best.ExternalURL != "":
			res.Stats.External++
		default:
			res.Stats.Unavailable++
		}
		var id int64
		var status string
		err := tx.QueryRow(`SELECT id, status FROM manga_chapters WHERE series_id = ? AND number = ?`, seriesID, number).Scan(&id, &status)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			wanted := wantNew && best.Readable()
			ins, err := tx.Exec(`INSERT INTO manga_chapters (series_id, number, sort_key, volume, title, group_key, group_name, language, source, dex_id, pages, external_url, unavailable, published_at, wanted)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'mangadex', ?, ?, ?, ?, ?, ?)`,
				seriesID, number, ChapterSort(number), volume, best.Title, best.GroupKey, best.GroupName, orDefault(best.Language, language), best.DexID, best.Pages, best.ExternalURL, best.Unavailable, best.PublishedAt, wanted)
			if err != nil {
				return res, err
			}
			id, _ = ins.LastInsertId()
			res.New++
		case err != nil:
			return res, err
		case status == StatusDownloaded:
			// The file is here and stays. Only what's said about the chapter changes.
			gone := true
			for _, c := range copies {
				gone = gone && !c.Readable()
			}
			if _, err := tx.Exec(`UPDATE manga_chapters SET volume = ?, unavailable = ? WHERE id = ?`, volume, gone, id); err != nil {
				return res, err
			}
		default:
			if _, err := tx.Exec(`UPDATE manga_chapters SET volume = ?, title = ?, group_key = ?, group_name = ?, language = ?, source = 'mangadex', dex_id = ?, pages = ?, external_url = ?, unavailable = ?, published_at = ?,
					wanted = CASE WHEN ? THEN wanted ELSE 0 END WHERE id = ?`,
				volume, best.Title, best.GroupKey, best.GroupName, orDefault(best.Language, language), best.DexID, best.Pages, best.ExternalURL, best.Unavailable, best.PublishedAt, best.Readable(), id); err != nil {
				return res, err
			}
		}
		if _, err := tx.Exec(`DELETE FROM manga_chapter_copies WHERE chapter_id = ?`, id); err != nil {
			return res, err
		}
		for _, c := range copies {
			if _, err := tx.Exec(`INSERT INTO manga_chapter_copies (chapter_id, dex_id, group_key, group_name, pages, external_url, unavailable, published_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				id, c.DexID, c.GroupKey, c.GroupName, c.Pages, c.ExternalURL, c.Unavailable, c.PublishedAt); err != nil {
				return res, err
			}
		}
	}
	// What's gone from the list without a file here goes too. An empty list can be a
	// takedown or a passing fault, so those rows stay, said to be taken down.
	rows, err := tx.Query(`SELECT id, number FROM manga_chapters WHERE series_id = ? AND source = 'mangadex' AND status != 'downloaded'`, seriesID)
	if err != nil {
		return res, err
	}
	var goneIDs []int64
	for rows.Next() {
		var id int64
		var number string
		if err := rows.Scan(&id, &number); err != nil {
			rows.Close()
			return res, err
		}
		if !seen[number] {
			goneIDs = append(goneIDs, id)
		}
	}
	rows.Close()
	for _, id := range goneIDs {
		q := `DELETE FROM manga_chapters WHERE id = ?`
		if len(feed) == 0 {
			q = `UPDATE manga_chapters SET unavailable = 1, wanted = 0 WHERE id = ?`
		}
		if _, err := tx.Exec(q, id); err != nil {
			return res, err
		}
	}
	return res, tx.Commit()
}

// ChapterGroup is a group with chapters of a series, for choosing one.
type ChapterGroup struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Chapters int    `json:"chapters"`
}

// ChapterGroups are the groups with chapters that can be downloaded, most first.
func (r *Repo) ChapterGroups(seriesID int64) ([]ChapterGroup, error) {
	rows, err := r.db.Query(`SELECT k.group_key, MAX(k.group_name), COUNT(DISTINCT k.chapter_id) FROM manga_chapter_copies k JOIN manga_chapters c ON c.id = k.chapter_id
		WHERE c.series_id = ? AND k.group_key != '' AND k.external_url = '' AND k.unavailable = 0 AND k.pages > 0 GROUP BY k.group_key ORDER BY 3 DESC, 2`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChapterGroup{}
	for rows.Next() {
		var g ChapterGroup
		if err := rows.Scan(&g.Key, &g.Name, &g.Chapters); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RechooseCopies applies a newly chosen group to chapters not yet here. No file is
// fetched again for it.
func (r *Repo) RechooseCopies(seriesID int64, prefer string) error {
	if prefer == "" {
		return nil
	}
	_, err := r.db.Exec(`UPDATE manga_chapters SET
			dex_id = (SELECT k.dex_id FROM manga_chapter_copies k WHERE k.chapter_id = manga_chapters.id AND k.group_key = ? AND k.external_url = '' AND k.unavailable = 0 AND k.pages > 0 ORDER BY k.pages DESC LIMIT 1),
			group_name = (SELECT k.group_name FROM manga_chapter_copies k WHERE k.chapter_id = manga_chapters.id AND k.group_key = ? AND k.external_url = '' AND k.unavailable = 0 AND k.pages > 0 ORDER BY k.pages DESC LIMIT 1),
			pages = (SELECT k.pages FROM manga_chapter_copies k WHERE k.chapter_id = manga_chapters.id AND k.group_key = ? AND k.external_url = '' AND k.unavailable = 0 AND k.pages > 0 ORDER BY k.pages DESC LIMIT 1),
			group_key = ?
		WHERE series_id = ? AND status != 'downloaded' AND EXISTS (SELECT 1 FROM manga_chapter_copies k WHERE k.chapter_id = manga_chapters.id AND k.group_key = ? AND k.external_url = '' AND k.unavailable = 0 AND k.pages > 0)`,
		prefer, prefer, prefer, prefer, seriesID, prefer)
	return err
}

// EnsureChapter is a series' chapter with this number, added when it isn't there.
func (r *Repo) EnsureChapter(seriesID int64, number, source string) (Chapter, error) {
	number = NormalNumber(number)
	if _, err := r.db.Exec(`INSERT INTO manga_chapters (series_id, number, sort_key, source) VALUES (?, ?, ?, ?) ON CONFLICT (series_id, number) DO NOTHING`, seriesID, number, ChapterSort(number), source); err != nil {
		return Chapter{}, err
	}
	return scanChapter(r.db.QueryRow(`SELECT `+chapterCols+` FROM manga_chapters c WHERE c.series_id = ? AND c.number = ?`, seriesID, number))
}

// SetChapterFile records a chapter's file.
func (r *Repo) SetChapterFile(id int64, path string, size int64) error {
	_, err := r.db.Exec(`UPDATE manga_chapters SET status = 'downloaded', file_path = ?, file_size = ?, tries = 0, next_try = '', problem = '', set_aside = 0 WHERE id = ?`, path, size, id)
	return err
}

// SetChapterAside notes that a chapter's file was moved aside, or put back.
func (r *Repo) SetChapterAside(id int64, path string, aside bool) error {
	_, err := r.db.Exec(`UPDATE manga_chapters SET file_path = ?, set_aside = ? WHERE id = ?`, path, aside, id)
	return err
}

// ClearChapterFile forgets a file that has gone, and it isn't wanted after that,
// as with volumes: a drive that's away can't start a round of downloads.
func (r *Repo) ClearChapterFile(id int64) error {
	_, err := r.db.Exec(`UPDATE manga_chapters SET status = 'missing', file_path = '', file_size = 0, set_aside = 0, wanted = 0 WHERE id = ?`, id)
	return err
}

// SetChaptersWanted changes whether chapters are wanted.
func (r *Repo) SetChaptersWanted(ids []int64, wanted bool) (int, error) {
	n := 0
	for _, id := range ids {
		res, err := r.db.Exec(`UPDATE manga_chapters SET wanted = ?, tries = 0, next_try = '', problem = '' WHERE id = ? AND status != 'downloaded'`, wanted, id)
		if err != nil {
			return n, err
		}
		c, _ := res.RowsAffected()
		n += int(c)
	}
	return n, nil
}

// WantAllChapters makes every chapter that can be downloaded wanted.
func (r *Repo) WantAllChapters(seriesID int64) error {
	_, err := r.db.Exec(`UPDATE manga_chapters SET wanted = 1 WHERE series_id = ? AND status != 'downloaded' AND external_url = '' AND unavailable = 0 AND dex_id != ''`, seriesID)
	return err
}

const chapterToFetch = `c.wanted = 1 AND c.status != 'downloaded' AND c.external_url = '' AND c.unavailable = 0 AND c.dex_id != ''
	AND NOT EXISTS (SELECT 1 FROM manga_volumes v WHERE v.series_id = c.series_id AND c.volume != '' AND v.number = c.volume AND v.status = 'downloaded')
	AND NOT EXISTS (SELECT 1 FROM download_queue q WHERE q.id = c.queue_id AND q.status IN ` + activeQueue + `)`

// ChaptersToFetch are a series' chapters waiting for a download, first ones first,
// and how many wait in all. One that failed a moment ago waits its turn.
func (r *Repo) ChaptersToFetch(seriesID int64, now time.Time, limit int) ([]Chapter, int, error) {
	at := now.UTC().Format(time.RFC3339)
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM manga_chapters c WHERE c.series_id = ? AND c.next_try <= ? AND `+chapterToFetch, seriesID, at).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(`SELECT `+chapterCols+` FROM manga_chapters c WHERE c.series_id = ? AND c.next_try <= ? AND `+chapterToFetch+` ORDER BY c.sort_key LIMIT ?`, seriesID, at, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Chapter
	for rows.Next() {
		c, err := scanChapter(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// AssignBatch gives chapters to a queue item.
func (r *Repo) AssignBatch(queueID int64, ids []int64) error {
	for _, id := range ids {
		if _, err := r.db.Exec(`UPDATE manga_chapters SET queue_id = ? WHERE id = ?`, queueID, id); err != nil {
			return err
		}
	}
	return nil
}

// BatchChapters are the chapters of a queue item, in order.
func (r *Repo) BatchChapters(queueID int64) ([]Chapter, error) {
	rows, err := r.db.Query(`SELECT `+chapterCols+` FROM manga_chapters c WHERE c.queue_id = ? ORDER BY c.sort_key`, queueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chapter
	for rows.Next() {
		c, err := scanChapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MoveBatch hands a batch's chapters to its retry.
func (r *Repo) MoveBatch(from, to int64) error {
	_, err := r.db.Exec(`UPDATE manga_chapters SET queue_id = ?, tries = 0, next_try = '', problem = '' WHERE queue_id = ?`, to, from)
	return err
}

// ChapterTrouble notes a chapter that couldn't be fetched, and when to try again:
// after an hour, then later each time.
func (r *Repo) ChapterTrouble(id int64, problem string, now time.Time) error {
	var tries int
	_ = r.db.QueryRow(`SELECT tries FROM manga_chapters WHERE id = ?`, id).Scan(&tries)
	tries++
	wait := time.Hour << min(tries-1, 6)
	_, err := r.db.Exec(`UPDATE manga_chapters SET tries = ?, next_try = ?, problem = ? WHERE id = ?`, tries, now.Add(wait).UTC().Format(time.RFC3339), problem, id)
	return err
}

// MarkChapterGone notes that MangaDex says a chapter's pages aren't there.
func (r *Repo) MarkChapterGone(id int64) error {
	_, err := r.db.Exec(`UPDATE manga_chapters SET unavailable = 1, wanted = 0 WHERE id = ?`, id)
	return err
}

// ActiveDexBatches counts the MangaDex batches waiting or running.
func (r *Repo) ActiveDexBatches() (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM manga_downloads d JOIN download_queue q ON q.id = d.queue_id WHERE d.source = 'mangadex' AND q.status IN ` + activeQueue).Scan(&n)
	return n, err
}

// ActiveDexBatch reports whether a series has a batch waiting or running.
func (r *Repo) ActiveDexBatch(seriesID int64) (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM manga_downloads d JOIN download_queue q ON q.id = d.queue_id WHERE d.source = 'mangadex' AND d.series_id = ? AND q.status IN `+activeQueue, seriesID).Scan(&n)
	return n > 0, err
}

// ChapterFile is a chapter that has a file.
type ChapterFile struct {
	ID       int64
	SeriesID int64
	Path     string
}

// ChapterFiles are every chapter with a file, for seeing that they're still there.
func (r *Repo) ChapterFiles() ([]ChapterFile, error) {
	rows, err := r.db.Query(`SELECT id, series_id, file_path FROM manga_chapters WHERE status = 'downloaded' AND file_path != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChapterFile
	for rows.Next() {
		var f ChapterFile
		if err := rows.Scan(&f.ID, &f.SeriesID, &f.Path); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ----- reading -----

// ChapterItem is a chapter's key in home_positions.
func ChapterItem(id int64) string { return "manga:c:" + strconv.FormatInt(id, 10) }

// ChapterOfItem reads a chapter's id back from its key.
func ChapterOfItem(item string) (int64, bool) {
	rest, ok := strings.CutPrefix(item, "manga:c:")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	return id, err == nil && id > 0
}

// MarkChaptersRead marks chapters read or unread for someone. Either way their
// place in them is forgotten.
func (r *Repo) MarkChaptersRead(user int64, ids []int64, read bool, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if read {
			if _, err := tx.Exec(`INSERT INTO manga_chapter_read (user_id, chapter_id, read_ms) SELECT ?, id, ? FROM manga_chapters WHERE id = ?
				ON CONFLICT (user_id, chapter_id) DO UPDATE SET read_ms = excluded.read_ms`, user, now.UnixMilli(), id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(`DELETE FROM manga_chapter_read WHERE user_id = ? AND chapter_id = ?`, user, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM home_positions WHERE user_id = ? AND item = ?`, user, ChapterItem(id)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FillChapterReading says, on each chapter, what this person has read and where they
// are. A chapter in a volume they've read counts as read.
func (r *Repo) FillChapterReading(user, seriesID int64, list []Chapter) error {
	read := map[int64]bool{}
	rows, err := r.db.Query(`SELECT m.chapter_id FROM manga_chapter_read m JOIN manga_chapters c ON c.id = m.chapter_id WHERE m.user_id = ? AND c.series_id = ?`, user, seriesID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		read[id] = true
	}
	rows.Close()
	volRead := map[string]bool{}
	rows, err = r.db.Query(`SELECT v.number FROM manga_read m JOIN manga_volumes v ON v.id = m.volume_id WHERE m.user_id = ? AND v.series_id = ?`, user, seriesID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		volRead[n] = true
	}
	rows.Close()
	rows, err = r.db.Query(`SELECT p.item, p.position, p.duration FROM home_positions p JOIN manga_chapters c ON p.item = 'manga:c:' || c.id WHERE p.user_id = ? AND c.series_id = ?`, user, seriesID)
	if err != nil {
		return err
	}
	defer rows.Close()
	places := map[int64][2]float64{}
	for rows.Next() {
		var item string
		var pos, dur float64
		if err := rows.Scan(&item, &pos, &dur); err != nil {
			return err
		}
		if id, ok := ChapterOfItem(item); ok {
			places[id] = [2]float64{pos, dur}
		}
	}
	for i := range list {
		list[i].Read = read[list[i].ID] || (list[i].Volume != "" && volRead[list[i].Volume])
		if at, ok := places[list[i].ID]; ok {
			list[i].Page, list[i].PagesFor = int(at[0]), int(at[1])
		}
	}
	return rows.Err()
}

// ReadingOrderFor is a series' reading order, with this person's read marks.
func (r *Repo) ReadingOrderFor(user, seriesID int64) ([]ReadingItem, error) {
	volumes, err := r.Volumes(seriesID)
	if err != nil {
		return nil, err
	}
	if err := r.FillReading(user, seriesID, volumes); err != nil {
		return nil, err
	}
	chapters, err := r.Chapters(seriesID)
	if err != nil {
		return nil, err
	}
	if err := r.FillChapterReading(user, seriesID, chapters); err != nil {
		return nil, err
	}
	return ReadingOrder(volumes, chapters), nil
}

// CarryToVolume moves what people read in chapters over to a volume that has
// arrived holding them. Everyone who read all of its chapters has read the volume.
// Someone part way through keeps their place, counted in pages: approximate, as a
// volume has a cover and extras that chapters don't. A place or a read mark they
// already have in the volume is left alone.
func (r *Repo) CarryToVolume(volumeID int64) error {
	var seriesID int64
	var number string
	if err := r.db.QueryRow(`SELECT series_id, number FROM manga_volumes WHERE id = ?`, volumeID).Scan(&seriesID, &number); err != nil {
		return err
	}
	rows, err := r.db.Query(`SELECT id, pages FROM manga_chapters WHERE series_id = ? AND volume = ? ORDER BY sort_key`, seriesID, number)
	if err != nil {
		return err
	}
	type chap struct {
		id    int64
		pages int
	}
	var chapters []chap
	for rows.Next() {
		var c chap
		if err := rows.Scan(&c.id, &c.pages); err != nil {
			rows.Close()
			return err
		}
		chapters = append(chapters, c)
	}
	rows.Close()
	if len(chapters) == 0 {
		return nil
	}
	users := map[int64]bool{}
	rows, err = r.db.Query(`SELECT DISTINCT m.user_id FROM manga_chapter_read m JOIN manga_chapters c ON c.id = m.chapter_id WHERE c.series_id = ? AND c.volume = ?
		UNION SELECT DISTINCT p.user_id FROM home_positions p JOIN manga_chapters c ON p.item = 'manga:c:' || c.id WHERE c.series_id = ? AND c.volume = ?`, seriesID, number, seriesID, number)
	if err != nil {
		return err
	}
	for rows.Next() {
		var u int64
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return err
		}
		users[u] = true
	}
	rows.Close()
	now := time.Now()
	for user := range users {
		var already int
		_ = r.db.QueryRow(`SELECT COUNT(*) FROM manga_read WHERE user_id = ? AND volume_id = ?`, user, volumeID).Scan(&already)
		if already > 0 {
			continue
		}
		var hasPlace int
		_ = r.db.QueryRow(`SELECT COUNT(*) FROM home_positions WHERE user_id = ? AND item = ?`, user, PositionItem(volumeID)).Scan(&hasPlace)
		page, readAll := 0, true
		stopped := false
		for _, c := range chapters {
			if stopped {
				readAll = false
				continue
			}
			var isRead int
			_ = r.db.QueryRow(`SELECT COUNT(*) FROM manga_chapter_read WHERE user_id = ? AND chapter_id = ?`, user, c.id).Scan(&isRead)
			if isRead > 0 {
				page += c.pages
				continue
			}
			readAll, stopped = false, true
			var pos float64
			if r.db.QueryRow(`SELECT position FROM home_positions WHERE user_id = ? AND item = ?`, user, ChapterItem(c.id)).Scan(&pos) == nil {
				page += int(pos)
			}
		}
		switch {
		case readAll:
			if err := r.MarkRead(user, []int64{volumeID}, true, now); err != nil {
				return err
			}
		case hasPlace == 0 && page > 1:
			if _, err := r.db.Exec(`INSERT INTO home_positions (user_id, item, position, duration, updated_ms) VALUES (?, ?, ?, 0, ?) ON CONFLICT DO NOTHING`, user, PositionItem(volumeID), page, now.UnixMilli()); err != nil {
				return err
			}
		}
		for _, c := range chapters {
			if _, err := r.db.Exec(`DELETE FROM home_positions WHERE user_id = ? AND item = ?`, user, ChapterItem(c.id)); err != nil {
				return err
			}
		}
	}
	return nil
}
