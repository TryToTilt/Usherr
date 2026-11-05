package api

// Usherr: Re-Posterr with Plex. A title's copies are listed together in
// art_titles.jf_ids: Jellyfin's items, and Plex's as
// "plex:<server>:<rating key>:<library>:<movie|show>". The picture calls
// below send each to its own server, so choosing, approving, keeping what's
// replaced and putting it back work the same for both.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

type artPlexRef struct {
	server       int64
	key, section string
	kind         string // movie or show
}

func (r artPlexRef) String() string {
	return fmt.Sprintf("plex:%d:%s:%s:%s", r.server, r.key, r.section, r.kind)
}

func parseArtPlex(id string) (artPlexRef, bool) {
	rest, ok := strings.CutPrefix(id, "plex:")
	if !ok {
		return artPlexRef{}, false
	}
	p := strings.Split(rest, ":")
	if len(p) != 4 {
		return artPlexRef{}, false
	}
	srv, err := strconv.ParseInt(p[0], 10, 64)
	if err != nil || p[1] == "" {
		return artPlexRef{}, false
	}
	return artPlexRef{srv, p[1], p[2], p[3]}, true
}

func (s *Server) artPlexServer(id int64) (mediaservers.Server, bool) {
	for _, m := range s.plexServers() {
		if m.ID == id {
			return m, true
		}
	}
	return mediaservers.Server{}, false
}

// artServer is the Jellyfin (or Emby) server Re-Posterr writes to, and
// whether it has anywhere to write at all: Jellyfin, or a Plex.
func (s *Server) artServer() (mediaservers.Server, bool) {
	if srv, ok := s.jellyfinServer(); ok {
		return srv, true
	}
	return mediaservers.Server{}, len(s.plexServers()) > 0
}

// artHasJF is whether srv is a Jellyfin or Emby server (not the empty one
// artServer gives a Plex-only household).
func artHasJF(srv mediaservers.Server) bool { return srv.BaseURL != "" }

// artPlexItems are Plex's films and shows as Re-Posterr's titles. Plex has
// no logos to fill in, so none is asked for.
func (s *Server) artPlexItems(ctx context.Context) ([]artJFItem, error) {
	var out []artJFItem
	for _, srv := range s.plexServers() {
		list, err := s.mediaClient.PlexArtItems(ctx, srv)
		plexAnswered(srv.ID, err)
		if err != nil {
			return nil, err
		}
		for _, it := range list {
			typ := "Movie"
			if it.Kind == "show" {
				typ = "Series"
			}
			ref := artPlexRef{srv.ID, it.Key, it.Section, it.Kind}
			out = append(out, artJFItem{ID: ref.String(), Type: typ, Name: it.Title, Year: it.Year, TMDBID: it.TMDBID, TVDBID: it.TVDBID, HasPoster: it.HasPoster, HasBackdrop: it.HasArt, HasLogo: true})
		}
	}
	return out, nil
}

// artItems are every server's titles: Jellyfin's (when there is one) and
// Plex's.
func (s *Server) artItems(ctx context.Context, srv mediaservers.Server, fresh bool) ([]artJFItem, error) {
	var all []artJFItem
	if artHasJF(srv) {
		list, err := s.artJFItems(ctx, srv, fresh)
		if err != nil {
			return nil, err
		}
		all = append(all, list...)
	}
	plex, err := s.artPlexItems(ctx)
	if err != nil && len(all) == 0 {
		return nil, err
	}
	return append(all, plex...), nil
}

// artSetImage puts a picture on one copy of a title, on its own server.
func (s *Server) artSetImage(ctx context.Context, srv mediaservers.Server, id, typ string, data []byte, mime string) error {
	var err error
	if ref, ok := parseArtPlex(id); ok {
		ps, ok := s.artPlexServer(ref.server)
		if !ok {
			return fmt.Errorf("the Plex server for this title isn't connected")
		}
		err = s.mediaClient.PlexSetImage(ctx, ps, ref.key, ref.section, ref.kind, typ, data, mime)
	} else {
		err = jfSetImage(ctx, srv, id, typ, data, mime)
	}
	if err == nil && typ == "poster" {
		s.badgeTookPoster(id) // Usherr: the new poster is the badges' new original (home_badges.go)
	}
	return err
}

