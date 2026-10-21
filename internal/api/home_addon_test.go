package api

import "testing"

func TestParseAddonID(t *testing.T) {
	cases := []struct {
		typ, id string
		want    addonRef
		ok      bool
	}{
		{"movie", "tt0133093", addonRef{Kind: "movie", IMDB: "tt0133093"}, true},
		{"series", "tt0903747:5:14", addonRef{Kind: "series", IMDB: "tt0903747", Season: 5, Episode: 14}, true},
		{"series", "tt0903747", addonRef{}, false}, // a show without an episode isn't asked for
		{"series", "tt0903747:1:0", addonRef{}, false},
		{"movie", "usherr:tmdb:movie:603", addonRef{Kind: "movie", TMDB: 603}, true},
		{"series", "usherr:tmdb:series:1399", addonRef{Kind: "series", TMDB: 1399}, true},
		{"series", "usherr:tmdb:series:1399:2:3", addonRef{Kind: "series", TMDB: 1399, Season: 2, Episode: 3}, true},
		{"tv", "usherr:tv:42", addonRef{Kind: "tv", Channel: 42}, true},
		{"movie", "usherr:tmdb:movie:x", addonRef{}, false},
		{"movie", "usherr:tmdb:book:5", addonRef{}, false},
		{"movie", "kitsu:1", addonRef{}, false},
		{"movie", "", addonRef{}, false},
	}
	for _, c := range cases {
		got, ok := parseAddonID(c.typ, c.id)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseAddonID(%q, %q) = %+v, %v; want %+v, %v", c.typ, c.id, got, ok, c.want, c.ok)
		}
	}
}

func TestAddonTitleID(t *testing.T) {
	if got := addonTitleID("movie", 603, "tt0133093"); got != "tt0133093" {
		t.Errorf("with an IMDb id: %q", got)
	}
	if got := addonTitleID("series", 1399, ""); got != "usherr:tmdb:series:1399" {
		t.Errorf("without: %q", got)
	}
	if got := addonTitleID("series", 1399, "nonsense"); got != "usherr:tmdb:series:1399" {
		t.Errorf("a bad IMDb id: %q", got)
	}
}
