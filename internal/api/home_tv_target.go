package api

// Home fork: which season and episodes a queued TV download is for, read the
// way grabTV reads it at grab time: through the show's own numbering. Without
// it, the start of a download read "[Group] Show Season 3 - 01 ... S03E01" as
// season 3 of a show TMDB numbers as one long season (S01E49), found no such
// episode and failed with "Those episodes are already in your library",
// and the next automatic search grabbed it again, over and over.

import (
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/parser"
)

func (s *Server) queuedTVTarget(series library.Series, releaseTitle string, hintSeason, hintEpisode int) (int, []int) {
	season, episodes := resolveTVTarget(releaseTitle, hintSeason, hintEpisode)
	raw := parser.Parse(releaseTitle)
	if rel := s.releaseMapper(series)(raw); raw.Season == 0 && !raw.Special {
		if (rel.Season > 0 || rel.Special) && len(rel.Episodes) > 0 {
			season, episodes = rel.Season, rel.Episodes
		}
	} else if rel.Season != raw.Season || !equalInts(rel.Episodes, raw.Episodes) {
		season, episodes = rel.Season, rel.Episodes
	}
	// The episode the grab was made for, when the name still leads nowhere.
	if hintEpisode > 0 {
		if _, err := s.tvGrabTargets(series.ID, season, episodes); err != nil {
			return hintSeason, []int{hintEpisode}
		}
	}
	return season, episodes
}
