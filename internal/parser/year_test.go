package parser_test

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/parser"
)

// TestParseYearEdgeCases pins how a year-like number is read: the last
// plausible year is the release year, and a number that is part of a title
// ("Blade Runner 2049", "2012") stays in the title.
func TestParseYearEdgeCases(t *testing.T) {
	cases := []struct {
		in       string
		title    string
		year     int
		season   int
		episodes []int
		late     bool // the year came after the episode marker
	}{
		// Movies: a year-like number that is part of the title.
		{"Blade.Runner.2049.2017.1080p.BluRay.x264-GRP", "Blade Runner 2049", 2017, 0, nil, false},
		{"Blade Runner 2049 2017 1080p BluRay x265", "Blade Runner 2049", 2017, 0, nil, false},
		{"Blade.Runner.2049.(2017).2160p.UHD.BluRay", "Blade Runner 2049", 2017, 0, nil, false},
		{"Blade Runner 2049 (2017)", "Blade Runner 2049", 2017, 0, nil, false},
		{"Blade.Runner.2049.1080p.BluRay.x264-GRP", "Blade Runner 2049", 0, 0, nil, false},
		{"Blade Runner 2049 720p", "Blade Runner 2049", 0, 0, nil, false},
		{"Blade.Runner.2049.REMUX.2160p", "Blade Runner 2049", 0, 0, nil, false},
		{"2012 (2009)", "2012", 2009, 0, nil, false},
		{"2012.2009.1080p.BluRay.x264-GRP", "2012", 2009, 0, nil, false},
		{"2012.1080p.BluRay.x264-GRP", "2012", 0, 0, nil, false},
		{"2012 1080p BluRay", "2012", 0, 0, nil, false},
		{"1917.2019.1080p.BluRay.x264-GRP", "1917", 2019, 0, nil, false},
		{"1917 2019", "1917", 2019, 0, nil, false},
		{"1917 (2019) 720p", "1917", 2019, 0, nil, false},
		{"1917.1080p.BluRay.x264-GRP", "1917", 0, 0, nil, false},
		{"1917 1080p BluRay", "1917", 0, 0, nil, false},
		{"300.2006.1080p.BluRay.x264-GRP", "300", 2006, 0, nil, false},
		{"300 2006", "300", 2006, 0, nil, false},
		{"Wonder.Woman.1984.2020.1080p.WEB-DL.DDP5.1.x264-GRP", "Wonder Woman 1984", 2020, 0, nil, false},
		{"Wonder Woman 1984 2020 2160p", "Wonder Woman 1984", 2020, 0, nil, false},
		{"Wonder.Woman.1984.(2020).1080p", "Wonder Woman 1984", 2020, 0, nil, false},
		// Ambiguous without a lookup; the matching code tries "title + year".
		{"Wonder.Woman.1984.1080p.BluRay", "Wonder Woman", 1984, 0, nil, false},
		{"Cars.2.2011.720p.BluRay.x264-GRP", "Cars 2", 2011, 0, nil, false},
		{"Cars 2 2011 1080p", "Cars 2", 2011, 0, nil, false},
		{"1984.1984.720p.BluRay", "1984", 1984, 0, nil, false},
		{"1984.1080p.BluRay", "1984", 0, 0, nil, false},
		{"2010.1984.1080p.BluRay.x264-GRP", "2010", 1984, 0, nil, false},
		{"2010.The.Year.We.Make.Contact.1984.720p", "2010 The Year We Make Contact", 1984, 0, nil, false},
		{"2001.A.Space.Odyssey.1968.1080p.BluRay", "2001 A Space Odyssey", 1968, 0, nil, false},
		{"2001 A Space Odyssey 1080p BluRay", "2001 A Space Odyssey", 0, 0, nil, false},
		{"2046.2004.1080p.BluRay.x264-GRP", "2046", 2004, 0, nil, false},
		{"Movie.Name.2099.1080p.BluRay", "Movie Name 2099", 0, 0, nil, false},
		{"Apollo.13.1995.1080p.BluRay.x264-GRP", "Apollo 13", 1995, 0, nil, false},
		{"Metropolis.1927.1080p.BluRay", "Metropolis", 1927, 0, nil, false},
		{"Dune.2021.2160p.WEB-DL.DDP5.1.Atmos.HDR.HEVC-GRP", "Dune", 2021, 0, nil, false},

		// Shows: a year in the show's name or before the episode marker.
		{"Show.Name.2019.S01E01.1080p.WEB-DL.x264-GRP", "Show Name", 2019, 1, []int{1}, false},
		{"Show Name 2019 S01E01", "Show Name", 2019, 1, []int{1}, false},
		{"Show.Name.(2019).S01E01.720p", "Show Name", 2019, 1, []int{1}, false},
		{"Show.Name.2019.S01.1080p.WEB-DL", "Show Name", 2019, 1, nil, false},
		{"Show.Name.S01E01.2019.1080p.WEB-DL.x264-GRP", "Show Name", 2019, 1, []int{1}, true},
		{"Show Name S01E01 2019", "Show Name", 2019, 1, []int{1}, true},
		{"Show.Name.S01E05.2015.WEB-DL", "Show Name", 2015, 1, []int{5}, true},
		{"Show.Name.S01.2019.1080p.BluRay", "Show Name", 2019, 1, nil, true},

		// Shows: a year inside the episode's title is not the show's year.
		{"Show.Name.S02E05.2012.Olympics.Special.720p.HDTV.x264-GRP", "Show Name", 0, 2, []int{5}, false},
		{"Show Name S02E05 2012 Olympics Special 720p", "Show Name", 0, 2, []int{5}, false},
		{"Show.Name.S01E05.Christmas.2019.720p.HDTV", "Show Name", 0, 1, []int{5}, false},
		{"Show.Name.S03E10.The.Year.2000.720p.HDTV.x264-GRP", "Show Name", 0, 3, []int{10}, false},
		{"Show.Name.2010.S04E02.Back.in.1999.1080p.WEB-DL", "Show Name", 2010, 4, []int{2}, false},
		{"Show.Name.S01E03.1999.A.Retrospective.WEB-DL.x264-GRP", "Show Name", 0, 1, []int{3}, false},
		{"Show.Name.S02E01E02.2012.Special.HDTV", "Show Name", 0, 2, []int{1, 2}, false},
		{"Show.Name.1x05.2012.Olympics.720p", "Show Name", 0, 1, []int{5}, false},
		{"Show.Name.S10E22.Remembering.2001.1080p.AMZN.WEB-DL", "Show Name", 0, 10, []int{22}, false},

		// Shows whose name is a number.
		{"1923.S01E01.1080p.WEB-DL", "1923", 0, 1, []int{1}, false},
		{"Season 1 Show Name 2019 1080p", "Show Name", 2019, 1, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := parser.Parse(tc.in)
			if got.Title != tc.title || got.Year != tc.year || got.Season != tc.season || got.YearLate != tc.late {
				t.Errorf("Parse(%q) = title %q year %d season %d late %v; want %q %d %d %v",
					tc.in, got.Title, got.Year, got.Season, got.YearLate, tc.title, tc.year, tc.season, tc.late)
			}
			if len(tc.episodes) > 0 && !reflect.DeepEqual(got.Episodes, tc.episodes) {
				t.Errorf("Parse(%q).Episodes = %v; want %v", tc.in, got.Episodes, tc.episodes)
			}
		})
	}
}
