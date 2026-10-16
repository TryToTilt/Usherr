package metadata

import (
	"context"
	"net/url"
	"strconv"
)

// Home fork (Usherr): what's in cinemas. TMDB's now-playing and upcoming
// lists are per country (region, such as "US"): a film's dates differ by
// country, and a country's cinemas only show what's out there.

// NowPlaying is one page of the films in cinemas in region now.
func (c *Client) NowPlaying(ctx context.Context, region string, page int) (*MoviePage, error) {
	return c.moviePage(ctx, "/movie/now_playing", cinemaParams(region, page))
}

// UpcomingInCinemas is one page of the films about to open in region's cinemas.
func (c *Client) UpcomingInCinemas(ctx context.Context, region string, page int) (*MoviePage, error) {
	return c.moviePage(ctx, "/movie/upcoming", cinemaParams(region, page))
}

func cinemaParams(region string, page int) url.Values {
	q := url.Values{"page": {strconv.Itoa(ClampPage(page))}}
	if region != "" {
		q.Set("region", region)
	}
	return q
}
