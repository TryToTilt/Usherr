package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
)

var (
	shamelessUS = library.Series{ID: 2, TMDBID: 34307, Title: "Shameless", Year: 2011, OriginCountry: []string{"US"}}
	shamelessUK = library.Series{ID: 1, TMDBID: 2161, Title: "Shameless", Year: 2004, OriginCountry: []string{"GB"}}
	officeUS    = library.Series{ID: 3, TMDBID: 2316, Title: "The Office", Year: 2005, OriginCountry: []string{"US"}}
	officeUK    = library.Series{ID: 4, TMDBID: 2996, Title: "The Office", Year: 2001, OriginCountry: []string{"GB"}}
	// Shows from before the country was stored: nil, not read yet.
	officeNoCountry = library.Series{ID: 5, TMDBID: 2316, Title: "The Office", Year: 2005}
)

func TestMatchesShowCountryMatrix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		release string
		series  library.Series
		want    bool
	}{
		// Tagged releases: the tag has to be the show's own country.
		{"US tag, US show", "Shameless.US.S01E01.720p.HDTV.x264-GRP", shamelessUS, true},
		{"US tag, UK show", "Shameless.US.S01E01.720p.HDTV.x264-GRP", shamelessUK, false},
		{"UK tag, UK show", "Shameless.UK.S02E03.720p.HDTV.x264-GRP", shamelessUK, true},
		{"UK tag, US show", "Shameless.UK.S02E03.720p.HDTV.x264-GRP", shamelessUS, false},
		{"GB tag is UK", "Shameless.GB.S02E03.720p.HDTV.x264-GRP", shamelessUK, true},
		{"USA tag is US", "The.Office.USA.S05E02.1080p.WEB.h264-GRP", officeUS, true},
		{"USA tag, UK show", "The.Office.USA.S05E02.1080p.WEB.h264-GRP", officeUK, false},
		{"brackets and spaces", "Shameless (UK) S02E03 1080p WEB-DL", shamelessUK, true},
		{"brackets, wrong show", "Shameless (UK) S02E03 1080p WEB-DL", shamelessUS, false},
		{"office UK year in name", "The.Office.UK.2001.S01E01.720p-GRP", officeUK, true},
		{"office US tag, UK show", "The.Office.US.S05E02.1080p.WEB.h264-GRP", officeUK, false},
		// A show made in two countries takes either tag.
		{"co-production, US tag", "Show.US.S01E01.720p-GRP", library.Series{Title: "Show", OriginCountry: []string{"CA", "US"}}, true},
		{"co-production, CA tag", "Show.CA.S01E01.720p-GRP", library.Series{Title: "Show", OriginCountry: []string{"CA", "US"}}, true},
		{"co-production, UK tag", "Show.UK.S01E01.720p-GRP", library.Series{Title: "Show", OriginCountry: []string{"CA", "US"}}, false},
		// Untagged releases still match by title for every show.
		{"untagged, US show", "Shameless.S01E01.720p.HDTV.x264-GRP", shamelessUS, true},
		{"untagged, UK show", "Shameless.S01E01.720p.HDTV.x264-GRP", shamelessUK, true},
		// Country not read yet: the older rule (US always, others with the year).
		{"unknown country, US tag", "The.Office.US.S05E02.1080p.WEB.h264-GRP", officeNoCountry, true},
		{"unknown country, UK tag, no year", "The.Office.UK.S01E01.720p-GRP", officeNoCountry, false},
		{"unknown country, UK tag, show's year", "The.Office.UK.2005.S01E01.720p-GRP", officeNoCountry, true},
		// A year that is not the show's still separates remakes of one name.
		{"year apart", "Doctor.Who.2005.S01E01.720p-GRP", library.Series{Title: "Doctor Who", Year: 1963, OriginCountry: []string{"GB"}}, false},
		{"year together", "Doctor.Who.2005.S01E01.720p-GRP", library.Series{Title: "Doctor Who", Year: 2005, OriginCountry: []string{"GB"}}, true},
		// A title that only ends in something that looks like a code.
		{"title ending in a code word", "Stephen.Kings.It.S01E01.720p-GRP", library.Series{Title: "Stephen King's It", OriginCountry: []string{"US"}}, true},
		{"other title with a tag-like end", "Cleaning.Up.IT.S01E01.720p-GRP", shamelessUS, false},
	} {
		if got := matchesShow(tc.release, tc.series); got != tc.want {
			t.Errorf("%s: matchesShow(%q, %q %v) = %v, want %v", tc.name, tc.release, tc.series.Title, tc.series.OriginCountry, got, tc.want)
		}
	}
}

