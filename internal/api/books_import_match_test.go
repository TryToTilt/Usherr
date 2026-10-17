package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A book the import could not find can be matched by hand: the picked book is
// attached to the file as an automatic match would be, and the file is only
// moved when the person asks for it.
func TestMatchBookImportByHand(t *testing.T) {
	cases := []struct {
		name      string
		audio     bool
		rename    bool
		wantEbook string // path under the ebooks root, ebook cases
		wantAudio string // folder under the audiobooks root, audiobook cases
	}{
		{name: "ebook stays where it is", wantEbook: filepath.Join("Zed Nobody", "Totally Different Name", "weird file name.epub")},
		{name: "ebook renamed", rename: true, wantEbook: filepath.Join("J.R.R. Tolkien", "The Hobbit (1937)", "The Hobbit.epub")},
		{name: "audiobook stays where it is", audio: true, wantAudio: filepath.Join("Zed Nobody", "Totally Different Name")},
		{name: "audiobook renamed", audio: true, rename: true, wantAudio: filepath.Join("J.R.R. Tolkien", "The Hobbit (1937)")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server, base, client := loginNewServer(t)
			server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL) // answers with The Hobbit for anything
			postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true, "audiobooks": true}, http.StatusOK)

			root := filepath.Join(t.TempDir(), "books")
			kind, setting := "ebook", "ebooksPath"
			local := filepath.Join(root, "Zed Nobody", "Totally Different Name")
			file := filepath.Join(local, "weird file name.epub")
			if c.audio {
				kind, setting = "audiobook", "audiobooksPath"
				file = filepath.Join(local, "01.m4b")
			}
			if err := os.MkdirAll(local, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("the book"), 0o644); err != nil {
				t.Fatal(err)
			}
			postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{setting: root}, http.StatusOK)

			pick := map[string]any{"path": file, "kind": kind, "olKey": "/works/OL1W", "title": "The Hobbit", "author": "J.R.R. Tolkien", "authorKey": "OL26320A", "year": 1937, "coverId": 123, "rename": c.rename}
			if c.audio {
				pick["path"] = local // an audiobook is its folder
			}
			postJSON[map[string]any](t, client, base+"/api/books/import/match", pick, http.StatusConflict) // no import yet

			postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{}, http.StatusAccepted)
			waitBackground(t, server)
			st := getJSON[map[string]any](t, client, base+"/api/books/import")
			if st["summary"].(map[string]any)["unmatched"] != float64(1) {
				t.Fatalf("the import should not find it: %v", st)
			}

			bad := map[string]any{"path": pick["path"], "kind": kind, "title": "The Hobbit"} // no book picked
			postJSON[map[string]any](t, client, base+"/api/books/import/match", bad, http.StatusBadRequest)
			other := map[string]any{"path": filepath.Join(root, "not in the list"), "kind": kind, "olKey": "OL1W", "title": "The Hobbit"}
			postJSON[map[string]any](t, client, base+"/api/books/import/match", other, http.StatusNotFound)

			st = postJSON[map[string]any](t, client, base+"/api/books/import/match", pick, http.StatusOK)
			sum := st["summary"].(map[string]any)
			if sum["imported"] != float64(1) || sum["unmatched"] != nil {
				t.Fatalf("after the match: %v", st)
			}
			res := st["results"].([]any)[0].(map[string]any)
			if res["matched"] != "The Hobbit" || res["bookId"] == nil || res["message"] != nil {
				t.Fatalf("result: %v", res)
			}

			want := c.wantEbook
			if c.audio {
				want = c.wantAudio
			}
			want = filepath.Join(root, want)
			if res["path"] != want {
				t.Fatalf("recorded path %v, want %s", res["path"], want)
			}
			if _, err := os.Stat(want); err != nil {
				t.Fatalf("file not at %s: %v", want, err)
			}
			if c.rename {
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Fatalf("the old file should be gone: %v", err)
				}
			}
			list := getJSON[[]map[string]any](t, client, base+"/api/books")
			if len(list) != 1 || list[0]["title"] != "The Hobbit" {
				t.Fatalf("library: %v", list)
			}
			f := list[0][kind].(map[string]any)
			if f["status"] != "downloaded" || f["path"] != want {
				t.Fatalf("%s in the library: %v", kind, f)
			}

			// It is found now: picking again is refused.
			pick["path"] = want
			postJSON[map[string]any](t, client, base+"/api/books/import/match", pick, http.StatusConflict)
		})
	}
}

