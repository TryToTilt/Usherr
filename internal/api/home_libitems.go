package api

// Usherr: every film and show the media servers have, as one list, for the
// collections Usherr pushes (home_collections.go) and the badges it draws on
// posters (home_badges.go): ids to match titles by, genres, rating, when each
// was added, its poster's tag (it changes when the poster does) and, for
// films, what the file holds (picture size, HDR, sound).

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

// libItem is one copy of a title on one media server.
type libItem struct {
	Copy     string // the copy's id as Re-Posterr writes it: Jellyfin's item id, or "plex:<server>:<key>:<section>:<kind>"
	Server   string // jellyfin or plex
	ServerID int64
	ID       string // Jellyfin's item id or Plex's rating key
	Section  string // Plex's library id
	Library  string // the library's name ("Plex: Movies" for one of Plex's)
	Kind     string // movie or tv
	Title    string
	Year     int
	TMDBID   int
	TVDBID   int
	IMDBID   string
	Genres   []string
	Rating   float64 // out of 10, the server's own (TMDB's by default)
	Added    time.Time
	Poster   string // the poster's tag; "" when it has none
	Media    badgeMedia
	// FilePrint (Plex) changes with the file; Plex's listing doesn't say
	// whether it's HDR, so its tracks are read once per file.
	FilePrint string
}

// libItemsCache keeps the lists a few minutes: a sync of several collections
// reads them once.
var libItemsCache struct {
	mu    sync.Mutex
	at    time.Time
	items []libItem
}

const libItemsFresh = 5 * time.Minute

// libItems are every server's films and shows: Jellyfin's (or Emby's) and
// each Plex's.
func (s *Server) libItems(ctx context.Context, fresh bool) ([]libItem, error) {
	libItemsCache.mu.Lock()
	defer libItemsCache.mu.Unlock()
	if !fresh && libItemsCache.items != nil && time.Since(libItemsCache.at) < libItemsFresh {
		return libItemsCache.items, nil
	}
	var all []libItem
	var firstErr error
	if srv, ok := s.jellyfinServer(); ok {
		list, err := s.jfLibItems(ctx, srv)
		if err != nil {
			firstErr = err
		}
		all = append(all, list...)
	}
	for _, ps := range s.plexServers() {
		list, err := s.mediaClient.PlexLibItems(ctx, ps)
		plexAnswered(ps.ID, err)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, it := range list {
			all = append(all, plexLibItem(ps.ID, it))
		}
	}
	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}
	libItemsCache.at, libItemsCache.items = time.Now(), all
	return all, nil
}

func libItemsForget() {
	libItemsCache.mu.Lock()
	libItemsCache.items = nil
	libItemsCache.mu.Unlock()
}

func plexLibItem(server int64, it mediaservers.PlexLibItem) libItem {
	kind := "movie"
	if it.Kind == "show" {
		kind = "tv"
	}
	li := libItem{
		Copy: artPlexRef{server, it.Key, it.Section, it.Kind}.String(), Server: "plex", ServerID: server, ID: it.Key, Section: it.Section,
		Library: "Plex: " + it.Library, Kind: kind, Title: it.Title, Year: it.Year, TMDBID: it.TMDBID, TVDBID: it.TVDBID, IMDBID: it.IMDBID,
		Genres: it.Genres, Rating: it.Rating, Poster: it.Thumb, FilePrint: it.FilePrint,
	}
	if it.AddedAt > 0 {
		li.Added = time.Unix(it.AddedAt, 0)
	}
	if kind == "movie" {
		li.Media = badgeMedia{Width: it.Width, Height: it.Height}
		if it.AudioCodec != "" {
			li.Media.Audio = []badgeTrack{{Codec: it.AudioCodec, Profile: it.AudioProfile, Channels: it.AudioChannels}}
		}
	}
	return li
}

