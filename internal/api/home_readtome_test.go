package api

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testEPUB builds a small EPUB 3: a cover, a copyright page, a table of
// contents page, two chapters (the first in two files, only one of them in
// the table of contents), and a page with next to nothing on it.
func testEPUB(t *testing.T, ncxOnly bool) string {
	t.Helper()
	long := func(prefix string, n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteString(prefix + " word number " + strings.Repeat("x", i%3) + " goes here. ")
		}
		return b.String()
	}
	page := func(body string, epubType string) string {
		return `<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title>x</title><style>p{color:red}</style><script>var hidden = "no";</script></head><body epub:type="` + epubType + `">` + body + `</body></html>`
	}
	files := map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"OEBPS/cover.xhtml":      page(`<h1>The Test Book</h1><p>By A. Writer, and a long line about the cover that has more than twenty words in it so only its name or kind can leave it out of the reading.</p>`, "cover"),
		"OEBPS/copyright.xhtml":  page(`<h1>Copyright</h1><p>`+long("Rights", 8)+`</p>`, "copyright-page"),
		"OEBPS/toc.xhtml":        page(`<h1>Contents</h1><p>`+long("Entry", 8)+`</p>`, "toc"),
		"OEBPS/ch1.xhtml":        page(`<h1>One</h1><p>`+long("First", 6)+`<a epub:type="noteref" href="#n1"><sup>1</sup></a> end of paragraph.</p><p>Second<br/>paragraph with a break.</p><p>`+long("More", 4)+`</p>`, "bodymatter chapter"),
		"OEBPS/ch1b.xhtml":       page(`<p>`+long("Continued", 5)+`</p>`, "bodymatter"),
		"OEBPS/ch2.xhtml":        page(`<h2>Two</h2><p>`+long("Later", 9)+`</p><p>“Run now,” said Tom.</p><p>“Where?” Mary asked.</p><p>“Away,” said Tom.</p>`, "bodymatter chapter"),
		"OEBPS/blank.xhtml":      page(`<p>Nothing here.</p>`, "bodymatter"),
	}
	var cover bytes.Buffer
	pix := image.NewRGBA(image.Rect(0, 0, 16, 24))
	for x := 0; x < 16; x++ {
		for y := 0; y < 24; y++ {
			pix.Set(x, y, color.RGBA{R: uint8(x * 15), G: uint8(y * 10), B: 120, A: 255})
		}
	}
	_ = png.Encode(&cover, pix)
	files["OEBPS/cover.png"] = cover.String()
	nav := `<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol>
		<li><a href="copyright.xhtml">Copyright</a></li><li><a href="ch1.xhtml">Chapter One</a></li><li><a href="ch2.xhtml#start">Chapter Two</a></li></ol></nav></body></html>`
	ncx := `<?xml version="1.0"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap>
		<navPoint id="a"><navLabel><text>Chapter One</text></navLabel><content src="ch1.xhtml"/></navPoint>
		<navPoint id="b"><navLabel><text>Chapter Two</text></navLabel><content src="ch2.xhtml"/></navPoint></navMap></ncx>`
	manifestNav := `<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`
	spineTOC := ``
	if ncxOnly {
		manifestNav = `<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>`
		spineTOC = ` toc="ncx"`
		files["OEBPS/toc.ncx"] = ncx
	} else {
		files["OEBPS/nav.xhtml"] = nav
	}
	files["OEBPS/content.opf"] = `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>The Test Book</dc:title><dc:creator>A. Writer</dc:creator></metadata><manifest>` + manifestNav + `
		<item id="coverimg" href="cover.png" media-type="image/png" properties="cover-image"/><item id="cover" href="cover.xhtml" media-type="application/xhtml+xml"/><item id="copy" href="copyright.xhtml" media-type="application/xhtml+xml"/><item id="tocpage" href="toc.xhtml" media-type="application/xhtml+xml"/>
		<item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/><item id="c1b" href="ch1b.xhtml" media-type="application/xhtml+xml"/><item id="c2" href="ch2.xhtml" media-type="application/xhtml+xml"/><item id="blank" href="blank.xhtml" media-type="application/xhtml+xml"/>
		</manifest><spine` + spineTOC + `><itemref idref="cover"/><itemref idref="copy"/><itemref idref="tocpage"/><itemref idref="c1"/><itemref idref="c1b"/><itemref idref="blank"/><itemref idref="c2"/></spine></package>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "test.epub")
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestReadEPUBChapters(t *testing.T) {
	for _, ncxOnly := range []bool{false, true} {
		book, err := readEPUBBook(testEPUB(t, ncxOnly))
		if err != nil {
			t.Fatalf("ncxOnly=%v: %v", ncxOnly, err)
		}
		if book.Title != "The Test Book" || book.Author != "A. Writer" {
			t.Errorf("ncxOnly=%v: %q by %q", ncxOnly, book.Title, book.Author)
		}
		// The cover, the copyright page, the contents page and the near-empty page are left out.
		if len(book.Chapters) != 2 {
			t.Fatalf("ncxOnly=%v: %d chapters: %+v", ncxOnly, len(book.Chapters), book.Chapters)
		}
		one, two := book.Chapters[0], book.Chapters[1]
		if one.Title != "Chapter One" || two.Title != "Chapter Two" {
			t.Errorf("ncxOnly=%v: titles %q, %q", ncxOnly, one.Title, two.Title)
		}
		// The file the table doesn't name belongs to the chapter before it.
		if !strings.Contains(one.Text, "First word") || !strings.Contains(one.Text, "Continued word") || strings.Contains(one.Text, "Later") {
			t.Errorf("ncxOnly=%v: chapter one: %.200q", ncxOnly, one.Text)
		}
		for _, bad := range []string{"hidden", "color:red", "<p>", "number xx goes here. 1 end"} {
			if strings.Contains(one.Text, bad) {
				t.Errorf("ncxOnly=%v: %q is in the text", ncxOnly, bad)
			}
		}
		// The footnote marker is gone, the paragraphs are kept, a break is a space.
		if strings.Contains(one.Text, "here.1") || strings.Contains(one.Text, "here. 1 end") {
			t.Errorf("ncxOnly=%v: the footnote marker was read: %.300q", ncxOnly, one.Text)
		}
		if !strings.Contains(one.Text, "\n\n") || !strings.Contains(one.Text, "Second paragraph with a break.") {
			t.Errorf("ncxOnly=%v: paragraphs: %.400q", ncxOnly, one.Text)
		}
	}
}

func TestReadEPUBRefusesWhatIsNotOne(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x.epub")
	_ = os.WriteFile(file, []byte("not a zip"), 0o644)
	if _, err := readEPUBBook(file); err == nil {
		t.Fatal("a text file was read as an EPUB")
	}
	if _, err := readEPUBBook(filepath.Join(t.TempDir(), "missing.epub")); err == nil {
		t.Fatal("a missing file was read as an EPUB")
	}
}

func TestFirstWords(t *testing.T) {
	text := strings.Repeat("word ", 195) + "and the sentence ends here. Then more words follow without end"
	got := firstWords(text, 200)
	if !strings.HasSuffix(got, "ends here.") {
		t.Errorf("not cut at the sentence end: ...%q", got[len(got)-30:])
	}
	if short := firstWords("only a few words", 200); short != "only a few words" {
		t.Errorf("short: %q", short)
	}
	if far := firstWords(strings.Repeat("word ", 300), 200); len(strings.Fields(far)) != 200 {
		t.Errorf("no sentence end near: %d words", len(strings.Fields(far)))
	}
	// Paragraph breaks are kept: a heading stays apart from the first paragraph.
	if kept := firstWords("Chapter One\n\nFirst paragraph is here.\n\nSecond paragraph.", 5); kept != "Chapter One\n\nFirst paragraph is here." {
		t.Errorf("paragraphs lost: %q", kept)
	}
}

func TestSplitForSpeech(t *testing.T) {
	text := "Mr. Smith went to St. Louis. He said, \"Hello there!\" Then he left. J. R. Ward wrote a book.\n\nA new paragraph starts here, and it is short.\n\n" +
		strings.Repeat("A very long sentence with many words, and commas, and more words, going on and on ", 12) + "end."
	pieces := splitForSpeech(text, 200)
	if len(pieces) < 4 {
		t.Fatalf("%d pieces: %q", len(pieces), pieces)
	}
	for _, p := range pieces {
		if n := len([]rune(p)); n > 200 || n == 0 {
			t.Errorf("a piece of %d characters: %.60q", n, p)
		}
	}
	// "Mr." and "St." and an initial don't end a sentence.
	if !strings.HasPrefix(pieces[0], "Mr. Smith went to St. Louis.") {
		t.Errorf("an abbreviation ended a sentence: %q", pieces[0])
	}
	for _, p := range pieces {
		if strings.Contains(p, "Louis.") && strings.Contains(p, "paragraph starts") {
			t.Errorf("two paragraphs in one piece: %q", p)
		}
	}
	// Nothing is lost.
	if strings.Join(strings.Fields(strings.Join(pieces, " ")), " ") != strings.Join(strings.Fields(text), " ") {
		t.Errorf("the pieces don't make the text: %q", pieces)
	}
}

func TestKokoroVoicesParsedAndSorted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"voices":[
			{"id":"bm_george","name":"bm_george","overall_grade":"C"},
			{"id":"af_heart","name":"af_heart","overall_grade":"A"},
			{"id":"ff_siwis","name":"ff_siwis","overall_grade":"B-"},
			{"id":"af_amelia_inno","name":"af_amelia_inno"},
			"am_adam"]}`))
	}))
	defer srv.Close()
	vs, err := kokoroEngine{base: srv.URL}.voices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, v := range vs {
		ids = append(ids, v.ID)
	}
	// Only English voices; the best graded first; ones without a grade last.
	if strings.Join(ids, ",") != "af_heart,bm_george,af_amelia_inno,am_adam" {
		t.Fatalf("voices: %v", ids)
	}
	if vs[0].Label != "Heart · American · woman" || vs[1].Label != "George · British · man" || vs[2].Label != "Amelia · American · woman" {
		t.Errorf("labels: %q %q %q", vs[0].Label, vs[1].Label, vs[2].Label)
	}
}

