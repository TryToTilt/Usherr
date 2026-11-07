package manga

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testChapterID = "11111111-1111-1111-1111-111111111111"

func quietDex(t *testing.T, h http.Handler) *MangaDex {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewMangaDex("Usherr/test", srv.URL)
	c.APIGap, c.AtHomeGap, c.ImageGap = 0, 0, 0
	return c
}

func TestDexSendsItsNameAndNoLogin(t *testing.T) {
	var ua, auth, via string
	c := quietDex(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, auth, via = r.UserAgent(), r.Header.Get("Authorization"), r.Header.Get("Via")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	if _, err := c.Search(context.Background(), "x", false); err != nil {
		t.Fatal(err)
	}
	if ua != "Usherr/test" || auth != "" || via != "" {
		t.Fatalf("user agent %q, auth %q, via %q", ua, auth, via)
	}
}

func TestDexKeepsToItsPace(t *testing.T) {
	c := quietDex(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"baseUrl":"https://example.invalid","chapter":{}}`))
	}))
	c.APIGap, c.AtHomeGap = 40*time.Millisecond, 120*time.Millisecond
	start := time.Now()
	for range 4 {
		if _, err := c.Search(context.Background(), "x", false); err != nil {
			t.Fatal(err)
		}
	}
	if took := time.Since(start); took < 110*time.Millisecond {
		t.Fatalf("4 requests took %v: the pace wasn't kept", took)
	}
	start = time.Now()
	for range 2 {
		if _, err := c.AtHome(context.Background(), testChapterID); err != nil {
			t.Fatal(err)
		}
	}
	if took := time.Since(start); took < 110*time.Millisecond {
		t.Fatalf("at-home's slower pace wasn't kept: %v", took)
	}
	if dexAPIGap < 200*time.Millisecond || dexAtHomeGap < 1500*time.Millisecond {
		t.Fatal("the real pace must stay under 5 a second, and 40 a minute for at-home")
	}
}

func TestDexStopsAskingWhenToldTo(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		header map[string]string
		kind   string
		least  time.Duration
	}{
		"429 with Retry-After":   {429, map[string]string{"Retry-After": "120"}, DexBusy, 100 * time.Second},
		"429 with its timestamp": {429, map[string]string{"X-RateLimit-Retry-After": strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10)}, DexBusy, 4 * time.Minute},
		"403, a ban":             {403, nil, DexRefused, 50 * time.Minute},
		"500":                    {500, nil, DexDown, time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			c := quietDex(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
			}))
			_, err := c.Search(context.Background(), "x", false)
			var de *DexError
			if !errors.As(err, &de) || de.Kind != tc.kind {
				t.Fatalf("got %v", err)
			}
			if time.Until(de.Until) < tc.least {
				t.Fatalf("waits only %v", time.Until(de.Until))
			}
			for range 3 {
				if _, err := c.Feed(context.Background(), testChapterID, "en", false); err == nil {
					t.Fatal("it should refuse without asking")
				}
			}
			if hits.Load() != 1 {
				t.Fatalf("asked %d times after being told to stop", hits.Load())
			}
		})
	}
}

func TestDexWaitsWhenThePeriodIsUsedUp(t *testing.T) {
	var hits atomic.Int32
	c := quietDex(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Retry-After", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))
		_, _ = w.Write([]byte(`{"baseUrl":"https://example.invalid","chapter":{"hash":"h","data":["a.png"]}}`))
	}))
	if _, err := c.AtHome(context.Background(), testChapterID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AtHome(context.Background(), testChapterID); err == nil || hits.Load() != 1 {
		t.Fatalf("err %v, %d asked: the last one allowed should end asking", err, hits.Load())
	}
}

// roundTripFunc answers image requests for any address.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDexReportsImagesFromTheNetwork(t *testing.T) {
	var mu sync.Mutex
	var reports []dexReport
	c := quietDex(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/network/report" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var rep dexReport
		_ = json.NewDecoder(r.Body).Decode(&rep)
		mu.Lock()
		reports = append(reports, rep)
		mu.Unlock()
	}))
	c.Images = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Error("no login may go to an image server")
		}
		if strings.Contains(r.URL.Path, "dead") {
			return nil, errors.New("no route")
		}
		status, cache := 200, "HIT from node"
		if strings.Contains(r.URL.Path, "bad") {
			status, cache = 500, ""
		}
		return &http.Response{StatusCode: status, Header: http.Header{"X-Cache": {cache}}, Body: io.NopCloser(strings.NewReader("12345")), ContentLength: -1}, nil
	})}
	ctx := context.Background()
	network := AtHome{BaseURL: "https://abc.mangadex.network/token", Hash: "h"}
	var out bytes.Buffer
	if n, err := c.Page(ctx, network, false, "good.png", &out); err != nil || n != 5 {
		t.Fatalf("good page: %d %v", n, err)
	}
	if _, err := c.Page(ctx, network, true, "bad.png", &out); !errors.Is(err, ErrDexPage) {
		t.Fatalf("bad page: %v", err)
	}
	if n, err := c.Page(ctx, network, false, "dead.png", &out); !errors.Is(err, ErrDexPage) || n != 0 {
		t.Fatalf("dead page: %d %v", n, err)
	}
	// MangaDex's own servers aren't reported.
	if _, err := c.Page(ctx, AtHome{BaseURL: "https://uploads.mangadex.org", Hash: "h"}, false, "own.png", &out); err != nil {
		t.Fatal(err)
	}
	c.WaitReports()
	mu.Lock()
	defer mu.Unlock()
	if len(reports) != 3 {
		t.Fatalf("%d reports: %+v", len(reports), reports)
	}
	good, bad, dead := reports[0], reports[1], reports[2]
	if good.URL != "https://abc.mangadex.network/token/data/h/good.png" || !good.Success || !good.Cached || good.Bytes != 5 {
		t.Fatalf("good report %+v", good)
	}
	if bad.Success || bad.Cached || !strings.Contains(bad.URL, "/data-saver/h/") {
		t.Fatalf("bad report %+v", bad)
	}
	if dead.Success || dead.Bytes != 0 {
		t.Fatalf("no answer should report no bytes: %+v", dead)
	}
}

func TestDexReportFailureStopsNothing(t *testing.T) {
	c := quietDex(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(522) }))
	c.Images = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("x")), ContentLength: 1}, nil
	})}
	if _, err := c.Page(context.Background(), AtHome{BaseURL: "https://a.mangadex.network", Hash: "h"}, false, "a.png", io.Discard); err != nil {
		t.Fatal(err)
	}
	c.WaitReports()
}

func TestDexFeedReadsWhatsTrueOfEachChapter(t *testing.T) {
	var q string
	c := quietDex(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"total":3,"data":[
			{"id":"a","attributes":{"volume":"1","chapter":"1","title":null,"translatedLanguage":"en","externalUrl":null,"isUnavailable":true,"pages":47,"publishAt":"2020"},"relationships":[{"id":"g1","type":"scanlation_group","attributes":{"name":"Miss Dream"}},{"id":"m","type":"manga"}]},
			{"id":"b","attributes":{"volume":"1","chapter":"1","translatedLanguage":"en","externalUrl":"https://kodansha.us/reader/30893","isUnavailable":false,"pages":0},"relationships":[{"id":"g2","type":"scanlation_group","attributes":{"name":"Kodansha USA"}}]},
			{"id":"c","attributes":{"volume":null,"chapter":null,"translatedLanguage":"en","externalUrl":"javascript:alert(1)","pages":3},"relationships":[]}
		]}`))
	}))
	list, err := c.Feed(context.Background(), testChapterID, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || !list[0].Unavailable || list[0].Groups[0].Name != "Miss Dream" || list[1].ExternalURL == "" || list[2].Number != "" {
		t.Fatalf("%+v", list)
	}
	if list[2].ExternalURL != "" {
		t.Fatal("only a web address may become a link")
	}
	for _, want := range []string{"includeUnavailable=1", "includeExternalUrl=1", "includeEmptyPages=1", "scanlation_group"} {
		if !strings.Contains(q, want) {
			t.Fatalf("the feed must ask for %s: %s", want, q)
		}
	}
	if strings.Contains(q, "pornographic") {
		t.Fatal("adult ratings only for a series known to be adult")
	}
}

