package api

import (
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/quality"
)

// Usherr: the profile editor's Advanced options in a search.

func extrasProfile() quality.Profile {
	return quality.Profile{Name: "HD", Allowed: []quality.Tier{quality.TierWebDL1080p, quality.TierBluray1080p}, Cutoff: quality.TierBluray1080p}
}

func TestExtrasPickHEVCGroupAndSize(t *testing.T) {
	p := extrasProfile()
	results := []indexers.Result{
		{Title: "Movie.2024.1080p.BluRay.x264-GRP", SizeBytes: 12 << 30},
		{Title: "Movie.2024.1080p.BluRay.x265-GRP", SizeBytes: 6 << 30},
	}
	if best := pickBestFor(results, p, 2024); best == nil || !strings.Contains(best.Title, "x264") {
		t.Fatalf("without the option the first equal release should win, got %v", best)
	}
	p.PreferHEVC = true
	if best := pickBestFor(results, p, 2024); best == nil || !strings.Contains(best.Title, "x265") {
		t.Errorf("prefer x265: got %v", best)
	}

	p = extrasProfile()
	p.PreferGroups = []string{"framestor"}
	results = []indexers.Result{
		{Title: "Movie.2024.1080p.BluRay.x264-GRP", SizeBytes: 12 << 30},
		{Title: "Movie.2024.1080p.BluRay.x264-FraMeSToR", SizeBytes: 14 << 30},
	}
	if best := pickBestFor(results, p, 2024); best == nil || !strings.Contains(best.Title, "FraMeSToR") {
		t.Errorf("preferred group: got %v", best)
	}

	p = extrasProfile()
	p.PreferSmaller = true
	results = []indexers.Result{
		{Title: "Movie.2024.1080p.BluRay.x264-AAA", SizeBytes: 20 << 30},
		{Title: "Movie.2024.1080p.BluRay.x264-BBB", SizeBytes: 9 << 30},
	}
	if best := pickBestFor(results, p, 2024); best == nil || !strings.Contains(best.Title, "BBB") {
		t.Errorf("prefer smaller: got %v", best)
	}

	p = extrasProfile()
	p.TierSizes = map[quality.Tier]quality.TierSize{quality.TierBluray1080p: {MaxGB: 15}}
	if best := pickBestFor(results, p, 2024); best == nil || !strings.Contains(best.Title, "BBB") {
		t.Errorf("a 20 GB release over the 15 GB range was picked: %v", best)
	}
}

func TestExtrasReasons(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	p := extrasProfile()
	p.Protocol = quality.ProtocolUsenet
	p.MinAgeMinutes = 30
	p.MaxAgeDays = 365
	usenet := indexers.Result{Title: "Movie.2024.1080p.BluRay.x264-GRP", IndexerName: "Geek", PublishDate: now.Add(-2 * time.Hour)}
	if why := extrasReason(p, nil, usenet, 0, now); why != "" {
		t.Errorf("a fine release refused: %s", why)
	}
	torrent := usenet
	torrent.Torrent = true
	if why := extrasReason(p, nil, torrent, 0, now); !strings.Contains(why, "Usenet only") {
		t.Errorf("torrent: %q", why)
	}
	fresh := usenet
	fresh.PublishDate = now.Add(-5 * time.Minute)
	if why := extrasReason(p, nil, fresh, 0, now); !strings.Contains(why, "waits") {
		t.Errorf("too new: %q", why)
	}
	old := usenet
	old.PublishDate = now.AddDate(-2, 0, 0)
	if why := extrasReason(p, nil, old, 0, now); !strings.Contains(why, "older") {
		t.Errorf("too old: %q", why)
	}
	if why := extrasReason(p, map[string]bool{"finder": true}, usenet, 0, now); !strings.Contains(why, "doesn't search") {
		t.Errorf("other indexer: %q", why)
	}
}

func TestTryProfile(t *testing.T) {
	p := extrasProfile()
	p.NeverGroups = []string{"YIFY"}
	p.MaxSizeGB = 20
	ok := tryOne(p, tryRelease{Title: "Movie.2024.1080p.BluRay.x264-GRP", SizeGB: 12})
	if !ok.Take || ok.Quality != "Bluray-1080p" {
		t.Errorf("a fitting release: %+v", ok)
	}
	bad := tryOne(p, tryRelease{Title: "Movie.2024.2160p.WEBRip.x264-YIFY", SizeGB: 40})
	if bad.Take || len(bad.Skip) < 3 {
		t.Errorf("want the quality, the group and the size given as reasons: %+v", bad)
	}
}
