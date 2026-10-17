package library

import "sort"

// Daily shows (talk shows, news) are named by the day an episode aired
// ("The Daily Show - 2024-01-15 - Guest.mkv"), not by season and episode.
// These place such files on the show's episode that aired that day.

// episodeOnDate is the episode of a show that aired on date: the only one,
// or the part-th of several that day (part counts from 1). ok is false when
// none did, or when several did and the name doesn't say which.
func episodeOnDate(byDate map[string][]Episode, date string, part int) (Episode, bool) {
	eps := byDate[date]
	switch {
	case len(eps) == 1 && part <= 1:
		return eps[0], true
	case part >= 1 && part <= len(eps):
		return eps[part-1], true
	}
	return Episode{}, false
}

// episodesByDate groups a show's regular episodes (not the specials) by air
// date, each day's in episode order.
func episodesByDate(episodes []Episode) map[string][]Episode {
	byDate := map[string][]Episode{}
	for _, e := range episodes {
		if e.Season > 0 && e.AirDate != "" {
			byDate[e.AirDate] = append(byDate[e.AirDate], e)
		}
	}
	for _, eps := range byDate {
		sort.Slice(eps, func(i, j int) bool {
			if eps[i].Season != eps[j].Season {
				return eps[i].Season < eps[j].Season
			}
			return eps[i].Episode < eps[j].Episode
		})
	}
	return byDate
}

// PlaceByAirDate gives the files named by an air date the season and
// episode of the show's episode that aired that day. A file whose day the
// show doesn't list (or lists several episodes for, without the name saying
// which part it is) is given season -1, which no rule places, so the import
// counts it as left out instead of guessing. Other files are returned as
// they are.
func PlaceByAirDate(files []ImportFile, episodes []Episode) []ImportFile {
	var byDate map[string][]Episode
	out := make([]ImportFile, len(files))
	copy(out, files)
	for i, f := range out {
		if f.AirDate == "" || len(f.Episodes) > 0 {
			continue
		}
		if byDate == nil {
			byDate = episodesByDate(episodes)
		}
		if e, ok := episodeOnDate(byDate, f.AirDate, f.DailyPart); ok {
			out[i].Season, out[i].Episodes = e.Season, []int{e.Episode}
		} else {
			out[i].Season, out[i].Episodes = -1, []int{0}
		}
	}
	return out
}
