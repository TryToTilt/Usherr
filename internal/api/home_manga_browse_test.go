package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/manga"
)

const (
	brTitle   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	brAdult   = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	brCh1     = "66666666-6666-6666-6666-666666666666"
	brCh2     = "77777777-7777-7777-7777-777777777777"
	brStaff   = "dddddddd-dddd-dddd-dddd-dddddddddddd"
	brSeason  = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	brOwner   = "ffffffff-ffff-ffff-ffff-ffffffffffff"
	brCover   = "12345678-1234-1234-1234-123456789012.jpg"
	brTagID   = "99999999-9999-9999-9999-999999999999"
	brGroupID = "88888888-8888-8888-8888-888888888888"
)

// fakeBrowse is a small MangaDex for browsing and reading online; no test asks
// the real service.
type fakeBrowse struct {
	*httptest.Server
	mu       sync.Mutex
	hits     map[string]int
	queries  []string
	reports  []map[string]any
	failData int // fail this many full-quality page fetches
	page     []byte
}

func brTitleJSON(id, title, rating string, al int) string {
	return fmt.Sprintf(`{"id":%q,"attributes":{"title":{"en":%q},"description":{"en":"About %s"},"links":{"al":"%d"},"year":2024,"status":"ongoing","contentRating":%q,"originalLanguage":"ja","availableTranslatedLanguages":["en"],"tags":[{"attributes":{"name":{"en":"Action"},"group":"genre"}}]},"relationships":[{"id":"x","type":"cover_art","attributes":{"fileName":%q}},{"id":"y","type":"author","attributes":{"name":"Author A"}}]}`, id, title, title, al, rating, brCover)
}

func brChapterJSON(id, number, group string) string {
	return fmt.Sprintf(`{"id":%q,"attributes":{"volume":"1","chapter":%q,"title":"T%s","translatedLanguage":"en","externalUrl":null,"isUnavailable":false,"pages":2,"publishAt":"2026-01-01T00:00:00+00:00","readableAt":"2026-11-01T00:00:00+00:00"},"relationships":[{"id":%q,"type":"scanlation_group","attributes":{"name":%q}},{"id":%q,"type":"manga"}]}`, id, number, number, brGroupID, group, brTitle)
}

