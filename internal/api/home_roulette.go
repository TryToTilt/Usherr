package api

// Home fork: Roulette. One random pick for tonight, weighted towards the
// better rated: from what the chosen people can watch and haven't seen
// ("here"), or from well-rated titles the house doesn't have ("anywhere").
// It can keep to a mood, a running time and the family filter's age. The
// answer has the pick and a few others for the reel that spins before it.

import (
	"context"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	rouletteLook = 14 // candidates looked at for one spin
	rouletteReel = 6  // others shown spinning past
)

// moodTMDBGenres are TMDB's genre ids for each mood, for "anywhere".
var moodTMDBGenres = map[string]map[string]string{
	"movie": {"funny": "35", "cozy": "10751|16|10749", "exciting": "28|12|53", "scary": "27", "thoughtful": "18|99|878", "family": "10751|16", "mystery": "9648|80"},
	"tv":    {"funny": "35", "cozy": "10751|16", "exciting": "10759", "scary": "9648", "thoughtful": "18|99|10765", "family": "10751|10762", "mystery": "9648|80"},
}

type roulettePayload struct {
	Pick    *browseItem  `json:"pick"`
	Reel    []browseItem `json:"reel"`
	Runtime int          `json:"runtime,omitempty"` // minutes, films
	Pool    int          `json:"pool"`              // how many it chose from
	Note    string       `json:"note,omitempty"`
}

// GET /api/home/roulette?kind=movie|tv&source=here|anywhere&who=a,b&mood=&minutes=&age=
func (s *Server) handleRoulette(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind != "tv" {
		kind = "movie"
	}
	mood := q.Get("mood")
	minutes, _ := strconv.Atoi(q.Get("minutes"))
	age, ageErr := strconv.Atoi(q.Get("age"))
	if ageErr != nil {
		age = -1 // no family filter
	}
	ctx := r.Context()
	rnd := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), rand.Uint64()))
	if !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusBadRequest, "Mediarium needs its TMDB key for this.")
		return
	}

	var pool []roomTitle
	if q.Get("source") == "anywhere" {
		pool = s.rouletteAnywhere(ctx, kind, mood, rnd)
	} else {
		var people []string
		for _, p := range strings.Split(q.Get("who"), ",") {
			if p = strings.TrimSpace(p); p != "" {
				people = append(people, p)
			}
		}
		if len(people) == 0 {
			writeError(w, http.StatusBadRequest, "Choose who's watching.")
			return
		}
		list, err := s.roomCandidates(ctx, people, kind, mood)
		if err != nil {
			writeUpstreamError(w, "read what's in the media server", err)
			return
		}
		pool = list
	}
	// Home fork: the taste engine. Never what someone here gave a thumbs
	// down; what fits their taste comes up more often.
	var tastes []*tasteProfile
	if q.Get("source") == "anywhere" {
		tastes = []*tasteProfile{s.myTaste(ctx, kind)}
	} else {
		tastes = s.roomTastes(ctx, strings.Split(q.Get("who"), ","), kind)
	}
	pool, matches := s.tasteFilter(ctx, kind, pool, tastes)
	out := roulettePayload{Pool: len(pool), Reel: []browseItem{}}
	if len(pool) == 0 {
		out.Note = "Nothing fits. Try another mood, or Anything."
		writeJSON(w, http.StatusOK, out)
		return
	}

	// Better rated more often, but anything can come up.
	sample := tasteSample(pool, matches, rouletteLook, rnd)
	ok := map[int]bool{}
	if age >= 0 {
		items := make([]familyItem, len(sample))
		for i, t := range sample {
			items[i] = familyItem{ID: t.TMDB, Title: t.Name, Year: t.Year, Genres: t.Genres}
		}
		fk := "movie"
		if kind == "tv" {
			fk = "tv"
		}
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		s.workOutFamily(wctx, fk, items)
		cancel()
		for id, a := range s.keptFamilyAges(ctx, fk, idsOf(items)) {
			ok[id] = a >= 0 && a <= age
		}
	}

	tm := s.displayTMDB()
	var pick *browseItem
	var reelIDs []int
	for _, t := range sample {
		if age >= 0 && !ok[t.TMDB] {
			continue
		}
		if pick == nil {
			if kind == "movie" {
				d, err := tm.GetMovieDetail(ctx, t.TMDB)
				if err != nil {
					continue
				}
				if minutes > 0 && (d.Runtime == 0 || d.Runtime > minutes) {
					continue
				}
				out.Runtime = d.Runtime
				if m, err := tm.GetMovie(ctx, t.TMDB); err == nil {
					if p := s.moviePagePayload(ctx, &metadata.MoviePage{Page: 1, TotalPages: 1, Results: []metadata.Movie{*m}}).Results; len(p) > 0 {
						pick = &p[0]
					}
				}
			} else if d, err := tm.GetShow(ctx, t.TMDB); err == nil {
				if p := s.showPagePayload(ctx, &metadata.ShowPage{Page: 1, TotalPages: 1, Results: []metadata.Show{d.Show}}).Results; len(p) > 0 {
					pick = &p[0]
				}
			}
			continue
		}
		if len(reelIDs) < rouletteReel {
			reelIDs = append(reelIDs, t.TMDB)
		}
	}
	if pick == nil {
		out.Note = "Nothing fits. Try a longer time, another mood, or Anything."
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Pick = pick
	out.Reel = s.rouletteReel(ctx, kind, reelIDs)
	writeJSON(w, http.StatusOK, out)
}

