package api

// Usherr: Showtimes at home. A film booked for a time: the Marquee shows a
// countdown, the pre-show starts on every Marquee screen when there are
// pre-show minutes left, and at showtime the film starts where it was booked
// to play: on a Showtime screen (/showtime, a browser left open by the TV,
// which starts it itself), on the TV (Play on TV), or nowhere (watched in
// Jellyfin or another app; the Marquee still counts down and runs the
// pre-show). A double feature is a second booking right after the first.

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	showtimeKeep      = 6 * time.Hour  // a past showtime is listed this long
	showtimeMarqueeIn = 12 * time.Hour // the Marquee shows what's within this
)

type showtime struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	TMDBID     int    `json:"tmdbId"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	PosterURL  string `json:"posterUrl,omitempty"`
	Runtime    int    `json:"runtime,omitempty"`
	AtMs       int64  `json:"atMs"`
	PreshowMin int    `json:"preshowMin"`
	PlayOn     string `json:"playOn"` // here, tv or none
	State      string `json:"state"`  // booked, preshow, playing, done
	Mine       bool   `json:"mine"`
	LibraryID  int64  `json:"libraryId,omitempty"` // the film's file in the library, played from it
	// Usherr: the film's saved release in the streamed library (Streams),
	// played from it when there's no file.
	StreamedFileID int64 `json:"streamedFileId,omitempty"`
	Theme          int64 `json:"preshowTheme"` // Usherr: the pre-show theme (0: the schedule's)
}

const showtimeCols = `id, user_id, tmdb_id, title, year, poster_url, runtime, at_ms, preshow_min, play_on, state, preshow_theme`

func scanShowtime(sc interface{ Scan(...any) error }) (showtime, error) {
	var st showtime
	err := sc.Scan(&st.ID, &st.UserID, &st.TMDBID, &st.Title, &st.Year, &st.PosterURL, &st.Runtime, &st.AtMs, &st.PreshowMin, &st.PlayOn, &st.State, &st.Theme)
	return st, err
}

// showtimesFrom lists the showtimes from a moment on, soonest first.
func (s *Server) showtimesFrom(ctx context.Context, from time.Time) ([]showtime, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+showtimeCols+` FROM showtimes WHERE at_ms >= ? ORDER BY at_ms, id`, from.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []showtime{}
	for rows.Next() {
		st, err := scanShowtime(rows)
		if err != nil {
			return out, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// withLibrary fills in the library's copy of each film, when there is one.
func (s *Server) withLibrary(list []showtime, me int64) []showtime {
	for i := range list {
		list[i].Mine = list[i].UserID == me
		if m, ok, err := s.MovieRepo.GetByTMDBID(list[i].TMDBID); err == nil && ok && m.FilePath != "" {
			list[i].LibraryID = m.ID
			continue
		}
		_ = s.db.QueryRow(`SELECT f.id FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id WHERE t.kind = 'movie' AND t.tmdb_id = ? ORDER BY f.id LIMIT 1`, list[i].TMDBID).Scan(&list[i].StreamedFileID)
	}
	return list
}

// GET /api/home/showtimes: the showtimes from a few hours ago on, with the
// server's clock (a Showtime screen counts down by it).
func (s *Server) handleShowtimes(w http.ResponseWriter, r *http.Request) {
	list, err := s.showtimesFrom(r.Context(), time.Now().Add(-showtimeKeep))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The showtimes couldn't be listed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"showtimes": s.withLibrary(list, requestUserID(r)), "nowMs": time.Now().UnixMilli(), "tv": s.tvReady()})
}

type showtimeBody struct {
	TMDBID     int    `json:"tmdbId"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	PosterURL  string `json:"posterUrl"`
	Runtime    int    `json:"runtime"`
	AtMs       int64  `json:"atMs"`
	PreshowMin *int   `json:"preshowMin"`
	PlayOn     string `json:"playOn"`
	Theme      *int64 `json:"preshowTheme"` // Usherr: the pre-show theme
}

