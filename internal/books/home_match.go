package books

// Home fork: a better matcher for books already on disk. The upstream import
// searched Open Library once, free text, with the folder names as they are,
// and counted any Open Library hiccup as "not found". On a real library that
// left a third of the books unmatched: "Stormlight Archive 01 - The Way of
// Kings", "The Final Empire (1 of 3)", "Hogwarts (v5.0)", "ReDawn (Skyward
// Flight", co-written books ("The Gathering Storm" is listed under Robert
// Jordan), and franchise folders standing in for the author ("Dragonlance",
// "Anthology").
//
// MatchLocal tries cleaned-up title variants, Open Library's title/author
// search fields first, then free text, then the title alone; it compares
// against every author of a result, and retries when Open Library errors.

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	// "Stormlight Archive 01 - The Way of Kings", "Warriors 06 - Title"
	seriesNumPrefix = regexp.MustCompile(`^(.{2,60}?)\s+#?\d{1,3}(?:\.\d)?\s*[-–:]\s+(.+)$`)
	// "Title (1 of 3)", "(v5.0)", "(True AZW3)", "(rack)", "(d-1)", "(epub, mobi)"
	noteInBrackets = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)
	// "ReDawn (Skyward Flight" (a bracket never closed)
	openBracket = regexp.MustCompile(`\s*[\(\[][^\)\]]*$`)
	// "Shadows for Silence_1", "Gunner's Apprentice 1" (a second copy)
	copySuffix = regexp.MustCompile(`(?:_|\s)\d$`)
	// "Edgedancer - From the Stormlight Archive", "Title - A Novel"
	taglineSuffix = regexp.MustCompile(`(?i)\s+[-–:]\s+(from|a|an|book|volume|vol|part)\b.*$`)
	// "D&D - Dragonlance - Warriors 06" style chains: keep the last part
	dashParts = regexp.MustCompile(`\s+[-–]\s+`)
)

// notAPerson names folders that group books by franchise or kind, not author.
var notAPerson = map[string]bool{
	"anthology": true, "anthologies": true, "dragonlance": true, "dungeons & dragons": true, "d&d": true,
	"forgotten realms": true, "star wars": true, "various": true, "various authors": true, "dk": true,
	"unknown": true, "unknown author": true, "collections": true, "box sets": true,
}

// TitleVariants lists the ways a title on disk may be written in the
// catalogue, best first, without repeats.
func TitleVariants(title string) []string {
	return titleVariants(title, "")
}

