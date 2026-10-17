package api_test

import (
	"io"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library/librarytest"
)

// TestScaleAPI times the pages a big library makes slow, through the API, and
// how big their answers are. It only runs with MEDIARIUM_SCALE set (see
// librarytest.BigSize).
func TestScaleAPI(t *testing.T) {
	librarytest.NeedScale(t)
	db, base, client := newServerWithDB(t)
	sz := librarytest.BigSize()
	if err := librarytest.Fill(db, sz); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d movies, %d shows, %d episodes", sz.Movies, sz.Shows, sz.Shows*sz.EpisodesPerShow)
	for _, path := range []string{
		"/api/dashboard", "/api/dashboard",
		"/api/wanted?kind=missing",
		"/api/wanted?kind=missing&group=1&limit=50", "/api/wanted?kind=missing&group=1&limit=0",
		"/api/wanted?kind=missing&series=1",
		"/api/series?brief=1", "/api/series?brief=1",
		"/api/library/counts",
	} {
		start := time.Now()
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		n, _ := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		t.Logf("%-45s %d %9d bytes in %s", path, resp.StatusCode, n, time.Since(start).Round(time.Millisecond))
	}
}
