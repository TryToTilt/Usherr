package api

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/queue"
)

func mangaUser(t *testing.T, s *Server, name string) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash) VALUES (?, 'x')`, name)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// fileVolume puts a CBZ in a series' folder and marks the volume as on disk.
func fileVolume(t *testing.T, repo *manga.Repo, root string, sr manga.Series, number string, pages int) manga.Volume {
	t.Helper()
	p := filepath.Join(root, sr.Title, manga.FileName(sr.Title, number)+".cbz")
	writeCBZ(t, p, pages)
	v, _, err := repo.EnsureVolume(sr.ID, number, true)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if err := repo.SetVolumeFile(v.ID, p, st.Size(), "own"); err != nil {
		t.Fatal(err)
	}
	v, _ = repo.GetVolume(v.ID)
	return v
}

func TestMangaReaderPlaceAndContinue(t *testing.T) {
	s, repo, root := mangaTestServer(t)
	user := mangaUser(t, s, "reader")
	sr := addSeries(t, repo, "Spy × Family", 3, 1)
	one := fileVolume(t, repo, root, sr, "1", 4)
	two := fileVolume(t, repo, root, sr, "2", 4)

	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/manga/volumes/%d/pages", one.ID), nil)
	r.SetPathValue("id", strconv.FormatInt(one.ID, 10))
	rec := httptest.NewRecorder()
	s.handleMangaPages(rec, withUser(r, user))
	var pages struct {
		Pages     int              `json:"pages"`
		Direction string           `json:"direction"`
		Layout    string           `json:"layout"`
		Next      *mangaNextVolume `json:"next"`
		NextHave  *mangaNextVolume `json:"nextHave"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pages); err != nil {
		t.Fatalf("%s: %v", rec.Body.String(), err)
	}
	if pages.Pages != 4 || pages.Direction != "rtl" || pages.Layout != "" || pages.Next == nil || pages.Next.ID != two.ID || !pages.Next.Have {
		t.Fatalf("%s", rec.Body.String())
	}

	// A place part way through is your Continue line.
	put := func(item string, pos, dur float64) {
		t.Helper()
		body := fmt.Sprintf(`{"item":%q,"position":%v,"duration":%v}`, item, pos, dur)
		rec := httptest.NewRecorder()
		s.handlePositionPut(rec, withUser(httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)), user))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	}
	put(manga.PositionItem(one.ID), 3, 4)
	row, err := repo.Continue(user, 0, 10)
	if err != nil || len(row) != 1 || row[0].VolumeID != one.ID || row[0].Page != 3 || row[0].Pages != 4 {
		t.Fatalf("%v %+v", err, row)
	}
	// The last page finishes it, and the next volume takes its place.
	put(manga.PositionItem(one.ID), 4, 4)
	row, err = repo.Continue(user, 0, 10)
	if err != nil || len(row) != 1 || row[0].VolumeID != two.ID || row[0].Page != 0 {
		t.Fatalf("%v %+v", err, row)
	}
	volumes, _ := repo.Volumes(sr.ID)
	if err := repo.FillReading(user, sr.ID, volumes); err != nil || !volumes[0].Read || volumes[1].Read {
		t.Fatalf("%v %+v", err, volumes)
	}
	// Someone else's reading is theirs.
	other := mangaUser(t, s, "other")
	if row, _ := repo.Continue(other, 0, 10); len(row) != 0 {
		t.Fatalf("%+v", row)
	}
	// All read: nothing to continue.
	put(manga.PositionItem(two.ID), 4, 4)
	if row, _ := repo.Continue(user, 0, 10); len(row) != 0 {
		t.Fatalf("%+v", row)
	}
}

