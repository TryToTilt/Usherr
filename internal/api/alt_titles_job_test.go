package api_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// The background job picks the other titles of what is in the library once a
// language besides English is wanted, and a change of the wanted languages
// makes them be picked again.
func TestTitleNamesJobAndLanguageChange(t *testing.T) {
	server, base, client := loginNewServer(t)
	tmdb, calls := newNamesTMDB(t, []string{"Der Fixture Film"})
	server.TestSetTMDBBaseURL("k", tmdb.URL)
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "The Fixture Movie", Year: 1999, Monitored: true}); err != nil {
		t.Fatal(err)
	}

	// English only: nothing is asked and nothing is kept.
	server.TestTitleNamesJob(context.Background())
	if calls.Load() != 0 {
		t.Fatalf("TMDB was asked %d times while only English is wanted", calls.Load())
	}

	putJSONStatus(t, client, base+"/api/settings", map[string]any{"qualityLanguage": "German"}, http.StatusOK)
	server.TestTitleNamesJob(context.Background())
	got, found, err := server.MovieRepo.TitleNames("movie", 605)
	if err != nil || !found || !reflect.DeepEqual(got.Names, []string{"Der Fixture Film"}) || got.Languages != "German" {
		t.Fatalf("the German title was not kept: %+v found=%v err=%v", got, found, err)
	}
	if calls.Load() != 1 {
		t.Errorf("TMDB was asked %d times, want 1", calls.Load())
	}

	// Once picked, the titles are not asked for again.
	server.TestTitleNamesJob(context.Background())
	if calls.Load() != 1 {
		t.Errorf("TMDB was asked again for titles that were already picked (%d requests)", calls.Load())
	}
	movies, _ := server.MovieRepo.List()
	if len(movies) != 1 || !reflect.DeepEqual(movies[0].AltTitles, []string{"Der Fixture Film"}) {
		t.Fatalf("the movie does not carry its other titles: %+v", movies)
	}

	// Going back to English forgets them, so they are not searched for any more.
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"qualityLanguage": "English"}, http.StatusOK)
	if _, found, _ := server.MovieRepo.TitleNames("movie", 605); found {
		t.Error("the other titles should be forgotten when the wanted languages change")
	}
	movies, _ = server.MovieRepo.List()
	if len(movies[0].AltTitles) != 0 {
		t.Errorf("the movie still carries %v", movies[0].AltTitles)
	}
}
