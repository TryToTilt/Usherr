package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/library"
)

// A title that always fails must not stand first in line forever: the job
// leaves it for a while and reaches the titles behind it.
func TestTitleNamesJobSkipsATitleThatFails(t *testing.T) {
	api.ResetTitleNamesState()
	t.Cleanup(api.ResetTitleNamesState)
	server, base, client := loginNewServer(t)
	good, goodCalls := newNamesTMDB(t, []string{"Der Fixture Film"})
	var brokenCalls atomic.Int32
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/movie/777" {
			brokenCalls.Add(1)
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		good.Config.Handler.ServeHTTP(w, r)
	}))
	defer tmdb.Close()
	server.TestSetTMDBBaseURL("k", tmdb.URL)
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 777, Title: "A Broken Title", Year: 1999, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "The Fixture Movie", Year: 1999, Monitored: true}); err != nil {
		t.Fatal(err)
	}
	putJSONStatus(t, client, base+"/api/settings", map[string]any{"qualityLanguage": "German"}, http.StatusOK)

	server.TestTitleNamesJob(context.Background()) // the broken one fails and TMDB is put on hold
	api.ResetTitleNamesPause()                     // the hold is over (it lasts minutes)
	server.TestTitleNamesJob(context.Background())
	if _, found, _ := server.MovieRepo.TitleNames("movie", 605); !found {
		t.Fatalf("the title behind the broken one was never reached (broken asked %d times, good %d)", brokenCalls.Load(), goodCalls.Load())
	}
	if brokenCalls.Load() != 1 {
		t.Errorf("the broken title was asked %d times, want 1", brokenCalls.Load())
	}
}
