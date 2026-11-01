package api

// Usherr: Live TV in Plex. "Add to Plex" (beside "Add to Jellyfin") sets
// Plex's Live TV & DVR up itself: Usherr answers as an HDHomeRun tuner at
// the root of its address (where Plex looks for one: /discover.json,
// /lineup.json), Plex adds it, makes a DVR of it with Usherr's XMLTV guide
// and loads the guide. The root addresses answer only once Add to Plex has
// been pressed (keyTVPlexOn); the streams they list carry the feed's key.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

const keyTVPlexOn = "home.tv.plex" // "1": the tuner answers at the root, for Plex

func (s *Server) tvPlexOn() bool { v, _ := s.Settings.Get(keyTVPlexOn); return v == "1" }

func (s *Server) registerTVPlexRoutes(root *http.ServeMux) {
	gate := func(h func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !s.tvPlexOn() {
				http.NotFound(w, r)
				return
			}
			h(w, r)
		}
	}
	root.HandleFunc("GET /discover.json", gate(func(w http.ResponseWriter, r *http.Request) {
		s.hdhrDiscover(w, strings.TrimRight(tvFeedBase(r), "/"))
	}))
	root.HandleFunc("GET /lineup.json", gate(func(w http.ResponseWriter, r *http.Request) {
		s.hdhrLineup(w, strings.TrimRight(tvFeedBase(r), "/")+"/api/tvfeed/"+s.tvFeedKey())
	}))
	root.HandleFunc("GET /lineup_status.json", gate(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ScanInProgress": 0, "ScanPossible": 1, "Source": "Cable", "SourceList": []string{"Cable"}})
	}))
	root.HandleFunc("POST /lineup.post", gate(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
}

// POST /api/tv/plex (admin): Usherr's channels and guide in Plex's Live TV.
func (s *Server) handleTVPlex(w http.ResponseWriter, r *http.Request) {
	servers := s.plexServersAll() // asked for just now
	if len(servers) == 0 {
		writeError(w, http.StatusPreconditionFailed, "Connect Plex first, under Settings, Media servers.")
		return
	}
	base := strings.TrimRight(s.feedAddress(r), "/")
	if base == "" {
		writeError(w, http.StatusPreconditionFailed, "Set Usherr's address on your network under Settings > Extras first, so Plex can reach it.")
		return
	}
	if err := s.Settings.Set(keyTVPlexOn, "1", false); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	xmltv := base + "/api/tvfeed/" + s.tvFeedKey() + "/plex.xml" // channels named as the tuner names them
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	var msgs []string
	for _, srv := range servers {
		msg, err := s.tvPlexAdd(ctx, srv, base, xmltv)
		if err != nil {
			writeError(w, http.StatusBadGateway, srv.Name+": "+err.Error()+" In Plex, Live TV & DVR needs Plex Pass.")
			return
		}
		msgs = append(msgs, msg)
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": strings.Join(msgs, " ")})
}

// tvPlexAdd sets one Plex up (or, when it has Usherr's tuner already, has
// it read the guide again).
func (s *Server) tvPlexAdd(ctx context.Context, srv mediaservers.Server, base, xmltv string) (string, error) {
	dvrs, err := s.mediaClient.PlexDVRs(ctx, srv)
	if err != nil {
		return "", fmt.Errorf("reading Plex's DVRs: %w", err)
	}
	lineup := mediaservers.PlexXMLTVLineup(xmltv, "Usherr TV")
	device := ""
	for _, d := range dvrs {
		for _, dev := range d.Devices {
			if strings.TrimRight(dev.URI, "/") != base {
				continue
			}
			if d.Lineup == lineup {
				n := dev.On
				if n == 0 { // switched on already: the map isn't sent again
					var err error
					if n, err = s.tvPlexChannels(ctx, srv, dev.UUID, lineup); err != nil {
						return "", err
					}
				}
				if err := s.mediaClient.PlexReloadGuide(ctx, srv, d.Key); err != nil {
					return "", fmt.Errorf("having Plex read the guide: %w", err)
				}
				return fmt.Sprintf("%s has Usherr TV already: %d channels on, and the guide is being read again.", srv.Name, n), nil
			}
			// Usherr's DVR with another guide (an earlier one): made again
			// with this one, on the same tuner.
			if err := s.mediaClient.PlexDeleteDVR(ctx, srv, d.Key); err != nil {
				return "", fmt.Errorf("replacing Usherr's DVR: %w", err)
			}
			device = dev.UUID
		}
	}
	if device == "" {
		var err error
		if device, err = s.mediaClient.PlexAddTuner(ctx, srv, base); err != nil {
			return "", fmt.Errorf("adding the tuner: %w", err)
		}
	}
	key, err := s.mediaClient.PlexAddDVR(ctx, srv, device, lineup)
	if err != nil {
		return "", fmt.Errorf("making the DVR: %w", err)
	}
	if key == "" { // Plex didn't say: the DVR on Usherr's tuner
		if list, err := s.mediaClient.PlexDVRs(ctx, srv); err == nil {
			for _, d := range list {
				for _, dev := range d.Devices {
					if dev.UUID == device {
						key = d.Key
					}
				}
			}
		}
	}
	n, err := s.tvPlexChannels(ctx, srv, device, lineup)
	if err != nil {
		return "", err
	}
	if key != "" {
		_ = s.mediaClient.PlexReloadGuide(ctx, srv, key)
	}
	return fmt.Sprintf("%s has Usherr TV: %d channels on, in Live TV once Plex has read the guide (a few minutes).", srv.Name, n), nil
}

// tvPlexChannels switches the tuner's channels on, each linked to its
// guide channel, as Plex's own setup does.
func (s *Server) tvPlexChannels(ctx context.Context, srv mediaservers.Server, device, lineup string) (int, error) {
	tuner, err := s.mediaClient.PlexTunerKey(ctx, srv, device)
	if err != nil {
		return 0, fmt.Errorf("finding the tuner: %w", err)
	}
	n, err := s.mediaClient.PlexMapChannels(ctx, srv, tuner, lineup)
	if err != nil {
		return 0, fmt.Errorf("switching the channels on: %w", err)
	}
	return n, nil
}
