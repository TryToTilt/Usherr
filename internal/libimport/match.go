package libimport

import "github.com/rdborg/mediarium/internal/parser"

// MatchStatus says how much a metadata search result can be trusted for a
// group found on disk.
type MatchStatus string

const (
	// MatchMatched is an unambiguous title (+year) match, safe to pre-select.
	MatchMatched MatchStatus = "matched"
	// MatchAmbiguous means candidates exist but none is clearly the one, so
	// the user has to choose.
	MatchAmbiguous MatchStatus = "ambiguous"
	// MatchUnmatched means nothing came back.
	MatchUnmatched MatchStatus = "unmatched"
)

const maxCandidates = 5

// Candidate is one metadata search result.
type Candidate struct {
	TMDBID     int    `json:"tmdbId"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	PosterPath string `json:"-"`
	// OriginalTitle is the title in the film's own language ("La vita è
	// bella" for Life Is Beautiful); a folder named that way matches it too.
	OriginalTitle string `json:"-"`
	// Adult is TMDB's adult flag. Such a title is never put before one
	// without it.
	Adult bool `json:"-"`
	// Countries are the countries TMDB gives for a show ("US", "GB"); empty
	// when unknown. They tell "Shameless (UK)" from "Shameless (US)".
	Countries []string `json:"-"`
}

// Decide ranks cands for a group titled title (year 0 = unknown) and
// reports how confident the top choice is. The returned candidates are
// best-first, capped for display; for MatchMatched the first is the match.
//
// The order is: same title and year, same title with the year one off,
// another title with the same year, same title with another year, then the
// rest with the year one off before the others. A folder named in another
// language or script rarely matches the title TMDB gives, but its year still
// tells the right film. Titles flagged adult always come after the others.
func Decide(title string, year int, cands []Candidate) (MatchStatus, []Candidate) {
	var plain, adult []Candidate
	for _, c := range cands {
		if c.Adult {
			adult = append(adult, c)
		} else {
			plain = append(plain, c)
		}
	}
	status, ordered := rank(title, year, plain)
	if len(plain) == 0 {
		status, ordered = rank(title, year, adult)
	} else {
		_, rest := rank(title, year, adult)
		ordered = append(ordered, rest...)
	}
	if len(ordered) > maxCandidates {
		ordered = ordered[:maxCandidates]
	}
	return status, ordered
}

// rank orders cands for Decide and says how sure the first one is.
func rank(title string, year int, cands []Candidate) (MatchStatus, []Candidate) {
	want := normalize(title)
	// A folder called "Shameless (UK)" names the show made in that country.
	wantBase, tag := "", ""
	if base, code := parser.SplitCountryTag(title); code != "" {
		wantBase, tag = normalize(base), code
	}
	var exactYear, nearYear, exactTitle, restYear, restNear, rest []Candidate
	for _, c := range cands {
		same := want != "" && (normalize(c.Title) == want || (c.OriginalTitle != "" && normalize(c.OriginalTitle) == want))
		if !same && tag != "" && wantBase != "" && (normalize(c.Title) == wantBase || (c.OriginalTitle != "" && normalize(c.OriginalTitle) == wantBase)) {
			// The name fits; the country decides (a show with no country read counts).
			same = len(c.Countries) == 0 || inCountries(c.Countries, tag)
		}
		switch {
		case !same && year > 0 && c.Year == year:
			restYear = append(restYear, c)
		case !same && year > 0 && c.Year != 0 && abs(c.Year-year) <= 1:
			restNear = append(restNear, c)
		case !same:
			rest = append(rest, c)
		case year > 0 && c.Year == year:
			exactYear = append(exactYear, c)
		case year > 0 && c.Year != 0 && abs(c.Year-year) <= 1:
			nearYear = append(nearYear, c)
		default:
			exactTitle = append(exactTitle, c)
		}
	}
	ordered := make([]Candidate, 0, len(cands))
	for _, list := range [][]Candidate{exactYear, nearYear, restYear, exactTitle, restNear, rest} {
		ordered = append(ordered, list...)
	}

	switch {
	case len(cands) == 0:
		return MatchUnmatched, nil
	case len(exactYear) > 0:
		return MatchMatched, ordered
	case year == 0 && len(exactTitle) == 1:
		return MatchMatched, ordered
	case year > 0 && len(nearYear) == 1:
		return MatchMatched, ordered
	}
	return MatchAmbiguous, ordered
}

// inCountries reports whether code is one of the countries (both in TMDB's
// two-letter form).
func inCountries(countries []string, code string) bool {
	for _, c := range countries {
		if parser.CanonicalCountry(c) == code {
			return true
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
