package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

// With two shows of one name in the library, the automatic pick for the one
// that gives way ignores releases without a country tag, takes the ones tagged
// for its country, and refuses the ones tagged for another country.
func TestAutomaticPickWithSameNameShows(t *testing.T) {
	_, profile := profileChain(true, quality.Preset720p)
	list := []library.Series{
		{ID: 1, TMDBID: 2161, Title: "Shameless", Year: 2004, OriginCountry: []string{"GB"}}, // added first, but British
		{ID: 2, TMDBID: 34307, Title: "Shameless", Year: 2011, OriginCountry: []string{"US"}},
	}
	markSameName(list)
	uk, us := list[0], list[1]
	if !uk.YieldsUntagged || us.YieldsUntagged {
		t.Fatalf("the UK show should give way to the US one: uk=%v us=%v", uk.YieldsUntagged, us.YieldsUntagged)
	}
	for _, tc := range []struct {
		name    string
		series  library.Series
		release string
		want    bool
	}{
		{"US show, untagged", us, "Shameless.S02E03.720p.WEB-DL.x264-GRP", true},
		{"UK show, untagged", uk, "Shameless.S02E03.720p.WEB-DL.x264-GRP", false},
		{"UK show, UK tag", uk, "Shameless.UK.S02E03.720p.WEB-DL.x264-GRP", true},
		{"UK show, US tag", uk, "Shameless.US.S02E03.720p.WEB-DL.x264-GRP", false},
		{"US show, UK tag", us, "Shameless.UK.S02E03.720p.WEB-DL.x264-GRP", false},
		{"US show, US tag", us, "Shameless.US.S02E03.720p.WEB-DL.x264-GRP", true},
		{"UK show, its own year", uk, "Shameless.2004.S02E03.720p.WEB-DL.x264-GRP", true},
	} {
		got := pickTVResult([]indexers.Result{{Title: tc.release, DownloadURL: "x"}}, tc.series, 2, 3, profile, nil)
		if (got != nil) != tc.want {
			t.Errorf("%s: picked=%v, want %v", tc.name, got != nil, tc.want)
		}
		found := tvTargetFound([]indexers.Result{{Title: tc.release}}, tc.series, 2, 3, nil)
		if found != tc.want {
			t.Errorf("%s: tvTargetFound=%v, want %v", tc.name, found, tc.want)
		}
	}

	// One show of the name: nothing changes for it.
	alone := []library.Series{{ID: 3, TMDBID: 5, Title: "Shameless", OriginCountry: []string{"GB"}}}
	markSameName(alone)
	if alone[0].YieldsUntagged {
		t.Error("a show alone with its name must take releases without a tag")
	}
}
