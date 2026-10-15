package books

// Home fork: a stricter, wider book release matcher. Checked against what
// indexers really return for Stephenie Meyer (testdata/meyer_releases.json):
//
//   - Series notes in brackets no longer hide the title: "Twilight (The
//     Twilight Saga series_book 1) by Stephenie Meyer" is Twilight.
//   - A series name in front of the title is fine ("Twilight - Breaking
//     Dawn"), but "Twilight 2 - New Moon" is not Twilight: a number followed
//     by another title is a series position.
//   - Other-language editions are left out ("Bis(s) zum Morgengrauen",
//     "[ITA]", "SWEDiSH", "NL Audiobook").
//   - Series packs ("The Twilight Saga Novel Collection 1-5 … EPUB") are
//     offered when no single release is found; the book is then picked out
//     of the pack by its file or folder name.

import (
	"html"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	segmentBreak = regexp.MustCompile(`[|]|\s[-–:]\s|\s[-–]|[-–]\s|\s\.\s`)
	bracketOpen  = regexp.MustCompile(`[\(\[\{]`)
	bracketClose = regexp.MustCompile(`[\)\]\}]`)
	langWords    = map[string]bool{
		"german": true, "deutsch": true, "french": true, "francais": true, "italiano": true, "italian": true, "spanish": true, "espanol": true,
		"dutch": true, "nederlands": true, "swedish": true, "svensk": true, "danish": true, "dansk": true, "hungarian": true, "arabic": true,
		"polish": true, "russian": true, "portuguese": true, "ljudbok": true, "luisterboek": true, "luisterboeken": true, "audioboek": true,
		"horbuch": true, "hoerbuch": true, "ungekurzt": true, "ungekuerzt": true, "gekurzt": true, "libros": true, "libri": true, "boeken": true,
		"tome": true, "tomo": true, "boek": true, "deel": true, "livre": true, "libro": true, "ita": true, "ger": true, "hun": true, "nld": true,
		"tntvillage": true, "crepusculo": true,
	}
	langTag      = regexp.MustCompile(`(?:^|[\s\[\(\-_.])(NL|FR|ES|SE|DK|IT|DE|PT|RU|PL|HUN|ITA|GER|SWE|NOR|FIN|ESP|SPA|ARA)(?:$|[\s\]\)\-_.,])`)
	germanBis    = regexp.MustCompile(`(?i)\bbis\(s\)`)
	packMarker   = regexp.MustCompile(`(?i)\b(\d+\s*-\s*\d+|\d+\s+(?:to|-)\s+\d+|collection|books\s+\d+|novels|box\s*set|boxset|omnibus|complete|trilogy|quartet)\b`)
	numRange     = regexp.MustCompile(`\b\d+\s+\d+\b`)
	splitArchive = regexp.MustCompile(`(?i)\.(par2|part\d+\.rar|r\d\d|vol\d+\+\d+\.par2)\b|yenc`)
)

