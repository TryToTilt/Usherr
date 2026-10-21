package indexers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// knabenReply is shaped like api.knaben.org/v1 (checked against the real
// service): hits with a main and a sub category, a magnet link, a hash, a
// size in bytes and seeders/peers.
const knabenReply = `{"hits":[
{"title":"Night Harbour 2024 1080p WEB-DL x264","hash":"DAFC8C076CA2F3ED376EEAE7C76A0D6BE2415C45","link":null,"magnetUrl":"magnet:?xt=urn:btih:DAFC8C076CA2F3ED376EEAE7C76A0D6BE2415C45&dn=Night.Harbour","bytes":2147483648,"seeders":140,"peers":25,"date":"2026-04-25T14:35:00+00:00","categoryId":[3000000,3001000],"details":"https://example.org/torrent/1","tracker":"Example"},
{"title":"Quiet Street S02E03 720p HDTV","hash":"0123456789ABCDEF0123456789ABCDEF01234567","magnetUrl":null,"bytes":734003200,"seeders":7,"peers":2,"date":"2024-09-07T22:00:00+0000","categoryId":[2000000,2001000]},
{"title":"Some Odd Thing","hash":"1111111111111111111111111111111111111111","magnetUrl":"magnet:?xt=urn:btih:1111111111111111111111111111111111111111","bytes":10,"seeders":1,"peers":0,"categoryId":[99000000]}
]}`

type knabenSite struct {
	*httptest.Server
	body   atomic.Value // last request body
	method atomic.Value
	ctype  atomic.Value
}

func newKnabenSite(t *testing.T, status int, reply func(w http.ResponseWriter)) *knabenSite {
	t.Helper()
	s := &knabenSite{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		s.body.Store(string(b))
		s.method.Store(r.Method)
		s.ctype.Store(r.Header.Get("Content-Type"))
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		reply(w)
	}))
	t.Cleanup(s.Close)
	return s
}

func knabenInstance(t *testing.T, site *knabenSite) (*CardigannManager, Instance) {
	t.Helper()
	m := fixtureManager(t, "fixturepublic")
	return m, Instance{ID: 31, Name: "Knaben", Kind: KindCardigann, DefinitionID: "knaben", BaseURL: site.URL + "/", Protocol: ProtocolTorrent, Cardigann: m}
}

func hasCat(list []int, c int) bool {
	for _, x := range list {
		if x == c {
			return true
		}
	}
	return false
}