func TestChooseCopy(t *testing.T) {
	a := ChapterCopy{DexID: "a", GroupKey: "A", Pages: 20, PublishedAt: "2021"}
	b := ChapterCopy{DexID: "b", GroupKey: "B", Pages: 30, PublishedAt: "2022"}
	ext := ChapterCopy{DexID: "e", GroupKey: "P", ExternalURL: "https://x.example"}
	gone := ChapterCopy{DexID: "g", GroupKey: "G", Pages: 40, Unavailable: true}
	for name, tc := range map[string]struct {
		list   []ChapterCopy
		prefer string
		counts map[string]int
		want   string
	}{
		"the chosen group wins":                    {[]ChapterCopy{b, a}, "A", map[string]int{"B": 50, "A": 1}, "a"},
		"a chosen group with no copy is passed":    {[]ChapterCopy{a, b}, "Z", map[string]int{"B": 2, "A": 1}, "b"},
		"the group with the most chapters":         {[]ChapterCopy{a, b}, "", map[string]int{"A": 9, "B": 1}, "a"},
		"then the fullest":                         {[]ChapterCopy{a, b}, "", nil, "b"},
		"the first published, for a stable choice": {[]ChapterCopy{{DexID: "x", GroupKey: "X", Pages: 9, PublishedAt: "2023"}, {DexID: "y", GroupKey: "Y", Pages: 9, PublishedAt: "2020"}}, "", nil, "y"},
		"anything readable beats external":         {[]ChapterCopy{ext, a}, "P", nil, "a"},
		"anything readable beats a takedown":       {[]ChapterCopy{gone, a}, "G", nil, "a"},
		"a link beats a takedown":                  {[]ChapterCopy{gone, ext}, "", nil, "e"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ChooseCopy(tc.list, tc.prefer, tc.counts); got.DexID != tc.want {
				t.Fatalf("got %q, want %q", got.DexID, tc.want)
			}
		})
	}
}

