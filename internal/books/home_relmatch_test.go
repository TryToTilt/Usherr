package books

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatchReleaseOnRealNames(t *testing.T) {
	meyer := func(title, pos string) Book {
		return Book{Title: title, Author: "Stephenie Meyer", SeriesPosition: pos}
	}
	cases := []struct {
		release string
		book    Book
		want    ReleaseMatch
	}{
		// single books, however they are written
		{"Stephenie Meyer - Twilight -Epub", meyer("Twilight", "1"), SingleBook},
		{"Twilight (The Twilight Saga series_book 1) by Stephenie Meyer ep", meyer("Twilight", "1"), SingleBook},
		{"Twilight - Breaking Dawn By Stephenie Meyer E-Books - {RedDragon", meyer("Breaking Dawn", "4"), SingleBook},
		{"Stephenie Meyer - Twilight Saga 2 - New Moon MP3", meyer("New Moon", "2"), SingleBook},
		{"Stephenie.Meyer.Midnight.Sun.2020.RETAiL.ePub.eBook-VENTOLiN", meyer("Midnight Sun", "5"), SingleBook},
		{"[EPUB] The Chemist by Stephenie Meyer", meyer("The Chemist", ""), SingleBook},
		{"Meyer, Stephenie - Twilight 1, ebook for mobile phones", meyer("Twilight", "1"), SingleBook},
		// another book of the series is not this one
		{"Meyer, Stephenie - Twilight 2 - New Moon ebook for mobile phones", meyer("Twilight", "1"), NoMatch},
		{"Twilight - Breaking Dawn By Stephenie Meyer E-Books - {RedDragon", meyer("Twilight", "1"), NoMatch},
		{"Stephenie Meyer - [Twilight] - Life and Death- Twilight Reimagined (retail) (epub)", meyer("Twilight", "1"), NoMatch},
		{"Meyer  Stephenie Twilight  - Eclipse 1 - {RedDragon}", meyer("Twilight", "1"), NoMatch},
		// other languages
		{"Stephenie Meyer - 01 - Twilight-Saga - Bis(s) Zum Morgengrauen", meyer("Twilight", "1"), NoMatch},
		{"Stephenie Meyer - New Moon, [Dod Lit Pdf Rtf - Ita]  [TNTVillage", meyer("New Moon", "2"), NoMatch},
		{"Stephenie Meyer - Twilight Om Jag Kunde Droemma-AUDiOBOOK-WEB-SE-2009-CRAViNGS iNT", meyer("Twilight", "1"), NoMatch},
		{"Stephenie Meyer-New Moon (2006), NL Audiobook(mp3), DMT", meyer("New Moon", "2"), NoMatch},
		{"Audiobook. Stephenie Meyer- de Twilightsage - boek 1 Twilight", meyer("Twilight", "1"), NoMatch},
		// packs
		{"The Twilight Saga Novel Collection 1 5 English Ebook Twilight New Moon Eclipse Breaking Dawn Midnight Sun Stephenie Meyer EPUB-KBC", meyer("Eclipse", "3"), SeriesPack},
		{"Stephenie Meyer-The Twilight Saga 1-5 [Unabridged] [m4b]", meyer("Twilight", "1"), SeriesPack},
		// split archives and posts aren't releases
		{`"Stephenie Meyer - Saga Twilight - Tome 1 - Fascination.par2" yEncab`, meyer("Twilight", "1"), NoMatch},
		// other authors
		{"Twilight by Stephenie Meyer", Book{Title: "Twilight", Author: "Erin Hunter"}, NoMatch},
	}
	for _, c := range cases {
		if got := MatchRelease(c.release, c.book); got != c.want {
			t.Errorf("%q for %q: got %d, want %d", c.release, c.book.Title, got, c.want)
		}
	}
}

func TestPickTheBookOutOfAPack(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Twilight Saga/01 - Twilight.epub", "Twilight Saga/02 - New Moon.epub", "Twilight Saga/03 - Eclipse.epub", "Twilight Saga/04 - Breaking Dawn.epub"} {
		p := filepath.Join(dir, n)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(n), 0o644)
	}
	got, _ := EbookFileFor(dir, Book{Title: "Eclipse", Author: "Stephenie Meyer", SeriesPosition: "3"}, true)
	if filepath.Base(got) != "03 - Eclipse.epub" {
		t.Errorf("picked %q", got)
	}
	if got, _ := EbookFileFor(dir, Book{Title: "Midnight Sun"}, true); got != "" {
		t.Errorf("a pack without the book gave %q", got)
	}

	adir := t.TempDir()
	for _, n := range []string{"Book 1 - Twilight/01.m4b", "Book 1 - Twilight/02.m4b", "Book 2 - New Moon/01.m4b"} {
		p := filepath.Join(adir, n)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(n), 0o644)
	}
	files, _ := AudioFilesFor(adir, Book{Title: "Twilight", SeriesPosition: "1"}, true)
	if len(files) != 2 {
		t.Errorf("audiobook from a pack: %q", files)
	}
	if files, _ := AudioFilesFor(adir, Book{Title: "Eclipse"}, true); files != nil {
		t.Errorf("a pack without the book gave %q", files)
	}
}
