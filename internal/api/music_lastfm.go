package api

// Usherr: Last.fm scrobbling, next to ListenBrainz (music_scrobble.go).
//
// An administrator saves the server's Last.fm API key and secret once
// (Settings > Services). Each person then connects their own Last.fm account
// through Last.fm's web sign-in: Usherr sends them to last.fm, Last.fm comes
// back with a token, and Usherr swaps it for a session key, kept encrypted.
// Plays go out from the server: "now playing" when a track starts, a
// scrobble once Last.fm's rule is met (longer than 30 s; half the track or 4
// minutes). Plays Last.fm can't take are kept and sent later, and a like in
// Usherr can love the track on Last.fm.
//
// The flow and the fan-out to both services follow DroppedNeedle (AGPL-3.0),
// backend/services/scrobble_service.py and lastfm_auth_service.py; the
// offline queue and love sync are Usherr's own (DroppedNeedle has neither).

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/lastfm"
)

const (
	keyLastFMKey    = "music.lastfm_key"    // the server's Last.fm API key
	keyLastFMSecret = "music.lastfm_secret" // encrypted; its shared secret

	// lastfmStateTTL is how long a sign-in may take on last.fm.
	lastfmStateTTL = 15 * time.Minute
	// scrobbleRetryEvery is how often kept plays are tried again.
	scrobbleRetryEvery = 15 * time.Minute
)

type lastfmState struct {
	userID  int64
	expires time.Time
}

// lastfmClient is the client for the saved key, ok false while none is saved.
func (s *Server) lastfmClient() (*lastfm.Client, bool) {
	key, _ := s.Settings.Get(keyLastFMKey)
	secret, _ := s.Settings.Get(keyLastFMSecret)
	if key == "" || secret == "" {
		return nil, false
	}
	var opts []lastfm.Option
	s.music.lfMu.Lock()
	if s.music.lfBase != "" {
		opts = append(opts, lastfm.WithBaseURL(s.music.lfBase))
	}
	if s.music.lfHTTP != nil {
		opts = append(opts, lastfm.WithHTTPClient(s.music.lfHTTP))
	}
	s.music.lfMu.Unlock()
	return lastfm.New(key, secret, s.version, opts...), true
}

type lastfmRow struct {
	Session   string
	User      string
	Enabled   bool
	LoveSync  bool
	LastError string
	Submitted int
}

func (s *Server) lastfmRow(userID int64) (lastfmRow, bool, error) {
	var r lastfmRow
	var enc string
	err := s.db.QueryRow(`SELECT session, lf_user, enabled, love_sync, last_error, submitted FROM music_lastfm WHERE user_id = ?`, userID).
		Scan(&enc, &r.User, &r.Enabled, &r.LoveSync, &r.LastError, &r.Submitted)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, fmt.Errorf("read Last.fm connection: %w", err)
	}
	if r.Session, err = s.box.Decrypt(enc); err != nil {
		return r, false, fmt.Errorf("read Last.fm session: %w", err)
	}
	return r, true, nil
}

type lastfmStatus struct {
	Configured bool   `json:"configured"` // the server has a Last.fm key
	Connected  bool   `json:"connected"`
	User       string `json:"user,omitempty"`
	Enabled    bool   `json:"enabled"`
	LoveSync   bool   `json:"loveSync"`
	LastError  string `json:"lastError,omitempty"`
	Submitted  int    `json:"submitted"`
	Pending    int    `json:"pending"`
}

func (s *Server) lastfmStatus(userID int64) (lastfmStatus, error) {
	_, configured := s.lastfmClient()
	st := lastfmStatus{Configured: configured}
	r, ok, err := s.lastfmRow(userID)
	if err != nil || !ok {
		return st, err
	}
	st.Connected, st.User, st.Enabled, st.LoveSync, st.LastError, st.Submitted = true, r.User, r.Enabled, r.LoveSync, r.LastError, r.Submitted
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM music_lastfm_pending WHERE user_id = ?`, userID).Scan(&st.Pending)
	return st, nil
}

// GET /api/music/lastfm
func (s *Server) handleLastFMStatus(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	st, err := s.lastfmStatus(me)
	if err != nil {
		slog.Warn("read Last.fm connection", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the Last.fm settings. Check the log for details.")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// PUT /api/music/lastfm {enabled?, loveSync?}
func (s *Server) handleLastFMSave(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		Enabled  *bool `json:"enabled"`
		LoveSync *bool `json:"loveSync"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Enabled == nil && req.LoveSync == nil) {
		writeError(w, http.StatusBadRequest, "Say whether to send plays, or whether to sync likes.")
		return
	}
	if _, ok, _ := s.lastfmRow(me); !ok {
		writeError(w, http.StatusNotFound, "Connect your Last.fm account first.")
		return
	}
	if req.Enabled != nil {
		_, _ = s.db.Exec(`UPDATE music_lastfm SET enabled = ?, last_error = '' WHERE user_id = ?`, b2i(*req.Enabled), me)
	}
	if req.LoveSync != nil {
		_, _ = s.db.Exec(`UPDATE music_lastfm SET love_sync = ? WHERE user_id = ?`, b2i(*req.LoveSync), me)
	}
	st, _ := s.lastfmStatus(me)
	writeJSON(w, http.StatusOK, st)
}

