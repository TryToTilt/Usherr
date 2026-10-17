package api_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// A TMDB key that is turned down fails for every show, so the refresh stops
// after the first instead of trying every monitored show.
func TestRefreshAllSeriesStopsOnARejectedKey(t *testing.T) {
	server, _, _ := loginNewServer(t)
	var calls atomic.Int32
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"status_code":7}`, http.StatusUnauthorized)
	}))
	t.Cleanup(tmdb.Close)
	server.TestSetTMDBBaseURL("bad-key", tmdb.URL)
	for i := 1; i <= 6; i++ {
		if _, err := server.MovieRepo.AddSeries(library.Series{TMDBID: 100 + i, Title: "Show", Monitored: true}, nil); err != nil {
			t.Fatal(err)
		}
	}
	server.TestRefreshAllSeries()
	if calls.Load() == 0 {
		t.Fatal("TMDB was never asked, so the test checks nothing")
	}
	if n := calls.Load(); n > 2 {
		t.Fatalf("TMDB was asked %d times with a rejected key, want it to stop after the first show", n)
	}
}
