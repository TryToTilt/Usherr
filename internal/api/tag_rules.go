package api

// Usherr: tags that do something, as in Radarr and Sonarr.
//
//   - Auto-tagging: a rule gives a tag to every movie or show that matches it
//     (its genres, years, original language, library folder), and can take
//     the tag off again when a title stops matching. Rules run when they are
//     saved, from "Tag the library now", and every few hours for new titles.
//   - Tag rules: titles with a tag search only some indexers, or only Usenet
//     or torrents. They narrow what the title's quality profile allows.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/quality"
)

const keyTagRules = "usherr.tag_rules"

type autoTagRule struct {
	Tag string `json:"tag"`
	// Kind is "" (movies and shows), "movie" or "tv".
	Kind      string   `json:"kind"`
	Genres    []string `json:"genres"`    // any of them ([] = any genre)
	Languages []string `json:"languages"` // original language codes, like "ja" ([] = any)
	YearFrom  int      `json:"yearFrom"`
	YearTo    int      `json:"yearTo"`
	RootPath  string   `json:"rootPath"` // library folder ("" = any)
	// Remove takes the tag off titles that no longer match.
	Remove  bool `json:"remove"`
	Enabled bool `json:"enabled"`
}

type tagRule struct {
	Tag      string  `json:"tag"`
	Indexers []int64 `json:"indexers"`
	Protocol string  `json:"protocol"` // "", "usenet" or "torrent"
}

type tagRulesConfig struct {
	AutoTags []autoTagRule `json:"autoTags"`
	TagRules []tagRule     `json:"tagRules"`
}

const maxTagRules = 50

func (c tagRulesConfig) validate() (tagRulesConfig, string) {
	if len(c.AutoTags) > maxTagRules || len(c.TagRules) > maxTagRules {
		return c, fmt.Sprintf("Use at most %d rules of each kind.", maxTagRules)
	}
	out := tagRulesConfig{AutoTags: []autoTagRule{}, TagRules: []tagRule{}}
	for _, r := range c.AutoTags {
		tags := library.NormalizeTags([]string{r.Tag})
		if len(tags) != 1 {
			return c, "Give each auto-tagging rule a tag (one word or a few, without commas)."
		}
		r.Tag = tags[0]
		if r.Kind != "" && r.Kind != "movie" && r.Kind != "tv" {
			return c, "A rule is for movies, shows, or both."
		}
		if r.YearFrom != 0 && r.YearTo != 0 && r.YearFrom > r.YearTo {
			return c, fmt.Sprintf("In the %q rule the first year is after the last.", r.Tag)
		}
		r.Genres = cleanWords(r.Genres)
		r.Languages = cleanWords(r.Languages)
		for i, l := range r.Languages {
			r.Languages[i] = strings.ToLower(l)
		}
		r.RootPath = strings.TrimSpace(r.RootPath)
		if len(r.Genres) == 0 && len(r.Languages) == 0 && r.YearFrom == 0 && r.YearTo == 0 && r.RootPath == "" && r.Kind == "" {
			return c, fmt.Sprintf("The %q rule has no condition, so it would tag everything. Give it at least one.", r.Tag)
		}
		out.AutoTags = append(out.AutoTags, r)
	}
	seen := map[string]bool{}
	for _, r := range c.TagRules {
		tags := library.NormalizeTags([]string{r.Tag})
		if len(tags) != 1 {
			return c, "Give each tag rule a tag."
		}
		r.Tag = tags[0]
		if seen[strings.ToLower(r.Tag)] {
			return c, fmt.Sprintf("There are two rules for the tag %q. Put them together.", r.Tag)
		}
		seen[strings.ToLower(r.Tag)] = true
		if r.Protocol != quality.ProtocolAny && r.Protocol != quality.ProtocolUsenet && r.Protocol != quality.ProtocolTorrent {
			return c, "A tag rule takes Usenet, torrents, or either."
		}
		ids := []int64{}
		for _, id := range r.Indexers {
			if id > 0 {
				ids = append(ids, id)
			}
		}
		r.Indexers = ids
		out.TagRules = append(out.TagRules, r)
	}
	return out, ""
}

