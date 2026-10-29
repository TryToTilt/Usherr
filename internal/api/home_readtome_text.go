package api

// Usherr: Read-to-Me's text, made ready to be read aloud. What a voice reads
// badly is said the way a narrator would: titles and abbreviations in full
// ("Dr." is "Doctor"), numbers in words ("1999" is "nineteen ninety-nine",
// "3rd" is "third"), dashes and ellipses as pauses, note markers dropped. The
// household's pronunciation list comes first: a word, and how to say it, for
// one book or for every book. Scene breaks (*** or a rule) are kept as a mark
// the splitter turns into a longer pause.

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// sceneMark stands for a scene break in a chapter's text: a paragraph of its own.
const sceneMark = "⁂"

var sceneLine = regexp.MustCompile(`^[\s*#~•·◇◆⁂○●❦❧✦✧§—–_=-]+$`)

// isSceneBreak: a paragraph that is only a scene break (***, * * *, ⁂, a rule).
func isSceneBreak(p string) bool {
	p = strings.TrimSpace(p)
	return p != "" && len([]rune(p)) <= 24 && sceneLine.MatchString(p)
}

// ---- the pronunciation list ----

type sayRule struct {
	Word string `json:"word"`
	Say  string `json:"say"`
}

// sayRules is the list for a book: its own words, then the ones for every book.
func (s *Server) sayRules(ctx context.Context, bookID int64) []sayRule {
	rows, err := s.db.QueryContext(ctx, `SELECT word, say FROM readtome_say WHERE book_id = ? OR book_id = 0 ORDER BY book_id DESC, length(word) DESC`, bookID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []sayRule
	seen := map[string]bool{}
	for rows.Next() {
		var r sayRule
		if rows.Scan(&r.Word, &r.Say) == nil && !seen[strings.ToLower(r.Word)] {
			seen[strings.ToLower(r.Word)] = true
			out = append(out, r)
		}
	}
	return out
}

func (s *Server) sayList(ctx context.Context, bookID int64) []sayRule {
	out := []sayRule{}
	rows, err := s.db.QueryContext(ctx, `SELECT word, say FROM readtome_say WHERE book_id = ? ORDER BY word COLLATE NOCASE`, bookID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r sayRule
		if rows.Scan(&r.Word, &r.Say) == nil {
			out = append(out, r)
		}
	}
	return out
}

// GET /api/home/readtome/say[?bookId=]: the words for every book, and the book's own.
func (s *Server) handleReadToMeSay(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("bookId"), 10, 64)
	out := map[string]any{"all": s.sayList(r.Context(), 0)}
	if id > 0 {
		out["book"] = s.sayList(r.Context(), id)
	}
	writeJSON(w, http.StatusOK, out)
}

const maxSayRules = 500

// PUT /api/home/readtome/say {bookId (0: every book), words: [{word, say}]}:
// the list is replaced by the one sent.
func (s *Server) handlePutReadToMeSay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookID int64     `json:"bookId"`
		Words  []sayRule `json:"words"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.BookID < 0 || len(req.Words) > maxSayRules {
		writeError(w, http.StatusBadRequest, "That list is too long.")
		return
	}
	if req.BookID > 0 {
		if _, err := s.BookRepo.Get(req.BookID); err != nil {
			writeError(w, http.StatusNotFound, "That book isn't in the library.")
			return
		}
	}
	clean := map[string]sayRule{}
	for _, rule := range req.Words {
		rule.Word, rule.Say = strings.Join(strings.Fields(rule.Word), " "), strings.Join(strings.Fields(rule.Say), " ")
		if rule.Word == "" || rule.Say == "" {
			continue
		}
		if len([]rune(rule.Word)) > 60 || len([]rune(rule.Say)) > 120 {
			writeError(w, http.StatusBadRequest, "A word is up to 60 letters, and how to say it up to 120.")
			return
		}
		clean[strings.ToLower(rule.Word)] = rule
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM readtome_say WHERE book_id = ?`, req.BookID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, rule := range clean {
		if _, err := tx.Exec(`INSERT INTO readtome_say (book_id, word, say) VALUES (?, ?, ?)`, req.BookID, rule.Word, rule.Say); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"words": s.sayList(r.Context(), req.BookID)})
}

