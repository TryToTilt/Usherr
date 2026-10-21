package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/library"
)

// addonGet reads an add-on address with no sign-in, the way the apps do.
func addonGet(t *testing.T, url string) (int, http.Header, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return resp.StatusCode, resp.Header, out
}

func TestStremioNuvioAddon(t *testing.T) {
	server, base, admin := loginNewServer(t)

	// The person's address is made on first use and shown with a QR code.
	info := getJSON[map[string]any](t, admin, base+"/api/addon")
	key, _ := info["key"].(string)
	if len(key) != 32 || !strings.HasSuffix(info["manifestUrl"].(string), "/addon/"+key+"/manifest.json") {
		t.Fatalf("addon info: %v", info)
	}
	if rows, _ := info["qr"].([]any); len(rows) < 21 {
		t.Fatalf("no QR code: %v", info["qr"])
	}
	if again := getJSON[map[string]any](t, admin, base+"/api/addon"); again["key"] != key {
		t.Fatal("the key changed by itself")
	}
	if st, _ := doJSONStatus(t, admin, http.MethodGet, base+"/api/addon?base=ftp://nope", nil); st != http.StatusBadRequest {
		t.Fatalf("a bad address answered %d", st)
	}
	if https := getJSON[map[string]any](t, admin, base+"/api/addon?base=https://usherr.example.com")["stremioUrl"]; https != "stremio://usherr.example.com/addon/"+key+"/manifest.json" {
		t.Fatalf("stremio link: %v", https)
	}

	// The manifest: open to whoever has the key, with CORS for the web apps.
	root := base + "/addon/" + key
	st, hdr, manifest := addonGet(t, root+"/manifest.json")
	if st != http.StatusOK || hdr.Get("Access-Control-Allow-Origin") != "*" || manifest["id"] != "com.usherr.addon" {
		t.Fatalf("manifest: %d %v %v", st, hdr, manifest)
	}
	if cats, _ := manifest["catalogs"].([]any); len(cats) != 5 {
		t.Fatalf("catalogs: %v", manifest["catalogs"])
	}
	if st, _, _ := addonGet(t, base+"/addon/"+strings.Repeat("0", 32)+"/manifest.json"); st != http.StatusNotFound {
		t.Fatalf("a wrong key answered %d", st)
	}
	if st, _, _ := addonGet(t, base+"/addon/short/manifest.json"); st != http.StatusNotFound {
		t.Fatalf("a short key answered %d", st)
	}
	req, _ := http.NewRequest(http.MethodOptions, root+"/catalog/movie/usherr-movies.json", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight: %v %v", resp, err)
	}

	// Catalogs: a library movie, found by search too; an empty one is still a list.
	m, err := server.MovieRepo.Add(library.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, "Bluray-1080p", "/movies/The Matrix (1999)/The Matrix.mkv"); err != nil {
		t.Fatal(err)
	}
	_, _, cat := addonGet(t, root+"/catalog/movie/usherr-movies.json")
	metas, _ := cat["metas"].([]any)
	if len(metas) != 1 || metas[0].(map[string]any)["id"] != "usherr:tmdb:movie:603" || metas[0].(map[string]any)["name"] != "The Matrix" {
		t.Fatalf("movies: %v", cat)
	}
	if _, _, cat = addonGet(t, root+"/catalog/movie/usherr-movies/search=matri.json"); len(cat["metas"].([]any)) != 1 {
		t.Fatalf("search: %v", cat)
	}
	if _, _, cat = addonGet(t, root+"/catalog/movie/usherr-movies/search=zzz.json"); len(cat["metas"].([]any)) != 0 {
		t.Fatalf("search found %v", cat)
	}
	if _, _, cat = addonGet(t, root+"/catalog/movie/usherr-movies/skip=100.json"); len(cat["metas"].([]any)) != 0 {
		t.Fatalf("a page past the end: %v", cat)
	}
	for _, id := range []string{"series/usherr-tv", "movie/usherr-watchlist-movies", "series/usherr-watchlist-tv", "tv/usherr-live"} {
		if st, _, c := addonGet(t, root+"/catalog/"+id+".json"); st != http.StatusOK || c["metas"] == nil {
			t.Fatalf("%s: %d %v", id, st, c)
		}
	}
	if st, _, _ := addonGet(t, root+"/catalog/movie/nope.json"); st != http.StatusNotFound {
		t.Fatalf("an unknown catalog answered %d", st)
	}

	// Streams: the library's file first, by the id the catalog gave.
	_, _, ss := addonGet(t, root+"/stream/movie/usherr:tmdb:movie:603.json")
	list, _ := ss["streams"].([]any)
	if len(list) != 1 || !strings.HasSuffix(list[0].(map[string]any)["url"].(string), "/addon/"+key+"/file/movie/"+strconv.FormatInt(m.ID, 10)) {
		t.Fatalf("streams: %v", ss)
	}
	// A Live TV channel plays through the tuner's address.
	_, _, ss = addonGet(t, root+"/stream/tv/usherr:tv:9.json")
	if list, _ = ss["streams"].([]any); len(list) != 1 || !strings.Contains(list[0].(map[string]any)["url"].(string), "/api/tvfeed/") {
		t.Fatalf("channel streams: %v", ss)
	}
	// Something that isn't an id the add-on knows gets an empty list, not an error.
	if st, _, ss := addonGet(t, root+"/stream/movie/whatever.json"); st != http.StatusOK || len(ss["streams"].([]any)) != 0 {
		t.Fatalf("unknown id: %d %v", st, ss)
	}
	// A stream that has expired says so.
	if st, _, _ := addonGet(t, root+"/play/ao_nothere"); st != http.StatusNotFound {
		t.Fatalf("an expired stream answered %d", st)
	}
	// The file route is closed to a wrong key.
	if st, _, _ := addonGet(t, base+"/addon/"+strings.Repeat("1", 32)+"/file/movie/"+strconv.FormatInt(m.ID, 10)); st != http.StatusNotFound {
		t.Fatalf("file with a wrong key answered %d", st)
	}

	// A new key stops the old address.
	newInfo := postJSON[map[string]any](t, admin, base+"/api/addon/regenerate", nil, http.StatusOK)
	if newInfo["key"] == key || len(newInfo["key"].(string)) != 32 {
		t.Fatalf("regenerate: %v", newInfo)
	}
	if st, _, _ := addonGet(t, root+"/manifest.json"); st != http.StatusNotFound {
		t.Fatalf("the old key still works: %d", st)
	}
	if st, _, _ := addonGet(t, base+"/addon/"+newInfo["key"].(string)+"/manifest.json"); st != http.StatusOK {
		t.Fatalf("the new key answered %d", st)
	}
}
