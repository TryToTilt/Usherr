package api

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

var lotr = library.Movie{
	Title: "The Lord of the Rings: The Fellowship of the Ring", Year: 2001,
	AltTitles: []string{"Der Herr der Ringe: Die Gefährten", "Der Herr der Ringe"},
}

func TestMatchesMovieOtherTitles(t *testing.T) {
	tests := []struct {
		name    string
		release string
		movie   library.Movie
		want    bool
	}{
		{"the English title still matches", "The.Lord.of.the.Rings.The.Fellowship.of.the.Ring.2001.1080p.BluRay.x264-GRP", lotr, true},
		{"a German-titled release matches", "Der.Herr.der.Ringe.Die.Gefaehrten.2001.German.DL.1080p.BluRay.x264-GRP", library.Movie{Title: lotr.Title, Year: 2001, AltTitles: []string{"Der Herr der Ringe: Die Gefaehrten"}}, true},
		{"the title with its umlaut matches the spelled-out release", "Der.Herr.der.Ringe.Die.Gefaehrten.2001.German.1080p.BluRay.x264-GRP", lotr, true},
		{"the shorter other title matches", "Der.Herr.der.Ringe.2001.German.1080p.BluRay.x264-GRP", lotr, true},
		{"punctuation and case do not matter", "der herr der ringe - 2001 - GERMAN 1080p BluRay x264-GRP", lotr, true},
		{"the year rule applies to other titles too", "Der.Herr.der.Ringe.2002.German.1080p.BluRay.x264-GRP", lotr, false},
		{"a release without a year does not match an other title of a movie that has a year", "Der.Herr.der.Ringe.1080p.BluRay.x264-GRP", lotr, false},
		{"a release without a year still matches the main title", "The.Lord.of.the.Rings.The.Fellowship.of.the.Ring.1080p.BluRay.x264-GRP", lotr, true},
		{"a release without a year matches an other title of a movie with no year", "Der.Herr.der.Ringe.1080p.BluRay.x264-GRP", library.Movie{Title: lotr.Title, AltTitles: lotr.AltTitles}, true},
		{"another film with the same short other title and no year is not taken", "Hitze.1080p.BluRay.x264-GRP", library.Movie{Title: "Heat Wave", Year: 2001, AltTitles: []string{"Hitze"}}, false},
		{"the same short other title with the right year is", "Hitze.2001.1080p.BluRay.x264-GRP", library.Movie{Title: "Heat Wave", Year: 2001, AltTitles: []string{"Hitze"}}, true},
		{"an other title of three letters never matches", "Eis.2001.1080p.BluRay.x264-GRP", library.Movie{Title: "The Ice Storm", Year: 2001, AltTitles: []string{"Eis"}}, false},
		{"an unrelated title does not", "Der.Pate.1972.German.1080p.BluRay.x264-GRP", lotr, false},
		{"a longer title that only starts with an other title does not", "Der.Herr.der.Ringe.Die.Zwei.Tuerme.2001.German.1080p.BluRay.x264-GRP", lotr, false},
		{"without other titles a German release does not match", "Der.Herr.der.Ringe.2001.German.1080p.BluRay.x264-GRP", library.Movie{Title: lotr.Title, Year: 2001}, false},
		{"a movie with that title as its English one is not confused", "Der.Herr.der.Ringe.2001.German.1080p.BluRay.x264-GRP", library.Movie{Title: "Der Herr der Ringe Parody", Year: 2001, AltTitles: []string{"Parodie"}}, false},
		{"another movie's other title does not match this movie", "Das.Boot.1981.German.1080p.BluRay.x264-GRP", lotr, false},
		{"a title too short to tell apart never matches", "Es.2001.German.1080p.BluRay.x264-GRP", library.Movie{Title: "It", Year: 2001, AltTitles: []string{"Es"}}, false},
		{"a Russian title matches in its own alphabet", "Матрица.1999.1080p.BluRay.x264-GRP", library.Movie{Title: "The Matrix", Year: 1999, AltTitles: []string{"Матрица"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesMovie(tt.release, tt.movie); got != tt.want {
				t.Errorf("matchesMovie(%q) = %v, want %v", tt.release, got, tt.want)
			}
		})
	}
}

// A title that is another movie's English title is not confused: the other
// titles of the movie being searched are the only ones tried.
func TestOtherTitlesBelongToOneMovie(t *testing.T) {
	godfather := library.Movie{Title: "The Godfather", Year: 1972, AltTitles: []string{"Der Pate"}}
	part2 := library.Movie{Title: "The Godfather Part II", Year: 1974, AltTitles: []string{"Der Pate 2"}}
	// A film whose English title is the same as the first one's German title.
	namesake := library.Movie{Title: "Der Pate", Year: 2005}
	release := "Der.Pate.1972.German.1080p.BluRay.x264-GRP"

	if !matchesMovie(release, godfather) {
		t.Error("the film should take a release named by its own other title")
	}
	if matchesMovie(release, part2) {
		t.Error("another film must not take a release named by this film's other title")
	}
	if matchesMovie(release, namesake) {
		t.Error("a film with that English title but another year must not take the release")
	}
	if matchesMovie("Der.Pate.2.1974.German.1080p.BluRay.x264-GRP", godfather) {
		t.Error("the sequel's release must not match the first film")
	}
	if !matchesMovie("Der.Pate.2.1974.German.1080p.BluRay.x264-GRP", part2) {
		t.Error("the sequel should take its own release")
	}
}

func TestMatchesShowOtherTitles(t *testing.T) {
	haus := library.Series{Title: "Money Heist", Year: 2017, AltTitles: []string{"Haus des Geldes"}}
	tests := []struct {
		name    string
		release string
		series  library.Series
		want    bool
	}{
		{"English title", "Money.Heist.S01E01.1080p.WEB-DL.x264-GRP", haus, true},
		{"German title", "Haus.des.Geldes.S01E01.German.1080p.WEB-DL.x264-GRP", haus, true},
		{"German title with a year", "Haus.des.Geldes.2017.S01E01.German.1080p.WEB-DL.x264-GRP", haus, true},
		{"German title with another year", "Haus.des.Geldes.2012.S01E01.German.1080p.WEB-DL.x264-GRP", haus, false},
		{"a three-letter other title is never taken", "Eis.S01E01.German.1080p.WEB-DL.x264-GRP", library.Series{Title: "Ice", Year: 2017, AltTitles: []string{"Eis"}}, false},
		{"another show", "Haus.des.Drachen.S01E01.German.1080p.WEB-DL.x264-GRP", haus, false},
		{"without other titles", "Haus.des.Geldes.S01E01.German.1080p.WEB-DL.x264-GRP", library.Series{Title: "Money Heist", Year: 2017}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesShow(tt.release, tt.series); got != tt.want {
				t.Errorf("matchesShow(%q) = %v, want %v", tt.release, got, tt.want)
			}
		})
	}
	// A special named by the show's other title is taken for the show too.
	if !matchesShowFor("Haus.des.Geldes.Die.Weihnachtsfolge.720p.WEB-DL.x264-GRP", haus, 0) {
		t.Error("a special named by an other title should match")
	}
	if matchesShowFor("Haus.des.Geldes.Die.Weihnachtsfolge.720p.WEB-DL.x264-GRP", haus, 1) {
		t.Error("a release with no episode marker is only taken for a special")
	}
}