// applySay puts each listed word (whole words, any case) as it should be said.
func applySay(text string, rules []sayRule) string {
	for _, r := range rules {
		re, err := regexp.Compile(`(?i)(^|[^\p{L}\p{N}])` + regexp.QuoteMeta(r.Word) + `($|[^\p{L}\p{N}])`)
		if err != nil {
			continue
		}
		say := strings.ReplaceAll(r.Say, "$", "$$")
		// Twice: a word right after another match shares the character between them.
		text = re.ReplaceAllString(text, "${1}"+say+"${2}")
		text = re.ReplaceAllString(text, "${1}"+say+"${2}")
	}
	return text
}

// ---- reading aloud ----

type textFix struct {
	re  *regexp.Regexp
	say string
}

var abbrevFixes = []textFix{
	{regexp.MustCompile(`\bMrs\.`), "Missus"},
	{regexp.MustCompile(`\bMr\.`), "Mister"},
	{regexp.MustCompile(`\bMs\.`), "Miz"},
	{regexp.MustCompile(`\bDr\.`), "Doctor"},
	{regexp.MustCompile(`\bProf\.`), "Professor"},
	{regexp.MustCompile(`\bCapt\.`), "Captain"},
	{regexp.MustCompile(`\bLt\.`), "Lieutenant"},
	{regexp.MustCompile(`\bSgt\.`), "Sergeant"},
	{regexp.MustCompile(`\bGen\.`), "General"},
	{regexp.MustCompile(`\bCol\.`), "Colonel"},
	{regexp.MustCompile(`\bRev\.`), "Reverend"},
	{regexp.MustCompile(`\bMt\.`), "Mount"},
	{regexp.MustCompile(`\bJr\.(\s+[a-z,])`), "Junior$1"},
	{regexp.MustCompile(`\bSr\.(\s+[a-z,])`), "Senior$1"},
	{regexp.MustCompile(`\bSt\.(\s+[A-Z])`), "Saint$1"},
	{regexp.MustCompile(`\bSt\.(\s+[a-z,])`), "Street$1"},
	{regexp.MustCompile(`\bvs?\.(\s)`), "versus$1"},
	{regexp.MustCompile(`\betc\.(\s+[a-z,])`), "et cetera$1"},
	{regexp.MustCompile(`\betc\.`), "et cetera."},
	{regexp.MustCompile(`\b[Ee]\.g\.,?`), "for example,"},
	{regexp.MustCompile(`\b[Ii]\.e\.,?`), "that is,"},
	{regexp.MustCompile(`\s&\s`), " and "},
}

var (
	noteMarks   = regexp.MustCompile(`\[\d{1,3}\]|\*+|†|‡`)
	dashCut     = regexp.MustCompile(`(—|--)\s*(["”’])`)
	numRange    = regexp.MustCompile(`(\d)\s*[–—]\s*(\d)`)
	dashes      = regexp.MustCompile(`\s*(—|--|–)\s*`)
	dots        = regexp.MustCompile(`\.\s?\.\s?\.|…`)
	clockTime   = regexp.MustCompile(`\b(\d{1,2}):(\d{2})\b`)
	numberRe    = regexp.MustCompile(`(\$?)(\d[\d,]*)(st|nd|rd|th|s)?(%?)`)
	commaSpaces = regexp.MustCompile(`\s+([,.;:!?])`)
	doubleComma = regexp.MustCompile(`,(\s*,)+`)
	commaStop   = regexp.MustCompile(`,\s*([.!?…])`)
	spaces      = regexp.MustCompile(`[ \t]+`)
)

