package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/books"
)

// audioScanServer has audiobooks on, with Audible and Audnexus fakes that know
// nothing, so what ends up on a book came from its folder.
func audioScanServer(t *testing.T) (*api.Server, string, *http.Client, string) {
	t.Helper()
	server, base, client := loginNewServer(t)
	openLibrary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"docs":[{"key":"/works/OL1W","title":"The Long Road","author_name":["Ann Author"],"author_key":["OL1A"],"first_publish_year":1937}]}`)
	}))
	t.Cleanup(openLibrary.Close)
	server.TestSetOpenLibrary(openLibrary.URL)
	none := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/books/") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"products":[]}`)
	}))
	t.Cleanup(none.Close)
	server.TestSetAudnexus(none.URL, none.URL)
	root := t.TempDir()
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"audiobooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"audiobooksPath": root}, http.StatusOK)
	return server, base, client, root
}

func writeTree(t *testing.T, files map[string]string) {
	t.Helper()
	for p, body := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A new book takes what its folder says; running the import again fills only
// what is still empty, and leaves what someone or something else set alone.
func TestAudiobookImportFillsOnlyWhatIsEmpty(t *testing.T) {
	server, base, client, root := audioScanServer(t)
	api.SetAudioProberForTest(t, func(context.Context, string) (books.AudioProbe, error) {
		return books.AudioProbe{Seconds: 60, Tags: map[string]string{"composer": "Tag Reader", "date": "1937"}}, nil
	})
	dir := filepath.Join(root, "Ann Author", "Middle Earth", "Book 1 - The Long Road {Folder Reader} [B0015T963C]")
	writeTree(t, map[string]string{
		filepath.Join(dir, "1.mp3"):    "a",
		filepath.Join(dir, "desc.txt"): "A blurb from the folder.",
	})
	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{"format": "audiobook"}, http.StatusAccepted)
	waitBackground(t, server)
	list := getJSON[[]map[string]any](t, client, base+"/api/books")
	if len(list) != 1 {
		t.Fatalf("books: %v", list)
	}
	id := list[0]["id"]
	got := getJSON[map[string]any](t, client, fmt.Sprintf("%s/api/books/%v", base, id))
	audio := got["audiobook"].(map[string]any)
	if audio["path"] != dir {
		t.Errorf("the files stay where they are: %v", audio["path"])
	}
	if got["seriesName"] != "Middle Earth" || got["seriesPosition"] != "1" || got["narrators"] != "Folder Reader" || got["asin"] != "B0015T963C" {
		t.Errorf("read from the folder: %v", got)
	}

	// Now someone has set the series and narrator; the folder says more.
	repo := server.BookRepo
	bookID := int64(id.(float64))
	if err := repo.SetSeries(bookID, "Chosen Series", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetAudioDetails(bookID, books.AudioDetails{ASIN: "B00CHOSEN0", Narrators: []string{"Chosen Reader"}, RuntimeMin: 5}); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{"format": "audiobook"}, http.StatusAccepted)
	waitBackground(t, server)
	b, err := repo.Get(bookID)
	if err != nil {
		t.Fatal(err)
	}
	if b.SeriesName != "Chosen Series" || b.SeriesPosition != "" || b.Narrators != "Chosen Reader" || b.ASIN != "B00CHOSEN0" {
		t.Errorf("overwrote what was set: %+v", b)
	}
	if b.AudioPath != dir {
		t.Errorf("moved: %s", b.AudioPath)
	}
}

func TestMergeAudioDetailsKeepsTheFolders(t *testing.T) {
	server, base, client, root := audioScanServer(t)
	writeTree(t, map[string]string{filepath.Join(root, "Ann Author", "The Long Road {Folder Reader}", "1.mp3"): "a"})
	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{"format": "audiobook"}, http.StatusAccepted)
	waitBackground(t, server)
	list, err := server.BookRepo.List()
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	// The fakes know nothing: the look-up must not wipe the folder's narrator.
	if list[0].Narrators != "Folder Reader" {
		t.Errorf("after the look-up: %q", list[0].Narrators)
	}
	if list[0].AudioCheckedAt == "" {
		t.Error("the look-up isn't noted")
	}
}

