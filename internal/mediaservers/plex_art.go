package mediaservers

// Usherr: Re-Posterr for Plex. What Plex's films and shows look like (have
// they a poster, a background), reading a picture, and putting one on: it's
// uploaded and then locked, so Plex's own metadata refresh doesn't swap it
// back. Plex takes posters ("thumb") and backgrounds ("art"); a logo stays
// Jellyfin's.

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// PlexArtItem is a film or show in one of Plex's libraries.
type PlexArtItem struct {
	Key       string // the rating key
	Section   string // its library's id
	Kind      string // "movie" or "show"
	Title     string
	Year      int
	TMDBID    int
	TVDBID    int
	HasPoster bool
	HasArt    bool
}

// PlexArtItems lists the films and shows in Plex's film and TV libraries.
func (c *Client) PlexArtItems(ctx context.Context, s Server) ([]PlexArtItem, error) {
	secs, err := c.plexSections(ctx, s)
	if err != nil {
		return nil, err
	}
	var out []PlexArtItem
	for _, sec := range secs {
		if sec.Type != "movie" && sec.Type != "show" {
			continue
		}
		var doc struct {
			MediaContainer struct {
				Metadata []struct {
					plexMeta
					Thumb string `json:"thumb"`
					Art   string `json:"art"`
				} `json:"Metadata"`
			} `json:"MediaContainer"`
		}
		q := url.Values{}
		q.Set("includeGuids", "1")
		if err := c.do(ctx, s, http.MethodGet, "/library/sections/"+url.PathEscape(sec.ID)+"/all", q, nil, &doc); err != nil {
			return nil, explain(s, err)
		}
		for _, m := range doc.MediaContainer.Metadata {
			it := PlexArtItem{Key: m.RatingKey, Section: sec.ID, Kind: sec.Type, Title: m.Title, Year: m.Year, TMDBID: m.tmdb(), HasPoster: m.Thumb != "", HasArt: m.Art != ""}
			for _, g := range m.Guids {
				if rest, ok := cutPrefix(g.ID, "tvdb://"); ok {
					it.TVDBID, _ = strconv.Atoi(rest)
				}
			}
			out = append(out, it)
		}
	}
	return out, nil
}

func cutPrefix(s, p string) (string, bool) {
	if len(s) >= len(p) && s[:len(p)] == p {
		return s[len(p):], true
	}
	return "", false
}

// plexArtField is Plex's name for a picture: a poster is its "thumb", a
// background its "art". "" for one Plex doesn't take (a logo).
func plexArtField(typ string) string {
	switch typ {
	case "poster":
		return "thumb"
	case "backdrop":
		return "art"
	}
	return ""
}

// PlexImage is the title's picture of the type as Plex has it (nil: none,
// or a type Plex doesn't keep).
func (c *Client) PlexImage(ctx context.Context, s Server, key, typ string) ([]byte, string, error) {
	field := plexArtField(typ)
	if field == "" {
		return nil, "", nil
	}
	data, ctype, err := c.plexRaw(ctx, s, http.MethodGet, "/library/metadata/"+url.PathEscape(key)+"/"+field, nil, nil, "")
	if err == errNotFound {
		return nil, "", nil
	}
	return data, ctype, err
}

// PlexSetImage puts the picture on the title as its poster or background
// and locks it there. A type Plex doesn't take (a logo) is left alone.
func (c *Client) PlexSetImage(ctx context.Context, s Server, key, section, kind, typ string, data []byte, mime string) error {
	field := plexArtField(typ)
	if field == "" {
		return nil
	}
	upload := map[string]string{"thumb": "posters", "art": "arts"}[field]
	if _, _, err := c.plexRaw(ctx, s, http.MethodPost, "/library/metadata/"+url.PathEscape(key)+"/"+upload, nil, data, mime); err != nil {
		return explain(s, err)
	}
	q := url.Values{}
	q.Set("type", map[string]string{"movie": "1", "show": "2"}[kind])
	q.Set("id", key)
	q.Set(field+".locked", "1")
	if _, _, err := c.plexRaw(ctx, s, http.MethodPut, "/library/sections/"+url.PathEscape(section)+"/all", q, nil, ""); err != nil {
		return explain(s, err)
	}
	return nil
}

// plexRaw is a request to Plex with a raw body (a picture), answering the
// raw body back.
func (c *Client) plexRaw(ctx context.Context, s Server, method, path string, q url.Values, body []byte, ctype string) ([]byte, string, error) {
	base, err := NormalizeURL(s.BaseURL)
	if err != nil {
		return nil, "", userErr(err, "The server address is not valid: %v", err)
	}
	target := base + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, "", err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	c.authorize(req, s)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, "", unreachable(s, base, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, "", errUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return nil, "", errNotFound
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, "", &statusError{code: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswerBytes))
	return data, resp.Header.Get("Content-Type"), err
}
