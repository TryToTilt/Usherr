package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// Titles TMDB no longer has are remembered as having no artwork. Otherwise a
// dozen of them at the front of the calendar would use up every run and the
// titles behind them would never get a backdrop.
func TestArtworkJobMovesPastTitlesTMDBDoesNotHave(t *testing.T) {
	server, _, _ := loginNewServer(t)
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/701/images" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"backdrops":[{"file_path":"/clean.jpg","iso_639_1":null,"vote_average":5,"width":1920}],"logos":[]}`)
	}))
	defer tmdb.Close()
	server.TestSetTMDBBaseURL("key", tmdb.URL)

	soon := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	later := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	for i := 0; i < 12; i++ {
		if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 801 + i, Title: fmt.Sprintf("Gone %d", i), Year: 2027, Monitored: true, ReleaseDate: soon}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 701, Title: "Night Harbour", Year: 2027, Monitored: true, ReleaseDate: later}); err != nil {
		t.Fatal(err)
	}

	server.TestRunArtworkJob() // the twelve that TMDB does not have use up this run
	server.TestRunArtworkJob() // so the next one reaches the last title
	found, err := server.MovieRepo.TitleArt("movie", []int{701})
	if err != nil {
		t.Fatal(err)
	}
	if found[701].CheckedAt == "" {
		t.Fatal("the title behind the missing ones was never asked about")
	}
}
