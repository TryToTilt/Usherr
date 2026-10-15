package api_test

import (
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/queue"
)

// Home fork: a release named for a later season ("Season 3 - 01 ... S03E01")
// of a show numbered as one long season starts as the episode it was grabbed
// for (S01E49), instead of failing with "already in your library".
func TestQueuedDownloadKeepsTheEpisodeItWasGrabbedFor(t *testing.T) {
	env := newTVAutoEnv(t, nil, []episodeSpec{
		{1, 48, "2025-07-04", library.StatusDownloaded, "WEBDL-1080p"},
		{1, 49, "2026-10-02", library.StatusMissing, ""},
	})
	item := queue.Item{
		ID:           1,
		SeriesID:     env.seriesID,
		Season:       1,
		Episode:      49,
		ReleaseTitle: "[Half-Baked] Fixture Show Season 3 - 01 [WEB 1080p HEVC AAC] _ Fixture Show - S03E01",
		Protocol:     queue.ProtocolUsenet,
	}
	if _, err := env.server.TestPreparePipeline(item); err != nil {
		t.Fatalf("the download should start: %v", err)
	}
	if ep, _ := env.server.MovieRepo.GetEpisode(env.seriesID, 1, 49); ep.Status != library.StatusDownloading {
		t.Fatalf("S01E49 should be downloading: %s", ep.Status)
	}
}
