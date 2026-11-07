package indexers

import (
	"bytes"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// bothEngines compiles a pattern both engines can run, once on each.
func bothEngines(t *testing.T, pattern string) (fast, slow *dotNetRegex) {
	t.Helper()
	f := buildDotNetRegex(pattern)
	if f.err != nil || f.re.fast == nil {
		t.Fatalf("%q: not on RE2: %v", pattern, f.err)
	}
	s := buildBacktrackingRegex(pattern, translateDotNetRegex(pattern))
	if s.err != nil || s.re.slow == nil {
		t.Fatalf("%q: not on the backtracking engine: %v", pattern, s.err)
	}
	return f.re, s.re
}

// What a pattern and a replacement mean doesn't depend on the engine.
func TestBothEnginesAgree(t *testing.T) {
	for _, tt := range []struct{ pattern, in, rep, want string }{
		{`(\w+) (\w+)`, "the matrix", "$2-$1", "matrix-the"},
		{`(\w+)`, "ab", "$1x", "abx"},
		{`(\w+)`, "ab", "${1}x", "abx"},
		{`b`, "abc", "[$0]", "a[b]c"},
		{`b`, "abc", "$$", "a$c"},
		{`b`, "abc", "$", "a$c"},
		{`b`, "abc", "$ x", "a$ xc"},
		{`b`, "abc", "${", "a${c"},
		{`b`, "abc", "$5", "ac"},
		{`b`, "abc", "$nosuch", "ac"},
		{`(?<y>\d+)-(?<m>\d+)`, "on 2026-11 ok", "${m}/${y}", "on 11/2026 ok"},
		{`(?<y>\d+)-(?<m>\d+)`, "on 2026-11 ok", "$2/$1", "on 11/2026 ok"},
		{`(?<y>\d+)`, "a 12 b", "<$y>", "a <12> b"},
		{`(a)?b`, "b ab", "[$1]", "[] [a]"},
		{`(a)|(b)`, "ab", "<$1|$2>", "<a|><|b>"},
		{`a*`, "baaac", "-", "-b-c-"},
		{`x*`, "héllo", "-", "-h-é-l-l-o-"},
		{`é(.)`, "aéb né", "[$1]", "a[b] né"},
		{`z`, "no match", "y", "no match"},
		{`^`, "abc", ">", ">abc"},
		{`$`, "abc", "<", "abc<"},
		{`(?i)MOVIE`, "a movie", "x", "a x"},
		{`\.`, "a.b.c", "", "abc"},
	} {
		fast, slow := bothEngines(t, tt.pattern)
		for name, re := range map[string]*dotNetRegex{"RE2": fast, "backtracking": slow} {
			got, err := regexReplace(re, tt.in, tt.rep)
			if err != nil {
				t.Errorf("%s: %q on %q: %v", name, tt.pattern, tt.in, err)
			} else if got != tt.want {
				t.Errorf("%s: %q on %q with %q: got %q, want %q", name, tt.pattern, tt.in, tt.rep, got, tt.want)
			}
		}
	}
	for _, tt := range []struct{ pattern, in, want string }{
		{`(\d+)`, "a 12 b", "12"},
		{`\d+`, "a 12 b", ""},
		{`(x)?y`, "y", ""},
		{`(x)`, "y", ""},
		{`(?<n>\d)`, "a1", "1"},
		{`é(.)`, "aéb", "b"},
	} {
		fast, slow := bothEngines(t, tt.pattern)
		for name, re := range map[string]*dotNetRegex{"RE2": fast, "backtracking": slow} {
			if got, err := re.firstGroup(tt.in); err != nil || got != tt.want {
				t.Errorf("%s: first group of %q on %q: %q, %v, want %q", name, tt.pattern, tt.in, got, err, tt.want)
			}
		}
	}
}

// RE2 runs what it can; only what it lacks goes to the other engine.
func TestEngineChoice(t *testing.T) {
	for pattern, onRE2 := range map[string]bool{
		`\bS(20\d{2})\b`:          true,
		`(?<y>\d+)-(?<m>\d+)`:     true,
		`(\d+)-(?<m>\d+)`:         true,
		`(?i)(?<![a-z])(Movie)`:   false,
		`Movie(?!s)`:              false,
		`(?=a)a`:                  false,
		`(?<=a)b`:                 false,
		`(?>a+)b`:                 false,
		`(a)\1`:                   false,
		`(?<n>a)\k<n>`:            false,
		`(?<m>\d+)-(\d+)`:         false, // .NET numbers (\d+) first
		`[\p{IsCyrillic}]+(?=\s)`: false,
		` (?=x)`:                  false,
		`(?x) a # a comment`:      false,
	} {
		c := buildDotNetRegex(pattern)
		if c.err != nil {
			t.Errorf("%q: %v", pattern, c.err)
		} else if (c.re.fast != nil) != onRE2 {
			t.Errorf("%q: on RE2 = %v, want %v", pattern, c.re.fast != nil, onRE2)
		}
	}
	// .NET's numbering, unnamed groups first
	re, err := compileDotNetRegex(`(?<a>\d)-(\d)`)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := re.firstGroup("1-2"); err != nil || got != "2" {
		t.Errorf("group 1 = %q, %v", got, err)
	}
	// Too big for RE2 is refused, never handed to the engine that backtracks.
	for _, pattern := range []string{`a{2000}`, `(?:a{1000}){1000}`, strings.Repeat("(", 1100) + strings.Repeat(")", 1100)} {
		if c := buildDotNetRegex(pattern); c.err == nil {
			t.Errorf("%q: accepted", shortenText(pattern, 40))
		}
	}
	// A length limit holds on both.
	if _, err := compileDotNetRegex(`(?=x)` + strings.Repeat("a", maxRegexLength)); !errors.Is(err, errRegexTooBig) {
		t.Errorf("a long look-ahead: %v", err)
	}
}

// A pattern that would backtrack for ever on a short text costs a field its
// value, quickly, whichever way it is run.
func TestCatastrophicPatternsStop(t *testing.T) {
	text := strings.Repeat("a", 64) + "!"
	for _, pattern := range []string{
		`(a+)+(?=b)`,
		`(a|aa)+\1b`,
		`(?<=x?)(a*)*(?=b)`,
		`^(([a-z])+.)+[A-Z]([a-z])+(?!x)$`,
		`(?=(a+)+b)`,
	} {
		c := buildDotNetRegex(pattern)
		if c.err != nil || c.re.slow == nil {
			t.Fatalf("%q: %v", pattern, c.err)
		}
		start := time.Now()
		if _, err := c.re.firstGroup(text); !errors.Is(err, errRegexTimedOut) {
			t.Errorf("%q first group: %v", pattern, err)
		}
		if _, err := regexReplace(c.re, text, "$1"); !errors.Is(err, errRegexTimedOut) {
			t.Errorf("%q replace: %v", pattern, err)
		}
		if d := time.Since(start); d > 4*time.Second {
			t.Errorf("%q took %v", pattern, d)
		}
	}

	// a row's field gets a plain error...
	budget := newWorkBudget()
	timedOut := []FilterBlock{{Name: "regexp", Args: []any{`(a+)+(?=c)`}}}
	if _, err := applyFiltersWith(text, timedOut, nil, budget); !errors.Is(err, errRegexTimedOut) {
		t.Fatalf("field: %v", err)
	}
	// ...and a page of them stops soon, for a pattern or for many
	start := time.Now()
	for i := 0; ; i++ {
		_, err := applyFiltersWith(text, []FilterBlock{{Name: "re_replace", Args: []any{`(a+)+(?=` + strings.Repeat("d", i+1) + `)`, "x"}}}, nil, budget)
		if errors.Is(err, errPageTooComplex) {
			break
		}
		if i > 8 {
			t.Fatalf("the page wasn't stopped: %v", err)
		}
	}
	if d := time.Since(start); d > 8*time.Second {
		t.Errorf("the page took %v", d)
	}

	// templates have no page budget: there the pattern itself goes to rest
	const pattern = `(a+)+(?=e)`
	start = time.Now()
	for range maxRegexTimeouts {
		if _, err := tmplReReplace(text, pattern, "x"); !errors.Is(err, errRegexTimedOut) {
			t.Fatalf("template: %v", err)
		}
	}
	spent := time.Since(start)
	start = time.Now()
	for range 1000 {
		if _, err := tmplReReplace(text, pattern, "x"); !errors.Is(err, errRegexTimedOut) {
			t.Fatalf("template at rest: %v", err)
		}
	}
	if d := time.Since(start); d > spent/2 {
		t.Errorf("a pattern at rest still ran: %v, against %v for the first %d", d, spent, maxRegexTimeouts)
	}

	// a rest ends, and a good run forgets the timeouts
	c := buildDotNetRegex(`(?=a)(a+)+(?=b|!)`)
	if c.err != nil {
		t.Fatal(c.err)
	}
	c.re.timeouts.Store(maxRegexTimeouts)
	c.re.restingTill.Store(time.Now().Add(-time.Second).Unix())
	if _, err := c.re.firstGroup("aa!"); err != nil {
		t.Errorf("after a rest: %v", err)
	}
	if n := c.re.timeouts.Load(); n != 0 {
		t.Errorf("%d timeouts remembered", n)
	}
}

func TestBacktrackingCosts(t *testing.T) {
	if c := buildDotNetRegex(`(?=a)a`); c.err != nil || c.weight < backtrackWeight {
		t.Fatalf("weight %d: %v", c.weight, c.err)
	}
	if c := buildDotNetRegex(`a`); c.err != nil || c.weight >= backtrackWeight {
		t.Fatalf("weight on RE2 %d: %v", c.weight, c.err)
	}
	// text an RE2 pattern may run over is refused by this one at a smaller size
	budget := &workBudget{left: 10000}
	text := strings.Repeat("a", 1000)
	if _, err := applyFiltersWith(text, []FilterBlock{{Name: "regexp", Args: []any{`(a+)`}}}, nil, budget); err != nil {
		t.Fatalf("on RE2: %v", err)
	}
	budget = &workBudget{left: 10000}
	if _, err := applyFiltersWith(text, []FilterBlock{{Name: "regexp", Args: []any{`(?=a)(a+)`}}}, nil, budget); !errors.Is(err, errPageTooComplex) {
		t.Fatalf("backtracking: %v", err)
	}

	// text past the cap: a plain error for regexp, left alone by re_replace
	long := strings.Repeat("a", maxBacktrackInput+1)
	if _, err := applyFilters(long, []FilterBlock{{Name: "regexp", Args: []any{`(?=a)(a)`}}}, nil); !errors.Is(err, errRegexInputTooLong) {
		t.Errorf("regexp on a long text: %v", err)
	}
	if got, err := applyFilters(long, []FilterBlock{{Name: "re_replace", Args: []any{`(?=a)a`, "b"}}}, nil); err != nil || got != long {
		t.Errorf("re_replace on a long text: %d bytes, %v", len(got), err)
	}
	if got, err := applyFilters(long, []FilterBlock{{Name: "re_replace", Args: []any{`a`, "b"}}}, nil); err != nil || got == long {
		t.Errorf("RE2 has no such cap: %v", err)
	}

	// A group in a look-ahead holds text beyond its match, so a replacement can
	// grow far past what the usual check allows for: it stops at the limit.
	re, err := compileDotNetRegex(`(?=(.*))`)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := regexReplaceMax(re, strings.Repeat("a", 50000), "$1$1", 1<<20); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("a growing replacement: %v", err)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("took %v", d)
	}
}

