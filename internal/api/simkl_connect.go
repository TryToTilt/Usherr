package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/simkl"
)

// Connecting a Simkl account (Settings > Info, lists and subtitles > Import
// lists). The person gets a short code, enters it at simkl.com/pin, and
// Mediarium collects the access token. No password reaches Mediarium. The
// token is kept encrypted in the settings and never sent to the browser; only
// the code and whether an account is connected are.

// simklPinTTL is how long Mediarium keeps waiting for a code to be entered
// (Simkl's own codes last about as long).
const simklPinTTL = 15 * time.Minute

// errSimklTokenUnreadable means the saved sign-in can't be decrypted (the
// secret.key is not the one the database was saved with).
var errSimklTokenUnreadable = errors.New("the saved Simkl sign-in can't be read")

type simklState struct {
	Connected bool `json:"connected"`
	// Unreadable is true when a sign-in is saved but can't be read, so the
	// account has to be connected again.
	Unreadable  bool `json:"unreadable"`
	HasClientID bool `json:"hasClientId"`
}

func (s *Server) simklState() simklState {
	st := simklState{HasClientID: s.Simkl().HasClientID()}
	tok, err := s.Settings.Get(settings.KeySimklToken)
	switch {
	case err != nil:
		st.Unreadable = true
	case tok != "":
		st.Connected = true
	}
	return st
}

// GET /api/settings/simkl
func (s *Server) handleGetSimkl(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.simklState())
}

// POST /api/settings/simkl/pin: start connecting an account.
func (s *Server) handleStartSimklPin(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	pin, err := s.Simkl().StartPIN(ctx)
	switch {
	case errors.Is(err, simkl.ErrNoClientID):
		writeError(w, http.StatusPreconditionFailed, "Add your Simkl client ID first.")
		return
	case err != nil:
		var rl *simkl.RateLimitError
		if errors.As(err, &rl) {
			writeError(w, http.StatusTooManyRequests, rl.Error()+".")
			return
		}
		writeUpstreamError(w, "start the Simkl sign-in", err)
		return
	}
	s.simklPins.Put(pin.UserCode, pin.UserCode)
	writeJSON(w, http.StatusOK, map[string]any{
		"code":      pin.UserCode,
		"url":       pin.VerificationURL,
		"expiresIn": int(simklPinTTL.Seconds()),
		"interval":  int(pin.Interval.Seconds()),
	})
}

// GET /api/settings/simkl/pin/{code}: has the code been entered yet?
func (s *Server) handlePollSimklPin(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if _, ok := s.simklPins.Get(code); !ok {
		writeError(w, http.StatusGone, "That Simkl code has expired. Start again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	token, err := s.Simkl().CheckPIN(ctx, code)
	var rl *simkl.RateLimitError
	switch {
	case errors.Is(err, simkl.ErrPINExpired):
		s.simklPins.Delete(code)
		writeError(w, http.StatusGone, "That Simkl code has expired. Start again.")
		return
	case errors.As(err, &rl):
		writeJSON(w, http.StatusOK, map[string]any{"done": false}) // too fast: the page just asks again later
		return
	case err != nil:
		writeUpstreamError(w, "check the Simkl sign-in", err)
		return
	case token == "":
		writeJSON(w, http.StatusOK, map[string]any{"done": false})
		return
	}
	s.simklPins.Delete(code)
	if err := s.Settings.Set(settings.KeySimklToken, token, true); err != nil {
		slog.Error("simkl: save the sign-in", "err", err)
		writeError(w, http.StatusInternalServerError, "The Simkl sign-in couldn't be saved.")
		return
	}
	slog.Info("simkl account connected")
	writeJSON(w, http.StatusOK, map[string]any{"done": true, "status": s.simklState()})
}

// DELETE /api/settings/simkl: forget the connected account. Lists already
// added to the library stay; the Simkl lists in Mediarium wait until an
// account is connected again.
func (s *Server) handleDisconnectSimkl(w http.ResponseWriter, r *http.Request) {
	if err := s.Settings.Set(settings.KeySimklToken, "", true); err != nil {
		slog.Error("simkl: forget the sign-in", "err", err)
		writeError(w, http.StatusInternalServerError, "The Simkl account couldn't be disconnected.")
		return
	}
	slog.Info("simkl account disconnected")
	w.WriteHeader(http.StatusNoContent)
}
