package books

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGuessAudiobookFolderNames(t *testing.T) {
	for _, c := range []struct {
		parts []string
		want  LocalBook
	}{
		{[]string{"Ann Author", "Plain Title"}, LocalBook{Author: "Ann Author", Title: "Plain Title"}},
		{[]string{"Ann Author", "Plain Title (2011) [Unabridged]"}, LocalBook{Author: "Ann Author", Title: "Plain Title", Year: 2011}},
		{[]string{"Ann Author", "Plain Title {Nat Reader}"}, LocalBook{Author: "Ann Author", Title: "Plain Title", Narrators: "Nat Reader"}},
		{[]string{"Ann Author", "Plain Title {Nat Reader & Pat Voice}"}, LocalBook{Author: "Ann Author", Title: "Plain Title", Narrators: "Nat Reader, Pat Voice"}},
		{[]string{"Ann Author", "Plain Title [B0015T963C]"}, LocalBook{Author: "Ann Author", Title: "Plain Title", ASIN: "B0015T963C"}},
		{[]string{"Ann Author", "Plain Title {Nat Reader} [B0015T963C]"}, LocalBook{Author: "Ann Author", Title: "Plain Title", Narrators: "Nat Reader", ASIN: "B0015T963C"}},
		// A lower-case or short note in square brackets isn't an ASIN.
		{[]string{"Ann Author", "Plain Title [abridged]"}, LocalBook{Author: "Ann Author", Title: "Plain Title"}},
		// In a series folder: the forms Audiobookshelf reads.
		{[]string{"Ann Author", "The Saga", "Book 2 - Second Part"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "2", Title: "Second Part"}},
		{[]string{"Ann Author", "The Saga", "Second Part - Vol 12"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "12", Title: "Second Part"}},
		{[]string{"Ann Author", "The Saga", "Vol. 3 Second Part"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "3", Title: "Second Part"}},
		{[]string{"Ann Author", "The Saga", "Volume 12. Second Part"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "12", Title: "Second Part"}},
		{[]string{"Ann Author", "The Saga", "1980 - Book 2 - Second Part"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "2", Title: "Second Part", Year: 1980}},
		{[]string{"Ann Author", "The Saga", "02 - Second Part"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "2", Title: "Second Part"}},
		{[]string{"Ann Author", "The Saga", "0.5 - Prequel"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "0.5", Title: "Prequel"}},
		{[]string{"Ann Author", "The Saga", "6. Second Part"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "6", Title: "Second Part"}},
		{[]string{"Ann Author", "The Saga", "Second Part, Book 4"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "4", Title: "Second Part"}},
		// A title that only starts with a number stays a title.
		{[]string{"Ann Author", "The Saga", "101 Dogs"}, LocalBook{Author: "Ann Author", Series: "The Saga", Title: "101 Dogs"}},
		{[]string{"Ann Author", "The Saga", "1984"}, LocalBook{Author: "Ann Author", Series: "The Saga", Title: "1984"}},
		{[]string{"Ann Author", "The Saga", "Book of Sand"}, LocalBook{Author: "Ann Author", Series: "The Saga", Title: "Book of Sand"}},
		// All at once.
		{[]string{"Ann Author", "The Saga", "Vol 2 - Second Part {Nat Reader} [B0015T963C]"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "2", Title: "Second Part", Narrators: "Nat Reader", ASIN: "B0015T963C"}},
		// The folder is only a number: the series names it.
		{[]string{"Ann Author", "The Saga", "Vol 3"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "3", Title: "The Saga Vol 3"}},
		// Outside a series only a label counts.
		{[]string{"Ann Author", "Book 2 - Second Part"}, LocalBook{Author: "Ann Author", Position: "2", Title: "Second Part"}},
		{[]string{"Ann Author", "3 - Second Part"}, LocalBook{Author: "Ann Author", Title: "Second Part"}},
		// Only the nearest three folders count.
		{[]string{"Fiction", "Ann Author", "The Saga", "Book 1 - Start"}, LocalBook{Author: "Ann Author", Series: "The Saga", Position: "1", Title: "Start"}},
	} {
		got := guessAudiobook(c.parts, false)
		if got != c.want {
			t.Errorf("%v:\n got %+v\nwant %+v", c.parts, got, c.want)
		}
	}
}

