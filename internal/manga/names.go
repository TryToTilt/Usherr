// Package manga is the Manga section's own logic: reading release and file
// names, telling an official English edition from a fan scan, judging a search
// result, the search words, and the series, volumes and search log in the
// database. It knows nothing of comics' issues: a manga series is counted in
// volumes.
//
// The name parsing follows the ideas of Mangarr (Kha-kis/manga-arr, AGPL-3.0):
// ranges before single volumes, numbers as decimals so half volumes work, a
// language reject list. It is written anew in Go; no code is copied.
package manga

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Kind is what a name says it holds.
type Kind string

const (
	KindVolume  Kind = "volume"  // one volume: "v01", "Vol. 1", "Volume 1"
	KindPack    Kind = "pack"    // a run of volumes: "v01-v50"
	KindWhole   Kind = "whole"   // a whole series with no numbers: "Complete"
	KindChapter Kind = "chapter" // one chapter or a run of them: "c123", "ch.123", "Chapter 123"
	KindBare    Kind = "bare"    // a number with no word for what it counts: "Name 023"
	KindRange   Kind = "range"   // a range with no word for what it counts: "Name 001-016"
	KindNone    Kind = "none"    // no number at all
)

// Name is what a release or file name says.
type Name struct {
	Series  string  // the known name it starts with, or the words before the numbers
	Known   bool    // it starts with one of the names asked about
	Extra   string  // words between the name and the numbers: another series, unless they're noise
	Kind    Kind    //
	Number  string  // the volume, normalised: "12", "12.5" ("" for a pack)
	Lo, Hi  float64 // a pack's range
	Chapter string  // the chapter, for a chapter name
	// ChapterTo is the last chapter, for a run of them: "Chapters 1-721".
	ChapterTo string
	Year      int
	Edition   string // "omnibus" and the like: numbered apart from the regular volumes
}

var (
	bracketed = regexp.MustCompile(`[\(\[\{][^\)\]\}]*[\)\]\}]`)
	yearOnly  = regexp.MustCompile(`^\s*((?:19|20)\d\d)\s*$`)
	bareYear  = regexp.MustCompile(`(?:^|[\s,])((?:19|20)\d\d)(?:$|[\s,])`)
	fileExt   = regexp.MustCompile(`(?i)\.(cbz|cbr|cb7|cbt|pdf|epub|zip|rar|7z|nzb)$`)
	dotInWord = regexp.MustCompile(`(\D)\.|\.(\D)`)

	number = `0*(\d{1,4}(?:\.\d{1,2})?)`
	volTag = `(?:volumes?|vols?\.?|v\.?)`
	dash   = `\s*(?:-|–|—|~|\bto\b|\bthru\b)\s*`

	volRange  = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])` + volTag + `\s*` + number + dash + `(?:` + volTag + `\s*)?` + number + `(?:$|[^\p{L}\p{N}])`)
	oneVol    = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])` + volTag + `\s*` + number + `(?:$|[^\p{L}\p{N}])`)
	chapRange = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(?:chapters?\s*|chap\.?\s*|ch\.?\s*|c)` + number + `(?:` + dash + `(?:chapters?\s*|chap\.?\s*|ch\.?\s*|c)?` + number + `)?(?:$|[^\p{L}\p{N}])`)
	bareRange = regexp.MustCompile(`(?:^|\s)` + number + dash + number + `(?:$|\s)`)
	hashNum   = regexp.MustCompile(`#\s*` + number + `(?:$|[^\p{L}\p{N}])`)
	plainNum  = regexp.MustCompile(`^\d{1,4}(?:\.\d{1,2})?$`)
	wholeWord = regexp.MustCompile(`(?i)\b(complete|completed|full series|whole series|all volumes|collection)\b`)
	wordRe    = regexp.MustCompile(`[\p{L}\p{N}]+`)

	editionWord = regexp.MustCompile(`(?i)\b(omnibus|colossal|viz\s?big|deluxe|perfect edition|box\s?set|boxset|\d-in-\d|\din\d|hardcover|collector'?s?( edition)?|master edition|kanzenban|bunko|wideban|full colou?r|fullmetal edition)\b`)
)

// Words that say nothing about which series it is.
var noise = map[string]bool{
	"manga": true, "comic": true, "comics": true, "eng": true, "english": true, "en": true, "digital": true,
	"official": true, "scan": true, "scans": true, "scanlation": true, "cbz": true, "cbr": true, "pdf": true,
	"the": true, "complete": true, "completed": true, "series": true, "collection": true,
}

