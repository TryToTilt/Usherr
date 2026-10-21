package metadata

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// A person's page changes rarely, so what TMDB says about them is kept far
// longer than a Discover list.
const personCacheTTL = 6 * time.Hour

// The kinds of work a filmography is split by.
const (
	RoleActing    = "acting"
	RoleDirecting = "directing"
	RoleWriting   = "writing"
	RoleOther     = "other" // producing, camera, sound and the rest
)

// PersonCredit is one line of a person's combined credits (GET /person/{id}).
type PersonCredit struct {
	ID           int     `json:"id"`
	MediaType    string  `json:"media_type"`
	Title        string  `json:"title"` // movies
	Name         string  `json:"name"`  // shows
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	PosterPath   string  `json:"poster_path"`
	Overview     string  `json:"overview"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	Popularity   float64 `json:"popularity"`
	GenreIDs     []int   `json:"genre_ids"`
	Adult        bool    `json:"adult"`
	Character    string  `json:"character"`  // cast credits
	Job          string  `json:"job"`        // crew credits
	Department   string  `json:"department"` // crew credits
}

// PersonDetail is GET /person/{id} with the person's movie and TV credits
// appended in the same request.
type PersonDetail struct {
	ID                 int     `json:"id"`
	Name               string  `json:"name"`
	Biography          string  `json:"biography"`
	Birthday           string  `json:"birthday"`
	Deathday           string  `json:"deathday"`
	PlaceOfBirth       string  `json:"place_of_birth"`
	ProfilePath        string  `json:"profile_path"`
	KnownForDepartment string  `json:"known_for_department"`
	IMDBID             string  `json:"imdb_id"`
	Popularity         float64 `json:"popularity"`
	CombinedCredits    struct {
		Cast []PersonCredit `json:"cast"`
		Crew []PersonCredit `json:"crew"`
	} `json:"combined_credits"`
}

// GetPerson fetches a person and everything they are credited on in one call.
// A biography TMDB has not translated into the chosen language is read in
// English instead, with one more request. The answer is kept for hours and
// must not be modified.
func (c *Client) GetPerson(ctx context.Context, id int) (*PersonDetail, error) {
	key := fmt.Sprintf("person:%d", id)
	if v, ok := c.pages.get(key); ok {
		return v.(*PersonDetail), nil
	}
	var d PersonDetail
	q := url.Values{"append_to_response": {"combined_credits"}}
	if err := c.get(ctx, fmt.Sprintf("/person/%d", id), q, &d); err != nil {
		return nil, err
	}
	if strings.TrimSpace(d.Biography) == "" && c.lang() != DefaultLanguage {
		var en struct {
			Biography string `json:"biography"`
		}
		// The biography is garnish: if this fails the page just has none.
		if err := c.get(ctx, fmt.Sprintf("/person/%d", id), url.Values{"language": {DefaultLanguage}}, &en); err == nil {
			d.Biography = en.Biography
		}
	}
	c.pages.putFor(key, &d, personCacheTTL)
	return &d, nil
}

// FilmographyItem is one title a person worked on, with every kind of work
// they did on it.
type FilmographyItem struct {
	MediaType   string // "movie" or "tv"
	TMDBID      int
	Title       string
	ReleaseDate string // release date, or first air date for a show
	PosterPath  string
	Overview    string
	VoteAverage float64
	VoteCount   int
	Popularity  float64
	GenreIDs    []int
	Roles       []string // Role* values, acting first
	Character   string   // the part played, when they acted in it
}

// Movie is the item as a movie, for the code that builds Discover cards.
func (f FilmographyItem) Movie() Movie {
	return Movie{TMDBID: f.TMDBID, Title: f.Title, Overview: f.Overview, ReleaseDate: f.ReleaseDate, PosterPath: f.PosterPath,
		VoteAverage: f.VoteAverage, VoteCount: f.VoteCount, Popularity: f.Popularity, GenreIDs: f.GenreIDs}
}

// Show is the item as a show.
func (f FilmographyItem) Show() Show {
	return Show{TMDBID: f.TMDBID, Name: f.Title, Overview: f.Overview, FirstAirDate: f.ReleaseDate, PosterPath: f.PosterPath,
		VoteAverage: f.VoteAverage, VoteCount: f.VoteCount, Popularity: f.Popularity, GenreIDs: f.GenreIDs}
}

// Year is the year of the release or first air date, or 0.
func (f FilmographyItem) Year() int { return yearOfDate(f.ReleaseDate) }

var roleOrder = map[string]int{RoleActing: 0, RoleDirecting: 1, RoleWriting: 2, RoleOther: 3}

// roleOfDepartment sorts a crew credit into the kind of work it is.
func roleOfDepartment(dept string) string {
	switch dept {
	case "Directing":
		return RoleDirecting
	case "Writing":
		return RoleWriting
	}
	return RoleOther
}

// Filmography turns the combined credits into one list of movies and one of
// shows. A title the person worked on in several ways (acted in and directed)
// appears once with all its roles. Each list is sorted by popularity, then by
// year (newest first), then by title. Credits TMDB gives without an id or a
// title, and adult titles, are left out.
func (d *PersonDetail) Filmography() (movies, shows []FilmographyItem) {
	type key struct {
		kind string
		id   int
	}
	byKey := map[key]*FilmographyItem{}
	var order []key
	add := func(cr PersonCredit, role string) {
		kind := cr.MediaType
		if kind != "movie" && kind != "tv" {
			return
		}
		title := cr.Title
		if kind == "tv" {
			title = cr.Name
		}
		if cr.ID == 0 || strings.TrimSpace(title) == "" || cr.Adult {
			return
		}
		date := cr.ReleaseDate
		if kind == "tv" {
			date = cr.FirstAirDate
		}
		k := key{kind, cr.ID}
		it, ok := byKey[k]
		if !ok {
			it = &FilmographyItem{MediaType: kind, TMDBID: cr.ID, Title: title, ReleaseDate: date, PosterPath: cr.PosterPath, Overview: cr.Overview,
				VoteAverage: cr.VoteAverage, VoteCount: cr.VoteCount, Popularity: cr.Popularity, GenreIDs: cr.GenreIDs}
			byKey[k] = it
			order = append(order, k)
		}
		has := false
		for _, r := range it.Roles {
			if r == role {
				has = true
			}
		}
		if !has {
			it.Roles = append(it.Roles, role)
		}
		if role == RoleActing && it.Character == "" {
			it.Character = strings.TrimSpace(cr.Character)
		}
	}
	for _, cr := range d.CombinedCredits.Cast {
		add(cr, RoleActing)
	}
	for _, cr := range d.CombinedCredits.Crew {
		add(cr, roleOfDepartment(cr.Department))
	}
	for _, k := range order {
		it := byKey[k]
		sort.SliceStable(it.Roles, func(i, j int) bool { return roleOrder[it.Roles[i]] < roleOrder[it.Roles[j]] })
		if k.kind == "movie" {
			movies = append(movies, *it)
		} else {
			shows = append(shows, *it)
		}
	}
	sortFilmography(movies)
	sortFilmography(shows)
	return movies, shows
}

func sortFilmography(items []FilmographyItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Popularity != b.Popularity {
			return a.Popularity > b.Popularity
		}
		if a.Year() != b.Year() {
			return a.Year() > b.Year()
		}
		return a.Title < b.Title
	})
}

// WorkKinds lists which of acting, directing and writing the person has
// credits in, in that order. The page offers a filter when there are two or
// more.
func WorkKinds(movies, shows []FilmographyItem) []string {
	seen := map[string]bool{}
	for _, list := range [][]FilmographyItem{movies, shows} {
		for _, it := range list {
			for _, r := range it.Roles {
				seen[r] = true
			}
		}
	}
	out := []string{}
	for _, r := range []string{RoleActing, RoleDirecting, RoleWriting} {
		if seen[r] {
			out = append(out, r)
		}
	}
	return out
}
