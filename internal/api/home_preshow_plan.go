package api

// Usherr: the pre-show for one film (home_preshow.go has the clips, themes
// and schedules). The plan is a list of steps Usherr's player plays one
// after another: a card it draws, a clip from the pre-show folder, a
// coming-soon trailer. Clips and trailers that played lately wait their turn.
// While it plays, the player tells the Marquee screens each step, and they
// follow (the trailer's poster, "Feature presentation"...).

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/settings"
)

const preshowComingDays = 60 // a trailer is "coming soon" this long before it opens

type preshowStep struct {
	Kind        string            `json:"kind"` // card, clip or trailer
	Card        string            `json:"card,omitempty"`
	URL         string            `json:"url,omitempty"`     // a clip
	YouTube     string            `json:"youtube,omitempty"` // a trailer
	Title       string            `json:"title,omitempty"`
	TMDBID      int               `json:"tmdbId,omitempty"`
	PosterURL   string            `json:"posterUrl,omitempty"`
	BackdropURL string            `json:"backdropUrl,omitempty"`
	ReleaseDate string            `json:"releaseDate,omitempty"`
	Category    string            `json:"category,omitempty"`
	Season      int               `json:"season,omitempty"` // a show's new season (a trailer, its card)
	Why         string            `json:"why,omitempty"`    // a trailer's: wanted, cinema or season
	Reel        []preshowReelItem `json:"reel,omitempty"`   // the reel card: what's coming
	Seconds     float64           `json:"seconds"`
	item        string            // what's recorded as played
}