// removeMarks takes accents off letters: "Pokémon" compares as "Pokemon".
var removeMarks = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

func foldAccents(s string) string {
	out, _, err := transform.String(removeMarks, s)
	if err != nil {
		return s
	}
	return out
}

// Key is a name for comparing: lower case, accents off, letters and digits
// only, "×" read as "x", "&" as "and", without a leading "the". A name in
// another script keeps its letters.
func Key(name string) string {
	s := strings.ToLower(foldAccents(strings.TrimSpace(name)))
	s = strings.NewReplacer("×", "x", "✕", "x", "&", " and ", "'", "", "’", "").Replace(s)
	s = strings.TrimPrefix(s, "the ")
	return strings.Join(wordRe.FindAllString(s, -1), "")
}

// clean makes a name's separators spaces: "Kaiju.No.8.v01" and "Kaiju_No_8_v01"
// read the same as "Kaiju No. 8 v01", and a dot between digits stays ("v12.5").
func clean(s string) string {
	s = fileExt.ReplaceAllString(strings.TrimSpace(html.UnescapeString(s)), "") // "Yotsuba&amp;!"
	s = strings.ReplaceAll(s, "_", " ")
	if !strings.Contains(s, " ") {
		for dotInWord.MatchString(s) {
			s = dotInWord.ReplaceAllString(s, "$1 $2")
		}
	}
	return s
}

// afterName finds the longest known name at the start of s, comparing word by
// word against its Key, so "Spy x Family", "SpyxFamily" and "Spy × Family" are
// the same, and "Mob Psycho 100" or "Kaiju No. 8" keep their numbers. It
// returns the name and where the rest of s starts.
func afterName(s string, names []string) (string, int, bool) {
	bestName, bestEnd := "", -1
	folded := strings.NewReplacer("×", "x", "✕", "x", "&", " and ", "'", "", "’", "")
	for _, name := range names {
		key := Key(name)
		if key == "" {
			continue
		}
		// Words of s, folded one at a time so positions stay those of s.
		built, end, started := "", -1, false
		for _, loc := range anyWord.FindAllStringIndex(s, -1) {
			word := folded.Replace(strings.ToLower(foldAccents(s[loc[0]:loc[1]])))
			word = strings.Join(wordRe.FindAllString(word, -1), "")
			if word == "" {
				continue
			}
			if !started && word == "the" && !strings.HasPrefix(key, "the") {
				started = true
				continue
			}
			started = true
			next := built + word
			if !strings.HasPrefix(key, next) {
				break
			}
			built = next
			if built == key {
				end = loc[1]
				break
			}
		}
		if end > bestEnd {
			bestName, bestEnd = name, end
		}
	}
	return bestName, bestEnd, bestEnd >= 0
}

// publisherFirst is a publisher's name at the very start of a release, as the
// published editions on indexers are named: "VIZ.Media.Chainsaw.Man.Vol.01",
// "Kodansha-Vinland.Saga.Vol.09", "Dark.Horse.-.Mob.Psycho.100.Vol.11".
var publisherFirst = regexp.MustCompile(`(?i)^\s*(viz(?:\s+media)?|kodansha(?:\s+(?:comics|usa|manga))?|dark\s+horse(?:\s+(?:comics|manga))?|yen\s+press|seven\s+seas(?:\s+entertainment)?|vertical(?:\s+comics)?|square\s+enix(?:\s+manga)?|tokyopop|titan(?:\s+(?:manga|comics))?|one\s+peace(?:\s+books)?|denpa(?:\s+books)?|udon(?:\s+entertainment)?|ablaze|sublime|ghost\s+ship|airship|inklore|fantagraphics|digital\s+manga)(\s*[-–—:]\s*|\s+)`)

// startsWithNumbers is a name with nothing before its volume number.
var startsWithNumbers = regexp.MustCompile(`(?i)^\s*(?:` + volTag + `\s*)?\d`)

