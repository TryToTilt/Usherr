package api

// Usherr: Read-to-Me's full cast. Dialogue (what is inside quotation marks)
// can be read in other voices than the narration: one voice for all of it, or
// a voice for each character. Who says a line is worked out in layers:
//
//  1. the text's own tags: "Run," Tom said / said Tom / Tom said, "Run";
//     he said / she said give at least whether it's a man or a woman;
//  2. an action beat: a paragraph that starts with a name and has a quote
//     ("Tom grinned. 'Run.'") is that person's;
//  3. the AI (when asked, chapter by chapter, home_readtome_cast_ai.go) for
//     the lines left;
//  4. an exchange: untagged lines between two people take turns;
//  5. what someone set by hand, which wins over everything.
//
// A name has to come up at least twice before it is a character; the rest are
// "someone". Lines nobody is found for get the "other dialogue" voice.

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ---- the voices of a cast ----

type rtvVoiceRef struct {
	Engine string `json:"engine"`
	Voice  string `json:"voice"`
}

type rtvCast struct {
	Dialogue *rtvVoiceRef           `json:"dialogue,omitempty"` // lines nobody known says, and characters without a voice
	Men      *rtvVoiceRef           `json:"men,omitempty"`      // men without a voice of their own
	Women    *rtvVoiceRef           `json:"women,omitempty"`
	People   map[string]rtvVoiceRef `json:"people,omitempty"` // character -> voice ("I": a first-person narrator's own lines)
	AI       bool                   `json:"ai,omitempty"`     // ask the AI for the lines the text doesn't tag
}

// refs is every voice the cast uses, for checking them.
func (c *rtvCast) refs() []rtvVoiceRef {
	if c == nil {
		return nil
	}
	var out []rtvVoiceRef
	for _, r := range []*rtvVoiceRef{c.Dialogue, c.Men, c.Women} {
		if r != nil {
			out = append(out, *r)
		}
	}
	for _, r := range c.People {
		out = append(out, r)
	}
	return out
}

// voiceFor is the voice a speaker's lines are read in; nil is the narrator's.
func (c *rtvCast) voiceFor(speaker string, genders map[string]string) *rtvVoiceRef {
	if c == nil || speaker == "" {
		return nil
	}
	if r, ok := c.People[speaker]; ok {
		return &r
	}
	if speaker == "I" {
		return nil // a first-person narrator says their own lines
	}
	g := genders[speaker]
	switch speaker {
	case "?m":
		g = "m"
	case "?f":
		g = "f"
	}
	if g == "m" && c.Men != nil {
		return c.Men
	}
	if g == "f" && c.Women != nil {
		return c.Women
	}
	return c.Dialogue
}

// ---- quotes ----

type quoteSeg struct {
	Text  string
	Quote bool
}

// quoteSegments cuts a paragraph into narration and quoted speech, without
// the quotation marks. Curly double quotes and straight ones; single curly
// quotes (British books) only in a paragraph with no double ones, and an
// apostrophe inside a word is never one. A quote still open at the end of the
// paragraph ends there (the next paragraph opens it again).
func quoteSegments(p string) []quoteSeg {
	r := []rune(p)
	single := !strings.ContainsAny(p, "“\"") && strings.ContainsRune(p, '‘')
	var out []quoteSeg
	var cur []rune
	in := false
	flush := func(q bool) {
		if s := strings.TrimSpace(string(cur)); s != "" {
			if !q {
				s = strings.TrimLeft(s, ",;: ")
			}
			if strings.IndexFunc(s, func(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) }) >= 0 {
				out = append(out, quoteSeg{Text: s, Quote: q})
			}
		}
		cur = cur[:0]
	}
	for i, c := range r {
		var prev, next rune
		if i > 0 {
			prev = r[i-1]
		}
		if i+1 < len(r) {
			next = r[i+1]
		}
		switch {
		case !in && (c == '“' || c == '"' || single && c == '‘' && !unicode.IsLetter(prev)):
			flush(false)
			in = true
			continue
		case in && (c == '”' || c == '"' || single && c == '’' && !unicode.IsLetter(next)):
			flush(true)
			in = false
			continue
		}
		cur = append(cur, c)
	}
	flush(in)
	return out
}

// ---- who says it ----