func cleanWords(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, w := range in {
		w = strings.TrimSpace(w)
		if w == "" || seen[strings.ToLower(w)] || len(w) > 60 {
			continue
		}
		seen[strings.ToLower(w)] = true
		out = append(out, w)
	}
	return out
}

func (s *Server) tagRules() tagRulesConfig {
	c := tagRulesConfig{AutoTags: []autoTagRule{}, TagRules: []tagRule{}}
	raw, err := s.Settings.Get(keyTagRules)
	if err != nil || raw == "" {
		return c
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		slog.Warn("tag rules: couldn't read them", "err", err)
		return tagRulesConfig{AutoTags: []autoTagRule{}, TagRules: []tagRule{}}
	}
	if c.AutoTags == nil {
		c.AutoTags = []autoTagRule{}
	}
	if c.TagRules == nil {
		c.TagRules = []tagRule{}
	}
	return c
}

func (s *Server) handleGetTagRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.tagRules())
}

func (s *Server) handlePutTagRules(w http.ResponseWriter, r *http.Request) {
	var in tagRulesConfig
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	c, problem := in.validate()
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	raw, _ := json.Marshal(c)
	if err := s.Settings.Set(keyTagRules, string(raw), false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.startAutoTag()
	writeJSON(w, http.StatusOK, c)
}

// POST /api/tag-rules/apply: run the auto-tagging rules over the library now.
func (s *Server) handleApplyAutoTags(w http.ResponseWriter, r *http.Request) {
	started := s.startAutoTag()
	writeJSON(w, http.StatusAccepted, map[string]any{"started": started, "last": autoTagLast.Load()})
}

// ---- auto-tagging ----

var (
	autoTagBusy atomic.Bool
	autoTagLast atomic.Value // autoTagReport
)

type autoTagReport struct {
	At      string `json:"at"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Checked int    `json:"checked"`
}

// startAutoTag runs the rules in the background; false when a run is going.
func (s *Server) startAutoTag() bool {
	if !autoTagBusy.CompareAndSwap(false, true) {
		return false
	}
	go func() {
		defer autoTagBusy.Store(false)
		s.runAutoTags(context.Background())
	}()
	return true
}

func (s *Server) autoTagJob(ctx context.Context) {
	if !autoTagBusy.CompareAndSwap(false, true) {
		return
	}
	defer autoTagBusy.Store(false)
	s.runAutoTags(ctx)
}

// titleFacts is what a rule looks at.
type titleFacts struct {
	kind     string
	tmdbID   int
	genres   []string
	year     int
	root     string
	language func() string // looked up only when a rule asks
}

func (r autoTagRule) matches(f titleFacts) bool {
	if r.Kind != "" && r.Kind != f.kind {
		return false
	}
	if r.YearFrom != 0 && (f.year == 0 || f.year < r.YearFrom) {
		return false
	}
	if r.YearTo != 0 && (f.year == 0 || f.year > r.YearTo) {
		return false
	}
	if r.RootPath != "" && !strings.EqualFold(strings.TrimRight(r.RootPath, `/\`), strings.TrimRight(f.root, `/\`)) {
		return false
	}
	if len(r.Genres) > 0 {
		hit := false
		for _, g := range f.genres {
			for _, want := range r.Genres {
				if strings.EqualFold(g, want) {
					hit = true
				}
			}
		}
		if !hit {
			return false
		}
	}
	if len(r.Languages) > 0 {
		lang := strings.ToLower(f.language())
		hit := false
		for _, want := range r.Languages {
			if lang == want {
				hit = true
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// autoTagChanges is what the rules add to and take off one title.
func autoTagChanges(rules []autoTagRule, f titleFacts, has []string) (add, remove []string) {
	have := map[string]bool{}
	for _, t := range has {
		have[strings.ToLower(t)] = true
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if r.matches(f) {
			if !have[strings.ToLower(r.Tag)] {
				add = append(add, r.Tag)
				have[strings.ToLower(r.Tag)] = true
			}
		} else if r.Remove && have[strings.ToLower(r.Tag)] {
			remove = append(remove, r.Tag)
			delete(have, strings.ToLower(r.Tag))
		}
	}
	return add, remove
}

func (s *Server) runAutoTags(ctx context.Context) {
	rules := s.tagRules().AutoTags
	on := 0
	for _, r := range rules {
		if r.Enabled {
			on++
		}
	}
	if on == 0 {
		return
	}
	rep := autoTagReport{}
	apply := func(k library.TitleKind, id int64, f titleFacts, has []string) {
		add, remove := autoTagChanges(rules, f, has)
		if len(add) == 0 && len(remove) == 0 {
			return
		}
		added, removed, err := s.MovieRepo.ChangeTitleTags(k, id, add, remove)
		if err != nil {
			slog.Warn("auto-tagging: couldn't change tags", "kind", k, "id", id, "err", err)
			return
		}
		rep.Added += len(added)
		rep.Removed += len(removed)
		if len(added) > 0 || len(removed) > 0 {
			s.pushTags(k, f.tmdbID, added, removed)
		}
	}
	lang := func(kind string, tmdb int) func() string {
		return func() string { return s.originalLanguageOf(kind, tmdb) }
	}
	if movies, err := s.MovieRepo.List(); err == nil {
		tags, _ := s.MovieRepo.AllTitleTags(library.TagMovie)
		for _, m := range movies {
			if ctx.Err() != nil {
				return
			}
			rep.Checked++
			apply(library.TagMovie, m.ID, titleFacts{kind: "movie", tmdbID: m.TMDBID, genres: m.Genres, year: m.Year, root: m.RootPath, language: lang("movie", m.TMDBID)}, tags[m.ID])
		}
	}
	if shows, err := s.MovieRepo.ListSeries(); err == nil {
		tags, _ := s.MovieRepo.AllTitleTags(library.TagSeries)
		for _, sr := range shows {
			if ctx.Err() != nil {
				return
			}
			rep.Checked++
			apply(library.TagSeries, sr.ID, titleFacts{kind: "tv", tmdbID: sr.TMDBID, genres: sr.Genres, year: sr.Year, root: sr.RootPath, language: lang("tv", sr.TMDBID)}, tags[sr.ID])
		}
	}
	rep.At = time.Now().UTC().Format(time.RFC3339)
	autoTagLast.Store(rep)
	if rep.Added > 0 || rep.Removed > 0 {
		slog.Info("auto-tagging", "added", rep.Added, "removed", rep.Removed, "checked", rep.Checked)
	}
}

// ---- tag rules in a search ----

// withTagRules narrows a profile by the rules of a title's tags: their
// indexers (several tags: any of their indexers; with the profile's own
// list, only indexers both allow) and their protocol (the profile's own
// choice wins).
func withTagRules(p quality.Profile, rules []tagRule, tags []string) quality.Profile {
	if len(rules) == 0 || len(tags) == 0 {
		return p
	}
	has := map[string]bool{}
	for _, t := range tags {
		has[strings.ToLower(t)] = true
	}
	var ids []int64
	seen := map[int64]bool{}
	protocol := ""
	for _, r := range rules {
		if !has[strings.ToLower(r.Tag)] {
			continue
		}
		for _, id := range r.Indexers {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		if protocol == "" {
			protocol = r.Protocol
		}
	}
	if len(ids) > 0 {
		if len(p.Indexers) == 0 {
			p.Indexers = ids
		} else {
			var both []int64
			for _, id := range p.Indexers {
				if seen[id] {
					both = append(both, id)
				}
			}
			if len(both) == 0 {
				both = []int64{-1} // nothing both allow: no indexer fits
			}
			p.Indexers = both
		}
	}
	if p.Protocol == quality.ProtocolAny && protocol != "" {
		p.Protocol = protocol
	}
	return p
}

// tagRulesFor is withTagRules for a title in the library.
func (s *Server) tagRulesFor(p quality.Profile, k library.TitleKind, id int64) quality.Profile {
	rules := s.tagRules().TagRules
	if len(rules) == 0 || id == 0 {
		return p
	}
	tags, err := s.MovieRepo.TitleTags(k, id)
	if err != nil {
		return p
	}
	return withTagRules(p, rules, tags)
}
