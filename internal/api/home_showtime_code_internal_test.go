package api

import (
	"strings"
	"testing"
	"time"
)

// A code carries the film, the start to the minute and the pre-show; it reads
// back from a pasted message, with or without dashes, with O for 0; a typo is
// caught by the check character.
func TestShowtimeCode(t *testing.T) {
	at := time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC).UnixMilli()
	c := showtimeCode{TMDBID: 68726, AtMs: at, PreshowMin: 10}
	code := encodeShowtimeCode(c)
	if len(code) != 15 || strings.Count(code, "-") != 2 {
		t.Fatalf("code %q", code)
	}
	for _, pasted := range []string{
		code,
		strings.ReplaceAll(code, "-", ""),
		strings.ToLower(code),
		"Showtime code: `" + code + "`. Paste it in Usherr.",
	} {
		got, err := findShowtimeCode(pasted)
		if err != nil || got != c {
			t.Errorf("%q: %+v %v", pasted, got, err)
		}
	}
	if strings.Contains(code, "0") {
		if got, err := findShowtimeCode(strings.ReplaceAll(code, "0", "O")); err != nil || got != c {
			t.Errorf("O for 0: %v", err)
		}
	}
	// One character changed: refused, not another film.
	b := []byte(code)
	if b[2] == 'A' {
		b[2] = 'B'
	} else {
		b[2] = 'A'
	}
	if _, err := findShowtimeCode(string(b)); err == nil {
		t.Errorf("a mistyped code was accepted: %s", b)
	}
	if _, err := findShowtimeCode("hello there"); err == nil {
		t.Error("no code in it, but one was found")
	}
}

// A typed review's rating, however it's written, in half stars.
func TestParseReview(t *testing.T) {
	for in, want := range map[string]struct {
		stars int
		text  string
	}{
		"#review 4.5/5 Loved the robots": {9, "Loved the robots"},
		"#REVIEW 8/10 - solid":           {8, "solid"},
		"#review ★★★★½ Kaiju!":           {9, "Kaiju!"},
		"#review 3 stars, fun enough":    {6, "fun enough"},
		"#review just great, no number":  {0, "just great, no number"},
		"#review 4,5/5 comma":            {9, "comma"},
		"#review ||the ending|| 2/5":     {4, "||the ending||"},
	} {
		stars, text := parseReview(in)
		if stars != want.stars || text != want.text {
			t.Errorf("%q: %d %q, want %d %q", in, stars, text, want.stars, want.text)
		}
	}
	if starsText(9) != "★★★★½" || starsText(2) != "★" || starsText(0) != "" {
		t.Error(starsText(9), starsText(2))
	}
}