const speechVerbs = `said|says|asked|asks|replied|replies|answered|answers|whispered|whispers|shouted|shouts|cried|called|yelled|muttered|murmured|snapped|added|continued|began|insisted|exclaimed|laughed|sighed|demanded|protested|agreed|admitted|explained|repeated|suggested|warned|growled|hissed|breathed|screamed|pleaded|interrupted|offered|observed|remarked|retorted|announced|declared|stammered|grumbled|mumbled|whimpered|sobbed|gasped|chuckled|groaned|barked|roared|teased|joked|ordered|commanded|urged|begged|wondered|told|went on|put in|called out|spoke`

const nameRe = `((?:(?:Mister|Missus|Miz|Doctor|Professor|Captain|Lieutenant|Sergeant|General|Colonel|Reverend|Lord|Lady|Sir|Dame|Aunt|Uncle|King|Queen|Prince|Princess|Father|Mother|Master|Madam|Saint)\s+)?[A-Z][\p{L}'’-]+(?:\s+[A-Z][\p{L}'’-]+)?)`

var (
	tagVerbName   = regexp.MustCompile(`^(?:` + speechVerbs + `)\s+` + nameRe)                                               // said Tom
	tagNameVerb   = regexp.MustCompile(`^` + nameRe + `\s+(?:\p{Ll}+ly\s+)?(?:` + speechVerbs + `)\b`)                       // Tom said
	tagPronounA   = regexp.MustCompile(`^(?i:(he|she|i|they|we))\s+(?:\p{Ll}+ly\s+)?(?:` + speechVerbs + `)\b`)              // he said
	tagVerbPron   = regexp.MustCompile(`^(?:` + speechVerbs + `)\s+(he|she)\b`)                                              // said he
	tagBeforeName = regexp.MustCompile(nameRe + `\s+(?:\p{Ll}+ly\s+)?(?:` + speechVerbs + `)(?:\s+[\p{L}'’]+){0,4}\s*[,:]$`) // Tom said quietly,
	tagBeforePron = regexp.MustCompile(`(?:^|[\s.!?])(?i:(he|she|i))\s+(?:\p{Ll}+ly\s+)?(?:` + speechVerbs + `)(?:\s+[\p{L}'’]+){0,4}\s*[,:]$`)
	beatName      = regexp.MustCompile(`^` + nameRe + `\s+\p{Ll}`)
)

// notNames are capitalised words a tag or a beat can start with that aren't anyone.
var notNames = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`The A An He She It They We You I His Her Hers Its Their Our My Your Me Him Them Us But And Then When What Why How Where Who Whom Whose Which Yes No Oh Ah Well If As So Now There Here This That These Those After Before In On At For With Not Just All One Some Someone Somebody Everyone Everybody Nobody Anyone Nothing Something Everything Chapter Part Book Still Only Even Again Suddenly Finally Instead Perhaps Maybe Meanwhile Later Soon Outside Inside Behind Together Both Each Every Neither Either Another Other Most Many Several Few Once Twice Today Tonight Tomorrow Yesterday Please Thanks Thank Hello Hey Okay Sorry Good God Dear Darling Honey Mom Mum Dad Mommy Daddy Grandma Grandpa Sweetheart Love Listen Look Wait Stop Come Go Let Don’t Don't Can’t Can't Won’t Won't It’s It's I’m I'm`) {
		notNames[w] = true
	}
}

// castName checks a name a pattern found; "" when it isn't one.
func castName(n string) string {
	n = strings.TrimRight(strings.TrimSpace(n), "’'-")
	n = strings.TrimSuffix(strings.TrimSuffix(n, "’s"), "'s")
	f := strings.Fields(n)
	if len(f) == 0 || notNames[f[0]] {
		return ""
	}
	if len(f) == 2 && notNames[f[1]] {
		n = f[0]
	}
	return n
}

func pronounSpeaker(p string) string {
	switch strings.ToLower(p) {
	case "he":
		return "?m"
	case "she":
		return "?f"
	case "i":
		return "I"
	}
	return "?"
}

// tagAfter reads the narration right after a quote: said Tom, Tom said, he said.
func tagAfter(n string) string {
	if m := tagVerbName.FindStringSubmatch(n); m != nil {
		if name := castName(m[1]); name != "" {
			return name
		}
	}
	if m := tagNameVerb.FindStringSubmatch(n); m != nil {
		if name := castName(m[1]); name != "" {
			return name
		}
	}
	if m := tagPronounA.FindStringSubmatch(n); m != nil {
		return pronounSpeaker(m[1])
	}
	if m := tagVerbPron.FindStringSubmatch(n); m != nil {
		return pronounSpeaker(m[1])
	}
	return ""
}

