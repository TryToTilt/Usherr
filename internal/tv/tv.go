// Package tv is Mediarium TV (home fork): channels that play the library on
// a schedule, like broadcast TV. A channel's rules say what it plays (shows,
// movies, genres, decades) and in which order; the scheduler lays programs
// back to back a few days ahead, and tuning in plays whatever is on, from
// the right second.
package tv

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Rules say what a channel plays.
type Rules struct {
	Series []int64 `json:"series,omitempty"` // shows, by library id
	Movies []int64 `json:"movies,omitempty"` // movies, by library id
	// Genres and Decades add every show (and, with AllMovies, movie) that
	// matches: "Animation"; 1980 for the 1980s.
	Genres  []string `json:"genres,omitempty"`
	Decades []int    `json:"decades,omitempty"`
	// AllMovies lets genres and decades pick movies too, not only shows.
	AllMovies bool `json:"allMovies,omitempty"`
	// Order: "rotate" (default: one episode of each show in turn, shows
	// reshuffled each round, every show in episode order), "inorder" (each
	// show start to finish, one after another) or "shuffle" (any episode).
	Order string `json:"order,omitempty"`
	// MaxMoviesPerDay caps movies on a channel that mixes shows and movies
	// (0 = no cap).
	MaxMoviesPerDay int `json:"maxMoviesPerDay,omitempty"`

	// Live channels (Kind "live"): a free stream relayed as it is, with its
	// guide from an XMLTV feed (GuideID is the channel's id in that feed).
	StreamURL string `json:"streamUrl,omitempty"`
	GuideURL  string `json:"guideUrl,omitempty"`
	GuideID   string `json:"guideId,omitempty"`

	// Stream adds shows that aren't in the library: their episodes come from
	// TMDB and play through StreamNZB when they come on.
	Stream []StreamShow `json:"stream,omitempty"`
}

// StreamShow is a show a channel streams rather than plays from the library.
type StreamShow struct {
	TMDBID int64  `json:"tmdbId"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
}

// StreamPath is how a streamed episode is written where a file path would
// be: "stream:<tmdb id>:<season>:<episode>".
func StreamPath(tmdbID int64, season, episode int) string {
	return fmt.Sprintf("stream:%d:%d:%d", tmdbID, season, episode)
}

// ParseStreamPath reads a StreamPath back.
func ParseStreamPath(p string) (tmdbID int64, season, episode int, ok bool) {
	n, err := fmt.Sscanf(p, "stream:%d:%d:%d", &tmdbID, &season, &episode)
	return tmdbID, season, episode, err == nil && n == 3 && tmdbID > 0
}

// Channel is one TV channel.
type Channel struct {
	ID      int64  `json:"id"`
	Number  int    `json:"number"`
	Name    string `json:"name"`
	LogoURL string `json:"logoUrl,omitempty"`
	Kind    string `json:"kind"`
	Rules   Rules  `json:"rules"`
	Enabled bool   `json:"enabled"`
}

// Program is one slot in a channel's schedule.
type Program struct {
	ChannelID int64     `json:"channelId"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Kind      string    `json:"kind"` // episode | movie | live | stream
	ItemID    int64     `json:"itemId"`
	SeriesID  int64     `json:"seriesId,omitempty"`
	Title     string    `json:"title"`
	Subtitle  string    `json:"subtitle,omitempty"`
	FilePath  string    `json:"-"`
}

// Repo stores channels and schedules.
type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

var ErrNotFound = errors.New("no such channel")

func (r *Repo) Channels() ([]Channel, error) {
	rows, err := r.db.Query(`SELECT id, number, name, logo_url, kind, rules, enabled FROM tv_channels ORDER BY number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		c, err := scanChannel(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) Channel(id int64) (Channel, error) {
	c, err := scanChannel(r.db.QueryRow(`SELECT id, number, name, logo_url, kind, rules, enabled FROM tv_channels WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func scanChannel(scan func(...any) error) (Channel, error) {
	var c Channel
	var rules string
	var enabled int
	if err := scan(&c.ID, &c.Number, &c.Name, &c.LogoURL, &c.Kind, &rules, &enabled); err != nil {
		return c, err
	}
	c.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(rules), &c.Rules)
	return c, nil
}

// Save adds a channel (ID 0) or updates one.
func (r *Repo) Save(c Channel) (Channel, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return c, errors.New("give the channel a name")
	}
	if c.Kind == "" {
		c.Kind = "library"
	}
	rules, _ := json.Marshal(c.Rules)
	if c.Number <= 0 {
		// Your own channels count up from 1, live ones from 100.
		if c.Kind == "live" {
			_ = r.db.QueryRow(`SELECT COALESCE(MAX(number), 99) + 1 FROM tv_channels WHERE number >= 100`).Scan(&c.Number)
		} else {
			_ = r.db.QueryRow(`SELECT COALESCE(MAX(number), 0) + 1 FROM tv_channels WHERE number < 100`).Scan(&c.Number)
		}
	}
	if c.ID == 0 {
		res, err := r.db.Exec(`INSERT INTO tv_channels (number, name, logo_url, kind, rules, enabled) VALUES (?, ?, ?, ?, ?, ?)`,
			c.Number, c.Name, c.LogoURL, c.Kind, string(rules), c.Enabled)
		if err != nil {
			return c, fmt.Errorf("add channel: %w", err)
		}
		c.ID, _ = res.LastInsertId()
		return c, nil
	}
	_, err := r.db.Exec(`UPDATE tv_channels SET number = ?, name = ?, logo_url = ?, kind = ?, rules = ?, enabled = ? WHERE id = ?`,
		c.Number, c.Name, c.LogoURL, c.Kind, string(rules), c.Enabled, c.ID)
	if err != nil {
		return c, fmt.Errorf("save channel: %w", err)
	}
	return c, nil
}

