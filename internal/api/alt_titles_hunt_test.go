package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/settings"
)

// titleIndexer is a fake Newznab indexer that only knows a film by its German
// title: it answers a search for "Der Fixture Film" with the German release and
// every other search with nothing. It writes down what it was asked.
type titleIndexer struct {
	*httptest.Server
	mu      sync.Mutex
	queries []string
}

func newTitleIndexer(t *testing.T) *titleIndexer {
	t.Helper()
	ix := &titleIndexer{}
	ix.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		ix.mu.Lock()
		ix.queries = append(ix.queries, q)
		ix.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		items := ""
		if strings.EqualFold(q, "Der Fixture Film") {
			items = fmt.Sprintf(`<item>
<title>Der.Fixture.Film.1999.German.DL.1080p.BluRay.x264-FIXTURE</title>
<guid>german-guid</guid>
<enclosure url="http://%s/nzb/german.nzb" length="9663676416" type="application/x-nzb" />
<newznab:attr name="size" value="9663676416"/>
<newznab:attr name="category" value="2000"/>
</item>`, r.Host)
		}
		fmt.Fprintf(w, `<?xml version="1.0"?>
<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>%s</channel></rss>`, items)
	}))
	t.Cleanup(ix.Close)
	return ix
}

func (ix *titleIndexer) asked() []string {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var out []string
	for _, q := range ix.queries {
		if q != "" { // a look at the latest releases, not a title search
			out = append(out, q)
		}
	}
	sort.Strings(out)
	return out
}

// newNamesTMDB is a fake TMDB that lists the German names of movie 605 and
// counts the requests it gets.
func newNamesTMDB(t *testing.T, translations []string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/movie/605" {
			http.NotFound(w, r)
			return
		}
		var tr []map[string]any
		for _, name := range translations {
			tr = append(tr, map[string]any{"iso_3166_1": "DE", "iso_639_1": "de", "data": map[string]any{"title": name}})
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": 605, "title": "The Fixture Movie", "original_title": "The Fixture Movie", "original_language": "en",
			"alternative_titles": map[string]any{"titles": []map[string]any{}},
			"translations":       map[string]any{"translations": tr},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func setUpTitleHunt(t *testing.T, languages string, translations []string) (*titleIndexer, *atomic.Int32, bool) {
	t.Helper()
	server, httpSrv, client := newHuntTestServer(t)
	ix := newTitleIndexer(t)
	tmdb, calls := newNamesTMDB(t, translations)
	server.TestSetTMDBBaseURL("k", tmdb.URL)
	if languages != "" {
		if err := server.Settings.Set(settings.KeyQualityLanguage, languages, false); err != nil {
			t.Fatal(err)
		}
	}
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/onboarding/admin", map[string]string{
		"username": "ryan", "password": "correct-horse-battery-staple", "firstName": "Ryan", "lastName": "Tester",
	}, http.StatusCreated)
	postJSON[map[string]any](t, client, httpSrv.URL+"/api/indexers", map[string]any{
		"name": "Fixture Indexer", "definitionId": "fixture", "baseUrl": ix.URL, "apiKey": "fixture-key",
	}, http.StatusCreated)
	if _, err := server.MovieRepo.Add(library.Movie{TMDBID: 605, Title: "The Fixture Movie", Year: 1999, Monitored: true}); err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	// Adding the indexer tested it: only what the hunt asks is counted.
	ix.mu.Lock()
	ix.queries = nil
	ix.mu.Unlock()
	calls.Store(0)

	server.TestHunt(context.Background())
	waitBackground(t, server)

	queue := getJSON[[]map[string]any](t, client, httpSrv.URL+"/api/queue")
	grabbed := false
	if len(queue) > 0 {
		if title, _ := queue[0]["releaseTitle"].(string); title == "Der.Fixture.Film.1999.German.DL.1080p.BluRay.x264-FIXTURE" {
			grabbed = true
		}
		// The fixture serves no NZB, so the download fails; wait for that so
		// it does not outlive the test.
		waitForQueueTerminal(t, client, httpSrv.URL)
	}
	return ix, calls, grabbed
}

// A person who wants German audio finds a release that is named by the German
// title, which a search for the English title never turns up.
func TestHuntFindsAReleaseNamedByTheGermanTitle(t *testing.T) {
	ix, calls, grabbed := setUpTitleHunt(t, "German", []string{"Der Fixture Film", "Der Fixture Film"})
	if !grabbed {
		t.Fatalf("the German release was not grabbed; the indexer was asked %q", ix.asked())
	}
	if want := []string{"Der Fixture Film", "The Fixture Movie"}; !reflect.DeepEqual(ix.asked(), want) {
		t.Errorf("the indexer was asked %q, want %q", ix.asked(), want)
	}
	if calls.Load() != 1 {
		t.Errorf("TMDB was asked %d times for the titles, want 1", calls.Load())
	}
}

// However many names TMDB has, a hunt stops at the first other title that
// finds the release, so it never asks for all of them.
func TestHuntStopsOnceAnOtherTitleFindsTheRelease(t *testing.T) {
	names := []string{"Der Fixture Film", "Fixture Film Eins", "Fixture Film Zwei", "Fixture Film Drei", "Fixture Film Vier", "Fixture Film Fuenf", "Fixture Film Sechs"}
	ix, _, grabbed := setUpTitleHunt(t, "German", names)
	if !grabbed {
		t.Fatalf("the German release was not grabbed; the indexer was asked %q", ix.asked())
	}
	if got := len(ix.asked()); got != 2 {
		t.Errorf("the indexer was asked %d times (%q), want the title and the one other title that found it", got, ix.asked())
	}
}

// With English as the only language wanted, nothing extra is searched and TMDB
// is not asked for other names.
func TestHuntDoesNothingExtraForEnglish(t *testing.T) {
	ix, calls, grabbed := setUpTitleHunt(t, "", []string{"Der Fixture Film"})
	if grabbed {
		t.Error("a German release was grabbed for an English-only person")
	}
	if want := []string{"The Fixture Movie"}; !reflect.DeepEqual(ix.asked(), want) {
		t.Errorf("the indexer was asked %q, want %q", ix.asked(), want)
	}
	if calls.Load() != 0 {
		t.Errorf("TMDB was asked %d times, want none", calls.Load())
	}
}
