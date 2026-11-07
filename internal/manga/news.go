package manga

// What a followed series got lately, for the calendar, and what someone has read.

import "time"

// NoteNewVolumes puts volumes on the calendar, on the day they were found.
func (r *Repo) NoteNewVolumes(volumes []Volume, now time.Time) error {
	for _, v := range volumes {
		if _, err := r.db.Exec(`INSERT OR IGNORE INTO manga_new_volumes (volume_id, found_on) VALUES (?, ?)`, v.ID, now.Format("2006-01-02")); err != nil {
			return err
		}
	}
	return nil
}

// NewVolumeEntry is a new volume on the calendar.
type NewVolumeEntry struct {
	VolumeID int64
	SeriesID int64
	Series   string
	Number   string
	CoverURL string
	Status   string // downloaded or missing
	FoundOn  string
}

// NewVolumesBetween are the new volumes found in a window of days. Adult series
// stay off the calendar, which everyone sees.
func (r *Repo) NewVolumesBetween(from, to string) ([]NewVolumeEntry, error) {
	rows, err := r.db.Query(`SELECT v.id, s.id, s.title, v.number, s.cover_url, v.status, n.found_on
		FROM manga_new_volumes n JOIN manga_volumes v ON v.id = n.volume_id JOIN manga_series s ON s.id = v.series_id
		WHERE n.found_on >= ? AND n.found_on <= ? AND s.is_adult = 0 ORDER BY n.found_on, s.sort_title, v.sort_key`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NewVolumeEntry
	for rows.Next() {
		var e NewVolumeEntry
		if err := rows.Scan(&e.VolumeID, &e.SeriesID, &e.Series, &e.Number, &e.CoverURL, &e.Status, &e.FoundOn); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// NumbersOf says volumes the way a sentence does: "Volume 12", "Volumes 12, 13", or
// "Volumes 12 to 40" for a long run.
func NumbersOf(volumes []Volume) string {
	switch n := len(volumes); {
	case n == 0:
		return ""
	case n == 1:
		return "Volume " + volumes[0].Number
	case n > 4:
		return "Volumes " + volumes[0].Number + " to " + volumes[n-1].Number
	}
	out := "Volumes " + volumes[0].Number
	for _, v := range volumes[1:] {
		out += ", " + v.Number
	}
	return out
}

// ReadStats is what someone has read.
type ReadStats struct {
	Volumes   int `json:"volumes"`
	Chapters  int `json:"chapters"`
	Series    int `json:"series"`
	ThisMonth int `json:"thisMonth"` // volumes and chapters
}

// ReadStatsFor counts someone's finished volumes and chapters.
func (r *Repo) ReadStatsFor(user int64, now time.Time) (ReadStats, error) {
	var s ReadStats
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).UnixMilli()
	err := r.db.QueryRow(`SELECT
			COALESCE(SUM(chapter = 0), 0), COALESCE(SUM(chapter = 1), 0), COUNT(DISTINCT series), COALESCE(SUM(at >= ?), 0)
		FROM (
			SELECT 0 AS chapter, v.series_id AS series, m.read_ms AS at FROM manga_read m JOIN manga_volumes v ON v.id = m.volume_id WHERE m.user_id = ?
			UNION ALL
			SELECT 1, c.series_id, m.read_ms FROM manga_chapter_read m JOIN manga_chapters c ON c.id = m.chapter_id WHERE m.user_id = ?
		)`, month, user, user).Scan(&s.Volumes, &s.Chapters, &s.Series, &s.ThisMonth)
	return s, err
}

// LibraryIDs maps AniList ids to the series in the library.
func (r *Repo) LibraryIDs() (map[int64]int64, error) {
	rows, err := r.db.Query(`SELECT anilist_id, id FROM manga_series WHERE anilist_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var a, id int64
		if err := rows.Scan(&a, &id); err != nil {
			return nil, err
		}
		out[a] = id
	}
	return out, rows.Err()
}

// NewVolumesAfter picks, of volumes just found, those past the last one the series
// already had. A gap being filled isn't news, and neither is a series' first list.
func NewVolumesAfter(found []Volume, highest float64, hadRows bool) []Volume {
	if !hadRows {
		return nil
	}
	var out []Volume
	for _, v := range found {
		if SortKey(v.Number) > highest {
			out = append(out, v)
		}
	}
	return out
}

// RefreshDue says whether AniList should be asked about a series again: a running
// series after most of a day, a finished one after a month.
func RefreshDue(status, checkedAt string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, checkedAt)
	if err != nil {
		return true
	}
	wait := 20 * time.Hour
	if status == "finished" || status == "cancelled" {
		wait = 30 * 24 * time.Hour
	}
	return now.Sub(t) >= wait
}
