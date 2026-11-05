package api

import (
	"context"
	"encoding/json"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

// fakeCollJF is a Jellyfin with a kept and a streamed library; it keeps its
// collections and what's in them.
type fakeCollJF struct {
	mu      sync.Mutex
	cols    map[string][]string // id -> item ids
	names   map[string]string
	calls   []string
	nextCol int
}

func (f *fakeCollJF) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
	js := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	q := r.URL.Query()
	movie := func(id string, tmdb int) map[string]any {
		return map[string]any{"Id": id, "Type": "Movie", "Name": "Film " + strconv.Itoa(tmdb), "ProductionYear": 2000 + tmdb, "ProviderIds": map[string]string{"Tmdb": strconv.Itoa(tmdb)}}
	}
	switch {
	case r.URL.Path == "/Library/VirtualFolders":
		js([]map[string]any{{"Name": "Movies", "ItemId": "lib1", "CollectionType": "movies"}, {"Name": "Movies-Stream", "ItemId": "lib2", "CollectionType": "movies"}})
	case r.URL.Path == "/Items" && q.Get("ParentId") == "lib1":
		js(map[string]any{"TotalRecordCount": 2, "Items": []map[string]any{movie("m1", 1), movie("m2", 2)}})
	case r.URL.Path == "/Items" && q.Get("ParentId") == "lib2":
		js(map[string]any{"TotalRecordCount": 1, "Items": []map[string]any{movie("s1", 1)}})
	case r.URL.Path == "/Items" && q.Get("IncludeItemTypes") == "BoxSet":
		var out []map[string]any
		for id, n := range f.names {
			out = append(out, map[string]any{"Id": id, "Name": n})
		}
		js(map[string]any{"Items": out})
	case r.URL.Path == "/Items" && q.Get("ParentId") != "":
		ids, ok := f.cols[q.Get("ParentId")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var out []map[string]any
		for _, id := range ids {
			out = append(out, map[string]any{"Id": id})
		}
		js(map[string]any{"Items": out})
	case r.URL.Path == "/Collections" && r.Method == http.MethodPost:
		f.nextCol++
		id := "c" + strconv.Itoa(f.nextCol)
		f.cols[id] = strings.Split(q.Get("Ids"), ",")
		f.names[id] = q.Get("Name")
		js(map[string]any{"Id": id})
	case strings.HasPrefix(r.URL.Path, "/Collections/") && strings.HasSuffix(r.URL.Path, "/Items"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/Collections/"), "/Items")
		ids := strings.Split(q.Get("Ids"), ",")
		if r.Method == http.MethodPost {
			f.cols[id] = append(f.cols[id], ids...)
		} else {
			var keep []string
			for _, m := range f.cols[id] {
				if !containsStr(ids, m) {
					keep = append(keep, m)
				}
			}
			f.cols[id] = keep
		}
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(r.URL.Path, "/Images/Primary/0") && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(r.URL.Path, "/Items/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(r.URL.Path, "/Items/")
		delete(f.cols, id)
		delete(f.names, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeCollJF) members(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.cols[id]...)
	sort.Strings(out)
	return out
}

// fakeCollPlex is a Plex with one film library.
type fakeCollPlex struct {
	mu    sync.Mutex
	cols  map[string][]string
	calls []string
}

func (f *fakeCollPlex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("X-Plex-Token") != "tok" {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	f.calls = append(f.calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
	js := func(c map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"MediaContainer": c})
	}
	keysOf := func(uri string) []string {
		return strings.Split(uri[strings.LastIndex(uri, "/")+1:], ",")
	}
	switch {
	case r.URL.Path == "/identity":
		js(map[string]any{"machineIdentifier": "m1"})
	case r.URL.Path == "/library/sections":
		js(map[string]any{"Directory": []map[string]any{{"key": "1", "type": "movie", "title": "Movies"}}})
	case r.URL.Path == "/library/sections/1/all" && r.Method == http.MethodGet:
		js(map[string]any{"Metadata": []map[string]any{
			{"ratingKey": "77", "type": "movie", "title": "Film 1", "Guid": []map[string]any{{"id": "tmdb://1"}}},
			{"ratingKey": "78", "type": "movie", "title": "Film 3", "Guid": []map[string]any{{"id": "tmdb://3"}}},
		}})
	case r.URL.Path == "/library/sections/1/collections":
		var out []map[string]any
		for id := range f.cols {
			out = append(out, map[string]any{"ratingKey": id, "title": "Old name"})
		}
		js(map[string]any{"Metadata": out})
	case r.URL.Path == "/library/collections" && r.Method == http.MethodPost:
		f.cols["900"] = keysOf(r.URL.Query().Get("uri"))
		js(map[string]any{"Metadata": []map[string]any{{"ratingKey": "900"}}})
	case strings.HasPrefix(r.URL.Path, "/library/collections/900"):
		rest := strings.TrimPrefix(r.URL.Path, "/library/collections/900")
		switch {
		case rest == "/children":
			ids, ok := f.cols["900"]
			if !ok {
				http.NotFound(w, r)
				return
			}
			var out []map[string]any
			for _, id := range ids {
				out = append(out, map[string]any{"ratingKey": id})
			}
			js(map[string]any{"Metadata": out})
		case rest == "/items" && r.Method == http.MethodPut:
			f.cols["900"] = append(f.cols["900"], keysOf(r.URL.Query().Get("uri"))...)
		case strings.HasPrefix(rest, "/items/") && r.Method == http.MethodDelete:
			item := strings.TrimPrefix(rest, "/items/")
			var keep []string
			for _, m := range f.cols["900"] {
				if m != item {
					keep = append(keep, m)
				}
			}
			f.cols["900"] = keep
		case rest == "" && r.Method == http.MethodDelete:
			delete(f.cols, "900")
		}
	case strings.HasPrefix(r.URL.Path, "/hubs/sections/1/manage") && r.Method == http.MethodGet:
		http.NotFound(w, r)
	default:
		// sort titles, hubs: fine
	}
}

func (f *fakeCollPlex) members() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.cols["900"]...)
	sort.Strings(out)
	return out
}

func collTestServer(t *testing.T) (*Server, *fakeCollJF, *fakeCollPlex) {
	t.Helper()
	s, db := optionsServer(t)
	s.db = db
	box, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	s.MediaServers = mediaservers.NewRepo(db, box)
	s.mediaClient = mediaservers.NewClient("test")
	_ = s.Settings.Set(keyArtFolder, t.TempDir(), false)
	fj := &fakeCollJF{cols: map[string][]string{}, names: map[string]string{}}
	fp := &fakeCollPlex{cols: map[string][]string{}}
	js, ps := httptest.NewServer(fj), httptest.NewServer(fp)
	t.Cleanup(js.Close)
	t.Cleanup(ps.Close)
	if _, err := s.MediaServers.Create(mediaservers.Server{Name: "Jellyfin", Kind: mediaservers.KindJellyfin, BaseURL: js.URL, Token: "tok", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MediaServers.Create(mediaservers.Server{Name: "Plex", Kind: mediaservers.KindPlex, BaseURL: ps.URL, Token: "tok", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	libItemsForget()
	t.Cleanup(libItemsForget)
	return s, fj, fp
}

func pick(ids ...int) []collPick {
	var out []collPick
	for _, id := range ids {
		out = append(out, collPick{Kind: "movie", TMDBID: id, Title: "Film " + strconv.Itoa(id)})
	}
	return out
}

// A collection goes to Jellyfin (one, the kept copy rather than the
// streamed one) and to Plex's library; titles that no longer belong come
// out; deleting it with "from the servers too" deletes it there.
func TestPushCollectionsToJellyfinAndPlex(t *testing.T) {
	s, fj, fp := collTestServer(t)
	ctx := context.Background()
	in := collInput{Name: "Favourites", Source: "picked", Servers: []string{"jellyfin", "plex"}, Config: collConfig{Picked: pick(1, 2, 3, 4)}}
	c, problem := s.collCheck(ctx, in)
	if problem != "" {
		t.Fatal(problem)
	}
	id, err := s.collSaveRow(ctx, c, 0)
	if err != nil {
		t.Fatal(err)
	}
	s.collSync(ctx, id, true)
	if got := fj.members("c1"); !reflect.DeepEqual(got, []string{"m1", "m2"}) {
		t.Fatalf("Jellyfin's collection: %v (want the kept copy of film 1, and film 2)", got)
	}
	if fj.names["c1"] != "Favourites" {
		t.Errorf("Jellyfin's collection is named %q", fj.names["c1"])
	}
	if got := fp.members(); !reflect.DeepEqual(got, []string{"77", "78"}) {
		t.Fatalf("Plex's collection: %v", got)
	}
	got, err := s.collGet(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastStatus != "ok" || got.LastFound != 4 || got.LastMatched != 3 || !reflect.DeepEqual(got.LastMissing, []string{"Film 4"}) {
		t.Fatalf("the sync: %+v", got)
	}
	if len(got.Targets) != 2 {
		t.Errorf("where it is: %v", got.Targets)
	}

	// Film 1 leaves the collection; film 3 stays.
	c.Config.Picked = pick(2, 3)
	if _, err := s.collSaveRow(ctx, c, id); err != nil {
		t.Fatal(err)
	}
	s.collSync(ctx, id, false)
	if got := fj.members("c1"); !reflect.DeepEqual(got, []string{"m2"}) {
		t.Errorf("Jellyfin after: %v", got)
	}
	if got := fp.members(); !reflect.DeepEqual(got, []string{"78"}) {
		t.Errorf("Plex after: %v", got)
	}

	// Without "take out what no longer matches", nothing comes out.
	c.Config.Picked, c.RemoveMissing = pick(1), false
	_, _ = s.collSaveRow(ctx, c, id)
	s.collSync(ctx, id, false)
	if got := fj.members("c1"); !reflect.DeepEqual(got, []string{"m1", "m2"}) {
		t.Errorf("Jellyfin kept-everything: %v", got)
	}

	// Deleted on Jellyfin by hand: made again on the next sync.
	fj.mu.Lock()
	delete(fj.cols, "c1")
	delete(fj.names, "c1")
	fj.mu.Unlock()
	s.collSync(ctx, id, false)
	if got := fj.members("c2"); !reflect.DeepEqual(got, []string{"m1"}) {
		t.Errorf("made again: %v", got)
	}

	// Deleted in Usherr and on the servers.
	req := httptest.NewRequest(http.MethodDelete, "/api/home/collections/"+strconv.FormatInt(id, 10)+"?servers=1", nil)
	req.SetPathValue("id", strconv.FormatInt(id, 10))
	rec := httptest.NewRecorder()
	s.handleDeletePushCollection(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if len(fj.members("c2")) != 0 || len(fp.members()) != 0 {
		t.Error("the collection is still on a server")
	}
	if _, ok := fp.cols["900"]; ok {
		t.Error("Plex's collection wasn't deleted")
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM server_collection_links`).Scan(&n)
	if n != 0 {
		t.Errorf("%d links left", n)
	}
}

// A collection of the same name already on the server is taken over.
func TestPushCollectionTakesOverExisting(t *testing.T) {
	s, fj, _ := collTestServer(t)
	fj.cols["k9"], fj.names["k9"] = []string{"m2"}, "Favourites"
	ctx := context.Background()
	c, _ := s.collCheck(ctx, collInput{Name: "Favourites", Source: "picked", Servers: []string{"jellyfin"}, Config: collConfig{Picked: pick(1)}})
	id, _ := s.collSaveRow(ctx, c, 0)
	s.collSync(ctx, id, true)
	if got := fj.members("k9"); !reflect.DeepEqual(got, []string{"m1"}) {
		t.Fatalf("the existing collection: %v", got)
	}
	if fj.nextCol != 0 {
		t.Error("a second collection was made")
	}

	// Its own poster goes once (and again only when it changes).
	if err := os.MkdirAll(filepath.Dir(s.collUploadFile(id)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.collUploadFile(id), pngOf(t, 20, 30, color.RGBA{R: 255, A: 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	c.Poster = "upload"
	_, _ = s.collSaveRow(ctx, c, id)
	posts := func() int {
		fj.mu.Lock()
		defer fj.mu.Unlock()
		n := 0
		for _, call := range fj.calls {
			if strings.HasPrefix(call, "POST /Items/k9/Images/Primary/0") {
				n++
			}
		}
		return n
	}
	s.collSync(ctx, id, false)
	s.collSync(ctx, id, false)
	if n := posts(); n != 1 {
		t.Errorf("the poster was sent %d times, want once", n)
	}
}

func TestCollectionRules(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	items := []libItem{
		{Copy: "a", Title: "Alien", Year: 1979, Genres: []string{"Horror", "Science Fiction"}, Rating: 8.1, Added: now.AddDate(0, 0, -100)},
		{Copy: "b", Title: "The Thing", Year: 1982, Genres: []string{"Horror"}, Rating: 8.2, Added: now.AddDate(0, 0, -3)},
		{Copy: "c", Title: "Heat", Year: 1995, Genres: []string{"Crime"}, Rating: 7.9, Added: now.AddDate(0, 0, -5)},
		{Copy: "d", Title: "Scream", Year: 1996, Genres: []string{"horror"}, Rating: 6.9},
	}
	names := func(list []libItem) string {
		var out []string
		for _, it := range list {
			out = append(out, it.Copy)
		}
		return strings.Join(out, ",")
	}
	if got := names(collRule(collConfig{Genres: []string{"Horror"}, YearFrom: 1980, YearTo: 1989}, items, now)); got != "b" {
		t.Errorf("horror of the 80s: %s", got)
	}
	if got := names(collRule(collConfig{AddedDays: 30}, items, now)); got != "b,c" {
		t.Errorf("added lately: %s", got)
	}
	if got := names(collRule(collConfig{Count: 2}, items, now)); got != "b,a" {
		t.Errorf("the two best rated: %s", got)
	}
	if got := names(collRule(collConfig{Genres: []string{"HORROR"}, MinRating: 7}, items, now)); got != "a,b" {
		t.Errorf("horror rated 7 and up: %s", got)
	}
	// Matching by IMDb id and by TheTVDB id when there's no TMDB id.
	lib := []libItem{{Copy: "x", Kind: "movie", IMDBID: "tt0078748"}, {Copy: "y", Kind: "tv", TVDBID: 81189}}
	found := map[int]bool{}
	got := collMatch([]collWant{{IMDBID: "TT0078748"}, {TVDBID: 81189}, {Kind: "movie", TMDBID: 5}}, lib, found)
	if names(got) != "x,y" || !found[0] || !found[1] || found[2] {
		t.Errorf("matching: %s %v", names(got), found)
	}
}

func TestCollectionCheck(t *testing.T) {
	s, _, _ := collTestServer(t)
	ctx := context.Background()
	for _, c := range []struct {
		in   collInput
		want string
	}{
		{collInput{Source: "picked", Servers: []string{"jellyfin"}}, "Give the collection a name."},
		{collInput{Name: "x", Source: "nope", Servers: []string{"jellyfin"}}, "Choose where the collection's titles come from."},
		{collInput{Name: "x", Source: "rule", Servers: []string{"emby"}}, "Choose Jellyfin, Plex or both."},
		{collInput{Name: "x", Source: "rule", Servers: []string{"plex"}}, "Set at least one rule: a genre, years, a rating, how recently added, or the best rated few."},
		{collInput{Name: "x", Source: "list", Servers: []string{"plex"}, Config: collConfig{ListID: 99}}, "That import list isn't there any more."},
		{collInput{Name: "x", Source: "picked", Servers: []string{"plex"}}, "Pick at least one title."},
	} {
		if _, got := s.collCheck(ctx, c.in); got != c.want {
			t.Errorf("%+v: %q, want %q", c.in, got, c.want)
		}
	}
	c, problem := s.collCheck(ctx, collInput{Name: "Recent", Source: "rule", Servers: []string{"plex", "plex"}, Visibility: []string{"home", "bogus", "library"}, Config: collConfig{AddedDays: 30, YearFrom: 2020, YearTo: 1990}})
	if problem != "" || !reflect.DeepEqual(c.Servers, []string{"plex"}) || !reflect.DeepEqual(c.Visibility, []string{"home", "library"}) || c.Config.YearFrom != 1990 || !c.OnlyLibrary || !c.RemoveMissing {
		t.Errorf("tidied: %+v %q", c, problem)
	}
}
