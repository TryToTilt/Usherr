package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// A monitored show never gives way to an unmonitored one of the same name.
func TestSameNameWinnerPrefersMonitored(t *testing.T) {
	mon := func(s library.Series, on bool) library.Series { s.Monitored = on; return s }
	for _, tc := range []struct {
		name string
		list []library.Series
		want int // TMDB id of the winner
	}{
		{"American is unmonitored, British is monitored", []library.Series{mon(shamelessUS, false), mon(shamelessUK, true)}, 2161},
		{"both monitored: the American one", []library.Series{mon(shamelessUK, true), mon(shamelessUS, true)}, 34307},
		{"none monitored: the American one", []library.Series{mon(shamelessUK, false), mon(shamelessUS, false)}, 34307},
		{"two monitored, none American: first added", []library.Series{
			mon(library.Series{ID: 7, TMDBID: 70, Title: "Top Gear", OriginCountry: []string{"AU"}}, true),
			mon(library.Series{ID: 6, TMDBID: 60, Title: "Top Gear", OriginCountry: []string{"GB"}}, true),
			mon(library.Series{ID: 5, TMDBID: 50, Title: "Top Gear", OriginCountry: []string{"US"}}, false),
		}, 60},
	} {
		list := append([]library.Series(nil), tc.list...)
		markSameName(list)
		for _, sr := range list {
			if want := sr.TMDBID != tc.want; sr.YieldsUntagged != want {
				t.Errorf("%s: show %d YieldsUntagged=%v, want %v", tc.name, sr.TMDBID, sr.YieldsUntagged, want)
			}
		}
		release := tc.list[0].Title + ".S01E01.720p-GRP"
		got := showsForRelease(release, 1, tc.list)
		if len(got) != 1 || got[0].TMDBID != tc.want {
			t.Errorf("%s: showsForRelease gave %v, want only %d", tc.name, got, tc.want)
		}
	}
}