// afterPublisher is where a name starts behind a leading publisher (0 when it has
// none). A word that's also an everyday title word only counts as a publisher
// with a dash after it, when no series' name is there to settle it.
func afterPublisher(s string, known bool) int {
	m := publisherFirst.FindStringSubmatchIndex(s)
	if m == nil || startsWithNumbers.MatchString(s[m[1]:]) || strings.TrimSpace(s[m[1]:]) == "" {
		return 0
	}
	if !known && strings.TrimSpace(s[m[4]:m[5]]) == "" {
		switch strings.ToLower(s[m[2]:m[3]]) {
		case "titan", "vertical", "udon", "sublime", "ablaze", "airship", "inklore":
			return 0
		}
	}
	return m[1]
}

// anyWord is one word of a name, with what makes it up besides letters and
// digits ("×", "&", an apostrophe).
var anyWord = regexp.MustCompile(`[\p{L}\p{N}'’×✕&]+`)

// numOf normalises a number: "023" is "23", "12.50" is "12.5", "07.0" is "7".
func numOf(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	s = strings.TrimLeft(s, "0")
	if s == "" || strings.HasPrefix(s, ".") {
		s = "0" + s
	}
	return s
}

// NormalNumber is a volume number in one form ("007" and "7.0" are "7").
func NormalNumber(s string) string { return numOf(s) }

