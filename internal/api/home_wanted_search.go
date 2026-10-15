package api

// Home fork: the wanted list showed every episode of a show the show's
// newest search ("Searched for S02E13: …" on S01E01). Each episode now gets
// the newest search that was for it, its season, or the whole show.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/queue"
)

var (
	searchedEpisode = regexp.MustCompile(`(?i)\bfor S(\d+)E(\d+)\b`)
	searchedSeason  = regexp.MustCompile(`(?i)\bfor season (\d+)\b`)
)

// episodeSearch returns the newest "searched" event (events newest first)
// that covers season/episode.
func episodeSearch(events []queue.Event, season, episode int) (string, string) {
	for _, e := range events {
		if e.Kind != "searched" {
			continue
		}
		if m := searchedEpisode.FindStringSubmatch(e.Message); m != nil {
			s, _ := strconv.Atoi(m[1])
			ep, _ := strconv.Atoi(m[2])
			if s == season && ep == episode {
				return e.Message, e.At
			}
			continue
		}
		if m := searchedSeason.FindStringSubmatch(e.Message); m != nil {
			if s, _ := strconv.Atoi(m[1]); s == season {
				return e.Message, e.At
			}
			continue
		}
		if strings.Contains(e.Message, fmt.Sprintf("S%02d", season)) {
			return e.Message, e.At
		}
		return e.Message, e.At // a search for the whole show
	}
	return "", ""
}