func TestChapterPiecesSpeakTheTitleOnce(t *testing.T) {
	// The title is spoken first, unless the text starts with it already.
	ps := chapterPieces(rtvChapter{Title: "Chapter One", Text: "It was a dark night."}, 300, nil, true)
	if len(ps) != 2 || ps[0].Text != "Chapter One." || !ps[0].ParaEnd || ps[1].Text != "It was a dark night." {
		t.Fatalf("pieces: %+v", ps)
	}
	ps = chapterPieces(rtvChapter{Title: "Chapter One", Text: "Chapter One\n\nIt was a dark night."}, 300, nil, true)
	if len(ps) != 2 || ps[0].Text != "Chapter One" || ps[1].Text != "It was a dark night." {
		t.Fatalf("the title was said twice: %+v", ps)
	}
	if ps := chapterPieces(rtvChapter{Title: "Chapter One", Text: "It was a dark night."}, 300, nil, false); len(ps) != 1 {
		t.Fatalf("a title was read when titles are off: %+v", ps)
	}
	// Only the last piece of a paragraph marks its end.
	ps = splitSpeech("The first sentence has about fifty characters in it ok. The second sentence has about fifty characters too.\n\nSeven eight.", 80)
	var ends []bool
	for _, p := range ps {
		ends = append(ends, p.ParaEnd)
	}
	if len(ps) < 3 || !ps[len(ps)-1].ParaEnd || ps[0].ParaEnd {
		t.Fatalf("paragraph ends: %v for %+v", ends, ps)
	}
}

