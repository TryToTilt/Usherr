package mediaservers

// Usherr: what a Plex server has to watch, for the picks (Tonight, Pick for
// the Room, Catch Me Up) and "what we have": every film and show in its
// libraries, what's on deck, and a show's next episode.

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ShelfTitle is a film or show on the server.
type ShelfTitle struct {
	Kind          string // "movie" or "show"
	TMDBID        int
	Key           string // the server's own id (Plex's rating key)
	Title         string
	Year          int
	Genres        []string
	Rating        float64
	ContentRating string
	File          string // a film's file or a show's folder, as the server sees it
}

// adultSection matches libraries kept out of every suggestion.
var adultSection = regexp.MustCompile(`(?i)(18\+|nsfw|adult|xxx)`)

type plexShelfEntry struct {
	plexMeta
	ContentRating  string         `json:"contentRating"`
	AudienceRating float64        `json:"audienceRating"`
	Rating         float64        `json:"rating"`
	Genre          []plexTag      `json:"Genre"`
	Location       []plexLocation `json:"Location"` // a show's folder
	Media          []struct {
		Part []struct {
			File string `json:"file"`
		} `json:"Part"`
	} `json:"Media"`
}

// PlexShelf is every film and show in the server's film and TV libraries
// (adult ones left out), with their TMDB ids.
func (c *Client) PlexShelf(ctx context.Context, s Server) ([]ShelfTitle, error) {
	secs, err := c.plexSections(ctx, s)
	if err != nil {
		return nil, err
	}
	var out []ShelfTitle
	for _, sec := range secs {
		if (sec.Type != "movie" && sec.Type != "show") || adultSection.MatchString(sec.Title) {
			continue
		}
		var doc struct {
			MediaContainer struct {
				Metadata []plexShelfEntry `json:"Metadata"`
			} `json:"MediaContainer"`
		}
		q := url.Values{}
		q.Set("includeGuids", "1")
		if err := c.do(ctx, s, http.MethodGet, "/library/sections/"+url.PathEscape(sec.ID)+"/all", q, nil, &doc); err != nil {
			continue // one library that won't list doesn't spoil the rest
		}
		for _, m := range doc.MediaContainer.Metadata {
			id := m.tmdb()
			if id <= 0 || strings.EqualFold(m.ContentRating, "XXX") {
				continue
			}
			t := ShelfTitle{Kind: sec.Type, TMDBID: id, Key: m.RatingKey, Title: m.Title, Year: m.Year, Rating: m.AudienceRating, ContentRating: m.ContentRating}
			if t.Rating == 0 {
				t.Rating = m.Rating
			}
			for _, g := range m.Genre {
				t.Genres = append(t.Genres, g.Tag)
			}
			if len(m.Media) > 0 && len(m.Media[0].Part) > 0 {
				t.File = m.Media[0].Part[0].File
			}
			if t.File == "" && len(m.Location) > 0 {
				t.File = m.Location[0].Path
			}
			out = append(out, t)
		}
	}
	return out, nil
}

// Upcoming is something to carry on with: a film or episode started, or a
// show's next episode.
type Upcoming struct {
	Kind    string // "movie" or "episode"
	TMDBID  int    // the film's, or the show's
	Title   string // the film's, or the show's
	Year    int
	Season  int
	Episode int
	Name    string // the episode's
	Started bool   // part-way through
}

// PlexOnDeck is the server owner's On Deck: films and episodes started, and
// the next episodes of shows they watch.
func (c *Client) PlexOnDeck(ctx context.Context, s Server) ([]Upcoming, error) {
	var doc struct {
		MediaContainer struct {
			Metadata []struct {
				plexMeta
				GrandparentTitle string `json:"grandparentTitle"`
				ViewOffset       int64  `json:"viewOffset"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	q := url.Values{}
	q.Set("includeGuids", "1")
	if err := c.do(ctx, s, http.MethodGet, "/library/onDeck", q, nil, &doc); err != nil {
		return nil, explain(s, err)
	}
	var out []Upcoming
	for _, m := range doc.MediaContainer.Metadata {
		u := Upcoming{Kind: m.Type, Started: m.ViewOffset > 0}
		switch m.Type {
		case "movie":
			u.TMDBID, u.Title, u.Year = m.tmdb(), m.Title, m.Year
		case "episode":
			u.TMDBID, u.Title, u.Season, u.Episode, u.Name = c.plexShowTMDBCached(ctx, s, m.GrandparentRatingKey), m.GrandparentTitle, m.ParentIndex, m.Index, m.Title
		default:
			continue
		}
		if u.TMDBID > 0 {
			out = append(out, u)
		}
	}
	return out, nil
}

// PlexNextEpisode is the episode after the one given, in the show's order
// (specials left out), if the server has it.
func (c *Client) PlexNextEpisode(ctx context.Context, s Server, showKey string, season, episode int) (Upcoming, bool) {
	var doc struct {
		MediaContainer struct {
			Metadata []plexMeta `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.do(ctx, s, http.MethodGet, "/library/metadata/"+url.PathEscape(showKey)+"/allLeaves", nil, nil, &doc); err != nil {
		return Upcoming{}, false
	}
	var best *plexMeta
	for i := range doc.MediaContainer.Metadata {
		m := &doc.MediaContainer.Metadata[i]
		after := m.ParentIndex > season || (m.ParentIndex == season && m.Index > episode)
		if m.ParentIndex <= 0 || !after {
			continue
		}
		if best == nil || m.ParentIndex < best.ParentIndex || (m.ParentIndex == best.ParentIndex && m.Index < best.Index) {
			best = m
		}
	}
	if best == nil {
		return Upcoming{}, false
	}
	return Upcoming{Kind: "episode", Season: best.ParentIndex, Episode: best.Index, Name: best.Title}, true
}
