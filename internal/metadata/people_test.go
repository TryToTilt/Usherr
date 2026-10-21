package metadata_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
)

const personJSON = `{
	"id": 525, "name": "Christopher Nolan", "biography": "British-American filmmaker.",
	"birthday": "1970-07-30", "deathday": null, "place_of_birth": "London, England",
	"profile_path": "/nolan.jpg", "known_for_department": "Directing", "imdb_id": "nm0634240",
	"combined_credits": {
		"cast": [
			{"id": 1, "media_type": "movie", "title": "Following", "release_date": "1998-09-12", "popularity": 5.5, "character": "Cobb"},
			{"id": 2, "media_type": "tv", "name": "Some Talk Show", "first_air_date": "2010-01-01", "popularity": 30, "character": "Himself"},
			{"id": 9, "media_type": "movie", "title": "No Date Cut", "popularity": 1, "character": " "}
		],
		"crew": [
			{"id": 1, "media_type": "movie", "title": "Following", "release_date": "1998-09-12", "popularity": 5.5, "department": "Directing", "job": "Director"},
			{"id": 1, "media_type": "movie", "title": "Following", "release_date": "1998-09-12", "popularity": 5.5, "department": "Writing", "job": "Writer"},
			{"id": 1, "media_type": "movie", "title": "Following", "release_date": "1998-09-12", "popularity": 5.5, "department": "Production", "job": "Producer"},
			{"id": 3, "media_type": "movie", "title": "Inception", "release_date": "2010-07-15", "popularity": 90, "department": "Directing", "job": "Director", "poster_path": "/inc.jpg", "vote_average": 8.36, "genre_ids": [28]},
			{"id": 4, "media_type": "movie", "title": "Same Fame Older", "release_date": "2001-01-01", "popularity": 5.5, "department": "Writing", "job": "Writer"},
			{"id": 5, "media_type": "movie", "title": "Same Fame Newer", "release_date": "2005-01-01", "popularity": 5.5, "department": "Writing", "job": "Writer"},
			{"id": 6, "media_type": "movie", "title": "Adult Title", "adult": true, "popularity": 99, "department": "Directing"},
			{"id": 0, "media_type": "movie", "title": "No Id", "popularity": 99, "department": "Directing"},
			{"id": 7, "media_type": "movie", "title": "  ", "popularity": 99, "department": "Directing"},
			{"id": 8, "media_type": "person", "title": "Not A Title", "popularity": 99},
			{"id": 10, "media_type": "tv", "name": "Dark Show", "first_air_date": "2020-05-05", "popularity": 12, "department": "Writing", "job": "Creator"}
		]
	}
}`

func TestFilmographyMergesRolesAndSorts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(personJSON))
	}))
	defer srv.Close()
	p, err := metadata.NewWithBaseURL("k", srv.URL).GetPerson(context.Background(), 525)
	if err != nil {
		t.Fatal(err)
	}
	movies, shows := p.Filmography()

	var titles []string
	for _, m := range movies {
		titles = append(titles, m.Title)
	}
	// Popularity first; the three at 5.5 go newest first; the least popular last.
	if got, want := strings.Join(titles, "|"), "Inception|Same Fame Newer|Same Fame Older|Following|No Date Cut"; got != want {
		t.Errorf("movie order %q, want %q", got, want)
	}

	byTitle := map[string]metadata.FilmographyItem{}
	for _, m := range movies {
		byTitle[m.Title] = m
	}
	rolesCases := []struct {
		title     string
		roles     string
		character string
	}{
		{"Following", "acting,directing,writing,other", "Cobb"}, // four jobs on one title, one card
		{"Inception", "directing", ""},
		{"Same Fame Older", "writing", ""},
		{"No Date Cut", "acting", ""}, // a blank character stays blank
	}
	for _, c := range rolesCases {
		got := byTitle[c.title]
		if strings.Join(got.Roles, ",") != c.roles || got.Character != c.character {
			t.Errorf("%s: roles %v character %q, want %s / %q", c.title, got.Roles, got.Character, c.roles, c.character)
		}
	}

	if len(shows) != 2 || shows[0].Title != "Some Talk Show" || shows[1].Title != "Dark Show" {
		t.Fatalf("shows %+v", shows)
	}
	if shows[1].Roles[0] != "writing" || shows[1].Year() != 2020 || shows[1].MediaType != "tv" {
		t.Errorf("show item %+v", shows[1])
	}
	if got := byTitle["Inception"].Movie(); got.TMDBID != 3 || got.PosterPath != "/inc.jpg" || got.Year() != 2010 || len(got.GenreIDs) != 1 {
		t.Errorf("as movie: %+v", got)
	}
	if got := shows[1].Show(); got.Name != "Dark Show" || got.FirstAirDate != "2020-05-05" {
		t.Errorf("as show: %+v", got)
	}
	if got := strings.Join(metadata.WorkKinds(movies, shows), ","); got != "acting,directing,writing" {
		t.Errorf("work kinds %q", got)
	}
}

func TestWorkKinds(t *testing.T) {
	item := func(roles ...string) metadata.FilmographyItem { return metadata.FilmographyItem{Roles: roles} }
	for _, c := range []struct {
		name          string
		movies, shows []metadata.FilmographyItem
		want          string
	}{
		{"nothing", nil, nil, ""},
		{"actor only", []metadata.FilmographyItem{item("acting")}, nil, "acting"},
		{"others do not count", []metadata.FilmographyItem{item("other")}, nil, ""},
		{"spread over movies and shows", []metadata.FilmographyItem{item("writing")}, []metadata.FilmographyItem{item("acting", "other")}, "acting,writing"},
	} {
		if got := strings.Join(metadata.WorkKinds(c.movies, c.shows), ","); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestGetPersonCachesAndFillsBiographyInEnglish(t *testing.T) {
	var full, bio atomic.Int32
	var langs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/person/525" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		langs = append(langs, r.URL.Query().Get("language"))
		if r.URL.Query().Get("append_to_response") == "combined_credits" {
			full.Add(1)
			// German TMDB data, without a German biography.
			_, _ = w.Write([]byte(`{"id":525,"name":"Christopher Nolan","biography":"","combined_credits":{"cast":[],"crew":[]}}`))
			return
		}
		bio.Add(1)
		_, _ = w.Write([]byte(`{"biography":"English text."}`))
	}))
	defer srv.Close()

	c := metadata.NewWithBaseURL("k", srv.URL).Localized("de-DE")
	for i := 0; i < 3; i++ {
		p, err := c.GetPerson(context.Background(), 525)
		if err != nil {
			t.Fatal(err)
		}
		if p.Biography != "English text." {
			t.Fatalf("biography %q, want the English one when the chosen language has none", p.Biography)
		}
	}
	if full.Load() != 1 || bio.Load() != 1 {
		t.Errorf("TMDB was asked %d and %d times, want once each (the answer is cached)", full.Load(), bio.Load())
	}
	if langs[0] != "de-DE" || langs[1] != "en-US" {
		t.Errorf("languages asked %v, want de-DE then en-US", langs)
	}
}

func TestGetPersonErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	_, err := metadata.NewWithBaseURL("k", srv.URL).GetPerson(context.Background(), 1)
	var se *metadata.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("want a 404 status error, got %v", err)
	}
	if _, err := metadata.New("").GetPerson(context.Background(), 1); !errors.Is(err, metadata.ErrNoAPIKey) {
		t.Errorf("without a key: %v", err)
	}
}