func newFakeBrowse(t *testing.T) *fakeBrowse {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	f := &fakeBrowse{hits: map[string]int{}, page: buf.Bytes()}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		f.mu.Lock()
		f.hits[p]++
		f.queries = append(f.queries, p+"?"+r.URL.RawQuery)
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("a login went to %s", p)
		}
		switch {
		case p == "/network/report":
			var rep map[string]any
			_ = json.NewDecoder(r.Body).Decode(&rep)
			f.mu.Lock()
			f.reports = append(f.reports, rep)
			f.mu.Unlock()
		case strings.HasPrefix(p, "/img/"):
			f.mu.Lock()
			fail := strings.Contains(p, "/data/") && f.failData > 0
			if fail {
				f.failData--
			}
			f.mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("X-Cache", "HIT")
			_, _ = w.Write(f.page)
		case strings.HasPrefix(p, "/uploads/covers/"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg"))
		case strings.HasPrefix(p, "/at-home/server/"):
			_, _ = fmt.Fprintf(w, `{"result":"ok","baseUrl":%q,"chapter":{"hash":"h","data":["a.png","b.png"],"dataSaver":["a.jpg","b.jpg"]}}`, f.URL+"/img")
		case p == "/manga/tag":
			_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"attributes":{"name":{"en":"Action"},"group":"genre"}}]}`, brTagID)
		case p == "/manga/random":
			_, _ = fmt.Fprintf(w, `{"data":%s}`, brTitleJSON(brTitle, "Tower", "safe", 7))
		case strings.HasSuffix(p, "/feed"):
			_, _ = fmt.Fprintf(w, `{"total":2,"data":[%s,%s]}`, brChapterJSON(brCh1, "1", "Inkwell"), brChapterJSON(brCh2, "2", "Inkwell"))
		case p == "/manga":
			_, _ = fmt.Fprintf(w, `{"total":2,"data":[%s,%s]}`, brTitleJSON(brTitle, "Tower", "safe", 7), brTitleJSON(brAdult, "Night", "erotica", 0))
		case p == "/manga/"+brAdult:
			_, _ = fmt.Fprintf(w, `{"data":%s}`, brTitleJSON(brAdult, "Night", "erotica", 0))
		case strings.HasPrefix(p, "/manga/"):
			_, _ = fmt.Fprintf(w, `{"data":%s}`, brTitleJSON(brTitle, "Tower", "safe", 7))
		case p == "/chapter":
			_, _ = fmt.Fprintf(w, `{"data":[%s]}`, brChapterJSON(brCh2, "2", "Inkwell"))
		case strings.HasPrefix(p, "/chapter/"):
			id := strings.TrimPrefix(p, "/chapter/")
			_, _ = fmt.Fprintf(w, `{"data":%s}`, brChapterJSON(id, map[string]string{brCh1: "1", brCh2: "2"}[id], "Inkwell"))
		case p == "/list/"+brStaff:
			_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"attributes":{"name":"Staff Picks"},"relationships":[{"id":%q,"type":"manga"},{"id":%q,"type":"user"}]}}`, brStaff, brTitle, brOwner)
		case p == "/user/"+brOwner+"/list":
			_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"attributes":{"name":"Seasonal: Summer 2026"},"relationships":[{"id":%q,"type":"manga"}]},{"id":%q,"attributes":{"name":"Seasonal: Fall 2026"},"relationships":[{"id":%q,"type":"manga"}]},{"id":%q,"attributes":{"name":"Staff Picks"},"relationships":[{"id":%q,"type":"manga"}]}]}`, brAdult, brAdult, brSeason, brTitle, brStaff, brTitle)
		case p == "/statistics/chapter":
			_, _ = fmt.Fprintf(w, `{"statistics":{%q:{"comments":{"repliesCount":5}}}}`, brCh2)
		case p == "/statistics/manga":
			_, _ = fmt.Fprintf(w, `{"statistics":{%q:{"follows":1200,"rating":{"bayesian":8.4}}}}`, brTitle)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeBrowse) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for k, v := range f.hits {
		if strings.HasPrefix(k, prefix) {
			n += v
		}
	}
	return n
}

func browseServer(t *testing.T, f *fakeBrowse) (*Server, *manga.Repo) {
	t.Helper()
	s, repo, _ := mangaTestServer(t)
	mangaDexNoGap = true
	st := s.mangaMod()
	st.dex = manga.NewMangaDex("Usherr/test", f.URL)
	st.dex.APIGap, st.dex.AtHomeGap, st.dex.ImageGap = 0, 0, 0
	if err := s.Settings.Set(keyMangaSourceDex, "1", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Settings.Set(keyMangaDexStaffList, brStaff, false); err != nil {
		t.Fatal(err)
	}
	return s, repo
}

func browseGet(t *testing.T, s *Server, h http.HandlerFunc, url string, user *auth.User, path map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, url, nil)
	for k, v := range path {
		r.SetPathValue(k, v)
	}
	r = r.WithContext(auth.WithUser(r.Context(), user))
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

func TestMangaDexHubRowsAndCache(t *testing.T) {
	f := newFakeBrowse(t)
	s, _ := browseServer(t, f)
	member := &auth.User{ID: mangaUser(t, s, "reader")}
	rec := browseGet(t, s, s.handleMangaDexHub, "/api/manga/dex/hub?adult=1", member, nil)
	var out struct {
		DexOn    bool `json:"dexOn"`
		Mangadex struct {
			Popular  []map[string]any `json:"popular"`
			Latest   []map[string]any `json:"latest"`
			Recent   []map[string]any `json:"recent"`
			Seasonal *struct {
				Name   string           `json:"name"`
				Titles []map[string]any `json:"titles"`
			} `json:"seasonal"`
			Recommended *struct {
				Titles []map[string]any `json:"titles"`
			} `json:"recommended"`
		} `json:"mangadex"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(rec.Body.String())
	}
	m := out.Mangadex
	if !out.DexOn || len(m.Popular) != 1 || len(m.Recent) != 1 || len(m.Latest) != 1 {
		t.Fatalf("rows: %s", rec.Body.String())
	}
	// The adult title stays out: this account isn't allowed it, whatever the page asked.
	for _, p := range m.Popular {
		if p["id"] == brAdult {
			t.Error("an erotica title reached a normal account")
		}
	}
	if m.Seasonal == nil || m.Seasonal.Name != "Seasonal: Fall 2026" || len(m.Seasonal.Titles) != 1 {
		t.Errorf("the newest seasonal list: %+v", m.Seasonal)
	}
	if m.Recommended == nil || len(m.Recommended.Titles) != 1 {
		t.Errorf("staff picks: %+v", m.Recommended)
	}
	u := m.Latest[0]
	if u["comments"] != float64(5) || u["manga"].(map[string]any)["title"] != "Tower" || len(u["groups"].([]any)) != 1 {
		t.Errorf("latest update: %v", u)
	}
	if cover, _ := m.Popular[0]["coverUrl"].(string); !strings.HasPrefix(cover, "/api/manga/dex/covers/"+brTitle+"/") {
		t.Errorf("cover through Usherr: %q", cover)
	}
	for _, q := range f.queries {
		if strings.Contains(q, "erotica") || strings.Contains(q, "pornographic") {
			t.Errorf("asked for adult ratings: %s", q)
		}
	}
	// A second look within the minutes kept asks MangaDex nothing.
	before := f.count("/")
	browseGet(t, s, s.handleMangaDexHub, "/api/manga/dex/hub", member, nil)
	if f.count("/") != before {
		t.Errorf("the hub asked again: %d calls, then %d", before, f.count("/"))
	}

	// The cover handed out comes through Usherr, without a login.
	rec = browseGet(t, s, s.handleMangaDexCover, "/", member, map[string]string{"id": brTitle, "file": brCover})
	if rec.Code != http.StatusOK || rec.Body.String() != "jpeg" {
		t.Errorf("cover: %d %s", rec.Code, rec.Body.String())
	}
	rec = browseGet(t, s, s.handleMangaDexCover, "/", member, map[string]string{"id": brAdult, "file": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.jpg"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("a cover never handed out: %d", rec.Code)
	}
}

