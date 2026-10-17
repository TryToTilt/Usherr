package indexers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
)

// A torrent the indexer says has 0 seeders is dead; one that does not say is unknown.
func TestTorznabSaysWhetherSeedersAreKnown(t *testing.T) {
	const feed = `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>Dead.Film.2026.1080p.WEB-DL.x264-GRP</title><guid>1</guid><enclosure url="magnet:?xt=urn:btih:aaaa" length="100" type="application/x-bittorrent"/><torznab:attr name="seeders" value="0"/></item>
<item><title>Live.Film.2026.1080p.WEB-DL.x264-GRP</title><guid>2</guid><enclosure url="magnet:?xt=urn:btih:bbbb" length="100" type="application/x-bittorrent"/><torznab:attr name="seeders" value="7"/></item>
<item><title>Unknown.Film.2026.1080p.WEB-DL.x264-GRP</title><guid>3</guid><enclosure url="magnet:?xt=urn:btih:cccc" length="100" type="application/x-bittorrent"/></item>
</channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(feed))
	}))
	defer srv.Close()
	results, err := indexers.NewNewznabClient("T", srv.URL, "k").Search(context.Background(), "film", []int{2000})
	if err != nil || len(results) != 3 {
		t.Fatalf("search: %v, %d results", err, len(results))
	}
	want := []struct {
		known bool
		n     int
	}{{true, 0}, {true, 7}, {false, 0}}
	for i, w := range want {
		if results[i].SeedersKnown != w.known || results[i].Seeders != w.n {
			t.Errorf("%s: seeders %d known %v, want %d known %v", results[i].Title, results[i].Seeders, results[i].SeedersKnown, w.n, w.known)
		}
	}
}
