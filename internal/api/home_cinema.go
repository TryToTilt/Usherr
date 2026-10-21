package api

// Usherr: films in cinemas. The "In theaters now" row (Dashboard and Library),
// the cinema openings on the calendar, and the household's own cinema: its ZIP
// code and the theater it goes to, with links out to buy tickets and to see
// where else a film is playing.
//
// Today the list is TMDB's "now playing" for the household's country. When a
// theater chain's showtimes are wired in (AMC first, with the household's own
// key, home.cinema.amc_key), the list narrows to what that theater shows.

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	keyCinemaOn         = "home.cinema.on"          // "0" hides the cinema rows and calendar openings
	keyCinemaRegion     = "home.cinema.region"      // the country whose cinemas count, as TMDB's two-letter code (default "US")
	keyCinemaZip        = "home.cinema.zip"         // the household's ZIP or postcode, for "where else is it playing"
	keyCinemaTheatre    = "home.cinema.theatre"     // the theater the household goes to, by name
	keyCinemaAMCKey     = "home.cinema.amc_key"     // the household's own AMC vendor key (kept encrypted), for showtimes
	keyCinemaTickets    = "home.cinema.tickets"     // Usherr: where Tickets goes: see cinemaTicketServices ("" is Fandango in the US)
	keyCinemaTicketsURL = "home.cinema.tickets_url" // Usherr: a link of your own, with {title} and {zip}, for "custom"
	cinemaPages         = 2                         // pages of TMDB's lists (20 films each)
	cinemaCacheFor      = 6 * time.Hour
)

var (
	cinemaRegionRe = regexp.MustCompile(`^[A-Z]{2}$`)
	cinemaZipRe    = regexp.MustCompile(`^[A-Za-z0-9 -]{3,10}$`)
)

type cinemaCache struct {
	mu       sync.Mutex
	at       time.Time
	region   string
	now      []metadata.Movie
	upcoming []metadata.Movie
}

var cinemaLists cinemaCache

func (s *Server) cinemaOn() bool {
	v, _ := s.Settings.Get(keyCinemaOn)
	return v != "0"
}

func (s *Server) cinemaRegion() string {
	v, _ := s.Settings.Get(keyCinemaRegion)
	if v = strings.ToUpper(strings.TrimSpace(v)); cinemaRegionRe.MatchString(v) {
		return v
	}
	return "US"
}

// cinemaFilms is what's in the region's cinemas now and what opens soon,
// fetched at most every few hours.
func (s *Server) cinemaFilms(ctx context.Context) (now, upcoming []metadata.Movie) {
	region := s.cinemaRegion()
	c := &cinemaLists
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.region == region && time.Since(c.at) < cinemaCacheFor && c.now != nil {
		return c.now, c.upcoming
	}
	if !s.TMDB().HasAPIKey() {
		return nil, nil
	}
	tm := s.displayTMDB()
	collect := func(get func(context.Context, string, int) (*metadata.MoviePage, error)) ([]metadata.Movie, bool) {
		var out []metadata.Movie
		seen := map[int]bool{}
		for page := 1; page <= cinemaPages; page++ {
			p, err := get(ctx, region, page)
			if err != nil {
				return out, page > 1
			}
			for _, m := range p.Results {
				if !seen[m.TMDBID] {
					seen[m.TMDBID] = true
					out = append(out, m)
				}
			}
			if page >= p.TotalPages {
				break
			}
		}
		return out, true
	}
	nowList, ok1 := collect(tm.NowPlaying)
	upList, ok2 := collect(tm.UpcomingInCinemas)
	if !ok1 || !ok2 {
		return nowList, upList // not cached: try again next time
	}
	today := time.Now().Format("2006-01-02")
	// TMDB's "upcoming" overlaps "now playing" by a few days; a film counts as
	// upcoming only while its date is still ahead.
	ahead := upList[:0]
	for _, m := range upList {
		if m.ReleaseDate > today {
			ahead = append(ahead, m)
		}
	}
	// In cinemas: newest first, as a cinema's own listing reads.
	sort.SliceStable(nowList, func(i, j int) bool { return nowList[i].ReleaseDate > nowList[j].ReleaseDate })
	c.at, c.region, c.now, c.upcoming = time.Now(), region, nowList, ahead
	return c.now, c.upcoming
}

