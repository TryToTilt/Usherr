package mediaservers

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	// Usherr: how it plays ("DirectPlay", "DirectStream" or "Transcode") and
	// its length in minutes, for the Marquee.
	PlayMethod string `json:"playMethod,omitempty"`
	Runtime    int    `json:"runtime,omitempty"`
	// Usherr (Marquee phase 3): what the picture and sound are, the plot, and
	// the show's TMDB id (for an episode's poster).
	Resolution string   `json:"resolution,omitempty"` // "4K", "1080p", "720p" or "SD"
	HDR        string   `json:"hdr,omitempty"`        // "Dolby Vision", "HDR10+", "HDR10" or "HLG"
	Audio      string   `json:"audio,omitempty"`      // "Dolby Atmos", "DTS:X", "TrueHD", "DTS-HD MA", "Dolby Digital+", ...
	Channels   string   `json:"channels,omitempty"`   // "7.1", "5.1", "Stereo"
	Aspect     string   `json:"aspect,omitempty"`     // Usherr: "2.39:1", "1.85:1", "16:9"
	Overview   string   `json:"overview,omitempty"`
	Genres     []string `json:"genres,omitempty"`
	Rated      string   `json:"rated,omitempty"`
	Rating     float64  `json:"rating,omitempty"`
	SeriesTMDB int      `json:"-"`
	SeriesID   string   `json:"-"` // the media server's own id for the show
}

// mediaStream is one stream of what is playing.
type mediaStream struct {
	Type           string
	Codec          string
	Profile        string
	Height         int
	Width          int
	Channels       int
	VideoRangeType string
	DisplayTitle   string
	Index          int
}

// describeStreams names the picture and the sound (Usherr).
func describeStreams(p *Playing, streams []mediaStream, audioIndex int) {
	var video, audio *mediaStream
	for i := range streams {
		st := &streams[i]
		switch st.Type {
		case "Video":
			if video == nil {
				video = st
			}
		case "Audio":
			if audio == nil || st.Index == audioIndex {
				audio = st
			}
		}
	}
	if video != nil {
		p.Resolution = ResolutionLabel(video.Width, video.Height)
		switch r := strings.ToUpper(video.VideoRangeType); {
		case strings.HasPrefix(r, "DOVI"):
			p.HDR = "Dolby Vision"
		case r == "HDR10PLUS":
			p.HDR = "HDR10+"
		case r == "HDR10":
			p.HDR = "HDR10"
		case r == "HLG":
			p.HDR = "HLG"
		}
		p.Aspect = AspectLabel(float64(video.Width), float64(video.Height))
	}
	if audio != nil {
		p.Audio, p.Channels = SoundLabel(audio.Codec, audio.DisplayTitle+" "+audio.Profile, audio.Channels)
	}
}

// ResolutionLabel is "4K", "1080p", "720p", "SD" or "" for a picture's size.
func ResolutionLabel(w, h int) string {
	switch {
	case w >= 3200 || h >= 2000:
		return "4K"
	case w >= 1700 || h >= 1000:
		return "1080p"
	case w >= 1200 || h >= 700:
		return "720p"
	case w > 0:
		return "SD"
	}
	return ""
}

// SoundLabel names a sound track ("Dolby Atmos", "DTS:X", "TrueHD", "DTS-HD
// MA", "Dolby Digital+", ...) and its channels ("7.1", "5.1", "Stereo") from
// its codec, what it says about itself (profile, title) and its channel count.
func SoundLabel(codec, about string, channels int) (sound, chans string) {
	all := strings.ToLower(about)
	codec = strings.ToLower(codec)
	switch {
	case strings.Contains(all, "atmos"):
		sound = "Dolby Atmos"
	case strings.Contains(all, "dts:x") || strings.Contains(all, "dts-x"):
		sound = "DTS:X"
	case codec == "truehd":
		sound = "TrueHD"
	case codec == "dts" && (strings.Contains(all, "ma") || strings.Contains(all, "hd")):
		sound = "DTS-HD MA"
	case codec == "dts":
		sound = "DTS"
	case codec == "eac3":
		sound = "Dolby Digital+"
	case codec == "ac3":
		sound = "Dolby Digital"
	case codec == "flac":
		sound = "FLAC"
	}
	switch {
	case channels >= 8:
		chans = "7.1"
	case channels >= 6:
		chans = "5.1"
	case channels == 2:
		chans = "Stereo"
	case channels == 1:
		chans = "Mono"
	}
	return sound, chans
}