func TestMangaReaderPrefs(t *testing.T) {
	if p := manga.ResolveReader("manga", "", manga.ReaderPrefs{}); p.Direction != "rtl" || p.Layout != "" {
		t.Fatalf("%+v", p)
	}
	if p := manga.ResolveReader("manhwa", "", manga.ReaderPrefs{}); p.Direction != "ltr" || p.Layout != "webtoon" {
		t.Fatalf("%+v", p)
	}
	if p := manga.ResolveReader("manga", "Yes", manga.ReaderPrefs{}); p.Direction != "ltr" {
		t.Fatalf("a file that says left to right: %+v", p)
	}
	if p := manga.ResolveReader("manhwa", "", manga.ReaderPrefs{Layout: "single", Direction: "rtl"}); p.Direction != "rtl" || p.Layout != "single" {
		t.Fatalf("your choice comes first: %+v", p)
	}
	s, repo, _ := mangaTestServer(t)
	user := mangaUser(t, s, "reader")
	sr := addSeries(t, repo, "Prefs", 1, 1)
	if err := repo.SetReaderPrefs(user, sr.ID, manga.ReaderPrefs{Direction: "ltr"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetReaderPrefs(user, sr.ID, manga.ReaderPrefs{Layout: "double"}); err != nil {
		t.Fatal(err)
	}
	if p := repo.ReaderPrefsFor(user, sr.ID); p.Direction != "ltr" || p.Layout != "double" {
		t.Fatalf("%+v", p)
	}
}

func TestMangaMarkItems(t *testing.T) {
	s, repo, root := mangaTestServer(t)
	user := mangaUser(t, s, "reader")
	sr := addSeries(t, repo, "Marks", 1, 1)
	v := fileVolume(t, repo, root, sr, "1", 3)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"position":"page:2","label":"Page 2","percent":50}`))
	r.SetPathValue("id", strconv.FormatInt(v.ID, 10))
	rec := httptest.NewRecorder()
	s.handleAddMangaMark(rec, withUser(r, user))
	if rec.Code >= 300 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

// writeEPUB makes an EPUB whose reading order isn't the order of its files' names.
func writeEPUB(t *testing.T, p string, pages int, words string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z := zip.NewWriter(f)
	add := func(name string, b []byte) {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(b)
	}
	add("META-INF/container.xml", []byte(`<container><rootfiles><rootfile full-path="OEBPS/book.opf"/></rootfiles></container>`))
	var manifest, spine strings.Builder
	for i := 0; i < pages; i++ {
		// Backwards: page 0 is the last image by name.
		img := fmt.Sprintf("images/z%03d.png", pages-i)
		var png1 bytes.Buffer
		_ = png.Encode(&png1, image.NewRGBA(image.Rect(0, 0, 10+i, 20)))
		add("OEBPS/"+img, png1.Bytes())
		add(fmt.Sprintf("OEBPS/p%d.xhtml", i), []byte(fmt.Sprintf(`<html><head><title>x</title></head><body>%s<img src="%s"/></body></html>`, words, img)))
		fmt.Fprintf(&manifest, `<item id="p%d" href="p%d.xhtml"/>`, i, i)
		fmt.Fprintf(&spine, `<itemref idref="p%d"/>`, i)
	}
	add("OEBPS/book.opf", []byte(`<package><manifest>`+manifest.String()+`</manifest><spine>`+spine.String()+`</spine></package>`))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMangaEPUBOfPicturesIsRead(t *testing.T) {
	s, repo, root := mangaTestServer(t)
	sr := addSeries(t, repo, "Pictures", 2, 1)
	work := t.TempDir()
	p := filepath.Join(work, "Pictures v01.epub")
	writeEPUB(t, p, 12, "")
	d := manga.Download{SeriesID: sr.ID, Class: string(manga.ClassOfficial)}
	out, err := s.fileMangaDownload(&sr, d, []string{p}, "Yen.Press-Pictures.Vol.01.2022.Retail.EPUB.eBook-BitBook", "1")
	if err != nil || out.filed != 1 {
		t.Fatalf("%v %+v", err, out)
	}
	v, _, _ := repo.EnsureVolume(sr.ID, "1", false)
	if !strings.HasSuffix(v.FilePath, "Pictures v01.epub") {
		t.Fatalf("%q", v.FilePath)
	}
	pages, err := s.mangaPages(v)
	if err != nil || len(pages) != 12 {
		t.Fatalf("%v %d", err, len(pages))
	}
	// In the book's order: the first page's picture is 10 wide.
	sizes := pageSizes(pages)
	if sizes[0][0] != 10 || sizes[11][0] != 21 {
		t.Fatalf("%v", sizes)
	}
	_ = root
}

func TestMangaNovelInAnEPUBIsntFiledOrBlocklisted(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	sr := addSeries(t, repo, "Words", 1, 1)
	work := t.TempDir()
	p := filepath.Join(work, "Words v01.epub")
	writeEPUB(t, p, 12, strings.Repeat("A great many words on every page. ", 80))
	d := manga.Download{SeriesID: sr.ID, Class: string(manga.ClassOfficial)}
	out, err := s.fileMangaDownload(&sr, d, []string{p}, "Words Vol 01 Retail EPUB", "1")
	if err != nil || out.filed != 0 || out.novels != 1 {
		t.Fatalf("%v %+v", err, out)
	}
}

func TestMangaDownloadOfOtherFormatsSaysWhat(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"book.azw3", "book.mobi", "book.nfo"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if why := mangaHolds(dir); !strings.Contains(why, "AZW3, MOBI") || strings.Contains(why, "NFO") {
		t.Fatalf("%q", why)
	}
	if len(mangaFiles(dir)) != 0 {
		t.Fatal("those aren't pages")
	}
	if why := mangaHolds(t.TempDir()); !strings.Contains(why, "no pages") {
		t.Fatalf("%q", why)
	}
}

// Published volumes often come as a RAR holding a ZIP: by now the ZIP is what's
// left, and it must become pages rather than "holds only ZIP files".
func TestMangaZipsInADownloadBecomePages(t *testing.T) {
	writeZip := func(path string, names ...string) {
		t.Helper()
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		w := zip.NewWriter(f)
		for _, n := range names {
			e, err := w.Create(n)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = e.Write([]byte("sample"))
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	writeZip(filepath.Join(dir, "Sample Vol 02.zip"), "p001.jpg", "p002.jpg")
	writeZip(filepath.Join(dir, "Other Vol 03.zip"), "Other Vol 03.pdf")
	writeZip(filepath.Join(dir, "notes.zip"), "readme.txt")
	if err := os.WriteFile(filepath.Join(dir, "broken.zip"), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	openWrappedManga(dir)
	var got []string
	for _, f := range mangaFiles(dir) {
		got = append(got, filepath.Base(f))
	}
	if strings.Join(got, ",") != "Other Vol 03.pdf,Sample Vol 02.cbz" {
		t.Fatalf("files: %v", got)
	}
}

func TestMangaPackKeepsTheBetterCopy(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	sr := addSeries(t, repo, "Doubles", 3, 1)
	work := t.TempDir()
	small := filepath.Join(work, "Doubles v01 a.cbz")
	big := filepath.Join(work, "Doubles v01 b.cbz")
	fan := filepath.Join(work, "Doubles v02 (scans).cbz")
	official := filepath.Join(work, "Doubles v02 (Digital) (danke-Empire).cbz")
	pdf := filepath.Join(work, "Doubles v03.pdf")
	cbz := filepath.Join(work, "Doubles v03.cbz")
	writeCBZ(t, small, 2)
	writeCBZ(t, big, 6)
	writeCBZ(t, fan, 9)
	writeCBZ(t, official, 2)
	writeCBZ(t, cbz, 2)
	if err := os.WriteFile(pdf, append([]byte("%PDF-1.4 "), make([]byte, 1<<20)...), 0o644); err != nil {
		t.Fatal(err)
	}
	wantSize := func(p string) int64 { st, _ := os.Stat(p); return st.Size() }
	bigSize, officialSize := wantSize(big), wantSize(official)
	d := manga.Download{SeriesID: sr.ID, Class: string(manga.ClassOfficial)}
	out, err := s.fileMangaDownload(&sr, d, []string{small, big, fan, official, pdf, cbz}, "Doubles v01-v03", "")
	if err != nil || out.filed != 3 || out.doubles != 3 {
		t.Fatalf("%v %+v", err, out)
	}
	volumes, _ := repo.Volumes(sr.ID)
	if volumes[0].ReleaseSize != bigSize {
		t.Fatalf("v01: kept %d, not the bigger %d", volumes[0].ReleaseSize, bigSize)
	}
	if volumes[1].ReleaseSize != officialSize {
		t.Fatalf("v02: kept %d, not the official %d", volumes[1].ReleaseSize, officialSize)
	}
	if !strings.HasSuffix(volumes[2].FilePath, ".cbz") {
		t.Fatalf("v03: kept %q, not the CBZ", volumes[2].FilePath)
	}
	// Against a file already there, a different copy is for a person to judge.
	other := filepath.Join(work, "Doubles v01 c.cbz")
	writeCBZ(t, other, 11)
	out, err = s.fileMangaDownload(&sr, d, []string{other}, "Doubles v01-v03", "")
	if err != nil || out.copies != 1 || len(out.copyVolumes) != 1 || out.copyVolumes[0] != volumes[0].ID {
		t.Fatalf("%v %+v", err, out)
	}
	// Taking it from a pack swaps that one volume only.
	again := filepath.Join(work, "Doubles v02 again.cbz")
	writeCBZ(t, again, 13)
	d.Replace, d.VolumeID = true, volumes[0].ID
	out, err = s.fileMangaDownload(&sr, d, []string{other, again}, "Doubles v01-v03", "1")
	if err != nil || out.filed != 1 {
		t.Fatalf("%v %+v", err, out)
	}
	after, _ := repo.Volumes(sr.ID)
	if after[1].ReleaseSize != officialSize {
		t.Fatal("another volume of the pack was swapped too")
	}
}

// mangaCategoriesAsked is a fake indexer that remembers what it was asked for.
func TestMangaSearchAsksBooksCategories(t *testing.T) {
	var mu sync.Mutex
	var cats []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><caps><server title="fake"/></caps>`))
			return
		}
		mu.Lock()
		cats = append(cats, r.URL.Query().Get("cat"))
		mu.Unlock()
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel><item><title>VIZ.Media.Chainsaw.Man.Vol.02.2020.HYBRiD.MANGA.eBook-PNLS</title><link>` + "http://example.invalid/get/1" + `</link><guid>1</guid><enclosure url="http://example.invalid/get/1" length="112799512" type="application/x-nzb"/><newznab:attr name="category" value="7020"/><newznab:attr name="size" value="112799512"/></item></channel></rss>`))
	}))
	defer srv.Close()
	s, repo, _ := mangaTestServer(t)
	if _, err := s.IndexerRepo.Create(indexers.Instance{Name: "Fake", BaseURL: srv.URL, APIKey: "k", Protocol: indexers.ProtocolUsenet, Kind: indexers.KindNewznab, Enabled: true}, nil); err != nil {
		t.Fatal(err)
	}
	sr := addSeries(t, repo, "Chainsaw Man", 2, 1)
	volumes, _ := repo.Volumes(sr.ID)
	got, sum := s.searchMangaVolume(t.Context(), sr, volumes[1], queue.PriorityManual, false)
	if !got {
		t.Fatalf("the published edition wasn't taken: %+v", sum)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, c := range cats {
		for _, want := range []string{"7000", "7020", "7030"} {
			if !strings.Contains(c, want) {
				t.Fatalf("asked for %q", c)
			}
		}
	}
	if len(cats) == 0 {
		t.Fatal("nothing was asked")
	}
}

// A Release downloaded and found to hold the wrong kind of file is remembered, not blocklisted.
func TestMangaWrongKindIsAskedAboutNextTime(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	sr := addSeries(t, repo, "Chainsaw Man", 2, 1)
	title := "VIZ.Media.Chainsaw.Man.Vol.02.2020.HYBRiD.MANGA.eBook-PNLS"
	if err := repo.NoteRelease(sr.ID, title, "This download holds only AZW3 files."); err != nil {
		t.Fatal(err)
	}
	v := manga.Evaluate(manga.Release{Title: title, SizeBytes: 100 << 20}, s.mangaTarget(sr), "")
	if v.Reject != "" || v.Confident() || !strings.Contains(strings.Join(v.Notes, " "), "AZW3") {
		t.Fatalf("%+v", v)
	}
}

// ---- Nyaa ----

// fakeDefinitions serves the shared list of sites with a Nyaa in it. The site's own
// address is never asked: adding it only reads the list.
func fakeDefinitions(t *testing.T, s *Server, withNyaa bool) {
	t.Helper()
	def := `---
