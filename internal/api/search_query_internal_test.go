package api

import "testing"

func TestIndexerQueryDropsTheAmpersand(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"Tom & Jerry", "Tom Jerry"},
		{"Law & Order", "Law Order"},
		{"Crosby, Stills & Nash", "Crosby, Stills Nash"},
		{"Dungeons &amp; Dragons", "Dungeons Dragons"},
		{"Fast and Furious", "Fast and Furious"},
		// inside a word it is part of the name and stays
		{"AT&T Story", "AT&T Story"},
		{"R&B Divas", "R&B Divas"},
		{"Q&A", "Q&A"},
		{"  Sam  &  Max ", "Sam Max"},
		// nothing left to search for: keep the title as it is
		{"&", "&"},
	} {
		if got := indexerQuery(tt.in); got != tt.want {
			t.Errorf("indexerQuery(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTVSearchQueryDropsTheAmpersand(t *testing.T) {
	if got, want := tvSearchQuery("Law & Order", 1, 2), "Law Order S01E02"; got != want {
		t.Errorf("episode query = %q, want %q", got, want)
	}
	if got, want := tvSearchQuery("Brothers & Sisters", 2, 0), "Brothers Sisters S02"; got != want {
		t.Errorf("season query = %q, want %q", got, want)
	}
}
