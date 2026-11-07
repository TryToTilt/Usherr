package comics

// Usherr: reading comics, per person. Where someone is in an issue stays in
// home_positions, shared with the other readers; this adds what they finished
// (comic_read), what they want to read (comic_want_read) and how they like to
// read (comic_reader_prefs). Everything is keyed to the issue's row, so it
// survives a rename, a convert or a fixed ComicVine match. The Continue, Want
// to Read and Read views follow Pullbox's reading workspace, written anew here.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Series types, set by hand on a series.
const (
	TypeComic     = "comic"
	TypeManga     = "manga"
	TypeWebtoon   = "webtoon"
	TypeCollected = "collected" // its "issues" are TPBs or hardcovers
)

// ValidType reports whether t is a series type ("" clears it).
func ValidType(t string) bool {
	switch t {
	case "", TypeComic, TypeManga, TypeWebtoon, TypeCollected:
		return true
	}
	return false
}

// SetSeriesType records what kind of series it is.
func (r *Repo) SetSeriesType(id int64, t string) error {
	_, err := r.db.Exec(`UPDATE comic_series SET series_type = ? WHERE id = ?`, t, id)
	return err
}

// ReaderPrefs is how someone reads. Empty means not chosen.
type ReaderPrefs struct {
	Direction string `json:"direction"` // ltr or rtl
	Layout    string `json:"layout"`    // flip, slide, double, webtoon or panels
}

// ValidDirection reports whether d is a reading direction ("" leaves it unchosen).
func ValidDirection(d string) bool { return d == "" || d == "ltr" || d == "rtl" }

// ValidLayout reports whether l is a reader layout ("" leaves it unchosen).
func ValidLayout(l string) bool {
	switch l {
	case "", "flip", "slide", "double", "webtoon", "panels":
		return true
	}
	return false
}

// ResolveReader works out how a person reads an issue. Their choice for this
// series comes first. After that the direction comes from what is known about
// the comic: a file marked YesAndRightToLeft, a series set to manga, or (when
// neither the file nor the series says) a manga publisher. Then their default,
// then left to right. A file marked Yes or No stops the guess from the
// publisher: Manga "Yes" means manga printed left to right. fileManga is
// ComicInfo's Manga value.
func ResolveReader(s Series, fileManga string, own, def ReaderPrefs) ReaderPrefs {
	out := own
	if out.Direction == "" {
		fm := strings.ToLower(strings.TrimSpace(fileManga))
		switch {
		case fm == "yesandrighttoleft", s.Type == TypeManga:
			out.Direction = "rtl"
		case fm != "yes" && fm != "no" && s.Type == "" && PublisherManga(s):
			out.Direction = "rtl"
		case def.Direction != "":
			out.Direction = def.Direction
		default:
			out.Direction = "ltr"
		}
	}
	if out.Layout == "" {
		if s.Type == TypeWebtoon {
			out.Layout = "webtoon"
		} else {
			out.Layout = def.Layout
		}
	}
	return out
}

// ReaderPrefsFor returns someone's choices for a series (0: their default).
func (r *Repo) ReaderPrefsFor(user, series int64) ReaderPrefs {
	var p ReaderPrefs
	_ = r.db.QueryRow(`SELECT direction, layout FROM comic_reader_prefs WHERE user_id = ? AND series_id = ?`, user, series).Scan(&p.Direction, &p.Layout)
	return p
}

// SetReaderPrefs keeps what's chosen in p; an empty field stays as it was.
func (r *Repo) SetReaderPrefs(user, series int64, p ReaderPrefs) error {
	_, err := r.db.Exec(`INSERT INTO comic_reader_prefs (user_id, series_id, direction, layout) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, series_id) DO UPDATE SET
			direction = CASE WHEN excluded.direction != '' THEN excluded.direction ELSE direction END,
			layout = CASE WHEN excluded.layout != '' THEN excluded.layout ELSE layout END`,
		user, series, p.Direction, p.Layout)
	return err
}

