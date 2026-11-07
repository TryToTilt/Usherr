package manga

// Telling an official English edition from a fan scan, and judging a search
// result against a series. "Official" describes the translation and edition,
// by the release's name. It doesn't say the copy on an indexer is licensed.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rdborg/mediarium/internal/releaselang"
)

// Class is where a release's translation comes from, going by its name.
type Class string

const (
	ClassOfficial Class = "official"
	ClassFan      Class = "fan"
	ClassUnknown  Class = "unknown"
	ClassOwn      Class = "own" // a file the person already had
)

// Confidence in a match, as in Comics: high and medium are grabbed by
// themselves, low goes on the list for a person to look at.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Why a release was turned down, for the "Why nothing yet?" sentence.
const (
	RejectFormat   = "format"   // a video, a novel, an EPUB
	RejectCategory = "category" // outside the indexer's book categories
	RejectLanguage = "language" // Verdict.Language says which
	RejectSeries   = "series"   // another series
	RejectVolume   = "volume"   // another volume
	RejectChapter  = "chapter"  // chapters; this series follows volumes
	RejectFan      = "fan"      // a fan scan, and those are off
	RejectOfficial = "official" // official, and those are off
	RejectNyaa     = "nyaa"     // from Nyaa, and that's off
	RejectDead     = "dead"     // a torrent nobody is sharing
	RejectBlocked  = "blocked"  // on the blocklist
	RejectTorrent  = "torrent"  // a torrent, and torrents are off
)

var (
	fanMark = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(scanlations?|scanlated|scans?|fan[\s._-]?(?:scan|scans|trans|translation|translated|made)|fanmade|mtl)(?:$|[^\p{L}\p{N}])`)
	// Groups known for fan translations. A start: it needs tuning against what
	// the household's indexers really carry.
	fanGroup = regexp.MustCompile(`(?i)[\[\(\s\-.](strays|pirateboy|silverrg)(?:[\]\)\s.]|$)`)
	// Words that come with a release of the published edition, as seen on real
	// indexers: a publisher, and "Retail" or "HYBRID.MANGA" or "Hybrid.Comic".
	officialMark = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(viz(?:[\s._]media)?|kodansha(?:[\s._](?:comics|usa))?|yen[\s._]press|seven[\s._]seas|dark[\s._]horse|vertical(?:[\s._]comics)?|square[\s._]enix|tokyopop|titan[\s._]manga|one[\s._]peace|udon|denpa|ablaze|j-novel|retail|hybrid[\s._-]?(?:manga|comic))(?:$|[^\p{L}\p{N}])`)
	// Groups that release the published English editions. "-Empire" is the tag of
	// the digital-retail releases: danke-Empire, XRA-Empire.
	officialGroup = regexp.MustCompile(`(?i)[\[\(\s\-.](1r0n|lucaz|[a-z0-9]+-empire|oak|smokeofficial|bitbook|pnls|21a1)(?:[\]\)\s.]|$)`)
	digitalMark   = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])digital(?:[\s-]?hd)?(?:$|[^\p{L}\p{N}])`)

	notPictures = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])(s\d{1,2}e\d{1,3}|480p|720p|1080p|2160p|x26[45]|hevc|web-?dl|blu-?ray|mp3|flac|m4b|audiobook|anime|mobi|azw3|light novel|novel|ln)(?:$|[^\p{L}\p{N}])`)
	// An EPUB can hold a volume of pictures or a novel, and only its insides say.
	epubMark = regexp.MustCompile(`(?i)(?:^|[^\p{L}\p{N}])epub(?:$|[^\p{L}\p{N}])`)
)

// chapterRunLeast is how many chapters make a run: a whole series' worth, not a
// release of the latest few.
const chapterRunLeast = 20

