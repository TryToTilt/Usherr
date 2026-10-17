package api

// Home fork: the dashboard counts the whole library, streamed layer
// included: titles that stream with nothing on disk add to the movie and
// show totals, and to the quality breakdown at the quality of the release
// they were collected with. The counts change only when titles are
// imported or downloaded, so they're worked out at most every two minutes.

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

type streamedCounts struct {
	movies, shows, episodes int
	tiers                   map[string]int
	// Usherr: anime (in the library and streamed) and comics.
	animeShows, animeEpisodes, animeOnDisk, animeStreams int
	animeLibShows, animeLibEpisodes, animeLibOnDisk      int // the library's part, taken off the TV counts
	animeStreamShows                                     int // streamed anime shows not in the library
	comicSeries, comicIssues, comicDownloaded, comicMiss int
}

var streamedCountsCache struct {
	sync.Mutex
	at         time.Time
	counts     streamedCounts
	refreshing bool
}

func (s *Server) addStreamedToDashboard(ctx context.Context, out *dashboardPayload) {
	streamedCountsCache.Lock()
	if streamedCountsCache.at.IsZero() {
		// The first time, count now; after that, old counts are shown while
		// new ones are worked out in the background.
		if c, err := s.countStreamed(ctx); err == nil {
			streamedCountsCache.counts, streamedCountsCache.at = c, time.Now()
		}
	} else if time.Since(streamedCountsCache.at) > 2*time.Minute && !streamedCountsCache.refreshing {
		streamedCountsCache.refreshing = true
		go func() {
			c, err := s.countStreamed(context.Background())
			streamedCountsCache.Lock()
			if err == nil {
				streamedCountsCache.counts, streamedCountsCache.at = c, time.Now()
			}
			streamedCountsCache.refreshing = false
			streamedCountsCache.Unlock()
		}()
	}
	c := streamedCountsCache.counts
	streamedCountsCache.Unlock()

	out.Library.Movies.Total += c.movies
	out.Library.Movies.Streams = c.movies
	out.Library.Series.Total += c.shows
	out.Library.Series.Streams = c.shows
	out.Library.Series.Episodes += c.episodes
	out.Library.Series.EpisodesStreams = c.episodes
	// Usherr: anime has a card of its own, so TV leaves it out.
	out.Library.Series.Total -= c.animeLibShows + c.animeStreamShows
	out.Library.Series.Streams -= c.animeStreamShows
	out.Library.Series.Episodes -= c.animeLibEpisodes + c.animeStreams
	out.Library.Series.EpisodesDownloaded -= c.animeLibOnDisk
	out.Library.Series.EpisodesStreams -= c.animeStreams
	out.Library.Series.EpisodesMissing -= c.animeLibEpisodes - c.animeLibOnDisk
	out.Library.Anime.Total = c.animeShows
	out.Library.Anime.Episodes = c.animeEpisodes
	out.Library.Anime.EpisodesDownloaded = c.animeOnDisk
	out.Library.Anime.EpisodesStreams = c.animeStreams
	out.Library.Comics.Series = c.comicSeries
	out.Library.Comics.Issues = c.comicIssues
	out.Library.Comics.Downloaded = c.comicDownloaded
	out.Library.Comics.Missing = c.comicMiss
	if len(c.tiers) == 0 {
		return
	}
	tiers := make(map[string]int, len(c.tiers))
	for t, n := range c.tiers {
		tiers[t] = n
	}
	for i := range out.Library.Qualities {
		if n, ok := tiers[out.Library.Qualities[i].Tier]; ok {
			out.Library.Qualities[i].Count += n
			delete(tiers, out.Library.Qualities[i].Tier)
		}
	}
	for t, n := range tiers {
		out.Library.Qualities = append(out.Library.Qualities, dashQuality{Tier: t, Count: n})
	}
	sort.Slice(out.Library.Qualities, func(i, j int) bool {
		if out.Library.Qualities[i].Count != out.Library.Qualities[j].Count {
			return out.Library.Qualities[i].Count > out.Library.Qualities[j].Count
		}
		return out.Library.Qualities[i].Tier < out.Library.Qualities[j].Tier
	})
}

