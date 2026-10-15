package api

// Home fork: Wrapped. A person's year in watching, from their Diary's dated
// entries: how much and when, the shows they couldn't stop, the films they
// rated best, and what their year was made of (genres, people, subjects).
// Running times the Diary doesn't know are counted as a typical film or
// episode, and the total says "about".

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
)

const (
	typicalFilm    = 105 // minutes, when a film's running time isn't known
	typicalEpisode = 42
)

type wrappedTitle struct {
	Kind     string `json:"kind"`
	TMDBID   int    `json:"tmdbId"`
	Title    string `json:"title"`
	Image    string `json:"image,omitempty"`
	Episodes int    `json:"episodes,omitempty"`
	Stars    int    `json:"stars,omitempty"`
	Day      string `json:"day,omitempty"`
	Times    int    `json:"times,omitempty"`
}

type wrappedCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type wrappedPayload struct {
	Year       int            `json:"year"`
	Years      []int          `json:"years"`
	Films      int            `json:"films"`      // film watches
	NewFilms   int            `json:"newFilms"`   // seen for the first time
	Rewatches  int            `json:"rewatches"`  // films seen again
	Episodes   int            `json:"episodes"`   // episodes
	Shows      int            `json:"shows"`      // different shows
	Minutes    int            `json:"minutes"`    // all of it
	About      bool           `json:"about"`      // some running times guessed
	Days       int            `json:"days"`       // days with something watched
	Streak     int            `json:"streak"`     // the most days in a row
	Months     []int          `json:"months"`     // minutes, January first
	Weekday    int            `json:"weekday"`    // the busiest, 0 = Sunday
	TopShows   []wrappedTitle `json:"topShows"`   // by episodes
	TopRated   []wrappedTitle `json:"topRated"`   // by stars
	MostSeen   *wrappedTitle  `json:"mostSeen"`   // the film watched most, when more than once
	First      *wrappedTitle  `json:"first"`      // the year's first film
	Last       *wrappedTitle  `json:"last"`       // and its latest
	AvgStars   float64        `json:"avgStars"`   // out of 10
	Genres     []wrappedCount `json:"genres"`     // what the year was made of
	People     []wrappedCount `json:"people"`     //
	Subjects   []wrappedCount `json:"subjects"`   //
	Empty      bool           `json:"empty"`      // nothing dated that year
	FromServer bool           `json:"fromServer"` // the Diary reads the media server
}

// GET /api/home/wrapped?year=: your year in watching (this year, or the
// latest with anything in it).
func (s *Server) handleWrapped(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := auth.UserFromContext(ctx)
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	all, err := s.diaryEntries(ctx, u.ID, ` AND dated = 1`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The Diary couldn't be read.")
		return
	}
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	out := s.wrapped(ctx, all, year)
	out.FromServer = s.hasMediaServerPerson(ctx, u)
	writeJSON(w, http.StatusOK, out)
}

