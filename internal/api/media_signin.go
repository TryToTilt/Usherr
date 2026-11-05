package api

// Usherr: friends sign in with the Jellyfin or Plex account they already
// have, as in Seerr. Both are off until an administrator switches them on
// (Settings, Accounts, "Sign in with Jellyfin or Plex").
//
//   - Jellyfin: the sign-in form's name and password are checked by the
//     Jellyfin server Usherr is connected to. A name that has a password here
//     keeps signing in here; it is never handed to Jellyfin.
//   - Plex: "Sign in with Plex" on the sign-in page. The Plex account must be
//     able to see the Plex server Usherr is connected to (its owner, or a
//     friend it is shared with).
//
// The first sign-in makes an account (a member unless the administrator chose
// viewer); later ones find it by the Jellyfin or Plex account's own id, never
// by name. The password and the Plex token are not kept.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/httpsec"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

const keyMediaSignIn = "usherr.media_signin"

type mediaSignInConfig struct {
	Jellyfin bool   `json:"jellyfin"`
	Plex     bool   `json:"plex"`
	Role     string `json:"role"` // the role of a new account: member (default) or viewer
}

func (s *Server) mediaLogin() mediaSignInConfig {
	c := mediaSignInConfig{Role: auth.RoleMember}
	if raw, err := s.Settings.Get(keyMediaSignIn); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	if c.Role != auth.RoleViewer {
		c.Role = auth.RoleMember
	}
	return c
}

func (s *Server) handleGetMediaSignIn(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mediaLogin())
}

