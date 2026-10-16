package comics

// Home fork: judging a search result against the issue wanted, the way
// Pullbox's release validator does (Pullbox, GPL-3.0-or-later,
// https://github.com/pullboxapp/pullbox: ideas re-written here in Go). A
// release is either rejected, with a plain reason, or accepted with a score
// and a confidence: high and medium are grabbed by themselves; low is put in
// front of a person to decide (the review list) instead of guessed at.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Confidence in a match.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Verdict is what Evaluate makes of one release.
type Verdict struct {
	Reject     string   `json:"reject,omitempty"` // why it isn't this issue ("" when it may be)
	Score      int      `json:"score"`            // higher is better, among accepted releases
	Confidence string   `json:"confidence,omitempty"`
	Notes      []string `json:"notes,omitempty"` // what lowered the confidence
}

// Release is what Evaluate needs to know about a search result.
type Release struct {
	Title      string
	SizeBytes  int64
	Categories []int
}

// Usherr: a release in another language, by the tag in its name ("ger",
// "German", "Deutsch"...). ComicVine's series are the original editions.
var otherLanguage = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])(ger|german|deutsch|fre|french|fra|ita|italian|italiano|spa|esp|spanish|espanol|español|dut|dutch|ned|nl|por|portuguese|pt-?br)(?:$|[\s._\-\]\)])`)

var languageName = map[string]string{"ger": "German", "german": "German", "deutsch": "German", "fre": "French", "french": "French", "fra": "French",
	"ita": "Italian", "italian": "Italian", "italiano": "Italian", "spa": "Spanish", "esp": "Spanish", "spanish": "Spanish", "espanol": "Spanish", "español": "Spanish",
	"dut": "Dutch", "dutch": "Dutch", "ned": "Dutch", "nl": "Dutch", "por": "Portuguese", "portuguese": "Portuguese", "pt-br": "Portuguese", "ptbr": "Portuguese"}

var (
	notComic          = regexp.MustCompile(`(?i)\b(s\d{1,2}e\d{1,3}|480p|720p|1080p|2160p|x26[45]|hevc|web-?dl|bluray|mp3|flac|m4b|audiobook|epub|mobi|azw3)\b`)
	digital           = regexp.MustCompile(`(?i)\bdigital\b`)
	annual            = regexp.MustCompile(`(?i)\bannual\b`)
	fcbd              = regexp.MustCompile(`(?i)\b(free comic book day|fcbd)\b`)
	directors         = regexp.MustCompile(`(?i)\bdirector'?s cut\b`)
	extraWords        = regexp.MustCompile(`(?i)\b(annual|free comic book day|fcbd|director'?s cut)\b`)
	westernCollection = regexp.MustCompile(`(?i)\b(tpb|trade paperback|omnibus|hc|hardcover|deluxe|compendium|absolute edition|epic collection|masterworks)\b`) // home fork: "absolute" alone starts series names
	packLeavesOut     = regexp.MustCompile(`(?i)\b(?:no|without|missing|minus|except|sans)\s*#?0*(\d{1,4})\b`)
	packRange         = regexp.MustCompile(`(?i)(?:^|[\s.#_(-])0*(\d{1,4})\s*(?:-|–|to)\s*0*(\d{1,4})(?:[\s.)_\]]|$)`)
)

const (
	smallComic = 2 << 20   // under 2 MB: too small for a whole issue
	largeComic = 300 << 20 // over 300 MB: more than one issue, or not a comic
)