// speakable is text as it should be read aloud; paragraph breaks are kept.
func speakable(text string, rules []sayRule) string {
	paras := strings.Split(text, "\n\n")
	for i, p := range paras {
		if isSceneBreak(p) {
			paras[i] = sceneMark
		}
	}
	text = applySay(strings.Join(paras, "\n\n"), rules)
	text = noteMarks.ReplaceAllString(text, "")
	for _, f := range abbrevFixes {
		text = f.re.ReplaceAllString(text, f.say)
	}
	text = numRange.ReplaceAllString(text, "$1 to $2")
	text = dashCut.ReplaceAllString(text, "…$2")
	text = dashes.ReplaceAllString(text, ", ")
	text = dots.ReplaceAllString(text, "…")
	text = clockTime.ReplaceAllStringFunc(text, func(m string) string {
		p := clockTime.FindStringSubmatch(m)
		h, _ := strconv.Atoi(p[1])
		min, _ := strconv.Atoi(p[2])
		if h > 24 || min > 59 {
			return m
		}
		switch {
		case min == 0:
			return numWords(h) + " o'clock"
		case min < 10:
			return numWords(h) + " oh " + numWords(min)
		}
		return numWords(h) + " " + numWords(min)
	})
	text = sayNumbers(text)
	text = commaSpaces.ReplaceAllString(text, "$1")
	text = doubleComma.ReplaceAllString(text, ",")
	text = commaStop.ReplaceAllString(text, "$1")
	// tidy the spaces without joining paragraphs
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaces.ReplaceAllString(l, " "))
		lines[i] = strings.TrimLeft(lines[i], ", ")
	}
	return strings.Join(lines, "\n")
}

var groupedNumber = regexp.MustCompile(`^\d{1,3}(,\d{3})+$`)

// sayNumbers puts whole numbers in words: years as years, ordinals, money,
// percentages. Decimals, codes ("B12", "3D") and numbers of a million or more
// are left for the voice.
func sayNumbers(text string) string {
	var b strings.Builder
	last := 0
	for _, m := range numberRe.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[0], m[1]
		dollar := m[3] > m[2]
		digits := text[m[4]:m[5]]
		suffix := ""
		if m[6] >= 0 {
			suffix = text[m[6]:m[7]]
		}
		percent := m[9] > m[8]
		// A comma at the end belongs to the sentence.
		trail := ""
		for strings.HasSuffix(digits, ",") {
			digits = digits[:len(digits)-1]
			trail += ","
		}
		before, after := rune(0), rune(0)
		if start > 0 {
			before = rtLastRune(text[:start])
		}
		if end < len(text) {
			after = firstRune(text[end:])
		}
		grouped := strings.Contains(digits, ",")
		switch {
		case unicode.IsLetter(before) || unicode.IsDigit(before) || before == '.' || before == ',' && !dollar,
			unicode.IsLetter(after) || unicode.IsDigit(after),
			(after == '.' || after == ',') && end+1 < len(text) && unicode.IsDigit(firstRune(text[end+1:])) && trail == "",
			grouped && !groupedNumber.MatchString(digits):
			continue
		}
		n, err := strconv.Atoi(strings.ReplaceAll(digits, ",", ""))
		if err != nil || n >= 1_000_000 {
			continue
		}
		var say string
		switch {
		case suffix == "s" && !grouped && n >= 1100 && n <= 2099 && n%10 == 0:
			say = pluralWords(yearWords(n)) // the 1990s
		case suffix == "s":
			say = pluralWords(numWords(n))
		case suffix != "":
			say = ordinal(numWords(n))
		case !dollar && !percent && !grouped && len(digits) == 4 && n >= 1100 && n <= 2099:
			say = yearWords(n)
		default:
			say = numWords(n)
		}
		if dollar {
			if n == 1 {
				say += " dollar"
			} else {
				say += " dollars"
			}
		}
		if percent {
			say += " percent"
		}
		b.WriteString(text[last:start])
		b.WriteString(say + trail)
		last = end
	}
	b.WriteString(text[last:])
	return b.String()
}

func rtLastRune(s string) rune {
	r := []rune(s)
	return r[len(r)-1]
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

var (
	smallWords = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
		"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
	tensWords = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}
)

