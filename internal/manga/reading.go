package manga

// Reading manga, per person. Where someone is in a volume stays in
// home_positions, shared with the other readers; this adds what they finished
// (manga_read) and how they like to read a series (manga_reader_prefs). All of it
// is keyed to the volume's row, so a rename or another copy of the file keeps it.

import (
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PositionItem is a volume's key in home_positions and the reading marks.
func PositionItem(volumeID int64) string { return "manga:v:" + strconv.FormatInt(volumeID, 10) }

// VolumeOfItem reads a volume's id back from its key.
func VolumeOfItem(item string) (int64, bool) {
	rest, ok := strings.CutPrefix(item, "manga:v:")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	return id, err == nil && id > 0
}

// ReaderPrefs is how someone reads a series. Empty means not chosen.
type ReaderPrefs struct {
	Direction string `json:"direction"` // ltr or rtl
	Layout    string `json:"layout"`    // single, double, webtoon, wide (one strip across) or panels
	Shift     int    `json:"shift"`     // two pages at a time: 1 pairs start a page later, -1 they don't, 0 not chosen
}

// ValidDirection reports whether d is a reading direction ("" leaves it unchosen).
func ValidDirection(d string) bool { return d == "" || d == "ltr" || d == "rtl" }

// ValidLayout reports whether l is a reader layout ("" leaves it unchosen: two
// pages on a wide screen, one on a narrow one).
func ValidLayout(l string) bool {
	return l == "" || l == "single" || l == "double" || l == "webtoon" || l == "wide" || l == "panels"
}

// ResolveReader works out how someone reads a volume. Their own choice comes
// first. Then the file: ComicInfo's Manga value says right to left
// (YesAndRightToLeft) or not (Yes, No). Then the format: manhwa and manhua are
// one long scroll, top to bottom, and everything else is right to left.
func ResolveReader(format, fileManga string, own ReaderPrefs) ReaderPrefs {
	out := own
	long := format == "manhwa" || format == "manhua"
	if out.Direction == "" {
		switch fm := strings.ToLower(strings.TrimSpace(fileManga)); {
		case fm == "yesandrighttoleft":
			out.Direction = "rtl"
		case fm == "yes" || fm == "no" || long:
			out.Direction = "ltr"
		default:
			out.Direction = "rtl"
		}
	}
	if out.Layout == "" && long {
		out.Layout = "webtoon"
	}
	return out
}

// ReaderPrefsFor is someone's choices for a series.
func (r *Repo) ReaderPrefsFor(user, series int64) ReaderPrefs {
	var p ReaderPrefs
	_ = r.db.QueryRow(`SELECT direction, layout, shift FROM manga_reader_prefs WHERE user_id = ? AND series_id = ?`, user, series).Scan(&p.Direction, &p.Layout, &p.Shift)
	return p
}

// SetReaderPrefs keeps what's chosen in p; an empty field stays as it was.
func (r *Repo) SetReaderPrefs(user, series int64, p ReaderPrefs) error {
	_, err := r.db.Exec(`INSERT INTO manga_reader_prefs (user_id, series_id, direction, layout, shift) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, series_id) DO UPDATE SET
			direction = CASE WHEN excluded.direction != '' THEN excluded.direction ELSE direction END,
			layout = CASE WHEN excluded.layout != '' THEN excluded.layout ELSE layout END,
			shift = CASE WHEN excluded.shift != 0 THEN excluded.shift ELSE shift END`,
		user, series, p.Direction, p.Layout, p.Shift)
	return err
}

// MarkRead marks volumes read or unread for someone. Either way their place in
// the volume is forgotten: it opens at the first page.
func (r *Repo) MarkRead(user int64, ids []int64, read bool, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if read {
			if _, err := tx.Exec(`INSERT INTO manga_read (user_id, volume_id, read_ms) SELECT ?, id, ? FROM manga_volumes WHERE id = ?
				ON CONFLICT (user_id, volume_id) DO UPDATE SET read_ms = excluded.read_ms`, user, now.UnixMilli(), id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(`DELETE FROM manga_read WHERE user_id = ? AND volume_id = ?`, user, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM home_positions WHERE user_id = ? AND item = ?`, user, PositionItem(id)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FillReading says, on each of a series' volumes, what this person has read and
// where they are.
func (r *Repo) FillReading(user, seriesID int64, list []Volume) error {
	read := map[int64]bool{}
	rows, err := r.db.Query(`SELECT m.volume_id FROM manga_read m JOIN manga_volumes v ON v.id = m.volume_id WHERE m.user_id = ? AND v.series_id = ?`, user, seriesID)
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
	places := map[int64][2]float64{}
	rows, err = r.db.Query(`SELECT p.item, p.position, p.duration FROM home_positions p WHERE p.user_id = ? AND p.item LIKE 'manga:v:%'`, user)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item string
		var pos, dur float64
		if err := rows.Scan(&item, &pos, &dur); err != nil {
			return err
		}
		if id, ok := VolumeOfItem(item); ok {
			places[id] = [2]float64{pos, dur}
		}
	}
	for i := range list {
		list[i].Read = read[list[i].ID]
		if at, ok := places[list[i].ID]; ok {
			list[i].Page, list[i].Pages = int(at[0]), int(at[1])
		}
	}
	return rows.Err()
}

// NextVolumes is what comes after a volume: the very next one, and the next one
// with a file (the same one when the very next has a file).
func NextVolumes(list []Volume, id int64) (next, have *Volume) {
	for i := range list {
		if list[i].ID != id {
			continue
		}
		for j := i + 1; j < len(list); j++ {
			if next == nil {
				next = &list[j]
			}
			if list[j].Status == StatusDownloaded {
				return next, &list[j]
			}
		}
		return next, nil
	}
	return nil, nil
}

// ContinueItem is one line of someone's Continue row: a volume they stopped in,
// or the next one after a volume they finished.
type ContinueItem struct {
	VolumeID  int64  `json:"volumeId,omitempty"`
	ChapterID int64  `json:"chapterId,omitempty"` // a chapter, in place of a volume
	SeriesID  int64  `json:"seriesId"`
	Series    string `json:"series"`
	Number    string `json:"number"`
	CoverURL  string `json:"coverUrl,omitempty"`
	Page      int    `json:"page"`  // 0: the start of a new volume
	Pages     int    `json:"pages"` // of how many, when known
	IsAdult   bool   `json:"-"`
	at        int64
}

// Continue is someone's row, newest first and one line a series: their place in
// a volume, or else the next unread volume with a file after what they've read.
// With seriesID it's for that series alone.
func (r *Repo) Continue(user, seriesID int64, limit int) ([]ContinueItem, error) {
	args := []any{user}
	where := ""
	if seriesID > 0 {
		where, args = ` AND s.id = ?`, append(args, seriesID)
	}
	rows, err := r.db.Query(`SELECT v.id, s.id, s.title, v.number, s.cover_url, s.is_adult, p.position, p.duration, p.updated_ms
		FROM home_positions p
		JOIN manga_volumes v ON p.item = 'manga:v:' || v.id
		JOIN manga_series s ON s.id = v.series_id
		WHERE p.user_id = ? AND v.status = 'downloaded'`+where+` ORDER BY p.updated_ms DESC`, args...)
	if err != nil {
		return nil, err
	}
	out := []ContinueItem{}
	seen := map[int64]bool{}
	for rows.Next() {
		var c ContinueItem
		var pos, dur float64
		if err := rows.Scan(&c.VolumeID, &c.SeriesID, &c.Series, &c.Number, &c.CoverURL, &c.IsAdult, &pos, &dur, &c.at); err != nil {
			rows.Close()
			return nil, err
		}
		if seen[c.SeriesID] {
			continue
		}
		seen[c.SeriesID] = true
		c.Page, c.Pages = int(pos), int(dur)
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Chapters someone stopped in. A later place in the same series wins; a chapter
	// that a volume here now holds has handed its place over to that volume.
	rows, err = r.db.Query(`SELECT c.id, s.id, s.title, c.number, s.cover_url, s.is_adult, p.position, p.duration, p.updated_ms
		FROM home_positions p
		JOIN manga_chapters c ON p.item = 'manga:c:' || c.id
		JOIN manga_series s ON s.id = c.series_id
		WHERE p.user_id = ? AND c.status = 'downloaded'
			AND NOT EXISTS (SELECT 1 FROM manga_volumes v WHERE v.series_id = c.series_id AND c.volume != '' AND v.number = c.volume AND v.status = 'downloaded')`+where+` ORDER BY p.updated_ms DESC`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c ContinueItem
		var pos, dur float64
		if err := rows.Scan(&c.ChapterID, &c.SeriesID, &c.Series, &c.Number, &c.CoverURL, &c.IsAdult, &pos, &dur, &c.at); err != nil {
			rows.Close()
			return nil, err
		}
		c.Page, c.Pages = int(pos), int(dur)
		placed := false
		for i := range out {
			if out[i].SeriesID == c.SeriesID {
				placed = true
				if c.at > out[i].at {
					out[i] = c
				}
				break
			}
		}
		if !placed {
			seen[c.SeriesID] = true
			out = append(out, c)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Series read last, with nothing part way through: whatever comes next.
	rows, err = r.db.Query(`SELECT id, MAX(at) FROM (
			SELECT s.id AS id, m.read_ms AS at FROM manga_read m JOIN manga_volumes v ON v.id = m.volume_id
				JOIN manga_series s ON s.id = v.series_id WHERE m.user_id = ?`+where+`
			UNION ALL
			SELECT s.id AS id, m.read_ms AS at FROM manga_chapter_read m JOIN manga_chapters c ON c.id = m.chapter_id
				JOIN manga_series s ON s.id = c.series_id WHERE m.user_id = ?`+where+`
		) GROUP BY id`, append(append([]any{}, args...), args...)...)
	if err != nil {
		return nil, err
	}
	type recent struct{ series, at int64 }
	var finished []recent
	for rows.Next() {
		var f recent
		if err := rows.Scan(&f.series, &f.at); err != nil {
			rows.Close()
			return nil, err
		}
		if !seen[f.series] {
			finished = append(finished, f)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, f := range finished {
		c, ok, err := r.nextUnread(user, f.series)
		if err != nil {
			return nil, err
		}
		if ok {
			c.at = f.at
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at > out[j].at })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// nextUnread is the first volume with a file after the last one this person read,
// or with chapters in the series, whatever comes next in the reading order.
func (r *Repo) nextUnread(user, seriesID int64) (ContinueItem, bool, error) {
	var c ContinueItem
	var chapters int
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM manga_chapters WHERE series_id = ? AND status = 'downloaded'`, seriesID).Scan(&chapters)
	if chapters > 0 {
		order, err := r.ReadingOrderFor(user, seriesID)
		if err != nil {
			return c, false, err
		}
		next := NextUnreadInOrder(order)
		if next == nil {
			return c, false, nil
		}
		if err := r.db.QueryRow(`SELECT id, title, cover_url, is_adult FROM manga_series WHERE id = ?`, seriesID).Scan(&c.SeriesID, &c.Series, &c.CoverURL, &c.IsAdult); err != nil {
			return c, false, err
		}
		c.Number = next.Number
		if next.Chapter {
			c.ChapterID = next.ID
		} else {
			c.VolumeID = next.ID
		}
		return c, true, nil
	}
	err := r.db.QueryRow(`SELECT v.id, s.id, s.title, v.number, s.cover_url, s.is_adult
		FROM manga_volumes v JOIN manga_series s ON s.id = v.series_id
		WHERE v.series_id = ? AND v.status = 'downloaded'
			AND v.sort_key > (SELECT MAX(w.sort_key) FROM manga_read m JOIN manga_volumes w ON w.id = m.volume_id WHERE m.user_id = ? AND w.series_id = ?)
			AND NOT EXISTS (SELECT 1 FROM manga_read m WHERE m.user_id = ? AND m.volume_id = v.id)
		ORDER BY v.sort_key LIMIT 1`, seriesID, user, seriesID, user).
		Scan(&c.VolumeID, &c.SeriesID, &c.Series, &c.Number, &c.CoverURL, &c.IsAdult)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}