// rouletteReel is the posters that spin past before the pick.
func (s *Server) rouletteReel(ctx context.Context, kind string, ids []int) []browseItem {
	tm := s.displayTMDB()
	if kind == "tv" {
		var shows []metadata.Show
		for _, id := range ids {
			if d, err := tm.GetShow(ctx, id); err == nil {
				shows = append(shows, d.Show)
			}
		}
		return s.showPagePayload(ctx, &metadata.ShowPage{Page: 1, TotalPages: 1, Results: shows}).Results
	}
	var movies []metadata.Movie
	for _, id := range ids {
		if m, err := tm.GetMovie(ctx, id); err == nil {
			movies = append(movies, *m)
		}
	}
	return s.moviePagePayload(ctx, &metadata.MoviePage{Page: 1, TotalPages: 1, Results: movies}).Results
}

// rouletteAnywhere is well-rated titles the house doesn't have and hasn't
// set aside: a random page of TMDB's, in the mood when one is chosen.
func (s *Server) rouletteAnywhere(ctx context.Context, kind, mood string, rnd *rand.Rand) []roomTitle {
	v := url.Values{"include_adult": {"false"}, "sort_by": {"popularity.desc"}, "vote_average.gte": {"7"}, "vote_count.gte": {"800"}}
	if g := moodTMDBGenres[kind][mood]; g != "" {
		v.Set("with_genres", g)
	}
	h := s.have(ctx)
	sets := h.Movies
	if kind == "tv" {
		sets = h.Shows
	}
	had := map[int]bool{}
	for _, l := range [][]int{sets.Disk, sets.Streamed, sets.Wanted} {
		for _, id := range l {
			had[id] = true
		}
	}
	excluded := s.excludedIDs(ctx, kind)
	var out []roomTitle
	keep := func(id int, name string, year int, rating float64, genres []int) {
		if had[id] || excluded[id] {
			return
		}
		out = append(out, roomTitle{TMDB: id, Name: name, Year: year, Rating: rating})
	}
	// Three random pages: most of one page is often already in the house.
	pages := rnd.Perm(15)[:3]
	for _, pg := range pages {
		v.Set("page", strconv.Itoa(pg+1))
		if kind == "tv" {
			if p, err := s.displayTMDB().DiscoverTVWith(ctx, v); err == nil {
				for _, sh := range p.Results {
					keep(sh.TMDBID, sh.Name, sh.Year(), sh.VoteAverage, sh.GenreIDs)
				}
			}
			continue
		}
		if p, err := s.displayTMDB().DiscoverMoviesWith(ctx, v); err == nil {
			for _, m := range p.Results {
				keep(m.TMDBID, m.Title, m.Year(), m.VoteAverage, m.GenreIDs)
			}
		}
	}
	return out
}

// excludedIDs is the household's Not interested list for movies or shows.
func (s *Server) excludedIDs(ctx context.Context, kind string) map[int]bool {
	out := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id FROM exclusions WHERE kind = ?`, kind)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}
