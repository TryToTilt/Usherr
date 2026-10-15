package comics

import "testing"

// "Absolute" starts the names of real series (Marvel's Absolute Carnage line,
// DC's Absolute Batman); only an "Absolute Edition" is a collected book.
func TestAbsoluteIsASeriesNameNotACollection(t *testing.T) {
	for file, want := range map[string]string{
		"Absolute Carnage - Scream 001 (2019) (Digital) (Zone-Empire).cbr":        "Absolute Carnage: Scream",
		"Absolute Carnage - Miles Morales 002 (2019) (Digital) (Zone-Empire).cbr": "Absolute Carnage: Miles Morales",
		"Absolute Batman 003 (2025) (Digital).cbz":                                "Absolute Batman",
	} {
		n := ParseName(file)
		if Key(n.Series) != Key(want) || n.Number == "" || n.Collection {
			t.Fatalf("%q parsed as series %q, number %q, collection %v; want %q", file, n.Series, n.Number, n.Collection, want)
		}
	}
	if n := ParseName("Batman - Hush (Absolute Edition) (2005).cbz"); !n.Collection {
		t.Fatal("an Absolute Edition is still a collected book")
	}
	s := Series{Name: "Absolute Carnage: Scream", Publisher: "Marvel", IssueCount: 3}
	if v := Evaluate(Release{Title: "Absolute Carnage - Scream 002 (2019) (Digital) (Zone-Empire)"}, s, Issue{Number: "2"}); v.Reject != "" {
		t.Fatalf("the issue itself was turned down: %s", v.Reject)
	}
}