func TestFFMetadataEscapes(t *testing.T) {
	m := ffMetadata(bookMeta{Title: "A=B; C#D", Author: `Back\slash`, ReadBy: "Read by Usherr · Heart", Year: 2001},
		[]chapterMark{{Title: "One", Start: 0, End: 1500 * time.Millisecond}, {Title: "Two = 2", Start: 1500 * time.Millisecond, End: 4 * time.Second}})
	for _, want := range []string{";FFMETADATA1", `title=A\=B\; C\#D`, `artist=Back\\slash`, "composer=Read by Usherr · Heart", "date=2001", "START=0\nEND=1500\ntitle=One", `START=1500` + "\nEND=4000\n" + `title=Two \= 2`} {
		if !strings.Contains(m, want) {
			t.Errorf("no %q in:\n%s", want, m)
		}
	}
}

func TestRTVFreeNameNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	first := rtvFreeName(dir, "Book", ".m4b")
	if filepath.Base(first) != "Book.m4b" {
		t.Fatal(first)
	}
	_ = os.WriteFile(first, []byte("x"), 0o644)
	second := rtvFreeName(dir, "Book", ".m4b")
	_ = os.WriteFile(second, []byte("x"), 0o644)
	third := rtvFreeName(dir, "Book", ".m4b")
	if filepath.Base(second) != "Book (Read-to-Me).m4b" || filepath.Base(third) != "Book (Read-to-Me 2).m4b" {
		t.Fatalf("%s, %s", second, third)
	}
}

func TestInNightWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 10, 25, h, m, 0, 0, time.Local) }
	for _, c := range []struct {
		from, to string
		now      time.Time
		want     bool
	}{
		{"23:00", "07:00", at(23, 0), true}, // across midnight: the start counts
		{"23:00", "07:00", at(3, 30), true},
		{"23:00", "07:00", at(6, 59), true},
		{"23:00", "07:00", at(7, 0), false}, // the end doesn't
		{"23:00", "07:00", at(12, 0), false},
		{"01:00", "05:00", at(2, 0), true}, // inside one day
		{"01:00", "05:00", at(5, 0), false},
		{"01:00", "05:00", at(23, 0), false},
		{"08:00", "08:00", at(15, 0), true},    // equal: all day
		{"nonsense", "07:00", at(15, 0), true}, // unreadable: no limit
	} {
		if got := inNightWindow(c.now, c.from, c.to); got != c.want {
			t.Errorf("inNightWindow(%s, %s-%s) = %v, want %v", c.now.Format("15:04"), c.from, c.to, got, c.want)
		}
	}
}
