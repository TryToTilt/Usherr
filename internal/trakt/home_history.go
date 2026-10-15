package trakt

// Home fork: a public Trakt profile's watch history and ratings, for the
// Diary's import. Only the API key is needed; the profile must be public.

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Watched is one thing a person watched on Trakt: a film, or an episode of a
// show (TMDBID is then the show's).
type Watched struct {
	Kind      string // "movie" or "tv"
	TMDBID    int
	Title     string
	Year      int
	Season    int
	Episode   int
	WatchedAt time.Time
}

// Rated is one rating, 1 to 10.
type Rated struct {
	Kind    string
	TMDBID  int
	Title   string
	Year    int
	Rating  int
	RatedAt time.Time
}

type historyItem struct {
	WatchedAt time.Time  `json:"watched_at"`
	RatedAt   time.Time  `json:"rated_at"`
	Rating    int        `json:"rating"`
	Type      string     `json:"type"`
	Movie     *mediaBody `json:"movie"`
	Show      *mediaBody `json:"show"`
	Episode   *struct {
		Season int `json:"season"`
		Number int `json:"number"`
	} `json:"episode"`
}

// historyPages caps how far back a history is read (100 to a page).
const historyPages = 100

// pages reads every page of a user's list at path, newest first.
func (c *Client) pages(ctx context.Context, path string, max int) ([]historyItem, error) {
	var all []historyItem
	for page := 1; page <= max; page++ {
		var items []historyItem
		h, err := c.getJSON(ctx, path, url.Values{"page": {strconv.Itoa(page)}, "limit": {"100"}}, &items)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		count, _ := strconv.Atoi(h.Get("X-Pagination-Page-Count"))
		if len(items) == 0 || page >= count {
			break
		}
	}
	return all, nil
}

// UserHistory is everything a public profile has watched, films and
// episodes, newest first.
func (c *Client) UserHistory(ctx context.Context, user string) ([]Watched, error) {
	items, err := c.pages(ctx, fmt.Sprintf("/users/%s/history", url.PathEscape(user)), historyPages)
	if err != nil {
		return nil, err
	}
	var out []Watched
	for _, it := range items {
		switch {
		case it.Type == "movie" && it.Movie != nil && it.Movie.IDs.TMDB > 0:
			out = append(out, Watched{Kind: "movie", TMDBID: it.Movie.IDs.TMDB, Title: it.Movie.Title, Year: it.Movie.Year, WatchedAt: it.WatchedAt})
		case it.Type == "episode" && it.Show != nil && it.Show.IDs.TMDB > 0 && it.Episode != nil:
			out = append(out, Watched{Kind: "tv", TMDBID: it.Show.IDs.TMDB, Title: it.Show.Title, Year: it.Show.Year, Season: it.Episode.Season, Episode: it.Episode.Number, WatchedAt: it.WatchedAt})
		}
	}
	return out, nil
}

// UserRatings is a public profile's ratings of films and shows.
func (c *Client) UserRatings(ctx context.Context, user string) ([]Rated, error) {
	var out []Rated
	for _, kind := range []string{"movies", "shows"} {
		items, err := c.pages(ctx, fmt.Sprintf("/users/%s/ratings/%s", url.PathEscape(user), kind), 20)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.Movie != nil && it.Movie.IDs.TMDB > 0 {
				out = append(out, Rated{Kind: "movie", TMDBID: it.Movie.IDs.TMDB, Title: it.Movie.Title, Year: it.Movie.Year, Rating: it.Rating, RatedAt: it.RatedAt})
			} else if it.Show != nil && it.Show.IDs.TMDB > 0 {
				out = append(out, Rated{Kind: "tv", TMDBID: it.Show.IDs.TMDB, Title: it.Show.Title, Year: it.Show.Year, Rating: it.Rating, RatedAt: it.RatedAt})
			}
		}
	}
	return out, nil
}