// normRelease decodes HTML entities (some feeds double-encode "&amp;amp;")
// and straightens quotes.
func normRelease(s string) string {
	for i := 0; i < 2; i++ {
		s = html.UnescapeString(s)
	}
	return strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`, "_", " ").Replace(s)
}

// OtherLanguage reports whether a release is a translation or a foreign
// edition, so an English library isn't filled with them.
func OtherLanguage(release string) bool {
	r := normRelease(release)
	if germanBis.MatchString(r) || langTag.MatchString(r) {
		return true
	}
	for _, w := range words(r) {
		if langWords[w] {
			return true
		}
	}
	return false
}

// segWords splits a release into words, with "|" where a bracket or a
// spaced dash separates parts of the name.
func segWords(release string) []string {
	s := segmentBreak.ReplaceAllString(normRelease(release), " \x01 ")
	s = bracketOpen.ReplaceAllString(s, " \x02 ")
	s = bracketClose.ReplaceAllString(s, " \x03 ")
	var out []string
	for _, f := range strings.Fields(s) {
		switch f {
		case "\x01":
			out = append(out, "|")
		case "\x02":
			out = append(out, "(")
		case "\x03":
			out = append(out, ")")
		default:
			out = append(out, words(f)...)
		}
	}
	return out
}

func isBreak(w string) bool { return w == "|" || w == "(" || w == ")" }

// insideBrackets reports whether position i is within "( … )".
func insideBrackets(rel []string, i int) bool {
	for k := i - 1; k >= 0; k-- {
		if rel[k] == ")" {
			return false
		}
		if rel[k] == "(" {
			return true
		}
	}
	return false
}

// ReleaseMatch says how a release relates to a book.
type ReleaseMatch int

const (
	NoMatch    ReleaseMatch = iota
	SingleBook              // the book on its own
	SeriesPack              // several books, this one among them
)

// MatchRelease decides whether release is book b, a pack with b in it, or
// neither.
func MatchRelease(release string, b Book) ReleaseMatch {
	if splitArchive.MatchString(release) || OtherLanguage(release) {
		return NoMatch
	}
	rel := segWords(release)
	have := map[string]bool{}
	for _, w := range rel {
		have[w] = true
	}
	author := words(b.Author)
	if len(author) > 0 && !have[author[len(author)-1]] {
		return NoMatch
	}
	authorSet := map[string]bool{}
	for _, w := range author {
		authorSet[w] = true
	}
	title := words(normRelease(b.Title))
	for len(title) > 0 && small[title[0]] && len(title) > 1 {
		if single(rel, title, authorSet, b) {
			return SingleBook
		}
		title = title[1:] // a leading "The"/"A" is often left out
	}
	if single(rel, title, authorSet, b) {
		return SingleBook
	}
	if pack(release, rel, words(normRelease(b.Title))) {
		return SeriesPack
	}
	return NoMatch
}

func edgeWord(w string, author map[string]bool) bool {
	return isBreak(w) || author[w] || noiseWord[w] || EbookRank(w) > 0 || AudioRank(w) > 0 || isNumber(w)
}

// single finds title as a run of words bounded by the release's edges. A
// run followed by "<number> |" and more title words is a series position
// ("Twilight 2 - New Moon"): it only counts when the number is the book's.
func single(rel, title []string, author map[string]bool, b Book) bool {
	important := 0
	for _, w := range title {
		if !small[w] {
			important++
		}
	}
	if important == 0 {
		return false
	}
	for i := 0; i+len(title) <= len(rel); i++ {
		match := true
		for k, w := range title {
			if rel[i+k] != w {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		end := i + len(title)
		before := i == 0 || edgeWord(rel[i-1], author)
		after := end == len(rel) || edgeWord(rel[end], author)
		if !before || !after {
			continue
		}
		// Inside brackets after another title it is a series note:
		// "Midnight Sun (Twilight 1 5)".
		if insideBrackets(rel, i) && titleWordsBefore(rel[:i], author) {
			continue
		}
		// "Twilight 2 - New Moon": a series name, number, then another title.
		if end+2 < len(rel) && isNumber(rel[end]) && rel[end+1] == "|" && titleWordsFollow(rel[end+2:], author) {
			if b.SeriesPosition == "" || strings.TrimLeft(rel[end], "0") != strings.TrimLeft(b.SeriesPosition, "0") {
				continue
			}
		}
		// "Twilight - Eclipse", "[Twilight] - Life and Death": another title
		// after a dash makes this one the series name (a note in brackets,
		// like "(The Twilight Saga series_book 1)", doesn't).
		if k := nextAfterBreaks(rel, end); k >= 0 && rel[k-1] == "|" && titleWordsFollow(rel[k:], author) && !seriesNote(rel[k:]) {
			continue
		}
		return true
	}
	return false
}

// titleWordsFollow reports whether rest starts with words that read like
// another title (not the author, a format, a number or noise).
func titleWordsFollow(rest []string, author map[string]bool) bool {
	n := 0
	for _, w := range rest {
		if isBreak(w) {
			break
		}
		if edgeWord(w, author) || small[w] {
			continue
		}
		n++
	}
	return n >= 1
}

// nextAfterBreaks skips the breaks (and closing brackets) after position
// end and returns the next word's index, or -1.
func nextAfterBreaks(rel []string, end int) int {
	k := end
	for k < len(rel) && (rel[k] == ")" || rel[k] == "|") {
		k++
	}
	if k == end || k >= len(rel) {
		return -1
	}
	return k
}

func titleWordsBefore(before []string, author map[string]bool) bool {
	for _, w := range before {
		if !edgeWord(w, author) && !small[w] && w != "(" {
			return true
		}
	}
	return false
}

// seriesNote reports whether the words up to the next break describe a
// series rather than name another book.
func seriesNote(rest []string) bool {
	for k, w := range rest {
		if isBreak(w) {
			break
		}
		switch w {
		case "saga", "series", "serie", "trilogy", "cycle", "chronicles":
			return true
		case "book", "books", "volume", "vol": // "book 1", not "e-books"
			if k+1 < len(rest) && isNumber(rest[k+1]) {
				return true
			}
		}
	}
	return false
}

// pack reports whether a release is a multi-book pack naming the title.
func pack(raw string, rel, title []string) bool {
	r := normRelease(raw)
	if !packMarker.MatchString(r) && !numRange.MatchString(strings.Join(rel, " ")) {
		return false
	}
	var plain []string
	for _, w := range rel {
		if !isBreak(w) {
			plain = append(plain, w)
		}
	}
	joined := " " + strings.Join(plain, " ") + " "
	t := " " + strings.Join(title, " ") + " "
	return strings.Contains(joined, t)
}

// ---- picking the book out of a finished download ----

// EbookFileFor is EbookFile, but in a download holding several books (a
// series pack) it picks the file named after b. With strict (the release
// was a pack), a download without such a file gives nothing rather than
// another book.
func EbookFileFor(dir string, b Book, strict bool, opts ...FindOption) (path, format string) {
	var named []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && EbookRank(filepath.Ext(p)) > 0 && fileNamesBook(p, dir, b) {
			named = append(named, p)
		}
		return nil
	})
	if len(named) == 0 {
		if strict {
			return "", ""
		}
		return EbookFile(dir, opts...)
	}
	sort.SliceStable(named, func(i, j int) bool {
		return EbookRank(filepath.Ext(named[i])) > EbookRank(filepath.Ext(named[j]))
	})
	return named[0], strings.TrimPrefix(strings.ToLower(filepath.Ext(named[0])), ".")
}

// AudioFilesFor is AudioFiles, but when the download holds several books
// (audio in more than one folder, or files named after different books) it
// keeps only b's.
func AudioFilesFor(dir string, b Book, strict bool, opts ...FindOption) (files []string, format string) {
	all, format := AudioFiles(dir, opts...)
	if len(all) == 0 {
		return all, format
	}
	folders := map[string]bool{}
	for _, f := range all {
		folders[filepath.Dir(f)] = true
	}
	var mine []string
	for _, f := range all {
		if fileNamesBook(f, dir, b) {
			mine = append(mine, f)
		}
	}
	if len(mine) == 0 {
		if strict && len(folders) > 1 {
			return nil, ""
		}
		return all, format
	}
	return mine, format
}

// fileNamesBook reports whether a file's path below dir names book b (its
// folder or its own name has the title as a bounded run).
func fileNamesBook(p, dir string, b Book) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		rel = filepath.Base(p)
	}
	name := strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel))
	name = strings.ReplaceAll(name, "/", " | ")
	title := words(normRelease(b.Title))
	for len(title) > 1 && small[title[0]] {
		title = title[1:]
	}
	return single(segWords(name), title, map[string]bool{}, Book{SeriesPosition: b.SeriesPosition})
}
