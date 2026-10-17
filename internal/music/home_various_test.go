package music

import "testing"

func TestIsVariousArtists(t *testing.T) {
	for _, n := range []string{"Various Artists", "VA", "various"} {
		if !IsVariousArtists(n) {
			t.Errorf("%q not recognised", n)
		}
	}
	if IsVariousArtists("The Guess Who") {
		t.Error("a real artist counted as compilations")
	}
}
