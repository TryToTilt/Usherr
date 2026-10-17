package api

import (
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

func TestPickSkipsDeadTorrentsButNotUnknownOnes(t *testing.T) {
	torrent := func(title string, seeders int, known bool) indexers.Result {
		return indexers.Result{Title: title, DownloadURL: "http://x/" + title, Protocol: indexers.ProtocolTorrent, Seeders: seeders, SeedersKnown: known}
	}
	const (
		a = "Film.2026.1080p.WEB-DL.x264-AAA"
		b = "Film.2026.1080p.WEB-DL.x264-BBB"
		c = "Film.2026.720p.WEB-DL.x264-CCC"
	)
	profile := quality.Presets()[quality.PresetAny]
	tests := []struct {
		name    string
		results []indexers.Result
		want    string
	}{
		{"a dead torrent listed first loses to a live one of the same quality", []indexers.Result{torrent(a, 0, true), torrent(b, 12, true)}, b},
		{"a dead torrent of higher quality loses to a live lower one", []indexers.Result{torrent(a, 0, true), torrent(c, 3, true)}, c},
		{"unknown seeders are not dead", []indexers.Result{torrent(a, 0, false), torrent(c, 3, true)}, a},
		{"more seeders win between equals", []indexers.Result{torrent(a, 4, true), torrent(b, 40, true)}, b},
		{"a dead torrent is still taken when it is all there is", []indexers.Result{torrent(a, 0, true)}, a},
		{"a cached release needs no seeders", []indexers.Result{{Title: a, DownloadURL: "x", Protocol: indexers.ProtocolTorrent, SeedersKnown: true, Cached: true}, torrent(c, 3, true)}, a},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickedTitle(pickBestFor(tc.results, profile, 2026)); got != tc.want {
				t.Errorf("picked %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHuntWantsOnlyMoviesItWouldSearchFor(t *testing.T) {
	profile := quality.Presets()[quality.PresetAny]
	tests := []struct {
		name string
		m    library.Movie
		want bool
	}{
		{"missing and out", library.Movie{Monitored: true, Status: library.StatusMissing, ReleaseDate: "2020-01-01"}, true},
		{"missing, not out yet", library.Movie{Monitored: true, Status: library.StatusMissing, ReleaseDate: "2999-01-01"}, false},
		{"not monitored", library.Movie{Status: library.StatusMissing}, false},
		{"downloading", library.Movie{Monitored: true, Status: library.StatusDownloading}, false},
		{"downloaded, left alone", library.Movie{Monitored: true, Status: library.StatusDownloaded, NoUpgrade: true, Quality: "WEBDL-720p"}, false},
	}
	for _, tc := range tests {
		if got := huntWantsMovie(tc.m, profile); got != tc.want {
			t.Errorf("%s: huntWantsMovie = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNobodyAskedMeansNoNote(t *testing.T) {
	skipped := indexers.Outcome{IndexerName: "a", Skipped: true}
	asked := indexers.Outcome{IndexerName: "b"}
	if !nobodyAsked(nil) || !nobodyAsked([]indexers.Outcome{skipped, skipped}) {
		t.Error("a search where every indexer was skipped asked nobody")
	}
	if nobodyAsked([]indexers.Outcome{skipped, asked}) {
		t.Error("one indexer answered, so somebody was asked")
	}
}

func TestHuntWindowGoesOnWhereTheLastRunStopped(t *testing.T) {
	// 25 books and ten searches a run: three runs cover 0-9, 10-19, then 20-24 and 0-4.
	offset, covered := 0, map[int]int{}
	for run := 0; run < 3; run++ {
		start, n := huntWindow(25, offset, maxBookSearchesPerHunt)
		for i := 0; i < n; i++ {
			covered[(start+i)%25]++
		}
		offset = start + n
	}
	for i := 0; i < 25; i++ {
		if covered[i] == 0 {
			t.Errorf("book %d was never searched for in three runs", i)
		}
	}
	if start, n := huntWindow(4, 37, 10); start != 1 || n != 4 {
		t.Errorf("huntWindow(4, 37, 10) = %d, %d, want 1, 4", start, n)
	}
	if _, n := huntWindow(0, 5, 10); n != 0 {
		t.Errorf("nothing to search should take no searches, got %d", n)
	}
}
