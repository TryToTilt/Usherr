package indexers

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
	backtrackSyntax "github.com/dlclark/regexp2/syntax"
)

// Definitions are written for .NET's regular expressions. Most run on Go's RE2,
// which takes time in proportion to the text whatever the pattern. The rest
// (look-around, back-references, atomic groups and a few smaller things) run on
// regexp2, which backtracks: a badly or deliberately badly written pattern can
// take forever on a short text, and definitions can be added by hand. So on that
// engine every match has a time limit, the text is capped, it costs far more of
// a page's work budget, and a pattern that keeps running out of time rests.

const (
	// backtrackMatchTimeout is how long one match may take. regexp2's clock
	// ticks coarsely, so a real timeout can take about 200 ms longer.
	backtrackMatchTimeout = 250 * time.Millisecond
	// backtrackCallLimit is how long one replacement may take over all of its
	// matches: many matches of nearly the time limit each are just as slow.
	backtrackCallLimit = time.Second
	// maxBacktrackInput is the longest text the backtracking engine runs on.
	// Values are short; the odd definition rewrites a whole page.
	maxBacktrackInput = 1 << 20
	// backtrackWeight is the least a backtracking pattern pays per byte of text
	// from a page's work budget, where a pattern on RE2 usually pays a few.
	backtrackWeight = 32
	// regexTimeoutCost is what a timeout takes from a page's work budget, so a
	// page stops after a handful of them instead of one per row and field.
	regexTimeoutCost = pageWorkBudget / 8
	// maxRegexTimeouts is how many timeouts in a row send a pattern to rest for
	// regexTimeoutRest. Templates have no page budget, and this holds for them.
	maxRegexTimeouts = 8
	regexTimeoutRest = 10 * time.Minute
)

var (
	// errRegexTimedOut marks a pattern that took too long on some text.
	errRegexTimedOut = errors.New("the regular expression took too long")
	// errRegexInputTooLong marks text too long for the backtracking engine.
	errRegexInputTooLong = errors.New("the text is too long for this regular expression")
)

// dotNetRegex is a compiled definition pattern on whichever engine runs it.
// Exactly one of fast and slow is set.
type dotNetRegex struct {
	fast *regexp.Regexp
	slow *regexp2.Regexp

	timeouts    atomic.Int32 // in a row, on the slow engine
	restingTill atomic.Int64 // unix seconds
}

// buildBacktrackingRegex compiles a pattern RE2 can't run. translated is the
// pattern with its .NET-only escapes rewritten, which regexp2 reads too; it has
// no \p{IsCyrillic} of its own, for one.
func buildBacktrackingRegex(pattern, translated string) (c compiledRegex) {
	defer func() {
		if p := recover(); p != nil {
			c = compiledRegex{err: fmt.Errorf("regular expression %q is not supported: %v", pattern, p)}
		}
	}()
	steps := 0
	if tree, err := backtrackSyntax.Parse(translated, backtrackSyntax.RegexOptions(regexp2.None)); err == nil {
		if code, err := backtrackSyntax.Write(tree); err == nil {
			steps = len(code.Codes)
		}
	}
	if steps > maxRegexSteps {
		return compiledRegex{err: fmt.Errorf("regular expression %q: %w", shortenText(pattern, 60), errRegexTooBig)}
	}
	re, err := regexp2.Compile(translated, regexp2.None)
	if err != nil {
		return compiledRegex{err: fmt.Errorf("regular expression %q is not supported: %w", pattern, err)}
	}
	re.MatchTimeout = backtrackMatchTimeout
	return compiledRegex{re: &dotNetRegex{slow: re}, weight: backtrackWeight + int64(steps)/16}
}

// numbersGroupsUnlikeDotNet reports a pattern whose groups .NET numbers
// differently from RE2: unnamed groups come first there, whatever the order they
// are written in, and a name can be a number.
func numbersGroupsUnlikeDotNet(re *regexp.Regexp) bool {
	named := false
	for _, name := range re.SubexpNames()[1:] {
		switch {
		case name == "" && named:
			return true
		case name != "":
			if name[0] >= '0' && name[0] <= '9' {
				return true
			}
			named = true
		}
	}
	return false
}

// checkSlowRun says whether the slow engine may run on data at all.
func (r *dotNetRegex) checkSlowRun(data string) error {
	if len(data) > maxBacktrackInput {
		return fmt.Errorf("regular expression %q on %d bytes of text: %w", shortenText(r.slow.String(), 60), len(data), errRegexInputTooLong)
	}
	if r.timeouts.Load() >= maxRegexTimeouts {
		if time.Now().Unix() < r.restingTill.Load() {
			return fmt.Errorf("regular expression %q: %w", shortenText(r.slow.String(), 60), errRegexTimedOut)
		}
		r.timeouts.Store(0)
	}
	return nil
}