// tagBefore reads the narration right before a quote: Tom said, "...".
func tagBefore(n string) string {
	n = strings.TrimSpace(n)
	if m := tagBeforeName.FindStringSubmatch(n); m != nil {
		if name := castName(m[1]); name != "" {
			return name
		}
	}
	if m := tagBeforePron.FindStringSubmatch(n); m != nil {
		return pronounSpeaker(m[1])
	}
	return ""
}

type castLine struct {
	Para    int    `json:"para"`
	Index   int    `json:"index"` // its place among the chapter's quotes
	Text    string `json:"text"`
	Hash    string `json:"-"`
	Speaker string `json:"speaker"` // a name, "I", "?" (someone), "?m" / "?f" (a man, a woman)
	How     string `json:"how"`     // tag, beat, ai, turn, you, "" (not known)
}

type castPara struct {
	Scene bool
	Segs  []quoteSeg
	Lines []int // the paragraph's quotes, as indexes into the chapter's lines
}

// castChapter cuts a chapter (made speakable) into paragraphs of narration
// and quotes, and finds who says each quote from the text alone (tags and
// beats; the exchanges come later, after the AI and the rare names).
func castChapter(text string) ([]castPara, []castLine) {
	var paras []castPara
	var lines []castLine
	for _, raw := range strings.Split(text, "\n\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if isSceneBreak(raw) {
			paras = append(paras, castPara{Scene: true})
			continue
		}
		cp := castPara{Segs: quoteSegments(raw)}
		pi := len(paras)
		speaker, how := "", ""
		var own []string
		for si, sg := range cp.Segs {
			if !sg.Quote {
				continue
			}
			tag := ""
			if si+1 < len(cp.Segs) && !cp.Segs[si+1].Quote {
				tag = tagAfter(cp.Segs[si+1].Text)
			}
			if tag == "" && si > 0 && !cp.Segs[si-1].Quote {
				tag = tagBefore(cp.Segs[si-1].Text)
			}
			if tag != "" && speaker == "" {
				speaker, how = tag, "tag"
			}
			own = append(own, tag)
			cp.Lines = append(cp.Lines, len(lines))
			lines = append(lines, castLine{Para: pi, Index: len(lines), Text: sg.Text, Hash: hashOf(sg.Text)})
		}
		// No tag: a paragraph that starts with someone doing something is theirs.
		if speaker == "" && len(cp.Lines) > 0 && len(cp.Segs) > 0 && !cp.Segs[0].Quote {
			if m := beatName.FindStringSubmatch(cp.Segs[0].Text); m != nil {
				if name := castName(m[1]); name != "" {
					speaker, how = name, "beat"
				}
			}
		}
		// A paragraph's quotes are one person's, unless one says otherwise.
		for k, li := range cp.Lines {
			switch {
			case own[k] != "":
				lines[li].Speaker, lines[li].How = own[k], "tag"
			case speaker != "":
				lines[li].Speaker, lines[li].How = speaker, how
			default:
				lines[li].Speaker = "?"
			}
		}
		paras = append(paras, cp)
	}
	return paras, lines
}

// takeTurns gives untagged lines in an exchange between two people to the one
// whose turn it is: a paragraph of dialogue nobody is found for, after two
// paragraphs by A and then B, is A's. Narration between breaks the exchange.
func takeTurns(paras []castPara, lines []castLine) {
	var last, before string
	for _, p := range paras {
		if p.Scene || len(p.Lines) == 0 {
			last, before = "", ""
			continue
		}
		who := lines[p.Lines[0]].Speaker
		known := who != "" && !strings.HasPrefix(who, "?")
		if !known && last != "" && before != "" && last != before {
			for _, li := range p.Lines {
				if lines[li].Speaker == "?" || lines[li].Speaker == "?m" || lines[li].Speaker == "?f" {
					lines[li].Speaker, lines[li].How = before, "turn"
				}
			}
			who, known = before, true
		}
		if known {
			before, last = last, who
		} else {
			last, before = "", ""
		}
	}
}

// ---- a whole book ----

type castPerson struct {
	Name   string `json:"name"`
	Lines  int    `json:"lines"`
	Gender string `json:"gender,omitempty"` // m, f (from the AI)
}