// What's known about the film the pre-show is for.
type preshowFilm struct {
	Kind        string   `json:"kind"`
	Title       string   `json:"title,omitempty"`
	TMDBID      int      `json:"tmdbId,omitempty"`
	PosterURL   string   `json:"posterUrl,omitempty"`
	BackdropURL string   `json:"backdropUrl,omitempty"`
	Rated       string   `json:"rated,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Sound       string   `json:"sound,omitempty"`
	Picture     []string `json:"picture,omitempty"` // 4k, hdr, dv, imax
	Showtime    bool     `json:"showtime"`
	AtMs        int64    `json:"atMs,omitempty"` // a Showtime's start
	Files       bool     `json:"-"`              // for a player that plays only files (the Jellyfin app): no YouTube trailers
}

type preshowPlan struct {
	Theme   preshowTheme  `json:"theme"`
	Steps   []preshowStep `json:"steps"`
	Seconds float64       `json:"seconds"`
	Film    preshowFilm   `json:"film"`
	Theater string        `json:"theater,omitempty"`
	Holiday string        `json:"holiday,omitempty"`
}

var kidsRatings = map[string]bool{"G": true, "PG": true, "TV-Y": true, "TV-Y7": true, "TV-G": true, "TV-PG": true, "U": true}

// fits says whether a block's conditions hold for the film now.
func (w *preshowWhen) fits(f preshowFilm, now time.Time) bool {
	if w == nil {
		return true
	}
	has := func(list []string, v string) bool {
		for _, x := range list {
			if strings.EqualFold(strings.TrimSpace(x), v) {
				return true
			}
		}
		return false
	}
	if len(w.Genres) > 0 {
		ok := false
		for _, g := range f.Genres {
			if has(w.Genres, g) {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	for _, g := range f.Genres {
		if has(w.NotGenres, g) {
			return false
		}
	}
	if len(w.Kinds) > 0 && !has(w.Kinds, f.Kind) {
		return false
	}
	soundOK := len(w.Sound) == 0 || has(w.Sound, soundKey(f.Sound))
	pictureOK := len(w.Picture) == 0
	for _, p := range f.Picture {
		if has(w.Picture, p) {
			pictureOK = true
		}
	}
	if w.EitherAV && len(w.Sound) > 0 && len(w.Picture) > 0 {
		// Either is enough: a file's labels aren't always all there.
		if !soundOK && !pictureOK {
			return false
		}
	} else if !soundOK || !pictureOK {
		return false
	}
	if w.Kids != nil && (f.Rated == "" || kidsRatings[strings.ToUpper(f.Rated)] != *w.Kids) {
		return false
	}
	if w.Showtime != nil && *w.Showtime != f.Showtime {
		return false
	}
	if len(w.Holidays) > 0 {
		h := preshowHolidayNow(now)
		if h == "" || (!has(w.Holidays, "any") && !has(w.Holidays, h)) {
			return false
		}
	}
	return hourIn(now.Hour(), w.FromHour, w.ToHour)
}

// soundKey turns a sound label ("Dolby Atmos", "DTS:X") into a block's word.
func soundKey(label string) string {
	l := strings.ToLower(label)
	switch {
	case strings.Contains(l, "atmos"):
		return "atmos"
	case strings.Contains(l, "dts:x"):
		return "dtsx"
	case strings.Contains(l, "truehd"):
		return "truehd"
	case strings.Contains(l, "dts"):
		return "dts"
	}
	return ""
}

// pictureOf turns a picture's labels into a block's words.
func pictureOf(resolution, hdr string, formats []string) []string {
	var out []string
	if resolution == "4K" {
		out = append(out, "4k")
	}
	if hdr != "" {
		out = append(out, "hdr")
	}
	if strings.Contains(hdr, "Dolby Vision") {
		out = append(out, "dv")
	}
	for _, f := range formats {
		if strings.Contains(strings.ToUpper(f), "IMAX") {
			out = append(out, "imax")
			break
		}
	}
	return out
}

// preshowFilmFor gathers what's known about the film: the library's file
// (its sound and picture), else a stream's release name, and TMDB.
func (s *Server) preshowFilmFor(ctx context.Context, kind string, libraryID int64, tmdbID int, release string) preshowFilm {
	f := preshowFilm{Kind: kind, TMDBID: tmdbID}
	if f.Kind != "episode" {
		f.Kind = "movie"
	}
	var resolution, hdr string
	if kind == "movie" && libraryID > 0 {
		if m, err := s.MovieRepo.Get(libraryID); err == nil {
			f.Title, f.TMDBID = m.Title, m.TMDBID
			if sp, _, ok := s.librarySpecs(m.FilePath); ok {
				f.Sound, resolution, hdr = sp.sound, sp.resolution, sp.hdr
			}
		}
	}
	if release != "" {
		f.Sound, _ = releaseSound(release)
		l := strings.ToLower(release)
		if strings.Contains(l, "2160p") || strings.Contains(l, "4k") {
			resolution = "4K"
		}
		switch {
		case strings.Contains(l, ".dv.") || strings.Contains(l, "dovi") || strings.Contains(l, "dolby.vision"):
			hdr = "Dolby Vision"
		case strings.Contains(l, "hdr"):
			hdr = "HDR10"
		}
	}
	var formats []string
	if f.Kind == "movie" && f.TMDBID > 0 && s.TMDB().HasAPIKey() {
		if ft, ok := s.marqueeFeature(ctx, f.TMDBID, "playing"); ok {
			f.Title = firstNonEmptyStr(f.Title, ft.Title)
			f.PosterURL, f.BackdropURL, f.Rated, f.Genres, formats = ft.PosterURL, ft.BackdropURL, ft.Rated, ft.Genres, ft.Formats
		}
	}
	f.Picture = pictureOf(resolution, hdr, formats)
	return f
}

// lastPlayed reads when each item last played.
func (s *Server) lastPlayed(ctx context.Context) map[string]int64 {
	out := map[string]int64{}
	rows, err := s.db.QueryContext(ctx, `SELECT item, played_ms FROM preshow_played`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var v int64
		if rows.Scan(&k, &v) == nil {
			out[k] = v
		}
	}
	return out
}

// leastLately orders the items so the longest unplayed (never first) come
// first; ties are shuffled.
func leastLately[T any](items []T, key func(T) string, played map[string]int64) []T {
	out := append([]T(nil), items...)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	sort.SliceStable(out, func(i, j int) bool { return played[key(out[i])] < played[key(out[j])] })
	return out
}

// preshowComing lists the films coming soon: in the library and waiting for
// their release, then opening in cinemas, then showing there now.
func (s *Server) preshowComing(ctx context.Context) []int {
	today := time.Now()
	var ids []int
	seen := map[int]bool{}
	add := func(id int) {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id FROM movies WHERE monitored = 1 AND status != 'downloaded' AND release_date BETWEEN ? AND ? ORDER BY release_date`,
		today.Format("2006-01-02"), today.AddDate(0, 0, preshowComingDays).Format("2006-01-02"))
	if err == nil {
		for rows.Next() {
			var id int
			if rows.Scan(&id) == nil {
				add(id)
			}
		}
		rows.Close()
	}
	if s.TMDB().HasAPIKey() {
		now, upcoming := s.cinemaFilms(ctx)
		for _, m := range upcoming {
			add(m.TMDBID)
		}
		for _, m := range now {
			add(m.TMDBID)
		}
	}
	return ids
}

