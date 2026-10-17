package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

// newLoginPageIndexer is a Newznab indexer with one release whose NZB link
// answers with a sign-in page, as an indexer does when its session or daily
// limit ran out. It counts the NZB requests.
func newLoginPageIndexer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var nzbRequests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0"?>
<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">
<channel>
<item>
<title>The.Fixture.Movie.1999.1080p.BluRay.x264-LOGiN</title>
<guid>fixture-guid-login</guid>
<enclosure url="%[1]s/getnzb/login.nzb" length="8400000000" type="application/x-nzb" />
<newznab:attr name="size" value="8400000000"/>
<newznab:attr name="category" value="2000"/>
</item>
</channel>
</rss>`, "http://"+r.Host)
	})
	mux.HandleFunc("/getnzb/", func(w http.ResponseWriter, r *http.Request) {
		nzbRequests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body><form>Sign in</form></body></html>"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &nzbRequests
}

// A release whose NZB link sends a sign-in page is not grabbed again by the
// next automatic searches (the scheduled search and the RSS check): it is held
// back for some hours, the failed row says so, and the "failed" notification
// goes out once, even when a person grabs it again by hand.
func TestReleaseWhoseLinkSendsAPageIsHeldBack(t *testing.T) {
	server, httpSrv, client := newHuntTestServer(t)
	indexerSrv, nzbRequests := newLoginPageIndexer(t)
	hook, rec := newHookRecorder(t, http.StatusOK)

	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": indexerSrv.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/notifications", map[string]any{
		"name": "Hook", "type": "webhook", "events": []string{"failed"}, "config": map[string]string{"url": hook.URL},
	}, http.StatusCreated)
	movie, err := server.MovieRepo.Add(library.Movie{TMDBID: 6077, Title: "The Fixture Movie", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}

	server.TestHunt(context.Background())
	waitBackground(t, server)
	if got := nzbRequests.Load(); got != 1 {
		t.Fatalf("the first search grabs the release once, NZB requests = %d", got)
	}
	queueList := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
	if len(queueList) != 1 || queueList[0]["status"] != "failed" {
		t.Fatalf("expected one failed download, got %+v", queueList)
	}
	if note, _ := queueList[0]["note"].(string); !strings.Contains(note, "leave this release alone") {
		t.Errorf("the failed row should say the release is held back, note = %q", note)
	}
	if rec.count() != 1 {
		t.Fatalf("one \"failed\" notification for the first failure, got %d", rec.count())
	}

	// The next scheduled search and the RSS check leave it alone.
	server.TestHunt(context.Background())
	server.TestRSSSync(context.Background())
	waitBackground(t, server)
	if got := nzbRequests.Load(); got != 1 {
		t.Fatalf("the held-back release was grabbed again: NZB requests = %d", got)
	}
	if n := len(getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")); n != 1 {
		t.Fatalf("no new download while the release is held back, got %d rows", n)
	}

	// A person can still pick it; it fails the same way, without a second
	// notification.
	results := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/search?q=fixture")
	if len(results) != 1 {
		t.Fatalf("expected the release in a search by hand, got %+v", results)
	}
	postJSON[map[string]any](t, client, fmt.Sprintf("%s/api/movies/%d/grab", httpSrv.URL, movie.ID), map[string]any{
		"releaseTitle": results[0]["title"], "downloadUrl": results[0]["downloadUrl"], "sizeBytes": 8400000000,
	}, http.StatusAccepted)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		rows := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
		if len(rows) == 2 && rows[0]["status"] == "failed" && rows[1]["status"] == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	waitBackground(t, server)
	if got := nzbRequests.Load(); got != 2 {
		t.Fatalf("a grab by hand is never held back: NZB requests = %d", got)
	}
	if rec.count() != 1 {
		t.Fatalf("the same failure again sends no second notification, got %d", rec.count())
	}
}
