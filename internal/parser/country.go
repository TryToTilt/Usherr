package parser

import (
	"strings"
	"unicode"
)

// CanonicalCountry turns a country code or tag as release names write it into
// the two-letter ISO code TMDB uses ("UK" and "GBR" are "GB", "USA" is "US").
// It returns "" for anything that is not a country tag we know, and for the
// empty string. Case does not matter.
func CanonicalCountry(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "US", "USA":
		return "US"
	case "UK", "GB", "GBR":
		return "GB"
	case "AU", "AUS":
		return "AU"
	case "NZ":
		return "NZ"
	case "CA":
		return "CA"
	case "DE":
		return "DE"
	case "FR":
		return "FR"
	case "IT":
		return "IT"
	case "ES":
		return "ES"
	case "IE":
		return "IE"
	case "SE":
		return "SE"
	case "NO":
		return "NO"
	case "DK":
		return "DK"
	case "NL":
		return "NL"
	case "JP":
		return "JP"
	}
	return ""
}

// SplitCountryTag splits a country tag off the end of a release or folder
// title: "Shameless US" and "Shameless (UK)" give ("Shameless", "US") and
// ("Shameless", "GB"). The code comes back in TMDB's form (see
// CanonicalCountry). A title without a tag, or one that is nothing but a tag,
// comes back unchanged with an empty code.
//
// A title that merely ends in a word that looks like a code ("Stephen King's
// It") is split too; callers must only use the split to compare with a title
// they already know, never to rewrite what the person sees.
func SplitCountryTag(title string) (base, country string) {
	rs := []rune(strings.TrimSpace(title))
	end := len(rs)
	for end > 0 && !isWordRune(rs[end-1]) {
		end--
	}
	start := end
	for start > 0 && isWordRune(rs[start-1]) {
		start--
	}
	if start == end || end-start > 3 {
		return title, ""
	}
	code := CanonicalCountry(string(rs[start:end]))
	if code == "" {
		return title, ""
	}
	rest := strings.TrimRightFunc(string(rs[:start]), func(r rune) bool { return !isWordRune(r) && r != '\'' && r != '!' && r != '?' && r != '&' })
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return title, ""
	}
	return rest, code
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
