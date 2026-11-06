package api

// Usherr: who may use the Subsonic API (subsonic.go).
//
// Music apps sign in with the account's app password: the one password that
// e-reader apps already use (internal/ereader, ereader_auth.go), never the
// account's own password. There is one store and one set of wrong-password
// counters for both. Subsonic's token sign-in sends md5(password + salt), so
// the server must read the password back: it is kept encrypted with the
// server key besides its hash. A password made before that was kept works
// with p= and apiKey only; making a new one fixes token sign-in. Phone apps
// are off for each account until the person switches them on.
//
// The sign-in rules are adapted from DroppedNeedle (AGPL-3.0),
// backend/api/compat/subsonic/auth.py.

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/ereader"
	"github.com/rdborg/mediarium/internal/subsonic"
)

const keySubsonicOn = "music.subsonic" // "0" turns the Subsonic API off for everyone

func (s *Server) subsonicOn() bool {
	v, _ := s.Settings.Get(keySubsonicOn)
	return v != "0"
}

// tokenVariants are the ways an app may hold the password: as made
// ("abcd-efgh-…"), without the dashes, and in capitals (phone keyboards).
func tokenVariants(pw string) []string {
	n := ereader.NormalizePassword(pw)
	return []string{pw, n, strings.ToUpper(pw), strings.ToUpper(n)}
}

// subsonicUser signs a request in. On failure code is a Subsonic error code
// and msg says why.
func (s *Server) subsonicUser(r *http.Request) (u *auth.User, code int, msg string) {
	rt := s.ereaderState()
	ipKey := s.ereaderLimitKey(r)
	if !rt.failIP.Allow(ipKey) {
		return nil, subsonic.ErrWrongAuth, "Too many wrong tries. Wait a few minutes and try again."
	}
	c, code, msg := subsonic.ReadCredentials(r.Form)
	if code != 0 {
		return nil, code, msg
	}
	userKey := strings.ToLower(strings.TrimSpace(c.User))
	if len(userKey) > 128 {
		userKey = userKey[:128]
	}
	if userKey != "" && !rt.failUser.Allow(userKey) {
		return nil, subsonic.ErrWrongAuth, "Too many wrong tries for this account. Wait a few minutes and try again."
	}
	const wrong = "Wrong user name or app password. Use the app password from Usherr (Settings > Services > Phone apps), not your own password."
	fail := func(code int, msg string) (*auth.User, int, string) {
		rt.failIP.RecordFailure(ipKey)
		if userKey != "" {
			rt.failUser.RecordFailure(userKey)
		}
		slog.Warn("subsonic: sign-in failed", "user", userKey, "ip", s.clientIP(r))
		return nil, code, msg
	}
	var userID int64
	switch {
	case c.APIKey != "":
		id, err := s.EReader.UserForOPDSPassword(c.APIKey)
		if err != nil {
			return fail(subsonic.ErrBadAPIKey, "That API key isn't valid.")
		}
		userID = id
	default:
		user, err := s.Auth.UserByName(c.User)
		if err != nil {
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				slog.Error("subsonic: look up account", "err", err)
			}
			s.EReader.CheckOPDS(0, c.Password) // the same work as for a real account
			return fail(subsonic.ErrWrongAuth, wrong)
		}
		userID = user.ID
		if c.Password != "" {
			if !s.EReader.CheckOPDS(userID, c.Password) {
				return fail(subsonic.ErrWrongAuth, wrong)
			}
		} else {
			secret, err := s.EReader.OPDSSecret(userID)
			if err != nil {
				slog.Warn("subsonic: read app password", "err", err)
			}
			if secret == "" {
				return fail(subsonic.ErrWrongAuth, "This app password was made before music apps could use it. Make a new one in Usherr (Settings > Services > Phone apps).")
			}
			ok := false
			for _, v := range tokenVariants(secret) {
				if subsonic.TokenMatches(v, c.Salt, c.Token) {
					ok = true
				}
			}
			if !ok {
				return fail(subsonic.ErrWrongAuth, wrong)
			}
		}
	}
	u, err := s.Auth.GetUser(userID)
	if err != nil || u == nil || u.Disabled {
		return fail(subsonic.ErrWrongAuth, "This account is turned off.")
	}
	if !s.subsonicEnabledFor(userID) {
		return nil, subsonic.ErrNotAuthorized, "Phone apps are switched off for this account. Switch them on in Usherr: Settings > Services > Phone apps."
	}
	rt.failIP.RecordSuccess(ipKey)
	return u, 0, ""
}

func (s *Server) subsonicEnabledFor(userID int64) bool {
	var on int
	return s.db.QueryRow(`SELECT enabled FROM subsonic_users WHERE user_id = ?`, userID).Scan(&on) == nil && on != 0
}

type subsonicStatus struct {
	ServerOn bool   `json:"serverOn"`
	Enabled  bool   `json:"enabled"`
	Username string `json:"username"`
	// The account's app password, shared with e-reader apps.
	HasPassword     bool   `json:"hasPassword"`
	PasswordCreated string `json:"passwordCreated,omitempty"`
	TokenReady      bool   `json:"tokenReady"` // made since music apps can use it fully
}

func (s *Server) subsonicStatus(userID int64) (subsonicStatus, error) {
	st := subsonicStatus{ServerOn: s.subsonicOn(), Enabled: s.subsonicEnabledFor(userID)}
	if u, err := s.Auth.GetUser(userID); err == nil && u != nil {
		st.Username = u.Username
	}
	a, err := s.EReader.Get(userID)
	if err != nil {
		return st, err
	}
	st.HasPassword, st.PasswordCreated = a.OPDS, a.OPDSCreated
	if a.OPDS {
		secret, _ := s.EReader.OPDSSecret(userID)
		st.TokenReady = secret != ""
	}
	return st, nil
}

// GET /api/music/subsonic
func (s *Server) handleSubsonicStatus(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	st, err := s.subsonicStatus(me)
	if err != nil {
		slog.Warn("read phone app settings", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the phone app settings. Check the log for details.")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// PUT /api/music/subsonic {enabled}
func (s *Server) handleSubsonicSave(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "Say whether phone apps may sign in.")
		return
	}
	if _, err := s.db.Exec(`INSERT INTO subsonic_users (user_id, enabled) VALUES (?, ?) ON CONFLICT(user_id) DO UPDATE SET enabled = excluded.enabled`, me, b2i(*req.Enabled)); err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't save the setting. Check the log for details.")
		return
	}
	st, _ := s.subsonicStatus(me)
	writeJSON(w, http.StatusOK, st)
}

// POST /api/music/subsonic/password: a new app password for reading and
// music apps (it replaces the e-reader password too), shown once.
func (s *Server) handleNewAppPassword(w http.ResponseWriter, r *http.Request) {
	// The password keeps working without the key, so a key can't make one.
	if !requireSession(w, r) {
		return
	}
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	pw, err := s.EReader.NewOPDSPassword(me)
	if err != nil {
		slog.Error("subsonic: new app password", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't make the password. Try again.")
		return
	}
	slog.Info("app password made", "user", me)
	st, _ := s.subsonicStatus(me)
	writeJSON(w, http.StatusOK, map[string]any{"status": st, "password": pw})
}

// DELETE /api/music/subsonic/password: no app password any more (e-reader
// apps stop working too).
func (s *Server) handleRemoveAppPassword(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	if err := s.EReader.RemoveOPDS(me); err != nil {
		slog.Error("subsonic: remove app password", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't remove it. Check the log for details.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errNoSuchItem = errors.New("not found")
