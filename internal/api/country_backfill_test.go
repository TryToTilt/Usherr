package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// Shows added before the country was stored get it from TMDB in the
// background, a few at a time; a show TMDB no longer knows ends up with no
// country and is not asked about again.
func TestOriginCountryBackfill(t *testing.T) {
	server, _, _ := loginNewServer(t)
	var calls atomic.Int32
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		countries := map[string][]string{"/tv/34307": {"US"}, "/tv/2161": {"GB"}}
		c, ok := countries[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "Shameless", "origin_country": c, "seasons": []any{}})
	}))
	t.Cleanup(tmdb.Close)
	server.TestSetTMDBBaseURL("k", tmdb.URL)

	add := func(tmdbID int, year int) library.Series {
		sr, err := server.MovieRepo.AddSeries(library.Series{TMDBID: tmdbID, Title: "Shameless", Year: year, Monitored: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return sr
	}
	us, uk, gone := add(34307, 2011), add(2161, 2004), add(404, 1999)

	server.TestBackfillCountries(context.Background())

	for _, tc := range []struct {
		id   int64
		want []string
	}{{us.ID, []string{"US"}}, {uk.ID, []string{"GB"}}, {gone.ID, []string{}}} {
		got, err := server.MovieRepo.GetSeries(tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if got.OriginCountry == nil || !reflect.DeepEqual(got.OriginCountry, tc.want) {
			t.Errorf("show %d: country %#v, want %#v", tc.id, got.OriginCountry, tc.want)
		}
	}

	// A second copy of a show follows the main show's countries.
	cp, err := server.MovieRepo.AddSeriesCopy(us.ID, firstProfileID(t, server))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cp.OriginCountry, []string{"US"}) {
		t.Errorf("copy country = %#v", cp.OriginCountry)
	}

	n := calls.Load()
	server.TestBackfillCountries(context.Background())
	if calls.Load() != n {
		t.Errorf("TMDB was asked again for shows that were already filled in (%d more requests)", calls.Load()-n)
	}
}
