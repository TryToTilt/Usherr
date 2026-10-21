package api

// Usherr: the next episode, for the player's "Up next" card. From a library
// episode it's the next one in the library's list; from a streamed episode
// it's the next in the library when the show is there, else TMDB's. Only an
// episode that has aired is offered; one with a file plays that file, the
// rest stream.

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

type nextEpisode struct {
	TMDBID    int    `json:"tmdbId"` // the show's
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	Title     string `json:"title,omitempty"` // the episode's
	Show      string `json:"show"`
	EpisodeID int64  `json:"episodeId,omitempty"` // a library episode with a file: played from it
}

// aired is whether an episode's air date ("YYYY-MM-DD") has come.
func aired(date string) bool {
	return date != "" && date <= time.Now().Format("2006-01-02")
}

// nextInLibrary is the episode after season/episode in a library show.
func (s *Server) nextInLibrary(sr library.Series, season, episode int) (nextEpisode, bool) {
	eps, err := s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		return nextEpisode{}, false
	}
	sort.Slice(eps, func(i, j int) bool {
		if eps[i].Season != eps[j].Season {
			return eps[i].Season < eps[j].Season
		}
		return eps[i].Episode < eps[j].Episode
	})
	for _, e := range eps {
		if e.Season == 0 || e.Season < season || (e.Season == season && e.Episode <= episode) {
			continue // specials, and what came before
		}
		n := nextEpisode{TMDBID: sr.TMDBID, Season: e.Season, Episode: e.Episode, Title: e.Title, Show: sr.Title}
		if e.FilePath != "" {
			n.EpisodeID = e.ID
			return n, true
		}
		if aired(e.AirDate) {
			return n, true
		}
		return nextEpisode{}, false // the next one hasn't aired yet
	}
	return nextEpisode{}, false
}

// nextOnTMDB is the episode after season/episode, from TMDB's seasons.
func (s *Server) nextOnTMDB(ctx context.Context, tmdbID, season, episode int) (nextEpisode, bool) {
	show, err := s.displayTMDB().GetShow(ctx, tmdbID)
	if err != nil {
		return nextEpisode{}, false
	}
	seasons := []int{}
	for _, se := range show.Seasons {
		if se.SeasonNumber >= season && se.SeasonNumber > 0 {
			seasons = append(seasons, se.SeasonNumber)
		}
	}
	sort.Ints(seasons)
	for _, sn := range seasons {
		eps, err := s.displayTMDB().GetSeason(ctx, tmdbID, sn)
		if err != nil {
			return nextEpisode{}, false
		}
		sort.Slice(eps, func(i, j int) bool { return eps[i].Episode < eps[j].Episode })
		for _, e := range eps {
			if sn == season && e.Episode <= episode {
				continue
			}
			if !aired(e.AirDate) {
				return nextEpisode{}, false
			}
			return nextEpisode{TMDBID: tmdbID, Season: sn, Episode: e.Episode, Title: e.Name, Show: show.Name}, true
		}
	}
	return nextEpisode{}, false
}

// GET /api/play/next?episodeId=N, or ?tmdbId=&season=&episode= for a
// streamed episode: the next one, or {"next": null}.
func (s *Server) handlePlayNext(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var (
		next nextEpisode
		ok   bool
	)
	if id, err := strconv.ParseInt(q.Get("episodeId"), 10, 64); err == nil && id > 0 {
		ep, err := s.MovieRepo.GetEpisodeByID(id)
		if err == nil {
			if sr, err := s.MovieRepo.GetSeries(ep.SeriesID); err == nil {
				next, ok = s.nextInLibrary(sr, ep.Season, ep.Episode)
			}
		}
	} else {
		tmdbID, _ := strconv.Atoi(q.Get("tmdbId"))
		season, _ := strconv.Atoi(q.Get("season"))
		episode, _ := strconv.Atoi(q.Get("episode"))
		if tmdbID <= 0 || season <= 0 || episode <= 0 {
			writeError(w, http.StatusBadRequest, "Say which episode is playing.")
			return
		}
		if sr, found, err := s.MovieRepo.GetSeriesByTMDBID(tmdbID); err == nil && found {
			next, ok = s.nextInLibrary(sr, season, episode)
		}
		if !ok && s.TMDB().HasAPIKey() {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			next, ok = s.nextOnTMDB(ctx, tmdbID, season, episode)
			cancel()
		}
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"next": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"next": next})
}
