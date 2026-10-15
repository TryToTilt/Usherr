package api

// Home fork: Streams for you. Each person can switch it on (on their You
// page) once an administrator has allowed it for the household. Then, a few
// times a day, the film that fits their taste best and that the household
// doesn't have anywhere goes into the streamed library by itself, as "Add as
// stream" would: a pointer file only, nothing downloaded, no indexer asked
// until someone presses play.
//
// Limits: at most autostreamPerWeek films a person a week, one at a time,
// only matches of autostreamFloor or more, films only. Nothing they've
// seen, voted on, or the household set aside; nothing the household has in
// any tier (on disk, in the streamed library or nzbdav's stream libraries,
// wanted). Nothing is ever removed by itself: the You page lists what was
// added, each with a Remove button.

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
)

const (
	autostreamPerWeek = 3
	autostreamFloor   = 85
	autostreamEvery   = 6 * time.Hour
	keyAutostreamOn   = "home.autostream_household" // "1": an administrator allowed it
)

type autostreamAdded struct {
	ID         int64  `json:"id"`
	TMDBID     int    `json:"tmdbId"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	Image      string `json:"image,omitempty"`
	Match      int    `json:"match"`
	Because    string `json:"because,omitempty"`
	StreamedID int64  `json:"streamedId,omitempty"`
	AddedAt    string `json:"addedAt"`
	Removed    bool   `json:"removed,omitempty"`
}

func (s *Server) autostreamAllowed() bool {
	v, _ := s.Settings.Get(keyAutostreamOn)
	return v == "1"
}

// streamedReady reports whether the streamed library is set up: pointer
// files need the address they lead to.
func (s *Server) streamedReady() bool { return s.streamedSetting(keyStreamedBase, "") != "" }

func (s *Server) autostreamOn(ctx context.Context, uid int64) bool {
	var v int
	err := s.db.QueryRowContext(ctx, `SELECT enabled FROM home_autostream WHERE user_id = ?`, uid).Scan(&v)
	return err == nil && v == 1
}

// autostreamThisWeek is how many films were added for a person in the last
// seven days (taken out again or not).
func (s *Server) autostreamThisWeek(ctx context.Context, uid int64, now time.Time) int {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM home_autostream_added WHERE user_id = ? AND added_at >= ?`,
		uid, now.Add(-7*24*time.Hour).UTC().Format(time.RFC3339)).Scan(&n)
	return n
}

// autostreamPick is the best film to add for a person now: the first of
// their "For you" list that fits well enough and that the household doesn't
// have in any tier. ok is false when there's none.
func (s *Server) autostreamPick(ctx context.Context, u *auth.User) (c similarCandidate, why tasteWhy, ok bool) {
	p := s.tasteOfUser(ctx, u, "movie")
	list, whys := s.forYou(ctx, p)
	if len(list) == 0 {
		return c, why, false
	}
	had := map[int]bool{}
	h := s.have(ctx)
	for _, l := range [][]int{h.Movies.Disk, h.Movies.Streamed, h.Movies.Wanted} {
		for _, id := range l {
			had[id] = true
		}
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id FROM streamed_titles WHERE kind = 'movie'`); err == nil {
		for rows.Next() {
			var id int
			if rows.Scan(&id) == nil {
				had[id] = true
			}
		}
		rows.Close()
	}
	// Never the same film twice for anyone, even one taken out again.
	if rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id FROM home_autostream_added WHERE kind = 'movie'`); err == nil {
		for rows.Next() {
			var id int
			if rows.Scan(&id) == nil {
				had[id] = true
			}
		}
		rows.Close()
	}
	for _, cand := range list {
		w := whys[cand.ID]
		if w.Match >= autostreamFloor && !had[cand.ID] {
			return cand, w, true
		}
	}
	return c, why, false
}