// fixtureNyaaResults reads the saved results page with def and these settings,
// as a search would, without asking any site.
func fixtureNyaaResults(t *testing.T, def []byte, settings map[string]string) map[string]Result {
	t.Helper()
	d, err := ParseDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	if p := d.Problems(); len(p) != 0 {
		t.Fatalf("definition refused: %v", p)
	}
	s, err := newSession(&CardigannManager{}, d, Instance{Name: "nyaa", Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join("testdata", "cardigann", "fixturenyaa.html"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(d.Links[0])
	if err != nil {
		t.Fatal(err)
	}
	vars := s.vars()
	vars.Query["Keywords"] = "vagabond"
	results, rows, rowErr, err := s.parseHTMLRows(doc, vars, base, "vagabond")
	if err != nil {
		t.Fatal(err)
	}
	if rowErr != nil {
		t.Fatalf("a row failed: %v", rowErr)
	}
	if rows != 6 || len(results) != 6 {
		t.Fatalf("%d rows, %d results", rows, len(results))
	}
	out := map[string]Result{}
	for _, r := range results {
		out[r.Title] = r
	}
	return out
}

// Nyaa's title fields, which brought this on, on a saved page of results.
func TestNyaaFieldsOnSavedPage(t *testing.T) {
	def := fixtureDef(t, "fixturenyaa")
	check := func(got map[string]Result, want ...string) {
		t.Helper()
		for _, title := range want {
			if _, ok := got[title]; !ok {
				t.Errorf("no result titled %q in %v", title, keysOf(got))
			}
		}
	}
	check(fixtureNyaaResults(t, def, nil),
		"[Group] Vagabond v01 (Digital)",
		"[Subs] Some Anime Movie 2 [1080p]",
		"[Subs] Removie Time [1080p]",
		"[Subs] Some Show Season 2 [1080p]",
		"Hotel Del Luna 720p HDTV",
		"Hotel.del.Luna.2019.1080p.WEB",
	)
	check(fixtureNyaaResults(t, def, map[string]string{"sonarr_compatibility": "true"}),
		"[Group] Vagabond v01 (Digital)",    // a book: left alone
		"[Subs] Some Anime Movie 2 [1080p]", // whole-word Movie: no season
		"[Subs] Removie Time S01 [1080p]",   // Movie inside a word doesn't count
		"[Subs] Some Show S2 [1080p]",
		"Hotel Del Luna S01 720p HDTV",
		"Hotel.del.Luna.2019.S01.1080p.WEB",
	)
}

func keysOf(m map[string]Result) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