// countStreamed counts what streams with nothing on disk: movies and shows
// not in the library, episodes not downloaded, and their qualities.
func (s *Server) countStreamed(ctx context.Context) (streamedCounts, error) {
	c := streamedCounts{tiers: map[string]int{}}
	movieIn, movieOnDisk := map[int]bool{}, map[int]bool{}
	if rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id, status = 'downloaded' FROM movies`); err == nil {
		for rows.Next() {
			var t int
			var on bool
			if rows.Scan(&t, &on) == nil {
				movieIn[t] = true
				movieOnDisk[t] = movieOnDisk[t] || on
			}
		}
		rows.Close()
	}
	showIn := map[int]bool{}
	if rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id FROM series`); err == nil {
		for rows.Next() {
			var t int
			if rows.Scan(&t) == nil {
				showIn[t] = true
			}
		}
		rows.Close()
	}
	epOnDisk := map[[3]int]bool{}
	if rows, err := s.db.QueryContext(ctx, `SELECT sr.tmdb_id, e.season, e.episode FROM episodes e JOIN series sr ON sr.id = e.series_id WHERE e.status = 'downloaded'`); err == nil {
		for rows.Next() {
			var k [3]int
			if rows.Scan(&k[0], &k[1], &k[2]) == nil {
				epOnDisk[k] = true
			}
		}
		rows.Close()
	}
	// Usherr: the anime shows in the library, and comics.
	animeLib := map[int]bool{} // tmdb ids
	if ids, err := s.animeSeriesList(ctx); err == nil && len(ids) > 0 {
		set := map[int64]bool{}
		for _, id := range ids {
			set[id] = true
		}
		// Counted as SeriesCounts counts them: no copies, specials only when wanted or downloaded.
		if rows, err := s.db.QueryContext(ctx, `SELECT sr.id, sr.tmdb_id,
			COALESCE(SUM(CASE WHEN e.season > 0 OR e.monitored = 1 OR e.status = 'downloaded' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN e.status = 'downloaded' THEN 1 ELSE 0 END), 0)
			FROM series sr LEFT JOIN episodes e ON e.series_id = sr.id WHERE sr.copy_of IS NULL GROUP BY sr.id`); err == nil {
			for rows.Next() {
				var id int64
				var tmdb, eps, onDisk int
				if rows.Scan(&id, &tmdb, &eps, &onDisk) == nil && set[id] {
					animeLib[tmdb] = true
					c.animeLibShows++
					c.animeLibEpisodes += eps
					c.animeLibOnDisk += onDisk
				}
			}
			rows.Close()
		}
	}
	c.animeShows, c.animeEpisodes, c.animeOnDisk = c.animeLibShows, c.animeLibEpisodes, c.animeLibOnDisk
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM comic_series`).Scan(&c.comicSeries)
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(status = 'downloaded'), 0), COALESCE(SUM(wanted = 1 AND status != 'downloaded'), 0) FROM comic_issues`).Scan(&c.comicIssues, &c.comicDownloaded, &c.comicMiss)

	rows, err := s.db.QueryContext(ctx, `SELECT t.kind, t.tmdb_id, t.anime, f.season, f.episode, f.release
		FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	shows := map[int]bool{}
	for rows.Next() {
		var kind, release string
		var tmdb, anime, season, episode int
		if rows.Scan(&kind, &tmdb, &anime, &season, &episode, &release) != nil {
			continue
		}
		if kind == "movie" {
			if movieOnDisk[tmdb] {
				continue
			}
			if !movieIn[tmdb] {
				c.movies++
			}
		} else {
			if epOnDisk[[3]int{tmdb, season, episode}] {
				continue
			}
			c.episodes++
			isAnime := anime == 1 || animeLib[tmdb]
			if isAnime {
				c.animeStreams++
				c.animeEpisodes++
			}
			if !showIn[tmdb] && !shows[tmdb] {
				shows[tmdb] = true
				c.shows++
				if isAnime {
					c.animeStreamShows++
					c.animeShows++
				}
			}
		}
		if t := releaseTier(release); t != quality.TierUnknown {
			c.tiers[string(t)]++
		}
	}
	return c, rows.Err()
}

var releaseTiers sync.Map // release name -> quality.Tier

// releaseTier is the quality a release name says, worked out once per name.
func releaseTier(release string) quality.Tier {
	if v, ok := releaseTiers.Load(release); ok {
		return v.(quality.Tier)
	}
	t := quality.Classify(parser.Parse(release))
	releaseTiers.Store(release, t)
	return t
}