// jfLibItems lists Jellyfin's films and shows, a library at a time (so each
// knows its library's name).
func (s *Server) jfLibItems(ctx context.Context, srv mediaservers.Server) ([]libItem, error) {
	var folders []struct {
		Name           string
		ItemID         string `json:"ItemId"`
		CollectionType string
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/Library/VirtualFolders", nil, &folders); err != nil {
		return nil, err
	}
	var out []libItem
	for _, f := range folders {
		if f.ItemID == "" || (f.CollectionType != "" && f.CollectionType != "movies" && f.CollectionType != "tvshows" && f.CollectionType != "mixed") {
			continue
		}
		for start := 0; ; start += 500 {
			var page struct {
				Items []struct {
					ID              string `json:"Id"`
					Type            string
					Name            string
					ProductionYear  int
					ProviderIds     map[string]string
					Genres          []string
					CommunityRating float64
					DateCreated     string
					ImageTags       map[string]string
					MediaStreams    []struct {
						Type           string
						Codec          string
						Profile        string
						DisplayTitle   string
						Title          string
						Channels       int
						Width          int
						Height         int
						VideoRange     string
						VideoRangeType string
					}
				}
				TotalRecordCount int
			}
			q := fmt.Sprintf("/Items?ParentId=%s&Recursive=true&IncludeItemTypes=Movie,Series&Fields=ProviderIds,Genres,DateCreated,MediaStreams&EnableImageTypes=Primary&ImageTypeLimit=1&StartIndex=%d&Limit=500", url.QueryEscape(f.ItemID), start)
			if err := jfJSON(ctx, srv, http.MethodGet, q, nil, &page); err != nil {
				return nil, err
			}
			for _, it := range page.Items {
				li := libItem{Copy: it.ID, Server: "jellyfin", ServerID: srv.ID, ID: it.ID, Library: f.Name, Kind: "movie", Title: it.Name, Year: it.ProductionYear, Genres: it.Genres, Rating: it.CommunityRating, Poster: it.ImageTags["Primary"]}
				if it.Type == "Series" {
					li.Kind = "tv"
				}
				li.TMDBID, _ = strconv.Atoi(it.ProviderIds["Tmdb"])
				li.TVDBID, _ = strconv.Atoi(it.ProviderIds["Tvdb"])
				li.IMDBID = it.ProviderIds["Imdb"]
				if t, err := time.Parse(time.RFC3339Nano, it.DateCreated); err == nil {
					li.Added = t
				}
				video := false
				for _, st := range it.MediaStreams {
					switch st.Type {
					case "Video":
						if video {
							continue
						}
						video = true
						li.Media.Width, li.Media.Height = st.Width, st.Height
						li.Media.Range = jfRange(st.VideoRange, st.VideoRangeType)
					case "Audio":
						title := st.DisplayTitle
						if st.Title != "" {
							title += " " + st.Title
						}
						li.Media.Audio = append(li.Media.Audio, badgeTrack{Codec: st.Codec, Profile: st.Profile, Title: title, Channels: st.Channels})
					}
				}
				li.Media.Known = video
				out = append(out, li)
			}
			if len(page.Items) < 500 || start+500 >= page.TotalRecordCount {
				break
			}
		}
	}
	return out, nil
}

// jfRange is Jellyfin's video range as a badge's: dv, hdr10+, hdr, hlg or "".
func jfRange(rng, typ string) string {
	t := strings.ToUpper(typ)
	switch {
	case strings.Contains(t, "DOVI"):
		return "dv"
	case strings.Contains(t, "HDR10PLUS"):
		return "hdr10+"
	case strings.Contains(t, "HLG"):
		return "hlg"
	case strings.Contains(t, "HDR"):
		return "hdr"
	case strings.EqualFold(rng, "HDR"):
		return "hdr"
	}
	return ""
}

// plexRange is a Plex video track's range as a badge's.
func plexRange(v mediaservers.PlexVideo) string {
	t := strings.ToUpper(v.Title)
	switch {
	case v.DolbyVision || strings.Contains(t, "DOVI") || strings.Contains(t, "DOLBY VISION"):
		return "dv"
	case strings.Contains(t, "HDR10+"):
		return "hdr10+"
	case v.ColorTrc == "arib-std-b67" || strings.Contains(t, "HLG"):
		return "hlg"
	case v.ColorTrc == "smpte2084" || strings.Contains(t, "HDR"):
		return "hdr"
	}
	return ""
}
