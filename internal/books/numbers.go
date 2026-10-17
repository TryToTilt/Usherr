package books

import "strconv"

// Titles that are a number are often catalogued with it written out: a file
// called "1984.epub" is Nineteen Eighty-Four on Open Library, "Catch 22" may
// be "Catch-Twenty-Two". spelledNumbers writes the numbers in a list of words
// out in English the way such titles read them: a four-digit number as a
// year ("nineteen eighty four"), anything smaller as a plain number.

var (
	ones = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
		"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
	tensWords = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}
)

// underHundred spells 0 to 99.
func underHundred(n int) []string {
	if n < 20 {
		return []string{ones[n]}
	}
	if n%10 == 0 {
		return []string{tensWords[n/10]}
	}
	return []string{tensWords[n/10], ones[n%10]}
}

// spellNumber writes n (0 to 9999) out in words; nil for anything else.
func spellNumber(n int) []string {
	switch {
	case n < 0 || n > 9999:
		return nil
	case n < 100:
		return underHundred(n)
	case n < 1000:
		out := []string{ones[n/100], "hundred"}
		if n%100 != 0 {
			out = append(out, underHundred(n%100)...)
		}
		return out
	case n%1000 < 100 && (n/100)%10 == 0:
		// 2001, 2000: "two thousand one"
		out := []string{ones[n/1000], "thousand"}
		if n%1000 != 0 {
			out = append(out, underHundred(n%100)...)
		}
		return out
	}
	// Read like a year: 1984 is "nineteen eighty four", 1900 "nineteen hundred".
	out := underHundred(n / 100)
	if n%100 == 0 {
		return append(out, "hundred")
	}
	if n%100 < 10 {
		out = append(out, "oh")
	}
	return append(out, underHundred(n%100)...)
}

// spelledNumbers replaces each number in ws by its words. ok is false when
// there was none.
func spelledNumbers(ws []string) (out []string, ok bool) {
	for _, w := range ws {
		if isNumber(w) && len(w) <= 4 {
			if n, err := strconv.Atoi(w); err == nil {
				if sp := spellNumber(n); sp != nil {
					out = append(out, sp...)
					ok = true
					continue
				}
			}
		}
		out = append(out, w)
	}
	return out, ok
}