func TestMangaDexOffAsksNothing(t *testing.T) {
	f := newFakeBrowse(t)
	s, _ := browseServer(t, f)
	if err := s.Settings.Set(keyMangaSourceDex, "0", false); err != nil {
		t.Fatal(err)
	}
	member := &auth.User{ID: mangaUser(t, s, "reader")}
	rec := browseGet(t, s, s.handleMangaDexHub, "/api/manga/dex/hub", member, nil)
	if strings.Contains(rec.Body.String(), `"mangadex"`) || !strings.Contains(rec.Body.String(), `"dexOn":false`) {
		t.Errorf("hub with MangaDex off: %s", rec.Body.String())
	}
	for _, h := range []http.HandlerFunc{s.handleMangaDexSearch, s.handleMangaDexTags, s.handleMangaDexRandom} {
		if rec := browseGet(t, s, h, "/?title=x", member, nil); rec.Code != http.StatusNotFound {
			t.Errorf("switched off: %d", rec.Code)
		}
	}
	if rec := browseGet(t, s, s.handleMangaDexPages, "/", member, map[string]string{"id": brCh1}); rec.Code != http.StatusNotFound {
		t.Errorf("reading online with MangaDex off: %d", rec.Code)
	}
	if f.count("/") != 0 {
		t.Fatalf("asked MangaDex %d times with the switch off", f.count("/"))
	}
}