// Which issues a "mark read" covers.
const (
	ScopeIssue  = "issue"
	ScopeUpTo   = "upto"
	ScopeSeries = "series"
)

// ScopeIssues returns the issues a mark covers. Marking read leaves out issues
// that aren't out yet, so a whole series read still has its next issue to come.
func (r *Repo) ScopeIssues(is Issue, scope string, read bool, now time.Time) ([]int64, error) {
	if scope == ScopeIssue {
		return []int64{is.ID}, nil
	}
	list, err := r.Issues(is.SeriesID)
	if err != nil {
		return nil, err
	}
	var out []int64
	for _, i := range list {
		if !read || i.Released(now) || i.ID == is.ID {
			out = append(out, i.ID)
		}
		if scope == ScopeUpTo && i.ID == is.ID {
			break
		}
	}
	return out, nil
}

// MarkRead marks issues read or unread for someone. Either way their place in
// the issue is forgotten: read or unread, it opens at the first page. An issue
// read comes off their Want to Read. Ids that no longer exist are skipped.
func (r *Repo) MarkRead(user int64, ids []int64, read bool, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if read {
			if _, err := tx.Exec(`INSERT INTO comic_read (user_id, issue_id, read_ms) SELECT ?, id, ? FROM comic_issues WHERE id = ?
				ON CONFLICT (user_id, issue_id) DO UPDATE SET read_ms = excluded.read_ms`, user, now.UnixMilli(), id); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM comic_want_read WHERE user_id = ? AND issue_id = ?`, user, id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(`DELETE FROM comic_read WHERE user_id = ? AND issue_id = ?`, user, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM home_positions WHERE user_id = ? AND item = ?`, user, fmt.Sprintf("comic:%d", id)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetWantRead puts a series (issue 0) or one issue on someone's Want to Read,
// or takes it off.
func (r *Repo) SetWantRead(user, series, issue int64, want bool, now time.Time) error {
	if !want {
		_, err := r.db.Exec(`DELETE FROM comic_want_read WHERE user_id = ? AND series_id = ? AND issue_id = ?`, user, series, issue)
		return err
	}
	_, err := r.db.Exec(`INSERT OR REPLACE INTO comic_want_read (user_id, series_id, issue_id, added_ms) VALUES (?, ?, ?, ?)`, user, series, issue, now.UnixMilli())
	return err
}

// idsOf runs a query for one id column.
func (r *Repo) idsOf(q string, args ...any) (map[int64]bool, error) {
	rows, err := r.db.Query(q, args...)
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

// FillReading sets one person's state on a series and its issues.
func (r *Repo) FillReading(user int64, s *Series, issues []Issue) error {
	read, err := r.idsOf(`SELECT c.issue_id FROM comic_read c JOIN comic_issues i ON i.id = c.issue_id WHERE c.user_id = ? AND i.series_id = ?`, user, s.ID)
	if err != nil {
		return err
	}
	wants, err := r.idsOf(`SELECT issue_id FROM comic_want_read WHERE user_id = ? AND series_id = ?`, user, s.ID)
	if err != nil {
		return err
	}
	places := map[int64][2]int{}
	rows, err := r.db.Query(`SELECT i.id, p.position, p.duration FROM home_positions p JOIN comic_issues i ON p.item = 'comic:' || i.id WHERE p.user_id = ? AND i.series_id = ?`, user, s.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var pos, dur float64
		if err := rows.Scan(&id, &pos, &dur); err != nil {
			return err
		}
		places[id] = [2]int{int(pos), int(dur)}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.WantRead = wants[0]
	for k := range issues {
		i := &issues[k]
		i.Read = read[i.ID]
		i.WantRead = wants[i.ID]
		if at, ok := places[i.ID]; ok {
			i.Page, i.Pages = at[0], at[1]
		}
	}
	return nil
}

// ReadingItem is one entry in someone's reading views. IssueID 0 is a whole
// series on Want to Read.
type ReadingItem struct {
	SeriesID int64  `json:"seriesId"`
	IssueID  int64  `json:"issueId,omitempty"`
	Series   string `json:"series"`
	Number   string `json:"number,omitempty"`
	Title    string `json:"title,omitempty"`
	ImageURL string `json:"imageUrl"`
	Status   string `json:"status,omitempty"`
	Page     int    `json:"page,omitempty"`  // the page they left it at
	Pages    int    `json:"pages,omitempty"` // of how many
	Next     bool   `json:"next,omitempty"`  // Continue: the next issue after one they read
	At       int64  `json:"at"`              // when, in milliseconds
}

// ReadingViews is someone's Continue, Want to Read and Read lists.
type ReadingViews struct {
	Continue []ReadingItem `json:"continue"`
	Want     []ReadingItem `json:"want"`
	Read     []ReadingItem `json:"read"`
}

const (
	continueMax = 40
	readViewMax = 500
)

const readingItemCols = `i.series_id, i.id, s.name, i.number, i.title,
	CASE WHEN i.image_url != '' THEN i.image_url ELSE s.image_url END, i.status`

func (r *Repo) readingItems(ctx context.Context, q string, args ...any) ([]ReadingItem, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReadingItem{}
	for rows.Next() {
		var it ReadingItem
		var pos, dur float64
		if err := rows.Scan(&it.SeriesID, &it.IssueID, &it.Series, &it.Number, &it.Title, &it.ImageURL, &it.Status, &pos, &dur, &it.At); err != nil {
			return nil, err
		}
		it.Page, it.Pages = int(pos), int(dur)
		out = append(out, it)
	}
	return out, rows.Err()
}

// Reading returns someone's reading views, newest first.
func (r *Repo) Reading(ctx context.Context, user int64) (ReadingViews, error) {
	var v ReadingViews
	var err error
	if v.Continue, err = r.continueReading(ctx, user); err != nil {
		return v, err
	}
	if v.Read, err = r.readingItems(ctx, `SELECT `+readingItemCols+`, 0, 0, c.read_ms FROM comic_read c
		JOIN comic_issues i ON i.id = c.issue_id JOIN comic_series s ON s.id = i.series_id
		WHERE c.user_id = ? ORDER BY c.read_ms DESC, s.name COLLATE NOCASE, i.sort_key DESC LIMIT ?`, user, readViewMax); err != nil {
		return v, err
	}
	if v.Want, err = r.readingItems(ctx, `SELECT `+readingItemCols+`, 0, 0, w.added_ms FROM comic_want_read w
		JOIN comic_issues i ON i.id = w.issue_id JOIN comic_series s ON s.id = i.series_id
		WHERE w.user_id = ? ORDER BY w.added_ms DESC`, user); err != nil {
		return v, err
	}
	// A whole series stays until nothing in it is left unread.
	rows, err := r.db.QueryContext(ctx, `SELECT s.id, s.name, s.image_url, w.added_ms FROM comic_want_read w
		JOIN comic_series s ON s.id = w.series_id
		WHERE w.user_id = ? AND w.issue_id = 0 AND (
			NOT EXISTS (SELECT 1 FROM comic_issues i WHERE i.series_id = s.id)
			OR EXISTS (SELECT 1 FROM comic_issues i WHERE i.series_id = s.id
				AND NOT EXISTS (SELECT 1 FROM comic_read c WHERE c.user_id = w.user_id AND c.issue_id = i.id)))`, user)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var it ReadingItem
		if err := rows.Scan(&it.SeriesID, &it.Series, &it.ImageURL, &it.At); err != nil {
			return v, err
		}
		v.Want = append(v.Want, it)
	}
	if err := rows.Err(); err != nil {
		return v, err
	}
	sort.SliceStable(v.Want, func(i, j int) bool { return v.Want[i].At > v.Want[j].At })
	return v, nil
}

// continueReading is what someone is part-way through, newest first, then the
// next issue they have of a series they've been reading.
func (r *Repo) continueReading(ctx context.Context, user int64) ([]ReadingItem, error) {
	out, err := r.readingItems(ctx, `SELECT `+readingItemCols+`, p.position, p.duration, p.updated_ms FROM home_positions p
		JOIN comic_issues i ON p.item = 'comic:' || i.id JOIN comic_series s ON s.id = i.series_id
		WHERE p.user_id = ? AND i.status = 'downloaded' ORDER BY p.updated_ms DESC LIMIT ?`, user, continueMax)
	if err != nil {
		return nil, err
	}
	busy := map[int64]bool{}
	for _, it := range out {
		busy[it.SeriesID] = true
	}
	// Series read from, newest first, and each one's read issues.
	type recent struct {
		series int64
		at     int64
	}
	var order []recent
	read := map[int64]bool{}
	seen := map[int64]bool{}
	rows, err := r.db.QueryContext(ctx, `SELECT i.series_id, i.id, c.read_ms FROM comic_read c JOIN comic_issues i ON i.id = c.issue_id
		WHERE c.user_id = ? ORDER BY c.read_ms DESC`, user)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var series, id, at int64
		if err := rows.Scan(&series, &id, &at); err != nil {
			rows.Close()
			return nil, err
		}
		read[id] = true
		if !seen[series] {
			seen[series] = true
			order = append(order, recent{series, at})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, o := range order {
		if len(out) >= continueMax {
			break
		}
		if busy[o.series] {
			continue
		}
		s, err := r.GetSeries(o.series)
		if err != nil {
			continue
		}
		list, err := r.Issues(o.series)
		if err != nil {
			return nil, err
		}
		if next, ok := nextToRead(list, read); ok {
			img := next.ImageURL
			if img == "" {
				img = s.ImageURL
			}
			out = append(out, ReadingItem{SeriesID: s.ID, IssueID: next.ID, Series: s.Name, Number: next.Number, Title: next.Title, ImageURL: img, Status: next.Status, Next: true, At: o.at})
		}
	}
	return out, nil
}

// nextToRead is the first unread issue with a file after the last one read.
// Unread issues before that were skipped on purpose.
func nextToRead(list []Issue, read map[int64]bool) (Issue, bool) {
	last := -1
	for k, i := range list {
		if read[i.ID] {
			last = k
		}
	}
	if last < 0 {
		return Issue{}, false
	}
	for _, i := range list[last+1:] {
		if i.Status == StatusDownloaded && !read[i.ID] {
			return i, true
		}
	}
	return Issue{}, false
}

// NextIssues returns what comes after an issue: the very next one, and the next
// one that has a file (which may be the same).
func NextIssues(list []Issue, id int64) (next, have *Issue) {
	for k := range list {
		if list[k].ID != id {
			continue
		}
		for j := k + 1; j < len(list); j++ {
			if next == nil {
				next = &list[j]
			}
			if list[j].Status == StatusDownloaded {
				have = &list[j]
				break
			}
		}
		break
	}
	return next, have
}

// ---- fixing a wrong ComicVine match ----

// RepointPlan is what happens to a series' issues when it is pointed at another
// ComicVine volume. Issues are paired by number and keep their row, so files,
// read state, places, bookmarks and download links stay with them.
type RepointPlan struct {
	Matched []RepointPair // an issue here takes the new volume's issue of that number
	New     []Issue       // the new volume's issues with no row here
	Kept    []Issue       // no such number there, but a file or a download here: stays
	Dropped []Issue       // no such number there and nothing here: goes
}

// RepointPair pairs an issue here with the new volume's.
type RepointPair struct {
	Here Issue
	CV   Issue
}

// PlanRepoint pairs a series' issues with another volume's. Where two rows have
// the same number, the one with a file is paired first.
func PlanRepoint(here, incoming []Issue) RepointPlan {
	byFile := append([]Issue(nil), here...)
	sort.SliceStable(byFile, func(i, j int) bool { return byFile[i].Status != StatusMissing && byFile[j].Status == StatusMissing })
	free := map[string][]Issue{}
	for _, i := range byFile {
		k := NormalNumber(i.Number)
		free[k] = append(free[k], i)
	}
	var p RepointPlan
	for _, cv := range incoming {
		k := NormalNumber(cv.Number)
		if rows := free[k]; len(rows) > 0 {
			p.Matched = append(p.Matched, RepointPair{Here: rows[0], CV: cv})
			free[k] = rows[1:]
			continue
		}
		p.New = append(p.New, cv)
	}
	for _, i := range byFile {
		rows := free[NormalNumber(i.Number)]
		if len(rows) == 0 || rows[0].ID != i.ID {
			continue
		}
		free[NormalNumber(i.Number)] = rows[1:]
		if i.Status == StatusMissing {
			p.Dropped = append(p.Dropped, i)
		} else {
			p.Kept = append(p.Kept, i)
		}
	}
	return p
}

// ErrIssueElsewhere means an issue of the chosen volume already belongs to
// another series here.
var ErrIssueElsewhere = errors.New("an issue of that volume already belongs to another series in your library")

// Repoint points a series at another ComicVine volume, all or nothing. Series
// details and matched issues take ComicVine's; the series' folder, its type
// and every file stay as they are. Issues kept with no match lose their
// ComicVine id (a negative stand-in, which nothing looks up). New issues are
// wanted only when the series gets new issues and they aren't out yet.
func (r *Repo) Repoint(s, info Series, plan RepointPlan, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Stand-ins first, so nothing collides as the real ids go in.
	if _, err := tx.Exec(`UPDATE comic_issues SET cv_id = -id WHERE series_id = ?`, s.ID); err != nil {
		return err
	}
	all := make([]Issue, 0, len(plan.Matched)+len(plan.New))
	for _, m := range plan.Matched {
		all = append(all, m.CV)
	}
	all = append(all, plan.New...)
	for _, i := range all {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM comic_issues WHERE cv_id = ?`, i.CVID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrIssueElsewhere
		}
	}
	for _, d := range plan.Dropped {
		if _, err := tx.Exec(`DELETE FROM comic_issues WHERE id = ?`, d.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM comic_want_read WHERE issue_id = ?`, d.ID); err != nil {
			return err
		}
	}
	for _, m := range plan.Matched {
		if _, err := tx.Exec(`UPDATE comic_issues SET cv_id = ?, number = ?, sort_key = ?, title = ?, cover_date = ?, store_date = ?, image_url = ? WHERE id = ?`,
			m.CV.CVID, m.CV.Number, SortKey(m.CV.Number), m.CV.Title, m.CV.CoverDate, m.CV.StoreDate, m.CV.ImageURL, m.Here.ID); err != nil {
			return err
		}
	}
	for _, i := range plan.New {
		if _, err := tx.Exec(`INSERT INTO comic_issues (series_id, cv_id, number, sort_key, title, cover_date, store_date, image_url, wanted) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.ID, i.CVID, i.Number, SortKey(i.Number), i.Title, i.CoverDate, i.StoreDate, i.ImageURL, b2i(s.MonitorNew && !i.Released(now))); err != nil {
			return err
		}
	}
	// What a search found for the old volume no longer means anything.
	for _, q := range []string{
		`DELETE FROM comic_reviews WHERE issue_id IN (SELECT id FROM comic_issues WHERE series_id = ?)`,
		`DELETE FROM comic_search_log WHERE issue_id IN (SELECT id FROM comic_issues WHERE series_id = ?)`,
	} {
		if _, err := tx.Exec(q, s.ID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE comic_series SET cv_id = ?, name = ?, start_year = ?, publisher = ?, issue_count = ?, description = ?, image_url = ?, checked_at = ? WHERE id = ?`,
		info.CVID, info.Name, info.StartYear, info.Publisher, info.IssueCount, info.Description, info.ImageURL, now.UTC().Format(time.RFC3339), s.ID); err != nil {
		return err
	}
	return tx.Commit()
}