// Evaluate judges a release against issue i of series s.
func Evaluate(r Release, s Series, i Issue) Verdict {
	n := ParseName(r.Title)
	reject := func(format string, a ...any) Verdict { return Verdict{Reject: fmt.Sprintf(format, a...)} }

	if notComic.MatchString(r.Title) {
		return reject("Not a comic (a video, music or ebook release)")
	}
	if len(r.Categories) > 0 {
		book := false
		for _, c := range r.Categories {
			book = book || (c >= 7000 && c < 8000) || c >= 100000 // books, or an indexer's own category
		}
		if !book {
			return reject("Listed outside the indexer's book and comic categories")
		}
	}
	if IsReprint(r.Title) {
		return reject("A reprint or facsimile edition")
	}
	if m := otherLanguage.FindStringSubmatch(r.Title); m != nil && !otherLanguage.MatchString(s.Name) {
		return reject("In another language (%s)", languageName[strings.ToLower(m[1])])
	}
	want := NormalNumber(i.Number)
	if alt := AltNumber(i); alt != "" {
		want = alt
	}
	// Packs, collected editions and annuals aren't the issue itself, but they
	// may be what's wanted: they go to the review list (low confidence), for
	// a person to decide, and are never grabbed by themselves.
	doubtful := func(score int, note string) Verdict {
		return Verdict{Score: score, Confidence: ConfidenceLow, Notes: []string{note}}
	}
	sameSeries := Key(n.Series) == Key(s.Name) || Key(extraWords.ReplaceAllString(n.Series, " ")) == Key(s.Name)
	if loc := packRange.FindStringSubmatchIndex(r.Title); loc != nil && r.Title[loc[2]:loc[3]] != r.Title[loc[4]:loc[5]] &&
		!yearLike(r.Title[loc[2]:loc[3]]) && !yearLike(r.Title[loc[4]:loc[5]]) {
		m := []string{"", r.Title[loc[2]:loc[3]], r.Title[loc[4]:loc[5]]}
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		w, err := strconv.Atoi(want)
		// The series is what comes before the numbers ("Daredevil 001-016").
		packSeries := strings.NewReplacer(".", " ", "_", " ").Replace(r.Title[:loc[0]])
		left := false // "Saga 25-33 no 29": the pack leaves #29 out
		for _, x := range packLeavesOut.FindAllStringSubmatch(r.Title[loc[1]:], -1) {
			if v, e := strconv.Atoi(x[1]); e == nil && v == w {
				left = true
			}
		}
		if left {
			return reject("A pack of issues %d to %d that leaves this one out", lo, hi)
		}
		if Key(packSeries) != Key(s.Name) || err != nil || w < lo || w > hi {
			return reject("A pack of issues %s to %s, without this one", m[1], m[2])
		}
		return doubtful(40, fmt.Sprintf("A pack of issues %d to %d, this one among them", lo, hi))
	}
	if n.Collection {
		if !sameSeries {
			return reject("A collected edition of another series")
		}
		// "Vagabond Volume 023": manga counts its volumes as the issues, so
		// only the volume with this number can be it. A western collection
		// ("Daredevil Vol. 3 TPB") numbers its volumes apart from the issues.
		if n.Volume > 0 && IsManga(s) && !westernCollection.MatchString(r.Title) {
			if strconv.Itoa(n.Volume) != want {
				return reject("Volume %d, not this one", n.Volume)
			}
			return doubtful(60, fmt.Sprintf("Volume %d: the issue itself for manga", n.Volume))
		}
		return doubtful(30, "A collected edition (TPB, omnibus or similar): check it holds this issue")
	}
	if n.Number == "" {
		return reject("No issue number in the name")
	}
	for _, x := range []struct {
		re   *regexp.Regexp
		what string
	}{{annual, "An annual"}, {fcbd, "A Free Comic Book Day issue"}, {directors, "A director's cut"}} {
		if x.re.MatchString(r.Title) && !x.re.MatchString(s.Name) && !x.re.MatchString(i.Title) {
			if !sameSeries || NormalNumber(n.Number) != want {
				return reject("%s, not this issue", x.what)
			}
			return doubtful(30, x.what+" with this number: a different book from the regular issue")
		}
	}
	if Key(n.Series) != Key(s.Name) {
		return reject("Another series (%q)", n.Series)
	}
	if NormalNumber(n.Number) != want {
		return reject("Issue %s, not %s", n.Number, strings.TrimSuffix(want, ".now")+map[bool]string{true: ".NOW"}[strings.HasSuffix(want, ".now")])
	}

	v := Verdict{Score: 100, Confidence: ConfidenceHigh}
	lower := func(to, note string) {
		if to == ConfidenceLow || v.Confidence == ConfidenceHigh {
			v.Confidence = to
		}
		v.Notes = append(v.Notes, note)
	}
	switch off := YearDistance(n, i); {
	case n.Year == 0:
		v.Score -= 5
		lower(ConfidenceMedium, "No year in the name")
	case n.Year == s.StartYear || off == 0:
	case off == 1:
		v.Score -= 5 // cover dates run ahead of store dates
	default:
		v.Score -= 10 * off
		lower(ConfidenceLow, fmt.Sprintf("Year %d, the issue is from %d", n.Year, issueYear(i)))
	}
	if digital.MatchString(r.Title) {
		v.Score += 10
	}
	switch {
	case r.SizeBytes > 0 && r.SizeBytes < smallComic:
		lower(ConfidenceLow, "Very small for a whole issue")
	case r.SizeBytes > largeComic:
		lower(ConfidenceLow, "Very large for one issue")
	}
	return v
}

// NamedIssue is the issue number a release's name says it is, when it says:
// "Daredevil 022" is 22, and a manga "Vagabond Volume 023" is 23 (manga counts
// volumes as issues). "" when the name doesn't say (a western TPB, a pack).
func NamedIssue(title string, manga bool) string {
	n := ParseName(title)
	if packRange.MatchString(title) {
		return ""
	}
	if n.Number != "" {
		return NormalNumber(n.Number)
	}
	if manga && n.Collection && n.Volume > 0 && !westernCollection.MatchString(title) {
		return strconv.Itoa(n.Volume)
	}
	return ""
}

var mangaPublisher = regexp.MustCompile(`(?i)\b(viz|kodansha|shueisha|shogakukan|yen press|seven seas|square enix|kadokawa|hakusensha|akita shoten|tokyopop|vertical|del rey manga|dark horse manga|j-novel|futabasha|ichijinsha|houbunsha|mag garden|enterbrain|media factory|ascii media works|tokuma|takeshobo|nihon bungeisha|coamix|shonen gahosha|kodansha usa|denpa|one peace|udon|digital manga)\b`)

// IsManga reports whether a series is manga, by its publisher: manga counts
// its volumes as the issues ("Vagabond Volume 023" is #23); a western
// collection ("Saga Vol. 6") holds several issues instead.
func IsManga(s Series) bool { return mangaPublisher.MatchString(s.Publisher) }