func TestShowsForReleaseSameName(t *testing.T) {
	ids := func(in []library.Series) []int64 {
		out := make([]int64, 0, len(in))
		for _, s := range in {
			out = append(out, s.ID)
		}
		return out
	}
	other := library.Series{ID: 9, TMDBID: 99, Title: "Another Show", OriginCountry: []string{"US"}}
	// Second copy of the American show: same TMDB id, own row.
	copyUS := shamelessUS
	copyUS.ID, copyUS.CopyOf = 10, 2
	// No American one: the one added first wins.
	topGearUK := library.Series{ID: 20, TMDBID: 1, Title: "Top Gear", OriginCountry: []string{"GB"}}
	topGearAU := library.Series{ID: 21, TMDBID: 2, Title: "Top Gear", OriginCountry: []string{"AU"}}
	unknownA := library.Series{ID: 30, TMDBID: 3, Title: "Misfits"}
	unknownB := library.Series{ID: 31, TMDBID: 4, Title: "Misfits"}

	for _, tc := range []struct {
		name    string
		release string
		library []library.Series
		want    []int64
	}{
		{"untagged goes to the US show", "Shameless.S02E03.720p.HDTV.x264-GRP", []library.Series{shamelessUK, shamelessUS, other}, []int64{2}},
		{"untagged, US added first", "Shameless.S02E03.720p.HDTV.x264-GRP", []library.Series{shamelessUS, shamelessUK}, []int64{2}},
		{"US tag goes to the US show", "Shameless.US.S02E03.720p.HDTV.x264-GRP", []library.Series{shamelessUK, shamelessUS}, []int64{2}},
		{"UK tag goes to the UK show", "Shameless.UK.S02E03.720p.HDTV.x264-GRP", []library.Series{shamelessUS, shamelessUK}, []int64{1}},
		{"office untagged", "The.Office.S03E04.1080p.WEB.h264-GRP", []library.Series{officeUK, officeUS}, []int64{3}},
		{"office UK tag", "The.Office.UK.S01E01.1080p.WEB.h264-GRP", []library.Series{officeUK, officeUS}, []int64{4}},
		{"no American show: first added", "Top.Gear.S10E01.720p-GRP", []library.Series{topGearAU, topGearUK}, []int64{20}},
		{"countries not read: first added", "Misfits.S01E01.720p-GRP", []library.Series{unknownB, unknownA}, []int64{30}},
		{"only one show of the name", "Shameless.S02E03.720p.HDTV.x264-GRP", []library.Series{shamelessUK, other}, []int64{1}},
		{"a copy of the same show is not a second show", "Shameless.S02E03.720p.HDTV.x264-GRP", []library.Series{shamelessUS, copyUS}, []int64{2, 10}},
		{"nothing matches", "Unrelated.S01E01.720p-GRP", []library.Series{shamelessUS, shamelessUK}, []int64{}},
	} {
		got := ids(showsForRelease(tc.release, 1, tc.library))
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
				break
			}
		}
	}
}

func TestBestTVMatchCountry(t *testing.T) {
	tmdbUS := metadata.Show{TMDBID: 34307, Name: "Shameless", FirstAirDate: "2011-01-09", OriginCountry: []string{"US"}}
	tmdbUK := metadata.Show{TMDBID: 2161, Name: "Shameless", FirstAirDate: "2004-01-13", OriginCountry: []string{"GB"}}
	for _, tc := range []struct {
		name    string
		release string
		shows   []metadata.Show
		want    int
	}{
		{"UK tag picks the UK show", "Shameless.UK.S02E03.720p-GRP", []metadata.Show{tmdbUS, tmdbUK}, 2161},
		{"US tag picks the US show", "Shameless.US.S02E03.720p-GRP", []metadata.Show{tmdbUK, tmdbUS}, 34307},
		{"untagged prefers the US show", "Shameless.S02E03.720p-GRP", []metadata.Show{tmdbUK, tmdbUS}, 34307},
		{"untagged, only the UK show", "Shameless.S02E03.720p-GRP", []metadata.Show{tmdbUK}, 2161},
		{"UK tag and only the US show", "Shameless.UK.S02E03.720p-GRP", []metadata.Show{tmdbUS}, 0},
	} {
		got := bestTVMatch(tc.shows, parser.Parse(tc.release))
		id := 0
		if got != nil {
			id = got.TMDBID
		}
		if id != tc.want {
			t.Errorf("%s: picked %d, want %d", tc.name, id, tc.want)
		}
	}
}
