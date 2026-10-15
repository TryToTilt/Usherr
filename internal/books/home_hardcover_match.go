package books

// Home fork: when Open Library doesn't know a book on disk (its catalogue is
// patchy: Margaret Weis's "Dragonwing" is missing, "The Nutmeg of
// Consolation" has no author, pen names are filed under the real name),
// the import asks Hardcover. A book found only there is kept with the key
// "HC<id>" in place of an Open Library work key, and its cover is
// Hardcover's.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HardcoverKeyPrefix marks a book that came from Hardcover, not Open Library.
const HardcoverKeyPrefix = "HC"

// IsHardcoverKey reports whether a book's key is a Hardcover one.
func IsHardcoverKey(key string) bool {
	return strings.HasPrefix(key, HardcoverKeyPrefix) && len(key) > len(HardcoverKeyPrefix)
}

// HardcoverMatch is a book found on Hardcover.
type HardcoverMatch struct {
	Found              // Key is "HC<id>"
	ImageURL    string // the cover, on assets.hardcover.app
	Description string
}

type hcHit struct {
	ID          json.Number `json:"id"`
	Title       string      `json:"title"`
	AuthorNames []string    `json:"author_names"`
	ReleaseYear json.Number `json:"release_year"`
	Description string      `json:"description"`
	Image       *struct {
		URL string `json:"url"`
	} `json:"image"`
}

// Hardcover allows 60 requests a minute: the import keeps well under it.
var (
	hcPaceMu sync.Mutex
	hcLast   time.Time
)

func hcPace(ctx context.Context) error {
	hcPaceMu.Lock()
	defer hcPaceMu.Unlock()
	if wait := 1100*time.Millisecond - time.Since(hcLast); wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	hcLast = time.Now()
	return nil
}

// searchHits runs Hardcover's book search.
func (h *Hardcover) searchHits(ctx context.Context, q string) ([]hcHit, error) {
	if err := hcPace(ctx); err != nil {
		return nil, err
	}
	var res struct {
		Search struct {
			Results json.RawMessage `json:"results"`
		} `json:"search"`
	}
	err := h.query(ctx, `query Search($q: String!) {
  search(query: $q, query_type: "Book", per_page: 15, page: 1) { results }
}`, map[string]any{"q": q}, &res)
	if err != nil {
		return nil, err
	}
	var results struct {
		Hits []struct {
			Document hcHit `json:"document"`
		} `json:"hits"`
	}
	raw := res.Search.Results
	// The results come as a JSON object, or as a string holding one.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&results); err != nil {
		return nil, fmt.Errorf("hardcover search: %w", err)
	}
	out := make([]hcHit, 0, len(results.Hits))
	for _, h := range results.Hits {
		out = append(out, h.Document)
	}
	return out, nil
}

// MatchLocal finds a book on disk on Hardcover, with the same care as the
// Open Library match: the same title (and author, when the folder names a
// person), collections and guides only on an exact title. It also tries
// the author and title the other way round, which some files' details have.
func (h *Hardcover) MatchLocal(ctx context.Context, lb LocalBook) (*HardcoverMatch, error) {
	if h == nil {
		return nil, ErrHardcoverToken
	}
	type attempt struct{ title, author string }
	var tries []attempt
	author := normText(strings.TrimSpace(lb.Author))
	for _, t := range titleVariants(lb.Title, author) {
		tries = append(tries, attempt{t, author})
	}
	if a, t := AuthorFromTitle(noteInBrackets.ReplaceAllString(normText(lb.Title), "")); a != "" {
		tries = append(tries, attempt{t, a})
	}
	// Swapped: "The Light Fantastic" as the author, "Terry Pratchett" as the title.
	if LooksSwapped(lb.Author, lb.Title) {
		tries = append([]attempt{{author, normText(lb.Title)}}, tries...)
	}
	seen := map[string]bool{}
	hits := map[string][]hcHit{}
	var lastErr error
	answered := false
	for i, tr := range tries {
		if i >= 6 || ctx.Err() != nil {
			break
		}
		q := strings.TrimSpace(tr.title + " " + tr.author)
		key := strings.ToLower(q)
		if !seen[key] {
			seen[key] = true
			res, err := h.searchHits(ctx, q)
			if err != nil {
				lastErr = err
				continue
			}
			answered = true
			hits[key] = res
		}
		cands := make([]candidate, 0, len(hits[key]))
		for _, hit := range hits[key] {
			cands = append(cands, hit.candidate())
		}
		need := ""
		if IsPersonName(tr.author) {
			need = tr.author
		}
		f := pick(tr.title, need, cands)
		if f == nil {
			f = pickCompact(tr.title, need, cands)
		}
		if f != nil {
			for _, hit := range hits[key] {
				if HardcoverKeyPrefix+hit.ID.String() == f.Key {
					m := &HardcoverMatch{Found: *f, Description: strings.TrimSpace(hit.Description)}
					if hit.Image != nil {
						m.ImageURL = hit.Image.URL
					}
					return m, nil
				}
			}
		}
	}
	if !answered && lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func (hit hcHit) candidate() candidate {
	c := candidate{Found: Found{Key: HardcoverKeyPrefix + hit.ID.String(), Title: hit.Title}}
	if len(hit.AuthorNames) > 0 {
		c.Author = hit.AuthorNames[0]
		c.authors = hit.AuthorNames
	}
	if y, err := strconv.Atoi(hit.ReleaseYear.String()); err == nil {
		c.Year = y
	}
	return c
}

// pickCompact matches a title that differs only in spacing and punctuation
// ("Dragonwing" for "Dragon Wing"), the author agreeing.
func pickCompact(title, author string, cands []candidate) *Found {
	want := compactTitle(title)
	if len(want) < 4 {
		return nil
	}
	for i := range cands {
		c := &cands[i]
		if compactTitle(c.Title) != want || LooksLikeSet(c.Title) {
			continue
		}
		if author != "" && !anyAuthor(author, *c) {
			continue
		}
		return &c.Found
	}
	return nil
}

func compactTitle(s string) string {
	var b strings.Builder
	for _, w := range words(normText(s)) {
		b.WriteString(w)
	}
	return b.String()
}

var nameWordsNot = map[string]bool{"the": true, "of": true, "and": true, "a": true, "an": true, "in": true, "to": true, "for": true, "from": true, "with": true, "on": true, "at": true, "by": true}

// LooksLikeName reports whether s reads like a person's name: two or three
// words, none of them "the", "of" and the like, no digits.
func LooksLikeName(s string) bool {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) < 2 || len(f) > 3 {
		return false
	}
	for _, w := range f {
		lw := strings.ToLower(strings.Trim(w, ".,"))
		if nameWordsNot[lw] || strings.ContainsAny(w, "0123456789") {
			return false
		}
	}
	return true
}

// LooksSwapped reports whether a file's author and title seem to be the
// wrong way round: the title reads like a name and the author doesn't.
func LooksSwapped(author, title string) bool {
	return strings.TrimSpace(author) != "" && LooksLikeName(title) && !LooksLikeName(author)
}