// wrapped works a year out from a Diary's dated entries (newest first); year
// 0 is this year, or the latest with anything in it.
func (s *Server) wrapped(ctx context.Context, all []diaryEntry, year int) wrappedPayload {
	out := wrappedPayload{Years: []int{}, Months: make([]int, 12), TopShows: []wrappedTitle{}, TopRated: []wrappedTitle{}, Genres: []wrappedCount{}, People: []wrappedCount{}, Subjects: []wrappedCount{}}
	seenYear := map[int]bool{}
	for _, e := range all {
		if y, err := strconv.Atoi(e.WatchedOn[:4]); err == nil && !seenYear[y] {
			seenYear[y] = true
			out.Years = append(out.Years, y)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out.Years)))
	out.Year = year
	if out.Year == 0 {
		out.Year = time.Now().Year()
		if !seenYear[out.Year] && len(out.Years) > 0 {
			out.Year = out.Years[0]
		}
	}
	prefix := strconv.Itoa(out.Year) + "-"
	var picked []diaryEntry
	firstSeen := map[int]string{} // film -> the first day ever
	for _, e := range all {
		if e.Kind == "movie" {
			if d, ok := firstSeen[e.TMDBID]; !ok || e.WatchedOn < d {
				firstSeen[e.TMDBID] = e.WatchedOn
			}
		}
		if len(e.WatchedOn) == 10 && e.WatchedOn[:5] == prefix {
			picked = append(picked, e)
		}
	}
	if len(picked) == 0 {
		out.Empty = true
		return out
	}
	s.fillPosters(ctx, picked)

	days := map[string]bool{}
	weekdays := make([]int, 7)
	shows := map[int]*wrappedTitle{}
	films := map[int]*wrappedTitle{}
	var starSum, starN int
	weight := map[int]int{}                 // TMDB id (shows negative) -> how much of the year it was
	for i := len(picked) - 1; i >= 0; i-- { // oldest first
		e := picked[i]
		mins := e.Runtime
		if e.Kind == "movie" {
			out.Films++
			if mins == 0 {
				mins, out.About = typicalFilm, true
			}
			if firstSeen[e.TMDBID] == e.WatchedOn {
				out.NewFilms++
			} else {
				out.Rewatches++
			}
			t := films[e.TMDBID]
			if t == nil {
				t = &wrappedTitle{Kind: "movie", TMDBID: e.TMDBID, Title: e.Title, Image: e.Image}
				films[e.TMDBID] = t
			}
			t.Times++
			if e.Stars > t.Stars {
				t.Stars = e.Stars
			}
			if out.First == nil {
				out.First = &wrappedTitle{Kind: "movie", TMDBID: e.TMDBID, Title: e.Title, Image: e.Image, Day: e.WatchedOn}
			}
			out.Last = &wrappedTitle{Kind: "movie", TMDBID: e.TMDBID, Title: e.Title, Image: e.Image, Day: e.WatchedOn}
			weight[e.TMDBID]++
		} else {
			n := max(1, e.Episodes)
			out.Episodes += n
			if mins == 0 {
				mins, out.About = typicalEpisode*n, true
			}
			t := shows[e.TMDBID]
			if t == nil {
				t = &wrappedTitle{Kind: "tv", TMDBID: e.TMDBID, Title: e.Title, Image: e.Image}
				shows[e.TMDBID] = t
			}
			t.Episodes += n
			weight[-e.TMDBID] += n
		}
		if e.Stars > 0 {
			starSum += e.Stars
			starN++
		}
		out.Minutes += mins
		if m, err := strconv.Atoi(e.WatchedOn[5:7]); err == nil && m >= 1 && m <= 12 {
			out.Months[m-1] += mins
		}
		if !days[e.WatchedOn] {
			days[e.WatchedOn] = true
			if t, err := time.Parse("2006-01-02", e.WatchedOn); err == nil {
				weekdays[t.Weekday()]++
			}
		}
	}
	out.Shows, out.Days = len(shows), len(days)
	if starN > 0 {
		out.AvgStars = float64(starSum) / float64(starN)
	}
	for d := range weekdays {
		if weekdays[d] > weekdays[out.Weekday] {
			out.Weekday = d
		}
	}
	out.Streak = longestStreak(days)

	for _, t := range shows {
		out.TopShows = append(out.TopShows, *t)
	}
	sort.Slice(out.TopShows, func(i, j int) bool { return out.TopShows[i].Episodes > out.TopShows[j].Episodes })
	out.TopShows = out.TopShows[:min(5, len(out.TopShows))]
	var rated []wrappedTitle
	for _, t := range films {
		if t.Stars > 0 {
			rated = append(rated, *t)
		}
		if t.Times > 1 && (out.MostSeen == nil || t.Times > out.MostSeen.Times) {
			c := *t
			out.MostSeen = &c
		}
	}
	sort.Slice(rated, func(i, j int) bool { return rated[i].Stars > rated[j].Stars })
	out.TopRated = append(out.TopRated, rated[:min(5, len(rated))]...)

	// What the year was made of: the traits of what was watched, by how much.
	for _, kind := range []string{"movie", "tv"} {
		sign := 1
		if kind == "tv" {
			sign = -1
		}
		var ids []int
		for id := range weight {
			if id*sign > 0 {
				ids = append(ids, id*sign)
			}
		}
		traits := s.traitsFor(ctx, kind, ids, 8*time.Second)
		for id, t := range traits {
			n := weight[sign*id]
			if kind == "tv" {
				n = (n + 2) / 3 // a show's episodes count less than as many films
			}
			for _, k := range traitKeys(t) {
				switch k[0] {
				case 'g':
					addCount(&out.Genres, k[2:], n)
				case 'm', 'c':
					addCount(&out.People, k[2:], n)
				case 'k':
					addCount(&out.Subjects, k[2:], n)
				}
			}
		}
	}
	out.Genres, out.People, out.Subjects = topCounts(out.Genres, 5), topCounts(out.People, 6), topCounts(out.Subjects, 8)
	return out
}

func addCount(list *[]wrappedCount, name string, n int) {
	for i := range *list {
		if (*list)[i].Name == name {
			(*list)[i].Count += n
			return
		}
	}
	*list = append(*list, wrappedCount{name, n})
}

func topCounts(list []wrappedCount, n int) []wrappedCount {
	sort.Slice(list, func(i, j int) bool {
		return list[i].Count > list[j].Count || list[i].Count == list[j].Count && list[i].Name < list[j].Name
	})
	return list[:min(n, len(list))]
}

// longestStreak is the most days in a row with something watched.
func longestStreak(days map[string]bool) int {
	best := 0
	for d := range days {
		t, err := time.Parse("2006-01-02", d)
		if err != nil || days[t.AddDate(0, 0, -1).Format("2006-01-02")] {
			continue // not the start of a run
		}
		n := 0
		for days[t.AddDate(0, 0, n).Format("2006-01-02")] {
			n++
		}
		best = max(best, n)
	}
	return best
}
