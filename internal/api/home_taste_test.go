package api

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

// withTraits fills the kept traits with a made-up catalogue for one test.
func withTraits(t *testing.T, list map[int]*metadata.TitleTraits) {
	t.Helper()
	traitStore.Lock()
	saved := traitStore.m
	traitStore.m = map[string]*metadata.TitleTraits{}
	for id, tr := range list {
		traitStore.m[traitID("movie", id)] = tr
	}
	traitStore.df, traitStore.loaded = nil, true
	traitStore.Unlock()
	t.Cleanup(func() {
		traitStore.Lock()
		traitStore.m, traitStore.df, traitStore.loaded = saved, nil, false
		traitStore.Unlock()
	})
}

func heist(name string, year int) *metadata.TitleTraits {
	return &metadata.TitleTraits{Name: name, Year: year, Lang: "en", Genres: []string{"Crime", "Thriller"}, Keywords: []string{"heist", "con artist"}, Makers: []string{"Steven Soderbergh"}, Cast: []string{"George Clooney"}}
}

func horror(name string, year int) *metadata.TitleTraits {
	return &metadata.TitleTraits{Name: name, Year: year, Lang: "en", Genres: []string{"Horror"}, Keywords: []string{"haunted house", "possession"}, Makers: []string{"James Wan"}}
}

func TestTasteMatchesWhatYouLike(t *testing.T) {
	cat := map[int]*metadata.TitleTraits{
		1: heist("Ocean's Eleven", 2001), 2: heist("Ocean's Twelve", 2004), 3: heist("Logan Lucky", 2017), 4: heist("Out of Sight", 1998),
		5: horror("The Conjuring", 2013), 6: horror("Insidious", 2010),
		100: heist("Ocean's Thirteen", 2007), 101: horror("Annabelle", 2014),
	}
	// An ordinary catalogue, for what an ordinary title scores.
	for i := 0; i < 40; i++ {
		cat[200+i] = &metadata.TitleTraits{Name: fmt.Sprintf("Drama %d", i), Year: 1980 + i, Lang: "en", Genres: []string{"Drama"}, Keywords: []string{fmt.Sprintf("topic %d", i%7)}, Makers: []string{fmt.Sprintf("Director %d", i)}}
	}
	withTraits(t, cat)

	p := &tasteProfile{kind: "movie", w: map[string]float64{}, voted: map[int]int{1: 1, 2: 1, 5: -1, 6: -1}, at: time.Now(), signals: []tasteSignal{
		{1, "Ocean's Eleven", 2, "liked"}, {2, "Ocean's Twelve", 2, "liked"}, {5, "The Conjuring", -2.5, "disliked"}, {6, "Insidious", -2.5, "disliked"},
		{3, "Logan Lucky", 1, "watched"}, {4, "Out of Sight", 0.9, "watched"},
	}}
	traits := map[int]*metadata.TitleTraits{}
	for _, sg := range p.signals {
		traits[sg.id] = cat[sg.id]
	}
	var sample []*metadata.TitleTraits
	for i := 0; i < 40; i++ {
		sample = append(sample, cat[200+i])
	}
	p.build(traits, sample)
	if !p.ready {
		t.Fatal("four liked or watched titles: the taste should be ready")
	}

	more, scary, plain := p.judge(cat[100], traits, 100), p.judge(cat[101], traits, 101), p.match(cat[205])
	if more.Match < 85 || scary.Match > 25 || plain < 35 || plain > 65 {
		t.Fatalf("matches: heist %d, horror %d, ordinary drama %d", more.Match, scary.Match, plain)
	}
	if more.Because == "" || more.Via != "liked" {
		t.Errorf("a heist film should be because of a liked heist film, got %q via %q", more.Because, more.Via)
	}
	if len(more.Tags) == 0 || (more.Tags[0] != "Steven Soderbergh" && more.Tags[0] != "heist" && more.Tags[0] != "con artist") {
		t.Errorf("tags: %v", more.Tags)
	}
	if scary.Because != "" {
		t.Errorf("a poor match has no because, got %q", scary.Because)
	}
	likes, dislikes := p.leanings(5, 5)
	if len(likes) == 0 || len(dislikes) == 0 || dislikes[0].Type == "" {
		t.Fatalf("leanings: %v / %v", likes, dislikes)
	}
	if line := p.tasteLine(); line == "" {
		t.Error("the AI's line about the taste is empty")
	}
}

func TestTasteSampleFavoursGoodMatches(t *testing.T) {
	list := []roomTitle{{TMDB: 1, Rating: 7}, {TMDB: 2, Rating: 7}}
	matches := map[int]int{1: 95, 2: 10}
	r := rand.New(rand.NewPCG(1, 2))
	first := map[int]int{}
	for i := 0; i < 400; i++ {
		first[tasteSample(list, matches, 2, r)[0].TMDB]++
	}
	if first[1] < 300 {
		t.Fatalf("the good match came first %d times in 400", first[1])
	}
}

func TestNoTasteWithTooLittle(t *testing.T) {
	withTraits(t, map[int]*metadata.TitleTraits{1: heist("Ocean's Eleven", 2001)})
	p := &tasteProfile{kind: "movie", w: map[string]float64{}, signals: []tasteSignal{{1, "Ocean's Eleven", 2, "liked"}}}
	p.build(map[int]*metadata.TitleTraits{1: heist("Ocean's Eleven", 2001)}, nil)
	if p.ready || p.match(heist("Ocean's Thirteen", 2007)) != 0 {
		t.Fatal("one liked title isn't a taste yet")
	}
}
