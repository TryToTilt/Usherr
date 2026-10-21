package tv

import (
	"fmt"
	"math/rand"
	"sort"
	"time"
)

// Item is something a channel can play: an episode or a movie with a file,
// or a streamed episode (Kind "stream", Path a StreamPath, Runtime its
// length as TMDB gives it).
type Item struct {
	Kind     string // episode | movie | stream
	ID       int64
	SeriesID int64
	Title    string
	Subtitle string
	Path     string
	Size     int64
	Runtime  time.Duration
	// Usherr: when an episode first aired (YYYY-MM-DD) and the youngest age
	// the title suits (0 = not known), for premieres and family hours.
	AirDate string
	Age     int
}

// Library is where a channel's items come from.
type Library interface {
	// Episodes lists a show's episodes that have a file, in airing order.
	// A negative id is a streamed show (minus its TMDB id): its episodes are
	// the ones that have aired.
	Episodes(seriesID int64) ([]Item, error)
	// SeriesMatching lists shows with any of the genres and in any of the
	// decades (an empty list matches all).
	SeriesMatching(genres []string, decades []int) ([]int64, error)
	// Movies lists movies with a file: the given ids, plus (when all is set)
	// every movie matching the genres and decades.
	Movies(ids []int64, all bool, genres []string, decades []int) ([]Item, error)
}

// Prober measures how long a file plays.
type Prober func(it Item) (time.Duration, error)

// State is where a channel is in its rotation, kept between builds so the
// schedule carries on rather than starting over.
type State struct {
	Next      map[int64]int `json:"next"`      // show -> index of its next episode
	Round     []int64       `json:"round"`     // shows still to come this round
	ShowIndex int           `json:"showIndex"` // in-order channels: which show is on
	MovieDeck []int64       `json:"movieDeck"` // movies still to come, shuffled
	MovieDay  string        `json:"movieDay"`  // the day MoviesToday counts
	MoviesDay int           `json:"moviesDay"`
	// Usherr: where each daypart's own line-up is (blocks.go), where each
	// holiday's list is, and which new episodes have had their premiere.
	Parts     map[string]*PartState `json:"parts,omitempty"`
	Holiday   map[string]int        `json:"holiday,omitempty"`
	Premiered map[string]string     `json:"premiered,omitempty"` // file -> day it premiered
}

func errNothing(name string) error {
	return fmt.Errorf("channel %q has nothing to play: add shows or movies that are in the library", name)
}

func errMeasure(name string) error {
	return fmt.Errorf("channel %q: too many files couldn't be measured", name)
}

// Build lays out a channel's programs from from until until (the last one
// may run past it). The state moves on as items are used.
func Build(ch Channel, lib Library, probe Prober, st *State, from, until time.Time, rnd *rand.Rand) ([]Program, error) {
	if st.Next == nil {
		st.Next = map[int64]int{}
	}
	if len(ch.Rules.Fixed) > 0 { // Usherr: Real TV, a network's day
		return buildFixed(ch, lib, probe, st, from, until, rnd)
	}
	if usesBlocks(ch.Rules) { // Usherr: a grid, dayparts, slots or filler
		return buildBlocks(ch, lib, probe, st, from, until, rnd)
	}
	shows, err := channelShows(ch.Rules, lib)
	if err != nil {
		return nil, err
	}
	episodes := map[int64][]Item{}
	for _, s := range shows {
		list, err := lib.Episodes(s)
		if err != nil {
			return nil, err
		}
		if len(list) > 0 {
			episodes[s] = list
		}
	}
	shows = keepWith(shows, episodes)
	movies, err := lib.Movies(ch.Rules.Movies, ch.Rules.AllMovies, ch.Rules.Genres, ch.Rules.Decades)
	if err != nil {
		return nil, err
	}
	movieByID := map[int64]Item{}
	for _, m := range movies {
		movieByID[m.ID] = m
	}
	if len(shows) == 0 && len(movies) == 0 {
		return nil, fmt.Errorf("channel %q has nothing to play: add shows or movies that are in the library", ch.Name)
	}

	order := ch.Rules.Order
	if order == "" {
		order = "rotate"
	}
	var out []Program
	t := from
	failures := 0
	sinceMovie := 0
	for t.Before(until) {
		if failures > 50 {
			return out, fmt.Errorf("channel %q: too many files couldn't be measured", ch.Name)
		}
		it, ok := nextItem(order, shows, episodes, movies, movieByID, st, rnd, &sinceMovie, ch.Rules.MaxMoviesPerDay, t)
		if !ok {
			break
		}
		d, err := probe(it)
		if err != nil || d < 30*time.Second {
			failures++
			continue
		}
		failures = 0
		out = append(out, Program{
			ChannelID: ch.ID, Start: t, End: t.Add(d), Kind: it.Kind, ItemID: it.ID, SeriesID: it.SeriesID,
			Title: it.Title, Subtitle: it.Subtitle, FilePath: it.Path,
		})
		t = t.Add(d)
	}
	return out, nil
}