// aspects are the shapes films are made in, as they're usually written.
var aspects = []struct {
	r     float64
	label string
}{
	{1.33, "4:3"}, {1.375, "1.37:1"}, {1.43, "1.43:1"}, {1.66, "1.66:1"}, {1.78, "16:9"}, {1.85, "1.85:1"},
	{1.90, "1.90:1"}, {2.0, "2:1"}, {2.2, "2.20:1"}, {2.39, "2.39:1"}, {2.55, "2.55:1"}, {2.76, "2.76:1"},
}

// AspectLabel names a picture's shape from its width and height ("2.39:1",
// "1.85:1", "16:9"): the nearest usual one, else the ratio itself.
func AspectLabel(w, h float64) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	r := w / h
	best, diff := "", 1.0
	for _, a := range aspects {
		if d := math.Abs(r-a.r) / a.r; d < diff {
			best, diff = a.label, d
		}
	}
	if r >= 2.3 && r <= 2.45 { // 2.35, 2.39 and 2.40 are all "scope"
		return "2.39:1"
	}
	if diff <= 0.025 {
		return best
	}
	return strconv.FormatFloat(math.Round(r*100)/100, 'f', 2, 64) + ":1"
}

// NowPlaying lists what Jellyfin or Emby is playing right now. Other servers
// answer nothing.
func (c *Client) NowPlaying(ctx context.Context, s Server) ([]Playing, error) {
	if s.Kind != KindJellyfin && s.Kind != KindEmby {
		return nil, nil
	}
	var list []struct {
		DeviceName     string
		UserId         string // Usherr
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
			Overview          string        // Usherr
			Genres            []string      // Usherr
			OfficialRating    string        // Usherr
			CommunityRating   float64       // Usherr
			MediaStreams      []mediaStream // Usherr
		}
		PlayState struct {
			PositionTicks    int64
			IsPaused         bool
			PlayMethod       string // Usherr
			AudioStreamIndex int    // Usherr
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
			p.Runtime = int(n.RunTimeTicks / 600_000_000) // Usherr
		}
		p.PlayMethod = it.PlayState.PlayMethod // Usherr
		describeStreams(&p, n.MediaStreams, it.PlayState.AudioStreamIndex)
		p.Overview, p.Genres, p.Rated, p.Rating, p.SeriesID = n.Overview, n.Genres, n.OfficialRating, n.CommunityRating, n.SeriesId
		if p.Kind == "episode" && n.SeriesId != "" && it.UserId != "" {
			p.SeriesTMDB = c.seriesTMDBCached(ctx, s, it.UserId, n.SeriesId)
		}
		out = append(out, p)
	}
	return out, nil
}

// Usherr: a show's TMDB id, asked of the media server once per show.
var seriesTMDBSeen = struct {
	sync.Mutex
	m map[string]int
}{m: map[string]int{}}

func (c *Client) seriesTMDBCached(ctx context.Context, s Server, userID, seriesID string) int {
	key := s.BaseURL + "|" + seriesID
	seriesTMDBSeen.Lock()
	id, ok := seriesTMDBSeen.m[key]
	seriesTMDBSeen.Unlock()
	if ok {
		return id
	}
	id = c.embySeriesTMDB(ctx, s, userID, seriesID)
	seriesTMDBSeen.Lock()
	if len(seriesTMDBSeen.m) > 2000 {
		seriesTMDBSeen.m = map[string]int{}
	}
	seriesTMDBSeen.m[key] = id
	seriesTMDBSeen.Unlock()
	return id
}