type castBook struct {
	Paras   [][]castPara
	Lines   [][]castLine
	People  []castPerson
	Genders map[string]string
	Quotes  int
	Known   int // lines given to a character (or the "I" narrator)
}

// castOf works out the whole book's cast: the text's tags and beats, names
// that come up only once made "someone", what the AI found, the exchanges,
// and what was set by hand.
func (s *Server) castOf(ctx context.Context, bookID int64, book rtvBook, rules []sayRule) castBook {
	cb := castBook{Genders: map[string]string{}}
	count := map[string]int{}
	for _, ch := range book.Chapters {
		paras, lines := castChapter(speakable(ch.Text, rules))
		for _, l := range lines {
			if l.How != "" && !strings.HasPrefix(l.Speaker, "?") {
				count[l.Speaker]++
			}
		}
		cb.Paras = append(cb.Paras, paras)
		cb.Lines = append(cb.Lines, lines)
	}
	ai, you := s.castRows(ctx, bookID)
	rows, err := s.db.QueryContext(ctx, `SELECT name, gender FROM readtome_people WHERE book_id = ?`, bookID)
	if err == nil {
		for rows.Next() {
			var n, g string
			if rows.Scan(&n, &g) == nil {
				cb.Genders[n] = g
			}
		}
		rows.Close()
	}
	for ci := range cb.Lines {
		lines := cb.Lines[ci]
		for i := range lines {
			l := &lines[i]
			if l.Speaker != "I" && !strings.HasPrefix(l.Speaker, "?") && count[l.Speaker] < 2 {
				l.Speaker, l.How = "?", "" // one mention: not a character
			}
			if r, ok := ai[lineKey{ci, l.Index}]; ok && r.hash == l.Hash && l.How != "tag" {
				l.Speaker, l.How = r.speaker, "ai"
			}
		}
		takeTurns(cb.Paras[ci], lines)
		for i := range lines {
			if r, ok := you[lineKey{ci, lines[i].Index}]; ok && r.hash == lines[i].Hash {
				lines[i].Speaker, lines[i].How = r.speaker, "you"
			}
		}
	}
	people := map[string]int{}
	for _, lines := range cb.Lines {
		for _, l := range lines {
			cb.Quotes++
			if l.Speaker != "" && !strings.HasPrefix(l.Speaker, "?") {
				cb.Known++
				people[l.Speaker]++
			}
		}
	}
	for n, c := range people {
		cb.People = append(cb.People, castPerson{Name: n, Lines: c, Gender: cb.Genders[n]})
	}
	sort.Slice(cb.People, func(i, j int) bool {
		return cb.People[i].Lines > cb.People[j].Lines || cb.People[i].Lines == cb.People[j].Lines && cb.People[i].Name < cb.People[j].Name
	})
	return cb
}

type lineKey struct{ chapter, quote int }

type lineRow struct{ speaker, hash string }

func (s *Server) castRows(ctx context.Context, bookID int64) (ai, you map[lineKey]lineRow) {
	ai, you = map[lineKey]lineRow{}, map[lineKey]lineRow{}
	rows, err := s.db.QueryContext(ctx, `SELECT chapter, quote, hash, speaker, source FROM readtome_lines WHERE book_id = ?`, bookID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var k lineKey
		var r lineRow
		var src string
		if rows.Scan(&k.chapter, &k.quote, &r.hash, &r.speaker, &src) == nil {
			if src == "you" {
				you[k] = r
			} else {
				ai[k] = r
			}
		}
	}
	return
}

// castPieces is what is spoken for a chapter with a cast: each paragraph cut
// into narration and quotes, each piece saying who says it.
func castPieces(paras []castPara, lines []castLine, max int) []speechPiece {
	var pieces []speechPiece
	for _, p := range paras {
		if p.Scene {
			if n := len(pieces); n > 0 {
				pieces[n-1].SceneEnd = true
			}
			continue
		}
		q := 0
		for _, sg := range p.Segs {
			speaker := ""
			if sg.Quote {
				speaker = lines[p.Lines[q]].Speaker
				q++
			}
			for _, c := range chunkText(sg.Text, max) {
				pieces = append(pieces, speechPiece{Text: c, Speaker: speaker})
			}
		}
		if n := len(pieces); n > 0 {
			pieces[n-1].ParaEnd = true
		}
	}
	return pieces
}
