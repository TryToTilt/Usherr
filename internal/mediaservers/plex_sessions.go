package mediaservers

// Usherr: what Plex is playing right now (/status/sessions), in the same
// shape Jellyfin's sessions give, so the Marquee, Re-Posterr and the rest
// work the same with either server.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type plexTag struct {
	Tag string `json:"tag"`
}

type plexStream struct {
	StreamType           int    `json:"streamType"` // 1 video, 2 audio, 3 subtitles
	Codec                string `json:"codec"`
	Profile              string `json:"profile"`
	Width                int    `json:"width"`
	Height               int    `json:"height"`
	Channels             int    `json:"channels"`
	DisplayTitle         string `json:"displayTitle"`
	ExtendedDisplayTitle string `json:"extendedDisplayTitle"`
	Selected             bool   `json:"selected"`
	DOVIPresent          bool   `json:"DOVIPresent"`
	ColorTrc             string `json:"colorTrc"`
}

type plexSession struct {
	Type                 string     `json:"type"` // movie, episode, track, clip
	Title                string     `json:"title"`
	GrandparentTitle     string     `json:"grandparentTitle"`
	GrandparentRatingKey string     `json:"grandparentRatingKey"`
	ParentIndex          int        `json:"parentIndex"`
	Index                int        `json:"index"`
	Year                 int        `json:"year"`
	Duration             int64      `json:"duration"`   // ms
	ViewOffset           int64      `json:"viewOffset"` // ms
	Summary              string     `json:"summary"`
	ContentRating        string     `json:"contentRating"`
	AudienceRating       float64    `json:"audienceRating"`
	Rating               float64    `json:"rating"`
	Genre                []plexTag  `json:"Genre"`
	Guids                []plexGuid `json:"Guid"`
	RatingKey            string     `json:"ratingKey"`  // Usherr: the watch history
	SessionKey           string     `json:"sessionKey"` // Usherr
	User                 struct {
		ID    plexLooseID `json:"id"` // the plex.tv account
		Title string      `json:"title"`
	} `json:"User"` // Usherr
	Session struct {
		ID        string `json:"id"`
		Bandwidth int    `json:"bandwidth"` // kbps
		Location  string `json:"location"`  // lan, wan
	} `json:"Session"` // Usherr
	Player struct {
		Title    string `json:"title"`
		Device   string `json:"device"`
		Product  string `json:"product"`
		Platform string `json:"platform"` // Usherr
		State    string `json:"state"`    // playing, paused, buffering
		Address  string `json:"address"`  // Usherr
		Local    *bool  `json:"local"`    // Usherr
	} `json:"Player"`
	TranscodeSession *struct {
		VideoDecision    string `json:"videoDecision"` // transcode, copy, directplay
		AudioDecision    string `json:"audioDecision"`
		VideoCodec       string `json:"videoCodec"`       // Usherr
		AudioCodec       string `json:"audioCodec"`       // Usherr
		SourceVideoCodec string `json:"sourceVideoCodec"` // Usherr
		SourceAudioCodec string `json:"sourceAudioCodec"` // Usherr
		Width            int    `json:"width"`            // Usherr
		Height           int    `json:"height"`           // Usherr
	} `json:"TranscodeSession"`
	Media []struct {
		AspectRatio float64 `json:"aspectRatio"`
		VideoCodec  string  `json:"videoCodec"` // Usherr
		AudioCodec  string  `json:"audioCodec"` // Usherr
		Bitrate     int     `json:"bitrate"`    // Usherr: kbps
		Part        []struct {
			Stream []plexStream `json:"Stream"`
		} `json:"Part"`
	} `json:"Media"`
}

