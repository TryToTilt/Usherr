package metadata

// Home fork: what a title is made of, for the taste engine: its genres,
// TMDB keywords, main cast, director (films) or creators (shows), year and
// original language. One call per title, keywords and credits appended.

import (
	"context"
	"fmt"
	"net/url"
)

// TitleTraits is what the taste engine compares titles by.
type TitleTraits struct {
	Name     string   `json:"name"`
	Year     int      `json:"year,omitempty"`
	Lang     string   `json:"lang,omitempty"`
	Genres   []string `json:"genres,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
	Cast     []string `json:"cast,omitempty"` // the first few billed
	Makers   []string `json:"makers,omitempty"`
	Rating   float64  `json:"rating,omitempty"`
	Votes    int      `json:"votes,omitempty"`
}

const traitsCast = 6

// Traits fetches a film's or show's traits ("movie" or "tv"). Names come in
// the client's language; keywords are always English on TMDB.
func (c *Client) Traits(ctx context.Context, kind string, tmdbID int) (*TitleTraits, error) {
	var d struct {
		Title        string  `json:"title"`
		Name         string  `json:"name"`
		ReleaseDate  string  `json:"release_date"`
		FirstAirDate string  `json:"first_air_date"`
		Lang         string  `json:"original_language"`
		VoteAverage  float64 `json:"vote_average"`
		VoteCount    int     `json:"vote_count"`
		Genres       []Genre `json:"genres"`
		CreatedBy    []struct {
			Name string `json:"name"`
		} `json:"created_by"`
		Keywords struct {
			Keywords []Named `json:"keywords"` // films
			Results  []Named `json:"results"`  // shows
		} `json:"keywords"`
		Credits struct {
			Cast []struct {
				Name string `json:"name"`
			} `json:"cast"`
			Crew []struct {
				Name string `json:"name"`
				Job  string `json:"job"`
			} `json:"crew"`
		} `json:"credits"`
	}
	path := fmt.Sprintf("/movie/%d", tmdbID)
	if kind == "tv" {
		path = fmt.Sprintf("/tv/%d", tmdbID)
	}
	if err := c.get(ctx, path, url.Values{"append_to_response": {"keywords,credits"}}, &d); err != nil {
		return nil, err
	}
	t := &TitleTraits{Name: d.Title, Lang: d.Lang, Rating: d.VoteAverage, Votes: d.VoteCount}
	date := d.ReleaseDate
	if kind == "tv" {
		t.Name, date = d.Name, d.FirstAirDate
	}
	if len(date) >= 4 {
		fmt.Sscanf(date[:4], "%d", &t.Year)
	}
	for _, g := range d.Genres {
		t.Genres = append(t.Genres, g.Name)
	}
	for _, k := range append(d.Keywords.Keywords, d.Keywords.Results...) {
		t.Keywords = append(t.Keywords, k.Name)
	}
	for i, p := range d.Credits.Cast {
		if i == traitsCast {
			break
		}
		t.Cast = append(t.Cast, p.Name)
	}
	for _, p := range d.CreatedBy {
		t.Makers = append(t.Makers, p.Name)
	}
	if kind != "tv" {
		for _, p := range d.Credits.Crew {
			if p.Job == "Director" {
				t.Makers = append(t.Makers, p.Name)
			}
		}
	}
	return t, nil
}