// DELETE /api/music/lastfm: forget the session and any unsent plays.
func (s *Server) handleLastFMForget(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM music_lastfm_pending WHERE user_id = ?`, me)
	if _, err := s.db.Exec(`DELETE FROM music_lastfm WHERE user_id = ?`, me); err != nil {
		slog.Warn("forget Last.fm connection", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't disconnect Last.fm. Check the log for details.")
		return
	}
	slog.Info("Last.fm disconnected", "user", me)
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/music/lastfm/connect: the last.fm page to send the person to.
func (s *Server) handleLastFMConnect(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	c, ok := s.lastfmClient()
	if !ok {
		writeError(w, http.StatusConflict, "Last.fm isn't set up on this server yet. An administrator adds the API key in Settings > Services.")
		return
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	state := hex.EncodeToString(b)
	s.music.lfMu.Lock()
	if s.music.lfStates == nil {
		s.music.lfStates = map[string]lastfmState{}
	}
	now := time.Now()
	for k, v := range s.music.lfStates {
		if now.After(v.expires) {
			delete(s.music.lfStates, k)
		}
	}
	if len(s.music.lfStates) > 200 {
		s.music.lfStates = map[string]lastfmState{}
	}
	s.music.lfStates[state] = lastfmState{userID: me, expires: now.Add(lastfmStateTTL)}
	s.music.lfMu.Unlock()
	callback := s.publicBase(r) + s.cfg.URLBase + "/api/music/lastfm/callback?" + url.Values{"state": {state}}.Encode()
	writeJSON(w, http.StatusOK, map[string]string{"url": c.AuthPageURL(callback)})
}

// GET /api/music/lastfm/callback?state&token: where last.fm sends the person
// back. It ends on the settings page either way.
func (s *Server) handleLastFMCallback(w http.ResponseWriter, r *http.Request) {
	back := func(result string) {
		http.Redirect(w, r, s.cfg.URLBase+"/settings/services?lastfm="+result+"#lastfm", http.StatusFound)
	}
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	state, token := r.URL.Query().Get("state"), strings.TrimSpace(r.URL.Query().Get("token"))
	s.music.lfMu.Lock()
	st, found := s.music.lfStates[state]
	delete(s.music.lfStates, state)
	s.music.lfMu.Unlock()
	if !found || st.userID != me || time.Now().After(st.expires) || token == "" || len(token) > 128 {
		back("expired")
		return
	}
	c, ok := s.lastfmClient()
	if !ok {
		back("nokey")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	user, key, err := c.Session(ctx, token)
	if err != nil {
		slog.Info("Last.fm sign-in failed", "user", me, "err", err)
		back("failed")
		return
	}
	enc, err := s.box.Encrypt(key)
	if err != nil {
		back("failed")
		return
	}
	if _, err := s.db.Exec(`INSERT INTO music_lastfm (user_id, session, lf_user) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET session = excluded.session, lf_user = excluded.lf_user, enabled = 1, last_error = ''`, me, enc, user); err != nil {
		slog.Warn("save Last.fm connection", "err", err)
		back("failed")
		return
	}
	slog.Info("Last.fm connected", "user", me)
	back("ok")
}

// GET /api/music/lastfm/app: the server's Last.fm application (key shown, secret never).
func (s *Server) handleLastFMAppGet(w http.ResponseWriter, r *http.Request) {
	key, _ := s.Settings.Get(keyLastFMKey)
	secret, _ := s.Settings.Get(keyLastFMSecret)
	writeJSON(w, http.StatusOK, map[string]any{"apiKey": key, "hasSecret": secret != ""})
}

// PUT /api/music/lastfm/app {apiKey, secret?}
func (s *Server) handleLastFMAppSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey *string `json:"apiKey"`
		Secret *string `json:"secret"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.APIKey == nil && req.Secret == nil) {
		writeError(w, http.StatusBadRequest, "Send the Last.fm API key and shared secret.")
		return
	}
	clean := func(v string) (string, bool) {
		v = strings.TrimSpace(v)
		if len(v) > 64 || strings.ContainsAny(v, " \t\r\n") {
			return "", false
		}
		return v, true
	}
	if req.APIKey != nil {
		v, ok := clean(*req.APIKey)
		if !ok {
			writeError(w, http.StatusBadRequest, "That doesn't look like a Last.fm API key.")
			return
		}
		if err := s.Settings.Set(keyLastFMKey, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, "Couldn't save the key. Check the log for details.")
			return
		}
	}
	if req.Secret != nil {
		v, ok := clean(*req.Secret)
		if !ok {
			writeError(w, http.StatusBadRequest, "That doesn't look like a Last.fm shared secret.")
			return
		}
		if err := s.Settings.Set(keyLastFMSecret, v, true); err != nil {
			writeError(w, http.StatusInternalServerError, "Couldn't save the secret. Check the log for details.")
			return
		}
	}
	s.handleLastFMAppGet(w, r)
}

