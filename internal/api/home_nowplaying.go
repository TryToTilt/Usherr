package api

// Usherr: what Usherr itself is playing, for the Marquee's "now showing",
// next to what Jellyfin, Emby and StreamNZB say. Three ways in:
//
//   - Usherr's player in the browser (library files, Stream now, Live TV)
//     tells the server every few seconds what's on, how far in and whether
//     it's paused, and says when it closes;
//   - the Stremio and Nuvio add-on, when an app reads a library file or a
//     stream from Usherr's engine through it;
//   - nothing else: a player that stops telling drops off after a while.
//
// Kept in memory only: it's what's on now, not a history. NSFW videos are
// never listed.

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/parser"
)

// usherrPlayingFor is how long a player is listed after it last said so.
const usherrPlayingFor = 45 * time.Second

type usherrPlaying struct {
	userID   int64
	device   string // "Usherr in the browser", "Stremio or Nuvio", "Live TV · Channel 4"
	kind     string // "movie", "episode" or "other"
	title    string // the film's or episode's title
	series   string
	season   int
	episode  int
	tmdbID   int // the film's
	showID   int // the show's (an episode)
	year     int
	release  string  // for the picture and sound, when known
	progress float64 // 0..1, when known
	paused   bool
	at       time.Time
}

var usherrNow = struct {
	mu sync.Mutex
	m  map[string]*usherrPlaying // a player's key -> what it plays
}{m: map[string]*usherrPlaying{}}

func usherrNowSet(key string, p *usherrPlaying) {
	p.at = time.Now()
	usherrNow.mu.Lock()
	defer usherrNow.mu.Unlock()
	usherrNow.m[key] = p
	for k, v := range usherrNow.m {
		if time.Since(v.at) > usherrPlayingFor {
			delete(usherrNow.m, k)
		}
	}
}

func usherrNowList() []usherrPlaying {
	usherrNow.mu.Lock()
	defer usherrNow.mu.Unlock()
	out := []usherrPlaying{}
	keys := make([]string, 0, len(usherrNow.m))
	for k, v := range usherrNow.m {
		if time.Since(v.at) <= usherrPlayingFor {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, *usherrNow.m[k])
	}
	return out
}

// usherrPlayingFromLibrary fills in a library film or episode by its id.
// ok is false for anything that isn't one (or isn't there).
func (s *Server) usherrPlayingFromLibrary(kind string, id int64, p *usherrPlaying) bool {
	switch kind {
	case "movie":
		m, err := s.MovieRepo.Get(id)
		if err != nil {
			return false
		}
		p.kind, p.title, p.tmdbID, p.year = "movie", m.Title, m.TMDBID, m.Year
		return true
	case "episode":
		ep, err := s.MovieRepo.GetEpisodeByID(id)
		if err != nil {
			return false
		}
		p.kind, p.title, p.season, p.episode, p.release = "episode", ep.Title, ep.Season, ep.Episode, ep.ReleaseTitle
		if sr, err := s.MovieRepo.GetSeries(ep.SeriesID); err == nil {
			p.series, p.showID = sr.Title, sr.TMDBID
		}
		return true
	}
	return false
}