// GET /api/home/cinema: the films in cinemas now, for the "In theaters now"
// row, with the household's cinema settings the links need.
func (s *Server) handleCinema(w http.ResponseWriter, r *http.Request) {
	zip, _ := s.Settings.Get(keyCinemaZip)
	theatre, _ := s.Settings.Get(keyCinemaTheatre)
	out := map[string]any{"on": s.cinemaOn(), "region": s.cinemaRegion(), "zip": zip, "theatre": theatre, "source": "tmdb", "results": []browseItem{}}
	out["logos"] = s.linkLogos()
	out["tickets"], out["ticketsUrl"] = s.cinemaTickets()
	if !s.cinemaOn() {
		writeJSON(w, http.StatusOK, out)
		return
	}
	now, _ := s.cinemaFilms(r.Context())
	page := s.moviePagePayload(r.Context(), &metadata.MoviePage{Page: 1, TotalPages: 1, TotalResults: len(now), Results: now})
	out["results"] = page.Results
	writeJSON(w, http.StatusOK, out)
}

// GET /api/home/cinema/settings (admin).
func (s *Server) handleGetCinemaSettings(w http.ResponseWriter, r *http.Request) {
	zip, _ := s.Settings.Get(keyCinemaZip)
	theatre, _ := s.Settings.Get(keyCinemaTheatre)
	key, _ := s.Settings.Get(keyCinemaAMCKey)
	tickets, ticketsURL := s.cinemaTickets()
	writeJSON(w, http.StatusOK, map[string]any{"on": s.cinemaOn(), "region": s.cinemaRegion(), "zip": zip, "theatre": theatre, "hasAmcKey": key != "", "logos": s.linkLogos(), "tickets": tickets, "ticketsUrl": ticketsURL})
}

// PUT /api/home/cinema/settings {on, region, zip, theatre, amcKey} (admin):
// only what's sent changes; an empty amcKey removes the key.
func (s *Server) handlePutCinemaSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On         *bool   `json:"on"`
		Region     *string `json:"region"`
		Zip        *string `json:"zip"`
		Theatre    *string `json:"theatre"`
		AMCKey     *string `json:"amcKey"`
		Tickets    *string `json:"tickets"`    // Usherr
		TicketsURL *string `json:"ticketsUrl"` // Usherr
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.Region != nil {
		v := strings.ToUpper(strings.TrimSpace(*req.Region))
		if v != "" && !cinemaRegionRe.MatchString(v) {
			writeError(w, http.StatusBadRequest, "The country is a two-letter code, such as US or GB.")
			return
		}
		_ = s.Settings.Set(keyCinemaRegion, v, false)
	}
	if req.Zip != nil {
		v := strings.TrimSpace(*req.Zip)
		if v != "" && !cinemaZipRe.MatchString(v) {
			writeError(w, http.StatusBadRequest, "That doesn't look like a ZIP or postcode.")
			return
		}
		_ = s.Settings.Set(keyCinemaZip, v, false)
	}
	if req.Tickets != nil {
		v := strings.TrimSpace(*req.Tickets)
		if !cinemaTicketServices[v] {
			writeError(w, http.StatusBadRequest, "That isn't a ticket service Usherr knows.")
			return
		}
		_ = s.Settings.Set(keyCinemaTickets, v, false)
	}
	if req.TicketsURL != nil {
		v := strings.TrimSpace(*req.TicketsURL)
		if v != "" && (len(v) > 300 || !(strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://")) || strings.ContainsAny(v, " \t\r\n\"'<>")) {
			writeError(w, http.StatusBadRequest, "Give the full address, starting with https://, with {title} where the film's name goes.")
			return
		}
		_ = s.Settings.Set(keyCinemaTicketsURL, v, false)
	}
	if req.Theatre != nil {
		v := strings.TrimSpace(*req.Theatre)
		if len(v) > 120 {
			v = v[:120]
		}
		_ = s.Settings.Set(keyCinemaTheatre, v, false)
	}
	if req.AMCKey != nil {
		v := strings.TrimSpace(*req.AMCKey)
		if len(v) > 100 {
			writeError(w, http.StatusBadRequest, "That doesn't look like an AMC key.")
			return
		}
		_ = s.Settings.Set(keyCinemaAMCKey, v, v != "")
	}
	if req.On != nil {
		v := "1"
		if !*req.On {
			v = "0"
		}
		_ = s.Settings.Set(keyCinemaOn, v, false)
	}
	cinemaLists.mu.Lock()
	cinemaLists.at = time.Time{} // the country may have changed
	cinemaLists.mu.Unlock()
	s.handleGetCinemaSettings(w, r)
}