// Classify says whether a release's name is that of an official edition or a
// fan scan. licensed is what's known of the series (1 yes, else unknown), and
// only tips a bare "(Digital)" over: without it, that name proves nothing. An
// unknown one goes in front of a person, whichever sources are on.
func Classify(title string, kind Kind, licensed int) Class {
	switch {
	case fanMark.MatchString(title), fanGroup.MatchString(title), kind == KindChapter:
		return ClassFan
	case kind != KindVolume && kind != KindPack && kind != KindWhole:
		return ClassUnknown
	case officialMark.MatchString(title), officialGroup.MatchString(title):
		return ClassOfficial
	case digitalMark.MatchString(title) && licensed == 1:
		return ClassOfficial
	}
	return ClassUnknown
}

// BetterCopy says whether one copy of a volume beats another: official over
// unknown over fan scan, by its name; then the bigger file, which is the fuller or
// sharper one. Exactly the same size is the same file, so the first one stays.
func BetterCopy(name string, size int64, otherName string, otherSize int64, licensed int) bool {
	rank := func(n string) int {
		switch Classify(n, KindVolume, licensed) {
		case ClassOfficial:
			return 2
		case ClassFan:
			return 0
		}
		return 1
	}
	if a, b := rank(name), rank(otherName); a != b {
		return a > b
	}
	return size > otherSize
}

// Sources says which kinds of release a series may take.
type Sources struct {
	Official bool
	Fan      bool
	Nyaa     bool
	MangaDex bool
}

// Any reports whether anything can come at all.
func (s Sources) Any() bool { return s.Official || s.Fan }

// Target is the series a release is judged against.
type Target struct {
	Names     []string // the title first, then the other titles
	StartYear int
	Licensed  int
	Sources   Sources
	// Notes are what an earlier download of a release showed, by its name: it's
	// not blocklisted for that, but it isn't taken without asking again.
	Notes map[string]string
	// FollowsChapters: this series follows chapters, so a run of them may be of use.
	FollowsChapters bool
}

// Release is what's judged of a search result.
type Release struct {
	Title      string
	SizeBytes  int64
	Categories []int
	Indexer    string
}

// Verdict is what Evaluate makes of a release.
type Verdict struct {
	Reject     string   `json:"reject,omitempty"`     // a plain reason ("" when it may be it)
	Why        string   `json:"why,omitempty"`        // Reject* code
	Language   string   `json:"language,omitempty"`   // with RejectLanguage
	Score      int      `json:"score"`                // higher is better, among accepted ones
	Confidence string   `json:"confidence,omitempty"` //
	Notes      []string `json:"notes,omitempty"`      // what lowered the confidence
	Class      Class    `json:"class,omitempty"`      //
	Kind       Kind     `json:"kind,omitempty"`       //
	Number     string   `json:"number,omitempty"`     // the volume it is, when one
	Lo         float64  `json:"lo,omitempty"`         // a pack's range
	Hi         float64  `json:"hi,omitempty"`
}

const (
	smallVolume = 5 << 20    // under 5 MB: too small for a whole volume
	largeVolume = 1536 << 20 // over 1.5 GB: more than one volume, or not one
)