func val(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// SortKey is the number for putting volumes in order.
func SortKey(number string) float64 { return val(number) }

// ParseFor reads a name, looking for one of the series' names at its start
// before reading any numbers.
func ParseFor(title string, names []string) Name { return parse(title, names) }

// Parse reads a name without knowing the series: what comes before the numbers
// is the series.
func Parse(title string) Name { return parse(title, nil) }

func parse(title string, names []string) Name {
	s := clean(title)
	var n Name
	if m := editionWord.FindString(s); m != "" {
		n.Edition = strings.ToLower(m)
	}
	var tags []string
	for _, b := range bracketed.FindAllString(s, -1) {
		inner := strings.Trim(b, "()[]{}")
		if m := yearOnly.FindStringSubmatch(inner); m != nil && n.Year == 0 {
			n.Year, _ = strconv.Atoi(m[1])
			continue
		}
		tags = append(tags, inner)
	}
	main := bracketed.ReplaceAllString(s, " ")
	if len(strings.TrimSpace(main)) == 0 { // a name that's all brackets: "[Group] [Series]"
		main = strings.Join(tags, " ")
		tags = nil
	}
	// The series' own name wins, so a title beginning like a publisher stays whole.
	name, end, ok := afterName(main, names)
	if !ok {
		if at := afterPublisher(main, len(names) > 0); at > 0 {
			main = main[at:]
			name, end, ok = afterName(main, names)
		}
	}
	rest := main
	if ok {
		n.Series, n.Known = name, true
		rest = main[end:]
	}
	start := n.read(rest)
	if n.Kind == KindNone { // only in a bracket: "Series [v01-v05] [Group]", "Series [ch225-233]"
		for _, tag := range tags {
			var from Name
			from.Known = true // what's before it in a bracket isn't the series
			from.read(tag)
			if from.Kind == KindVolume || from.Kind == KindPack || from.Kind == KindChapter {
				n.Kind, n.Number, n.Lo, n.Hi, n.Chapter, n.ChapterTo = from.Kind, from.Number, from.Lo, from.Hi, from.Chapter, from.ChapterTo
				break
			}
		}
	}
	if start < 0 {
		start = len(rest)
	}
	before := rest[:start]
	if n.Known {
		n.Extra = extraWords(before)
	} else {
		n.Series = strings.Trim(strings.Join(strings.Fields(editionWord.ReplaceAllString(before, " ")), " "), " -–—:,.")
	}
	if n.Year == 0 {
		if m := bareYear.FindStringSubmatch(rest[start:]); m != nil {
			n.Year, _ = strconv.Atoi(m[1])
		}
	}
	if n.Kind == KindNone && wholeWord.MatchString(s) {
		n.Kind = KindWhole
	}
	// "Vagabond (Manga)": the series' name, the word manga, and no number.
	if n.Kind == KindNone && n.Known && n.Extra == "" && strings.TrimSpace(rest) == "" {
		for _, tag := range tags {
			if strings.EqualFold(strings.TrimSpace(tag), "manga") {
				n.Kind = KindWhole
			}
		}
	}
	return n
}

// read finds the numbers in the part of a name after the series, and returns
// where they start (-1 for none).
func (n *Name) read(rest string) int {
	n.Kind = KindNone
	// A chapter outside brackets decides, even with a volume beside it ("v03 c17").
	chap := chapRange.FindStringSubmatchIndex(rest)
	if m := volRange.FindStringSubmatchIndex(rest); m != nil && chap == nil {
		lo, hi := numOf(rest[m[2]:m[3]]), numOf(rest[m[4]:m[5]])
		if val(lo) < val(hi) {
			n.Kind, n.Lo, n.Hi = KindPack, val(lo), val(hi)
			return m[0]
		}
	}
	if m := oneVol.FindStringSubmatchIndex(rest); m != nil {
		num := numOf(rest[m[2]:m[3]])
		if chap == nil {
			n.Kind, n.Number = KindVolume, num
			return m[0]
		}
		n.Number = num
	}
	if chap != nil {
		n.Kind, n.Chapter = KindChapter, numOf(rest[chap[2]:chap[3]])
		if chap[4] >= 0 {
			n.ChapterTo = numOf(rest[chap[4]:chap[5]])
		}
		return chap[0]
	}
	if m := hashNum.FindStringSubmatchIndex(rest); m != nil {
		n.Kind, n.Number = KindBare, numOf(rest[m[2]:m[3]])
		return m[0]
	}
	if m := bareRange.FindStringSubmatchIndex(rest); m != nil {
		lo, hi := numOf(rest[m[2]:m[3]]), numOf(rest[m[4]:m[5]])
		if val(lo) < val(hi) && !yearLike(lo) && !yearLike(hi) {
			n.Kind, n.Lo, n.Hi = KindRange, val(lo), val(hi)
			return m[0]
		}
	}
	return n.readBare(rest)
}

// readBare takes a plain number: with a known series the first one after its
// name, otherwise the last, since a number before it may be in the title
// ("Some Title 100 005").
func (n *Name) readBare(rest string) int {
	offset, last := 0, -1
	var found string
	for _, f := range strings.Fields(rest) {
		at := strings.Index(rest[offset:], f) + offset
		offset = at + len(f)
		tok := strings.TrimRight(strings.TrimLeft(f, "#"), ",:;-")
		if plainNum.MatchString(tok) && !(len(tok) == 4 && yearLike(tok)) {
			found, last = tok, at
			if n.Known {
				break
			}
		}
	}
	if found != "" {
		n.Kind, n.Number = KindBare, numOf(found)
		return last
	}
	return -1
}

func yearLike(tok string) bool {
	v, err := strconv.Atoi(tok)
	return err == nil && v >= 1930 && v <= 2100
}

// extraWords is what stands between a series' name and its numbers, without the
// noise, the editions and the years.
func extraWords(s string) string {
	s = editionWord.ReplaceAllString(s, " ")
	var keep []string
	for _, w := range wordRe.FindAllString(s, -1) {
		l := strings.ToLower(w)
		if noise[l] || (len(w) == 4 && yearLike(w)) {
			continue
		}
		keep = append(keep, w)
	}
	return strings.Join(keep, " ")
}

// Holds reports whether a pack's range holds a volume number.
func (n Name) Holds(number string) bool {
	v := val(number)
	return (n.Kind == KindPack || n.Kind == KindRange) && v >= n.Lo && v <= n.Hi
}

// FileName is a volume's file name, without its extension: "Series v01", and
// "Series v12.5" for a half volume.
func FileName(series, number string) string {
	num := numOf(number)
	whole, frac, has := strings.Cut(num, ".")
	if len(whole) < 2 {
		whole = "0" + whole
	}
	if has {
		return series + " v" + whole + "." + frac
	}
	return series + " v" + whole
}

// IsPictureFile reports whether a file can hold a volume's pages. An EPUB may be a
// novel instead: look inside before filing one (books.EPUBPictures).
func IsPictureFile(name string) bool {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return false
	}
	switch strings.ToLower(name[i:]) {
	case ".cbz", ".cbr", ".cb7", ".cbt", ".pdf", ".epub":
		return true
	}
	return false
}

// FormatRank says how well the reader shows a file: pages as pictures first, then
// an EPUB of pictures, then a PDF, which the browser has to draw.
func FormatRank(name string) int {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return 0
	}
	switch strings.ToLower(name[i:]) {
	case ".cbz", ".cbr":
		return 3
	case ".epub":
		return 2
	case ".pdf":
		return 1
	}
	return 0
}
