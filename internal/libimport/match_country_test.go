package libimport_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/libimport"
)

func TestDecideWithCountryTag(t *testing.T) {
	us := libimport.Candidate{TMDBID: 34307, Title: "Shameless", Year: 2011, Countries: []string{"US"}}
	uk := libimport.Candidate{TMDBID: 2161, Title: "Shameless", Year: 2004, Countries: []string{"GB"}}
	noCountry := libimport.Candidate{TMDBID: 5, Title: "Shameless", Year: 2004}
	for _, tc := range []struct {
		name   string
		folder string
		year   int
		in     []libimport.Candidate
		status libimport.MatchStatus
		first  int
	}{
		{"UK folder picks the UK show", "Shameless (UK)", 0, []libimport.Candidate{us, uk}, libimport.MatchMatched, 2161},
		{"US folder picks the US show", "Shameless US", 0, []libimport.Candidate{uk, us}, libimport.MatchMatched, 34307},
		{"GB is the same as UK", "Shameless.GB", 0, []libimport.Candidate{us, uk}, libimport.MatchMatched, 2161},
		{"tag and no show from there", "Shameless (AU)", 0, []libimport.Candidate{us, uk}, libimport.MatchAmbiguous, 34307},
		{"countries not read: the name decides", "Shameless (UK)", 0, []libimport.Candidate{noCountry}, libimport.MatchMatched, 5},
		{"no tag, two shows: the person picks", "Shameless", 0, []libimport.Candidate{us, uk}, libimport.MatchAmbiguous, 34307},
		{"no tag, the year decides", "Shameless", 2004, []libimport.Candidate{us, uk}, libimport.MatchMatched, 2161},
		{"tag and year agree", "Shameless (UK)", 2004, []libimport.Candidate{us, uk}, libimport.MatchMatched, 2161},
	} {
		status, got := libimport.Decide(tc.folder, tc.year, tc.in)
		if status != tc.status || len(got) == 0 || got[0].TMDBID != tc.first {
			first := 0
			if len(got) > 0 {
				first = got[0].TMDBID
			}
			t.Errorf("%s: Decide(%q, %d) = %s first %d, want %s first %d", tc.name, tc.folder, tc.year, status, first, tc.status, tc.first)
		}
	}
}