func TestDexMatch(t *testing.T) {
	sr := Series{AniListID: 30092, Title: "Sailor Moon", StartYear: 1991}
	names := []string{"Sailor Moon", "Bishoujo Senshi Sailor Moon"}
	right := DexSeries{ID: "right", Title: "Bishoujo Senshi Sailor Moon", AniListID: 30092, Year: 1991}
	dj := DexSeries{ID: "dj", Title: "Sailor Moon (Doujinshi)", Year: 1991}
	other := DexSeries{ID: "other", Title: "Sailor Moon", AniListID: 999, Year: 1991}
	if best, how, _ := DexMatch(sr, names, []DexSeries{dj, other, right}); best == nil || best.ID != "right" || how != "anilist" {
		t.Fatalf("a cross-link is proof: %+v %s", best, how)
	}
	plain := DexSeries{ID: "p", Title: "Sailor Moon", Year: 1992}
	if best, how, _ := DexMatch(sr, names, []DexSeries{dj, other, plain}); best == nil || best.ID != "p" || how != "title" {
		t.Fatalf("one exact title in the right years: %+v %s", best, how)
	}
	far := DexSeries{ID: "far", Title: "Sailor Moon", Year: 2015}
	if best, _, unsure := DexMatch(sr, names, []DexSeries{far}); best != nil || len(unsure) != 1 {
		t.Fatal("the wrong years need a person")
	}
	two := DexSeries{ID: "two", AltTitles: []string{"Sailor Moon"}, Title: "PGSM", Year: 1991}
	if best, _, unsure := DexMatch(sr, names, []DexSeries{plain, two}); best != nil || len(unsure) != 2 {
		t.Fatal("two exact titles need a person")
	}
	if best, _, unsure := DexMatch(sr, names, []DexSeries{dj, other}); best != nil || len(unsure) != 0 {
		t.Fatalf("a doujinshi and another series aren't candidates: %+v", unsure)
	}
}

func TestDexIDFromLink(t *testing.T) {
	if id := DexIDFromLink("https://mangadex.org/title/e39944f5-15bf-4464-9556-a4e9b3945571/bishoujo"); id != "e39944f5-15bf-4464-9556-a4e9b3945571" {
		t.Fatal(id)
	}
	if DexIDFromLink("https://example.com/title/x") != "" {
		t.Fatal("not a MangaDex link")
	}
}

func TestChapterFileName(t *testing.T) {
	for _, tc := range []struct{ number, group, want string }{
		{"123", "Inkwell Scans", "Series c0123 [Inkwell Scans]"},
		{"12.5", "", "Series c0012.5"},
		{OneshotNumber, "G", "Series Oneshot [G]"},
	} {
		if got := ChapterFileName("Series", tc.number, tc.group); got != tc.want {
			t.Fatalf("%q: got %q", tc.number, got)
		}
	}
	if n := ParseFor("Series c0123 [Inkwell Scans].cbz", []string{"Series"}); n.Kind != KindChapter || n.Chapter != "123" {
		t.Fatalf("our own name must read back: %+v", n)
	}
}

