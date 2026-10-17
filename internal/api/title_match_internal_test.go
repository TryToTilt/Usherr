package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
)

// Release names are mostly plain ASCII, titles from TMDB are not: the accents
// have to fold, and a title in another alphabet has to compare as real text.
func TestNormalizeTitleFoldsAccentsAndKeepsOtherAlphabets(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{"Pokémon", "Pokemon", true},
		{"Amélie", "Amelie", true},
		{"Élite", "Elite", true},
		{"Zoë", "Zoe", true},
		{"Léon: The Professional", "Leon The Professional", true},
		{"Straße", "Strasse", true},
		{"Bjørk", "Bjork", true},
		{"Pokémon", "Pokmon", false},
		{"千と千尋の神隠し", "千と千尋の神隠し", true},
		{"千と千尋の神隠し", "もののけ姫", false},
		{"Игра престолов", "игра.престолов", true},
		{"Игра престолов", "Игра", false},
		{"Ελλάδα", "Ελλαδα", true},
		{"Fast & Furious", "Fast.and.Furious", true},
	}
	for _, tc := range cases {
		if got := sameTitle(tc.a, tc.b); got != tc.same {
			t.Errorf("sameTitle(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.same)
		}
	}
	if normalizeTitle("千と千尋の神隠し") == "" || normalizeTitle("Игра престолов") == "" {
		t.Error("a title in another alphabet normalised to the empty string")
	}
}

// A title with nothing to compare matches nothing, not even another one.
func TestEmptyTitleKeyNeverMatches(t *testing.T) {
	if sameTitle("", "") || sameTitle("!!!", "...") || sameTitle("", "Anything") {
		t.Error("an empty normalised title matched")
	}
	// A name that starts with a tag has no title for the parser.
	movie := library.Movie{Title: "!!!", Year: 2020}
	if matchesMovie("1080p.BluRay.x264-GRP", movie) || matchesMovie("2020.1080p.BluRay.x264-GRP", movie) {
		t.Error("a release with no title matched a movie whose title has no letters")
	}
	show := library.Series{Title: "!!!"}
	if matchesShow("1080p.WEB-DL.x264-GRP", show) {
		t.Error("a release with no title matched a show whose title has no letters")
	}
}

func TestMatchersFindAccentedAndNonLatinTitles(t *testing.T) {
	movies := []struct {
		release, title string
		year           int
		want           bool
	}{
		{"Amelie.2001.1080p.BluRay.x264-GRP", "Amélie", 2001, true},
		{"Leon.The.Professional.1994.1080p.BluRay.x264-GRP", "Léon: The Professional", 1994, true},
		{"Pokemon.The.First.Movie.1998.1080p.BluRay.x264-GRP", "Pokémon: The First Movie", 1998, true},
		{"Amelie.2001.1080p.BluRay.x264-GRP", "Amélie", 2002, false},
		{"Spirited.Away.2001.1080p.BluRay.x264-GRP", "千と千尋の神隠し", 2001, false},
		{"Игра.престолов.2001.1080p.BluRay.x264-GRP", "千と千尋の神隠し", 2001, false},
		{"千と千尋の神隠し.2001.1080p.BluRay.x264-GRP", "千と千尋の神隠し", 2001, true},
	}
	for _, tc := range movies {
		if got := matchesMovie(tc.release, library.Movie{Title: tc.title, Year: tc.year}); got != tc.want {
			t.Errorf("matchesMovie(%q, %q %d) = %v, want %v", tc.release, tc.title, tc.year, got, tc.want)
		}
	}
	shows := []struct {
		release, title string
		want           bool
	}{
		{"Elite.S01E01.1080p.WEB-DL.x264-GRP", "Élite", true},
		{"Pokemon.S01E01.1080p.WEB-DL.x264-GRP", "Pokémon", true},
		{"Zoe.S01E01.1080p.WEB-DL.x264-GRP", "Zoë", true},
		{"Игра.престолов.S01E01.1080p.WEB-DL.x264-GRP", "Игра престолов", true},
		{"Игра.престолов.S01E01.1080p.WEB-DL.x264-GRP", "Бригада", false},
		{"Игра.престолов.S01E01.1080p.WEB-DL.x264-GRP", "Game of Thrones", false},
	}
	for _, tc := range shows {
		if got := matchesShow(tc.release, library.Series{Title: tc.title}); got != tc.want {
			t.Errorf("matchesShow(%q, %q) = %v, want %v", tc.release, tc.title, got, tc.want)
		}
	}
}

// Picking a release from the search bar must not add some other film or show
// just because TMDB listed it first.
func TestUnifiedSearchMatchNeedsTheSameTitle(t *testing.T) {
	films := []metadata.Movie{
		{TMDBID: 1, Title: "Amelie's Revenge", ReleaseDate: "2001-01-01"},
		{TMDBID: 2, Title: "Amélie", ReleaseDate: "2001-04-25"},
	}
	if m := bestTMDBMatch(films, "Amelie", 2001); m == nil || m.TMDBID != 2 {
		t.Errorf("bestTMDBMatch picked %+v, want the film named Amélie", m)
	}
	if m := bestTMDBMatch(films, "Something Else", 2001); m != nil {
		t.Errorf("bestTMDBMatch guessed %+v for a title nobody has", m)
	}
	if m := bestTMDBMatch([]metadata.Movie{{TMDBID: 3, Title: "Spirited Away", OriginalTitle: "千と千尋の神隠し"}}, "千と千尋の神隠し", 0); m == nil {
		t.Error("bestTMDBMatch missed a film named by its original title")
	}
	shows := []metadata.Show{{TMDBID: 1, Name: "Elite Squad"}, {TMDBID: 2, Name: "Élite"}}
	if m := bestTVMatch(shows, parser.Parse("Elite.S01E01.1080p.WEB-DL-GRP")); m == nil || m.TMDBID != 2 {
		t.Errorf("bestTVMatch picked %+v, want Élite", m)
	}
	if m := bestTVMatch(shows, parser.Parse("Unknown.Show.S01E01.1080p.WEB-DL-GRP")); m != nil {
		t.Errorf("bestTVMatch guessed %+v for a show nobody has", m)
	}
}