// Evaluate judges a release against a series. wanted is the volume looked for;
// "" takes any volume, for a search by the series' name.
func Evaluate(r Release, s Target, wanted string) Verdict {
	reject := func(why, format string, a ...any) Verdict {
		return Verdict{Why: why, Reject: fmt.Sprintf(format, a...)}
	}
	if notPictures.MatchString(strings.NewReplacer(".", " ", "_", " ").Replace(r.Title)) {
		return reject(RejectFormat, "Not a volume of pictures (a video, a novel or an ebook)")
	}
	if len(r.Categories) > 0 {
		book := false
		for _, c := range r.Categories {
			book = book || (c >= 7000 && c < 8000) || c >= 100000 // books, or an indexer's own category
		}
		if !book {
			return reject(RejectCategory, "Listed outside the indexer's book and comic categories")
		}
	}
	name := ParseFor(r.Title, s.Names)
	epub := epubMark.MatchString(strings.NewReplacer(".", " ", "_", " ").Replace(r.Title))
	if epub && (!name.Known || name.Kind == KindNone || name.Kind == KindBare) {
		return reject(RejectFormat, "An ebook, with nothing to say it's a volume of this series")
	}
	if !name.Known {
		return reject(RejectSeries, "Another series")
	}
	if lang := releaselang.OtherVolume(afterSeries(r.Title, s.Names), strings.Join(s.Names, " ")); lang != "" {
		v := reject(RejectLanguage, "In another language (%s)", lang)
		v.Language = lang
		return v
	}
	if name.Kind == KindChapter {
		// A whole run of chapters is a fan-scan pack: it follows that choice first.
		if val(name.ChapterTo)-val(name.Chapter) >= chapterRunLeast {
			if !s.Sources.Fan {
				return reject(RejectFan, "A run of fan-scan chapters (%s to %s), and fan scans are off", name.Chapter, name.ChapterTo)
			}
			if s.FollowsChapters && wanted == "" {
				// Never by itself: a person looks first, as it could be one huge file.
				return Verdict{Kind: KindChapter, Class: ClassFan, Score: 20, Confidence: ConfidenceLow, Notes: []string{fmt.Sprintf("A run of chapters %s to %s. Usherr files what it can, if each chapter is its own file", name.Chapter, name.ChapterTo)}}
			}
			return reject(RejectChapter, "A run of chapters (%s to %s). This series follows volumes. Follow its chapters to have this offered", name.Chapter, name.ChapterTo)
		}
		return reject(RejectChapter, "A chapter; this series follows volumes")
	}
	class := Classify(r.Title, name.Kind, s.Licensed)
	if strings.Contains(strings.ToLower(r.Indexer), "nyaa") && !s.Sources.Nyaa {
		return reject(RejectNyaa, "From Nyaa, and Nyaa is off")
	}
	switch {
	case class == ClassFan && !s.Sources.Fan:
		return reject(RejectFan, "A fan scan, and fan scans are off")
	case class == ClassOfficial && !s.Sources.Official:
		return reject(RejectOfficial, "An official edition, and those are off")
	}
	v := Verdict{Class: class, Kind: name.Kind, Number: name.Number, Lo: name.Lo, Hi: name.Hi}
	low := func(score int, note string) Verdict {
		v.Score, v.Confidence = score, ConfidenceLow
		v.Notes = append(v.Notes, note)
		return v
	}
	if name.Extra != "" && name.Kind != KindPack && name.Kind != KindRange && name.Kind != KindWhole {
		return reject(RejectSeries, "Another series (%s %s)", name.Series, name.Extra)
	}
	switch name.Kind {
	case KindNone:
		return reject(RejectVolume, "No volume number in the name")
	case KindPack, KindRange:
		if wanted != "" && !name.Holds(wanted) {
			return reject(RejectVolume, "Volumes %g to %g, without this one", name.Lo, name.Hi)
		}
		if name.Kind == KindRange {
			return low(25, fmt.Sprintf("Numbers %g to %g, with no word for what they count: they may be chapters", name.Lo, name.Hi))
		}
		if name.Extra != "" {
			return low(30, fmt.Sprintf("Volumes %g to %g, with more words after the name: %s", name.Lo, name.Hi, name.Extra))
		}
		return low(40, fmt.Sprintf("A pack of volumes %g to %g", name.Lo, name.Hi))
	case KindWhole:
		if name.Extra != "" {
			return low(20, "A whole series pack, with more words after the name: "+name.Extra)
		}
		return low(35, "A whole series pack: check what it holds")
	case KindBare:
		if wanted != "" && name.Number != numOf(wanted) {
			return reject(RejectVolume, "Number %s, not this one", name.Number)
		}
		return low(30, fmt.Sprintf("Number %s with no word for what it counts: it may be a chapter", name.Number))
	}
	if wanted != "" && name.Number != numOf(wanted) {
		return reject(RejectVolume, "Volume %s, not this one", name.Number)
	}
	switch class {
	case ClassOfficial:
		v.Score, v.Confidence = 100, ConfidenceHigh
	case ClassFan:
		v.Score, v.Confidence = 70, ConfidenceMedium
	default:
		v = low(50, "The name doesn't say whether this is an official edition or a fan scan")
	}
	if digitalMark.MatchString(r.Title) {
		v.Score += 10
	}
	lower := func(note string) {
		v.Confidence = ConfidenceLow
		v.Notes = append(v.Notes, note)
	}
	if name.Edition != "" {
		v.Score -= 30
		lower(fmt.Sprintf("A special edition (%s): its numbers may not match the regular volumes", name.Edition))
	}
	if name.Year > 0 && s.StartYear > 0 && name.Year < s.StartYear-1 {
		lower(fmt.Sprintf("Year %d, and the series started in %d", name.Year, s.StartYear))
	}
	switch {
	case r.SizeBytes > 0 && r.SizeBytes < smallVolume:
		lower("Very small for a whole volume")
	case r.SizeBytes > largeVolume:
		lower("Very large for one volume")
	}
	if epub {
		lower("An EPUB: it may hold the pages as pictures, or be a novel. Usherr looks inside when it arrives")
	}
	if note := s.Notes[r.Title]; note != "" {
		lower("Last time: " + note)
	}
	return v
}