// planPreshow puts the pre-show together for the film. budget (seconds, 0
// for none) drops trailers, then clips, until it fits; the cards stay.
func (s *Server) planPreshow(ctx context.Context, film preshowFilm, themeID int64, budget float64, now time.Time) preshowPlan {
	s.preshowScanIfStale(ctx)
	set := s.preshowSettingsNow()
	theme := s.preshowThemeFor(ctx, themeID, now)
	plan := preshowPlan{Theme: theme, Steps: []preshowStep{}, Film: film, Theater: set.Theater, Holiday: preshowHolidayNow(now)}
	clips, _ := s.preshowClips(ctx)
	played := s.lastPlayed(ctx)
	used := map[string]bool{}
	clipKey := func(c preshowClip) string { return "clip:" + strconv.FormatInt(c.ID, 10) }
	clipStep := func(c preshowClip) preshowStep {
		return preshowStep{Kind: "clip", URL: c.FileURL, Title: c.Title, Category: c.Category, Seconds: c.Seconds, item: clipKey(c)}
	}
	pickClips := func(match func(preshowClip) bool, n int, inOrder bool) {
		var pool []preshowClip
		for _, c := range clips {
			if c.Enabled && c.Seconds > 0 && !used[clipKey(c)] && match(c) {
				pool = append(pool, c)
			}
		}
		if !inOrder {
			pool = leastLately(pool, clipKey, played)
		}
		for _, c := range pool[:min(n, len(pool))] {
			used[clipKey(c)] = true
			plan.Steps = append(plan.Steps, clipStep(c))
		}
	}
	var coming []int
	comingRead := false
	var ready []preshowTrailer
	readyRead := false
	soonCard := s.trailerSettingsNow().Card
	// addTrailer adds a trailer, with its coming-soon card first.
	addTrailer := func(st preshowStep) {
		if soonCard {
			plan.Steps = append(plan.Steps, preshowStep{Kind: "card", Card: "soon", Title: st.Title, TMDBID: st.TMDBID, Season: st.Season, Why: st.Why,
				PosterURL: st.PosterURL, ReleaseDate: st.ReleaseDate, Seconds: preshowCards["soon"]})
		}
		plan.Steps = append(plan.Steps, st)
	}
	for _, b := range theme.Blocks {
		if !b.When.fits(film, now) {
			continue
		}
		n := max(b.Count, 1)
		switch b.Type {
		case "card":
			if b.Card == "rating" && film.Rated == "" {
				continue
			}
			st := preshowStep{Kind: "card", Card: b.Card, Title: film.Title, PosterURL: film.PosterURL, BackdropURL: film.BackdropURL, Seconds: preshowCards[b.Card]}
			switch b.Card {
			case "announce":
				st.Title = strings.TrimSpace(b.Text)
			case "qr":
				st.Title = firstNonEmptyStr(strings.TrimSpace(b.Text), s.preshowQRDefault())
				if st.Title == "" {
					continue
				}
			case "reel":
				if st.Reel = s.preshowReel(ctx, now); len(st.Reel) == 0 {
					continue
				}
			}
			plan.Steps = append(plan.Steps, st)
		case "clips":
			if b.ClipID > 0 {
				pickClips(func(c preshowClip) bool { return c.ID == b.ClipID }, 1, true)
				continue
			}
			pickClips(func(c preshowClip) bool { return sameName(c.Category, b.Category) }, n, b.InOrder)
		case "seasonal":
			if plan.Holiday != "" {
				pickClips(func(c preshowClip) bool { return sameName(c.Category, plan.Holiday) }, n, false)
			}
		case "trailers":
			// Downloaded ones first (they play at once, and in the Jellyfin
			// app); YouTube's for the rest, where the player can play them.
			if !readyRead {
				ready, readyRead = trailerOrder(s.readyTrailers(ctx, now), played, now), true
			}
			got := 0
			for len(ready) > 0 && got < n {
				t := ready[0]
				ready = ready[1:]
				if (t.Kind == "movie" && t.TMDBID == film.TMDBID) || used[t.item()] {
					continue
				}
				used[t.item()] = true
				st := preshowStep{Kind: "trailer", URL: t.FileURL, Title: t.Title, TMDBID: t.TMDBID, Season: t.Season, Why: t.Why, PosterURL: t.PosterURL,
					ReleaseDate: t.ReleaseDate, Seconds: t.Seconds, item: t.item()}
				if st.Seconds <= 0 {
					st.Seconds = float64(set.TrailerSeconds)
				}
				addTrailer(st)
				got++
			}
			if film.Files {
				continue
			}
			if !comingRead {
				coming, comingRead = s.preshowComing(ctx), true
				coming = leastLately(coming, func(id int) string { return "trailer:" + strconv.Itoa(id) }, played)
			}
			tries := 0
			for ; got < n && len(coming) > 0 && tries < 3*n+3; tries++ {
				id := coming[0]
				coming = coming[1:]
				if id == film.TMDBID || used["trailer:"+strconv.Itoa(id)] {
					continue
				}
				ft, ok := s.marqueeFeature(ctx, id, "coming")
				if !ok || ft.Trailer == "" {
					continue
				}
				used["trailer:"+strconv.Itoa(id)] = true
				addTrailer(preshowStep{Kind: "trailer", YouTube: ft.Trailer, Title: ft.Title, TMDBID: id, PosterURL: ft.PosterURL,
					BackdropURL: ft.BackdropURL, ReleaseDate: ft.ReleaseDate, Seconds: float64(set.TrailerSeconds), item: "trailer:" + strconv.Itoa(id)})
				got++
			}
		}
	}
	total := func() float64 {
		t := 0.0
		for _, st := range plan.Steps {
			t += st.Seconds
		}
		return t
	}
	for budget > 0 && total() > budget {
		cut := -1
		for _, kind := range []string{"trailer", "clip"} {
			for i := len(plan.Steps) - 1; i >= 0 && cut < 0; i-- {
				if plan.Steps[i].Kind == kind {
					cut = i
				}
			}
			if cut >= 0 {
				break
			}
		}
		if cut < 0 {
			break
		}
		from := cut
		if cut > 0 && plan.Steps[cut].Kind == "trailer" && plan.Steps[cut-1].Card == "soon" {
			from-- // its coming-soon card goes with it
		}
		plan.Steps = append(plan.Steps[:from], plan.Steps[cut+1:]...)
	}
	plan.Seconds = total()
	return plan
}