func (s *Server) handlePutMediaSignIn(w http.ResponseWriter, r *http.Request) {
	var c mediaSignInConfig
	if err := decodeJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if c.Role != auth.RoleViewer {
		c.Role = auth.RoleMember
	}
	if c.Jellyfin && s.mediaServerOf(mediaservers.KindJellyfin) == nil {
		writeError(w, http.StatusBadRequest, "Connect a Jellyfin server first (Settings, Media servers).")
		return
	}
	if c.Plex && s.mediaServerOf(mediaservers.KindPlex) == nil {
		writeError(w, http.StatusBadRequest, "Connect a Plex server first (Settings, Media servers).")
		return
	}
	raw, _ := json.Marshal(c)
	if err := s.Settings.Set(keyMediaSignIn, string(raw), false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// GET /api/auth/media-signin (public): which buttons and hints the sign-in page shows.
func (s *Server) handlePublicMediaSignIn(w http.ResponseWriter, r *http.Request) {
	c := s.mediaLogin()
	writeJSON(w, http.StatusOK, map[string]bool{
		"jellyfin": c.Jellyfin && s.mediaServerOf(mediaservers.KindJellyfin) != nil,
		"plex":     c.Plex && s.mediaServerOf(mediaservers.KindPlex) != nil,
	})
}

// mediaServerOf is the first enabled media server of a kind, nil when none.
func (s *Server) mediaServerOf(kind mediaservers.Kind) *mediaservers.Server {
	if s.MediaServers == nil {
		return nil
	}
	list, err := s.MediaServers.List()
	if err != nil {
		return nil
	}
	for i := range list {
		if list[i].Kind == kind && list[i].Enabled {
			return &list[i]
		}
	}
	return nil
}

// jellyfinSignInOn reports whether a typed name goes to Jellyfin: the switch
// is on, and the name has no password account here.
func (s *Server) jellyfinSignInOn(name string) bool {
	if !s.mediaLogin().Jellyfin || s.mediaServerOf(mediaservers.KindJellyfin) == nil {
		return false
	}
	known, err := s.Auth.UserByName(strings.TrimSpace(name))
	return err != nil || known.Source == auth.SourceJellyfin
}

// jellyfinSignIn checks a name and password with Jellyfin and returns the
// account for that Jellyfin user.
func (s *Server) jellyfinSignIn(ctx context.Context, username, password string) (*auth.User, error) {
	srv := s.mediaServerOf(mediaservers.KindJellyfin)
	if srv == nil {
		return nil, auth.ErrInvalidCredentials
	}
	clientID, err := s.mediaClientID()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	who, err := s.mediaClient.SignInUser(ctx, mediaservers.KindJellyfin, srv.BaseURL, clientID, username, password)
	if err != nil {
		slog.Info("auth: Jellyfin sign-in refused", "err", err)
		var ue *mediaservers.UserError
		if errors.As(err, &ue) && strings.Contains(ue.Error(), "Wrong username or password") {
			return nil, auth.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("%w: %v", errMediaUnreachable, err)
	}
	return s.accountForMedia(auth.SourceJellyfin, who.ID, who.Name)
}

var errMediaUnreachable = errors.New("media server unreachable")

// accountForMedia finds or makes the account of a Jellyfin or Plex user.
func (s *Server) accountForMedia(source, subject, name string) (*auth.User, error) {
	u, err := s.Auth.UserByIdentity(source, subject)
	switch {
	case err == nil:
		if u.Disabled {
			return nil, auth.ErrAccountDisabled
		}
		return u, nil
	case errors.Is(err, auth.ErrNoIdentity):
		created, err := s.Auth.CreateExternalUser(auth.ExternalProfile{
			Provider: source, Subject: subject, Username: name, Name: name, Role: s.mediaLogin().Role, AllowSuffix: true,
		})
		if err != nil {
			return nil, err
		}
		slog.Info("auth: account made on a first media server sign-in", "user", created.Username, "via", source)
		return created, nil
	}
	return nil, err
}

// ---- Sign in with Plex ----

type plexLoginPin struct {
	pinID    int64
	clientID string
}

const plexLoginTTL = 10 * time.Minute

var plexLoginPins = &mediaservers.Pending[plexLoginPin]{TTL: plexLoginTTL}

// POST /api/auth/plex/pin (public): starts a Plex sign-in.
func (s *Server) handleStartPlexLogin(w http.ResponseWriter, r *http.Request) {
	if !s.mediaLogin().Plex || s.mediaServerOf(mediaservers.KindPlex) == nil {
		writeError(w, http.StatusForbidden, "Signing in with Plex isn't switched on here.")
		return
	}
	ipKey := httpsec.LimitKey(s.clientIP(r))
	if !s.LoginLimiter.Reserve(ipKey) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-in attempts. Wait a few minutes and try again.")
		return
	}
	s.LoginLimiter.Refund(ipKey) // starting is not a failed try
	clientID, err := s.mediaClientID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't start Sign in with Plex.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	pin, err := s.plexTV(clientID).CreatePIN(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Plex couldn't be reached. Try again in a moment.")
		return
	}
	plexLoginPins.Put(strconv.FormatInt(pin.ID, 10), plexLoginPin{pinID: pin.ID, clientID: clientID})
	writeJSON(w, http.StatusOK, map[string]any{"pinId": pin.ID, "authUrl": mediaservers.PlexAuthURL(clientID, pin.Code)})
}

// POST /api/auth/plex/pin/{pinId} (public): finishes a Plex sign-in once the
// person said yes on plex.tv. {"waiting": true} until then.
func (s *Server) handleCheckPlexLogin(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("pinId")
	sess, ok := plexLoginPins.Get(key)
	if !ok {
		writeError(w, http.StatusNotFound, "This Plex sign-in has expired. Start again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	tv := s.plexTV(sess.clientID)
	pin, err := tv.CheckPIN(ctx, sess.pinID)
	if errors.Is(err, mediaservers.ErrPlexPINExpired) {
		plexLoginPins.Delete(key)
		writeError(w, http.StatusNotFound, "This Plex sign-in has expired. Start again.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "Plex couldn't be reached. Try again in a moment.")
		return
	}
	if pin.AuthToken == "" {
		writeJSON(w, http.StatusOK, map[string]any{"waiting": true})
		return
	}
	plexLoginPins.Delete(key)
	ipKey := httpsec.LimitKey(s.clientIP(r))
	user, why, err := s.plexAccountFor(ctx, tv, pin.AuthToken)
	if err != nil {
		slog.Error("auth: Plex sign-in failed", "err", err)
		writeError(w, http.StatusBadGateway, "Plex couldn't be reached. Try again in a moment.")
		return
	}
	if why != "" {
		slog.Warn("auth: Plex sign-in refused", "why", why, "ip", s.clientIP(r))
		writeError(w, http.StatusForbidden, why)
		return
	}
	st, err := s.Auth.TwoStepOf(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't sign you in. Try again.")
		return
	}
	if st.Enabled {
		s.askForCode(w, user)
		return
	}
	s.LoginLimiter.RecordSuccess(ipKey)
	s.finishSignInVia(w, r, user, auth.MethodPlex, nil)
}

// plexAccountFor is the account of the Plex user a token belongs to, or why
// they may not sign in.
func (s *Server) plexAccountFor(ctx context.Context, tv *mediaservers.PlexTV, token string) (*auth.User, string, error) {
	srv := s.mediaServerOf(mediaservers.KindPlex)
	if srv == nil {
		return nil, "Signing in with Plex isn't switched on here.", nil
	}
	acct, err := tv.Account(ctx, token)
	if err != nil {
		return nil, "", err
	}
	if acct.ID == 0 {
		return nil, "", errors.New("plex.tv gave no account id")
	}
	servers, err := tv.Servers(ctx, token)
	if err != nil {
		return nil, "", err
	}
	if !plexCanSee(servers, srv.ServerID) {
		return nil, "Your Plex account doesn't have access to this Plex server. Ask the owner to share it with you.", nil
	}
	name := acct.Username
	if name == "" {
		name = acct.Title
	}
	u, err := s.accountForMedia(auth.SourcePlex, strconv.FormatInt(acct.ID, 10), name)
	if errors.Is(err, auth.ErrAccountDisabled) {
		return nil, disabledMessage, nil
	}
	return u, "", err
}

// plexCanSee reports whether the account's servers include ours.
func plexCanSee(servers []mediaservers.PlexResource, machineID string) bool {
	if machineID == "" {
		return false
	}
	for _, r := range servers {
		if r.ClientIdentifier == machineID {
			return true
		}
	}
	return false
}
