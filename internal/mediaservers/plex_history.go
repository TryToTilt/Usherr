package mediaservers

// Usherr: who watches on a Plex server, and what each of them has watched,
// so the Diary, Wrapped, For you and the picks work for a Plex household as
// they do for a Jellyfin one. The server owner's token reads every account's
// history (/status/sessions/history/all?accountID=).

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PlexWatcher is someone who watches on the Plex server: the owner, a Plex
// Home member or a friend it's shared with.
type PlexWatcher struct {
	ID    int
	Name  string
	Owner bool
}

// PlexAccounts lists the server's accounts.
func (c *Client) PlexAccounts(ctx context.Context, s Server) ([]PlexWatcher, error) {
	var doc struct {
		MediaContainer struct {
			Account []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"Account"`
		} `json:"MediaContainer"`
	}
	if err := c.do(ctx, s, http.MethodGet, "/accounts", nil, nil, &doc); err != nil {
		return nil, explain(s, err)
	}
	var out []PlexWatcher
	for _, a := range doc.MediaContainer.Account {
		if a.ID <= 0 || strings.TrimSpace(a.Name) == "" { // 0 is Plex itself
			continue
		}
		out = append(out, PlexWatcher{ID: a.ID, Name: a.Name, Owner: a.ID == 1})
	}
	return out, nil
}

// Played is one film or episode someone finished.
type Played struct {
	Kind      string // "movie" or "episode"
	TMDBID    int    // the film's, or for an episode the show's
	Title     string // the film's, or the show's
	Year      int    // the film's, or the show's first
	Season    int
	Episode   int
	Minutes   int
	WatchedAt time.Time
	ShowKey   string // Plex: the show's rating key, for its next episode
}

// plexHistoryPageSize is how many plays are asked for at a time.
var plexHistoryPageSize = 500

// plexHistoryMax caps how far back the history is read.
const plexHistoryMax = 100_000

// PlexHistory is what an account finished on the server since the time
// given (all of it when zero), newest first. Plays of something no longer
// in the libraries, or without a TMDB id, are left out.
func (c *Client) PlexHistory(ctx context.Context, s Server, accountID int, since time.Time) ([]Played, error) {
	type play struct {
		RatingKey string `json:"ratingKey"`
		Type      string `json:"type"`
		ViewedAt  int64  `json:"viewedAt"`
	}
	var plays []play
	for start := 0; ; start += plexHistoryPageSize {
		q := url.Values{}
		q.Set("accountID", strconv.Itoa(accountID))
		q.Set("sort", "viewedAt:desc")
		q.Set("X-Plex-Container-Start", strconv.Itoa(start))
		q.Set("X-Plex-Container-Size", strconv.Itoa(plexHistoryPageSize))
		if !since.IsZero() {
			q.Set("viewedAt>", strconv.FormatInt(since.Unix(), 10))
		}
		var doc struct {
			MediaContainer struct {
				Metadata []play `json:"Metadata"`
			} `json:"MediaContainer"`
		}
		if err := c.do(ctx, s, http.MethodGet, "/status/sessions/history/all", q, nil, &doc); err != nil {
			return nil, explain(s, err)
		}
		for _, p := range doc.MediaContainer.Metadata {
			if (p.Type == "movie" || p.Type == "episode") && p.RatingKey != "" {
				plays = append(plays, p)
			}
		}
		if len(doc.MediaContainer.Metadata) < plexHistoryPageSize || start >= plexHistoryMax {
			break // the end, or far more than anyone watches
		}
	}
	// What each film and episode is, a batch at a time, then the shows.
	keys := map[string]bool{}
	for _, p := range plays {
		keys[p.RatingKey] = true
	}
	items, err := c.plexMetadata(ctx, s, keys)
	if err != nil {
		return nil, err
	}
	shows := map[string]bool{}
	for _, it := range items {
		if it.Type == "episode" && it.GrandparentRatingKey != "" {
			shows[it.GrandparentRatingKey] = true
		}
	}
	showItems, err := c.plexMetadata(ctx, s, shows)
	if err != nil {
		return nil, err
	}
	var out []Played
	for _, p := range plays {
		it, ok := items[p.RatingKey]
		if !ok {
			continue
		}
		pl := Played{Kind: it.Type, Minutes: int(it.Duration / 60_000), WatchedAt: time.Unix(p.ViewedAt, 0)}
		switch it.Type {
		case "movie":
			pl.TMDBID, pl.Title, pl.Year = it.tmdb(), it.Title, it.Year
		case "episode":
			show, ok := showItems[it.GrandparentRatingKey]
			if !ok {
				continue
			}
			pl.TMDBID, pl.Title, pl.Year, pl.Season, pl.Episode = show.tmdb(), show.Title, show.Year, it.ParentIndex, it.Index
			pl.ShowKey = it.GrandparentRatingKey
		}
		if pl.TMDBID > 0 {
			out = append(out, pl)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].WatchedAt.After(out[j].WatchedAt) })
	return out, nil
}

// plexMeta is a title's details from /library/metadata.
type plexMeta struct {
	RatingKey            string     `json:"ratingKey"`
	Type                 string     `json:"type"`
	Title                string     `json:"title"`
	Year                 int        `json:"year"`
	Duration             int64      `json:"duration"` // ms
	GUID                 string     `json:"guid"`
	Guids                []plexGuid `json:"Guid"`
	GrandparentRatingKey string     `json:"grandparentRatingKey"`
	ParentIndex          int        `json:"parentIndex"`
	Index                int        `json:"index"`
}

func (m plexMeta) tmdb() int {
	for _, g := range m.Guids {
		if id, ok := plexTMDBID(g.ID); ok {
			return id
		}
	}
	if id, ok := plexTMDBID(m.GUID); ok {
		return id
	}
	return 0
}

// plexMetadataBatch is how many titles one request asks about.
const plexMetadataBatch = 50

// plexMetadata looks titles up by rating key, several to a request
// (/library/metadata/1,2,3). Keys Plex no longer has are simply missing.
func (c *Client) plexMetadata(ctx context.Context, s Server, keys map[string]bool) (map[string]plexMeta, error) {
	list := make([]string, 0, len(keys))
	for k := range keys {
		list = append(list, k)
	}
	sort.Strings(list)
	out := map[string]plexMeta{}
	for i := 0; i < len(list); i += plexMetadataBatch {
		batch := list[i:min(i+plexMetadataBatch, len(list))]
		for j := range batch {
			batch[j] = url.PathEscape(batch[j])
		}
		var doc struct {
			MediaContainer struct {
				Metadata []plexMeta `json:"Metadata"`
			} `json:"MediaContainer"`
		}
		q := url.Values{}
		q.Set("includeGuids", "1")
		if err := c.do(ctx, s, http.MethodGet, "/library/metadata/"+strings.Join(batch, ","), q, nil, &doc); err != nil {
			if errors.Is(err, errNotFound) {
				continue // everything in the batch has gone
			}
			return nil, explain(s, err)
		}
		for _, m := range doc.MediaContainer.Metadata {
			out[m.RatingKey] = m
		}
	}
	return out, nil
}