// timedOut records a timeout and gives the error for it.
func (r *dotNetRegex) timedOut() error {
	if r.timeouts.Add(1) >= maxRegexTimeouts {
		r.restingTill.Store(time.Now().Add(regexTimeoutRest).Unix())
	}
	return fmt.Errorf("regular expression %q: %w", shortenText(r.slow.String(), 60), errRegexTimedOut)
}

// firstGroup is the text of the first group of the first match: "" when nothing
// matches, or the pattern has no such group.
func (r *dotNetRegex) firstGroup(data string) (out string, err error) {
	if r.fast != nil {
		if m := r.fast.FindStringSubmatch(data); len(m) >= 2 {
			return m[1], nil
		}
		return "", nil
	}
	if err := r.checkSlowRun(data); err != nil {
		return "", err
	}
	defer recoverRegex(r, &err)
	m, err := r.slow.FindStringMatch(data)
	if err != nil {
		return "", r.timedOut()
	}
	r.timeouts.Store(0)
	if m == nil {
		return "", nil
	}
	if g := m.GroupByNumber(1); g != nil {
		return g.String(), nil
	}
	return "", nil
}

// replaceAll replaces every match in data with template, which is in Go's
// syntax (see dotNetReplacement), giving up if the result would pass limit.
//
// regexp2 has a replacement syntax of its own and a different idea of empty
// matches, so its matches are walked and expanded here, the way Go's
// ReplaceAllString does: an engine must never change what a replacement means.
func (r *dotNetRegex) replaceAll(data, template string, limit int) (out string, err error) {
	if r.fast != nil {
		return r.fast.ReplaceAllString(data, template), nil
	}
	if err := r.checkSlowRun(data); err != nil {
		return "", err
	}
	defer recoverRegex(r, &err)
	start := time.Now()
	runes := []rune(data)
	m, err := r.slow.FindRunesMatch(runes)
	var sb strings.Builder
	last, prevEnd := 0, -1
	for err == nil && m != nil {
		// As in Go, an empty match right after the last match doesn't count.
		if m.Length > 0 || m.Index != prevEnd {
			sb.WriteString(string(runes[last:m.Index]))
			expandReplacement(&sb, template, m)
			last = m.Index + m.Length
			prevEnd = last
		}
		// A group inside a look-ahead can hold text outside its match, so the
		// size can't be told in advance as it can on RE2.
		if sb.Len() > limit {
			return "", fmt.Errorf("the replacement made the text too large")
		}
		if time.Since(start) > backtrackCallLimit {
			return "", r.timedOut()
		}
		m, err = r.slow.FindNextMatch(m)
	}
	if err != nil {
		return "", r.timedOut()
	}
	r.timeouts.Store(0)
	if prevEnd == -1 {
		return data, nil // nothing matched: not even a stray byte may change
	}
	sb.WriteString(string(runes[last:]))
	return sb.String(), nil
}

// recoverRegex turns a panic inside regexp2 into a plain error: a strange
// pattern must cost a field its value, not the whole app.
func recoverRegex(r *dotNetRegex, err *error) {
	if p := recover(); p != nil {
		*err = fmt.Errorf("regular expression %q failed: %v", shortenText(r.slow.String(), 60), p)
	}
}

// expandReplacement writes template with its references filled in from m,
// following the rules of Go's Regexp.Expand: "$$" is a dollar sign, "$1", "$name"
// and "${name}" are groups (an unknown or unmatched one is empty), and any other
// "$" is itself.
func expandReplacement(sb *strings.Builder, template string, m *regexp2.Match) {
	for {
		before, after, ok := strings.Cut(template, "$")
		sb.WriteString(before)
		if !ok {
			return
		}
		template = after
		if strings.HasPrefix(template, "$") {
			sb.WriteByte('$')
			template = template[1:]
			continue
		}
		name, rest, ok := replacementRef(template)
		if !ok {
			sb.WriteByte('$')
			continue
		}
		template = rest
		var g *regexp2.Group
		if num, isNum := replacementGroupNumber(name); isNum {
			g = m.GroupByNumber(num)
		} else {
			g = m.GroupByName(name)
		}
		if g != nil {
			sb.WriteString(g.String())
		}
	}
}

// replacementRef reads the group reference at the start of s (what follows a
// "$"): the name in it and what comes after.
func replacementRef(s string) (name, rest string, ok bool) {
	brace := strings.HasPrefix(s, "{")
	if brace {
		s = s[1:]
	}
	i := 0
	for i < len(s) {
		c, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' {
			break
		}
		i += size
	}
	if i == 0 {
		return "", "", false
	}
	name, rest = s[:i], s[i:]
	if brace {
		if !strings.HasPrefix(rest, "}") {
			return "", "", false
		}
		rest = rest[1:]
	}
	return name, rest, true
}

// replacementGroupNumber reads name as a group number, Go's way: no leading
// zeros, and nothing absurdly large.
func replacementGroupNumber(name string) (int, bool) {
	if name[0] == '0' && len(name) > 1 {
		return 0, false
	}
	num := 0
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' || num >= 1e8 {
			return 0, false
		}
		num = num*10 + int(name[i]-'0')
	}
	return num, true
}
