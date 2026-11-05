package watchlog

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Repo keeps the watch history in the database (migration 9068).
type Repo struct {
	db *sql.DB
}

// NewRepo returns a Repo on db.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const columns = `id, server_id, server_name, server_kind, session_key, server_user_id, user_name, user_id, kind, title, series,
	season, episode, year, tmdb_id, library_id, client, device, play_method, transcode_reasons, video_in, video_out,
	audio_in, audio_out, resolution_in, resolution_out, bandwidth_kbps, location, address, started_ms, ended_ms,
	last_seen_ms, watched_sec, progress, runtime_sec, paused, completed`

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func fromMS(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}

type scanner interface{ Scan(...any) error }

func scan(sc scanner) (Session, error) {
	var s Session
	var reasons string
	var started, ended, seen int64
	var paused, completed int
	err := sc.Scan(&s.ID, &s.ServerID, &s.ServerName, &s.ServerKind, &s.Key, &s.ServerUserID, &s.UserName, &s.UserID, &s.Kind, &s.Title, &s.Series,
		&s.Season, &s.Episode, &s.Year, &s.TMDBID, &s.LibraryID, &s.Client, &s.Device, &s.PlayMethod, &reasons, &s.VideoIn, &s.VideoOut,
		&s.AudioIn, &s.AudioOut, &s.ResolutionIn, &s.ResolutionOut, &s.BandwidthKbps, &s.Location, &s.Address, &started, &ended,
		&seen, &s.WatchedSec, &s.Progress, &s.RuntimeSec, &paused, &completed)
	if err != nil {
		return s, err
	}
	if reasons != "" {
		s.TranscodeReasons = strings.Split(reasons, "; ")
	}
	s.StartedAt, s.EndedAt, s.LastSeen = fromMS(started), fromMS(ended), fromMS(seen)
	s.Paused, s.Completed = paused == 1, completed == 1
	return s, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Insert saves a new playback and sets its ID.
func (r *Repo) Insert(ctx context.Context, s *Session) error {
	res, err := r.db.ExecContext(ctx, `INSERT INTO watch_sessions (server_id, server_name, server_kind, session_key, server_user_id, user_name,
		user_id, kind, title, series, season, episode, year, tmdb_id, library_id, client, device, play_method, transcode_reasons,
		video_in, video_out, audio_in, audio_out, resolution_in, resolution_out, bandwidth_kbps, location, address, started_ms,
		ended_ms, last_seen_ms, watched_sec, progress, runtime_sec, paused, completed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ServerID, s.ServerName, s.ServerKind, s.Key, s.ServerUserID, s.UserName, s.UserID, s.Kind, s.Title, s.Series,
		s.Season, s.Episode, s.Year, s.TMDBID, s.LibraryID, s.Client, s.Device, s.PlayMethod, strings.Join(s.TranscodeReasons, "; "),
		s.VideoIn, s.VideoOut, s.AudioIn, s.AudioOut, s.ResolutionIn, s.ResolutionOut, s.BandwidthKbps, s.Location, s.Address,
		ms(s.StartedAt), ms(s.EndedAt), ms(s.LastSeen), s.WatchedSec, s.Progress, s.RuntimeSec, b2i(s.Paused), b2i(s.Completed))
	if err != nil {
		return fmt.Errorf("save playback: %w", err)
	}
	s.ID, err = res.LastInsertId()
	return err
}

// Update saves what changes while a playback goes on.
func (r *Repo) Update(ctx context.Context, s Session) error {
	_, err := r.db.ExecContext(ctx, `UPDATE watch_sessions SET play_method = ?, transcode_reasons = ?, video_out = ?, audio_out = ?,
		resolution_out = ?, bandwidth_kbps = ?, ended_ms = ?, last_seen_ms = ?, watched_sec = ?, progress = ?, runtime_sec = ?,
		paused = ?, completed = ? WHERE id = ?`,
		s.PlayMethod, strings.Join(s.TranscodeReasons, "; "), s.VideoOut, s.AudioOut, s.ResolutionOut, s.BandwidthKbps,
		ms(s.EndedAt), ms(s.LastSeen), s.WatchedSec, s.Progress, s.RuntimeSec, b2i(s.Paused), b2i(s.Completed), s.ID)
	if err != nil {
		return fmt.Errorf("update playback: %w", err)
	}
	return nil
}

// Open lists the playbacks that have not ended.
func (r *Repo) Open(ctx context.Context) ([]Session, error) {
	return r.query(ctx, `SELECT `+columns+` FROM watch_sessions WHERE ended_ms = 0 ORDER BY id`)
}

