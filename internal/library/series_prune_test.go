package library_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

func TestPruneEpisodesRemovesOnlyWhatTMDBDroppedAndNeverGotAnything(t *testing.T) {
	repo := newRepo(t)
	all := []library.Episode{
		{Season: 1, Episode: 1}, {Season: 1, Episode: 2}, {Season: 1, Episode: 3},
		{Season: 1, Episode: 4}, {Season: 1, Episode: 5}, {Season: 1, Episode: 6},
	}
	sr, err := repo.AddSeries(library.Series{TMDBID: 1, Title: "Show", Monitored: true}, all)
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := repo.ListEpisodes(sr.ID)
	byNum := map[int]library.Episode{}
	for _, e := range eps {
		byNum[e.Episode] = e
	}
	// E02 has a file and E03 is downloading: both stay although TMDB dropped them.
	if err := repo.SetEpisodeStatus(byNum[2].ID, library.StatusDownloaded, "WEBDL-1080p", "/tv/Show/E02.mkv"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetEpisodeStatus(byNum[3].ID, library.StatusDownloading, "", ""); err != nil {
		t.Fatal(err)
	}

	// TMDB now lists E01, E05 and E06 (E02 to E04 gone).
	keep := []library.Episode{{Season: 1, Episode: 1}, {Season: 1, Episode: 5}, {Season: 1, Episode: 6}}
	n, err := repo.PruneEpisodes(sr.ID, keep)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("removed %d, want 1 (only E04: missing, no file)", n)
	}
	left, _ := repo.ListEpisodes(sr.ID)
	got := map[int]bool{}
	for _, e := range left {
		got[e.Episode] = true
	}
	for _, want := range []int{1, 2, 3, 5, 6} {
		if !got[want] {
			t.Errorf("E%02d should still be there", want)
		}
	}
	if got[4] {
		t.Error("E04 is no longer listed and has nothing: it should be gone")
	}
}

func TestPruneEpisodesIgnoresABadAnswer(t *testing.T) {
	repo := newRepo(t)
	sr, err := repo.AddSeries(library.Series{TMDBID: 2, Title: "Show", Monitored: true},
		[]library.Episode{{Season: 1, Episode: 1}, {Season: 1, Episode: 2}, {Season: 1, Episode: 3}, {Season: 1, Episode: 4}})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := repo.PruneEpisodes(sr.ID, nil); n != 0 {
		t.Fatalf("an empty list removed %d episodes", n)
	}
	if n, _ := repo.PruneEpisodes(sr.ID, []library.Episode{{Season: 1, Episode: 1}}); n != 0 {
		t.Fatalf("a list covering a quarter of the show removed %d episodes", n)
	}
	if left, _ := repo.ListEpisodes(sr.ID); len(left) != 4 {
		t.Fatalf("episodes left: %d", len(left))
	}
}