func TestMatchedOtherTitle(t *testing.T) {
	tests := []struct {
		release string
		want    string
	}{
		{"Der.Herr.der.Ringe.2001.German.1080p.BluRay.x264-GRP", "Der Herr der Ringe"},
		{"The.Lord.of.the.Rings.The.Fellowship.of.the.Ring.2001.1080p.BluRay.x264-GRP", ""},
		{"Der.Pate.1972.German.1080p.BluRay.x264-GRP", ""},
	}
	for _, tt := range tests {
		if got := matchedOtherTitle(tt.release, lotr.Title, lotr.AltTitles); got != tt.want {
			t.Errorf("matchedOtherTitle(%q) = %q, want %q", tt.release, got, tt.want)
		}
	}
	if got := matchedOtherTitle("Der.Herr.der.Ringe.2001.German.x264-GRP", lotr.Title, nil); got != "" {
		t.Errorf("a movie with no other titles matched on %q", got)
	}
}

func TestNameLanguageCodes(t *testing.T) {
	tests := []struct {
		list     string
		original string
		want     []string
	}{
		{"English", "", nil},
		{"", "", nil},
		{"German", "", []string{"de"}},
		{"German,English", "", []string{"de"}},
		{"English,Italian,German", "", []string{"it", "de"}},
		{"Original", "ja", []string{"ja"}},
		{"Original", "en", nil},
		{"Original", "", nil},
		{"German,Original", "de", []string{"de"}},
		{"Chinese", "", []string{"cn", "zh"}},
		{"Klingon", "", nil},
	}
	for _, tt := range tests {
		if got := nameLanguageCodes(tt.list, tt.original); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("nameLanguageCodes(%q, %q) = %v, want %v", tt.list, tt.original, got, tt.want)
		}
	}
	for list, want := range map[string]bool{"English": false, "": false, "english, English": false, "German": true, "English,Original": true, "English,Klingon": true} {
		if got := wantsOtherLanguage(list); got != want {
			t.Errorf("wantsOtherLanguage(%q) = %v, want %v", list, got, want)
		}
	}
}

func TestAltTVQueriesAreCapped(t *testing.T) {
	sr := library.Series{Title: "Money Heist", AltTitles: []string{"Haus des Geldes", "La casa de papel", "La Casa de Papel 3"}}
	got := altTVQueries(sr, 1, 2)
	want := []string{"Haus des Geldes S01E02", "La casa de papel S01E02"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("altTVQueries = %v, want %v", got, want)
	}
	if got := altTVQueries(sr, 2, 0); got[0] != "Haus des Geldes S02" {
		t.Errorf("a season search is %q", got[0])
	}
	if got := altTVQueries(library.Series{Title: "X"}, 1, 1); len(got) != 0 {
		t.Errorf("no other titles should give no extra searches, got %v", got)
	}
}
