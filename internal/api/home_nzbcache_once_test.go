package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/config"
	"github.com/rdborg/mediarium/internal/indexers"
)

const onceNZB = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><file poster="a" date="1" subject="x"><groups><group>a.b</group></groups><segments><segment bytes="10" number="1">abc@x</segment></segments></file></nzb>`

// Prowlarr gives the same release a new link at every search. Usherr must
// still fetch its NZB from the indexer once: NZBFinder counts each repeat
// and closes accounts that keep doing it. Several parts of Usherr asking at
// the same moment share one fetch too.
func TestReleaseNZBFetchedOnce(t *testing.T) {
	var hits atomic.Int32
	idx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(onceNZB))
	}))
	defer idx.Close()
	s := &Server{cfg: config.Config{ConfigDir: t.TempDir()}}
	release := func(link string) indexers.Result {
		return indexers.Result{Title: "Show.S01E01.1080p.WEB-DL-GRP", IndexerName: "NZBFinder (via Prowlarr)", SizeBytes: 2_000_000_000, DownloadURL: idx.URL + "/2/download?link=" + link}
	}
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.fetchReleaseOnce(ctx, release("search"+string(rune('a'+i)))); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, err := s.fetchReleaseOnce(ctx, release("a-later-search")); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("the indexer was asked %d times for one release; once is the most", n)
	}

	// A different release is a different NZB.
	other := release("x")
	other.Title = "Show.S01E02.1080p.WEB-DL-GRP"
	if _, err := s.fetchReleaseOnce(ctx, other); err != nil || hits.Load() != 2 {
		t.Fatalf("another release: %v, %d fetches", err, hits.Load())
	}

	// A release found broken is passed over, whatever its next link.
	s.markNZBBadKey(releaseKey(release("first")))
	if !s.nzbBadKey(releaseKey(release("second"))) {
		t.Error("a broken release should stay marked under a new link")
	}
}
