package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/queue"
)

const (
	dexTestManga = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	dexCh1       = "11111111-1111-1111-1111-111111111111"
	dexCh2       = "22222222-2222-2222-2222-222222222222"
	dexChOther   = "33333333-3333-3333-3333-333333333333"
	dexChExt     = "44444444-4444-4444-4444-444444444444"
	dexChGone    = "55555555-5555-5555-5555-555555555555"
)

// fakeDex is a small MangaDex; no test asks the real service.
type fakeDex struct {
	*httptest.Server
	mu       sync.Mutex
	hits     map[string]int
	reports  []map[string]any
	feed     string
	status   int // answer the API with this
	failPage map[string]int
	page     []byte
	onImage  func(string)
}

func dexChapterJSON(id, number, volume, group string, pages int, external string, gone bool) string {
	ext := "null"
	if external != "" {
		ext = fmt.Sprintf("%q", external)
	}
	rel := ""
	if group != "" {
		rel = fmt.Sprintf(`{"id":"group-%s","type":"scanlation_group","attributes":{"name":%q}}`, group, group)
	}
	return fmt.Sprintf(`{"id":%q,"attributes":{"volume":%q,"chapter":%q,"title":"Title %s","translatedLanguage":"en","externalUrl":%s,"isUnavailable":%v,"pages":%d,"publishAt":"2024-01-01T00:00:00+00:00"},"relationships":[%s]}`, id, volume, number, number, ext, gone, pages, rel)
}