func (b *showtimeBody) check() string {
	if b.TMDBID <= 0 || strings.TrimSpace(b.Title) == "" {
		return "Say which film."
	}
	if b.AtMs < time.Now().Add(-time.Minute).UnixMilli() || b.AtMs > time.Now().AddDate(0, 0, 60).UnixMilli() {
		return "Pick a time from now to two months ahead."
	}
	if b.PreshowMin != nil && (*b.PreshowMin < 0 || *b.PreshowMin > 60) {
		return "The pre-show can be up to an hour."
	}
	if b.PlayOn == "" {
		b.PlayOn = "here"
	}
	if b.PlayOn != "here" && b.PlayOn != "tv" && b.PlayOn != "none" {
		return "Say where it plays: here, on the TV, or nowhere."
	}
	if b.Runtime < 0 || b.Runtime > 600 {
		b.Runtime = 0
	}
	return ""
}

// POST /api/home/showtimes: book a film for a time.
func (s *Server) handleAddShowtime(w http.ResponseWriter, r *http.Request) {
	var b showtimeBody
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if msg := b.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	pre := 10
	if b.PreshowMin != nil {
		pre = *b.PreshowMin
	}
	if b.PlayOn == "tv" && !s.tvReady() {
		writeError(w, http.StatusBadRequest, "Playing on the TV isn't set up here (Settings > Extras).")
		return
	}
	// The film's length (when it ends: the next film of a double feature),
	// poster and year from TMDB when the page didn't send them.
	if (b.Runtime == 0 || b.PosterURL == "" || b.Year == 0) && s.TMDB().HasAPIKey() {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		if d, err := s.displayTMDB().GetMovieDetail(ctx, b.TMDBID); err == nil {
			if b.Runtime == 0 {
				b.Runtime = d.Runtime
			}
			if b.PosterURL == "" {
				b.PosterURL = metadata.ImageURL("w500", d.PosterPath)
			}
			if b.Year == 0 {
				b.Year = d.Year()
			}
		}
		cancel()
	}
	var theme int64
	if b.Theme != nil {
		theme = max(*b.Theme, 0)
	}
	res, err := s.db.ExecContext(r.Context(), `INSERT INTO showtimes (user_id, tmdb_id, title, year, poster_url, runtime, at_ms, preshow_min, play_on, created_ms, preshow_theme)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, requestUserID(r), b.TMDBID, trimRunes(strings.TrimSpace(b.Title), 200), b.Year,
		trimRunes(b.PosterURL, 500), b.Runtime, b.AtMs, pre, b.PlayOn, time.Now().UnixMilli(), theme)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The showtime couldn't be saved.")
		return
	}
	id, _ := res.LastInsertId()
	go s.artForFilm(b.TMDBID) // Usherr: its artwork ready before the night
	s.marqueeLive.send("", marqueeEvent{Type: "settings"})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// showtimeFor reads a showtime the asker may change: their own, or any for
// an administrator.
func (s *Server) showtimeFor(r *http.Request) (showtime, int, string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return showtime{}, http.StatusNotFound, "That showtime isn't known."
	}
	st, err := scanShowtime(s.db.QueryRowContext(r.Context(), `SELECT `+showtimeCols+` FROM showtimes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return st, http.StatusNotFound, "That showtime isn't known."
	} else if err != nil {
		return st, http.StatusInternalServerError, "The showtime couldn't be read."
	}
	if u := auth.UserFromContext(r.Context()); st.UserID != requestUserID(r) && (u == nil || !u.IsAdmin) {
		return st, http.StatusForbidden, "Only whoever booked it (or an administrator) can change it."
	}
	return st, 0, ""
}

