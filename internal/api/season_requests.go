package api

// Usherr: asking for some seasons of a show, as in Seerr. A new show can be
// added (or asked for) with only some seasons monitored, and a show that is
// already here can be asked for more of them: those seasons are switched on
// and searched for.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/library"
)

// cleanSeasons keeps real season numbers (specials, 0, included), once each, sorted.
func cleanSeasons(in []int) []int {
	out := []int{}
	for _, n := range in {
		if n >= 0 && n < 1000 && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

func seasonList(in []int) string {
	parts := []string{}
	for _, n := range cleanSeasons(in) {
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ",")
}

// payloadSeasons is the seasons an add request asks for.
func payloadSeasons(body []byte) []int {
	var v struct {
		Seasons []int `json:"seasons"`
	}
	_ = json.Unmarshal(body, &v)
	return cleanSeasons(v.Seasons)
}

// seasonNumbers is the seasons a show has in the library.
func (s *Server) seasonNumbers(seriesID int64) []int {
	eps, err := s.MovieRepo.ListEpisodes(seriesID)
	if err != nil {
		return nil
	}
	out := []int{}
	for _, e := range eps {
		if !slices.Contains(out, e.Season) {
			out = append(out, e.Season)
		}
	}
	slices.Sort(out)
	return out
}

// monitorOnlySeasons monitors the seasons asked for and no others.
func (s *Server) monitorOnlySeasons(seriesID int64, seasons []int) {
	want := cleanSeasons(seasons)
	for _, n := range s.seasonNumbers(seriesID) {
		_ = s.MovieRepo.SetSeasonMonitored(seriesID, n, slices.Contains(want, n))
	}
	_ = s.MovieRepo.SetSeriesMonitored(seriesID, true)
}

// seasonsNotWanted is those of seasons whose episodes aren't all monitored or
// downloaded yet: what asking for them would still change.
func (s *Server) seasonsNotWanted(tmdbID int, seasons []int) []int {
	sr, ok, err := s.MovieRepo.GetSeriesByTMDBID(tmdbID)
	if err != nil || !ok {
		return cleanSeasons(seasons)
	}
	eps, err := s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		return cleanSeasons(seasons)
	}
	out := []int{}
	for _, n := range cleanSeasons(seasons) {
		have := 0
		open := false
		for _, e := range eps {
			if e.Season != n {
				continue
			}
			have++
			if !e.Monitored && e.Status != library.StatusDownloaded {
				open = true
			}
		}
		if have == 0 || open {
			out = append(out, n)
		}
	}
	return out
}

// addMoreSeasons switches seasons of a show that is here on and searches them.
func (s *Server) addMoreSeasons(w http.ResponseWriter, r *http.Request, sr library.Series, seasons []int) {
	want := cleanSeasons(seasons)
	have := s.seasonNumbers(sr.ID)
	var on []string
	for _, n := range want {
		if slices.Contains(have, n) {
			if err := s.MovieRepo.SetSeasonMonitored(sr.ID, n, true); err == nil {
				on = append(on, strconv.Itoa(n))
			}
		}
	}
	if len(on) == 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("%s has none of those seasons yet.", sr.Title))
		return
	}
	_ = s.MovieRepo.SetSeriesMonitored(sr.ID, true)
	_, byline := requester(r)
	_ = s.QueueRepo.LogSeriesActivity(sr.ID, "added", fmt.Sprintf("Season %s of %s switched on%s", strings.Join(on, ", "), sr.Title, byline))
	go s.searchSeriesInBackground(sr.ID)
	sr, _ = s.MovieRepo.GetSeries(sr.ID)
	out := toSeriesPayload(sr, s.accountNames())
	out.RootPath = folderShown(r, s.tvFolders(), sr.RootPath)
	writeJSON(w, http.StatusOK, out)
}
