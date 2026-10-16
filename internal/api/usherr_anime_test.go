package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/metadata"
)

func TestIsAnimeShow(t *testing.T) {
	cases := []struct {
		name string
		sh   metadata.Show
		want bool
	}{
		{"Japanese animation", metadata.Show{GenreIDs: []int{16, 10759}, OriginCountry: []string{"JP"}}, true},
		{"in Japanese", metadata.Show{Genres: []metadata.Genre{{ID: 16}}, OriginalLanguage: "ja"}, true},
		{"American cartoon", metadata.Show{GenreIDs: []int{16}, OriginCountry: []string{"US"}, OriginalLanguage: "en"}, false},
		{"Japanese drama", metadata.Show{GenreIDs: []int{18}, OriginCountry: []string{"JP"}, OriginalLanguage: "ja"}, false},
	}
	for _, c := range cases {
		if got := isAnimeShow(c.sh); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