// recordPlayed notes the plan's clips and trailers as played now.
func (s *Server) recordPlayed(ctx context.Context, plan preshowPlan) {
	now := time.Now().UnixMilli()
	for _, st := range plan.Steps {
		if st.item != "" {
			_, _ = s.db.ExecContext(ctx, `INSERT INTO preshow_played (item, played_ms) VALUES (?, ?) ON CONFLICT (item) DO UPDATE SET played_ms = excluded.played_ms`, st.item, now)
		}
	}
}

// GET /api/home/preshow/plan?kind=movie&id=<library id>&tmdbId=&release=&showtime=<id>&theme=&budget=<seconds>
// The pre-show for a film, to play now. A Showtime's own theme and pre-show
// length count.
func (s *Server) handlePreshowPlan(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, _ := strconv.ParseInt(q.Get("id"), 10, 64)
	tmdbID, _ := strconv.Atoi(q.Get("tmdbId"))
	themeID, _ := strconv.ParseInt(q.Get("theme"), 10, 64)
	budget, _ := strconv.ParseFloat(q.Get("budget"), 64)
	showtimeID, _ := strconv.ParseInt(q.Get("showtime"), 10, 64)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var atMs int64
	if showtimeID > 0 {
		var st showtime
		var theme int64
		err := s.db.QueryRowContext(ctx, `SELECT tmdb_id, at_ms, preshow_min, preshow_theme FROM showtimes WHERE id = ?`, showtimeID).Scan(&st.TMDBID, &st.AtMs, &st.PreshowMin, &theme)
		if err != nil {
			writeError(w, http.StatusNotFound, "That showtime isn't known.")
			return
		}
		tmdbID, atMs = st.TMDBID, st.AtMs
		if themeID == 0 {
			themeID = theme
		}
		if budget == 0 && st.PreshowMin > 0 {
			budget = float64(st.PreshowMin * 60)
		}
		if id == 0 {
			if m, ok, err := s.MovieRepo.GetByTMDBID(tmdbID); err == nil && ok && m.FilePath != "" {
				id = m.ID
			}
		}
	}
	film := s.preshowFilmFor(ctx, q.Get("kind"), id, tmdbID, trimRunes(q.Get("release"), 300))
	film.Showtime, film.AtMs = showtimeID > 0, atMs
	plan := s.planPreshow(ctx, film, themeID, budget, time.Now())
	if q.Get("dry") != "1" {
		s.recordPlayed(ctx, plan)
	}
	writeJSON(w, http.StatusOK, plan)
}