// POST /api/home/playing: Usherr's player says what it plays now.
func (s *Server) handleNowPlaying(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key      string  `json:"key"`
		Kind     string  `json:"kind"` // movie, episode, stream or channel (video is never listed)
		ID       int64   `json:"id"`
		Title    string  `json:"title"`
		Subtitle string  `json:"subtitle"`
		Channel  string  `json:"channel"` // Live TV: the channel's name
		TMDBID   int     `json:"tmdbId"`  // a stream: the title, when the page knows it
		TVKind   string  `json:"tvKind"`  // "movie" or "tv"
		Season   int     `json:"season"`
		Episode  int     `json:"episode"`
		StreamID string  `json:"streamId"`
		Position float64 `json:"position"`
		Duration float64 `json:"duration"`
		Paused   bool    `json:"paused"`
	}
	if err := decodeJSON(r, &body); err != nil || !marqueeScreenIDRe.MatchString(strings.ToLower(body.Key)) {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	user := requestUserID(r)
	p := &usherrPlaying{userID: user, device: "Usherr in the browser", kind: "other", paused: body.Paused}
	if ch := trimRunes(strings.TrimSpace(body.Channel), 60); ch != "" {
		p.device = "Live TV · " + ch
	}
	if body.Duration > 0 && body.Position >= 0 {
		p.progress = min(body.Position/body.Duration, 1)
	}
	switch body.Kind {
	case "video":
		w.WriteHeader(http.StatusNoContent) // never shown
		return
	case "movie", "episode":
		if !s.usherrPlayingFromLibrary(body.Kind, body.ID, p) {
			p.title = trimRunes(strings.TrimSpace(body.Title), 200)
		}
	default: // a stream, or a live channel
		p.title = trimRunes(strings.TrimSpace(body.Title), 200)
		if ch := strings.TrimSpace(body.Channel); ch != "" {
			p.title = strings.TrimPrefix(p.title, ch+" · ") // the page's "Channel · Show"
		}
		if body.StreamID != "" {
			if ss, ok := streams.get(body.StreamID, user); ok {
				streams.mu.Lock()
				p.release = ss.release
				streams.mu.Unlock()
			}
		}
		switch {
		case body.TMDBID > 0 && body.TVKind == "movie":
			p.kind, p.tmdbID = "movie", body.TMDBID
		case body.TMDBID > 0 && body.TVKind == "tv":
			p.kind, p.showID, p.season, p.episode = "episode", body.TMDBID, body.Season, body.Episode
			p.series = p.title
			if sub := strings.TrimSpace(body.Subtitle); sub != "" {
				p.title = trimRunes(sub, 200)
			}
		}
	}
	if p.title == "" && p.series == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	usherrNowSet("p:"+strings.ToLower(body.Key), p)
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/home/playing/{key}: the player closed.
func (s *Server) handleNowPlayingStop(w http.ResponseWriter, r *http.Request) {
	key := "p:" + strings.ToLower(r.PathValue("key"))
	user := requestUserID(r)
	usherrNow.mu.Lock()
	if p, ok := usherrNow.m[key]; ok && p.userID == user {
		delete(usherrNow.m, key)
	}
	usherrNow.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// addonPlaying notes what a Stremio or Nuvio app reads through the add-on:
// key is the stream (or file) it reads, offset and size where in it the app
// is reading (its only sign of how far in it is).
func addonPlaying(key string, base usherrPlaying, offset, size int64) {
	p := base
	p.device = "Stremio or Nuvio"
	if size > 0 && offset > 0 {
		p.progress = min(float64(offset)/float64(size), 1)
	}
	usherrNow.mu.Lock()
	old, had := usherrNow.m["a:"+key]
	usherrNow.mu.Unlock()
	if had && offset == 0 {
		p.progress = old.progress // a read from the start (a probe) doesn't move it back
	}
	usherrNowSet("a:"+key, &p)
}

// rangeStart is where a Range request starts reading (0 without one).
func rangeStart(r *http.Request) int64 {
	v, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes=")
	if !ok {
		return 0
	}
	from, _, _ := strings.Cut(v, "-")
	var n int64
	for _, c := range from {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// usherrNowShowing is what Usherr plays, for the Marquee: posters and all,
// leaving out what another source already lists (a stream from StreamNZB
// played in Usherr's player is the same showing).
func (s *Server) usherrNowShowing(ctx context.Context, already []marqueePlaying, tmdbOK bool) []marqueePlaying {
	seen := map[string]bool{}
	key := func(kind string, tmdb, season, episode int) string {
		return kind + ":" + strconv.Itoa(tmdb) + ":" + strconv.Itoa(season) + ":" + strconv.Itoa(episode)
	}
	for _, p := range already {
		switch {
		case p.Kind == "movie" && p.TMDBID > 0:
			seen[key("movie", p.TMDBID, 0, 0)] = true
		case p.Kind == "episode" && p.SeriesTMDB > 0:
			seen[key("episode", p.SeriesTMDB, p.Season, p.Episode)] = true
		}
	}
	var out []marqueePlaying
	for _, u := range usherrNowList() {
		if u.kind == "other" && tmdbOK {
			s.usherrIdentify(ctx, &u)
		}
		k := ""
		switch {
		case u.kind == "movie" && u.tmdbID > 0:
			k = key("movie", u.tmdbID, 0, 0)
		case u.kind == "episode" && u.showID > 0:
			k = key("episode", u.showID, u.season, u.episode)
		}
		if k != "" {
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		pl := mediaservers.Playing{Title: u.title, Series: u.series, Season: u.season, Episode: u.episode, Kind: u.kind,
			TMDBID: u.tmdbID, Year: u.year, Progress: u.progress, Paused: u.paused, Device: u.device, SeriesTMDB: u.showID}
		if u.release != "" {
			pl.Resolution, pl.HDR = releaseResolution(u.release), hdrOf(u.release)
		}
		mp := marqueePlaying{Playing: pl}
		if u.kind == "movie" && u.tmdbID > 0 && tmdbOK {
			if f, ok := s.marqueeFeature(ctx, u.tmdbID, "playing"); ok {
				mp.PosterURL, mp.BackdropURL, mp.Cast, mp.Director = f.PosterURL, f.BackdropURL, f.Cast, f.Director
				if mp.Overview == "" {
					mp.Overview = f.Overview
				}
				if mp.Year == 0 {
					mp.Year = f.Year
				}
				if f.Title != "" {
					mp.Title = f.Title // the film's own name, not a release's
				}
			}
		}
		if u.kind == "episode" && u.showID > 0 && tmdbOK {
			mp.PosterURL, mp.BackdropURL, mp.Cast = s.marqueeShowArt(ctx, u.showID)
		}
		out = append(out, mp)
	}
	return out
}

// usherrFound remembers what a stream's name was found to be on TMDB (a
// zero id: not found), so it's asked once, not on every look.
var usherrFound sync.Map // "release|title" -> usherrPlaying (kind and ids only)

// usherrIdentify finds a stream that came without its title (a release from
// a list, a streamed Live TV show) on TMDB, by its release's name and year,
// so the Marquee has its poster and details.
func (s *Server) usherrIdentify(ctx context.Context, u *usherrPlaying) {
	key := u.release + "|" + u.title
	if v, ok := usherrFound.Load(key); ok {
		f := v.(usherrPlaying)
		if f.kind != "other" {
			u.kind, u.tmdbID, u.showID, u.season, u.episode = f.kind, f.tmdbID, f.showID, f.season, f.episode
			if f.series != "" {
				u.series = f.series
			}
		}
		return
	}
	name := u.release
	if name == "" {
		name = u.title
	}
	pr := parser.Parse(name)
	q := strings.TrimSpace(pr.Title)
	if q == "" {
		q = strings.TrimSpace(u.title)
	}
	found := usherrPlaying{kind: "other"}
	ask, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if q != "" {
		if pr.Season > 0 {
			if shows, err := s.displayTMDB().SearchTV(ask, q); err == nil && len(shows) > 0 {
				found = usherrPlaying{kind: "episode", showID: shows[0].TMDBID, season: pr.Season, episode: pr.Episode, series: shows[0].Name}
			}
		} else if films, err := s.displayTMDB().SearchMovies(ask, q); err == nil {
			for _, f := range films {
				if pr.Year == 0 || strings.HasPrefix(f.ReleaseDate, strconv.Itoa(pr.Year)) {
					found = usherrPlaying{kind: "movie", tmdbID: f.TMDBID}
					break
				}
			}
		}
	}
	if ask.Err() != nil && found.kind == "other" {
		return // TMDB didn't answer: asked again next time
	}
	usherrFound.Store(key, found)
	if found.kind != "other" {
		u.kind, u.tmdbID, u.showID, u.season, u.episode = found.kind, found.tmdbID, found.showID, found.season, found.episode
		if found.series != "" {
			u.series = found.series
		}
	}
}

// releaseResolution is "4K", "1080p", "720p" or "" from a release's name.
func releaseResolution(release string) string {
	switch heightOf(release) {
	case 2160:
		return "4K"
	case 1080:
		return "1080p"
	case 720:
		return "720p"
	}
	return ""
}