func (r *Repo) query(ctx context.Context, q string, args ...any) ([]Session, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("read watch history: %w", err)
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("read watch history: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Filter narrows the history. Zero values mean "any".
type Filter struct {
	OnlyUser int64  // an account sees only its own (0: everyone's)
	User     string // the name on the media server
	ServerID int64
	Query    string // part of the title or the show
	Device   string // part of the device's or the app's name
	Method   string // direct, stream or transcode
	From, To time.Time
	Limit    int
	Offset   int
}

var methodValue = map[string]string{"direct": "DirectPlay", "stream": "DirectStream", "transcode": "Transcode"}

func (f Filter) where() (string, []any) {
	var w []string
	var args []any
	if f.OnlyUser > 0 {
		w, args = append(w, "user_id = ?"), append(args, f.OnlyUser)
	}
	if f.User != "" {
		w, args = append(w, "user_name = ?"), append(args, f.User)
	}
	if f.ServerID > 0 {
		w, args = append(w, "server_id = ?"), append(args, f.ServerID)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + likeEscape(q) + "%"
		w, args = append(w, `(title LIKE ? ESCAPE '\' OR series LIKE ? ESCAPE '\')`), append(args, like, like)
	}
	if d := strings.TrimSpace(f.Device); d != "" {
		like := "%" + likeEscape(d) + "%"
		w, args = append(w, `(device LIKE ? ESCAPE '\' OR client LIKE ? ESCAPE '\')`), append(args, like, like)
	}
	if v, ok := methodValue[f.Method]; ok {
		w, args = append(w, "play_method = ?"), append(args, v)
	}
	if !f.From.IsZero() {
		w, args = append(w, "started_ms >= ?"), append(args, f.From.UnixMilli())
	}
	if !f.To.IsZero() {
		w, args = append(w, "started_ms < ?"), append(args, f.To.UnixMilli())
	}
	if len(w) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(w, " AND "), args
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// List is one page of the history, newest first, and how many there are in all.
func (r *Repo) List(ctx context.Context, f Filter) ([]Session, int, error) {
	where, args := f.where()
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM watch_sessions`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count watch history: %w", err)
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	list, err := r.query(ctx, `SELECT `+columns+` FROM watch_sessions`+where+` ORDER BY started_ms DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, max(f.Offset, 0))...)
	return list, total, err
}

// All is every playback the filter lets through, oldest first (for the stats).
func (r *Repo) All(ctx context.Context, f Filter) ([]Session, error) {
	where, args := f.where()
	return r.query(ctx, `SELECT `+columns+` FROM watch_sessions`+where+` ORDER BY started_ms`, args...)
}

// Choices are the values the history can be narrowed by.
type Choices struct {
	Users   []string `json:"users"`
	Servers []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"servers"`
}

// Choices lists the people and servers in the history (onlyUser > 0: in that
// account's history).
func (r *Repo) Choices(ctx context.Context, onlyUser int64) (Choices, error) {
	c := Choices{Users: []string{}}
	where, args := Filter{OnlyUser: onlyUser}.where()
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT user_name FROM watch_sessions`+where+` ORDER BY user_name COLLATE NOCASE`, args...)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil && n != "" {
			c.Users = append(c.Users, n)
		}
	}
	rows.Close()
	rows, err = r.db.QueryContext(ctx, `SELECT server_id, MAX(server_name) FROM watch_sessions`+where+` GROUP BY server_id ORDER BY 2 COLLATE NOCASE`, args...)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var s struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if rows.Scan(&s.ID, &s.Name) == nil {
			c.Servers = append(c.Servers, s)
		}
	}
	return c, rows.Err()
}

// Prune removes the playbacks that ended before the cutoff.
func (r *Repo) Prune(ctx context.Context, before time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM watch_sessions WHERE ended_ms > 0 AND ended_ms < ?`, before.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("prune watch history: %w", err)
	}
	return res.RowsAffected()
}

// ClearAddresses forgets every player's IP address.
func (r *Repo) ClearAddresses(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE watch_sessions SET address = '' WHERE address <> ''`)
	if err != nil {
		return 0, fmt.Errorf("clear addresses: %w", err)
	}
	return res.RowsAffected()
}

// Addresses counts the playbacks that still have an address.
func (r *Repo) Addresses(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM watch_sessions WHERE address <> ''`).Scan(&n)
	return n, err
}

// TitleSummary is how often one film or show was watched, and by whom.
type TitleSummary struct {
	Plays    int       `json:"plays"`
	Finished int       `json:"finished"`
	People   []string  `json:"people"`
	Last     time.Time `json:"last,omitzero"`
}

// Title sums up the playbacks of a library film (kind "movie") or show
// ("tv"), for everyone or one account.
func (r *Repo) Title(ctx context.Context, kind string, libraryID, onlyUser int64) (TitleSummary, error) {
	k := "movie"
	if kind == "tv" {
		k = "episode"
	}
	out := TitleSummary{People: []string{}}
	where := ` WHERE kind = ? AND library_id = ?`
	args := []any{k, libraryID}
	if onlyUser > 0 {
		where += ` AND user_id = ?`
		args = append(args, onlyUser)
	}
	var last sql.NullInt64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(completed), 0), MAX(started_ms) FROM watch_sessions`+where, args...).
		Scan(&out.Plays, &out.Finished, &last); err != nil {
		return out, fmt.Errorf("sum up a title's plays: %w", err)
	}
	if last.Valid {
		out.Last = fromMS(last.Int64)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT user_name FROM watch_sessions`+where+` AND user_name <> '' GROUP BY user_name ORDER BY COUNT(*) DESC, user_name LIMIT 6`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out.People = append(out.People, n)
		}
	}
	return out, rows.Err()
}
