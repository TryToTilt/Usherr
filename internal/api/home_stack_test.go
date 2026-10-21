package api_test

import (
	"net/http"
	"testing"
)

// The Stack: titles go on top, can be put in another order, come off again,
// and a title on it twice is still one.
func TestStack(t *testing.T) {
	_, base, admin := loginNewServer(t)
	list := func() []any {
		return getJSON[map[string]any](t, admin, base+"/api/home/stack")["items"].([]any)
	}
	if n := len(list()); n != 0 {
		t.Fatalf("%d titles to start", n)
	}
	add := func(body map[string]any) int {
		st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/stack", body)
		return st
	}
	for _, it := range []map[string]any{
		{"kind": "movie", "tmdbId": 603, "title": "The Matrix", "year": 1999, "posterUrl": "https://image.tmdb.org/t/p/w500/m.jpg"},
		{"kind": "tv", "tmdbId": 1396, "title": "Breaking Bad", "year": 2008},
		{"kind": "movie", "tmdbId": 603, "title": "The Matrix"},
	} {
		if st := add(it); st != http.StatusOK {
			t.Fatalf("add %v: %d", it, st)
		}
	}
	if add(map[string]any{"kind": "book", "tmdbId": 1}) != http.StatusBadRequest || add(map[string]any{"kind": "movie"}) != http.StatusBadRequest {
		t.Fatal("not a film or show, but added")
	}
	items := list()
	if len(items) != 2 || items[0].(map[string]any)["title"] != "Breaking Bad" || items[1].(map[string]any)["posterUrl"] != "https://image.tmdb.org/t/p/w500/m.jpg" {
		t.Fatalf("stack: %v", items)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/stack/order", map[string]any{"order": []string{"movie:603", "tv:1396"}}); st != http.StatusOK {
		t.Fatalf("order: %d", st)
	}
	if items = list(); items[0].(map[string]any)["tmdbId"] != float64(603) {
		t.Fatalf("not reordered: %v", items)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/stack/movie/603", nil); st != http.StatusOK {
		t.Fatalf("remove: %d", st)
	}
	if items = list(); len(items) != 1 || items[0].(map[string]any)["kind"] != "tv" {
		t.Fatalf("after remove: %v", items)
	}
}
