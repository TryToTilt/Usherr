package api

// Usherr: the Marquee, a lobby display for a tablet next to the TV (the
// /marquee page). One answer carries everything it rotates through: what's
// playing on the media server now, the films to feature (with their
// trailers), what arrived lately, what's in cinemas and the next week.

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	marqueeFeatures    = 12 // films featured with a big poster (and a trailer)
	marqueeArrivals    = 12
	marqueeTheaters    = 12
	marqueeComingDays  = 7
	marqueeDetailCache = 12 * time.Hour
)

type marqueeFeature struct {
	TMDBID      int      `json:"tmdbId"`
	Title       string   `json:"title"`
	Year        int      `json:"year,omitempty"`
	Tagline     string   `json:"tagline,omitempty"`
	Runtime     int      `json:"runtime,omitempty"`
	Rating      float64  `json:"rating,omitempty"`
	Rated       string   `json:"rated,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	PosterURL   string   `json:"posterUrl,omitempty"`
	BackdropURL string   `json:"backdropUrl,omitempty"`
	Trailer     string   `json:"trailer,omitempty"` // a YouTube video key
	Reason      string   `json:"reason"`            // "new" (arrived lately), "theaters" or "coming" (opens soon)
	ReleaseDate string   `json:"releaseDate,omitempty"`
}

type marqueePlaying struct {
	mediaservers.Playing
	PosterURL   string `json:"posterUrl,omitempty"`
	BackdropURL string `json:"backdropUrl,omitempty"`
}

type marqueePayload struct {
	NowPlaying []marqueePlaying       `json:"nowPlaying"`
	Features   []marqueeFeature       `json:"features"`
	Arrivals   []dashRecent           `json:"arrivals"`
	Theaters   []marqueeFeature       `json:"theaters"`
	Coming     []calendarEntryPayload `json:"coming"`
	Theatre    string                 `json:"theatre,omitempty"`
	Settings   marqueeSettings        `json:"settings"`
	TVNow      []marqueeTV            `json:"tvNow"` // Usherr: what the Live TV channels have on
}

// marqueeTV is a Live TV channel and what it has on now and next.
type marqueeTV struct {
	Number   int     `json:"number"`
	Name     string  `json:"name"`
	Logo     string  `json:"logo,omitempty"`
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle,omitempty"`
	Progress float64 `json:"progress"`
	Next     string  `json:"next,omitempty"`
	NextAt   string  `json:"nextAt,omitempty"`
}

// marqueeTVNow lists the enabled Live TV channels with what's on.
func (s *Server) marqueeTVNow() []marqueeTV {
	out := []marqueeTV{}
	repo := s.tvRepo()
	chans, err := repo.Channels()
	if err != nil {
		return out
	}
	now := time.Now()
	for _, ch := range s.appChannels(chans) {
		if !ch.Enabled {
			continue
		}
		list, err := repo.Programs(ch.ID, now, now.Add(6*time.Hour))
		if err != nil {
			continue
		}
		list = shows(list)
		if len(list) == 0 {
			continue
		}
		e := marqueeTV{Number: ch.Number, Name: ch.Name, Logo: ch.LogoURL}
		cur := list[0]
		if !cur.Start.After(now) {
			e.Title, e.Subtitle = cur.Title, cur.Subtitle
			e.Progress = float64(now.Sub(cur.Start)) / float64(cur.End.Sub(cur.Start))
			if len(list) > 1 {
				e.Next, e.NextAt = list[1].Title, list[1].Start.Format(time.RFC3339)
			}
		} else {
			e.Next, e.NextAt = cur.Title, cur.Start.Format(time.RFC3339)
		}
		out = append(out, e)
	}
	return out
}

// A film's details for the marquee, kept for a while: the marquee asks every
// few minutes, TMDB needn't be asked as often.
var marqueeDetails = struct {
	sync.Mutex
	m map[int]marqueeDetailEntry
}{m: map[int]marqueeDetailEntry{}}

type marqueeDetailEntry struct {
	at time.Time
	f  marqueeFeature
}

func (s *Server) marqueeFeature(ctx context.Context, tmdbID int, reason string) (marqueeFeature, bool) {
	marqueeDetails.Lock()
	e, ok := marqueeDetails.m[tmdbID]
	marqueeDetails.Unlock()
	if ok && time.Since(e.at) < marqueeDetailCache {
		e.f.Reason = reason
		return e.f, true
	}
	d, err := s.displayTMDB().GetMovieDetail(ctx, tmdbID)
	if err != nil {
		slog.Debug("marquee: film details", "tmdbId", tmdbID, "err", err)
		return marqueeFeature{}, false
	}
	f := marqueeFeature{TMDBID: d.TMDBID, Title: d.Title, Year: d.Year(), Tagline: d.Tagline, Runtime: d.Runtime,
		Rating: metadata.RoundRating(d.VoteAverage), Rated: d.Certification(), Genres: s.displayTMDB().MovieGenres(ctx, d.Movie),
		PosterURL: metadata.ImageURL("w780", d.PosterPath), BackdropURL: metadata.BackdropURL(d.BackdropPath), ReleaseDate: d.ReleaseDate}
	for _, t := range d.Trailers() {
		if t.Site == "YouTube" && t.Key != "" {
			f.Trailer = t.Key
			break
		}
	}
	marqueeDetails.Lock()
	marqueeDetails.m[tmdbID] = marqueeDetailEntry{at: time.Now(), f: f}
	marqueeDetails.Unlock()
	f.Reason = reason
	return f, true
}

