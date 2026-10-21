package quality

import "github.com/rdborg/mediarium/internal/parser"

// lazyRel is a release title whose parse is read at most once, and only if
// something needs it. A caller that already parsed the title hands that parse
// in (see the ...Rel functions), so a search over many releases does not parse
// the same name again for every check.
type lazyRel struct {
	title string
	rel   parser.Release
	done  bool
}

func lazyFor(title string) *lazyRel { return &lazyRel{title: title} }

func knownRel(title string, rel parser.Release) *lazyRel {
	return &lazyRel{title: title, rel: rel, done: true}
}

func (l *lazyRel) get() parser.Release {
	if !l.done {
		l.rel, l.done = parser.Parse(l.title), true
	}
	return l.rel
}