// PUT /api/home/showtimes/{id}: another time, pre-show or place.
func (s *Server) handlePutShowtime(w http.ResponseWriter, r *http.Request) {
	st, code, msg := s.showtimeFor(r)
	if code != 0 {
		writeError(w, code, msg)
		return
	}
	var b showtimeBody
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	b.TMDBID, b.Title, b.Runtime = st.TMDBID, st.Title, st.Runtime
	if msg := b.check(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	pre := st.PreshowMin
	if b.PreshowMin != nil {
		pre = *b.PreshowMin
	}
	theme := st.Theme
	if b.Theme != nil {
		theme = max(*b.Theme, 0)
	}
	// A new time starts it over.
	if _, err := s.db.ExecContext(r.Context(), `UPDATE showtimes SET at_ms = ?, preshow_min = ?, play_on = ?, preshow_theme = ?, state = 'booked' WHERE id = ?`, b.AtMs, pre, b.PlayOn, theme, st.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "The showtime couldn't be saved.")
		return
	}
	s.marqueeLive.send("", marqueeEvent{Type: "settings"})
	writeJSON(w, http.StatusOK, map[string]any{"id": st.ID})
}

// DELETE /api/home/showtimes/{id}: cancel it.
func (s *Server) handleDeleteShowtime(w http.ResponseWriter, r *http.Request) {
	st, code, msg := s.showtimeFor(r)
	if code != 0 {
		writeError(w, code, msg)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM showtimes WHERE id = ?`, st.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "The showtime couldn't be cancelled.")
		return
	}
	s.marqueeLive.send("", marqueeEvent{Type: "settings"})
	w.WriteHeader(http.StatusNoContent)
}

// showtimeJob keeps time: the pre-show starts on the Marquee screens when
// there are pre-show minutes left, a film booked for the TV starts there,
// and a showtime long over is marked done.
func (s *Server) showtimeJob(ctx context.Context) {
	now := time.Now()
	list, err := s.showtimesFrom(ctx, now.Add(-showtimeKeep))
	if err != nil {
		return
	}
	for _, st := range list {
		at := time.UnixMilli(st.AtMs)
		set := func(state string) {
			_, _ = s.db.ExecContext(ctx, `UPDATE showtimes SET state = ? WHERE id = ?`, state, st.ID)
		}
		switch {
		case st.State == "done":
		case now.After(at.Add(time.Duration(max(st.Runtime, 90)+30) * time.Minute)):
			set("done")
		case st.State == "booked" && st.PreshowMin > 0 && !now.Before(at.Add(-time.Duration(st.PreshowMin)*time.Minute)) && now.Before(at):
			s.showtimePreshow()
			set("preshow")
		case (st.State == "booked" || st.State == "preshow") && !now.Before(at) && now.Before(at.Add(5*time.Minute)):
			if st.PlayOn == "tv" && s.tvReady() {
				tctx, cancel := context.WithTimeout(ctx, 60*time.Second)
				if _, err := s.openOnTV(tctx, "movie", st.TMDBID, 0, 0); err != nil {
					slog.Info("showtime: on the TV", "title", st.Title, "err", err)
				}
				cancel()
			}
			set("playing")
		case (st.State == "booked" || st.State == "preshow") && !now.Before(at.Add(5*time.Minute)):
			set("playing") // missed its moment (the server was off): not started late
		}
	}
}

// showtimePreshow starts the pre-show on every Marquee screen that is on.
func (s *Server) showtimePreshow() {
	list, err := s.marqueeScreens(context.Background())
	if err != nil {
		return
	}
	var ids []string
	for _, sc := range list {
		if sc.Online {
			ids = append(ids, sc.ID)
		}
	}
	if len(ids) > 0 {
		_, _ = s.marqueeDo(ids, "preshow", "")
	}
}

// marqueeShowtimes are the showtimes the Marquee counts down to.
func (s *Server) marqueeShowtimes(ctx context.Context) []showtime {
	list, err := s.showtimesFrom(ctx, time.Now().Add(-3*time.Hour))
	if err != nil {
		return []showtime{}
	}
	out := []showtime{}
	for _, st := range list {
		if st.State == "done" || time.UnixMilli(st.AtMs).After(time.Now().Add(showtimeMarqueeIn)) {
			continue
		}
		out = append(out, st)
	}
	return out
}
