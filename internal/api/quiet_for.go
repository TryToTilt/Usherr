package api

import (
	"context"
	"net/http"
	"time"

	"github.com/rdborg/mediarium/internal/settings"
)

// "Quiet for a while": a button that holds the heavy background work (the
// searches, the library refreshes, the subtitle sweep, picture fetching) for a
// few hours, for example while a film is playing from a small server or a
// game is running on the same computer. Downloads that are running or waiting,
// imports, backups and what a person starts by hand are not held. The time is
// kept in the settings, so a restart doesn't end it early.

const maxQuietMinutes = 72 * 60

// quietUntil is when the quiet time ends, and whether it is on now.
func (s *Server) quietUntil() (until time.Time, active bool) {
	v, _ := s.Settings.Get(settings.KeyQuietUntil)
	if v == "" {
		return time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false
	}
	return until, time.Now().Before(until)
}

// quietRecheck is how often a held job looks again whether the quiet time is
// over, so ending it early lets the jobs go at once.
var quietRecheck = 30 * time.Second

// heavy wraps a background job so it waits out the quiet time and then runs. A
// run that came due during the quiet time is delayed, not dropped: with a long
// interval (a day or a week) dropping it would cost the whole interval.
func (s *Server) heavy(run func(context.Context)) func(context.Context) {
	return func(ctx context.Context) {
		if !s.waitOutQuiet(ctx) {
			return
		}
		run(ctx)
	}
}

// waitOutQuiet returns when the quiet time is over (at once when it is not on),
// or false when ctx ended first.
func (s *Server) waitOutQuiet(ctx context.Context) bool {
	for {
		until, active := s.quietUntil()
		if !active {
			return true
		}
		wait := min(time.Until(until), quietRecheck)
		t := time.NewTimer(max(wait, time.Millisecond))
		select {
		case <-ctx.Done():
			t.Stop()
			return false
		case <-t.C:
		}
	}
}

type quietAnswer struct {
	Active bool   `json:"active"`
	Until  string `json:"until,omitempty"` // RFC 3339
}

func (s *Server) quietAnswer() quietAnswer {
	until, active := s.quietUntil()
	if !active {
		return quietAnswer{}
	}
	return quietAnswer{Active: true, Until: until.UTC().Format(time.RFC3339)}
}

func (s *Server) handleGetQuiet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.quietAnswer())
}

// handlePutQuiet starts the quiet time for the given number of minutes, or
// ends it with 0.
func (s *Server) handlePutQuiet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Minutes *int `json:"minutes"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Minutes == nil || *req.Minutes < 0 || *req.Minutes > maxQuietMinutes {
		writeError(w, http.StatusBadRequest, "Send the number of minutes to be quiet for (0 to end it, 4320 at most).")
		return
	}
	val := ""
	if *req.Minutes > 0 {
		val = time.Now().Add(time.Duration(*req.Minutes) * time.Minute).UTC().Format(time.RFC3339)
	}
	if err := s.Settings.Set(settings.KeyQuietUntil, val, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.quietAnswer())
}