// Opening a book reads its lengths whatever the hour; the tracks answer says so
// until they are in, then gives them.
func TestTracksReadLengthsOnDemand(t *testing.T) {
	server, base, client, root := audioScanServer(t)
	api.SetAudioProberForTest(t, func(_ context.Context, path string) (books.AudioProbe, error) {
		if strings.HasPrefix(filepath.Base(path), "2") {
			return books.AudioProbe{Seconds: 200.5}, nil
		}
		return books.AudioProbe{Seconds: 100}, nil
	})
	writeTree(t, map[string]string{
		filepath.Join(root, "Ann Author", "The Long Road", "1.mp3"): "a",
		filepath.Join(root, "Ann Author", "The Long Road", "2.mp3"): "b",
	})
	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{"format": "audiobook"}, http.StatusAccepted)
	waitBackground(t, server)
	list := getJSON[[]map[string]any](t, client, base+"/api/books")
	if len(list) != 1 {
		t.Fatalf("books: %v", list)
	}
	url := fmt.Sprintf("%s/api/books/%v/tracks", base, list[0]["id"])
	first := getJSON[map[string]any](t, client, url)
	if first["lengthsPending"] != true {
		t.Errorf("the first answer: %v", first)
	}
	waitBackground(t, server)
	deadline := time.Now().Add(5 * time.Second)
	var again map[string]any
	for {
		again = getJSON[map[string]any](t, client, url)
		if again["lengthsPending"] == false || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if again["lengthsPending"] != false {
		t.Fatalf("still pending: %v", again)
	}
	tracks := again["tracks"].([]any)
	if tracks[0].(map[string]any)["duration"] != float64(100) || tracks[1].(map[string]any)["duration"] != 200.5 {
		t.Errorf("lengths: %v", tracks)
	}
}

// An audiobook's bookmarks are its own: the ebook reader never sees them.
func TestAudiobookMarksAreApartFromTheEbook(t *testing.T) {
	server, base, client, root := audioScanServer(t)
	writeTree(t, map[string]string{filepath.Join(root, "Ann Author", "The Long Road", "1.mp3"): "a"})
	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{"format": "audiobook"}, http.StatusAccepted)
	waitBackground(t, server)
	id := getJSON[[]map[string]any](t, client, base+"/api/books")[0]["id"]
	marks := fmt.Sprintf("%s/api/books/%v/marks", base, id)

	m := postJSON[map[string]any](t, client, marks+"?format=audiobook", map[string]any{"position": "2:93.5", "label": "Chapter 3 · 1:33", "percent": 12.5}, http.StatusCreated)
	if m["item"] != fmt.Sprintf("audio:%v", id) || m["position"] != "2:93.5" || m["kind"] != "bookmark" {
		t.Errorf("saved: %v", m)
	}
	if got := getJSON[[]map[string]any](t, client, marks+"?format=audiobook"); len(got) != 1 {
		t.Errorf("audiobook marks: %v", got)
	}
	if got := getJSON[[]map[string]any](t, client, marks); len(got) != 0 {
		t.Errorf("the ebook's list has it: %v", got)
	}
	postJSON[map[string]any](t, client, marks+"?format=audiobook", map[string]any{"kind": "highlight", "position": "1:1", "excerpt": "words"}, http.StatusBadRequest)
	postJSONMethod[map[string]any](t, client, http.MethodPut, fmt.Sprintf("%s/api/reading-marks/%v", base, m["id"]), map[string]any{"note": "a good bit"}, http.StatusOK)
	if got := getJSON[[]map[string]any](t, client, marks+"?format=audiobook"); got[0]["note"] != "a good bit" {
		t.Errorf("note: %v", got)
	}
}
