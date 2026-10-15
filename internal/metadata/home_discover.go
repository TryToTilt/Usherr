package metadata

// Home fork: Discover by TMDB keyword (the seasonal rails: "halloween",
// "christmas"…). Keywords are looked up by name once and kept with the
// other cached pages.

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// KeywordID returns TMDB's id for a keyword, matched by its exact name
// (case aside), or 0 when TMDB has none.
func (c *Client) KeywordID(ctx context.Context, name string) (int, error) {
	key := "keyword:" + strings.ToLower(name)
	if v, ok := c.pages.get(key); ok {
		return v.(int), nil
	}
	var resp struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/keyword", url.Values{"query": {name}}, &resp); err != nil {
		return 0, err
	}
	id := 0
	for _, k := range resp.Results {
		if strings.EqualFold(k.Name, name) {
			id = k.ID
			break
		}
	}
	c.pages.put(key, id)
	return id, nil
}

// KeywordQuery is a discover by keywords: titles with any of the keywords,
// most popular first, leaving out the barely known.
type KeywordQuery struct {
	Keywords []int
	Genres   []int // any of them, when set
	MinVotes int
	Page     int
}

func (q KeywordQuery) params() url.Values {
	v := url.Values{"include_adult": {"false"}, "sort_by": {"popularity.desc"}}
	ids := make([]string, len(q.Keywords))
	for i, k := range q.Keywords {
		ids[i] = strconv.Itoa(k)
	}
	v.Set("with_keywords", strings.Join(ids, "|"))
	if len(q.Genres) > 0 {
		gs := make([]string, len(q.Genres))
		for i, g := range q.Genres {
			gs[i] = strconv.Itoa(g)
		}
		v.Set("with_genres", strings.Join(gs, "|"))
	}
	if q.MinVotes > 0 {
		v.Set("vote_count.gte", strconv.Itoa(q.MinVotes))
	}
	v.Set("page", strconv.Itoa(ClampPage(q.Page)))
	return v
}

// DiscoverMoviesByKeyword is one page of movies with any of the keywords.
func (c *Client) DiscoverMoviesByKeyword(ctx context.Context, q KeywordQuery) (*MoviePage, error) {
	return c.moviePage(ctx, "/discover/movie", q.params())
}

// DiscoverTVByKeyword is DiscoverMoviesByKeyword for shows.
func (c *Client) DiscoverTVByKeyword(ctx context.Context, q KeywordQuery) (*ShowPage, error) {
	return c.showPage(ctx, "/discover/tv", q.params())
}