func TestMangaDexSearchKeepsToItsFilters(t *testing.T) {
	f := newFakeBrowse(t)
	s, _ := browseServer(t, f)
	member := &auth.User{ID: mangaUser(t, s, "reader")}
	rec := browseGet(t, s, s.handleMangaDexSearch, "/?title=tower&tag="+brTagID+"&notTag=bad&status=ongoing&status=nope&demo=seinen&lang=ja&year=2024&order=rating&readable=1&adult=1", member, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"Tower"`) || strings.Contains(rec.Body.String(), `"Night"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var asked string
	for _, q := range f.queries {
		if strings.HasPrefix(q, "/manga?") {
			asked = q
		}
	}
	for _, want := range []string{"title=tower", "includedTags%5B%5D=" + brTagID, "status%5B%5D=ongoing", "publicationDemographic%5B%5D=seinen", "originalLanguage%5B%5D=ja", "year=2024", "order%5Brating%5D=desc", "availableTranslatedLanguage%5B%5D=en", "contentRating%5B%5D=safe", "contentRating%5B%5D=suggestive"} {
		if !strings.Contains(asked, want) {
			t.Errorf("search lacks %s: %s", want, asked)
		}
	}
	for _, not := range []string{"nope", "notTag", "excludedTags", "erotica"} {
		if strings.Contains(asked, not) {
			t.Errorf("search has %s: %s", not, asked)
		}
	}
}

func TestMangaDexReadOnlineReportsEveryPage(t *testing.T) {
	f := newFakeBrowse(t)
	s, _ := browseServer(t, f)
	member := &auth.User{ID: mangaUser(t, s, "reader")}
	rec := browseGet(t, s, s.handleMangaDexPages, "/", member, map[string]string{"id": brCh1})
	var info struct {
		Pages     int    `json:"pages"`
		Direction string `json:"direction"`
		Group     string `json:"group"`
		Dex       struct {
			Next     *dexNextChapter `json:"next"`
			Chapters []any           `json:"chapters"`
		} `json:"dex"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if info.Pages != 2 || info.Direction != "rtl" || info.Group != "Inkwell" || info.Dex.Next == nil || info.Dex.Next.ID != brCh2 || len(info.Dex.Chapters) != 2 {
		t.Fatalf("%+v", info)
	}

	// The first full-size fetch fails: it's reported, a new address is asked for,
	// and the page still comes.
	f.mu.Lock()
	f.failData = 1
	f.mu.Unlock()
	homes := f.count("/at-home/")
	rec = browseGet(t, s, s.handleMangaDexPage, "/", member, map[string]string{"id": brCh1, "n": "1"})
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("page: %d %s", rec.Code, rec.Body.String())
	}
	if f.count("/at-home/") != homes+1 {
		t.Errorf("a failed page should ask for a new address: %d then %d", homes, f.count("/at-home/"))
	}
	// Both full-size tries fail: the smaller copy comes instead.
	f.mu.Lock()
	f.failData = 2
	f.mu.Unlock()
	rec = browseGet(t, s, s.handleMangaDexPage, "/", member, map[string]string{"id": brCh1, "n": "2"})
	if rec.Code != http.StatusOK {
		t.Fatalf("page 2: %d %s", rec.Code, rec.Body.String())
	}
	if f.count("/img/data-saver/") != 1 {
		t.Errorf("data-saver fallback: %d", f.count("/img/data-saver/"))
	}
	// Kept for a while: asking again fetches nothing.
	imgs := f.count("/img/")
	browseGet(t, s, s.handleMangaDexPage, "/", member, map[string]string{"id": brCh1, "n": "1"})
	if f.count("/img/") != imgs {
		t.Error("a kept page was fetched again")
	}
	s.mangaMod().dex.WaitReports()
	f.mu.Lock()
	reports := append([]map[string]any(nil), f.reports...)
	f.mu.Unlock()
	if len(reports) != f.count("/img/") {
		t.Fatalf("%d images, %d reports", f.count("/img/"), len(reports))
	}
	fails := 0
	for _, r := range reports {
		if !strings.HasPrefix(r["url"].(string), f.URL+"/img/") {
			t.Errorf("report url: %v", r)
		}
		if r["success"] == false {
			fails++
			continue
		}
		if r["cached"] != true || r["bytes"].(float64) <= 0 {
			t.Errorf("report: %v", r)
		}
	}
	if fails != 3 {
		t.Errorf("failed pages reported: %d", fails)
	}

	// The place is kept like any chapter's, and the Continue row shows it.
	put := httptest.NewRequest(http.MethodPut, "/api/home/position", strings.NewReader(`{"item":"manga:d:`+brCh1+`","position":2,"duration":3}`))
	put = put.WithContext(auth.WithUser(put.Context(), member))
	s.handlePositionPut(httptest.NewRecorder(), put)
	rec = browseGet(t, s, s.handleMangaDexContinue, "/", member, nil)
	if !strings.Contains(rec.Body.String(), brCh1) || !strings.Contains(rec.Body.String(), `"series":"Tower"`) {
		t.Errorf("continue: %s", rec.Body.String())
	}
}

func TestMangaDexAdultChapterNeedsNSFW(t *testing.T) {
	f := newFakeBrowse(t)
	s, _ := browseServer(t, f)
	member := &auth.User{ID: mangaUser(t, s, "reader")}
	rec := browseGet(t, s, s.handleMangaDexTitle, "/?adult=1", member, map[string]string{"id": brAdult})
	if rec.Code != http.StatusNotFound {
		t.Errorf("an erotica title for a normal account: %d %s", rec.Code, rec.Body.String())
	}
	rec = browseGet(t, s, s.handleMangaDexTitle, "/", member, map[string]string{"id": brTitle})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"chapters":[`) {
		t.Errorf("a safe title: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMangaDexDownloadNeedsTheSeries(t *testing.T) {
	f := newFakeBrowse(t)
	s, _ := browseServer(t, f)
	admin := &auth.User{ID: mangaUser(t, s, "boss"), IsAdmin: true}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.SetPathValue("id", brCh1)
	r = r.WithContext(auth.WithUser(r.Context(), admin))
	rec := httptest.NewRecorder()
	s.handleMangaDexDownload(rec, r)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "library first") {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	_, _ = io.Copy(io.Discard, rec.Body)
}

func TestDexReadableOnePerNumber(t *testing.T) {
	g := func(id, name string) []manga.DexGroup { return []manga.DexGroup{{ID: id, Name: name}} }
	feed := []manga.DexChapter{
		{ID: "a1", Number: "1", Pages: 5, Groups: g("ga", "A")},
		{ID: "b1", Number: "1", Pages: 5, Groups: g("gb", "B")},
		{ID: "b2", Number: "2", Pages: 5, Groups: g("gb", "B")},
		{ID: "x3", Number: "3", ExternalURL: "https://elsewhere.example/3"},
		{ID: "g4", Number: "4", Unavailable: true},
	}
	rows := dexReadable(feed, "")
	if len(rows) != 3 || rows[0].ID != "b1" || rows[0].Copies != 2 || rows[2].ExternalURL == "" {
		t.Fatalf("%+v", rows) // B did the most, so B's chapter 1; the taken-down one is gone
	}
	if rows := dexReadable(feed, "ga"); rows[0].ID != "a1" {
		t.Errorf("the group being read wins: %+v", rows[0])
	}
}
