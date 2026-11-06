package api_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/comics"
)

// writeCBZ writes a comic of n pages, each a width x width*3/2 JPEG, plus a
// note that isn't a page.
func writeCBZ(t *testing.T, path string, n, width int) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 1; i <= n; i++ {
		img := image.NewRGBA(image.Rect(0, 0, width, width*3/2))
		for y := 0; y < width*3/2; y += 4 {
			for x := 0; x < width; x += 4 {
				img.Set(x, y, color.RGBA{R: uint8(i * 40), G: uint8(x), B: uint8(y), A: 255})
			}
		}
		w, err := zw.Create(fmt.Sprintf("page%d.jpg", i))
		if err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(w, img, nil); err != nil {
			t.Fatal(err)
		}
	}
	w, _ := zw.Create("notes.txt")
	_, _ = w.Write([]byte("not a page"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pseFeed reads the parts of an acquisition feed page streaming adds.
type pseFeed struct {
	Entries []struct {
		Title string `xml:"title"`
		ID    string `xml:"id"`
		Links []struct {
			Rel          string `xml:"rel,attr"`
			Href         string `xml:"href,attr"`
			Type         string `xml:"type,attr"`
			Count        string `xml:"http://vaemendis.net/opds-pse/ns count,attr"`
			LastRead     string `xml:"http://vaemendis.net/opds-pse/ns lastRead,attr"`
			LastReadDate string `xml:"http://vaemendis.net/opds-pse/ns lastReadDate,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func parsePSE(t *testing.T, data []byte) pseFeed {
	t.Helper()
	var f pseFeed
	if err := xml.Unmarshal(data, &f); err != nil {
		t.Fatalf("not valid XML: %v\n%s", err, data)
	}
	return f
}

const pseRel = "http://vaemendis.net/opds-pse/stream"

func TestOPDSComicsWithPageStreaming(t *testing.T) {
	f := newEReaderFixture(t)
	client := erClient()
	pw := postJSON[ereaderAccess](t, f.admin, f.base+"/api/auth/ereader/opds", nil, http.StatusOK).Password

	// Comics off: no Comics section, and its addresses don't exist.
	_, data := erOPDSGet(t, client, f.base+"/opds/", "ryan", pw)
	if root := parseAtom(t, data); len(root.Entries) != 4 {
		t.Fatalf("comics off: %d sections", len(root.Entries))
	}
	if resp, _ := erOPDSGet(t, client, f.base+"/opds/comics", "ryan", pw); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("comics off: /opds/comics is %d", resp.StatusCode)
	}

	f.server.TestComicsOn(true)
	repo := f.server.TestComicRepo()
	folder := filepath.Join(t.TempDir(), "comics", "Night Owls", "Volume 01 (2020)")
	sr, err := repo.UpsertSeries(comics.Series{CVID: 9001, Name: "Night Owls", StartYear: 2020, Publisher: "Lantern Press", IssueCount: 2, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 2; n++ {
		if _, err := repo.UpsertIssue(comics.Issue{SeriesID: sr.ID, CVID: int64(9100 + n), Number: fmt.Sprint(n), Title: "Moonrise", CoverDate: "2020-0" + fmt.Sprint(n) + "-01"}); err != nil {
			t.Fatal(err)
		}
	}
	issues, _ := repo.Issues(sr.ID)
	one := issues[0]
	file := filepath.Join(folder, "Night Owls (2020) Issue 001.cbz")
	writeCBZ(t, file, 3, 1200)
	st, _ := os.Stat(file)
	if err := repo.SetState(one.ID, comics.StatusDownloaded, file, st.Size()); err != nil {
		t.Fatal(err)
	}
	// A series with nothing downloaded stays out.
	if _, err := repo.UpsertSeries(comics.Series{CVID: 9002, Name: "Empty Skies", IssueCount: 1}); err != nil {
		t.Fatal(err)
	}

	// The root has the Comics section.
	_, data = erOPDSGet(t, client, f.base+"/opds/", "ryan", pw)
	root := parseAtom(t, data)
	if len(root.Entries) != 5 || root.Entries[4].Title != "Comics" {
		t.Fatalf("root: %+v", root.Entries)
	}

	// Series, each with a cover, leading to their issues.
	resp, data := erOPDSGet(t, client, f.base+"/opds/comics", "ryan", pw)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/atom+xml;profile=opds-catalog;kind=navigation" {
		t.Fatalf("comics: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	list := parseAtom(t, data)
	if len(list.Entries) != 1 || list.Entries[0].Title != "Night Owls (2020)" || list.Total != 1 {
		t.Fatalf("series list: %+v", list)
	}
	var issuesHref, seriesCover string
	for _, l := range list.Entries[0].Links {
		switch l.Rel {
		case "subsection":
			issuesHref = l.Href
		case "http://opds-spec.org/image":
			seriesCover = l.Href
		}
	}
	if issuesHref != fmt.Sprintf("/opds/comics/series/%d", sr.ID) || seriesCover == "" {
		t.Fatalf("series links: %+v", list.Entries[0].Links)
	}
	// No ComicVine picture: the cover is the first page.
	resp, data = erOPDSGet(t, client, f.base+seriesCover, "ryan", pw)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("series cover: %d", resp.StatusCode)
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("series cover isn't a picture: %v", err)
	}

	// The issues: the file, covers and the page stream with its count.
	resp, data = erOPDSGet(t, client, f.base+issuesHref, "ryan", pw)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/atom+xml;profile=opds-catalog;kind=acquisition" {
		t.Fatalf("issues: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(data), `xmlns:pse="http://vaemendis.net/opds-pse/ns"`) {
		t.Fatal("the feed doesn't name the PSE namespace")
	}
	feed := parsePSE(t, data)
	if len(feed.Entries) != 1 || feed.Entries[0].Title != "Night Owls #1: Moonrise" {
		t.Fatalf("issues feed: %+v", feed.Entries)
	}
	links := map[string]string{}
	for _, l := range feed.Entries[0].Links {
		links[l.Rel] = l.Href
		if l.Rel == pseRel && (l.Count != "3" || l.LastRead != "" || l.Type != "image/jpeg") {
			t.Fatalf("stream link before reading: %+v", l)
		}
	}
	stream := links[pseRel]
	wantStream := fmt.Sprintf("/opds/comics/issues/%d/pages/{pageNumber}?width={maxWidth}", one.ID)
	if stream != wantStream || links["http://opds-spec.org/acquisition"] == "" || links["http://opds-spec.org/image/thumbnail"] == "" {
		t.Fatalf("issue links: %+v", links)
	}

	// Pages count from 0. An app that fills in a width gets the page cut down.
	page := func(n int, width string) (*http.Response, []byte) {
		u := strings.Replace(stream, "{pageNumber}", fmt.Sprint(n), 1)
		u = strings.Replace(u, "{maxWidth}", width, 1)
		return erOPDSGet(t, client, f.base+u, "ryan", pw)
	}
	resp, data = page(0, "{maxWidth}") // left as it was
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("page 0: %d", resp.StatusCode)
	}
	if img, _, err := image.Decode(bytes.NewReader(data)); err != nil || img.Bounds().Dx() != 1200 {
		t.Fatalf("page 0: %v", err)
	}
	resp, data = page(2, "600")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("page 2: %d", resp.StatusCode)
	}
	if img, _, err := image.Decode(bytes.NewReader(data)); err != nil || img.Bounds().Dx() != 600 {
		t.Fatalf("page 2 at 600: %v", err)
	}
	if resp, _ := page(3, "600"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("page past the end: %d", resp.StatusCode)
	}
	if resp, _ := erOPDSGet(t, client, f.base+strings.Replace(strings.Replace(stream, "{pageNumber}", "0", 1), "{maxWidth}", "600", 1), "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a page without sign-in: %d", resp.StatusCode)
	}

	// The download is the file itself.
	resp, data = erOPDSGet(t, client, f.base+links["http://opds-spec.org/acquisition"], "ryan", pw)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/vnd.comicbook+zip" || !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatalf("download: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// Where the reader got to in Usherr comes back as lastRead (0-based).
	postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.base+"/api/home/position", map[string]any{"item": fmt.Sprintf("comic:%d", one.ID), "position": 2, "duration": 3}, http.StatusNoContent)
	_, data = erOPDSGet(t, client, f.base+issuesHref, "ryan", pw)
	for _, l := range parsePSE(t, data).Entries[0].Links {
		if l.Rel == pseRel && (l.LastRead != "1" || l.LastReadDate == "") {
			t.Fatalf("stream link after reading: %+v", l)
		}
	}

	// One search finds books and comics.
	_, data = erOPDSGet(t, client, f.base+"/opds/search?q=night+owls", "ryan", pw)
	if found := parseAtom(t, data); len(found.Entries) != 1 || found.Entries[0].Title != "Night Owls #1: Moonrise" {
		t.Fatalf("comic search: %+v", found.Entries)
	}
	_, data = erOPDSGet(t, client, f.base+"/opds/search?q=dune", "ryan", pw)
	if found := parseAtom(t, data); len(found.Entries) != 1 || found.Entries[0].Title != "Dune" {
		t.Fatalf("book search with comics on: %+v", found.Entries)
	}
	_, data = erOPDSGet(t, client, f.base+"/opds/search?q=lantern", "ryan", pw)
	if found := parseAtom(t, data); len(found.Entries) != 1 {
		t.Fatalf("publisher search: %+v", found.Entries)
	}
	_, data = erOPDSGet(t, client, f.base+"/opds/opensearch.xml", "ryan", pw)
	if !strings.Contains(string(data), "books and comics") {
		t.Fatalf("opensearch: %s", data)
	}

	// OPDS 2.0 lists the comics too (without page streaming, which it lacks).
	resp, data = erOPDSGet(t, client, f.base+"/opds/v2"+strings.TrimPrefix(issuesHref, "/opds"), "ryan", pw)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(data), "Night Owls #1") || strings.Contains(string(data), "opds-pse") {
		t.Fatalf("v2 issues: %d %s", resp.StatusCode, data)
	}

	// A missing issue isn't offered.
	if resp, _ := erOPDSGet(t, client, f.base+fmt.Sprintf("/opds/comics/issues/%d/download", issues[1].ID), "ryan", pw); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing issue: %d", resp.StatusCode)
	}

	// With ebooks off the catalogue still opens, with comics only.
	postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.base+"/api/modules", map[string]any{"ebooks": false}, http.StatusOK)
	_, data = erOPDSGet(t, client, f.base+"/opds/", "ryan", pw)
	if root := parseAtom(t, data); len(root.Entries) != 1 || root.Entries[0].Title != "Comics" {
		t.Fatalf("comics only: %+v", root.Entries)
	}
	if resp, _ := erOPDSGet(t, client, f.base+"/opds/books", "ryan", pw); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("books with ebooks off: %d", resp.StatusCode)
	}
}

// A comic kept in the Books library streams page by page too.
func TestOPDSBookComicStreams(t *testing.T) {
	f := newEReaderFixture(t)
	client := erClient()
	pw := postJSON[ereaderAccess](t, f.admin, f.base+"/api/auth/ereader/opds", nil, http.StatusOK).Password

	file := filepath.Join(f.root, "Sam Rivers", "Paper Kites", "Paper Kites.cbz")
	writeCBZ(t, file, 4, 800)
	added, err := f.server.BookRepo.Add(books.Book{OLKey: "OL9W", Title: "Paper Kites", Author: "Sam Rivers", WantEbook: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.server.BookRepo.SetState(added.ID, books.Ebook, books.StatusDownloaded, "cbz", file); err != nil {
		t.Fatal(err)
	}
	_, data := erOPDSGet(t, client, f.base+"/opds/search?q=kites", "ryan", pw)
	feed := parsePSE(t, data)
	if len(feed.Entries) != 1 {
		t.Fatalf("search: %s", data)
	}
	var stream string
	for _, l := range feed.Entries[0].Links {
		if l.Rel == pseRel {
			if l.Count != "4" {
				t.Fatalf("count: %+v", l)
			}
			stream = l.Href
		}
	}
	if stream == "" {
		t.Fatalf("no stream link: %s", data)
	}
	resp, data := erOPDSGet(t, client, f.base+strings.Replace(stream, "{pageNumber}", "3", 1), "ryan", pw)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("last page: %d", resp.StatusCode)
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("last page: %v", err)
	}
	if resp, _ := erOPDSGet(t, client, f.base+strings.Replace(stream, "{pageNumber}", "4", 1), "ryan", pw); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("past the end: %d", resp.StatusCode)
	}
	// EPUBs get no stream link.
	_, data = erOPDSGet(t, client, f.base+"/opds/search?q=dune", "ryan", pw)
	if strings.Contains(string(data), pseRel) {
		t.Fatal("an EPUB has a page stream")
	}
}