func (r *Repo) Delete(id int64) error {
	_, err := r.db.Exec(`DELETE FROM tv_channels WHERE id = ?`, id)
	return err
}

// Programs returns a channel's programs that overlap [from, to).
func (r *Repo) Programs(channelID int64, from, to time.Time) ([]Program, error) {
	rows, err := r.db.Query(`SELECT channel_id, start_ms, end_ms, kind, item_id, series_id, title, subtitle, file_path
		FROM tv_programs WHERE channel_id = ? AND end_ms > ? AND start_ms < ? ORDER BY start_ms`,
		channelID, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Program
	for rows.Next() {
		var p Program
		var s, e int64
		if err := rows.Scan(&p.ChannelID, &s, &e, &p.Kind, &p.ItemID, &p.SeriesID, &p.Title, &p.Subtitle, &p.FilePath); err != nil {
			return nil, err
		}
		p.Start, p.End = time.UnixMilli(s), time.UnixMilli(e)
		out = append(out, p)
	}
	return out, rows.Err()
}

// At is what a channel is playing at t.
func (r *Repo) At(channelID int64, t time.Time) (Program, bool, error) {
	list, err := r.Programs(channelID, t, t.Add(time.Millisecond))
	if err != nil || len(list) == 0 {
		return Program{}, false, err
	}
	return list[0], true, nil
}

// LastEnd is when a channel's schedule runs out (zero when it has none).
func (r *Repo) LastEnd(channelID int64) (time.Time, error) {
	var ms sql.NullInt64
	err := r.db.QueryRow(`SELECT MAX(end_ms) FROM tv_programs WHERE channel_id = ?`, channelID).Scan(&ms)
	if err != nil || !ms.Valid {
		return time.Time{}, err
	}
	return time.UnixMilli(ms.Int64), nil
}

// Append adds programs at the end of the schedule.
func (r *Repo) Append(list []Program) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range list {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO tv_programs (channel_id, start_ms, end_ms, kind, item_id, series_id, title, subtitle, file_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.ChannelID, p.Start.UnixMilli(), p.End.UnixMilli(), p.Kind, p.ItemID, p.SeriesID, p.Title, p.Subtitle, p.FilePath); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearFrom drops a channel's programs that start at or after t (the one on
// air keeps playing); used when its rules change.
func (r *Repo) ClearFrom(channelID int64, t time.Time) error {
	_, err := r.db.Exec(`DELETE FROM tv_programs WHERE channel_id = ? AND start_ms >= ?`, channelID, t.UnixMilli())
	return err
}

// Replace swaps a channel's whole schedule for list (live channels, whose
// guide comes from outside).
func (r *Repo) Replace(channelID int64, list []Program) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM tv_programs WHERE channel_id = ?`, channelID); err != nil {
		return err
	}
	for _, p := range list {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO tv_programs (channel_id, start_ms, end_ms, kind, item_id, series_id, title, subtitle, file_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, channelID, p.Start.UnixMilli(), p.End.UnixMilli(), p.Kind, p.ItemID, p.SeriesID, p.Title, p.Subtitle, p.FilePath); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Trim forgets programs that ended before t.
func (r *Repo) Trim(t time.Time) error {
	_, err := r.db.Exec(`DELETE FROM tv_programs WHERE end_ms < ?`, t.UnixMilli())
	return err
}

func (r *Repo) State(channelID int64) (State, error) {
	var raw string
	err := r.db.QueryRow(`SELECT state FROM tv_state WHERE channel_id = ?`, channelID).Scan(&raw)
	st := State{Next: map[int64]int{}}
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	_ = json.Unmarshal([]byte(raw), &st)
	if st.Next == nil {
		st.Next = map[int64]int{}
	}
	return st, nil
}

func (r *Repo) SaveState(channelID int64, st State) error {
	raw, _ := json.Marshal(st)
	_, err := r.db.Exec(`INSERT INTO tv_state (channel_id, state) VALUES (?, ?) ON CONFLICT (channel_id) DO UPDATE SET state = excluded.state`, channelID, string(raw))
	return err
}

// Duration is a file's measured runtime, when known for this size.
func (r *Repo) Duration(path string, size int64) (float64, bool) {
	var secs float64
	var s int64
	if r.db.QueryRow(`SELECT size, seconds FROM tv_durations WHERE file_path = ?`, path).Scan(&s, &secs) != nil || s != size {
		return 0, false
	}
	return secs, true
}

func (r *Repo) SetDuration(path string, size int64, secs float64) {
	_, _ = r.db.Exec(`INSERT INTO tv_durations (file_path, size, seconds) VALUES (?, ?, ?)
		ON CONFLICT (file_path) DO UPDATE SET size = excluded.size, seconds = excluded.seconds`, path, size, secs)
}
