package comics

import "testing"

func TestOtherLanguageReleasesAreRejected(t *testing.T) {
	s := Series{Name: "The Amazing Spider-Man"}
	i := Issue{Number: "1"}
	for _, title := range []string{
		"The Amazing Spider-Man 001 (2025) (ger) (digital)",
		"The.Amazing.Spider-Man.001.2025.German.CBR",
		"The Amazing Spider-Man 001 (2025) [Deutsch]",
		"The Amazing Spider-Man 001 (2025) (fr) FRENCH",
	} {
		if v := Evaluate(Release{Title: title}, s, i); v.Reject == "" {
			t.Errorf("%q: not rejected", title)
		}
	}
	if v := Evaluate(Release{Title: "The Amazing Spider-Man 001 (2025) (digital) (Zone-Empire)"}, s, i); v.Reject != "" {
		t.Errorf("an English release was rejected: %s", v.Reject)
	}
}