func TestChapterComicInfoCredits(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "0001.png")
	if err := os.WriteFile(page, []byte("picture"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.cbz")
	sr := Series{Title: "Tower", Format: "manhwa", Author: "Han"}
	c := Chapter{Number: "88", Volume: "9", Title: "Floor 212", Group: "Inkwell Scans", Language: "en", DexID: "abc"}
	if err := WriteChapterCBZ(dest, []string{page}, InfoForChapter(sr, c)); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var xmlText string
	for _, f := range zr.File {
		if f.Name == "ComicInfo.xml" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			xmlText = string(b)
		}
	}
	for _, want := range []string{"<Translator>Inkwell Scans</Translator>", "<ScanInformation>Inkwell Scans, via MangaDex</ScanInformation>", "MangaDex (https://mangadex.org)", "<Web>https://mangadex.org/chapter/abc</Web>", "<Number>88</Number>", "<Volume>9</Volume>", "<Title>Floor 212</Title>", "<LanguageISO>en</LanguageISO>", "<Manga>Yes</Manga>"} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("no %s in\n%s", want, xmlText)
		}
	}
	// With no group, MangaDex is still credited and no translator is made up.
	info := InfoForChapter(sr, Chapter{Number: "1"})
	if info.Translator != "" || !strings.Contains(info.Notes, "MangaDex") {
		t.Fatalf("%+v", info)
	}
}

func TestReadingOrderAcrossVolumesAndChapters(t *testing.T) {
	volumes := []Volume{
		{ID: 1, Number: "1", SortKey: 1, Status: StatusDownloaded},
		{ID: 2, Number: "2", SortKey: 2, Status: StatusMissing, Wanted: true},
		{ID: 3, Number: "3", SortKey: 3, Status: StatusMissing, Wanted: true},
	}
	chapters := []Chapter{
		{ID: 10, Number: "1", SortKey: 1, Volume: "1", Status: StatusDownloaded, InVolumeID: 1}, // the volume holds it
		{ID: 20, Number: "8", SortKey: 8, Volume: "2", Status: StatusDownloaded},
		{ID: 21, Number: "9", SortKey: 9, Volume: "2", Status: StatusMissing, Wanted: true},
		{ID: 30, Number: "20", SortKey: 20, Status: StatusDownloaded},
		{ID: 31, Number: "21", SortKey: 21, ExternalURL: "https://x.example"},
		{ID: 32, Number: "22", SortKey: 22, Unavailable: true},
		{ID: 33, Number: "23", SortKey: 23, Status: StatusDownloaded},
	}
	order := ReadingOrder(volumes, chapters)
	var got []string
	for _, it := range order {
		kind := "v"
		if it.Chapter {
			kind = "c"
		}
		got = append(got, kind+it.Number)
	}
	if s := strings.Join(got, " "); s != "v1 c8 c9 v3 c20 c23" {
		t.Fatalf("order: %s", s)
	}
	next, have := NextInOrder(order, false, 1)
	if next == nil || !next.Chapter || next.ID != 20 || have != next {
		t.Fatalf("after the volume: %+v %+v", next, have)
	}
	next, have = NextInOrder(order, true, 20)
	if next.ID != 21 || next.Have || have.ID != 30 {
		t.Fatalf("after chapter 8: %+v %+v", next, have)
	}
	if next, have = NextInOrder(order, true, 33); next != nil || have != nil {
		t.Fatal("nothing after the last")
	}
	order[0].Read, order[1].Read = true, true
	if n := NextUnreadInOrder(order); n == nil || n.ID != 30 {
		t.Fatalf("next unread: %+v", n)
	}
}

func TestChapterRunIsOnlyForASeriesThatFollowsChapters(t *testing.T) {
	const run = "One Piece (Chapters 1-721) [Strays]"
	base := Target{Names: []string{"One Piece"}, Sources: Sources{Official: true}}
	if v := Evaluate(Release{Title: run}, base, ""); v.Why != RejectFan {
		t.Fatalf("fan scans off: %+v", v)
	}
	base.Sources.Fan = true
	if v := Evaluate(Release{Title: run}, base, ""); v.Why != RejectChapter {
		t.Fatalf("follows volumes: %+v", v)
	}
	base.FollowsChapters = true
	v := Evaluate(Release{Title: run}, base, "")
	if v.Reject != "" || v.Kind != KindChapter || v.Confident() {
		t.Fatalf("it should wait for a person, never come by itself: %+v", v)
	}
	if v := Evaluate(Release{Title: run}, base, "3"); v.Why != RejectChapter {
		t.Fatalf("a search for one volume doesn't take it: %+v", v)
	}
}