// lastfmTrack is what Last.fm is sent for a track.
func (s *Server) lastfmTrack(trackID, at int64) (lastfm.Track, error) {
	l, err := s.listenFor(trackID, at)
	if err != nil {
		return lastfm.Track{}, err
	}
	return lastfm.Track{Artist: l.ArtistName, Title: l.TrackName, Album: l.ReleaseName, DurationSec: (l.DurationMs + 500) / 1000,
		TrackNumber: l.TrackNumber, Timestamp: at}, nil
}

// lastfmFailed records an error shown next to the settings. A session Last.fm
// refuses switches sending off, so it isn't tried for every track.
func (s *Server) lastfmFailed(userID int64, err error) {
	msg := "Couldn't reach Last.fm. Plays are kept and sent later."
	switch {
	case errors.Is(err, lastfm.ErrBadSession):
		_, _ = s.db.Exec(`UPDATE music_lastfm SET enabled = 0, last_error = ? WHERE user_id = ?`,
			"Last.fm no longer accepts this connection. Connect again to carry on.", userID)
		return
	case errors.Is(err, lastfm.ErrBadKey):
		msg = "Last.fm doesn't accept the server's API key. An administrator can check it in Settings > Services."
	case errors.Is(err, lastfm.ErrRateLimited):
		msg = "Last.fm asked for fewer requests. Plays are kept and sent later."
	}
	_, _ = s.db.Exec(`UPDATE music_lastfm SET last_error = ? WHERE user_id = ?`, msg, userID)
}

// flushLastFM sends the account's kept plays, oldest first, 50 at a time.
// Plays Last.fm refuses as wrong are dropped; the rest wait for later.
func (s *Server) flushLastFM(ctx context.Context, c *lastfm.Client, userID int64, session string) (int, error) {
	_, _ = s.db.Exec(`DELETE FROM music_lastfm_pending WHERE user_id = ? AND id NOT IN
		(SELECT id FROM music_lastfm_pending WHERE user_id = ? ORDER BY listened_at DESC, id DESC LIMIT ?)`, userID, userID, listensKept)
	// Last.fm takes plays up to two weeks old.
	_, _ = s.db.Exec(`DELETE FROM music_lastfm_pending WHERE user_id = ? AND listened_at < ?`, userID, time.Now().Add(-listenMaxAge).Unix())
	total := 0
	for {
		rows, err := s.db.Query(`SELECT id, track_id, listened_at FROM music_lastfm_pending WHERE user_id = ? ORDER BY listened_at, id LIMIT ?`,
			userID, lastfm.MaxScrobbles)
		if err != nil {
			return total, fmt.Errorf("read kept plays: %w", err)
		}
		type kept struct{ id, track, at int64 }
		var batch []kept
		for rows.Next() {
			var k kept
			if err := rows.Scan(&k.id, &k.track, &k.at); err != nil {
				rows.Close()
				return total, fmt.Errorf("read kept plays: %w", err)
			}
			batch = append(batch, k)
		}
		rows.Close()
		if len(batch) == 0 {
			return total, nil
		}
		var send []lastfm.Track
		for _, k := range batch {
			if t, err := s.lastfmTrack(k.track, k.at); err == nil {
				send = append(send, t)
			}
		}
		_, err = c.Scrobble(ctx, session, send)
		var apiErr *lastfm.APIError
		if err != nil && !(errors.As(err, &apiErr) && (apiErr.Code == 6 || apiErr.Code == 7)) {
			return total, err
		}
		for _, k := range batch {
			_, _ = s.db.Exec(`DELETE FROM music_lastfm_pending WHERE id = ?`, k.id)
		}
		if err == nil {
			total += len(send)
			_, _ = s.db.Exec(`UPDATE music_lastfm SET submitted = submitted + ?, last_error = '' WHERE user_id = ?`, len(send), userID)
		}
		if len(batch) < lastfm.MaxScrobbles {
			return total, nil
		}
	}
}