// PersonID returns the TMDB id of the best-known person by that name, or 0.
func (c *Client) PersonID(ctx context.Context, name string) (int, error) {
	var resp struct {
		Results []struct {
			ID         int     `json:"id"`
			Name       string  `json:"name"`
			Popularity float64 `json:"popularity"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/person", url.Values{"query": {name}}, &resp); err != nil {
		return 0, err
	}
	best, bestPop := 0, -1.0
	for _, p := range resp.Results {
		if strings.EqualFold(p.Name, name) && p.Popularity > bestPop {
			best, bestPop = p.ID, p.Popularity
		}
	}
	if best == 0 && len(resp.Results) > 0 {
		best = resp.Results[0].ID
	}
	return best, nil
}

// PersonWork is what a person made: the films and shows they directed or
// wrote, or acted in a main part of, best known first.
func (c *Client) PersonWork(ctx context.Context, id int) ([]Movie, []Show, error) {
	var mv struct {
		Cast []struct {
			Movie
			Order int `json:"order"`
		} `json:"cast"`
		Crew []struct {
			Movie
			Job string `json:"job"`
		} `json:"crew"`
	}
	if err := c.get(ctx, "/person/"+strconv.Itoa(id)+"/movie_credits", nil, &mv); err != nil {
		return nil, nil, err
	}
	var tv struct {
		Cast []struct {
			Show
			EpisodeCount int `json:"episode_count"`
		} `json:"cast"`
		Crew []struct {
			Show
			Job string `json:"job"`
		} `json:"crew"`
	}
	if err := c.get(ctx, "/person/"+strconv.Itoa(id)+"/tv_credits", nil, &tv); err != nil {
		return nil, nil, err
	}
	// Released or dated work only, and no documentaries they only appear in.
	documentary := func(ids []int) bool {
		for _, g := range ids {
			if g == 99 {
				return true
			}
		}
		return false
	}
	seenM, seenS := map[int]bool{}, map[int]bool{}
	var movies []Movie
	var shows []Show
	for _, m := range mv.Crew {
		if (m.Job == "Director" || m.Job == "Screenplay" || m.Job == "Writer") && m.ReleaseDate != "" && !seenM[m.TMDBID] {
			seenM[m.TMDBID] = true
			movies = append(movies, m.Movie)
		}
	}
	for _, m := range mv.Cast {
		if m.Order <= 6 && m.ReleaseDate != "" && !documentary(m.GenreIDs) && !seenM[m.TMDBID] {
			seenM[m.TMDBID] = true
			movies = append(movies, m.Movie)
		}
	}
	for _, s := range tv.Crew {
		if (s.Job == "Director" || s.Job == "Creator" || s.Job == "Executive Producer" || s.Job == "Writer") && !seenS[s.TMDBID] {
			seenS[s.TMDBID] = true
			shows = append(shows, s.Show)
		}
	}
	for _, s := range tv.Cast {
		if s.EpisodeCount >= 5 && !documentary(s.GenreIDs) && !seenS[s.TMDBID] {
			seenS[s.TMDBID] = true
			shows = append(shows, s.Show)
		}
	}
	sort.SliceStable(movies, func(i, j int) bool { return movies[i].Popularity > movies[j].Popularity })
	sort.SliceStable(shows, func(i, j int) bool { return shows[i].Popularity > shows[j].Popularity })
	return movies, shows, nil
}

// CollectionID returns the TMDB id of the film series by that name, or 0.
func (c *Client) CollectionID(ctx context.Context, name string) (int, error) {
	var resp struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/collection", url.Values{"query": {name}}, &resp); err != nil {
		return 0, err
	}
	if len(resp.Results) == 0 {
		return 0, nil
	}
	// The series named exactly so ("Alien" is "Alien Collection", not
	// "Alien Sharks Collection"), else TMDB's first answer.
	norm := func(v string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		v = strings.TrimSuffix(v, " collection")
		return strings.TrimPrefix(v, "the ")
	}
	want := norm(name)
	for _, r := range resp.Results {
		if norm(r.Name) == want {
			return r.ID, nil
		}
	}
	return resp.Results[0].ID, nil
}

// Anime lists (home fork): Japanese animated shows from TMDB's discover,
// as "airing", "popular", "top" or "new".
func (c *Client) DiscoverAnime(ctx context.Context, list string, page int) (*ShowPage, error) {
	v := url.Values{"include_adult": {"false"}, "with_genres": {"16"}, "with_original_language": {"ja"}, "page": {strconv.Itoa(ClampPage(page))}}
	now := time.Now()
	switch list {
	case "airing":
		v.Set("sort_by", "popularity.desc")
		v.Set("air_date.gte", now.AddDate(0, 0, -7).Format("2006-01-02"))
		v.Set("air_date.lte", now.AddDate(0, 0, 7).Format("2006-01-02"))
	case "top":
		v.Set("sort_by", "vote_average.desc")
		v.Set("vote_count.gte", "300")
	case "new":
		v.Set("sort_by", "popularity.desc")
		v.Set("first_air_date.gte", now.AddDate(-1, 0, 0).Format("2006-01-02"))
		v.Set("first_air_date.lte", now.Format("2006-01-02"))
	default:
		v.Set("sort_by", "popularity.desc")
		v.Set("vote_count.gte", "50")
	}
	return c.showPage(ctx, "/discover/tv", v)
}