func newFakeDex(t *testing.T, chapters ...string) *fakeDex {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	f := &fakeDex{hits: map[string]int{}, failPage: map[string]int{}, page: buf.Bytes()}
	f.feed = fmt.Sprintf(`{"total":%d,"data":[%s]}`, len(chapters), strings.Join(chapters, ","))
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		f.mu.Lock()
		f.hits[path]++
		status, feed := f.status, f.feed
		f.mu.Unlock()
		switch {
		case path == "/network/report":
			var rep map[string]any
			_ = json.NewDecoder(r.Body).Decode(&rep)
			f.mu.Lock()
			f.reports = append(f.reports, rep)
			f.mu.Unlock()
		case strings.HasPrefix(path, "/img/"):
			if f.onImage != nil {
				f.onImage(path)
			}
			f.mu.Lock()
			fail := f.failPage[filepath.Base(path)] > 0
			if fail {
				f.failPage[filepath.Base(path)]--
			}
			f.mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("X-Cache", "MISS")
			_, _ = w.Write(f.page)
		case status != 0:
			w.WriteHeader(status)
		case strings.HasPrefix(path, "/at-home/server/"):
			_, _ = fmt.Fprintf(w, `{"result":"ok","baseUrl":%q,"chapter":{"hash":"hash","data":["p1.png","p2.png"],"dataSaver":["s1.jpg","s2.jpg"]}}`, f.URL+"/img")
		case strings.HasSuffix(path, "/feed"):
			_, _ = io.WriteString(w, feed)
		case path == "/manga":
			_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"attributes":{"title":{"en":"Tower"},"links":{},"year":2020,"availableTranslatedLanguages":["en"]}}]}`, dexTestManga)
		case strings.HasPrefix(path, "/manga/"):
			_, _ = fmt.Fprintf(w, `{"data":{"id":%q,"attributes":{"title":{"en":"Tower"},"links":{"engtl":"https://publisher.example/tower"},"availableTranslatedLanguages":["en","es"]}}}`, dexTestManga)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeDex) count(prefix string) int {
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

func (f *fakeDex) total() int { return f.count("/") }

// dexServer is a server pointed at the fake, with MangaDex switched on.
func dexServer(t *testing.T, f *fakeDex) (*Server, *manga.Repo, string) {
	t.Helper()
	s, repo, root := mangaTestServer(t)
	mangaDexNoGap = true
	st := s.mangaMod()
	st.dex = manga.NewMangaDex("Usherr/test", f.URL)
	st.dex.APIGap, st.dex.AtHomeGap, st.dex.ImageGap = 0, 0, 0
	if err := s.Settings.Set(keyMangaSourceDex, "1", false); err != nil {
		t.Fatal(err)
	}
	return s, repo, root
}

func dexSeriesIn(t *testing.T, s *Server, repo *manga.Repo, follow string) manga.Series {
	t.Helper()
	sr := addSeries(t, repo, "Tower", 0, -1)
	if err := repo.SaveDexState(manga.DexState{SeriesID: sr.ID, DexID: dexTestManga, Link: manga.DexLinked, Follow: follow}); err != nil {
		t.Fatal(err)
	}
	return sr
}

func dexBatch(t *testing.T, s *Server, repo *manga.Repo, sr manga.Series) int64 {
	t.Helper()
	list, _, err := repo.ChaptersToFetch(sr.ID, time.Now(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("nothing to fetch")
	}
	id, err := s.grabMangaDexBatch(sr, list, queue.PriorityManual)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMangaDexAsksNothingUntilSwitchedOn(t *testing.T) {
	f := newFakeDex(t)
	s, repo, _ := dexServer(t, f)
	dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.Settings.Set(keyMangaSourceDex, "0", false); err != nil {
		t.Fatal(err)
	}
	s.mangaDexJob(context.Background())
	if f.total() != 0 {
		t.Fatalf("asked MangaDex %d times with the switch off", f.total())
	}
	if err := s.Settings.Set(keyMangaSourceDex, "1", false); err != nil {
		t.Fatal(err)
	}
	s.mangaDexJob(context.Background())
	if f.count("/manga/") == 0 {
		t.Fatal("switched on, a followed series should be looked at")
	}
}

func TestMangaDexSkipsASeriesThatChoseOfficialOnly(t *testing.T) {
	f := newFakeDex(t)
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := repo.SetSourceMode(sr.ID, "official"); err != nil {
		t.Fatal(err)
	}
	s.mangaDexJob(context.Background())
	if f.total() != 0 {
		t.Fatal("official only means no MangaDex for this series")
	}
}

func TestMangaDexLinksByTitleAndAsksWhenUnsure(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Inkwell", 2, "", false))
	s, repo, _ := dexServer(t, f)
	sr, err := repo.AddSeries(manga.Series{Title: "Tower", LicensedEN: -1, StartYear: 2020, CheckedAt: time.Now().UTC().Format(time.RFC3339)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	if d := repo.DexStateOf(sr.ID); d.Link != manga.DexLinked || d.DexID != dexTestManga || d.LinkHow != "title" {
		t.Fatalf("%+v", d)
	}
	// The wrong years: nothing is assumed, and a person gets to choose.
	far, err := repo.AddSeries(manga.Series{Title: "Tower", LicensedEN: -1, StartYear: 1980, CheckedAt: time.Now().UTC().Format(time.RFC3339)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := f.count("/manga/")
	if err := s.checkMangaDexSeries(context.Background(), far); err != nil {
		t.Fatal(err)
	}
	d := repo.DexStateOf(far.ID)
	if d.Link != manga.DexUnsure || len(d.Candidates) != 1 || d.DexID != "" {
		t.Fatalf("%+v", d)
	}
	if f.count("/manga/") != before {
		t.Fatal("no chapters are asked for an unconfirmed entry")
	}
}

func TestMangaDexBatchFilesCreditsAndReports(t *testing.T) {
	f := newFakeDex(t,
		dexChapterJSON(dexCh1, "1", "1", "Inkwell", 2, "", false),
		dexChapterJSON(dexChOther, "1", "1", "Quick", 1, "", false), // a second group's chapter 1
		dexChapterJSON(dexCh2, "2", "1", "Inkwell", 2, "", false),
		dexChapterJSON(dexChExt, "3", "1", "Publisher", 0, "https://publisher.example/read", false),
		dexChapterJSON(dexChGone, "4", "1", "Gone Group", 40, "", true),
	)
	s, repo, root := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	d := repo.DexStateOf(sr.ID)
	if d.Stats.Listed != 4 || d.Stats.Readable != 2 || d.Stats.External != 1 || d.Stats.Unavailable != 1 {
		t.Fatalf("stats %+v", d.Stats)
	}
	list, _ := repo.Chapters(sr.ID)
	if len(list) != 4 || list[0].DexID != dexCh1 || list[0].Group != "Inkwell" || list[0].Copies != 2 {
		t.Fatalf("the group with more chapters should be chosen: %+v", list[0])
	}
	if list[2].State != "external" || list[2].Wanted || list[3].State != "unavailable" || list[3].Wanted {
		t.Fatalf("external and removed chapters are shown and skipped: %+v %+v", list[2], list[3])
	}
	id := dexBatch(t, s, repo, sr)
	if err := s.runMangaDexBatch(id, sr); err != nil {
		t.Fatal(err)
	}
	it, err := s.QueueRepo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Status != queue.StatusCompleted || it.ReleaseTitle != "Tower, 2 chapters" {
		t.Fatalf("Activity shows one line for the batch: %+v", it)
	}
	file := filepath.Join(root, "Tower", "Chapters", "Tower c0001 [Inkwell].cbz")
	zr, err := zip.OpenReader(file)
	if err != nil {
		t.Fatal("filed under Series/Chapters: ", err)
	}
	defer zr.Close()
	var names []string
	var info string
	for _, zf := range zr.File {
		names = append(names, zf.Name)
		if zf.Name == "ComicInfo.xml" {
			rc, _ := zf.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			info = string(b)
		}
	}
	if strings.Join(names, " ") != "0001.png 0002.png ComicInfo.xml" {
		t.Fatalf("files: %v", names)
	}
	if !strings.Contains(info, "<Translator>Inkwell</Translator>") || !strings.Contains(info, "MangaDex") {
		t.Fatal(info)
	}
	if f.count("/at-home/server/"+dexChOther) != 0 || f.count("/at-home/server/"+dexChExt) != 0 || f.count("/at-home/server/"+dexChGone) != 0 {
		t.Fatal("only the chosen chapters that can be downloaded are fetched")
	}
	s.mangaMod().dex.WaitReports()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reports) != 4 {
		t.Fatalf("every image is reported: %d", len(f.reports))
	}
	if f.reports[0]["success"] != true || f.reports[0]["bytes"].(float64) == 0 {
		t.Fatalf("%+v", f.reports[0])
	}
}

func TestMangaDexFailedPageIsReportedAndAddressAskedAgain(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Inkwell", 2, "", false))
	f.failPage["p1.png"] = 1
	s, repo, root := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	if err := s.runMangaDexBatch(dexBatch(t, s, repo, sr), sr); err != nil {
		t.Fatal(err)
	}
	if n := f.count("/at-home/server/"); n != 2 {
		t.Fatalf("the docs say: report, then ask at-home again. Asked %d times", n)
	}
	s.mangaMod().dex.WaitReports()
	f.mu.Lock()
	failed := 0
	for _, r := range f.reports {
		if r["success"] == false {
			failed++
		}
	}
	f.mu.Unlock()
	if failed != 1 {
		t.Fatalf("%d failed reports", failed)
	}
	if _, err := os.Stat(filepath.Join(root, "Tower", "Chapters", "Tower c0001 [Inkwell].cbz")); err != nil {
		t.Fatal(err)
	}
}

func TestMangaDexGivesUpOnAChapterAfterAFewTries(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Inkwell", 2, "", false))
	f.failPage["p1.png"] = 100
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	id := dexBatch(t, s, repo, sr)
	if err := s.runMangaDexBatch(id, sr); err == nil {
		t.Fatal("nothing could be fetched: it should fail")
	}
	if n := f.count("/img/"); n > mangaDexPageTries {
		t.Fatalf("hammered: %d image requests", n)
	}
	list, _ := repo.Chapters(sr.ID)
	if list[0].Problem == "" {
		t.Fatal("the reason should be on the chapter")
	}
	if todo, _, _ := repo.ChaptersToFetch(sr.ID, time.Now(), 10); len(todo) != 0 {
		t.Fatal("it should wait before trying again")
	}
	if todo, _, _ := repo.ChaptersToFetch(sr.ID, time.Now().Add(2*time.Hour), 10); len(todo) != 1 {
		t.Fatal("it should come back later")
	}
}

func TestMangaDexTakedownSaysWhy(t *testing.T) {
	// What the real feed says of a licensed series: every group's chapter taken down,
	// and the publisher's chapter a link to its own reader.
	f := newFakeDex(t,
		dexChapterJSON(dexCh1, "1", "1", "Fans", 47, "", true),
		dexChapterJSON(dexChExt, "1", "1", "Publisher", 0, "https://publisher.example/read", false),
		dexChapterJSON(dexCh2, "2", "1", "Fans", 33, "", true),
	)
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	d := repo.DexStateOf(sr.ID)
	if d.Stats.Readable != 0 || d.Stats.External != 1 || d.Stats.Unavailable != 1 || d.Stats.LicensedLink != "https://publisher.example/tower" {
		t.Fatalf("%+v", d.Stats)
	}
	if todo, _, _ := repo.ChaptersToFetch(sr.ID, time.Now(), 10); len(todo) != 0 {
		t.Fatal("nothing can be fetched")
	}
	sum := s.mangaChapterSummary(sr)
	if sum.Stats.Readable != 0 || sum.Empties != 1 {
		t.Fatalf("the page needs the reason: %+v", sum)
	}
	next, _ := time.Parse(time.RFC3339, d.NextAt)
	if time.Until(next) < 20*time.Hour {
		t.Fatalf("a series with nothing asks less often: %v", time.Until(next))
	}
	// And less often again, so it can't go 225 times with nothing to show.
	d.NextAt = ""
	_ = repo.SaveDexState(d)
	for range 3 {
		if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
			t.Fatal(err)
		}
	}
	d = repo.DexStateOf(sr.ID)
	next, _ = time.Parse(time.RFC3339, d.NextAt)
	if d.Empties != 4 || time.Until(next) < 25*24*time.Hour {
		t.Fatalf("after %d empties it waits %v", d.Empties, time.Until(next))
	}
}

func TestMangaDexEmptyFeedKeepsRowsAndSaysSo(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Fans", 5, "", false))
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowNew)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	list, _ := repo.Chapters(sr.ID)
	if len(list) != 1 || list[0].Wanted {
		t.Fatalf("new chapters only: the first look sets the starting line: %+v", list)
	}
	f.mu.Lock()
	f.feed = `{"total":0,"data":[]}`
	f.mu.Unlock()
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	list, _ = repo.Chapters(sr.ID)
	if len(list) != 1 || !list[0].Unavailable {
		t.Fatalf("a chapter that vanished is said to be gone: %+v", list)
	}
}

func TestMangaDexNewChapterIsWantedAfterTheStartingLine(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Fans", 2, "", false))
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowNew)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.feed = fmt.Sprintf(`{"total":2,"data":[%s,%s]}`, dexChapterJSON(dexCh1, "1", "", "Fans", 2, "", false), dexChapterJSON(dexCh2, "2", "", "Fans", 2, "", false))
	f.mu.Unlock()
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	list, _ := repo.Chapters(sr.ID)
	if list[0].Wanted || !list[1].Wanted {
		t.Fatalf("%+v", list)
	}
}

func TestMangaDexNoDuplicates(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Inkwell", 2, "", false), dexChapterJSON(dexCh2, "2", "", "Inkwell", 2, "", false))
	s, repo, root := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	// Chapter 2 is already in the folder: from an earlier life of the series, say.
	folder := filepath.Join(root, "Tower", "Chapters")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	there := filepath.Join(folder, "Tower c0002 [Inkwell].cbz")
	if err := os.WriteFile(there, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	list, _ := repo.Chapters(sr.ID)
	if list[1].Status != manga.StatusDownloaded || list[1].FilePath != there {
		t.Fatalf("a file that's there is claimed, not fetched: %+v", list[1])
	}
	if err := s.runMangaDexBatch(dexBatch(t, s, repo, sr), sr); err != nil {
		t.Fatal(err)
	}
	if f.count("/at-home/server/"+dexCh2) != 0 {
		t.Fatal("fetched a chapter that was already here")
	}
	first := f.count("/img/")
	if b, _ := os.ReadFile(there); string(b) != "kept" {
		t.Fatal("the file there was overwritten")
	}
	// Later looks change nothing, and a second group's copy doesn't bring a second file.
	f.mu.Lock()
	f.feed = fmt.Sprintf(`{"total":2,"data":[%s,%s]}`, dexChapterJSON(dexCh1, "1", "", "Inkwell", 2, "", false), dexChapterJSON(dexChOther, "1", "", "Quicker", 9, "", false))
	f.mu.Unlock()
	if err := repo.SaveDexState(func() manga.DexState { d := repo.DexStateOf(sr.ID); d.PreferGroup = "group-Quicker"; return d }()); err != nil {
		t.Fatal(err)
	}
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	s.startMangaDexBatches(time.Now())
	if todo, _, _ := repo.ChaptersToFetch(sr.ID, time.Now(), 10); len(todo) != 0 {
		t.Fatalf("nothing is left to fetch: %+v", todo)
	}
	if busy, _ := repo.ActiveDexBatch(sr.ID); busy {
		t.Fatal("no batch should start")
	}
	if f.count("/img/") != first {
		t.Fatal("pages were fetched again")
	}
	list, _ = repo.Chapters(sr.ID)
	if list[0].Group != "Inkwell" {
		t.Fatalf("a chapter that's here keeps its group: %+v", list[0])
	}
}

func TestMangaDexResumesAfterAStop(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Inkwell", 2, "", false), dexChapterJSON(dexCh2, "2", "", "Inkwell", 2, "", false))
	s, repo, root := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	id := dexBatch(t, s, repo, sr)
	// The power goes during chapter 1, with one page in.
	dir := s.workDirFor(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	chapters, err := repo.BatchChapters(id)
	if err != nil || len(chapters) != 2 {
		t.Fatal(err, chapters)
	}
	if _, err := s.fetchMangaDexChapter(ctx, sr, chapters[0], dir, func(n int64) { cancel() }); err == nil {
		t.Fatal("it was stopped: it shouldn't have finished")
	}
	before := f.count("/img/data/hash/p1.png")
	if before != 1 {
		t.Fatalf("%d", before)
	}
	// A restart: a new client, as after any restart, and the same batch carries on.
	st := s.mangaMod()
	st.dex = manga.NewMangaDex("Usherr/test", f.URL)
	st.dex.APIGap, st.dex.AtHomeGap, st.dex.ImageGap = 0, 0, 0
	if err := s.runMangaDexBatch(id, sr); err != nil {
		t.Fatal(err)
	}
	// Chapter 1 didn't ask for its first page again; chapter 2 asked for its own.
	if n := f.count("/img/data/hash/p1.png"); n != before+1 {
		t.Fatalf("p1.png fetched %d times in all; it should be once for each chapter", n)
	}
	for _, name := range []string{"Tower c0001 [Inkwell].cbz", "Tower c0002 [Inkwell].cbz"} {
		if _, err := os.Stat(filepath.Join(root, "Tower", "Chapters", name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := s.existingWorkDir(id); ok {
		t.Fatal("the work folder should be gone when the batch is done")
	}
}

func TestMangaDexBatchLeavesTheDownloadLineAlone(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Inkwell", 2, "", false))
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	id := dexBatch(t, s, repo, sr)
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	freed := make(chan struct{})
	var once sync.Once
	// A page won't come until the batch's place has been given back. Were the place
	// held for the whole batch, this would never finish.
	f.onImage = func(string) {
		select {
		case <-freed:
		case <-time.After(10 * time.Second):
			t.Error("the batch held its place in the line while it downloaded")
		}
	}
	if err := s.startQueued(item, func() { once.Do(func() { close(freed) }) }); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if it, err := s.QueueRepo.Get(id); err == nil && it.Status == queue.StatusCompleted {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the batch didn't finish")
}

func TestMangaDexStoppedBatchStaysStopped(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Inkwell", 2, "", false))
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	id := dexBatch(t, s, repo, sr)
	item, err := s.QueueRepo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.stopItem(item, false); err != nil {
		t.Fatal(err)
	}
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	if todo, _, _ := repo.ChaptersToFetch(sr.ID, time.Now(), 10); len(todo) != 0 {
		t.Fatalf("a person stopped it, so it mustn't come back by itself: %+v", todo)
	}
}

func TestMangaDexBacksOffWhenDown(t *testing.T) {
	f := newFakeDex(t)
	f.status = http.StatusBadGateway
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	s.mangaDexJob(context.Background())
	if f.total() != 1 {
		t.Fatalf("asked %d times", f.total())
	}
	d := repo.DexStateOf(sr.ID)
	if d.Trouble != manga.DexDown {
		t.Fatalf("the reason should be kept: %+v", d)
	}
	next, _ := time.Parse(time.RFC3339, d.NextAt)
	if time.Until(next) < 10*time.Minute {
		t.Fatalf("waits only %v", time.Until(next))
	}
	away := s.mangaDexAway()
	if away == nil || away.Kind != manga.DexDown {
		t.Fatalf("the page should be told: %+v", away)
	}
	// Later runs ask nothing, and neither does anything after a restart.
	s.mangaDexJob(context.Background())
	st := s.mangaMod()
	st.dex = manga.NewMangaDex("Usherr/test", f.URL)
	s.restoreDexPause(st.dex)
	s.mangaDexJob(context.Background())
	if f.total() != 1 {
		t.Fatalf("hammered: %d", f.total())
	}
	// A bad answer waits longer each time.
	if dexBackoff(1) >= dexBackoff(3) || dexBackoff(20) != 6*time.Hour {
		t.Fatalf("%v %v %v", dexBackoff(1), dexBackoff(3), dexBackoff(20))
	}
}

func TestMangaDexBusyStopsTheBatch(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Inkwell", 2, "", false), dexChapterJSON(dexCh2, "2", "", "Inkwell", 2, "", false))
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	id := dexBatch(t, s, repo, sr)
	f.mu.Lock()
	f.status = http.StatusTooManyRequests
	f.mu.Unlock()
	if err := s.runMangaDexBatch(id, sr); err == nil {
		t.Fatal("it should stop")
	}
	if n := f.count("/at-home/server/"); n != 1 {
		t.Fatalf("after the 429 it asked again: %d", n)
	}
	it, _ := s.QueueRepo.Get(id)
	if it.Status != queue.StatusFailed || !strings.Contains(it.Error, "slow down") {
		t.Fatalf("the reason should show in Activity: %+v", it)
	}
	if todo, _, _ := repo.ChaptersToFetch(sr.ID, time.Now(), 10); len(todo) != 2 {
		t.Fatal("the chapters stay wanted")
	}
}

func TestMangaDexBigCatchUpWaitsForTheNight(t *testing.T) {
	var chapters []string
	for n := 1; n <= mangaDexDayBatch+5; n++ {
		chapters = append(chapters, dexChapterJSON(fmt.Sprintf("%08d-0000-0000-0000-000000000000", n), fmt.Sprint(n), "", "G", 2, "", false))
	}
	f := newFakeDex(t, chapters...)
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	noon := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	s.startMangaDexBatches(noon)
	if busy, _ := repo.ActiveDexBatch(sr.ID); busy {
		t.Fatal("a big catch-up shouldn't start by day")
	}
	night := time.Date(2026, 11, 7, 3, 0, 0, 0, time.Local)
	s.startMangaDexBatches(night)
	if busy, _ := repo.ActiveDexBatch(sr.ID); !busy {
		t.Fatal("it should start in the window")
	}
}

func TestMangaDexVolumeArrivesOverChapters(t *testing.T) {
	f := newFakeDex(t,
		dexChapterJSON(dexCh1, "1", "1", "Inkwell", 20, "", false),
		dexChapterJSON(dexCh2, "2", "1", "Inkwell", 30, "", false),
		dexChapterJSON(dexChOther, "3", "2", "Inkwell", 25, "", false),
	)
	s, repo, root := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	if err := s.runMangaDexBatch(dexBatch(t, s, repo, sr), sr); err != nil {
		t.Fatal(err)
	}
	sr, _ = repo.GetSeries(sr.ID)
	list, _ := repo.Chapters(sr.ID)
	if list[0].Status != manga.StatusDownloaded {
		t.Fatalf("%+v", list[0])
	}
	// Someone has read chapter 1, and is on page 7 of chapter 2.
	user := mangaUser(t, s, "reader")
	if err := repo.MarkChaptersRead(user, []int64{list[0].ID}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO home_positions (user_id, item, position, duration, updated_ms) VALUES (?, ?, 7, 30, 1)`, user, manga.ChapterItem(list[1].ID)); err != nil {
		t.Fatal(err)
	}
	// Volume 1 arrives from another source.
	vol, _, err := repo.EnsureVolume(sr.ID, "1", true)
	if err != nil {
		t.Fatal(err)
	}
	volFile := filepath.Join(root, "Tower", "Tower v01.cbz")
	if err := os.WriteFile(volFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetVolumeFile(vol.ID, volFile, 1, "official"); err != nil {
		t.Fatal(err)
	}
	list, _ = repo.Chapters(sr.ID)
	if list[0].State != "in_volume" || list[0].InVolume != "1" || list[1].State != "in_volume" || list[2].State == "in_volume" {
		t.Fatalf("chapters should show In Volume 1: %+v", list)
	}
	// The place carries over, counted in pages: chapter 1 (20) + 7.
	var pos float64
	if err := s.db.QueryRow(`SELECT position FROM home_positions WHERE user_id = ? AND item = ?`, user, manga.PositionItem(vol.ID)).Scan(&pos); err != nil || pos != 27 {
		t.Fatalf("place %v %v", pos, err)
	}
	row, err := repo.Continue(user, sr.ID, 10)
	if err != nil || len(row) != 1 || row[0].VolumeID != vol.ID || row[0].Page != 27 {
		t.Fatalf("Continue should be in the volume: %+v %v", row, err)
	}
	// Reading the volume counts for its chapters.
	if err := repo.MarkRead(user, []int64{vol.ID}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = repo.FillChapterReading(user, sr.ID, list)
	if !list[1].Read || list[2].Read {
		t.Fatalf("%+v", list)
	}
	// Tidy sets them aside; nothing is deleted, and it can be undone.
	before := list[1].FilePath
	n, err := s.tidyMangaChapters(sr, false)
	if err != nil || n != 2 {
		t.Fatalf("moved %d: %v", n, err)
	}
	list, _ = repo.Chapters(sr.ID)
	if _, err := os.Stat(before); !os.IsNotExist(err) {
		t.Fatal("the chapter should have moved out of the way")
	}
	if !list[1].SetAside || !strings.Contains(list[1].FilePath, ".replaced") {
		t.Fatalf("%+v", list[1])
	}
	if _, err := os.Stat(list[1].FilePath); err != nil {
		t.Fatal("set aside, not deleted: ", err)
	}
	if _, err := os.Stat(list[2].FilePath); err != nil {
		t.Fatal("a chapter no volume holds stays: ", err)
	}
	n, err = s.tidyMangaChapters(sr, true)
	if err != nil || n != 2 {
		t.Fatalf("undo moved %d: %v", n, err)
	}
	if _, err := os.Stat(before); err != nil {
		t.Fatal("undo should put it back: ", err)
	}
	// A chapter a volume holds isn't fetched by a later batch.
	if err := repo.ClearChapterFile(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetChaptersWanted([]int64{list[0].ID}, true); err != nil {
		t.Fatal(err)
	}
	if todo, _, _ := repo.ChaptersToFetch(sr.ID, time.Now(), 10); len(todo) != 0 {
		t.Fatalf("%+v", todo)
	}
}

func TestMangaReadingRunsAcrossChaptersAndVolumes(t *testing.T) {
	f := newFakeDex(t, dexChapterJSON(dexCh1, "1", "", "Inkwell", 2, "", false), dexChapterJSON(dexCh2, "2", "", "Inkwell", 2, "", false))
	s, repo, _ := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	if err := s.checkMangaDexSeries(context.Background(), sr); err != nil {
		t.Fatal(err)
	}
	if err := s.runMangaDexBatch(dexBatch(t, s, repo, sr), sr); err != nil {
		t.Fatal(err)
	}
	list, _ := repo.Chapters(sr.ID)
	user := mangaUser(t, s, "reader")
	// Stopped part way through: Continue is that chapter.
	if _, err := s.db.Exec(`INSERT INTO home_positions (user_id, item, position, duration, updated_ms) VALUES (?, ?, 1.5, 2, 5)`, user, manga.ChapterItem(list[0].ID)); err != nil {
		t.Fatal(err)
	}
	row, err := repo.Continue(user, 0, 10)
	if err != nil || len(row) != 1 || row[0].ChapterID != list[0].ID {
		t.Fatalf("%+v %v", row, err)
	}
	// The last page finishes it, and Continue moves to the next chapter.
	s.mangaPlaceSaved(user, manga.ChapterItem(list[0].ID), 2, 2)
	if _, err := s.db.Exec(`DELETE FROM home_positions WHERE user_id = ? AND item = ?`, user, manga.ChapterItem(list[0].ID)); err != nil {
		t.Fatal(err)
	}
	row, err = repo.Continue(user, 0, 10)
	if err != nil || len(row) != 1 || row[0].ChapterID != list[1].ID || row[0].Number != "2" {
		t.Fatalf("%+v %v", row, err)
	}
	// The reader's last page offers the next chapter.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	out := map[string]any{}
	s.fillMangaNext(out, req, sr.ID, true, list[0].ID)
	next, ok := out["next"].(mangaNextVolume)
	if !ok || !next.Chapter || next.ID != list[1].ID || !next.Have {
		t.Fatalf("%+v", out)
	}
}

func TestMangaChapterPackFilesEachChapter(t *testing.T) {
	f := newFakeDex(t)
	s, repo, root := dexServer(t, f)
	sr := dexSeriesIn(t, s, repo, manga.FollowAll)
	dir := t.TempDir()
	one := filepath.Join(dir, "Tower c003.cbz")
	two := filepath.Join(dir, "Tower c004.cbz")
	for _, p := range []string{one, two} {
		writeTestMangaCBZ(t, p, f.page)
	}
	d := manga.Download{SeriesID: sr.ID, Class: string(manga.ClassFan)}
	out, err := s.fileMangaDownload(&sr, d, []string{one, two}, "Tower (Chapters 3-4)", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.chapters != 2 || out.filed != 0 {
		t.Fatalf("%+v", out)
	}
	if _, err := os.Stat(filepath.Join(root, "Tower", "Chapters", "Tower c0003.cbz")); err != nil {
		t.Fatal(err)
	}
	// One big file with a run in it can't be filed, and says so.
	big := filepath.Join(dir, "Tower (Chapters 1-721).cbz")
	writeTestMangaCBZ(t, big, f.page)
	out, err = s.fileMangaDownload(&sr, d, []string{big}, "Tower (Chapters 1-721)", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.runs != "1 to 721" || out.chapters != 0 {
		t.Fatalf("%+v", out)
	}
}

func writeTestMangaCBZ(t *testing.T, path string, page []byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	w, err := zw.Create("0001.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(page); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}
