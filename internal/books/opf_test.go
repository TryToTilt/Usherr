package books_test

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/books"
)

func writeEPUB(t *testing.T, opf string, extra map[string]string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "book.epub")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("META-INF/container.xml", `<container><rootfiles><rootfile full-path="OPS/content.opf"/></rootfiles></container>`)
	add("OPS/content.opf", opf)
	for k, v := range extra {
		add(k, v)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestReadFileDetailsCalibre(t *testing.T) {
	file := writeEPUB(t, `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:title> The Long Road </dc:title>
  <dc:creator opf:role="aut" opf:file-as="Writer, Ann">Ann Writer</dc:creator>
  <dc:creator opf:role="trl">Tom Translator</dc:creator>
  <dc:creator>Bea Coauthor</dc:creator>
  <dc:date>2014-03-02T00:00:00+00:00</dc:date>
  <dc:description>&lt;p&gt;A &lt;b&gt;fine&lt;/b&gt; book &amp;amp; more.&lt;/p&gt;</dc:description>
  <dc:identifier opf:scheme="calibre">12345</dc:identifier>
  <dc:identifier opf:scheme="ISBN">978-0-306-40615-7</dc:identifier>
  <dc:language>en</dc:language>
  <dc:publisher>Small Press</dc:publisher>
  <meta name="calibre:series" content="Roads"/>
  <meta name="calibre:series_index" content="2.0"/>
</metadata></package>`, nil)
	got, err := books.ReadFileDetails(file)
	if err != nil {
		t.Fatal(err)
	}
	want := books.FileDetails{Title: "The Long Road", Author: "Ann Writer, Bea Coauthor", SeriesName: "Roads", SeriesPosition: "2", Year: 2014,
		Description: "A fine book & more.", ISBN: "9780306406157", Language: "en", Publisher: "Small Press"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestReadFileDetailsEPUB3AndBlanks(t *testing.T) {
	file := writeEPUB(t, `<package><metadata>
  <title>Unknown</title><creator>Unknown</creator>
  <date>0101-01-01</date>
  <identifier>urn:isbn:0306406152</identifier>
  <meta property="belongs-to-collection" id="c1">The Saga</meta>
  <meta refines="#c1" property="collection-type">series</meta>
  <meta refines="#c1" property="group-position">3.5</meta>
</metadata></package>`, nil)
	got, err := books.ReadFileDetails(file)
	if err != nil {
		t.Fatal(err)
	}
	want := books.FileDetails{SeriesName: "The Saga", SeriesPosition: "3.5", ISBN: "0306406152"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestReadFileDetailsNotAnEPUB(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x.epub")
	if err := os.WriteFile(file, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := books.ReadFileDetails(file); !errors.Is(err, books.ErrNoFileDetails) {
		t.Errorf("got %v", err)
	}
}

func TestEPUBSpineSizes(t *testing.T) {
	file := writeEPUB(t, `<package><manifest>
  <item id="a" href="a%20b.xhtml"/><item id="b" href="text/b.xhtml"/><item id="n" href="nav.xhtml"/>
</manifest><spine><itemref idref="a"/><itemref idref="missing"/><itemref idref="b"/></spine></package>`,
		map[string]string{"OPS/a b.xhtml": strings.Repeat("a", 10), "OPS/text/b.xhtml": strings.Repeat("b", 30), "OPS/nav.xhtml": "n"})
	got, err := books.EPUBSpineSizes(file)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int64{10, 0, 30}) {
		t.Errorf("got %v", got)
	}
}

func TestLockedSeriesStaysThroughLookups(t *testing.T) {
	r := newRepo(t)
	b, err := r.Add(books.Book{OLKey: "OL9W", Title: "Wrong", Author: "Someone", WantEbook: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetSeries(b.ID, "From lookup", "1"); err != nil {
		t.Fatal(err)
	}
	d := books.Details{Title: "Right", Author: "Ann Writer", Year: 2001, Description: "New.", SeriesName: "By hand", SeriesPosition: "4"}
	if err := r.UpdateDetails(b.ID, d, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSeries(b.ID, "From lookup", "1"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(b.ID)
	if got.Title != "Right" || got.Author != "Ann Writer" || got.Year != 2001 || got.Description != "New." || got.SeriesName != "By hand" || got.SeriesPosition != "4" {
		t.Errorf("got %+v", got)
	}

	// Matching again starts over, so a look-up can fill the series in again.
	if err := r.Rematch(b.ID, books.Book{OLKey: "OL10W", Title: "Matched", Author: "Other", AuthorKey: "OL1A", Year: 1999, CoverID: 7, Description: "D"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSeries(b.ID, "From lookup", "1"); err != nil {
		t.Fatal(err)
	}
	got, _ = r.Get(b.ID)
	if got.OLKey != "OL10W" || got.Title != "Matched" || got.CoverID != 7 || got.SeriesName != "From lookup" {
		t.Errorf("after matching again: %+v", got)
	}

	if _, err := r.Add(books.Book{OLKey: "OL11W", Title: "Other", WantEbook: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Rematch(b.ID, books.Book{OLKey: "OL11W", Title: "X"}); !errors.Is(err, books.ErrExists) {
		t.Errorf("a key another book has: %v", err)
	}
}
