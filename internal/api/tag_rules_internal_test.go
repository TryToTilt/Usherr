package api

import (
	"reflect"
	"testing"

	"github.com/rdborg/mediarium/internal/quality"
)

// Usherr: auto-tagging and tag rules.

func TestAutoTagChanges(t *testing.T) {
	rules := []autoTagRule{
		{Tag: "Anime", Kind: "tv", Genres: []string{"Animation"}, Languages: []string{"ja"}, Enabled: true, Remove: true},
		{Tag: "Classics", YearTo: 1979, Enabled: true},
		{Tag: "Off", Genres: []string{"Animation"}, Enabled: false},
	}
	asked := 0
	ja := func() string { asked++; return "ja" }
	f := titleFacts{kind: "tv", genres: []string{"Animation", "Action"}, year: 1998, language: ja}
	add, remove := autoTagChanges(rules, f, nil)
	if !reflect.DeepEqual(add, []string{"Anime"}) || len(remove) != 0 {
		t.Errorf("anime show: add %v remove %v", add, remove)
	}
	// Already tagged: nothing to do.
	if add, _ := autoTagChanges(rules, f, []string{"anime"}); len(add) != 0 {
		t.Errorf("tagged twice: %v", add)
	}
	// No longer matching: the tag comes off (Remove is on), Classics stays (Remove off).
	movie := titleFacts{kind: "movie", genres: []string{"Drama"}, year: 2001, language: ja}
	add, remove = autoTagChanges(rules, movie, []string{"Anime", "Classics"})
	if len(add) != 0 || !reflect.DeepEqual(remove, []string{"Anime"}) {
		t.Errorf("stopped matching: add %v remove %v", add, remove)
	}
	old := titleFacts{kind: "movie", year: 1960, language: func() string { t.Error("language looked up for a rule that doesn't need it"); return "" }}
	if add, _ := autoTagChanges(rules[1:2], old, nil); !reflect.DeepEqual(add, []string{"Classics"}) {
		t.Errorf("year rule: %v", add)
	}
	if asked == 0 {
		t.Error("the language was never looked up")
	}
}

func TestTagRulesValidate(t *testing.T) {
	if _, p := (tagRulesConfig{AutoTags: []autoTagRule{{Tag: "Everything", Enabled: true}}}).validate(); p == "" {
		t.Error("a rule with no condition was accepted")
	}
	if _, p := (tagRulesConfig{AutoTags: []autoTagRule{{Tag: "Bad", YearFrom: 2000, YearTo: 1990}}}).validate(); p == "" {
		t.Error("years in the wrong order were accepted")
	}
	if _, p := (tagRulesConfig{TagRules: []tagRule{{Tag: "Kids"}, {Tag: "kids"}}}).validate(); p == "" {
		t.Error("two rules for one tag were accepted")
	}
	c, p := (tagRulesConfig{AutoTags: []autoTagRule{{Tag: " Anime ", Languages: []string{"JA", "ja"}, Enabled: true}}}).validate()
	if p != "" || c.AutoTags[0].Tag != "Anime" || !reflect.DeepEqual(c.AutoTags[0].Languages, []string{"ja"}) {
		t.Errorf("tidy: %+v %q", c, p)
	}
}

func TestWithTagRules(t *testing.T) {
	rules := []tagRule{{Tag: "Anime", Indexers: []int64{5, 6}, Protocol: "torrent"}, {Tag: "4K", Indexers: []int64{7}}}
	p := quality.Profile{Name: "HD"}
	got := withTagRules(p, rules, []string{"anime"})
	if !reflect.DeepEqual(got.Indexers, []int64{5, 6}) || got.Protocol != "torrent" {
		t.Errorf("one tag: %+v", got.Extras)
	}
	got = withTagRules(p, rules, []string{"Anime", "4K"})
	if !reflect.DeepEqual(got.Indexers, []int64{5, 6, 7}) {
		t.Errorf("two tags: %v", got.Indexers)
	}
	p.Indexers = []int64{6, 9}
	p.Protocol = "usenet"
	got = withTagRules(p, rules, []string{"Anime"})
	if !reflect.DeepEqual(got.Indexers, []int64{6}) || got.Protocol != "usenet" {
		t.Errorf("with the profile's own: %+v", got.Extras)
	}
	p.Indexers = []int64{9}
	if got := withTagRules(p, rules, []string{"Anime"}); !reflect.DeepEqual(got.Indexers, []int64{-1}) {
		t.Errorf("nothing in common should allow no indexer: %v", got.Indexers)
	}
	if got := withTagRules(quality.Profile{}, rules, []string{"Kids"}); len(got.Indexers) != 0 || got.Protocol != "" {
		t.Errorf("a tag with no rule changed the profile: %+v", got.Extras)
	}
}
