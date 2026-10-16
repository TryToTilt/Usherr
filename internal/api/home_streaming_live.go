package api

// Usherr: what StreamNZB is streaming right now (Nuvio, Stremio, Infuse...),
// for the Marquee's "Now showing". StreamNZB sends its own dashboard a live
// "stats" message over a WebSocket; one of them is read, and the file's size
// comes from StreamNZB's library, so the progress is bytes read / file size.
// Read-only: nothing in StreamNZB is changed.

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/metadata"
)

type snzbSession struct {
	ID         string `json:"id"` // "stream:<profile>:<movie|series>:<imdb id>[:<season>:<episode>]:<slot>"
	StreamName string `json:"stream_name"`
	Title      string `json:"title"` // the release's name
	BytesRead  int64  `json:"bytes_read"`
}

// liveSessions reads one "stats" message from StreamNZB's live feed.
func (c *streamNZB) liveSessions(ctx context.Context) ([]snzbSession, error) {
	u, err := url.Parse(c.base + "/api/ws")
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	d := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := d.DialContext(ctx, u.String(), http.Header{"Authorization": {"Bearer " + c.token}})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetReadDeadline(deadline)
	for {
		var m struct {
			Type    string `json:"type"`
			Payload struct {
				ActiveSessions []snzbSession `json:"active_sessions"`
			} `json:"payload"`
		}
		if err := conn.ReadJSON(&m); err != nil {
			return nil, err
		}
		if m.Type == "stats" {
			return m.Payload.ActiveSessions, nil
		}
	}
}

// releaseSize is the size of a release's video file, from StreamNZB's library.
func (c *streamNZB) releaseSize(ctx context.Context, release string) int64 {
	var out struct {
		Items []struct {
			ReleaseTitle  string `json:"release_title"`
			SizeBytes     int64  `json:"size_bytes"`
			MediaFileSize int64  `json:"media_file_size"`
		} `json:"items"`
	}
	q := url.Values{"q": {release}, "limit": {"10"}, "offset": {"0"}}
	if err := c.do(ctx, http.MethodGet, "/api/library?"+q.Encode(), nil, "", &out); err != nil {
		return 0
	}
	for _, it := range out.Items {
		if it.ReleaseTitle == release {
			if it.MediaFileSize > 0 {
				return it.MediaFileSize
			}
			return it.SizeBytes
		}
	}
	return 0
}

// streamingNow is what StreamNZB is playing, as the Marquee shows it.
func (s *Server) streamingNow(ctx context.Context) []marqueePlaying {
	c, err := s.streamNZB()
	if err != nil {
		return nil
	}
	sessions, err := c.liveSessions(ctx)
	if err != nil {
		return nil
	}
	var out []marqueePlaying
	seen := map[string]bool{}
	for _, se := range sessions {
		// stream:<profile>:<kind>:<imdb>[:<season>:<episode>]:<slot>
		parts := strings.Split(se.ID, ":")
		if len(parts) < 5 || parts[0] != "stream" {
			continue
		}
		kind, imdb := parts[2], parts[3]
		key := se.StreamName + "|" + strings.Join(parts[2:len(parts)-1], ":")
		if seen[key] {
			continue // failover slots of the same title
		}
		seen[key] = true
		p := marqueePlaying{Playing: mediaservers.Playing{Title: se.Title, Kind: "other", Device: se.StreamName}}
		if size := c.releaseSize(ctx, se.Title); size > 0 {
			p.Progress = float64(se.BytesRead) / float64(size)
			if p.Progress > 1 {
				p.Progress = 1
			}
		}
		if s.TMDB().HasAPIKey() && strings.HasPrefix(imdb, "tt") {
			movieID, showID, err := s.displayTMDB().FindByIMDB(ctx, imdb)
			switch {
			case err != nil:
			case kind == "movie" && movieID > 0:
				p.Kind, p.TMDBID = "movie", movieID
				if f, ok := s.marqueeFeature(ctx, movieID, "playing"); ok {
					p.Title, p.Year, p.PosterURL, p.BackdropURL = f.Title, f.Year, f.PosterURL, f.BackdropURL
				}
			case kind == "series" && showID > 0 && len(parts) >= 7:
				p.Kind = "episode"
				p.Season, _ = strconv.Atoi(parts[4])
				p.Episode, _ = strconv.Atoi(parts[5])
				if sh, err := s.displayTMDB().GetShow(ctx, showID); err == nil {
					p.Series, p.Title = sh.Name, ""
					p.PosterURL, p.BackdropURL = metadata.ImageURL("w780", sh.PosterPath), metadata.BackdropURL(sh.BackdropPath)
				}
			}
		}
		out = append(out, p)
	}
	return out
}