// scrobbleResult is what happened to one play.
type scrobbleResult struct {
	ListenBrainz bool `json:"listenbrainz"`
	LastFM       bool `json:"lastfm"`
}

// nowPlaying tells both services what an account has started playing.
func (s *Server) nowPlaying(ctx context.Context, userID, trackID int64) scrobbleResult {
	var res scrobbleResult
	var wg sync.WaitGroup
	if row, ok, err := s.scrobbleRow(userID); err == nil && ok && row.Enabled {
		if l, err := s.listenFor(trackID, 0); err == nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				if err := s.scrobbler().PlayingNow(cctx, row.Token, l); err != nil {
					slog.Info("ListenBrainz playing now failed", "user", userID, "err", err)
					s.scrobbleFailed(userID, err)
					return
				}
				res.ListenBrainz = true
			}()
		}
	}
	if row, ok, err := s.lastfmRow(userID); err == nil && ok && row.Enabled {
		if c, ok := s.lastfmClient(); ok {
			if t, err := s.lastfmTrack(trackID, 0); err == nil {
				wg.Add(1)
				go func() {
					defer wg.Done()
					cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()
					if err := c.NowPlaying(cctx, row.Session, t); err != nil {
						slog.Info("Last.fm now playing failed", "user", userID, "err", err)
						if !lastfm.Temporary(err) {
							s.lastfmFailed(userID, err)
						}
						return
					}
					res.LastFM = true
				}()
			}
		}
	}
	wg.Wait()
	return res
}

// recordPlay keeps a play that counted in the history and sends it to the
// account's services. playedSec is how long it really played, -1 when the
// caller doesn't say (a Subsonic app that judged it itself).
func (s *Server) recordPlay(ctx context.Context, userID, trackID, listenedAt int64, playedSec float64, source string) scrobbleResult {
	var res scrobbleResult
	t, err := s.MusicRepo.GetTrack(trackID)
	if err != nil {
		return res
	}
	_, _ = s.db.Exec(`INSERT INTO music_play_history (user_id, track_id, played_at, source) VALUES (?, ?, ?, ?)`, userID, trackID, listenedAt, source)
	var wg sync.WaitGroup
	if row, ok, err := s.scrobbleRow(userID); err == nil && ok && row.Enabled {
		if _, err := s.db.Exec(`INSERT INTO music_listens_pending (user_id, track_id, listened_at) VALUES (?, ?, ?)`, userID, trackID, listenedAt); err == nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				unlock := s.music.scrobbleAt.lock(userID)
				defer unlock()
				cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
				defer cancel()
				if _, err := s.flushListens(cctx, userID, row.Token); err != nil {
					slog.Info("ListenBrainz submit failed", "user", userID, "err", err)
					s.scrobbleFailed(userID, err)
					return
				}
				res.ListenBrainz = true
			}()
		}
	}
	lengthSec := float64(t.LengthMs) / 1000
	counts := lengthSec > 30 && (playedSec < 0 || lastfm.ShouldScrobble(lengthSec, playedSec))
	if row, ok, err := s.lastfmRow(userID); err == nil && ok && row.Enabled && counts {
		if c, ok := s.lastfmClient(); ok {
			if _, err := s.db.Exec(`INSERT INTO music_lastfm_pending (user_id, track_id, listened_at) VALUES (?, ?, ?)`, userID, trackID, listenedAt); err == nil {
				wg.Add(1)
				go func() {
					defer wg.Done()
					unlock := s.music.lfAt.lock(userID)
					defer unlock()
					cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
					defer cancel()
					if _, err := s.flushLastFM(cctx, c, userID, row.Session); err != nil {
						slog.Info("Last.fm scrobble failed", "user", userID, "err", err)
						s.lastfmFailed(userID, err)
						return
					}
					res.LastFM = true
				}()
			}
		}
	}
	wg.Wait()
	return res
}