// Confident reports whether a verdict can be grabbed without asking.
func (v Verdict) Confident() bool {
	return v.Reject == "" && v.Kind == KindVolume && (v.Confidence == ConfidenceHigh || v.Confidence == ConfidenceMedium)
}

// afterSeries is a release's name without the series' name, so words in the
// title itself can't read as a language.
func afterSeries(title string, names []string) string {
	s := clean(title)
	if _, end, ok := afterName(s, names); ok {
		return s[end:]
	}
	if at := afterPublisher(s, true); at > 0 {
		if _, end, ok := afterName(s[at:], names); ok {
			return s[at+end:]
		}
	}
	return s
}

var searchWords = regexp.MustCompile(`[^\p{L}\p{N}' ]+`)

// SearchName is a series' name as search words: "Spy × Family" is "Spy x Family".
func SearchName(name string) string {
	s := strings.NewReplacer("×", "x", "✕", "x", "&", " and ").Replace(name)
	return strings.Join(strings.Fields(searchWords.ReplaceAllString(foldAccents(s), " ")), " ")
}

// LatinOnly reports whether a name can be typed into an indexer's search: most
// releases are named in Latin letters, and other scripts waste a search.
func LatinOnly(name string) bool {
	seen := false
	for _, r := range name {
		if r > 0x24F && r != '×' && r != '✕' && r != '’' && r != '–' && r != '—' {
			return false
		}
		seen = true
	}
	return seen
}

// VolumeQueries are the searches for one volume, most likely first: "Vol 23",
// "v023", "v23", "Volume 23" under the series' title, then fewer under other
// titles, which are only tried once the first have found nothing. thorough
// also asks by the bare number, for a search a person started.
func VolumeQueries(names []string, number string, thorough bool) []string {
	num := numOf(number)
	padded3, padded2 := num, num
	if whole, frac, has := strings.Cut(num, "."); true {
		p3, p2 := whole, whole
		for len(p3) < 3 {
			p3 = "0" + p3
		}
		if len(p2) < 2 {
			p2 = "0" + p2
		}
		if has {
			p3, p2 = p3+"."+frac, p2+"."+frac
		}
		padded3, padded2 = p3, p2
	}
	var out []string
	add := func(q string) {
		for _, x := range out {
			if x == q {
				return
			}
		}
		out = append(out, q)
	}
	for i, n := range SearchableNames(names, 2) {
		name := SearchName(n)
		if i == 0 {
			// Published editions are mostly named "Name.Vol.01", the rest "Name v001".
			add(name + " Vol " + padded2)
			add(name + " v" + padded3)
			add(name + " v" + padded2)
			add(name + " Vol " + num)
			add(name + " Volume " + num)
			if thorough {
				add(name + " " + padded3)
			}
			continue
		}
		add(name + " Vol " + padded2)
		add(name + " v" + padded2)
	}
	return out
}

// SearchableNames are the names worth an indexer's time: the title and at most
// n other titles, in Latin letters, without repeats.
func SearchableNames(names []string, others int) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		k := Key(n)
		if k == "" || seen[k] || !LatinOnly(n) || len(k) < 2 {
			continue
		}
		seen[k] = true
		out = append(out, n)
		if len(out) > others {
			break
		}
	}
	return out
}
