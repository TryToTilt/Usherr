package api

// Usherr: what the Marquee shows besides films and lists (phase 3). A film's
// details for the info panel and the trivia (plot, cast, director, budget,
// studio), people in the spotlight with their films, tonight on Usherr TV,
// and theme music: a theme.mp3 in a film's or show's folder (where Jellyfin's
// Theme Songs plugin and others put them).

import (
	"context"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/qr"
)

type marqueePerson struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role,omitempty"` // the character, or the job
	Photo string `json:"photo,omitempty"`
}

type marqueeSpotFilm struct {
	TMDBID    int    `json:"tmdbId"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	PosterURL string `json:"posterUrl"`
	Have      bool   `json:"have"` // on the server
}

type marqueeSpotlight struct {
	marqueePerson
	Known string            `json:"known"` // "Directing" or "Acting"
	Bio   string            `json:"bio,omitempty"`
	Films []marqueeSpotFilm `json:"films"`
}

type marqueeTonightShow struct {
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Start     string `json:"start"`
	End       string `json:"end"`
	PosterURL string `json:"posterUrl,omitempty"`
}

type marqueeTonight struct {
	Number int                  `json:"number"`
	Name   string               `json:"name"`
	Logo   string               `json:"logo,omitempty"`
	Shows  []marqueeTonightShow `json:"shows"`
}

const (
	marqueeSpotlightPeople = 4
	marqueeSpotlightFilms  = 8
	marqueeTonightHours    = 5
	marqueeTonightShows    = 6
	marqueeTonightChannels = 16
)

// marqueeFilmDetails adds what the info panel and the trivia read to a film.
func (s *Server) marqueeFilmDetails(ctx context.Context, d *metadata.MovieDetail, f *marqueeFeature) {
	f.Overview = d.Overview
	f.Budget, f.Revenue = d.Budget, d.Revenue
	if d.OriginalTitle != "" && !strings.EqualFold(d.OriginalTitle, d.Title) {
		f.OriginalTitle = d.OriginalTitle
	}
	if len(d.ProductionCompanies) > 0 {
		f.Studio = d.ProductionCompanies[0].Name
	}
	if len(d.ProductionCountries) > 0 {
		f.Country = d.ProductionCountries[0].Name
	}
	if d.BelongsToCollection != nil {
		f.Collection = d.BelongsToCollection.Name
	}
	for _, c := range d.Credits.Cast {
		if len(f.Cast) >= 5 {
			break
		}
		f.Cast = append(f.Cast, marqueePerson{ID: c.ID, Name: c.Name, Role: c.Character, Photo: metadata.ProfileURL(c.ProfilePath)})
	}
	for _, c := range d.Credits.Crew {
		if c.Job == "Director" {
			f.Director = &marqueePerson{ID: c.ID, Name: c.Name, Role: "Director", Photo: metadata.ProfileURL(c.ProfilePath)}
			break
		}
	}
	if f.Director != nil {
		if p, err := s.displayTMDB().GetPerson(ctx, f.Director.ID); err == nil {
			for _, film := range personFilms(p, true) {
				if film.TMDBID != d.TMDBID && len(f.DirectorFilms) < 3 {
					f.DirectorFilms = append(f.DirectorFilms, film.Title)
				}
			}
		}
	}
}

// personFilms lists the films someone directed (or acted in), the best known
// first, with a poster each.
func personFilms(p *metadata.PersonDetail, directed bool) []marqueeSpotFilm {
	var credits []metadata.PersonCredit
	if directed {
		for _, c := range p.CombinedCredits.Crew {
			if c.Job == "Director" {
				credits = append(credits, c)
			}
		}
	} else {
		credits = append(credits, p.CombinedCredits.Cast...)
	}
	sort.SliceStable(credits, func(i, j int) bool { return credits[i].VoteCount > credits[j].VoteCount })
	out := []marqueeSpotFilm{}
	seen := map[int]bool{}
	for _, c := range credits {
		if c.MediaType != "movie" || c.Adult || c.PosterPath == "" || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		out = append(out, marqueeSpotFilm{TMDBID: c.ID, Title: c.Title, Year: dateYear(c.ReleaseDate), PosterURL: metadata.ImageURL("w342", c.PosterPath)})
	}
	return out
}

// marqueeShowArt is a show's poster, backdrop and leads, for an episode playing.
func (s *Server) marqueeShowArt(ctx context.Context, tmdbID int) (poster, backdrop string, cast []marqueePerson) {
	d, err := s.displayTMDB().GetShowFull(ctx, tmdbID)
	if err != nil {
		return "", "", nil
	}
	for _, c := range d.Credits.Cast {
		if len(cast) >= 5 {
			break
		}
		cast = append(cast, marqueePerson{ID: c.ID, Name: c.Name, Role: c.Character, Photo: metadata.ProfileURL(c.ProfilePath)})
	}
	return metadata.ImageURL("w780", d.PosterPath), metadata.BackdropURL(d.BackdropPath), cast
}

// People in the spotlight, kept for hours: their films hardly change.
var marqueeSpots = struct {
	sync.Mutex
	m map[int]marqueeSpotEntry
}{m: map[int]marqueeSpotEntry{}}

type marqueeSpotEntry struct {
	at time.Time
	p  marqueeSpotlight
}

func (s *Server) marqueeSpotlight(ctx context.Context, person marqueePerson, directed bool) (marqueeSpotlight, bool) {
	marqueeSpots.Lock()
	e, ok := marqueeSpots.m[person.ID]
	marqueeSpots.Unlock()
	if ok && time.Since(e.at) < marqueeDetailCache {
		return e.p, len(e.p.Films) >= 3
	}
	p, err := s.displayTMDB().GetPerson(ctx, person.ID)
	if err != nil {
		slog.Debug("marquee: spotlight", "person", person.ID, "err", err)
		return marqueeSpotlight{}, false
	}
	sp := marqueeSpotlight{marqueePerson: marqueePerson{ID: p.ID, Name: p.Name, Photo: metadata.ProfileLargeURL(p.ProfilePath)}, Known: "Acting", Bio: shortBio(p.Biography)}
	if directed {
		sp.Known = "Directing"
	}
	films := personFilms(p, directed)
	if len(films) > marqueeSpotlightFilms {
		films = films[:marqueeSpotlightFilms]
	}
	for i := range films {
		if m, have, err := s.MovieRepo.GetByTMDBID(films[i].TMDBID); err == nil && have && m.Status == library.StatusDownloaded {
			films[i].Have = true
		}
	}
	sp.Films = films
	marqueeSpots.Lock()
	marqueeSpots.m[person.ID] = marqueeSpotEntry{at: time.Now(), p: sp}
	marqueeSpots.Unlock()
	return sp, sp.Photo != "" && len(sp.Films) >= 3
}

// shortBio is the first sentences of a biography, up to about 260 letters.
func shortBio(bio string) string {
	bio = strings.TrimSpace(strings.Split(strings.TrimSpace(bio), "\n")[0])
	if len([]rune(bio)) <= 260 {
		return bio
	}
	out := ""
	for _, sentence := range strings.SplitAfter(bio, ". ") {
		if len([]rune(out+sentence)) > 260 {
			break
		}
		out += sentence
	}
	if out == "" {
		return string([]rune(bio)[:257]) + "…"
	}
	return strings.TrimSpace(out)
}

// marqueeTonightOn lists what the Live TV channels have on for the next hours.
func (s *Server) marqueeTonightOn() []marqueeTonight {
	out := []marqueeTonight{}
	repo := s.tvRepo()
	chans, err := repo.Channels()
	if err != nil {
		return out
	}
	now := time.Now()
	posters := map[string]string{}
	poster := func(kind string, id int64) string {
		key := kind + strconv.FormatInt(id, 10)
		if v, ok := posters[key]; ok {
			return v
		}
		v := ""
		if kind == "series" {
			if sr, err := s.MovieRepo.GetSeries(id); err == nil {
				v = metadata.ImageURL("w342", sr.PosterPath)
			}
		} else if m, err := s.MovieRepo.Get(id); err == nil {
			v = metadata.ImageURL("w342", m.PosterPath)
		}
		posters[key] = v
		return v
	}
	for _, ch := range s.appChannels(chans) {
		if !ch.Enabled || len(out) >= marqueeTonightChannels {
			continue
		}
		list, err := repo.Programs(ch.ID, now, now.Add(marqueeTonightHours*time.Hour))
		if err != nil {
			continue
		}
		list = shows(list)
		if len(list) == 0 {
			continue
		}
		e := marqueeTonight{Number: ch.Number, Name: ch.Name, Logo: ch.LogoURL, Shows: []marqueeTonightShow{}}
		for _, p := range list {
			if len(e.Shows) >= marqueeTonightShows {
				break
			}
			sh := marqueeTonightShow{Title: p.Title, Subtitle: p.Subtitle, Start: p.Start.Format(time.RFC3339), End: p.End.Format(time.RFC3339)}
			switch {
			case p.SeriesID > 0:
				sh.PosterURL = poster("series", p.SeriesID)
			case p.Kind == "movie" && p.ItemID > 0:
				sh.PosterURL = poster("movie", p.ItemID)
			}
			e.Shows = append(e.Shows, sh)
		}
		out = append(out, e)
	}
	return out
}

// marqueeMore fills in phase 3's parts of the Marquee's answer: only what the
// switched-on scenes and settings need.
func (s *Server) marqueeMore(ctx context.Context, set marqueeSettings, out *marqueePayload) {
	on := map[string]bool{}
	for _, sc := range set.Scenes {
		on[sc.Kind] = sc.On
	}
	if on["tonight"] || set.Style == "board" {
		out.Tonight = s.marqueeTonightOn()
	}
	if on["spotlight"] && s.TMDB().HasAPIKey() {
		seen := map[int]bool{}
		add := func(p *marqueePerson, directed bool) {
			if p == nil || seen[p.ID] || len(out.Spotlights) >= marqueeSpotlightPeople {
				return
			}
			seen[p.ID] = true
			if sp, ok := s.marqueeSpotlight(ctx, *p, directed); ok {
				out.Spotlights = append(out.Spotlights, sp)
			}
		}
		// The directors of the featured films first, then their leads.
		for _, f := range out.Features {
			add(f.Director, true)
		}
		for _, f := range out.Features {
			if len(f.Cast) > 0 {
				add(&f.Cast[0], false)
			}
		}
	}
	out.Media = s.marqueeMediaList(ctx, true)
	if set.ThemeMusic {
		for i, f := range out.Features {
			if f.Reason == "new" {
				if m, ok, err := s.MovieRepo.GetByTMDBID(f.TMDBID); err == nil && ok && s.marqueeThemeFile("movie", m.ID) != "" {
					out.Features[i].ThemeURL = "/api/home/marquee/theme/movie/" + strconv.FormatInt(m.ID, 10)
				}
			}
		}
		for _, a := range out.Arrivals {
			if a.Kind == "movie" {
				if s.marqueeThemeFile("movie", a.ID) != "" {
					out.Themes["movie:"+strconv.Itoa(a.TMDBID)] = "/api/home/marquee/theme/movie/" + strconv.FormatInt(a.ID, 10)
				}
			} else if a.SeriesID > 0 && s.marqueeThemeFile("tv", a.SeriesID) != "" {
				out.Themes["tv:"+strconv.FormatInt(a.SeriesID, 10)] = "/api/home/marquee/theme/tv/" + strconv.FormatInt(a.SeriesID, 10)
			}
		}
	}
}

var marqueeThemeNames = []string{"theme.mp3", "theme.m4a", "theme.flac", "theme.ogg", "theme.opus"}

// marqueeThemeFile finds a film's or show's theme music in its folder:
// theme.mp3 (and the like), or the first song in a theme-music folder.
func (s *Server) marqueeThemeFile(kind string, id int64) string {
	var dir string
	if kind == "movie" {
		m, err := s.MovieRepo.Get(id)
		if err != nil {
			return ""
		}
		if m.FilePath != "" {
			dir = filepath.Dir(m.FilePath)
		} else {
			dir = s.movieFolderFor(m)
		}
	} else {
		f, _, err := s.seriesFolder(id)
		if err != nil {
			return ""
		}
		dir = f.Dir
	}
	if dir == "" {
		return ""
	}
	for _, n := range marqueeThemeNames {
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "theme-music"))
	if err != nil {
		return ""
	}
	for _, e := range entries {
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".mp3", ".m4a", ".flac", ".ogg", ".opus":
			if e.Type().IsRegular() {
				return filepath.Join(dir, "theme-music", e.Name())
			}
		}
	}
	return ""
}

// GET /api/home/marquee/theme/{kind}/{id} (member): a film's or show's theme music.
func (s *Server) handleMarqueeTheme(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || (kind != "movie" && kind != "tv") {
		http.NotFound(w, r)
		return
	}
	p := s.marqueeThemeFile(kind, id)
	if p == "" {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ct := mime.TypeByExtension(filepath.Ext(p)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// GET /api/home/marquee/qr?text= (member): a QR code's rows ("0" and "1"),
// for a trailer's or a title's link on a screen.
func (s *Server) handleMarqueeQR(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if text == "" || len(text) > 200 || (!strings.HasPrefix(text, "https://") && !strings.HasPrefix(text, "http://")) {
		writeError(w, http.StatusBadRequest, "That link can't be made into a code.")
		return
	}
	code, err := qr.Encode([]byte(text))
	if err != nil {
		writeError(w, http.StatusBadRequest, "That link can't be made into a code.")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	writeJSON(w, http.StatusOK, map[string]any{"rows": code.Rows()})
}
