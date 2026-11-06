package music

// Adapted from DroppedNeedle (AGPL-3.0), backend/services/native/file_processor.py
// (_title_conflicts) and backend/services/native/title_match.py
// (normalize_recording_title): a real title tag that clearly names another
// recording vetoes a match made by track number and length alone, so a live
// track or a bonus track whose length happens to fit doesn't take a studio
// track's place.

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// titleConflictRatio: at or below this token-set similarity (0-100), a title
// tag names a different track.
const titleConflictRatio = 50

var (
	reLeadingBracket = regexp.MustCompile(`^\s*[\[(][^\])]*[\])]\s*`)
	reFeaturing      = regexp.MustCompile(`(?i)\s*[\[(]?\s*\b(feat\.?|ft\.?|featuring)\s+[^\])]*[\])]?\s*$`)
)

// recordingWords folds two spellings of the same recording to the same words:
// case, accents, punctuation, a leading "[Explicit]" and a trailing
// "(feat. X)" don't count. A trailing "(Remix)" or "(Live)" stays, since it
// names another recording.
func recordingWords(s string) []string {
	s = strings.TrimSpace(s)
	for {
		t := reLeadingBracket.ReplaceAllString(s, "")
		if t == s {
			break
		}
		s = t
	}
	s = reFeaturing.ReplaceAllString(s, "")
	s = strings.NewReplacer("'", "", "’", "", "*", "").Replace(strings.ToLower(s))
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}

// titleTagConflicts reports whether a file's title tag clearly names another
// track than the one it would be matched to. An empty tag never conflicts.
func titleTagConflicts(tagTitle, trackTitle string) bool {
	a, b := recordingWords(tagTitle), recordingWords(trackTitle)
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return tokenSetRatio(a, b) <= titleConflictRatio
}

// tokenSetRatio is RapidFuzz's token_set_ratio on two word lists: 100 when
// one's words are all in the other, otherwise the best similarity of the
// shared words with each side's whole set.
func tokenSetRatio(a, b []string) int {
	sa, sb := wordSet(a), wordSet(b)
	var inter, onlyA, onlyB []string
	for w := range sa {
		if sb[w] {
			inter = append(inter, w)
		} else {
			onlyA = append(onlyA, w)
		}
	}
	for w := range sb {
		if !sa[w] {
			onlyB = append(onlyB, w)
		}
	}
	if len(inter) > 0 && (len(onlyA) == 0 || len(onlyB) == 0) {
		return 100
	}
	sort.Strings(inter)
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	sect := strings.Join(inter, " ")
	join := func(rest []string) string {
		r := strings.Join(rest, " ")
		if sect == "" {
			return r
		}
		return sect + " " + r
	}
	c1, c2 := join(onlyA), join(onlyB)
	best := indelRatio(c1, c2)
	if sect != "" {
		best = max(best, indelRatio(sect, c1), indelRatio(sect, c2))
	}
	return int(best + 0.5)
}

func wordSet(ws []string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}

// indelRatio is 100 x (1 - insertions and deletions / total length), the
// normalized similarity RapidFuzz's ratio uses.
func indelRatio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	total := len(ra) + len(rb)
	if total == 0 {
		return 100
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			if ra[i-1] == rb[j-1] {
				cur[j] = prev[j-1] + 1
			} else {
				cur[j] = max(prev[j], cur[j-1])
			}
		}
		prev, cur = cur, prev
	}
	lcs := prev[len(rb)]
	return 100 * float64(2*lcs) / float64(total)
}