// POST /api/music/scrobble/playing {trackId}
func (s *Server) handleScrobbleNowPlaying(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		TrackID int64 `json:"trackId"`
	}
	if err := decodeJSON(r, &req); err != nil || req.TrackID <= 0 {
		writeError(w, http.StatusBadRequest, "Say which track is playing.")
		return
	}
	if _, err := s.MusicRepo.GetTrack(req.TrackID); err != nil {
		writeError(w, http.StatusNotFound, "That track isn't in your library.")
		return
	}
	writeJSON(w, http.StatusOK, s.nowPlaying(r.Context(), me, req.TrackID))
}

// POST /api/music/scrobble/listen {trackId, listenedAt, playedSec?}: a play
// that counted. listenedAt is the Unix time (seconds) it started; playedSec
// how long it really played (seeking left out).
func (s *Server) handleScrobblePlay(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		TrackID    int64    `json:"trackId"`
		ListenedAt int64    `json:"listenedAt"`
		PlayedSec  *float64 `json:"playedSec"`
	}
	if err := decodeJSON(r, &req); err != nil || req.TrackID <= 0 {
		writeError(w, http.StatusBadRequest, "Say which track was played.")
		return
	}
	now := time.Now()
	at := time.Unix(req.ListenedAt, 0)
	if req.ListenedAt < listenMinTime || at.After(now.Add(listenMaxAhead)) || at.Before(now.Add(-listenMaxAge)) {
		writeError(w, http.StatusBadRequest, "That play time isn't right.")
		return
	}
	if _, err := s.MusicRepo.GetTrack(req.TrackID); err != nil {
		writeError(w, http.StatusNotFound, "That track isn't in your library.")
		return
	}
	played := -1.0
	if req.PlayedSec != nil {
		played = *req.PlayedSec
	}
	writeJSON(w, http.StatusOK, s.recordPlay(r.Context(), me, req.TrackID, req.ListenedAt, played, "web"))
}

// scrobbleRetryJob sends the plays kept while ListenBrainz or Last.fm
// couldn't be reached.
func (s *Server) scrobbleRetryJob(ctx context.Context) {
	if !s.musicEnabled() {
		return
	}
	users := func(q string) []int64 {
		rows, err := s.db.Query(q)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var out []int64
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				out = append(out, id)
			}
		}
		return out
	}
	for _, id := range users(`SELECT DISTINCT p.user_id FROM music_listens_pending p JOIN music_listenbrainz c ON c.user_id = p.user_id WHERE c.enabled = 1`) {
		if row, ok, err := s.scrobbleRow(id); err == nil && ok {
			unlock := s.music.scrobbleAt.lock(id)
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if _, err := s.flushListens(cctx, id, row.Token); err != nil {
				s.scrobbleFailed(id, err)
			}
			cancel()
			unlock()
		}
	}
	c, ok := s.lastfmClient()
	if !ok {
		return
	}
	for _, id := range users(`SELECT DISTINCT p.user_id FROM music_lastfm_pending p JOIN music_lastfm c ON c.user_id = p.user_id WHERE c.enabled = 1`) {
		if row, ok, err := s.lastfmRow(id); err == nil && ok {
			unlock := s.music.lfAt.lock(id)
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if _, err := s.flushLastFM(cctx, c, id, row.Session); err != nil {
				s.lastfmFailed(id, err)
			}
			cancel()
			unlock()
		}
	}
}

// loveOnLastFM mirrors a like to Last.fm when the account asked for that. It
// runs on its own; a failure is only logged.
func (s *Server) loveOnLastFM(userID, trackID int64, love bool) {
	row, ok, err := s.lastfmRow(userID)
	if err != nil || !ok || !row.Enabled || !row.LoveSync {
		return
	}
	c, ok := s.lastfmClient()
	if !ok {
		return
	}
	t, err := s.lastfmTrack(trackID, 0)
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.Love(ctx, row.Session, t.Artist, t.Title, love); err != nil {
			slog.Info("Last.fm love failed", "user", userID, "err", err)
			if !lastfm.Temporary(err) {
				s.lastfmFailed(userID, err)
			}
		}
	}()
}