func writeFiles(t *testing.T, files map[string]string) {
	t.Helper()
	for p, body := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const sampleOPF = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
 <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
  <dc:title>The Opf Title</dc:title>
  <dc:creator opf:role="aut" opf:file-as="Writer, Olive">Olive Writer</dc:creator>
  <dc:creator opf:role="nrt">Nat Reader</dc:creator>
  <dc:creator opf:role="nrt">Pat Voice</dc:creator>
  <dc:description>&lt;p&gt;An &lt;b&gt;opf&lt;/b&gt; blurb.&lt;/p&gt;</dc:description>
  <dc:date>2014-03-01</dc:date>
  <dc:identifier opf:scheme="ISBN">9780000000000</dc:identifier>
  <dc:identifier opf:scheme="ASIN">B00OPFOPF0</dc:identifier>
  <meta name="calibre:series" content="Opf Saga"/>
  <meta name="calibre:series_index" content="3.0"/>
 </metadata>
</package>`

func TestParseOPF(t *testing.T) {
	want := bookWords{title: "The Opf Title", author: "Olive Writer", narrators: "Nat Reader, Pat Voice", asin: "B00OPFOPF0",
		description: "An opf blurb.", series: "Opf Saga", position: "3", year: 2014}
	if got := parseOPF(sampleOPF); got != want {
		t.Errorf("\n got %+v\nwant %+v", got, want)
	}
	if got := parseOPF("not xml at all <<<"); got != (bookWords{}) {
		t.Errorf("broken: %+v", got)
	}
}

func TestParseAudioMetadataJSON(t *testing.T) {
	got, err := parseAudioMetadataJSON(`{"title":"Json Title","subtitle":"x","authors":["Jo Writer","Other"],"narrators":["Nat Reader","Pat Voice"],
		"series":["Json Saga #2.5"],"publishedYear":"2019","description":"A json blurb.","asin":"B00JSONJS0","chapters":[],"tags":[],"explicit":false}`)
	want := bookWords{title: "Json Title", author: "Jo Writer", narrators: "Nat Reader, Pat Voice", series: "Json Saga", position: "2.5", year: 2019, description: "A json blurb.", asin: "B00JSONJS0"}
	if err != nil || got != want {
		t.Errorf("%v\n got %+v\nwant %+v", err, got, want)
	}
	// Other shapes older files use, and nulls.
	got, err = parseAudioMetadataJSON(`{"title":null,"authors":[{"name":"Jo Writer"}],"series":[{"name":"Saga","sequence":"4"}],"publishedYear":2001,"asin":null,"narrators":null}`)
	want = bookWords{author: "Jo Writer", series: "Saga", position: "4", year: 2001}
	if err != nil || got != want {
		t.Errorf("%v\n got %+v\nwant %+v", err, got, want)
	}
	if _, err := parseAudioMetadataJSON(`{broken`); err == nil {
		t.Error("broken json should say so")
	}
}

func TestScanAudiobooksReadsSideFiles(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, map[string]string{
		// Text files fill in beside the folder name; the reader's first line only.
		filepath.Join(root, "Ann Author", "Texts", "1.mp3"):         "a",
		filepath.Join(root, "Ann Author", "Texts", "desc.txt"):      "\xef\xbb\xbfA blurb.\r\n",
		filepath.Join(root, "Ann Author", "Texts", "Reader.TXT"):    "Nat Reader\r\nengineered by someone\r\n",
		filepath.Join(root, "Ann Author", "Texts", "CD 1", "2.mp3"): "b",
		// The .opf beats the folder name, and the text files.
		filepath.Join(root, "Ann Author", "Folder Title {Folder Reader}", "1.m4b"):      "a",
		filepath.Join(root, "Ann Author", "Folder Title {Folder Reader}", "x.opf"):      sampleOPF,
		filepath.Join(root, "Ann Author", "Folder Title {Folder Reader}", "reader.txt"): "Text Reader",
		// metadata.json beats the .opf; what it doesn't say stays.
		filepath.Join(root, "Ann Author", "Both", "1.m4b"):         "a",
		filepath.Join(root, "Ann Author", "Both", "x.opf"):         sampleOPF,
		filepath.Join(root, "Ann Author", "Both", "metadata.json"): `{"title":"Json Title","narrators":["Json Reader"]}`,
		// A broken file says nothing.
		filepath.Join(root, "Ann Author", "Broken", "1.m4b"):         "a",
		filepath.Join(root, "Ann Author", "Broken", "metadata.json"): `{nope`,
		filepath.Join(root, "Ann Author", "Broken", "bad.opf"):       `<<<`,
		// Side files in the root belong to no book.
		filepath.Join(root, "metadata.json"):          `{"title":"Wrong"}`,
		filepath.Join(root, "Ann Author - Loose.mp3"): "a",
	})
	list, err := ScanAudiobooks(root)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]LocalBook{}
	for _, b := range list {
		by[filepath.Base(b.Path)] = b
	}
	if b := by["Texts"]; b.Title != "Texts" || b.Description != "A blurb." || b.Narrators != "Nat Reader" || b.Files != 2 {
		t.Errorf("text files: %+v", b)
	}
	if b := by["Folder Title {Folder Reader}"]; b.Title != "The Opf Title" || b.Author != "Olive Writer" || b.Narrators != "Nat Reader, Pat Voice" ||
		b.ASIN != "B00OPFOPF0" || b.Series != "Opf Saga" || b.Position != "3" || b.Year != 2014 {
		t.Errorf("opf: %+v", b)
	}
	if b := by["Both"]; b.Title != "Json Title" || b.Author != "Olive Writer" || b.Narrators != "Json Reader" {
		t.Errorf("metadata.json over opf: %+v", b)
	}
	if b := by["Broken"]; b.Title != "Broken" || b.Author != "Ann Author" {
		t.Errorf("broken side files: %+v", b)
	}
	if b := by["Ann Author - Loose.mp3"]; b.Title != "Loose" || !b.Loose || b.FirstAudio == "" {
		t.Errorf("loose: %+v", b)
	}
}

func TestApplyAudioTagsFillsOnlyWhatIsEmpty(t *testing.T) {
	tags := map[string]string{"artist": "Tag Writer", "album": "Tag Album", "composer": "Tag Reader; Other Voice", "asin": "B00TAGTAG0",
		"description": "A tag blurb.", "date": "2003-05-01", "series": "Tag Saga", "series-part": "5/9"}
	empty := LocalBook{Title: "Folder Title", FirstAudio: "x.mp3"}
	if !empty.NeedsAudioTags() {
		t.Error("an empty book needs its tags")
	}
	ApplyAudioTags(&empty, tags)
	want := LocalBook{Title: "Folder Title", FirstAudio: "x.mp3", TagsRead: true, Author: "Tag Writer", Narrators: "Tag Reader, Other Voice",
		ASIN: "B00TAGTAG0", Description: "A tag blurb.", Year: 2003, Series: "Tag Saga", Position: "5"}
	if empty != want {
		t.Errorf("\n got %+v\nwant %+v", empty, want)
	}
	if empty.NeedsAudioTags() {
		t.Error("tags are read once")
	}
	full := LocalBook{Title: "T", Author: "A", Narrators: "N", ASIN: "B00FULLFU0", Description: "D", Year: 1999, Series: "S", Position: "1"}
	before := full
	ApplyAudioTags(&full, tags)
	full.TagsRead = false
	if full != before {
		t.Errorf("overwritten: %+v", full)
	}
	loose := LocalBook{Title: "audio final", Loose: true}
	ApplyAudioTags(&loose, tags)
	if loose.Title != "Tag Album" {
		t.Errorf("a loose file takes its album: %+v", loose)
	}
	short := LocalBook{Title: "T"}
	ApplyAudioTags(&short, map[string]string{"comment": "ripped by someone", "asin": "not-an-asin"})
	if short.Description != "" || short.ASIN != "" {
		t.Errorf("a short comment or a bad ASIN says nothing: %+v", short)
	}
}