func channelShows(r Rules, lib Library) ([]int64, error) {
	seen := map[int64]bool{}
	var out []int64
	for _, s := range r.Series {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(r.Genres) > 0 || len(r.Decades) > 0 {
		more, err := lib.SeriesMatching(r.Genres, r.Decades)
		if err != nil {
			return nil, err
		}
		for _, s := range more {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	for _, sh := range r.Stream {
		if id := -sh.TMDBID; sh.TMDBID > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

func keepWith(shows []int64, eps map[int64][]Item) []int64 {
	out := shows[:0:0]
	for _, s := range shows {
		if len(eps[s]) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// nextItem picks what plays next.
func nextItem(order string, shows []int64, eps map[int64][]Item, movies []Item, movieByID map[int64]Item, st *State, rnd *rand.Rand, sinceMovie *int, maxPerDay int, at time.Time) (Item, bool) {
	// A movie now and then on a channel with both; only movies on a movie channel.
	if len(movies) > 0 {
		day := at.Format("2006-01-02")
		if st.MovieDay != day {
			st.MovieDay, st.MoviesDay = day, 0
		}
		wantMovie := len(shows) == 0 || (*sinceMovie >= max(len(shows), 3) && (maxPerDay == 0 || st.MoviesDay < maxPerDay))
		if wantMovie {
			if m, ok := nextMovie(movies, movieByID, st, rnd); ok {
				*sinceMovie = 0
				st.MoviesDay++
				return m, true
			}
		}
	}
	if len(shows) == 0 {
		return Item{}, false
	}
	*sinceMovie++
	var show int64
	switch order {
	case "shuffle":
		show = shows[rnd.Intn(len(shows))]
		list := eps[show]
		return list[rnd.Intn(len(list))], true
	case "inorder":
		if st.ShowIndex >= len(shows) || st.ShowIndex < 0 {
			st.ShowIndex = 0
		}
		show = shows[st.ShowIndex]
		i := st.Next[show]
		if i >= len(eps[show]) {
			// This show is done: the next one, from its start.
			st.Next[show] = 0
			st.ShowIndex = (st.ShowIndex + 1) % len(shows)
			show = shows[st.ShowIndex]
			i = st.Next[show] % len(eps[show])
		}
		st.Next[show] = i + 1
		return eps[show][i], true
	default: // rotate
		st.Round = keepIn(st.Round, eps)
		if len(st.Round) == 0 {
			st.Round = append([]int64(nil), shows...)
			rnd.Shuffle(len(st.Round), func(a, b int) { st.Round[a], st.Round[b] = st.Round[b], st.Round[a] })
		}
		show, st.Round = st.Round[0], st.Round[1:]
		list := eps[show]
		i := st.Next[show] % len(list)
		st.Next[show] = (i + 1) % len(list)
		return list[i], true
	}
}

func keepIn(round []int64, eps map[int64][]Item) []int64 {
	out := round[:0:0]
	for _, s := range round {
		if len(eps[s]) > 0 {
			out = append(out, s)
		}
	}
	return out
}

func nextMovie(movies []Item, byID map[int64]Item, st *State, rnd *rand.Rand) (Item, bool) {
	for tries := 0; tries < 2; tries++ {
		for len(st.MovieDeck) > 0 {
			id := st.MovieDeck[0]
			st.MovieDeck = st.MovieDeck[1:]
			if m, ok := byID[id]; ok {
				return m, true
			}
		}
		// A fresh shuffled deck: every movie once before any repeats.
		ids := make([]int64, len(movies))
		for i, m := range movies {
			ids[i] = m.ID
		}
		sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
		rnd.Shuffle(len(ids), func(a, b int) { ids[a], ids[b] = ids[b], ids[a] })
		st.MovieDeck = ids
	}
	return Item{}, false
}
