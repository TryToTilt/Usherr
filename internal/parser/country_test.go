package parser

import "testing"

func TestCanonicalCountry(t *testing.T) {
	for in, want := range map[string]string{
		"US": "US", "usa": "US", "UK": "GB", "gb": "GB", "GBR": "GB", "AU": "AU", "nz": "NZ",
		"CA": "CA", "DE": "DE", "": "", "XX": "", "The": "", "S01": "",
	} {
		if got := CanonicalCountry(in); got != want {
			t.Errorf("CanonicalCountry(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitCountryTag(t *testing.T) {
	for _, tc := range []struct{ in, base, country string }{
		{"Shameless US", "Shameless", "US"},
		{"Shameless (UK)", "Shameless", "GB"},
		{"Shameless.UK", "Shameless", "GB"},
		{"The Office (US)", "The Office", "US"},
		{"The Office USA", "The Office", "US"},
		{"Top Gear (AU)", "Top Gear", "AU"},
		{"Hell's Kitchen - NZ", "Hell's Kitchen", "NZ"},
		{"Fast & Furious GB", "Fast & Furious", "GB"},
		{"Shameless", "Shameless", ""},
		{"Us", "Us", ""},     // a title that is only a tag stays whole
		{"(UK)", "(UK)", ""}, // same
		{"Doctor Who", "Doctor Who", ""},
		{"Shameless USSR", "Shameless USSR", ""},
		{"", "", ""},
	} {
		base, country := SplitCountryTag(tc.in)
		if base != tc.base || country != tc.country {
			t.Errorf("SplitCountryTag(%q) = (%q, %q), want (%q, %q)", tc.in, base, country, tc.base, tc.country)
		}
	}
}

// What the parser itself hands over as a title for a country-tagged release.
func TestSplitCountryTagOnParsedReleases(t *testing.T) {
	for _, tc := range []struct{ name, base, country string }{
		{"Shameless.US.S01E01.720p.HDTV.x264-GRP", "Shameless", "US"},
		{"Shameless (UK) S02E03 1080p WEB-DL", "Shameless", "GB"},
		{"The.Office.US.S05E02.1080p.WEB.h264-GRP", "The Office", "US"},
		{"Top.Gear.UK.S30E01.720p-GRP", "Top Gear", "GB"},
		{"Shameless.S01E01.720p-GRP", "Shameless", ""},
	} {
		base, country := SplitCountryTag(Parse(tc.name).Title)
		if base != tc.base || country != tc.country {
			t.Errorf("%q: title %q split to (%q, %q), want (%q, %q)", tc.name, Parse(tc.name).Title, base, country, tc.base, tc.country)
		}
	}
}