// GET /api/home/marquee
func (s *Server) handleMarquee(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := marqueePayload{NowPlaying: []marqueePlaying{}, Features: []marqueeFeature{}, Arrivals: []dashRecent{}, Theaters: []marqueeFeature{}, Coming: []calendarEntryPayload{}, TVNow: []marqueeTV{}}
	out.Theatre, _ = s.Settings.Get(keyCinemaTheatre)
	tmdbOK := s.TMDB().HasAPIKey()

	set := s.marqueeSettings()
	out.Settings = set

	// Now showing: what the media servers are playing.
	if list, err := s.MediaServers.List(); set.FromJellyfin && err == nil {
		for _, m := range list {
			if !m.Enabled {
				continue
			}
			ask, cancel := context.WithTimeout(ctx, 6*time.Second)
			playing, err := s.mediaClient.NowPlaying(ask, m)
			cancel()
			if err != nil {
				continue
			}
			for _, p := range playing {
				mp := marqueePlaying{Playing: p}
				if p.Kind == "movie" && p.TMDBID > 0 && tmdbOK {
					if f, ok := s.marqueeFeature(ctx, p.TMDBID, "playing"); ok {
						mp.PosterURL, mp.BackdropURL = f.PosterURL, f.BackdropURL
					}
				}
				out.NowPlaying = append(out.NowPlaying, mp)
			}
		}
	}

	// And what StreamNZB is streaming (Nuvio, Stremio...).
	if set.FromStream {
		wanted := map[string]bool{}
		for _, p := range set.Profiles {
			wanted[p] = true
		}
		for _, p := range s.streamingNow(ctx) {
			if len(wanted) == 0 || wanted[p.Device] {
				out.NowPlaying = append(out.NowPlaying, p)
			}
		}
	}
	if r.URL.Query().Get("only") == "playing" {
		writeJSON(w, http.StatusOK, out)
		return
	}

	// Just arrived: the newest titles that are on the server now.
	var newFilms []int
	if recent, err := s.MovieRepo.RecentlyAdded(recentScan); err == nil {
		var arrived []dashRecent
		for _, it := range recent {
			if it.State != "downloaded" && it.State != "partial" {
				continue
			}
			d := dashRecent{Kind: it.Kind, ID: it.ID, Title: it.Title, Year: it.Year, PosterURL: metadata.ImageURL("w780", it.PosterPath), At: it.AddedAt, State: it.State}
			if it.Kind == "movie" {
				d.TMDBID = it.TMDBID
			} else {
				d.SeriesID = it.ID
			}
			arrived = append(arrived, d)
		}
		for _, d := range s.latestArrivals(arrived) {
			if len(out.Arrivals) >= marqueeArrivals {
				break
			}
			if d.PosterURL == "" {
				continue
			}
			out.Arrivals = append(out.Arrivals, d)
			if d.Kind == "movie" && d.TMDBID > 0 {
				newFilms = append(newFilms, d.TMDBID)
			}
		}
	}

	// In cinemas now, and opening soon.
	nowShowing, upcoming := s.cinemaFilms(ctx)
	if !s.cinemaOn() {
		nowShowing, upcoming = nil, nil
	}

	// Features: new arrivals first, then cinema films, a few of each.
	if tmdbOK {
		seen := map[int]bool{}
		add := func(id int, reason string) {
			if len(out.Features) >= marqueeFeatures || seen[id] || id <= 0 {
				return
			}
			seen[id] = true
			if f, ok := s.marqueeFeature(ctx, id, reason); ok && f.PosterURL != "" {
				out.Features = append(out.Features, f)
			}
		}
		for i, id := range newFilms {
			if i < 5 {
				add(id, "new")
			}
		}
		// The cinema lists carry the country's own date; a film's details carry
		// its first release anywhere, so the list's date is kept.
		addDated := func(m metadata.Movie, reason string) {
			n := len(out.Features)
			add(m.TMDBID, reason)
			if len(out.Features) > n {
				out.Features[n].ReleaseDate = m.ReleaseDate
			}
		}
		for i, m := range nowShowing {
			if i < 5 {
				addDated(m, "theaters")
			}
		}
		for i, m := range upcoming {
			if i < 3 {
				addDated(m, "coming")
			}
		}
	}
	for _, m := range nowShowing {
		if len(out.Theaters) >= marqueeTheaters {
			break
		}
		if m.PosterPath == "" {
			continue
		}
		out.Theaters = append(out.Theaters, marqueeFeature{TMDBID: m.TMDBID, Title: m.Title, Year: yearOf(m.ReleaseDate),
			PosterURL: metadata.ImageURL("w500", m.PosterPath), Reason: "theaters", ReleaseDate: m.ReleaseDate})
	}

	// The next week: the library's releases and episodes, and other modules'.
	from := time.Now().Format("2006-01-02")
	to := time.Now().AddDate(0, 0, marqueeComingDays).Format("2006-01-02")
	var all []calendarEntryPayload
	if e, err := s.calendarEntries(); err == nil {
		all = append(all, e...)
	}
	if e, err := s.moduleCalendarEntries(); err == nil {
		all = append(all, e...)
	}
	for _, m := range upcoming {
		if m.ReleaseDate != "" {
			all = append(all, calendarEntryPayload{Kind: "movie", ID: -int64(m.TMDBID), TMDBID: m.TMDBID, Title: m.Title,
				Subtitle: "In theaters", ReleaseDate: m.ReleaseDate, Status: "theaters", PosterURL: metadata.PosterURL(m.PosterPath)})
		}
	}
	for _, e := range all {
		if e.ReleaseDate >= from && e.ReleaseDate <= to {
			out.Coming = append(out.Coming, e)
		}
	}
	sort.SliceStable(out.Coming, func(i, j int) bool { return out.Coming[i].ReleaseDate < out.Coming[j].ReleaseDate })
	out.TVNow = s.marqueeTVNow() // Usherr
	writeJSON(w, http.StatusOK, out)
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y := 0
	for _, c := range date[:4] {
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}
