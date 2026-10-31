package indexers

import "testing"

func TestDropPlaceholders(t *testing.T) {
	in := []Result{
		{Title: "No results returned", InfoHash: "0000000000000000000000000000000000000000"},
		{Title: "No results found"},
		{Title: "Absolute Batman - Arkham Asylum (2019)", SizeBytes: 400 << 20},
		// a real release that happens to be named so is kept (it has a size)
		{Title: "No Results", SizeBytes: 700 << 20, InfoHash: "abc123"},
	}
	out := dropPlaceholders(in)
	if len(out) != 2 || out[0].Title != "Absolute Batman - Arkham Asylum (2019)" || out[1].Title != "No Results" {
		t.Fatalf("got %+v", out)
	}
}
