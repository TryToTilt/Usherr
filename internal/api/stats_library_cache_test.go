package api_test

import "testing"

// The Statistics page is built from a pass over every file of the library,
// so a second request within a couple of minutes gets the same answer
// without another pass.
func TestLibraryStatsAreKeptForAFewMinutes(t *testing.T) {
	server, base, client := loginNewServer(t)
	seedMovie(t, server.MovieRepo, 949, "Heat", 1995, "")
	type stats struct {
		Movies int `json:"movies"`
	}
	if got := getJSON[stats](t, client, base+"/api/stats/library"); got.Movies != 1 {
		t.Fatalf("first answer: %+v", got)
	}
	seedMovie(t, server.MovieRepo, 950, "Ronin", 1998, "")
	if got := getJSON[stats](t, client, base+"/api/stats/library"); got.Movies != 1 {
		t.Fatalf("the second request within the cache time counted again: %+v", got)
	}
}
