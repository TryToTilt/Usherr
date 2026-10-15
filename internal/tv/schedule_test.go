package tv

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

type fakeLib struct {
	eps    map[int64][]Item
	movies []Item
}

func (f fakeLib) Episodes(id int64) ([]Item, error) { return f.eps[id], nil }
func (f fakeLib) SeriesMatching([]string, []int) ([]int64, error) {
	return nil, nil
}
func (f fakeLib) Movies(ids []int64, all bool, _ []string, _ []int) ([]Item, error) {
	if len(ids) == 0 && !all {
		return nil, nil
	}
	return f.movies, nil
}

func show(id int64, n int) []Item {
	var out []Item
	for i := 1; i <= n; i++ {
		out = append(out, Item{Kind: "episode", ID: id*100 + int64(i), SeriesID: id, Title: fmt.Sprintf("Show %d", id), Path: fmt.Sprintf("/s%d/e%d", id, i)})
	}
	return out
}

func halfHour(Item) (time.Duration, error) { return 30 * time.Minute, nil }

func TestRotateKeepsEpisodeOrderAcrossBuilds(t *testing.T) {
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 5), 2: show(2, 5)}}
	ch := Channel{ID: 7, Name: "Test", Kind: "library", Enabled: true, Rules: Rules{Series: []int64{1, 2}}}
	st := State{}
	rnd := rand.New(rand.NewSource(1))
	start := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	first, err := Build(ch, lib, halfHour, &st, start, start.Add(2*time.Hour), rnd)
	if err != nil || len(first) != 4 {
		t.Fatalf("first build: %d programs, %v", len(first), err)
	}
	second, _ := Build(ch, lib, halfHour, &st, first[len(first)-1].End, start.Add(4*time.Hour), rnd)
	next := map[int64]int64{}
	for _, p := range append(first, second...) {
		if prev := next[p.SeriesID]; prev != 0 && p.ItemID != prev+1 && !(prev%100 == 5 && p.ItemID%100 == 1) {
			t.Errorf("show %d jumped from %d to %d", p.SeriesID, prev, p.ItemID)
		}
		next[p.SeriesID] = p.ItemID
	}
	// Back to back, no gaps.
	all := append(first, second...)
	for i := 1; i < len(all); i++ {
		if !all[i].Start.Equal(all[i-1].End) {
			t.Fatalf("gap between %d and %d", i-1, i)
		}
	}
	// Each round has both shows.
	if all[0].SeriesID == all[1].SeriesID {
		t.Error("a round should play each show once")
	}
}

func TestMoviesCappedPerDay(t *testing.T) {
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 50), 2: show(2, 50), 3: show(3, 50)}, movies: []Item{
		{Kind: "movie", ID: 900, Title: "A", Path: "/m/a"}, {Kind: "movie", ID: 901, Title: "B", Path: "/m/b"}, {Kind: "movie", ID: 902, Title: "C", Path: "/m/c"},
	}}
	ch := Channel{ID: 8, Name: "Mix", Kind: "library", Enabled: true, Rules: Rules{Series: []int64{1, 2, 3}, AllMovies: true, MaxMoviesPerDay: 2}}
	st := State{}
	start := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	list, err := Build(ch, lib, halfHour, &st, start, start.Add(24*time.Hour), rand.New(rand.NewSource(2)))
	if err != nil {
		t.Fatal(err)
	}
	movies := 0
	for _, p := range list {
		if p.Kind == "movie" {
			movies++
		}
	}
	if movies != 2 {
		t.Errorf("movies in a day = %d, want 2", movies)
	}
}

func TestSkipsFilesThatCantBeMeasured(t *testing.T) {
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 3)}}
	ch := Channel{ID: 9, Name: "One", Kind: "library", Enabled: true, Rules: Rules{Series: []int64{1}}}
	probe := func(it Item) (time.Duration, error) {
		if it.ID == 102 {
			return 0, fmt.Errorf("broken")
		}
		return 20 * time.Minute, nil
	}
	st := State{}
	start := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	list, _ := Build(ch, lib, probe, &st, start, start.Add(time.Hour), rand.New(rand.NewSource(3)))
	for _, p := range list {
		if p.ItemID == 102 {
			t.Fatal("a file that couldn't be measured was scheduled")
		}
	}
	if len(list) < 3 {
		t.Errorf("got %d programs", len(list))
	}
}

func TestStreamShowsJoinTheRotation(t *testing.T) {
	streamed := []Item{
		{Kind: "stream", SeriesID: -500, Title: "Thundercats", Subtitle: "S01E01 · Exodus", Path: StreamPath(500, 1, 1), Runtime: 22 * time.Minute},
		{Kind: "stream", SeriesID: -500, Title: "Thundercats", Subtitle: "S01E02 · The Unholy Alliance", Path: StreamPath(500, 1, 2), Runtime: 22 * time.Minute},
	}
	lib := fakeLib{eps: map[int64][]Item{1: show(1, 3), -500: streamed}}
	ch := Channel{ID: 10, Name: "Mix", Kind: "library", Enabled: true, Rules: Rules{Series: []int64{1}, Stream: []StreamShow{{TMDBID: 500, Title: "Thundercats"}}}}
	probe := func(it Item) (time.Duration, error) {
		if it.Kind == "stream" {
			return it.Runtime, nil
		}
		return 30 * time.Minute, nil
	}
	st := State{}
	start := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	list, err := Build(ch, lib, probe, &st, start, start.Add(2*time.Hour), rand.New(rand.NewSource(4)))
	if err != nil {
		t.Fatal(err)
	}
	streams := 0
	for _, p := range list {
		if p.Kind == "stream" {
			streams++
			if id, s, e, ok := ParseStreamPath(p.FilePath); !ok || id != 500 || s != 1 || e < 1 {
				t.Errorf("stream program path %q", p.FilePath)
			}
		}
	}
	if streams == 0 {
		t.Error("the streamed show never came on")
	}
}