// cinemaCalendarEntries are the cinema openings on the calendar: films opening
// soon, and the ones in cinemas now (on the day they opened, so they stay
// there while they're showing). A film already on the calendar for the
// library keeps its own entry as well: the dates differ.
func (s *Server) cinemaCalendarEntries(ctx context.Context) []calendarEntryPayload {
	if !s.cinemaOn() {
		return nil
	}
	now, upcoming := s.cinemaFilms(ctx)
	out := make([]calendarEntryPayload, 0, len(now)+len(upcoming))
	add := func(m metadata.Movie, subtitle string) {
		if m.ReleaseDate == "" {
			return
		}
		poster := metadata.PosterURL(m.PosterPath)
		out = append(out, calendarEntryPayload{Kind: "movie", ID: -int64(m.TMDBID), TMDBID: m.TMDBID, Title: m.Title,
			Subtitle: subtitle, ReleaseDate: m.ReleaseDate, Status: "theaters", PosterURL: poster})
	}
	for _, m := range now {
		add(m, "In theaters now")
	}
	for _, m := range upcoming {
		add(m, "In theaters")
	}
	return out
}

// Usherr: a picture of the household's own on each ticket service's button
// (its logo, say), uploaded in Settings > Extras > In theaters and kept in
// the config folder (link-logos/fandango.png and so on). Nothing is shipped: without
// it the app draws a plain button with the service's name.
var linkLogoTypes = map[string]string{".png": "image/png", ".webp": "image/webp", ".jpg": "image/jpeg"}

// linkLogos says which services have a picture of the household's own.
func (s *Server) linkLogos() map[string]bool {
	out := map[string]bool{}
	for name := range cinemaTicketServices {
		if name != "" && name != "off" && s.linkLogoFile(name) != "" {
			out[name] = true
		}
	}
	return out
}

func (s *Server) linkLogoFile(name string) string {
	if name == "" || name == "off" || !cinemaTicketServices[name] {
		return ""
	}
	for ext := range linkLogoTypes {
		p := filepath.Join(s.cfg.ConfigDir, "link-logos", name+ext)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Size() < 2<<20 {
			return p
		}
	}
	return ""
}

// GET /api/home/link-logo/{name} (member): that logo, when there is one.
func (s *Server) handleLinkLogo(w http.ResponseWriter, r *http.Request) {
	p := s.linkLogoFile(r.PathValue("name"))
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
	w.Header().Set("Content-Type", linkLogoTypes[filepath.Ext(p)])
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// PUT /api/home/link-logo/{name} (admin, multipart "file"): a picture of the
// household's own for that link's button (PNG, JPG or WebP, 2 MB at most).
func (s *Server) handlePutLinkLogo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || name == "off" || !cinemaTicketServices[name] {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20+64<<10)
	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Choose a picture of 2 MB or less.")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 2<<20+1))
	if err != nil || len(data) > 2<<20 {
		writeError(w, http.StatusBadRequest, "Choose a picture of 2 MB or less.")
		return
	}
	ext := ""
	switch http.DetectContentType(data) {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	default:
		writeError(w, http.StatusBadRequest, "That isn't a PNG, JPG or WebP picture.")
		return
	}
	dir := filepath.Join(s.cfg.ConfigDir, "link-logos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "The picture couldn't be saved.")
		return
	}
	s.removeLinkLogo(name)
	if err := os.WriteFile(filepath.Join(dir, name+ext), data, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "The picture couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /api/home/link-logo/{name} (admin): back to the plain button.
func (s *Server) handleDeleteLinkLogo(w http.ResponseWriter, r *http.Request) {
	if name := r.PathValue("name"); name == "" || name == "off" || !cinemaTicketServices[name] {
		http.NotFound(w, r)
		return
	}
	s.removeLinkLogo(r.PathValue("name"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) removeLinkLogo(name string) {
	for ext := range linkLogoTypes {
		_ = os.Remove(filepath.Join(s.cfg.ConfigDir, "link-logos", name+ext))
	}
}

// The ticket services Tickets can open ("custom" is a link of your own, "off"
// shows none). Each one's address is built in the app (cinema.ts).
var cinemaTicketServices = map[string]bool{"": true, "fandango": true, "atom": true, "cinemark": true, "moviefone": true, "showtimes": true, "custom": true, "off": true}

// cinemaTickets is the ticket service chosen, and the custom link.
func (s *Server) cinemaTickets() (string, string) {
	v, _ := s.Settings.Get(keyCinemaTickets)
	u, _ := s.Settings.Get(keyCinemaTicketsURL)
	if !cinemaTicketServices[v] {
		v = ""
	}
	return v, u
}