// artGetImage is one copy's picture of the type, from its own server (nil:
// none).
func (s *Server) artGetImage(ctx context.Context, srv mediaservers.Server, id, typ string, maxWidth int) ([]byte, string, error) {
	if ref, ok := parseArtPlex(id); ok {
		ps, ok := s.artPlexServer(ref.server)
		if !ok {
			return nil, "", fmt.Errorf("the Plex server for this title isn't connected")
		}
		return s.mediaClient.PlexImage(ctx, ps, ref.key, typ)
	}
	return jfGetImage(ctx, srv, id, typ, maxWidth)
}

// artImageWidth is the width of one copy's picture (0: none or unknown).
func (s *Server) artImageWidth(ctx context.Context, srv mediaservers.Server, id, typ string) int {
	if _, ok := parseArtPlex(id); ok {
		data, _, err := s.artGetImage(ctx, srv, id, typ, 0)
		if err != nil || len(data) == 0 {
			return 0
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return 0
		}
		return cfg.Width
	}
	return jfImageWidth(ctx, srv, id, typ)
}

// artAnyonePlaying is whether someone is watching on any media server.
func (s *Server) artAnyonePlaying(ctx context.Context, srv mediaservers.Server) bool {
	if artHasJF(srv) {
		if playing, err := s.mediaClient.NowPlaying(ctx, srv); err == nil && len(playing) > 0 {
			return true
		}
	}
	for _, ps := range s.plexServers() {
		if playing, err := s.mediaClient.NowPlaying(ctx, ps); err == nil && len(playing) > 0 {
			return true
		}
	}
	return false
}

// artServerKey says whether a change may be for that server: "" (every
// one), "jellyfin" or "plex".
func artServerKey(k string) bool { return k == "" || k == "jellyfin" || k == "plex" }

// artServerOf is which server a copy is on: "plex" or "jellyfin".
func artServerOf(id string) string {
	if _, ok := parseArtPlex(id); ok {
		return "plex"
	}
	return "jellyfin"
}

// artServerName is a copy's server as the styles name it.
func artServerName(id string) string {
	if artServerOf(id) == "plex" {
		return "Plex"
	}
	return "Jellyfin"
}

// artIDsFor are a title's copies on the server ("" every one).
func artIDsFor(jfIDs, server string) []string {
	var out []string
	for _, id := range strings.Split(jfIDs, ",") {
		if id != "" && (server == "" || artServerOf(id) == server) {
			out = append(out, id)
		}
	}
	return out
}

// artServersOf are the servers a title is on, for the picker.
func artServersOf(jfIDs string) []string {
	out := []string{}
	for _, k := range []string{"jellyfin", "plex"} {
		if len(artIDsFor(jfIDs, k)) > 0 {
			out = append(out, k)
		}
	}
	return out
}

// artLibraryOf is a copy's library as the styles name it: Jellyfin's by its
// name, Plex's as "Plex: <name>".
func (s *Server) artLibraryOf(ctx context.Context, srv mediaservers.Server, id string) string {
	ref, ok := parseArtPlex(id)
	if !ok {
		return jfLibraryOf(ctx, srv, id)
	}
	ps, ok := s.artPlexServer(ref.server)
	if !ok {
		return ""
	}
	libs, err := s.mediaClient.PlexLibraries(ctx, ps)
	if err != nil {
		return ""
	}
	for _, l := range libs {
		if l.ID == ref.section {
			return "Plex: " + l.Title
		}
	}
	return ""
}

// artPlexLibraryNames are the style designer's Plex entries: Plex as a
// whole, Jellyfin as a whole (when both are there), and each Plex library.
func (s *Server) artPlexLibraryNames(ctx context.Context) []string {
	servers := s.plexServers()
	if len(servers) == 0 {
		return nil
	}
	var out []string
	if _, jf := s.jellyfinServer(); jf {
		out = append(out, "Jellyfin")
	}
	out = append(out, "Plex")
	for _, ps := range servers {
		libs, err := s.mediaClient.PlexLibraries(ctx, ps)
		if err != nil {
			continue
		}
		for _, l := range libs {
			if l.Type == "movie" || l.Type == "show" {
				out = append(out, "Plex: "+l.Title)
			}
		}
	}
	return out
}
