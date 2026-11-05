package mediaservers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeCollServer records every call and answers as Jellyfin or Plex would.
type fakeCollServer struct {
	mu    sync.Mutex
	calls []string
	reply map[string]any // "METHOD path" -> JSON answer
	body  map[string]string
}

func newFakeColl(t *testing.T, reply map[string]any) (*fakeCollServer, *httptest.Server) {
	f := &fakeCollServer{reply: reply, body: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("X-Emby-Token") != "tok" && r.Header.Get("X-Plex-Token") != "tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		call := r.Method + " " + r.URL.Path
		if q := r.URL.RawQuery; q != "" {
			call += "?" + q
		}
		f.calls = append(f.calls, call)
		b, _ := io.ReadAll(r.Body)
		f.body[r.Method+" "+r.URL.Path] = string(b)
		ans, ok := f.reply[r.Method+" "+r.URL.Path]
		if !ok {
			if r.Method == http.MethodGet {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ans)
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeCollServer) saw(t *testing.T, want string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == want {
			return
		}
	}
	t.Errorf("no call %q in\n%s", want, strings.Join(f.calls, "\n"))
}

func TestJellyfinCollections(t *testing.T) {
	f, srv := newFakeColl(t, map[string]any{
		"POST /Collections": map[string]any{"Id": "c1"},
		"GET /Items":        map[string]any{"Items": []map[string]any{{"Id": "c1", "Name": "Alien", "Overview": "keep me"}}},
	})
	s := Server{Kind: KindJellyfin, BaseURL: srv.URL, Token: "tok"}
	c := NewClient("test")
	ctx := context.Background()

	id, err := c.JellyfinCreateCollection(ctx, s, "Alien", []string{"a", "b"})
	if err != nil || id != "c1" {
		t.Fatalf("create: %q %v", id, err)
	}
	f.saw(t, "POST /Collections?Ids=a%2Cb&Name=Alien")
	if err := c.JellyfinCollectionAdd(ctx, s, "c1", []string{"x"}); err != nil {
		t.Fatal(err)
	}
	f.saw(t, "POST /Collections/c1/Items?Ids=x")
	if err := c.JellyfinCollectionRemove(ctx, s, "c1", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	f.saw(t, "DELETE /Collections/c1/Items?Ids=a")
	list, err := c.JellyfinCollections(ctx, s)
	if err != nil || len(list) != 1 || list[0].Title != "Alien" {
		t.Fatalf("list: %+v %v", list, err)
	}
	// The sort name goes back with the rest of the item, untouched.
	if err := c.JellyfinCollectionNames(ctx, s, "c1", "Alien", "Alien 1"); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(f.body["POST /Items/c1"]), &sent)
	if sent["ForcedSortName"] != "Alien 1" || sent["Overview"] != "keep me" {
		t.Fatalf("the item sent back: %v", sent)
	}
	if err := c.JellyfinDeleteItem(ctx, s, "c1"); err != nil {
		t.Fatal(err)
	}
	f.saw(t, "DELETE /Items/c1")

	// Many titles go in batches.
	many := make([]string, 200)
	for i := range many {
		many[i] = "i"
	}
	if err := c.JellyfinCollectionAdd(ctx, s, "c2", many); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, "POST /Collections/c2/Items") {
			n++
		}
	}
	if n != 3 {
		t.Errorf("200 titles in %d calls, want 3", n)
	}
}

func TestPlexCollections(t *testing.T) {
	f, srv := newFakeColl(t, map[string]any{
		"GET /identity":                         map[string]any{"MediaContainer": map[string]any{"machineIdentifier": "m1"}},
		"POST /library/collections":             map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{{"ratingKey": "900"}}}},
		"GET /library/sections/1/collections":   map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{{"ratingKey": "900", "title": "Alien"}}}},
		"GET /library/collections/900/children": map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{{"ratingKey": "77"}}}},
	})
	s := Server{Kind: KindPlex, BaseURL: srv.URL, Token: "tok"}
	c := NewClient("test")
	ctx := context.Background()

	key, err := c.PlexCreateCollection(ctx, s, "1", "movie", "Alien", []string{"77", "78"})
	if err != nil || key != "900" {
		t.Fatalf("create: %q %v", key, err)
	}
	f.saw(t, "POST /library/collections?sectionId=1&smart=0&title=Alien&type=1&uri=server%3A%2F%2Fm1%2Fcom.plexapp.plugins.library%2Flibrary%2Fmetadata%2F77%2C78")
	if err := c.PlexCollectionAdd(ctx, s, "900", []string{"79"}); err != nil {
		t.Fatal(err)
	}
	f.saw(t, "PUT /library/collections/900/items?uri=server%3A%2F%2Fm1%2Fcom.plexapp.plugins.library%2Flibrary%2Fmetadata%2F79")
	if err := c.PlexCollectionRemove(ctx, s, "900", "77"); err != nil {
		t.Fatal(err)
	}
	f.saw(t, "DELETE /library/collections/900/items/77")
	items, err := c.PlexCollectionItems(ctx, s, "900")
	if err != nil || len(items) != 1 || items[0] != "77" {
		t.Fatalf("items: %v %v", items, err)
	}
	if _, err := c.PlexCollectionItems(ctx, s, "404"); !IsNotFound(err) {
		t.Errorf("a collection that's gone: %v", err)
	}
	if err := c.PlexCollectionNames(ctx, s, "1", "900", "Alien", "!Alien"); err != nil {
		t.Fatal(err)
	}
	f.saw(t, "PUT /library/sections/1/all?id=900&title.locked=1&title.value=Alien&titleSort.locked=1&titleSort.value=%21Alien&type=18")
	if err := c.PlexCollectionVisibility(ctx, s, "1", "900", true, true, false); err != nil {
		t.Fatal(err)
	}
	f.saw(t, "POST /hubs/sections/1/manage?metadataItemId=900&promotedToOwnHome=1&promotedToRecommended=1&promotedToSharedHome=0")
	if err := c.PlexSetPoster(ctx, s, "900", []byte("jpg"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	f.saw(t, "POST /library/metadata/900/posters")
	if err := c.PlexDeleteCollection(ctx, s, "900"); err != nil {
		t.Fatal(err)
	}
	f.saw(t, "DELETE /library/collections/900")
}

// Plex's tracks: Dolby Vision and Atmos come from the file's streams.
func TestPlexTracks(t *testing.T) {
	_, srv := newFakeColl(t, map[string]any{
		"GET /library/metadata/77": map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{{"Media": []map[string]any{{"Part": []map[string]any{{"Stream": []map[string]any{
			{"streamType": 1, "width": 3840, "height": 1600, "DOVIPresent": true, "colorTrc": "smpte2084", "displayTitle": "4K DoVi/HDR10 (HEVC Main 10)"},
			{"streamType": 2, "codec": "truehd", "channels": 8, "displayTitle": "English (TRUEHD 7.1 Atmos)"},
			{"streamType": 3, "codec": "srt"},
		}}}}}}}}},
	})
	v, audio, err := NewClient("test").PlexTracks(context.Background(), Server{Kind: KindPlex, BaseURL: srv.URL, Token: "tok"}, "77")
	if err != nil {
		t.Fatal(err)
	}
	if v.Width != 3840 || !v.DolbyVision || v.ColorTrc != "smpte2084" {
		t.Errorf("video: %+v", v)
	}
	if len(audio) != 1 || audio[0].Channels != 8 || !strings.Contains(audio[0].Title, "Atmos") {
		t.Errorf("audio: %+v", audio)
	}
}