// plexNowPlaying is NowPlaying for Plex.
func (c *Client) plexNowPlaying(ctx context.Context, s Server) ([]Playing, error) {
	var doc struct {
		MediaContainer struct {
			Metadata []plexSession `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.do(ctx, s, http.MethodGet, "/status/sessions", nil, nil, &doc); err != nil {
		return nil, explain(s, err)
	}
	var out []Playing
	for _, it := range doc.MediaContainer.Metadata {
		p := Playing{Title: it.Title, Year: it.Year, Paused: it.Player.State == "paused", Kind: "other", Device: it.Player.Title}
		if p.Device == "" {
			p.Device = it.Player.Device
		}
		switch it.Type {
		case "movie":
			p.Kind = "movie"
			for _, g := range it.Guids {
				if id, ok := plexTMDBID(g.ID); ok {
					p.TMDBID = id
				}
			}
		case "episode":
			p.Kind, p.Series, p.Season, p.Episode = "episode", it.GrandparentTitle, it.ParentIndex, it.Index
			p.SeriesID = it.GrandparentRatingKey
			if it.GrandparentRatingKey != "" {
				p.SeriesTMDB = c.plexShowTMDBCached(ctx, s, it.GrandparentRatingKey)
			}
		}
		if it.Duration > 0 {
			p.Progress = float64(it.ViewOffset) / float64(it.Duration)
			p.Runtime = int(it.Duration / 60_000)
		}
		switch {
		case it.TranscodeSession == nil:
			p.PlayMethod = "DirectPlay"
		case it.TranscodeSession.VideoDecision == "transcode" || it.TranscodeSession.AudioDecision == "transcode":
			p.PlayMethod = "Transcode"
		default:
			p.PlayMethod = "DirectStream"
		}
		p.Overview, p.Rated = it.Summary, it.ContentRating
		p.Rating = it.AudienceRating
		if p.Rating == 0 {
			p.Rating = it.Rating
		}
		for _, g := range it.Genre {
			p.Genres = append(p.Genres, g.Tag)
		}
		if len(it.Media) > 0 {
			describePlexStreams(&p, it.Media[0].Part, it.Media[0].AspectRatio)
		}
		p.Session = plexSessionInfo(it)
		out = append(out, p)
	}
	return out, nil
}

// describePlexStreams fills in the picture and sound of what's playing.
func describePlexStreams(p *Playing, parts []struct {
	Stream []plexStream `json:"Stream"`
}, aspect float64) {
	var video, audio *plexStream
	for i := range parts {
		for j := range parts[i].Stream {
			st := &parts[i].Stream[j]
			switch st.StreamType {
			case 1:
				if video == nil {
					video = st
				}
			case 2:
				if audio == nil || st.Selected {
					audio = st
				}
			}
		}
	}
	if video != nil {
		p.Resolution = ResolutionLabel(video.Width, video.Height)
		switch {
		case video.DOVIPresent || strings.Contains(strings.ToLower(video.DisplayTitle), "dovi") || strings.Contains(video.DisplayTitle, "Dolby Vision"):
			p.HDR = "Dolby Vision"
		case strings.Contains(video.DisplayTitle, "HDR10+"):
			p.HDR = "HDR10+"
		case video.ColorTrc == "smpte2084" || strings.Contains(video.DisplayTitle, "HDR10"):
			p.HDR = "HDR10"
		case video.ColorTrc == "arib-std-b67" || strings.Contains(video.DisplayTitle, "HLG"):
			p.HDR = "HLG"
		}
		p.Aspect = AspectLabel(float64(video.Width), float64(video.Height))
	}
	if aspect > 0 {
		p.Aspect = AspectLabel(aspect, 1)
	}
	if audio != nil {
		p.Audio, p.Channels = SoundLabel(audio.Codec, audio.ExtendedDisplayTitle+" "+audio.DisplayTitle+" "+audio.Profile, audio.Channels)
	}
}

// A show's TMDB id, asked of Plex once per show.
var plexShowTMDBSeen = struct {
	sync.Mutex
	m map[string]int
}{m: map[string]int{}}

func (c *Client) plexShowTMDBCached(ctx context.Context, s Server, ratingKey string) int {
	key := s.BaseURL + "|" + ratingKey
	plexShowTMDBSeen.Lock()
	id, ok := plexShowTMDBSeen.m[key]
	plexShowTMDBSeen.Unlock()
	if ok {
		return id
	}
	if md, err := c.plexGet(ctx, s, "/library/metadata/"+url.PathEscape(ratingKey), nil); err == nil {
		for _, e := range md.items() {
			for _, g := range e.Guids {
				if v, ok := plexTMDBID(g.ID); ok {
					id = v
				}
			}
			if id == 0 {
				if v, ok := plexTMDBID(e.GUID); ok {
					id = v
				}
			}
		}
	}
	plexShowTMDBSeen.Lock()
	if len(plexShowTMDBSeen.m) > 2000 {
		plexShowTMDBSeen.m = map[string]int{}
	}
	plexShowTMDBSeen.m[key] = id
	plexShowTMDBSeen.Unlock()
	return id
}