// numWords says n (under a million) in words: 342 is "three hundred forty-two".
func numWords(n int) string {
	switch {
	case n < 20:
		return smallWords[n]
	case n < 100:
		if n%10 == 0 {
			return tensWords[n/10]
		}
		return tensWords[n/10] + "-" + smallWords[n%10]
	case n < 1000:
		if n%100 == 0 {
			return smallWords[n/100] + " hundred"
		}
		return smallWords[n/100] + " hundred " + numWords(n%100)
	}
	if n%1000 == 0 {
		return numWords(n/1000) + " thousand"
	}
	return numWords(n/1000) + " thousand " + numWords(n%1000)
}

// yearWords says a year: 1999 "nineteen ninety-nine", 1905 "nineteen oh five",
// 1900 "nineteen hundred", 2005 "two thousand five", 2024 "twenty twenty-four".
func yearWords(n int) string {
	hi, lo := n/100, n%100
	switch {
	case n >= 2000 && n < 2010:
		return numWords(n)
	case lo == 0:
		return numWords(hi) + " hundred"
	case lo < 10:
		return numWords(hi) + " oh " + numWords(lo)
	}
	return numWords(hi) + " " + numWords(lo)
}

var ordinalEnds = map[string]string{"one": "first", "two": "second", "three": "third", "five": "fifth", "eight": "eighth", "nine": "ninth", "twelve": "twelfth"}

// ordinal turns the words for a number into its ordinal: "twenty-one" "twenty-first".
func ordinal(words string) string {
	cut := strings.LastIndexAny(words, " -")
	head, last := words[:cut+1], words[cut+1:]
	if o, ok := ordinalEnds[last]; ok {
		return head + o
	}
	if strings.HasSuffix(last, "y") {
		return head + strings.TrimSuffix(last, "y") + "ieth"
	}
	return head + last + "th"
}

func pluralWords(words string) string {
	if strings.HasSuffix(words, "y") {
		return strings.TrimSuffix(words, "y") + "ies"
	}
	if strings.HasSuffix(words, "x") {
		return words + "es"
	}
	return words + "s"
}

var (
	romanTitle = regexp.MustCompile(`^(?i:(chapter|part|book|act|section)\s+)([IVXLC]+)\b`)
	romanOnly  = regexp.MustCompile(`^([IVXLC]+)\.?$`)
)

// speakableTitle is a chapter's title read aloud: "Chapter IV" is "Chapter Four".
func speakableTitle(title string, rules []sayRule) string {
	title = strings.TrimSpace(title)
	if m := romanTitle.FindStringSubmatch(title); m != nil {
		if n := romanValue(m[2]); n > 0 {
			title = m[1] + " " + numWords(n) + title[len(m[0]):]
		}
	} else if m := romanOnly.FindStringSubmatch(title); m != nil {
		if n := romanValue(m[1]); n > 0 {
			title = numWords(n)
		}
	}
	return speakable(title, rules)
}

// romanValue is the value of a Roman numeral, 0 when it isn't one.
func romanValue(s string) int {
	vals := map[byte]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100}
	total := 0
	for i := 0; i < len(s); i++ {
		v := vals[s[i]]
		if v == 0 {
			return 0
		}
		if i+1 < len(s) && vals[s[i+1]] > v {
			total -= v
		} else {
			total += v
		}
	}
	if total <= 0 || total >= 400 || toRoman(total) != s {
		return 0
	}
	return total
}

func toRoman(n int) string {
	var b strings.Builder
	for _, p := range []struct {
		v int
		s string
	}{{100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}} {
		for n >= p.v {
			b.WriteString(p.s)
			n -= p.v
		}
	}
	return b.String()
}

// sortedChapters is the chapters picked, in order, without repeats or ones the book hasn't got.
func sortedChapters(picked []int, n int) []int {
	seen := map[int]bool{}
	var out []int
	for _, c := range picked {
		if c >= 0 && c < n && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Ints(out)
	return out
}