func TestKnabenIsBuiltIn(t *testing.T) {
	data, ok := builtinDefinitions()["knaben"]
	if !ok {
		t.Fatal("knaben is not shipped")
	}
	def, err := ParseDefinition(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := def.Validate(); err != nil {
		t.Fatal(err)
	}
	if sum := summarize(def); !sum.Supported {
		t.Fatalf("not supported: %s", sum.Problem)
	}
	if def.Type != "public" {
		t.Errorf("type = %q, want public", def.Type)
	}
}

func TestKnabenSearchRequestAndMapping(t *testing.T) {
	site := newKnabenSite(t, 0, func(w http.ResponseWriter) { io.WriteString(w, knabenReply) })
	m, inst := knabenInstance(t, site)

	results, err := m.Search(context.Background(), inst, `Night "Harbour" \ 2024`, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if site.method.Load() != "POST" || !strings.HasPrefix(site.ctype.Load().(string), "application/json") {
		t.Errorf("request was %v with %v", site.method.Load(), site.ctype.Load())
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(site.body.Load().(string)), &req); err != nil {
		t.Fatalf("the request body is not JSON: %v: %s", err, site.body.Load())
	}
	want := map[string]any{
		"search_type": "100%", "search_field": "title", "query": `Night "Harbour" \ 2024`,
		"order_by": "seeders", "order_direction": "desc", "from": float64(0), "size": float64(100),
		"hide_unsafe": true, "hide_xxx": true,
	}
	for k, v := range want {
		if req[k] != v {
			t.Errorf("%s = %#v, want %#v", k, req[k], v)
		}
	}

	if len(results) != 3 {
		t.Fatalf("got %d results: %+v", len(results), results)
	}
	r := results[0]
	if r.Title != "Night Harbour 2024 1080p WEB-DL x264" || r.SizeBytes != 2147483648 || r.Seeders != 140 || r.Peers != 165 || !r.SeedersKnown {
		t.Errorf("first result wrong: %+v", r)
	}
	if !strings.HasPrefix(r.DownloadURL, "magnet:?xt=urn:btih:DAFC") {
		t.Errorf("download = %q", r.DownloadURL)
	}
	if r.InfoHash != "dafc8c076ca2f3ed376eeae7c76a0d6be2415c45" {
		t.Errorf("hash = %q", r.InfoHash)
	}
	if r.InfoURL != "https://example.org/torrent/1" {
		t.Errorf("info url = %q", r.InfoURL)
	}
	if r.PublishDate.IsZero() || r.PublishDate.Year() != 2026 {
		t.Errorf("date = %v", r.PublishDate)
	}
	if !hasCat(r.Categories, 2000) || !hasCat(r.Categories, 2040) {
		t.Errorf("movie categories = %v, want 2000 and 2040", r.Categories)
	}
	// no magnet link: built from the hash; the +0000 date style is read too
	tv := results[1]
	if !strings.HasPrefix(tv.DownloadURL, "magnet:?xt=urn:btih:0123456789ABCDEF") || tv.PublishDate.Year() != 2024 {
		t.Errorf("tv result wrong: %+v", tv)
	}
	if !hasCat(tv.Categories, 5000) || !hasCat(tv.Categories, 5040) {
		t.Errorf("tv categories = %v, want 5000 and 5040", tv.Categories)
	}
	if len(results[2].Categories) != 0 {
		t.Errorf("an unknown category stays uncategorised, got %v", results[2].Categories)
	}
}

func TestKnabenCategoryFilter(t *testing.T) {
	site := newKnabenSite(t, 0, func(w http.ResponseWriter) { io.WriteString(w, knabenReply) })
	m, inst := knabenInstance(t, site)
	results, err := m.Search(context.Background(), inst, "Night Harbour", []int{5000})
	if err != nil {
		t.Fatal(err)
	}
	// the TV hit stays, the movie goes, the uncategorised one is kept
	if len(results) != 2 || results[0].Title != "Quiet Street S02E03 720p HDTV" {
		t.Fatalf("got %+v", results)
	}
}

func TestKnabenEmptyResult(t *testing.T) {
	site := newKnabenSite(t, 0, func(w http.ResponseWriter) {
		io.WriteString(w, `{"hits":[],"max_score":null,"total":{"relation":"eq","value":0}}`)
	})
	m, inst := knabenInstance(t, site)
	results, err := m.Search(context.Background(), inst, "nothing here", nil)
	if err != nil || len(results) != 0 {
		t.Fatalf("results = %v, err = %v", results, err)
	}
	// an empty site is still a working one
	res, err := m.Test(context.Background(), inst)
	if err != nil || res.Releases != 0 {
		t.Fatalf("test: %+v, %v", res, err)
	}
}

func TestKnabenTestUsesASmallSearch(t *testing.T) {
	site := newKnabenSite(t, 0, func(w http.ResponseWriter) { io.WriteString(w, knabenReply) })
	m, inst := knabenInstance(t, site)
	res, err := m.Test(context.Background(), inst)
	if err != nil || res.Releases == 0 {
		t.Fatalf("test: %+v, %v", res, err)
	}
}

func TestKnabenErrorStatus(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusBadGateway} {
		site := newKnabenSite(t, status, nil)
		m, inst := knabenInstance(t, site)
		_, err := m.Search(context.Background(), inst, "anything", nil)
		if err == nil || !strings.Contains(err.Error(), "HTTP") {
			t.Errorf("status %d: err = %v", status, err)
		}
	}
}

func TestKnabenNotJSON(t *testing.T) {
	site := newKnabenSite(t, 0, func(w http.ResponseWriter) { io.WriteString(w, "<html>maintenance</html>") })
	m, inst := knabenInstance(t, site)
	if _, err := m.Search(context.Background(), inst, "anything", nil); err == nil {
		t.Fatal("a page that is not JSON should be an error")
	}
}

func TestKnabenOversizedResponse(t *testing.T) {
	site := newKnabenSite(t, 0, func(w http.ResponseWriter) {
		io.WriteString(w, `{"hits":[],"pad":"`)
		io.CopyN(w, zeroReader{}, maxResponseBytes+10)
	})
	m, inst := knabenInstance(t, site)
	_, err := m.Search(context.Background(), inst, "anything", nil)
	if err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("err = %v, want the size-cap message", err)
	}
}

func TestToJSONTemplateFunction(t *testing.T) {
	vars := &tmplVars{Keywords: "a \"b\" \\ c\n<d>"}
	got, err := applyTemplate(`{"q":{{ .Keywords | tojson }}}`, vars, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(got), &out); err != nil || out["q"] != vars.Keywords {
		t.Fatalf("%s -> %v, %v", got, out, err)
	}
}
