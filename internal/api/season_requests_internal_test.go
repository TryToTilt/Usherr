package api

import (
	"reflect"
	"testing"
)

// Usherr: asking for some seasons is its own request, apart from the whole show.
func TestSeasonRequestKeys(t *testing.T) {
	if got := cleanSeasons([]int{3, 1, 3, -1, 0}); !reflect.DeepEqual(got, []int{0, 1, 3}) {
		t.Errorf("cleanSeasons: %v", got)
	}
	whole := requestKey("tv", `{"tmdbId":1399}`)
	two := requestKey("tv", `{"tmdbId":1399,"seasons":[2,1]}`)
	same := requestKey("tv", `{"tmdbId":1399,"seasons":[1,2,2]}`)
	if whole == two || two != same || two != "tv:tmdb:1399:s1,2" {
		t.Errorf("keys: %q %q %q", whole, two, same)
	}
	if got := payloadSeasons([]byte(`{"seasons":[4,4,2]}`)); !reflect.DeepEqual(got, []int{2, 4}) {
		t.Errorf("payloadSeasons: %v", got)
	}
}
