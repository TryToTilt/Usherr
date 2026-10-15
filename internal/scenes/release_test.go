package scenes

import (
	"reflect"
	"testing"
)

func TestParseRelease(t *testing.T) {
	cases := []struct {
		name   string
		studio string
		date   string
		words  []string
		height int
	}{
		{"AcmeStudios.24.03.15.Jane.Doe.And.John.Smith.XXX.1080p.MP4-WRB", "acmestudios", "2024-03-15", []string{"jane", "doe", "and", "john", "smith"}, 1080},
		{"Northlight.2023.11.02.Some.Title.Here.XXX.2160p.MP4-P2P", "northlight", "2023-11-02", []string{"some", "title", "here"}, 2160},
		{"TeamSkeet - 2022-05-01 - A Title", "teamskeet", "2022-05-01", []string{"a", "title"}, 0},
		{"Tushy.21.12.24.Name.Surname.XXX.4K.MP4-GROUP", "tushy", "2021-12-24", []string{"name", "surname"}, 2160},
	}
	for _, c := range cases {
		r := ParseRelease(c.name)
		if r.Studio != c.studio || r.Date.Format("2006-01-02") != c.date || r.Height != c.height || !reflect.DeepEqual(r.Words, c.words) {
			t.Errorf("%s:\n got studio=%q date=%s words=%v height=%d\nwant studio=%q date=%s words=%v height=%d",
				c.name, r.Studio, r.Date.Format("2006-01-02"), r.Words, r.Height, c.studio, c.date, c.words, c.height)
		}
	}
	if r := ParseRelease("No.Date.Here.XXX.720p"); !r.Date.IsZero() {
		t.Errorf("a name without a date must have no date, got %v", r.Date)
	}
}

func scene(id, studio, date, title string, performers ...string) Scene {
	s := Scene{StashID: id, StudioName: studio, ReleaseDate: date, Title: title}
	for _, p := range performers {
		s.Performers = append(s.Performers, Credit{Name: p})
	}
	return s
}

func TestMatchesStudioDateAndPerformer(t *testing.T) {
	a := scene("a", "Acme Studios", "2024-03-15", "Office Hours", "Jane Doe", "John Smith")
	b := scene("b", "Acme Studios", "2024-03-15", "Pool Party", "Mary Major")
	r := ParseRelease("AcmeStudios.24.03.15.Jane.Doe.And.John.Smith.XXX.1080p.MP4-WRB")
	if !Matches(r, a, []Scene{a, b}) {
		t.Fatal("the release names a's performers and should match a")
	}
	if Matches(r, b, []Scene{a, b}) {
		t.Fatal("the release must not be taken for the other scene that day")
	}
}

func TestOnlyStudioAndDateIsAmbiguousWithSiblings(t *testing.T) {
	a := scene("a", "Northlight", "2023-11-02", "")
	b := scene("b", "Northlight", "2023-11-02", "")
	r := ParseRelease("Northlight.23.11.02.XXX.1080p.MP4-X")
	if Matches(r, a, []Scene{a, b}) {
		t.Fatal("two scenes that day and nothing else to tell them apart: no match")
	}
	if !Matches(r, a, []Scene{a}) {
		t.Fatal("the only scene that day should match on studio and date")
	}
}

func TestMatchRules(t *testing.T) {
	s := scene("a", "Tushy", "2021-12-24", "Holiday", "Name Surname")
	cases := map[string]bool{
		"Tushy.21.12.24.Name.Surname.XXX.1080p.MP4-G":   true,
		"Tushy.21.12.25.Name.Surname.XXX.1080p.MP4-G":   true,  // a day later: still fine
		"Tushy.21.12.27.Name.Surname.XXX.1080p.MP4-G":   false, // too far off
		"Blacked.21.12.24.Name.Surname.XXX.1080p.MP4-G": false,
		"Name.Surname.Tushy.XXX.1080p":                  false, // no date
	}
	for name, want := range cases {
		if got := MatchScore(ParseRelease(name), s) > 0; got != want {
			t.Errorf("%s: match=%v, want %v", name, got, want)
		}
	}
}

func TestStudioAliasesAndShortNames(t *testing.T) {
	s := Scene{StudioName: "Bang Bros", StudioAliases: []string{"BangBros Network"}, ReleaseDate: "2020-01-02"}
	if MatchScore(ParseRelease("BangBros.20.01.02.XXX.720p"), s) == 0 {
		t.Error("spacing differences in the studio name must still match")
	}
	short := Scene{StudioName: "Abc", ReleaseDate: "2020-01-02"}
	if MatchScore(ParseRelease("AbcdefGhi.20.01.02.XXX.720p"), short) != 0 {
		t.Error("a short studio name must not match a longer one by prefix")
	}
}

func TestSearchQueriesAndNames(t *testing.T) {
	s := scene("a", "Acme Studios", "2024-03-15", "Office Hours", "Jane Doe", "John Smith")
	q := SearchQueries(s)
	if len(q) != 2 || q[0] != "acmestudios 24 03 15" || q[1] != "acmestudios Jane Doe" {
		t.Fatalf("queries = %v", q)
	}
	if got := FileName(s); got != "Acme Studios - 2024-03-15 - Office Hours [Jane Doe, John Smith]" {
		t.Fatalf("file name = %q", got)
	}
	if Folder(Scene{}) != "Unknown studio" {
		t.Fatal("a scene without a studio needs a folder too")
	}
	if QualityRank(ParseRelease("X.24.01.01.XXX.2160p")) <= QualityRank(ParseRelease("X.24.01.01.XXX.1080p")) {
		t.Fatal("2160p must rank above 1080p")
	}
}
