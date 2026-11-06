package music

import "testing"

func TestTitleTagConflicts(t *testing.T) {
	for _, c := range []struct {
		tag, track string
		conflict   bool
	}{
		{"Kashmir", "KASHMIR", false},
		{"Don't Stop!", "dont stop", false},
		{"[Explicit] Song for You", "Song for You", false},
		{"Airbag (feat. Somebody)", "Airbag", false},
		{"Airbag - Remastered 2009", "Airbag", false}, // all of the track's words are there
		{"Café del Mar", "Cafe Del Mar", false},
		{"", "Airbag", false}, // untagged never conflicts
		{"Airbag", "", false}, // nothing to compare with
		{"Paranoid Android", "Airbag", true},
		{"Let Down", "Lucky", true},
		{"Exit Music (For a Film)", "Karma Police", true},
		{"No Surprises", "No Surprises (Live)", false}, // the studio title is inside the live one
		{"Subterranean Homesick Alien", "Subterranean Homesick Blues", false},
	} {
		if got := titleTagConflicts(c.tag, c.track); got != c.conflict {
			t.Errorf("%q vs %q: conflict %v, want %v", c.tag, c.track, got, c.conflict)
		}
	}
}

// A file numbered for track 2 whose length fits but whose title tag names
// another song is not taken for track 2; a file named only by its number is.
func TestMatchTracksTitleTagVeto(t *testing.T) {
	slots := []TrackSlot{{Disc: 1, Position: 1, Title: "Airbag", LengthMs: 284000}, {Disc: 1, Position: 2, Title: "Paranoid Android", LengthMs: 383000}}
	files := []FileInfo{
		{Path: "01.flac", Track: 1, Title: "Airbag", TitleTagged: true, DurationSec: 284},
		{Path: "02.flac", Track: 2, Title: "Lucky (Live in Paris)", TitleTagged: true, DurationSec: 380},
	}
	m, un := MatchTracks(files, slots)
	if len(m) != 1 || m[0].File != 0 || len(un) != 1 || un[0] != 1 {
		t.Fatalf("matches %v, unmatched %v: the live Lucky must not stand in for Paranoid Android", m, un)
	}
	files[1].TitleTagged = false // a title from the file name ("02") is no tag
	files[1].Title = "02"
	if m, _ := MatchTracks(files, slots); len(m) != 2 {
		t.Fatalf("an untagged file numbered 2 of the right length: %v", m)
	}
}
