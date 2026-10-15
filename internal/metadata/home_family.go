package metadata

// Home fork: a title's US age rating, for the family filter on Discover.

import (
	"context"
	"strconv"
)

// MovieUSRating is a film's US certification ("PG-13"), the theatrical one
// first; "" when TMDB has none.
func (c *Client) MovieUSRating(ctx context.Context, id int) (string, error) {
	var res struct {
		Results []struct {
			Country      string `json:"iso_3166_1"`
			ReleaseDates []struct {
				Certification string `json:"certification"`
				Type          int    `json:"type"`
			} `json:"release_dates"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/movie/"+strconv.Itoa(id)+"/release_dates", nil, &res); err != nil {
		return "", err
	}
	for _, r := range res.Results {
		if r.Country != "US" {
			continue
		}
		best := ""
		for _, d := range r.ReleaseDates {
			if d.Certification == "" {
				continue
			}
			if d.Type == 3 { // theatrical
				return d.Certification, nil
			}
			if best == "" {
				best = d.Certification
			}
		}
		return best, nil
	}
	return "", nil
}

// ShowUSRating is a show's US TV rating ("TV-14"); "" when TMDB has none.
func (c *Client) ShowUSRating(ctx context.Context, id int) (string, error) {
	var res struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/tv/"+strconv.Itoa(id)+"/content_ratings", nil, &res); err != nil {
		return "", err
	}
	for _, r := range res.Results {
		if r.Country == "US" {
			return r.Rating, nil
		}
	}
	return "", nil
}
