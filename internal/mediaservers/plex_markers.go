package mediaservers

// Usherr: where a film's or episode's intro and credits are, from Plex's
// own markers (Plex finds them with Plex Pass), for the player's "Skip
// intro" and an early "Up next".

import (
	"context"
	"net/http"
	"net/url"
)

// Markers are a title's intro and credits, in seconds into the file (0 for
// none known).
type Markers struct {
	IntroStart   float64
	IntroEnd     float64
	CreditsStart float64
}

// PlexMarkers reads a title's markers from Plex.
func (c *Client) PlexMarkers(ctx context.Context, s Server, ratingKey string) (Markers, error) {
	var doc struct {
		MediaContainer struct {
			Metadata []struct {
				Marker []struct {
					Type  string `json:"type"` // intro, credits, commercial
					Start int64  `json:"startTimeOffset"`
					End   int64  `json:"endTimeOffset"`
				} `json:"Marker"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	q := url.Values{}
	q.Set("includeMarkers", "1")
	if err := c.do(ctx, s, http.MethodGet, "/library/metadata/"+url.PathEscape(ratingKey), q, nil, &doc); err != nil {
		return Markers{}, explain(s, err)
	}
	var m Markers
	for _, md := range doc.MediaContainer.Metadata {
		for _, mk := range md.Marker {
			start, end := float64(mk.Start)/1000, float64(mk.End)/1000
			switch mk.Type {
			case "intro":
				if m.IntroEnd == 0 && end > start {
					m.IntroStart, m.IntroEnd = start, end
				}
			case "credits":
				if (m.CreditsStart == 0 || start < m.CreditsStart) && start > 60 {
					m.CreditsStart = start // the first credits: early "Up next"
				}
			}
		}
	}
	return m, nil
}

// PlexEpisodeKey is an episode's rating key, from its show's.
func (c *Client) PlexEpisodeKey(ctx context.Context, s Server, showKey string, season, episode int) (string, bool) {
	var doc struct {
		MediaContainer struct {
			Metadata []plexMeta `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.do(ctx, s, http.MethodGet, "/library/metadata/"+url.PathEscape(showKey)+"/allLeaves", nil, nil, &doc); err != nil {
		return "", false
	}
	for _, m := range doc.MediaContainer.Metadata {
		if m.ParentIndex == season && m.Index == episode {
			return m.RatingKey, true
		}
	}
	return "", false
}

// Item is a title on a server (cached like Links), for asking more of it.
func (f *Finder) Item(ctx context.Context, s Server, kind MediaKind, tmdbID int) (Item, bool) {
	return f.find(ctx, s, kind, tmdbID)
}
