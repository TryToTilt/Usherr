package api

// Usherr: Real TV channels refresh themselves each night. Between 4 and 6 AM
// the day before is fetched from the listings, matched, and filled from the
// remembered choices (home_tvreplay_memory.go): the AI is asked only about
// slots it has never seen, so a refresh usually costs nothing. A channel the
// listings no longer have keeps the day it had.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/tv"
)

const keyRealTVRefreshed = "home.realtv.refreshed" // the day of the last nightly refresh

// realTVRefreshJob runs the nightly refresh once a day, early in the morning.
func (s *Server) realTVRefreshJob(ctx context.Context) {
	now := time.Now()
	if now.Hour() < 4 || now.Hour() >= 6 {
		return
	}
	today := now.Format("2006-01-02")
	if v, _ := s.Settings.Get(keyRealTVRefreshed); v == today {
		return
	}
	_ = s.Settings.Set(keyRealTVRefreshed, today, false)
	n, err := s.refreshRealTV(ctx, now.AddDate(0, 0, -1))
	slog.Info("real tv: nightly refresh", "channels", n, "err", err)
}

// refreshRealTV gives every daily Real TV channel the given day, as it aired.
// It says how many channels changed.
func (s *Server) refreshRealTV(ctx context.Context, day time.Time) (int, error) {
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	repo := s.tvRepo()
	chans, err := repo.Channels()
	if err != nil {
		return 0, err
	}
	// One guide download per zip code and provider.
	type group struct {
		zip, lineup string
		chans       []tv.Channel
	}
	groups := map[string]*group{}
	var order []string
	for _, ch := range chans {
		src := ch.Rules.RealTV
		if src == nil || !src.Daily || src.GuideID == "" || len(ch.Rules.Fixed) == 0 {
			continue
		}
		k := src.Zip + "|" + src.Lineup
		if groups[k] == nil {
			groups[k] = &group{zip: src.Zip, lineup: src.Lineup}
			order = append(order, k)
		}
		groups[k].chans = append(groups[k].chans, ch)
	}
	// Only admins make Real TV channels; the refresh asks the AI on their behalf.
	system := &auth.User{IsAdmin: true}
	changed := 0
	var lastErr error
	for _, k := range order {
		g := groups[k]
		var ids []string
		for _, ch := range g.chans {
			ids = append(ids, ch.Rules.RealTV.GuideID)
		}
		job := &replayJob{}
		if err := s.buildReplay(ctx, job, system, g.zip, day, g.lineup, ids, nil, false); err != nil {
			lastErr = err
			continue
		}
		byGuide := map[string]replayNetwork{}
		for _, n := range job.Networks {
			byGuide[n.GuideID] = n
		}
		for _, ch := range g.chans {
			n, ok := byGuide[ch.Rules.RealTV.GuideID]
			if !ok || len(n.Slots) == 0 {
				continue // not in the listings that day: the channel keeps its day
			}
			var fixed []tv.FixedItem
			for _, sl := range n.Slots {
				fixed = append(fixed, sl.FixedItem)
			}
			ch.Rules.Fixed = fixed
			ch.Rules.FixedFrom = fmt.Sprintf("%s %s, %s (%s)", n.Network, n.CallSign, day.Format("2006-01-02"), g.zip)
			if _, err := repo.Save(ch); err != nil {
				lastErr = err
				continue
			}
			s.rebuildTVChannel(ch.ID)
			changed++
		}
	}
	return changed, lastErr
}

// POST /api/tv/replay/refresh: refresh the daily Real TV channels now, with
// yesterday (the nightly refresh, on demand).
func (s *Server) handleTVReplayRefresh(w http.ResponseWriter, r *http.Request) {
	daily := 0
	if chans, err := s.tvRepo().Channels(); err == nil {
		for _, ch := range chans {
			if src := ch.Rules.RealTV; src != nil && src.Daily && src.GuideID != "" && len(ch.Rules.Fixed) > 0 {
				daily++
			}
		}
	}
	if daily == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"message": "No Real TV channels refresh themselves yet. Channels made before this version don't: delete them and make them again once."})
		return
	}
	s.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
		defer cancel()
		n, err := s.refreshRealTV(ctx, time.Now().AddDate(0, 0, -1))
		slog.Info("real tv: refresh", "channels", n, "err", err)
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"message": fmt.Sprintf("Refreshing %d Real TV channels with yesterday. It takes a few minutes; the guide updates as each one is done.", daily)})
}
