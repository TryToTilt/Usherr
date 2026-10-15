package comics

import "testing"

func TestMarvelNowIssues(t *testing.T) {
	s := Series{Name: "Captain America", StartYear: 2013}
	now := Issue{Number: "16", Title: ".NOW The Iron Nail, Prologue: A Decision", CoverDate: "2014-03-01"}
	plain := Issue{Number: "17", Title: "The Iron Nail: Part 1", CoverDate: "2014-04-01"}
	if got := AltNumber(now); got != "15.now" {
		t.Fatalf("AltNumber = %q", got)
	}
	if AltNumber(plain) != "" {
		t.Fatal("plain issue got an alt number")
	}
	if got := DisplayTitle(now); got != "The Iron Nail, Prologue: A Decision" {
		t.Fatalf("DisplayTitle = %q", got)
	}
	for name, want := range map[string]bool{
		"Captain America 015.NOW (2014) (Digital) (Zone-Empire)": true,
		"Captain.America.015.NOW.2014.Digital.cbr":               true,
		"Captain America #15.NOW (2014)":                         true,
		"Captain America 016 (2014) (Digital)":                   false, // the plain #16 is another issue
		"Captain America 015 (2014) (Digital)":                   false,
	} {
		if got := Matches(ParseName(name), s, now); got != want {
			t.Errorf("%s: matches = %v, want %v (parsed %+v)", name, got, want, ParseName(name))
		}
	}
	if !Matches(ParseName("Captain America 017 (2014) (Digital)"), s, plain) {
		t.Error("plain issue no longer matches")
	}
	if q := SearchQueries(s, now); len(q) == 0 || q[0] != "Captain America 015.NOW" {
		t.Errorf("queries = %v", q)
	}
}
