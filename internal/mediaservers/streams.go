package mediaservers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

// ActiveStreams says how many people are watching something on the server
// right now (a paused stream counts: it is likely to carry on). Servers that
// are not a media server in that sense answer 0.
func (c *Client) ActiveStreams(ctx context.Context, s Server) (int, error) {
	switch s.Kind {
	case KindPlex:
		sessions, err := c.plexGet(ctx, s, "/status/sessions", nil)
		if err != nil {
			return 0, explain(s, err)
		}
		return len(sessions.Metadata) + len(sessions.Video), nil
	case KindJellyfin, KindEmby:
		var list []struct {
			NowPlayingItem json.RawMessage `json:"NowPlayingItem"`
		}
		if err := c.do(ctx, s, http.MethodGet, "/Sessions", nil, nil, &list); err != nil {
			return 0, explain(s, err)
		}
		n := 0
		for _, it := range list {
			if len(it.NowPlayingItem) > 0 && string(it.NowPlayingItem) != "null" {
				n++
			}
		}
		return n, nil
	}
	return 0, nil
}

// Playing is one thing being watched right now (Usherr: the marquee's "Now
// showing"). Progress is 0-1; TMDBID is the film's or, for an episode, the
// show's when the server knows it.
type Playing struct {
	Title    string  `json:"title"`
	Series   string  `json:"series,omitempty"`
	Season   int     `json:"season,omitempty"`
	Episode  int     `json:"episode,omitempty"`
	Kind     string  `json:"kind"` // "movie", "episode" or "other"
	TMDBID   int     `json:"tmdbId,omitempty"`
	Year     int     `json:"year,omitempty"`
	Progress float64 `json:"progress"`
	Paused   bool    `json:"paused"`
	Device   string  `json:"device,omitempty"`
}

// NowPlaying lists what Jellyfin or Emby is playing right now. Other servers
// answer nothing.
func (c *Client) NowPlaying(ctx context.Context, s Server) ([]Playing, error) {
	if s.Kind != KindJellyfin && s.Kind != KindEmby {
		return nil, nil
	}
	var list []struct {
		DeviceName     string
		NowPlayingItem *struct {
			Name              string
			SeriesName        string
			Type              string
			ParentIndexNumber int
			IndexNumber       int
			ProductionYear    int
			RunTimeTicks      int64
			ProviderIds       map[string]string
			SeriesId          string
		}
		PlayState struct {
			PositionTicks int64
			IsPaused      bool
		}
	}
	if err := c.do(ctx, s, http.MethodGet, "/Sessions", nil, nil, &list); err != nil {
		return nil, explain(s, err)
	}
	var out []Playing
	for _, it := range list {
		n := it.NowPlayingItem
		if n == nil {
			continue
		}
		p := Playing{Title: n.Name, Year: n.ProductionYear, Paused: it.PlayState.IsPaused, Device: it.DeviceName, Kind: "other"}
		switch n.Type {
		case "Movie":
			p.Kind = "movie"
		case "Episode":
			p.Kind, p.Series, p.Season, p.Episode = "episode", n.SeriesName, n.ParentIndexNumber, n.IndexNumber
		}
		if id := n.ProviderIds["Tmdb"]; id != "" && p.Kind == "movie" {
			p.TMDBID, _ = strconv.Atoi(id)
		}
		if n.RunTimeTicks > 0 {
			p.Progress = float64(it.PlayState.PositionTicks) / float64(n.RunTimeTicks)
		}
		out = append(out, p)
	}
	return out, nil
}
