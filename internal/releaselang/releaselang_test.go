package releaselang

import "testing"

func TestOther(t *testing.T) {
	cases := []struct{ title, series, want string }{
		{"One Piece v001 (Japanese) (raw)", "One Piece", "Japanese"},
		{"One.Piece.v001.RAW", "One Piece", "Japanese"},
		{"Solo Leveling v01 (Korean)", "Solo Leveling", "Korean"},
		{"Some Title 012 [CHS]", "Some Title", "Chinese"},
		{"Vagabond des limbes - T25", "Vagabond", "French"},
		{"Asterix Tome 12", "Asterix", "French or Spanish"},
		{"Batman 001 (ger)", "Batman", "German"},
		// Not languages.
		{"Strawberry Panic v01 (Digital)", "Strawberry Panic", ""},
		{"Raw Hero v01 (Digital)", "Raw Hero", ""},
		{"The French Dispatch 001 (French)", "The French Dispatch", ""},
		{"Terminator T2 001 (Digital)", "Terminator T2", ""},
		{"Terminator T2 001 (Digital)", "Terminator", ""},
	}
	for _, c := range cases {
		if got := Other(c.title, c.series); got != c.want {
			t.Errorf("%q: %q", c.title, got)
		}
	}
	if got := OtherVolume("Some Title T2 (Digital)", "Some Title"); got != "French" {
		t.Errorf("T2 of a volume: %q", got)
	}
}
