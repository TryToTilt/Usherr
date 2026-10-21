package metadata

// Home fork: what "Complete your library" (after CinePlete) needs from TMDB:
// a film's directors, leading cast and recommendations in one call, and the
// films a person directed or starred in.

import (
	"context"
	"net/url"
	"sort"
	"strconv"
)

// PersonRef is a person named on a film.
type PersonRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// FilmPeople is who made a film, who leads it, and what TMDB recommends after it.
type FilmPeople struct {
	Directors []PersonRef `json:"directors"`
	Cast      []PersonRef `json:"cast"` // the first five billed
	Recs      []int       `json:"recs"` // recommended films, TMDB ids
}

// GetFilmPeople reads a film's directors, leading cast and recommendations.
func (c *Client) GetFilmPeople(ctx context.Context, tmdbID int) (*FilmPeople, error) {
	var resp struct {
		Credits struct {
			Cast []struct {
				ID    int    `json:"id"`
				Name  string `json:"name"`
				Order int    `json:"order"`
			} `json:"cast"`
			Crew []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
				Job  string `json:"job"`
			} `json:"crew"`
		} `json:"credits"`
		Recommendations struct {
			Results []struct {
				ID int `json:"id"`
			} `json:"results"`
		} `json:"recommendations"`
	}
	if err := c.get(ctx, "/movie/"+strconv.Itoa(tmdbID), url.Values{"append_to_response": {"credits,recommendations"}}, &resp); err != nil {
		return nil, err
	}
	out := &FilmPeople{Directors: []PersonRef{}, Cast: []PersonRef{}, Recs: []int{}}
	for _, p := range resp.Credits.Crew {
		if p.Job == "Director" {
			out.Directors = append(out.Directors, PersonRef{ID: p.ID, Name: p.Name})
		}
	}
	sort.SliceStable(resp.Credits.Cast, func(i, j int) bool { return resp.Credits.Cast[i].Order < resp.Credits.Cast[j].Order })
	for _, p := range resp.Credits.Cast {
		if len(out.Cast) == 5 {
			break
		}
		out.Cast = append(out.Cast, PersonRef{ID: p.ID, Name: p.Name})
	}
	for _, r := range resp.Recommendations.Results {
		out.Recs = append(out.Recs, r.ID)
	}
	return out, nil
}

// PersonFilm is one film in a person's filmography.
type PersonFilm struct {
	Movie
	Order int `json:"order"` // billing, for acting
}

// PersonFilms lists the films a person directed and the ones they acted in,
// with TMDB's details for each (votes, popularity, release date).
func (c *Client) PersonFilms(ctx context.Context, id int) (directed []Movie, acted []PersonFilm, err error) {
	var resp struct {
		Cast []PersonFilm `json:"cast"`
		Crew []struct {
			Movie
			Job string `json:"job"`
		} `json:"crew"`
	}
	if err := c.get(ctx, "/person/"+strconv.Itoa(id)+"/movie_credits", nil, &resp); err != nil {
		return nil, nil, err
	}
	seen := map[int]bool{}
	for _, m := range resp.Crew {
		if m.Job == "Director" && !seen[m.TMDBID] {
			seen[m.TMDBID] = true
			directed = append(directed, m.Movie)
		}
	}
	return directed, resp.Cast, nil
}

// Person is someone's TMDB page: who they are, for the person page.
// A person's page reads GetPerson (people.go, from upstream).
