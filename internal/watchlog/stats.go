package watchlog

import (
	"sort"
	"strconv"
	"time"
)

// Stats are the numbers for one period.
type Stats struct {
	Days      int         `json:"days"`
	Plays     int         `json:"plays"`
	Hours     float64     `json:"hours"`
	People    int         `json:"people"`
	Titles    int         `json:"titles"`
	Finished  int         `json:"finished"`
	PerDay    []DayHours  `json:"perDay"`
	Users     []Count     `json:"users"`
	Movies    []Count     `json:"movies"`
	Shows     []Count     `json:"shows"`
	Clients   []Count     `json:"clients"`
	Devices   []Count     `json:"devices"`
	Methods   Methods     `json:"methods"`
	Reasons   []Count     `json:"reasons"`
	HourOfDay [24]float64 `json:"hourOfDay"` // hours watched by the hour they started in
	DayOfWeek [7]float64  `json:"dayOfWeek"` // Monday first
	Locations []Count     `json:"locations"`
}

// DayHours is the hours watched on one day.
type DayHours struct {
	Day   string  `json:"day"` // 2026-11-04
	Hours float64 `json:"hours"`
}

// Count is one line of a top list.
type Count struct {
	Name      string  `json:"name"`
	Plays     int     `json:"plays"`
	Hours     float64 `json:"hours"`
	TMDBID    int     `json:"tmdbId,omitempty"`
	LibraryID int64   `json:"libraryId,omitempty"`
	Episodes  int     `json:"episodes,omitempty"` // a show: how many different episodes
}

// Methods is how many playbacks played directly, direct streamed and
// transcoded.
type Methods struct {
	Direct    int `json:"direct"`
	Stream    int `json:"stream"`
	Transcode int `json:"transcode"`
}

const topN = 10

func round1(h float64) float64 { return float64(int(h*10+0.5)) / 10 }

// Sum works out the numbers for list, the playbacks of the last days days up
// to now (in loc, for the days and hours).
func Sum(list []Session, days int, now time.Time, loc *time.Location) Stats {
	st := Stats{Days: days, PerDay: []DayHours{}, Users: []Count{}, Movies: []Count{}, Shows: []Count{}, Clients: []Count{},
		Devices: []Count{}, Reasons: []Count{}, Locations: []Count{}}
	perDay := map[string]float64{}
	users, movies, shows, clients, devices, reasons, places := map[string]*Count{}, map[string]*Count{}, map[string]*Count{}, map[string]*Count{}, map[string]*Count{}, map[string]*Count{}, map[string]*Count{}
	episodes := map[string]map[string]bool{}
	titles := map[string]bool{}
	add := func(m map[string]*Count, key, name string, h float64) *Count {
		c := m[key]
		if c == nil {
			c = &Count{Name: name}
			m[key] = c
		}
		c.Plays++
		c.Hours += h
		return c
	}
	for _, s := range list {
		h := float64(s.WatchedSec) / 3600
		st.Plays++
		st.Hours += h
		if s.Completed {
			st.Finished++
		}
		start := s.StartedAt.In(loc)
		perDay[start.Format("2006-01-02")] += h
		st.HourOfDay[start.Hour()] += h
		st.DayOfWeek[(int(start.Weekday())+6)%7] += h
		add(users, s.UserName, s.UserName, h)
		switch s.Kind {
		case "movie":
			key := "m" + strconv.Itoa(s.TMDBID) + "|" + s.Title
			if s.TMDBID > 0 {
				key = "m" + strconv.Itoa(s.TMDBID)
			}
			c := add(movies, key, s.Title, h)
			c.TMDBID, c.LibraryID = s.TMDBID, max(c.LibraryID, s.LibraryID)
			titles[key] = true
		case "episode":
			key := "s|" + s.Series
			if s.TMDBID > 0 {
				key = "s" + strconv.Itoa(s.TMDBID)
			}
			c := add(shows, key, s.Series, h)
			c.TMDBID, c.LibraryID = s.TMDBID, max(c.LibraryID, s.LibraryID)
			if episodes[key] == nil {
				episodes[key] = map[string]bool{}
			}
			episodes[key][strconv.Itoa(s.Season)+"x"+strconv.Itoa(s.Episode)+s.Title] = true
			c.Episodes = len(episodes[key])
			titles[key] = true
		default:
			titles["o|"+s.Title] = true
		}
		if s.Client != "" {
			add(clients, s.Client, s.Client, h)
		}
		if s.Device != "" {
			add(devices, s.Device, s.Device, h)
		}
		switch PlayMethodName(s.PlayMethod) {
		case "direct":
			st.Methods.Direct++
		case "stream":
			st.Methods.Stream++
		case "transcode":
			st.Methods.Transcode++
		}
		for _, r := range s.TranscodeReasons {
			add(reasons, r, r, h)
		}
		if s.Location != "" {
			add(places, s.Location, s.Location, h)
		}
	}
	st.People, st.Titles = len(users), len(titles)
	st.Hours = round1(st.Hours)
	for i := range st.HourOfDay {
		st.HourOfDay[i] = round1(st.HourOfDay[i])
	}
	for i := range st.DayOfWeek {
		st.DayOfWeek[i] = round1(st.DayOfWeek[i])
	}
	if days > 0 {
		first := now.In(loc).AddDate(0, 0, -(days - 1))
		for d := 0; d < days; d++ {
			day := first.AddDate(0, 0, d).Format("2006-01-02")
			st.PerDay = append(st.PerDay, DayHours{Day: day, Hours: round1(perDay[day])})
		}
	}
	st.Users = top(users, 0)
	st.Movies, st.Shows = top(movies, topN), top(shows, topN)
	st.Clients, st.Devices = top(clients, topN), top(devices, topN)
	st.Reasons, st.Locations = top(reasons, topN), top(places, 0)
	return st
}

// top is the counts with the most hours first (then the most plays), at most n
// (0: all).
func top(m map[string]*Count, n int) []Count {
	out := make([]Count, 0, len(m))
	for _, c := range m {
		c.Hours = round1(c.Hours)
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hours != out[j].Hours {
			return out[i].Hours > out[j].Hours
		}
		if out[i].Plays != out[j].Plays {
			return out[i].Plays > out[j].Plays
		}
		return out[i].Name < out[j].Name
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