id: nyaasi
name: Nyaa.si
description: "test"
language: en-US
type: public
encoding: UTF-8
links:
  - http://nyaa.invalid/
caps:
  categorymappings:
    - {id: 3_0, cat: Books, desc: "Literature"}
    - {id: 3_1, cat: Books, desc: "Literature English"}
  modes:
    search: [q]
settings:
  - name: cat-id
    type: select
    label: Category
    default: 0_0
    options:
      0_0: "All"
      3_1: "Literature - English-translated"
search:
  paths:
    - path: /
  inputs:
    q: "{{ .Keywords }}"
    c: "{{ .Config.cat-id }}"
  rows:
    selector: tr
  fields:
    title:
      selector: a
    download:
      selector: a
      attribute: href
    size:
      selector: td
    seeders:
      selector: td
    leechers:
      selector: td
`
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if withNyaa {
		_ = tw.WriteHeader(&tar.Header{Name: "Indexers-master/definitions/v11/nyaasi.yml", Mode: 0o644, Size: int64(len(def))})
		_, _ = tw.Write([]byte(def))
	}
	_ = tw.Close()
	_ = gz.Close()
	archive := buf.Bytes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive) }))
	t.Cleanup(srv.Close)
	s.Definitions.SourceURL = srv.URL + "/a.tar.gz"
	s.Definitions.HTTPClient = srv.Client()
}

func putMangaSettings(s *Server, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handlePutMangaSettings(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)))
	return rec
}

func TestMangaNyaaSwitch(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	fakeDefinitions(t, s, true)
	if rec := putMangaSettings(s, `{"nyaa":true}`); rec.Code != http.StatusOK {
		t.Fatalf("the list of sites: %d %s", rec.Code, rec.Body.String())
	}
	list, err := s.IndexerRepo.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %+v", err, list)
	}
	nyaa := list[0]
	if nyaa.DefinitionID != "nyaasi" || nyaa.Protocol != indexers.ProtocolTorrent || nyaa.Enabled || nyaa.Settings["cat-id"] != "3_1" {
		t.Fatalf("%+v", nyaa)
	}
	// Stored off, so films and TV don't ask it; Manga does, while the switch is on.
	usual, ny, err := s.mangaInstances(true)
	if err != nil || len(usual) != 0 || len(ny) != 1 || !ny[0].Enabled {
		t.Fatalf("%v %+v %+v", err, usual, ny)
	}
	if _, ny, _ := s.mangaInstances(false); len(ny) != 0 {
		t.Fatal("asked with the switch off")
	}
	// On again changes nothing; off leaves it in place, not used.
	if rec := putMangaSettings(s, `{"nyaa":true}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if list, _ := s.IndexerRepo.List(); len(list) != 1 {
		t.Fatalf("added twice: %+v", list)
	}
	if rec := putMangaSettings(s, `{"nyaa":false}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if s.mangaSourcesFor(addSeries(t, repo, "Off", 0, 1)).Nyaa {
		t.Fatal("still on")
	}
}

func TestMangaNyaaYoursIsLeftAlone(t *testing.T) {
	s, _, _ := mangaTestServer(t)
	fakeDefinitions(t, s, false)
	if _, err := s.IndexerRepo.Create(indexers.Instance{Name: "My Nyaa", BaseURL: "http://nyaa.invalid", APIKey: "k", Protocol: indexers.ProtocolTorrent, Kind: indexers.KindTorznab, Enabled: true}, nil); err != nil {
		t.Fatal(err)
	}
	if rec := putMangaSettings(s, `{"nyaa":true}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	st := s.mangaNyaaNow()
	if st.Indexer != "My Nyaa" || st.Own {
		t.Fatalf("%+v", st)
	}
	if list, _ := s.IndexerRepo.List(); len(list) != 1 {
		t.Fatalf("%+v", list)
	}
}

func TestMangaNyaaNotAddedWithTorrentsOff(t *testing.T) {
	s, _, _ := mangaTestServer(t)
	fakeDefinitions(t, s, true)
	if err := s.setTorrentsEnabled(false); err != nil {
		t.Fatal(err)
	}
	if rec := putMangaSettings(s, `{"nyaa":true}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "torrents are switched off") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if list, _ := s.IndexerRepo.List(); len(list) != 0 || s.currentMangaSources().Nyaa {
		t.Fatalf("%+v", list)
	}
}

func TestMangaSeriesSourcesOverride(t *testing.T) {
	s, repo, _ := mangaTestServer(t)
	sr := addSeries(t, repo, "Choice", 0, 1)
	if got := s.mangaSourcesFor(sr); !got.Official || got.Fan {
		t.Fatalf("%+v", got)
	}
	sr.SourceMode = "fan"
	if got := s.mangaSourcesFor(sr); !got.Fan || !got.Official {
		t.Fatalf("%+v", got)
	}
	if rec := putMangaSettings(s, `{"fan":true}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	sr.SourceMode = "official"
	if got := s.mangaSourcesFor(sr); !got.Official || got.Fan {
		t.Fatalf("official only beats the settings: %+v", got)
	}
}
