package api

import (
	"sort"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

func TestMovieReleaseFor(t *testing.T) {
	cases := []struct {
		title, name string
		year        int
		want        bool
	}{
		{"Dune.Part.Two.2024.2160p.WEB-DL.DDP5.1.Atmos.DV.HDR10.H.265-FLUX", "Dune: Part Two", 2024, true},
		{"The.Matrix.1999.1080p.BluRay.x264", "The Matrix", 1999, true},
		{"Matrix.1999.1080p.BluRay.x264", "The Matrix", 1999, true},
		{"The.Matrix.Reloaded.2003.1080p.BluRay.x264", "The Matrix", 1999, false},
		{"Alien.1979.Directors.Cut.1080p", "Alien", 1979, true},
		{"Aliens.1986.1080p", "Alien", 1979, false},
		{"Alien.S01E01.1080p.WEB", "Alien", 0, false},
		{"Some.Film.2023.1080p", "Some Film", 2022, true}, // a year off: posters and TMDB differ
		{"Some.Film.2020.1080p", "Some Film", 2022, false},
		{"Some.Film.1080p", "Some Film", 2022, false},
	}
	for _, c := range cases {
		if got := movieReleaseFor(c.title, c.name, c.year); got != c.want {
			t.Errorf("movieReleaseFor(%q, %q, %d) = %v, want %v", c.title, c.name, c.year, got, c.want)
		}
	}
}

func TestHeightOf(t *testing.T) {
	for title, want := range map[string]int{
		"Show.S01E01.2160p.WEB": 2160, "Show.S01E01.4K.WEB": 2160, "Show.S01E01.1080p.WEB": 1080,
		"Show.S01E01.720p.HDTV": 720, "Show.S01E01.480p.DVDRip": 480, "Show.S01E01.WEB": 0,
	} {
		if got := heightOf(title); got != want {
			t.Errorf("heightOf(%q) = %d, want %d", title, got, want)
		}
	}
}

func TestStreamProfileAllowsAndOrders(t *testing.T) {
	p := streamProfile{Quality: "1080p", MaxSizeGB: 10, Exclude: "av1, remux"}
	gb := int64(1 << 30)
	if p.allows("Show.S01E01.2160p.WEB", 5*gb) {
		t.Error("a 2160p release passed the 1080p profile")
	}
	if p.allows("Show.S01E01.1080p.WEB", 12*gb) {
		t.Error("a 12 GB release passed a 10 GB limit")
	}
	if p.allows("Show.S01E01.1080p.BluRay.Remux", 8*gb) || p.allows("Show.S01E01.1080p.WEB.AV1", 2*gb) {
		t.Error("an excluded word passed")
	}
	if !p.allows("Show.S01E01.1080p.WEB", 3*gb) || !p.allows("Show.S01E01.WEB", 0) {
		t.Error("a good release was refused")
	}
	list := []indexers.Result{
		{Title: "Show.S01E01.720p.WEB", SizeBytes: 2 * gb},
		{Title: "Show.S01E01.1080p.HDTV", SizeBytes: 4 * gb},
		{Title: "Show.S01E01.1080p.WEB", SizeBytes: 3 * gb},
		{Title: "Show.S01E01.1080p.WEB.x265", SizeBytes: 2 * gb},
	}
	sort.SliceStable(list, func(i, j int) bool { return p.less(list[i], list[j]) })
	want := []string{"Show.S01E01.1080p.WEB", "Show.S01E01.1080p.WEB.x265", "Show.S01E01.1080p.HDTV", "Show.S01E01.720p.WEB"}
	for i, r := range list {
		if r.Title != want[i] {
			t.Fatalf("order %d: %s, want %s (all: %v)", i, r.Title, want[i], list)
		}
	}
}

func TestDefaultStreamProfilesAreComplete(t *testing.T) {
	got := defaultStreamProfiles()
	if len(got) != 3 || got[0].Quality != "4K" || got[1].Quality != "1080p" || got[2].Quality != "720p" {
		t.Fatalf("defaults: %+v", got)
	}
	for _, p := range got {
		if !p.excluded("Show.S01E01.1080p.AV1") {
			t.Errorf("%s: AV1 is not excluded by default", p.Quality)
		}
	}
}

func TestUstreamTokenAndClose(t *testing.T) {
	if got := ustreamToken("http://127.0.0.1:8264/api/ustream/us_abc123/Show.S01E01.mkv"); got != "us_abc123" {
		t.Fatalf("token = %q", got)
	}
	if ustreamToken("http://x/other/us_abc") != "" {
		t.Fatal("a token came out of an address that isn't a stream")
	}
	ustreamMu.Lock()
	ustreamSessions["us_gone"] = &ustreamSession{}
	ustreamMu.Unlock()
	ustreamClose("us_gone")
	ustreamClose("") // nothing to close
	ustreamMu.Lock()
	_, still := ustreamSessions["us_gone"]
	ustreamMu.Unlock()
	if still {
		t.Fatal("the session was not closed")
	}
}
