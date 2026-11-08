package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

// fakeMangaIndexer answers searches with the releases whose title holds the
// query's first word; no real indexer is ever asked.
type fakeMangaIndexer struct {
	*httptest.Server
	mu       sync.Mutex
	queries  []string
	releases []string
}

func newFakeMangaIndexer(t *testing.T, releases ...string) *fakeMangaIndexer {
	t.Helper()
	f := &fakeMangaIndexer{releases: releases}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("t") {
		case "caps":
			_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><server title="fake"/></caps>`))
		case "search", "book":
			q := r.URL.Query().Get("q")
			f.mu.Lock()
			f.queries = append(f.queries, q)
			list := append([]string(nil), f.releases...)
			f.mu.Unlock()
			var b strings.Builder
			b.WriteString(`<?xml version="1.0"?><rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel>`)
			word := strings.ToLower(strings.Fields(q + " x")[0])
			for i, title := range list {
				if !strings.Contains(strings.ToLower(title), word) {
					continue
				}
				fmt.Fprintf(&b, `<item><title>%s</title><link>%s/get/%d</link><guid>%d</guid><enclosure url="%s/get/%d" length="209715200" type="application/x-nzb"/><newznab:attr name="category" value="7030"/><newznab:attr name="size" value="209715200"/></item>`, title, f.URL, i, i, f.URL, i)
			}
			b.WriteString(`</channel></rss>`)
			_, _ = w.Write([]byte(b.String()))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeMangaIndexer) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...)
}

func mangaTestServer(t *testing.T) (*Server, *manga.Repo, string) {
	t.Helper()
	restore := indexers.SetSearchGaps(0, 0)
	t.Cleanup(restore)
	mangaNoGap = true
	s, _, dir := bigServer(t)
	root := filepath.Join(dir, "manga")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Settings.Set(keyMangaPath, root, false); err != nil {
		t.Fatal(err)
	}
	if err := s.setModules(moduleChanges{Manga: new(bool)}); err != nil {
		t.Fatal(err)
	}
	on := true
	if err := s.setModules(moduleChanges{Manga: &on}); err != nil {
		t.Fatal(err)
	}
	_ = s.dispatch.Hold() // nothing starts downloading, for as long as the test runs
	return s, s.mangaRepo(), root
}

func addFakeIndexer(t *testing.T, s *Server, f *fakeMangaIndexer, perDay int) {
	t.Helper()
	if _, err := s.IndexerRepo.Create(indexers.Instance{Name: "Fake", BaseURL: f.URL, APIKey: "k", Protocol: indexers.ProtocolUsenet, Kind: indexers.KindNewznab, Enabled: true, RequestsPerDay: perDay}, nil); err != nil {
		t.Fatal(err)
	}
}

func addSeries(t *testing.T, repo *manga.Repo, title string, volumes, licensed int, aliases ...string) manga.Series {
	t.Helper()
	var list []manga.Alias
	for _, a := range aliases {
		list = append(list, manga.Alias{Alias: a})
	}
	sr, err := repo.AddSeries(manga.Series{Title: title, LicensedEN: licensed, MonitorNew: true, WantAll: true, VolumeCount: volumes, Status: "finished", CheckedAt: time.Now().UTC().Format(time.RFC3339)}, list)
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= volumes; n++ {
		if _, _, err := repo.EnsureVolume(sr.ID, fmt.Sprint(n), true); err != nil {
			t.Fatal(err)
		}
	}
	return sr
}

func TestMangaModule(t *testing.T) {
	s, _, _ := bigServer(t)
	if s.mangaEnabled() || s.modulesPayload().Manga.Enabled {
		t.Fatal("Manga must be off until switched on")
	}
	if !s.modulesPayload().Manga.Available {
		t.Fatal("Manga must be offered")
	}
	on, off := true, false
	if err := s.setModules(moduleChanges{Manga: &on}); err != nil {
		t.Fatal(err)
	}
	if !s.mangaEnabled() {
		t.Fatal("the switch didn't stick")
	}
	// Manga alone counts as something on.
	if err := s.setModules(moduleChanges{Movies: &off, TV: &off}); err != nil {
		t.Fatal(err)
	}
	if err := s.setModules(moduleChanges{Manga: &off}); err == nil {
		t.Fatal("everything off should be refused")
	}
	if got := extraFolders["manga"].env; got != "MANGA_DIR" {
		t.Fatalf("env %q", got)
	}
	if v, _ := s.Settings.Get(settings.KeyModulesManga); v != "1" {
		t.Fatalf("saved %q", v)
	}
}

// A series your indexers don't carry costs one search by name (and its other
// titles), never one per volume, and waits longer each time.
func TestMangaHuntBacksOffWhenNothingIsThere(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	fake := newFakeMangaIndexer(t)
	addFakeIndexer(t, s, fake, 0)
	sr := addSeries(t, repo, "Paper Comet Club", 8, -1, "Kami Suisei-bu", "紙彗星部")

	s.huntManga(context.Background())
	if q := fake.asked(); len(q) != 2 || q[0] != "Paper Comet Club" || q[1] != "Kami Suisei bu" {
		t.Fatalf("asked %q", q)
	}
	log, _ := repo.SearchLog(sr.ID)
	if len(log) != 1 || log[0].VolumeID != 0 || log[0].Outcome != "nothing" || log[0].Empties != 1 {
		t.Fatalf("log %+v", log)
	}
	due := manga.DueAt(log, 0)
	if d := time.Until(due); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("next look in %v", d)
	}
	// Not due yet: nobody is asked.
	s.huntManga(context.Background())
	if q := fake.asked(); len(q) != 2 {
		t.Fatalf("asked again: %q", q)
	}
	// Each look that finds nothing waits longer.
	_ = repo.LogSearch(sr.ID, 0, "nothing", manga.SearchSummary{Asked: 1}, time.Now())
	log, _ = repo.SearchLog(sr.ID)
	if d := time.Until(manga.DueAt(log, 0)); d < 71*time.Hour || d > 73*time.Hour {
		t.Fatalf("second wait %v", d)
	}
}

// One look by name takes the sure volumes, and files the rest under why.
func TestMangaHuntTakesConfidentVolumes(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	fake := newFakeMangaIndexer(t,
		"Mob Psycho 100 v01 (2018) (Digital) (1r0n)",
		"Mob Psycho 100 v02 [scanlation] [Strays]",
		"Mob Psycho 100 v03 (Japanese) (raw)",
		"Mob Psycho 100 c100",
		"Mob Psycho 100 v01-v16",
		"Mob Psycho 100 Reigen v01 (Digital) (Viz)",
		"Mob Psycho 100 v05",
	)
	addFakeIndexer(t, s, fake, 0)
	sr := addSeries(t, repo, "Mob Psycho 100", 16, 1)

	grabbed, named := s.lookForManga(context.Background(), sr.ID, queue.PriorityAutomatic, 10)
	if !named || grabbed != 1 {
		t.Fatalf("grabbed %d, named %v", grabbed, named)
	}
	volumes, _ := repo.Volumes(sr.ID)
	if volumes[0].State != "downloading" {
		t.Fatalf("v01 is %s", volumes[0].State)
	}
	if volumes[1].State != manga.StatusMissing || volumes[2].State != manga.StatusMissing {
		t.Fatalf("a fan scan or a raw was taken: %+v", volumes[1:3])
	}
	if volumes[4].State != "waiting" {
		t.Fatalf("an unknown release should wait for a person: %+v", volumes[4])
	}
	reviews, _ := repo.Reviews(sr.ID)
	var pack bool
	for _, r := range reviews {
		pack = pack || (r.Kind == "pack" && r.VolumeID == 0)
	}
	if !pack {
		t.Fatalf("the pack wasn't offered: %+v", reviews)
	}
	log, _ := repo.SearchLog(sr.ID)
	sum := log[0].Summary
	if log[0].Outcome != "grabbed" || sum.Found != 7 || sum.Rejected["language:Japanese"] != 1 || sum.Rejected[manga.RejectFan] != 1 || sum.Rejected[manga.RejectChapter] != 1 || sum.Rejected[manga.RejectSeries] != 1 {
		t.Fatalf("%+v %+v", log[0], sum)
	}
	if len(fake.asked()) != 1 {
		t.Fatalf("asked %q", fake.asked())
	}

	// With fan scans allowed for this series, the fan volume comes too.
	_ = repo.SetSourceMode(sr.ID, "fan")
	grabbed, _ = s.lookForManga(context.Background(), sr.ID, queue.PriorityAutomatic, 10)
	if grabbed != 1 {
		t.Fatalf("grabbed %d with fan scans on", grabbed)
	}
	if v, _ := repo.GetVolume(volumes[1].ID); v.State != "downloading" {
		t.Fatalf("v02 is %s", v.State)
	}
}

// The grab limits hold: one look can't flood the line.
func TestMangaGrabLimits(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	var rel []string
	for n := 1; n <= 30; n++ {
		rel = append(rel, fmt.Sprintf("Long Series v%02d (Digital) (1r0n)", n))
	}
	addFakeIndexer(t, s, newFakeMangaIndexer(t, rel...), 0)
	sr := addSeries(t, repo, "Long Series", 30, 1)
	grabbed, _ := s.lookForManga(context.Background(), sr.ID, queue.PriorityAutomatic, maxMangaGrabsAtOnce)
	if grabbed != maxAutoDownloadsPerHr {
		t.Fatalf("grabbed %d", grabbed)
	}
	if s.autoGrabRoom() {
		t.Fatal("the hour's room should be used up")
	}
}

// Volume searches come only after the name found something, use volume words,
// and stop at the first good one.
func TestMangaVolumeSearchWords(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	fake := newFakeMangaIndexer(t, "Kaiju No. 8 v03 (Digital) (Viz)", "Kaiju No. 8 Vol. 4 (Digital) (1r0n)")
	addFakeIndexer(t, s, fake, 0)
	sr := addSeries(t, repo, "Kaiju No. 8", 4, 1)
	volumes, _ := repo.Volumes(sr.ID)
	got, _ := s.searchMangaVolume(context.Background(), sr, volumes[2], queue.PriorityManual, false)
	if !got {
		t.Fatal("v03 wasn't taken")
	}
	if q := fake.asked(); len(q) != 1 || q[0] != "Kaiju No 8 Vol 03" {
		t.Fatalf("asked %q", q)
	}
	// Nothing for this one: the next words are tried, and it waits a day.
	v, _ := repo.GetVolume(volumes[0].ID)
	if got, sum := s.searchMangaVolume(context.Background(), sr, v, queue.PriorityManual, false); got || sum.Found == 0 {
		t.Fatalf("v01: %v %+v", got, sum)
	}
	log, _ := repo.SearchLog(sr.ID)
	if d := time.Until(manga.DueAt(log, v.ID)); d < 23*time.Hour {
		t.Fatalf("no wait: %v", d)
	}
}

// An indexer out of searches for the day isn't asked by the automatic search,
// and that doesn't count against the series.
func TestMangaRespectsDailyLimit(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	fake := newFakeMangaIndexer(t)
	addFakeIndexer(t, s, fake, 1)
	one := addSeries(t, repo, "First Series", 2, 1)
	two := addSeries(t, repo, "Second Series", 2, 1)
	s.lookForManga(context.Background(), one.ID, queue.PriorityAutomatic, 10)
	s.lookForManga(context.Background(), two.ID, queue.PriorityAutomatic, 10)
	if q := fake.asked(); len(q) != 1 {
		t.Fatalf("asked %q with a limit of 1 a day", q)
	}
	log, _ := repo.SearchLog(two.ID)
	if len(log) != 1 || log[0].Outcome != "unasked" || log[0].Empties != 0 || log[0].Summary.Skipped != 1 {
		t.Fatalf("%+v", log)
	}
}

func writeCBZ(t *testing.T, path string, pages int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for i := 0; i < pages; i++ {
		w, _ := zw.Create(fmt.Sprintf("%03d.jpg", i))
		_, _ = w.Write([]byte("page data"))
	}
	_ = zw.Close()
	_ = f.Close()
}

func TestMangaFilingAndDuplicates(t *testing.T) {
	s, repo, root := mangaTestServer(t)
	sr := addSeries(t, repo, "Spy × Family", 3, 1)
	work := t.TempDir()
	first := filepath.Join(work, "file_0.cbz")
	writeCBZ(t, first, 3)

	var out mangaFiled
	if err := s.fileMangaFile(&sr, "1", first, "official", false, &out); err != nil {
		t.Fatal(err)
	}
	if out.filed != 1 {
		t.Fatalf("%+v", out)
	}
	v, _, _ := repo.EnsureVolume(sr.ID, "1", false)
	if v.Status != manga.StatusDownloaded || filepath.Dir(filepath.Dir(v.FilePath)) != root || !strings.HasSuffix(v.FilePath, "v01.cbz") {
		t.Fatalf("filed at %q (%s)", v.FilePath, v.Status)
	}
	// ComicInfo.xml went in, and says it's right to left, in English.
	zr, err := zip.OpenReader(v.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	var info string
	for _, f := range zr.File {
		if f.Name == "ComicInfo.xml" {
			rc, _ := f.Open()
			b := make([]byte, 4096)
			n, _ := rc.Read(b)
			info = string(b[:n])
			rc.Close()
		}
	}
	zr.Close()
	if !strings.Contains(info, "<Manga>YesAndRightToLeft</Manga>") || !strings.Contains(info, "<LanguageISO>en</LanguageISO>") || !strings.Contains(info, "<Number>1</Number>") {
		t.Fatalf("ComicInfo: %s", info)
	}

	// The same size again: dropped.
	same := filepath.Join(work, "same.cbz")
	body, _ := os.ReadFile(v.FilePath)
	_ = os.WriteFile(same, body, 0o644)
	out = mangaFiled{}
	if err := s.fileMangaFile(&sr, "1", same, "official", false, &out); err != nil || out.same != 1 || out.filed != 0 {
		t.Fatalf("%v %+v", err, out)
	}
	// Another copy: not filed, unless a person chose it.
	other := filepath.Join(work, "other.cbz")
	writeCBZ(t, other, 9)
	out = mangaFiled{}
	if err := s.fileMangaFile(&sr, "1", other, "official", false, &out); err != nil || out.copies != 1 || out.filed != 0 {
		t.Fatalf("%v %+v", err, out)
	}
	if files, _ := filepath.Glob(filepath.Join(filepath.Dir(v.FilePath), "*.cbz")); len(files) != 1 {
		t.Fatalf("a second copy was filed: %q", files)
	}
	out = mangaFiled{}
	if err := s.fileMangaFile(&sr, "1", other, "official", true, &out); err != nil || out.filed != 1 {
		t.Fatalf("%v %+v", err, out)
	}
	kept, _ := filepath.Glob(filepath.Join(root, ".replaced", "*", "*.cbz"))
	if len(kept) != 1 {
		t.Fatalf("the earlier file wasn't set aside: %q", kept)
	}
	// Junk isn't filed.
	junk := filepath.Join(work, "junk.cbz")
	_ = os.WriteFile(junk, []byte("<html>not a comic</html>"), 0o644)
	out = mangaFiled{}
	if err := s.fileMangaFile(&sr, "2", junk, "official", false, &out); err != nil || out.broken != 1 || out.filed != 0 {
		t.Fatalf("%v %+v", err, out)
	}
}

// A pack fills every volume it holds, adds the ones not listed, and leaves alone
// what the library has and what it can't name.
func TestMangaPackFilling(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	sr := addSeries(t, repo, "Kaiju No. 8", 3, 1)
	work := t.TempDir()
	var files []string
	for _, name := range []string{"Kaiju No. 8 v01.cbz", "Kaiju No. 8 v02 (Digital).cbz", "Kaiju No. 8 Vol. 4.cbz", "cover.cbz", "Kaiju No. 8 c099.cbz"} {
		p := filepath.Join(work, name)
		writeCBZ(t, p, 2)
		files = append(files, p)
	}
	d := manga.Download{SeriesID: sr.ID, Class: string(manga.ClassOfficial)}
	out, err := s.fileMangaDownload(&sr, d, files, "Kaiju No. 8 v01-v04 (Digital)", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.filed != 3 || out.unnamed != 2 {
		t.Fatalf("%+v", out)
	}
	volumes, _ := repo.Volumes(sr.ID)
	if len(volumes) != 4 || volumes[0].Status != manga.StatusDownloaded || volumes[2].Status != manga.StatusMissing || volumes[3].Number != "4" || volumes[3].Status != manga.StatusDownloaded {
		t.Fatalf("%+v", volumes)
	}
	if sr.Folder == "" {
		t.Fatal("the series' folder wasn't noted")
	}
	// The same pack again files nothing, so it can be told apart from a failure.
	for _, p := range files[:2] {
		writeCBZ(t, p, 2)
	}
	out, err = s.fileMangaDownload(&sr, d, files[:2], "Kaiju No. 8 v01-v02 (Digital)", "")
	if err != nil || out.filed != 0 || out.same == 0 {
		t.Fatalf("%v %+v", err, out)
	}
}

func TestMangaNumberOfAFile(t *testing.T) {
	names := []string{"Mob Psycho 100"}
	cases := []struct {
		file, release, linked string
		single                bool
		want                  string
	}{
		{"/w/file_0.cbz", "Mob Psycho 100 v05 (Digital)", "5", true, "5"},
		{"/w/Mob Psycho 100 v07.cbz", "Mob Psycho 100 v05 (Digital)", "5", true, "5"}, // one file: the release's word
		{"/w/Mob Psycho 100 v07.cbz", "Mob Psycho 100 v01-v16", "", false, "7"},
		{"/w/page.cbz", "Mob Psycho 100 v01-v16", "", false, ""},
		{"/w/x.cbz", "Some pack name", "9", true, "9"},
	}
	for _, c := range cases {
		if got := mangaNumberOf(c.file, c.release, c.linked, names, c.single); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

func TestMangaFolderScan(t *testing.T) {
	s, repo, root := mangaTestServer(t)
	sr := addSeries(t, repo, "Kaiju No. 8", 3, 1, "Kaiju Hachigo")
	_ = repo.SetFolder(sr.ID, "")
	writeCBZ(t, filepath.Join(root, "Kaiju No. 8", "Kaiju No. 8 v01.cbz"), 2)
	writeCBZ(t, filepath.Join(root, "Kaiju No. 8", "Kaiju No. 8 v01 (2).cbz"), 3)
	writeCBZ(t, filepath.Join(root, "Kaiju Hachigo v02.cbz"), 2) // loose, by another title
	writeCBZ(t, filepath.Join(root, "Kaiju No. 8", "Kaiju No. 8 c120.cbz"), 2)
	writeCBZ(t, filepath.Join(root, "Kaiju No. 8", "Kaiju No. 8 v04-v06.cbz"), 2)
	writeCBZ(t, filepath.Join(root, "Unknown Tale", "Unknown Tale v01.cbz"), 2)
	writeCBZ(t, filepath.Join(root, ".replaced", "Kaiju No. 8", "Kaiju No. 8 v03.cbz"), 2)

	res := s.scanManga(context.Background(), 0)
	if res.Problem != "" || res.Claimed != 2 || res.Files != 6 {
		t.Fatalf("%+v", res)
	}
	if res.Groups[0].Name != "Unknown Tale" || res.Groups[0].SeriesID != 0 {
		t.Fatalf("what's not in the library comes first: %+v", res.Groups)
	}
	var mine mangaScanGroup
	for _, g := range res.Groups {
		if g.SeriesID == sr.ID {
			mine = g
		}
	}
	if mine.Chapters != 1 || mine.Packs != 1 || mine.Doubles != 1 {
		t.Fatalf("%+v", mine)
	}
	volumes, _ := repo.Volumes(sr.ID)
	if volumes[0].Status != manga.StatusDownloaded || volumes[1].Status != manga.StatusDownloaded || volumes[2].Status != manga.StatusMissing {
		t.Fatalf("%+v", volumes)
	}
	if volumes[0].SourceClass != string(manga.ClassOwn) {
		t.Fatalf("class %q", volumes[0].SourceClass)
	}
	// Files stay: nothing was moved or renamed.
	if _, err := os.Stat(filepath.Join(root, "Kaiju Hachigo v02.cbz")); err != nil {
		t.Fatal(err)
	}
	// A file that went away is forgotten, and not searched for by itself.
	_ = os.Remove(filepath.Join(root, "Kaiju Hachigo v02.cbz"))
	res = s.scanManga(context.Background(), 0)
	if res.Gone != 1 {
		t.Fatalf("%+v", res)
	}
	if v, _ := repo.GetVolume(volumes[1].ID); v.Status != manga.StatusMissing || v.Wanted {
		t.Fatalf("%+v", v)
	}
	// An unmounted or empty folder undoes nothing.
	_ = os.RemoveAll(root)
	if res = s.scanManga(context.Background(), 0); res.Problem != "no-access" {
		t.Fatalf("%+v", res)
	}
	if v, _ := repo.GetVolume(volumes[0].ID); v.Status != manga.StatusDownloaded {
		t.Fatalf("a missing folder undid the library: %+v", v)
	}
}

// The nightly scan keeps to its hours.
func TestMangaScanJobHours(t *testing.T) {
	if mangaScanFromHour != 2 || mangaScanToHour != 9 {
		t.Fatal("heavy scans run between 2 and 9")
	}
}

const fakeAniListSearch = `{"data":{"Page":{"media":[
 {"id":7,"title":{"romaji":"Chochin Minato","english":"Lanternfish Harbor","native":"提灯港"},"synonyms":["LH"],"format":"MANGA","status":"FINISHED","startDate":{"year":2015},"volumes":3,"countryOfOrigin":"JP","isAdult":false,"description":"d","coverImage":{"large":"https://s4.anilist.co/c.jpg"},"siteUrl":"","staff":{"edges":[]},"externalLinks":[{"site":"Viz","url":"https://example.test/v","type":"INFO","language":"English"}]},
 {"id":8,"title":{"romaji":"Otona","english":"Grown Up","native":null},"synonyms":[],"format":"MANGA","status":"FINISHED","startDate":{"year":2015},"volumes":3,"countryOfOrigin":"JP","isAdult":true,"description":"","coverImage":{"large":""},"siteUrl":"","staff":{"edges":[]},"externalLinks":[]},
 {"id":9,"title":{"romaji":"Hon","english":"A Novel","native":null},"synonyms":[],"format":"NOVEL","status":"FINISHED","startDate":{"year":2015},"volumes":3,"countryOfOrigin":"JP","isAdult":false,"description":"","coverImage":{"large":""},"siteUrl":"","staff":{"edges":[]},"externalLinks":[]}
]}}}`

func TestMangaLookupAndAdd(t *testing.T) {
	fakeAni := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Variables["id"] != nil {
			var wrap struct {
				Data struct {
					Page struct {
						Media []json.RawMessage `json:"media"`
					} `json:"Page"`
				} `json:"data"`
			}
			_ = json.Unmarshal([]byte(fakeAniListSearch), &wrap)
			_, _ = w.Write([]byte(`{"data":{"Media":` + string(wrap.Data.Page.Media[0]) + `}}`))
			return
		}
		if body.Variables["q"] == "nothing here" {
			_, _ = w.Write([]byte(`{"data":{"Page":{"media":[]}}}`))
			return
		}
		_, _ = w.Write([]byte(fakeAniListSearch))
	}))
	defer fakeAni.Close()
	t.Setenv(mangaAniListURL, fakeAni.URL)
	s, repo, root := mangaTestServer(t)
	s.mangaMod().anilist.Gap = 0
	writeCBZ(t, filepath.Join(root, "Lanternfish Harbor", "Lanternfish Harbor v02.cbz"), 2)

	get := func(q string) map[string]any {
		rec := httptest.NewRecorder()
		s.handleMangaLookup(rec, httptest.NewRequest(http.MethodGet, "/api/manga/lookup?q="+strings.ReplaceAll(q, " ", "+"), nil))
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: %v", rec.Body.String(), err)
		}
		return out
	}
	out := get("lantern")
	results := out["results"].([]any)
	if len(results) != 1 || out["hiddenAdult"].(float64) != 1 || out["hiddenNovels"].(float64) != 1 {
		t.Fatalf("%+v", out)
	}
	if list, _ := repo.ListSeries(); len(list) != 0 {
		t.Fatal("a search saved something")
	}
	if results := get("nothing here")["results"].([]any); len(results) != 0 {
		t.Fatal("expected no results")
	}

	rec := httptest.NewRecorder()
	s.handleAddManga(rec, httptest.NewRequest(http.MethodPost, "/api/manga", strings.NewReader(`{"anilistId":7,"want":"new"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	sr, err := repo.SeriesByAniList(7)
	if err != nil {
		t.Fatal(err)
	}
	if sr.Title != "Lanternfish Harbor" || sr.WantAll || !sr.MonitorNew || sr.Publisher != "VIZ" || sr.LicensedEN != 1 {
		t.Fatalf("%+v", sr)
	}
	volumes, _ := repo.Volumes(sr.ID)
	if len(volumes) != 3 || volumes[0].Wanted || volumes[1].Status != manga.StatusDownloaded {
		t.Fatalf("new volumes only: %+v", volumes)
	}
	names := repo.Names(sr)
	if len(names) != 4 || names[0] != "Lanternfish Harbor" || names[1] != "Chochin Minato" {
		t.Fatalf("names %q", names)
	}
	// Adding it twice is the same series.
	rec = httptest.NewRecorder()
	s.handleAddManga(rec, httptest.NewRequest(http.MethodPost, "/api/manga", strings.NewReader(`{"anilistId":7,"want":"all"}`)))
	if list, _ := repo.ListSeries(); len(list) != 1 || rec.Code != http.StatusOK {
		t.Fatalf("%d %+v", rec.Code, list)
	}
	// By hand, with no AniList.
	rec = httptest.NewRecorder()
	s.handleAddManga(rec, httptest.NewRequest(http.MethodPost, "/api/manga", strings.NewReader(`{"title":"Unknown Tale","want":"none"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestMangaWhySentenceData(t *testing.T) {
	now := time.Now().UTC()
	later := now.Add(48 * time.Hour).Format(time.RFC3339)
	soon := now.Add(24 * time.Hour).Format(time.RFC3339)
	log := []manga.SearchEntry{
		{VolumeID: 5, SearchedAt: now.Format(time.RFC3339), Outcome: "nothing", NextAt: later, Summary: manga.SearchSummary{Found: 4}},
		{VolumeID: 0, SearchedAt: now.Add(-time.Hour).Format(time.RFC3339), Outcome: "nothing", NextAt: soon},
		{VolumeID: 9, SearchedAt: now.Add(-time.Hour).Format(time.RFC3339), Outcome: "nothing", NextAt: now.Format(time.RFC3339)}, // a volume you have now
	}
	volumes := []manga.Volume{{ID: 5, Number: "14", Wanted: true, Status: manga.StatusMissing}, {ID: 9, Number: "2", Wanted: true, Status: manga.StatusDownloaded}}
	st := mangaWhy(log, volumes)
	if st.Last == nil || st.Number != "14" || st.Last.Summary.Found != 4 || st.NextLook != soon {
		t.Fatalf("%+v", st)
	}
}

// A manga folder that can't be written stops searches and grabs before
// anything is fetched, and says why; a good one says nothing.
func TestMangaFolderProblemStopsGrabs(t *testing.T) {
	s, _, _ := bigServer(t)
	good := t.TempDir()
	if err := s.Settings.Set(keyMangaPath, good, false); err != nil {
		t.Fatal(err)
	}
	if p := s.mangaFolderProblem(); p != "" {
		t.Fatalf("a writable folder: %q", p)
	}
	if left, _ := os.ReadDir(good); len(left) != 0 {
		t.Fatalf("the check left something behind: %v", left)
	}
	// A plain file stands where the folder should be, so nothing can go there.
	blocked := filepath.Join(good, "not-a-folder")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Settings.Set(keyMangaPath, blocked, false); err != nil {
		t.Fatal(err)
	}
	if p := s.mangaFolderProblem(); !strings.Contains(p, "can't save") {
		t.Fatalf("a blocked folder: %q", p)
	}
	_, err := s.grabManga(manga.Series{}, 0, "Sample v01", "http://example.invalid/x.nzb", 1, indexers.ProtocolUsenet, "official", queue.PriorityAutomatic, false)
	if err == nil || !strings.Contains(err.Error(), "can't save") {
		t.Fatalf("a grab went ahead with nowhere to file it: %v", err)
	}
}

// A pack taken for a series counts as the download of each volume it holds,
// so the volumes aren't fetched one by one while it runs.
func TestMangaPackCoversItsVolumes(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	sr := addSeries(t, repo, "One-Punch Man", 35, 1)
	id, err := s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: "One-Punch Man v01-33 (2014-2026) (Digital) (1r0n, Rillant)", NZBURL: "magnet:?xt=urn:btih:x", Protocol: queue.Protocol("torrent")})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkDownload(manga.Download{QueueID: id, SeriesID: sr.ID, Class: string(manga.ClassFan)}); err != nil {
		t.Fatal(err)
	}
	volumes, _ := repo.Volumes(sr.ID)
	for _, v := range volumes {
		busy, err := repo.ActiveForVolume(v.ID)
		if err != nil {
			t.Fatal(err)
		}
		if want := v.Number != "34" && v.Number != "35"; busy != want {
			t.Fatalf("volume %s busy=%v", v.Number, busy)
		}
	}
	_ = s.QueueRepo.SetStatus(id, queue.StatusFailed, "gone")
	if busy, _ := repo.ActiveForVolume(volumes[0].ID); busy {
		t.Fatal("a failed pack still covers its volumes")
	}
}