// When the renamed place is taken, the book is still added and the file stays.
func TestMatchBookImportRenameBlocked(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	root := filepath.Join(t.TempDir(), "books")
	local := filepath.Join(root, "Zed Nobody", "Totally Different Name")
	file := filepath.Join(local, "weird file name.epub")
	taken := filepath.Join(root, "J.R.R. Tolkien", "The Hobbit (1937)", "The Hobbit.epub")
	writeFile := func(p string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(file)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"ebooksPath": root}, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{}, http.StatusAccepted)
	waitBackground(t, server)
	writeFile(taken) // something is in the way by the time the person picks

	pick := map[string]any{"path": file, "kind": "ebook", "olKey": "OL1W", "title": "The Hobbit", "author": "J.R.R. Tolkien", "year": 1937, "rename": true}
	st := postJSON[map[string]any](t, client, base+"/api/books/import/match", pick, http.StatusOK)
	for _, r := range st["results"].([]any) {
		res := r.(map[string]any)
		if res["path"] == file && res["status"] == "imported" && res["message"] != nil {
			if _, err := os.Stat(file); err != nil {
				t.Fatalf("the file should not have moved: %v", err)
			}
			return
		}
	}
	t.Fatalf("expected the book added with a note and the file left in place: %v", st)
}

// Two requests for the same file at once (a double click) match it once: the
// book ends up pointing at a file that is there.
func TestMatchBookImportTwiceAtOnce(t *testing.T) {
	server, base, client := loginNewServer(t)
	server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	root := filepath.Join(t.TempDir(), "books")
	local := filepath.Join(root, "Zed Nobody", "Totally Different Name")
	file := filepath.Join(local, "weird file name.epub")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("the book"), 0o644); err != nil {
		t.Fatal(err)
	}
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/modules", map[string]any{"ebooks": true}, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, base+"/api/settings", map[string]any{"ebooksPath": root}, http.StatusOK)
	postJSON[map[string]any](t, client, base+"/api/books/import", map[string]any{}, http.StatusAccepted)
	waitBackground(t, server)

	// Open Library answers slowly from here on, so the requests overlap.
	fast := newFakeOpenLibrary(t)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		resp, err := http.Get(fast.URL + r.URL.RequestURI())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(slow.Close)
	server.TestSetOpenLibrary(slow.URL)

	pick := map[string]any{"path": file, "kind": "ebook", "olKey": "OL1W", "title": "The Hobbit", "author": "J.R.R. Tolkien", "year": 1937, "rename": true}
	body, _ := json.Marshal(pick)
	const n = 6
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := client.Post(base+"/api/books/import/match", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			codes[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusConflict, http.StatusNotFound: // refused, or the file has left the list under its old path already
		default:
			t.Errorf("unexpected status %d in %v", c, codes)
		}
	}
	if ok != 1 {
		t.Errorf("%d requests matched the file, want 1 (%v)", ok, codes)
	}
	list := getJSON[[]map[string]any](t, client, base+"/api/books")
	if len(list) != 1 {
		t.Fatalf("library: %v", list)
	}
	path, _ := list[0]["ebook"].(map[string]any)["path"].(string)
	if _, err := os.Stat(path); path == "" || err != nil {
		t.Errorf("the book points at %q, which is not there: %v", path, err)
	}
}