// autostreamFor adds one film for a person when they're due one. It
// reports what it added (nil for nothing).
func (s *Server) autostreamFor(ctx context.Context, u *auth.User, now time.Time) (*autostreamAdded, error) {
	if s.autostreamThisWeek(ctx, u.ID, now) >= autostreamPerWeek {
		return nil, nil
	}
	c, why, ok := s.autostreamPick(ctx, u)
	if !ok {
		return nil, nil
	}
	actx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	sid, _, err := s.addStreamedTitle(actx, "movie", c.ID, "")
	if err != nil {
		return nil, err
	}
	a := &autostreamAdded{TMDBID: c.ID, Match: why.Match, Because: why.Because, StreamedID: sid}
	if c.movie != nil {
		a.Title, a.Year = c.movie.Title, c.movie.Year()
	}
	a.Title, a.Year, a.Image = s.tmdbBasicsOr(ctx, "movie", c.ID, a.Title, a.Year)
	res, err := s.db.ExecContext(ctx, `INSERT INTO home_autostream_added (user_id, kind, tmdb_id, title, year, image, match, because, streamed_id) VALUES (?, 'movie', ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, c.ID, a.Title, a.Year, a.Image, a.Match, a.Because, sid)
	if err != nil {
		return nil, err
	}
	a.ID, _ = res.LastInsertId()
	a.AddedAt = now.UTC().Format(time.RFC3339)
	go s.afterStreamedChange()
	slog.Info("streams for you: added", "user", u.Username, "title", a.Title, "match", a.Match)
	return a, nil
}

// tmdbBasicsOr is tmdbBasics, keeping what's already known when TMDB
// doesn't answer.
func (s *Server) tmdbBasicsOr(ctx context.Context, kind string, id int, title string, year int) (string, int, string) {
	t, y, img := s.tmdbBasics(ctx, kind, id)
	if t == "" {
		t, y = title, year
	}
	return t, y, img
}

// autostreamJob adds a film for each person who switched Streams for you on
// and is due one, while an administrator allows it and the streamed library
// is set up.
func (s *Server) autostreamJob(ctx context.Context) {
	if !s.autostreamAllowed() || !s.streamedReady() || !s.TMDB().HasAPIKey() {
		return
	}
	list, err := s.Auth.ListAccounts()
	if err != nil {
		return
	}
	now := time.Now()
	for i := range list {
		u := &list[i].User
		if u.Disabled || !s.autostreamOn(ctx, u.ID) {
			continue
		}
		if _, err := s.autostreamFor(ctx, u, now); err != nil {
			slog.Info("streams for you: add", "user", u.Username, "err", err)
		}
	}
}

// ---- the endpoints ----

func (s *Server) autostreamList(ctx context.Context, uid int64) []autostreamAdded {
	out := []autostreamAdded{}
	rows, err := s.db.QueryContext(ctx, `SELECT id, tmdb_id, title, year, image, match, because, streamed_id, added_at, removed_at FROM home_autostream_added
		WHERE user_id = ? ORDER BY added_at DESC, id DESC LIMIT 200`, uid)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var a autostreamAdded
		var removed string
		if rows.Scan(&a.ID, &a.TMDBID, &a.Title, &a.Year, &a.Image, &a.Match, &a.Because, &a.StreamedID, &a.AddedAt, &removed) == nil {
			a.Removed = removed != ""
			out = append(out, a)
		}
	}
	return out
}

// GET /api/home/autostream: whether Streams for you is allowed, set up and
// on for you, this week's count, and what it added for you.
func (s *Server) handleAutostream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := userID(r)
	u := auth.UserFromContext(ctx)
	writeJSON(w, http.StatusOK, map[string]any{
		"allowed":  s.autostreamAllowed(),
		"ready":    s.streamedReady(),
		"admin":    u != nil && u.IsAdmin,
		"on":       s.autostreamOn(ctx, uid),
		"thisWeek": s.autostreamThisWeek(ctx, uid, time.Now()),
		"perWeek":  autostreamPerWeek,
		"floor":    autostreamFloor,
		"added":    s.autostreamList(ctx, uid),
	})
}

// PUT /api/home/autostream {"on": bool}: switch it on or off for yourself.
// Switching it on looks for a first film straight away.
func (s *Server) handleAutostreamSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On bool `json:"on"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	u := auth.UserFromContext(ctx)
	if in.On && !s.autostreamAllowed() {
		writeError(w, http.StatusForbidden, "An administrator hasn't allowed Streams for you yet.")
		return
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO home_autostream (user_id, enabled) VALUES (?, ?)
		ON CONFLICT (user_id) DO UPDATE SET enabled = excluded.enabled`, u.ID, b2i(in.On)); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	var added *autostreamAdded
	if in.On && s.streamedReady() { // the person's own action: also in safe mode (nothing is downloaded)
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		var err error
		added, err = s.autostreamFor(actx, u, time.Now())
		cancel()
		if err != nil {
			slog.Info("streams for you: first add", "user", u.Username, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"on": in.On, "added": added})
}

// PUT /api/home/autostream/household {"on": bool}: an administrator allows
// Streams for you for the household, or stops it for everyone.
func (s *Server) handleAutostreamHousehold(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On bool `json:"on"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	v := ""
	if in.On {
		v = "1"
	}
	if err := s.Settings.Set(keyAutostreamOn, v, false); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"allowed": in.On})
}

// DELETE /api/home/autostream/{id}: take a film Streams for you added for
// you back out of the streamed library. Only ever one it added for you.
func (s *Server) handleAutostreamRemove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't one of yours.")
		return
	}
	ctx := r.Context()
	var sid int64
	var removed string
	if err := s.db.QueryRowContext(ctx, `SELECT streamed_id, removed_at FROM home_autostream_added WHERE id = ? AND user_id = ?`, id, userID(r)).Scan(&sid, &removed); err != nil {
		writeError(w, http.StatusNotFound, "That isn't one Streams for you added for you.")
		return
	}
	if removed == "" && sid > 0 {
		if _, err := s.removeStreamedTitle(ctx, sid); err != nil {
			writeError(w, http.StatusInternalServerError, "It couldn't be taken out: "+strings.TrimSpace(err.Error()))
			return
		}
		go s.afterStreamedChange()
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE home_autostream_added SET removed_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`, id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
