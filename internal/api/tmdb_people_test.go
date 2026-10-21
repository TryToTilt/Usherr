package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// newPeopleTMDB serves one movie and one show with credits that carry person
// ids, and two people: one with a full page and one with almost nothing.
func newPeopleTMDB(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var personHits atomic.Int64
	mux := http.NewServeMux()
	for _, kind := range []string{"movie", "tv"} {
		mux.HandleFunc("/genre/"+kind+"/list", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"genres":[{"id":28,"name":"Action"}]}`))
		})
	}
	mux.HandleFunc("/movie/603", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":603,"title":"The Matrix","release_date":"1999-03-30",
			"credits":{"cast":[
				{"id":6384,"name":"Keanu Reeves","character":"Neo","order":0,"profile_path":"/keanu.jpg"},
				{"name":"Extra Without Id","character":"","order":1}],
			"crew":[
				{"id":9340,"name":"Lana Wachowski","job":"Director","profile_path":"/lana.jpg"},
				{"id":9339,"name":"Lilly Wachowski","job":"Director"},
				{"id":9340,"name":"Lana Wachowski","job":"Writer"},
				{"id":1,"name":"Someone Else","job":"Editor"}]}}`))
	})
	mux.HandleFunc("/tv/95396", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":95396,"name":"Severance","first_air_date":"2022-02-17",
			"created_by":[{"id":55,"name":"Dan Erickson","profile_path":"/dan.jpg"},{"id":0,"name":""}],
			"credits":{"cast":[]}}`))
	})
	mux.HandleFunc("/person/6384", func(w http.ResponseWriter, r *http.Request) {
		personHits.Add(1)
		if r.URL.Query().Get("append_to_response") != "combined_credits" {
			// The English biography lookup for a language without one.
			_, _ = w.Write([]byte(`{"biography":"Canadian actor."}`))
			return
		}
		bio := "Canadian actor."
		if r.URL.Query().Get("language") != "en-US" {
			bio = ""
		}
		_, _ = w.Write([]byte(`{"id":6384,"name":"Keanu Reeves","biography":"` + bio + `","birthday":"1964-09-02","deathday":null,
			"place_of_birth":"Beirut, Lebanon","profile_path":"/keanu.jpg","known_for_department":"Acting","imdb_id":"nm0000206",
			"combined_credits":{"cast":[
				{"id":603,"media_type":"movie","title":"The Matrix","release_date":"1999-03-30","poster_path":"/m.jpg","popularity":80,"vote_average":8.24,"genre_ids":[28],"character":"Neo"},
				{"id":95396,"media_type":"tv","name":"Severance","first_air_date":"2022-02-17","popularity":20,"character":"Guest"}],
			"crew":[
				{"id":2,"media_type":"movie","title":"Man of Tai Chi","release_date":"2013-11-01","popularity":10,"department":"Directing","job":"Director"}]}}`))
	})
	mux.HandleFunc("/person/7", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":7,"name":"Nobody Known","biography":"","birthday":"","profile_path":null,"combined_credits":{"cast":[],"crew":[]}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &personHits
}

func TestTitlePagesCarryPeopleIDs(t *testing.T) {
	server, base, client := loginNewServer(t)
	tmdb, _ := newPeopleTMDB(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	m := getJSON[map[string]any](t, client, base+"/api/tmdb/movies/603")
	cast := m["cast"].([]any)
	first := cast[0].(map[string]any)
	if first["id"] != float64(6384) || first["profileUrl"] != "https://image.tmdb.org/t/p/w185/keanu.jpg" {
		t.Errorf("cast with id and photo: %+v", first)
	}
	if _, has := cast[1].(map[string]any)["id"]; has {
		t.Errorf("a cast member TMDB gave no id for must not get one: %+v", cast[1])
	}
	var got []string
	for _, c := range m["crew"].([]any) {
		c := c.(map[string]any)
		got = append(got, c["job"].(string)+":"+c["name"].(string))
	}
	// Directors first (twice is one), then the writer; the editor is not key crew.
	if want := "Director:Lana Wachowski,Director:Lilly Wachowski,Writer:Lana Wachowski"; strings.Join(got, ",") != want {
		t.Errorf("crew %v, want %s", got, want)
	}

	tv := getJSON[map[string]any](t, client, base+"/api/tmdb/tv/95396")
	crew := tv["crew"].([]any)
	if len(crew) != 1 || crew[0].(map[string]any)["id"] != float64(55) || crew[0].(map[string]any)["job"] != "Creator" {
		t.Errorf("creators %+v", crew)
	}
}

func TestPersonEndpoint(t *testing.T) {
	server, base, client := loginNewServer(t)
	tmdb, hits := newPeopleTMDB(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	p := getJSON[map[string]any](t, client, base+"/api/tmdb/people/6384")
	if p["name"] != "Keanu Reeves" || p["birthday"] != "1964-09-02" || p["deathday"] != nil || p["placeOfBirth"] != "Beirut, Lebanon" ||
		p["photoUrl"] != "https://image.tmdb.org/t/p/w342/keanu.jpg" || p["biography"] != "Canadian actor." {
		t.Fatalf("person: %+v", p)
	}
	movies := p["movies"].([]any)
	if len(movies) != 2 || movies[0].(map[string]any)["title"] != "The Matrix" || movies[1].(map[string]any)["title"] != "Man of Tai Chi" {
		t.Fatalf("movies should be the most popular first: %+v", movies)
	}
	matrix := movies[0].(map[string]any)
	if matrix["character"] != "Neo" || matrix["mediaType"] != "movie" || matrix["rating"] != 8.2 || strings.Join(toStrings(matrix["genres"]), ",") != "Action" ||
		matrix["posterUrl"] != "https://image.tmdb.org/t/p/w500/m.jpg" || strings.Join(toStrings(matrix["roles"]), ",") != "acting" {
		t.Errorf("matrix card: %+v", matrix)
	}
	if shows := p["shows"].([]any); len(shows) != 1 || shows[0].(map[string]any)["mediaType"] != "tv" {
		t.Errorf("shows: %+v", shows)
	}
	if got := strings.Join(toStrings(p["workKinds"]), ","); got != "acting,directing" {
		t.Errorf("work kinds %q: the filter needs both", got)
	}

	// Asking again costs nothing.
	before := hits.Load()
	getJSON[map[string]any](t, client, base+"/api/tmdb/people/6384")
	if hits.Load() != before {
		t.Error("a person already looked up went back to TMDB")
	}
}

func TestPersonWithNothingToShow(t *testing.T) {
	server, base, client := loginNewServer(t)
	tmdb, _ := newPeopleTMDB(t)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	p := getJSON[map[string]any](t, client, base+"/api/tmdb/people/7")
	if p["name"] != "Nobody Known" || p["photoUrl"] != nil || p["biography"] != nil || p["birthday"] != nil {
		t.Errorf("person: %+v", p)
	}
	if len(p["movies"].([]any)) != 0 || len(p["shows"].([]any)) != 0 || len(p["workKinds"].([]any)) != 0 {
		t.Errorf("no credits should give empty lists, not null: %+v", p)
	}
}

func TestPersonEndpointErrors(t *testing.T) {
	server, base, client := loginNewServer(t)
	tmdb, _ := newPeopleTMDB(t)
	for path, want := range map[string]int{
		"/api/tmdb/people/abc": http.StatusBadRequest,
		"/api/tmdb/people/0":   http.StatusBadRequest,
		"/api/tmdb/people/1":   http.StatusPreconditionFailed, // no key yet
	} {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: status %d, want %d", path, resp.StatusCode, want)
		}
	}
	server.TestSetTMDBBaseURL("k", tmdb.URL)
	resp, err := client.Get(base + "/api/tmdb/people/99999")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown person: status %d, want 404", resp.StatusCode)
	}
}