// POST /api/home/preshow/now: the player's pre-show is on this step (or
// over); the Marquee screens follow it.
func (s *Server) handlePreshowNow(w http.ResponseWriter, r *http.Request) {
	var b preshowNowMsg
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	s.preshowTell(b)
	w.WriteHeader(http.StatusNoContent)
}

// What the Marquee is told: the step playing, or End.
type preshowNowMsg struct {
	End         bool    `json:"end"`
	Kind        string  `json:"kind"`
	Card        string  `json:"card"`
	Title       string  `json:"title"`
	PosterURL   string  `json:"posterUrl"`
	BackdropURL string  `json:"backdropUrl"`
	ReleaseDate string  `json:"releaseDate"`
	FilmTitle   string  `json:"filmTitle"`
	FilmPoster  string  `json:"filmPoster"`
	Seconds     float64 `json:"seconds"`
}

// preshowTell has the Marquee screens follow the pre-show.
func (s *Server) preshowTell(m preshowNowMsg) {
	for _, p := range []*string{&m.Kind, &m.Card, &m.ReleaseDate} {
		*p = trimRunes(*p, 20)
	}
	for _, p := range []*string{&m.Title, &m.FilmTitle} {
		*p = trimRunes(*p, 200)
	}
	for _, p := range []*string{&m.PosterURL, &m.BackdropURL, &m.FilmPoster} {
		if !strings.HasPrefix(*p, "https://") && !strings.HasPrefix(*p, "/") {
			*p = ""
		}
		*p = trimRunes(*p, 500)
	}
	raw, _ := json.Marshal(m)
	s.marqueeLive.send("", marqueeEvent{Type: "command", Cmd: "follow", Arg: string(raw)})
}

// A title on the coming-soon reel.
type preshowReelItem struct {
	Title       string `json:"title"`
	PosterURL   string `json:"posterUrl"`
	ReleaseDate string `json:"releaseDate"`
	Season      int    `json:"season,omitempty"`
	Why         string `json:"why,omitempty"`
}

// preshowReel is up to four titles coming soonest (not yet out): the
// downloaded trailers' first, then the library's and the cinemas'.
func (s *Server) preshowReel(ctx context.Context, now time.Time) []preshowReelItem {
	today := now.Format("2006-01-02")
	var out []preshowReelItem
	seen := map[string]bool{}
	for _, t := range s.readyTrailers(ctx, now) {
		if len(out) < 4 && t.ReleaseDate >= today && t.PosterURL != "" && !seen[t.item()] {
			seen[t.item()] = true
			out = append(out, preshowReelItem{Title: t.Title, PosterURL: t.PosterURL, ReleaseDate: t.ReleaseDate, Season: t.Season, Why: t.Why})
		}
	}
	for _, id := range s.preshowComing(ctx) {
		if len(out) >= 4 {
			break
		}
		if seen["trailer:"+strconv.Itoa(id)] {
			continue
		}
		if ft, ok := s.marqueeFeature(ctx, id, "coming"); ok && ft.PosterURL != "" && ft.ReleaseDate >= today {
			seen["trailer:"+strconv.Itoa(id)] = true
			out = append(out, preshowReelItem{Title: ft.Title, PosterURL: ft.PosterURL, ReleaseDate: ft.ReleaseDate, Why: "cinema"})
		}
	}
	return out
}

// preshowQRDefault is Usherr's own address for the QR card: the public one
// set in Settings, else the Jellyfin pre-show's.
func (s *Server) preshowQRDefault() string {
	if v, _ := s.Settings.Get(settings.KeyPublicURL); strings.HasPrefix(strings.TrimSpace(v), "http") {
		return strings.TrimSpace(v)
	}
	return s.preshowJFNow().Address
}

// trailerOrder puts the trailers in the order the pre-show takes them: still
// to come before just out, the household's own (wanted films, new seasons)
// before the cinemas', and within each, the least lately played.
func trailerOrder(list []preshowTrailer, played map[string]int64, now time.Time) []preshowTrailer {
	today := now.Format("2006-01-02")
	rank := func(t preshowTrailer) int {
		r := 0
		if t.ReleaseDate < today {
			r += 2
		}
		if t.Why == "cinema" {
			r++
		}
		return r
	}
	out := leastLately(list, func(t preshowTrailer) string { return t.item() }, played)
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}