// normText decodes HTML entities and straightens quotes, so "Lisey’s Story"
// and "Weis &amp; Hickman" compare like the catalogue's spelling.
func normText(s string) string {
	s = html.UnescapeString(s)
	return strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`, "–", "-", "—", "-").Replace(s)
}

// withoutAuthor drops the author's name from the end of a title ("Dragons
// of the Hourglass Mage Margaret Weis & Tracy Hickman", "Title by Author").
func withoutAuthor(title, author string) string {
	if author == "" {
		return title
	}
	first := strings.TrimSpace(strings.Split(strings.Split(author, ",")[0], "&")[0])
	tl := strings.ToLower(title)
	for _, name := range []string{author, first} {
		n := strings.ToLower(strings.TrimSpace(name))
		if n == "" {
			continue
		}
		if i := strings.LastIndex(tl, " by "+n); i > 0 {
			return strings.TrimSpace(title[:i])
		}
		if i := strings.LastIndex(tl, n); i > 0 && len(words(title[:i])) >= 1 {
			return strings.TrimRight(strings.TrimSpace(title[:i]), " -,")
		}
	}
	return title
}

func titleVariants(title, author string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(t string) {
		t = strings.TrimSpace(spaces.ReplaceAllString(t, " "))
		k := strings.ToLower(t)
		if len(words(t)) == 0 || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, t)
	}
	base := normText(strings.ReplaceAll(title, "_", " "))
	t := noteInBrackets.ReplaceAllString(base, "")
	t = openBracket.ReplaceAllString(t, "")
	t = copySuffix.ReplaceAllString(t, "")
	t = withoutAuthor(t, author)
	if m := seriesNumPrefix.FindStringSubmatch(t); m != nil {
		add(m[2])
	}
	add(t)
	add(taglineSuffix.ReplaceAllString(t, ""))
	if parts := dashParts.Split(t, -1); len(parts) > 1 {
		add(parts[0])            // "Title - Author" or "Title - tagline"
		add(parts[len(parts)-1]) // "Series - Title"
	}
	if strings.Contains(t, ",") { // "Beyond the Veil, Book 5 The Grey Wolves Series"
		add(strings.Split(t, ",")[0])
	}
	add(base) // the name as it is on disk, last: brackets in it mislead
	return out
}

// AuthorFromTitle reads "Title - Margaret Weis, Tracy Hickman": the people
// after the last dash, when there are any.
func AuthorFromTitle(title string) (string, string) {
	parts := dashParts.Split(title, -1)
	if len(parts) < 2 {
		return "", title
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	first := strings.Split(last, ",")[0]
	if w := words(first); len(w) >= 2 && len(w) <= 4 && !taglineStart[w[0]] {
		return strings.TrimSpace(first), strings.TrimSpace(strings.Join(parts[:len(parts)-1], " - "))
	}
	return "", title
}

// taglineStart are first words of a subtitle, never of a name.
var taglineStart = map[string]bool{"from": true, "a": true, "an": true, "the": true, "book": true, "volume": true, "vol": true, "part": true}

// IsPersonName reports whether an author folder could be a person.
func IsPersonName(a string) bool {
	return a != "" && !notAPerson[strings.ToLower(strings.TrimSpace(a))]
}

// candidate is one Open Library result with every author it lists.
type candidate struct {
	Found
	authors []string
}

func (c *Client) searchDocs(ctx context.Context, q url.Values) ([]candidate, error) {
	q.Set("fields", docFields)
	q.Set("limit", "15")
	var res struct {
		Docs []olDoc `json:"docs"`
	}
	if err := c.get(ctx, "/search.json", q, &res); err != nil {
		return nil, err
	}
	fs := founds(res.Docs, false)
	out := make([]candidate, 0, len(fs))
	for _, f := range fs {
		cand := candidate{Found: f}
		for _, d := range res.Docs {
			if strings.TrimPrefix(d.Key, "/works/") == f.Key {
				cand.authors = d.AuthorName
				break
			}
		}
		out = append(out, cand)
	}
	return out, nil
}

// retrying runs a search up to three times when Open Library errors.
func retrying(ctx context.Context, f func() ([]candidate, error)) ([]candidate, error) {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		var res []candidate
		if res, err = f(); err == nil {
			return res, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(2+attempt*3) * time.Second):
		}
	}
	return nil, err
}

func anyAuthor(author string, c candidate) bool {
	if SameAuthor(author, c.Author) {
		return true
	}
	for _, a := range c.authors {
		if SameAuthor(author, a) {
			return true
		}
	}
	return false
}

// pick returns the best result for title (and author, when set): the same
// words first, then every catalogue word on disk. Collections and study
// guides ("Summary of…", "… Box Set") only win on an exact title.
func pick(title, author string, cands []candidate) *Found {
	for _, exact := range []bool{true, false} {
		for i := range cands {
			c := &cands[i]
			if !SameTitle(title, normText(c.Title), exact) {
				continue
			}
			if author != "" && !anyAuthor(author, *c) {
				continue
			}
			if !exact && LooksLikeSet(c.Title) {
				continue
			}
			return &c.Found
		}
	}
	// The name on disk is the shorter one ("Unfinished Tales" for "Unfinished
	// Tales of Númenor and Middle-earth", "The Dragon Reborn" for "The Dragon
	// Reborn (The Wheel of Time, Book 3)"): every word on disk in the
	// catalogue title, the author agreeing, the fewest extra words winning.
	n := len(sigWords(title))
	if author == "" || n == 0 {
		return nil
	}
	maxExtra := 1 << 30
	if n == 1 { // "Dragonwing": a one-word title may only gain a word
		maxExtra = 1
	}
	var best *Found
	bestExtra := 1 << 30
	for i := range cands {
		c := &cands[i]
		if !anyAuthor(author, *c) || LooksLikeSet(c.Title) || looksLikeGuide(c.Title) || translated(c.Title) || otherLanguage(c.Title) {
			continue
		}
		if extra, ok := coveredBy(title, normText(c.Title)); ok && extra <= maxExtra && extra < bestExtra {
			best, bestExtra = &c.Found, extra
		}
	}
	return best
}

func sigWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range words(s) {
		if !small[w] && len(w) > 1 {
			out[w] = true
		}
	}
	return out
}

// coveredBy reports whether every significant word of local is in catalogue,
// and how many words the catalogue title has besides.
func coveredBy(local, catalogue string) (int, bool) {
	l, c := sigWords(local), sigWords(catalogue)
	if len(l) == 0 {
		return 0, false
	}
	for w := range l {
		if !c[w] {
			return 0, false
		}
	}
	return len(c) - len(l), true
}

var guideWords = regexp.MustCompile(`(?i)\b(summary|study guide|workbook|analysis|sparknotes|cliffsnotes|novel guide|companion to)\b`)

func looksLikeGuide(title string) bool { return guideWords.MatchString(title) }

// ErrLookup is Open Library failing (not answering, or erroring) after retries.
var ErrLookup = errors.New("open library didn't answer")

// MatchLocal finds a book on disk on Open Library, or returns nil when there
// is no good match. An error means Open Library couldn't be asked.
func (c *Client) MatchLocal(ctx context.Context, lb LocalBook) (*Found, error) {
	author := normText(strings.TrimSpace(lb.Author))
	titles := titleVariants(lb.Title, author)
	if a, t := AuthorFromTitle(noteInBrackets.ReplaceAllString(normText(lb.Title), "")); a != "" && (!IsPersonName(author) || author == "") {
		author = a
		titles = append(titleVariants(t, a), titles...)
	}
	person := IsPersonName(author)
	answered := false
	var lastErr error
	try := func(q url.Values, title, needAuthor string) *Found {
		res, err := retrying(ctx, func() ([]candidate, error) { return c.searchDocs(ctx, q) })
		if err != nil {
			lastErr = err
			return nil
		}
		answered = true
		return pick(title, needAuthor, res)
	}
	for _, t := range titles {
		if person {
			if f := try(url.Values{"title": {t}, "author": {author}}, t, author); f != nil {
				return f, nil
			}
			if f := try(url.Values{"q": {t + " " + author}}, t, author); f != nil {
				return f, nil
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	// The title alone: only an exact title, and the author (when it is a
	// person) among the book's authors, or no author to check.
	for _, t := range titles {
		need := ""
		if person {
			need = author
		}
		if f := try(url.Values{"title": {t}}, t, need); f != nil && SameTitle(t, f.Title, true) {
			return f, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	if !answered && lastErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrLookup, lastErr)
	}
	return nil, nil
}

var languages = map[string]bool{"german": true, "french": true, "spanish": true, "italian": true, "dutch": true, "portuguese": true,
	"polish": true, "swedish": true, "russian": true, "japanese": true, "chinese": true, "korean": true, "turkish": true}

// otherLanguage spots catalogue entries for a translation ("German from the
// Ashes", "From the Ashes (German Edition)").
func otherLanguage(title string) bool {
	w := words(title)
	if len(w) > 0 && languages[w[0]] {
		return true
	}
	for i := 0; i+1 < len(w); i++ {
		if languages[w[i]] && (w[i+1] == "edition" || w[i+1] == "translation") {
			return true
		}
	}
	return false
}
