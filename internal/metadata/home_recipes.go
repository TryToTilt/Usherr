package metadata

// Home fork: what Discover's recipes need from TMDB: a discover with any
// filters, the streaming services of a country, where one title streams, and
// people and keywords to filter by.

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// DiscoverMoviesWith is one page of /discover/movie with these parameters.
func (c *Client) DiscoverMoviesWith(ctx context.Context, v url.Values) (*MoviePage, error) {
	return c.moviePage(ctx, "/discover/movie", v)
}

// DiscoverTVWith is DiscoverMoviesWith for shows.
func (c *Client) DiscoverTVWith(ctx context.Context, v url.Values) (*ShowPage, error) {
	return c.showPage(ctx, "/discover/tv", v)
}

// Provider is a streaming service.
type Provider struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Logo     string `json:"logo,omitempty"`
	Priority int    `json:"-"`
}

type cvProvider struct {
	ID       int    `json:"provider_id"`
	Name     string `json:"provider_name"`
	Logo     string `json:"logo_path"`
	Priority int    `json:"display_priority"`
}

func (p cvProvider) provider() Provider {
	out := Provider{ID: p.ID, Name: p.Name, Priority: p.Priority}
	if p.Logo != "" {
		out.Logo = ImageURL("w92", p.Logo)
	}
	return out
}

// Providers lists the streaming services TMDB knows in a country (films and
// shows together), the best known first.
func (c *Client) Providers(ctx context.Context, region string) ([]Provider, error) {
	key := "providers:" + region
	if v, ok := c.pages.get(key); ok {
		return v.([]Provider), nil
	}
	seen := map[int]Provider{}
	for _, kind := range []string{"movie", "tv"} {
		var resp struct {
			Results []cvProvider `json:"results"`
		}
		if err := c.get(ctx, "/watch/providers/"+kind, url.Values{"watch_region": {region}}, &resp); err != nil {
			return nil, err
		}
		for _, p := range resp.Results {
			if old, ok := seen[p.ID]; !ok || p.Priority < old.Priority {
				seen[p.ID] = p.provider()
			}
		}
	}
	out := make([]Provider, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Name < out[j].Name
	})
	c.pages.put(key, out)
	return out, nil
}

// TitleProviders lists the services a title streams on in a country
// (included with the subscription, or free), and TMDB's page for it.
func (c *Client) TitleProviders(ctx context.Context, kind string, id int, region string) ([]Provider, string, error) {
	path := "/movie/"
	if kind == "tv" {
		path = "/tv/"
	}
	var resp struct {
		Results map[string]struct {
			Link     string       `json:"link"`
			Flatrate []cvProvider `json:"flatrate"`
			Free     []cvProvider `json:"free"`
			Ads      []cvProvider `json:"ads"`
		} `json:"results"`
	}
	if err := c.get(ctx, path+strconv.Itoa(id)+"/watch/providers", nil, &resp); err != nil {
		return nil, "", err
	}
	r := resp.Results[strings.ToUpper(region)]
	out := []Provider{}
	seen := map[int]bool{}
	for _, list := range [][]cvProvider{r.Flatrate, r.Free, r.Ads} {
		for _, p := range list {
			if !seen[p.ID] {
				seen[p.ID] = true
				out = append(out, p.provider())
			}
		}
	}
	return out, r.Link, nil
}

// Named is a TMDB person or keyword found by name.
type Named struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Known string `json:"known,omitempty"` // a person: what they're known for (Acting, Directing)
	Image string `json:"image,omitempty"`
}

// SearchPeople finds people by name, the best known first.
func (c *Client) SearchPeople(ctx context.Context, q string) ([]Named, error) {
	var resp struct {
		Results []struct {
			ID         int     `json:"id"`
			Name       string  `json:"name"`
			Known      string  `json:"known_for_department"`
			Profile    string  `json:"profile_path"`
			Popularity float64 `json:"popularity"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/person", url.Values{"query": {q}}, &resp); err != nil {
		return nil, err
	}
	sort.SliceStable(resp.Results, func(i, j int) bool { return resp.Results[i].Popularity > resp.Results[j].Popularity })
	out := []Named{}
	for _, p := range resp.Results {
		n := Named{ID: p.ID, Name: p.Name, Known: p.Known}
		if p.Profile != "" {
			n.Image = ImageURL("w92", p.Profile)
		}
		out = append(out, n)
		if len(out) == 8 {
			break
		}
	}
	return out, nil
}

// SearchKeywords finds TMDB keywords ("heist", "time travel") by name.
func (c *Client) SearchKeywords(ctx context.Context, q string) ([]Named, error) {
	var resp struct {
		Results []Named `json:"results"`
	}
	if err := c.get(ctx, "/search/keyword", url.Values{"query": {q}}, &resp); err != nil {
		return nil, err
	}
	// The exact match first, then the shortest (the most general).
	sort.SliceStable(resp.Results, func(i, j int) bool {
		a, b := strings.EqualFold(resp.Results[i].Name, q), strings.EqualFold(resp.Results[j].Name, q)
		if a != b {
			return a
		}
		return len(resp.Results[i].Name) < len(resp.Results[j].Name)
	})
	if len(resp.Results) > 10 {
		resp.Results = resp.Results[:10]
	}
	return resp.Results, nil
}
