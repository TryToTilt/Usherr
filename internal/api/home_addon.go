package api

// Usherr: the add-on for Stremio and Nuvio. Both apps speak the same add-on
// protocol: they fetch /manifest.json from an address, then ask it for
// catalogs, details, streams and subtitles (JSON over plain GET, with CORS).
// Each person has a private key in the address (/addon/<key>/manifest.json),
// so the apps need no sign-in, and a new key stops the old address working.
//
// What it gives them:
//   - catalogs: the library's movies and shows (and the streamed ones), the
//     person's thumbs-up titles as a watchlist, and the Live TV channels;
//   - streams for any title the app asks about (by IMDb id): the library's
//     own file first, then releases found by Usherr's streaming engine,
//     best first, opened when one is chosen;
//   - subtitles from the library file (text subtitles in it or next to it);
//   - Live TV channels, played through the same address the tuner uses.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/qr"
)

const (
	addonID       = "com.usherr.addon"
	addonPageSize = 100
	addonStreams  = 6 // releases offered for a title
	addonKeep     = 3 * time.Hour
)

func (s *Server) addonRoutes(root *http.ServeMux) {
	root.HandleFunc("OPTIONS /addon/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		addonCORS(w)
		w.WriteHeader(http.StatusNoContent)
	})
	root.HandleFunc("GET /addon/{key}/manifest.json", s.handleAddonManifest)
	root.HandleFunc("GET /addon/{key}/catalog/{type}/{rest...}", s.handleAddonCatalog)
	root.HandleFunc("GET /addon/{key}/meta/{type}/{id}", s.handleAddonMeta)
	root.HandleFunc("GET /addon/{key}/stream/{type}/{id}", s.handleAddonStream)
	root.HandleFunc("GET /addon/{key}/subtitles/{type}/{rest...}", s.handleAddonSubtitles)
	root.HandleFunc("GET /addon/{key}/file/{kind}/{id}", s.handleAddonFile)
	root.HandleFunc("GET /addon/{key}/play/{token}", s.handleAddonPlay)
	root.HandleFunc("GET /addon/{key}/subs/{kind}/{id}", s.handleAddonSubs)
}

// ---- keys ----

func (s *Server) addonKeyOf(ctx context.Context, userID int64) (string, error) {
	var key string
	err := s.db.QueryRowContext(ctx, `SELECT key FROM addon_keys WHERE user_id = ?`, userID).Scan(&key)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return s.addonNewKey(ctx, userID)
}

// addonNewKey makes a person's key (or a new one in place of the old).
func (s *Server) addonNewKey(ctx context.Context, userID int64) (string, error) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	key := hex.EncodeToString(b)
	_, err := s.db.ExecContext(ctx, `INSERT INTO addon_keys (key, user_id) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET key = excluded.key, created_at = datetime('now')`, key, userID)
	return key, err
}

// addonUser is the person a request's key belongs to, when the account is on
// and may play.
func (s *Server) addonUser(r *http.Request) (*auth.User, bool) {
	key := r.PathValue("key")
	if len(key) != 32 {
		return nil, false
	}
	var uid int64
	if err := s.db.QueryRowContext(r.Context(), `SELECT user_id FROM addon_keys WHERE key = ?`, key).Scan(&uid); err != nil {
		return nil, false
	}
	u, err := s.Auth.GetUser(uid)
	if err != nil || u == nil || u.Disabled {
		return nil, false
	}
	if perms, err := s.Auth.PermissionsOf(uid); err != nil || !perms.Play {
		return nil, false
	}
	return u, true
}

// ---- answers ----

func addonCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Headers", "*")
	h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
}

func addonJSON(w http.ResponseWriter, v any, maxAge int) {
	addonCORS(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if maxAge > 0 {
		w.Header().Set("Cache-Control", "max-age="+strconv.Itoa(maxAge))
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	_ = json.NewEncoder(w).Encode(v)
}

// addonBase is the address the app reached this server at, for the links
// it's given.
func (s *Server) addonBase(r *http.Request) string {
	scheme := "http"
	if s.isHTTPS(r) {
		scheme = "https"
	}
	if r.Host == "" {
		return defaultTVBase
	}
	return scheme + "://" + r.Host
}

// ---- the manifest ----

func (s *Server) handleAddonManifest(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.addonUser(r); !ok {
		http.NotFound(w, r)
		return
	}
	search := []map[string]any{{"name": "search"}, {"name": "skip"}}
	skip := []map[string]any{{"name": "skip"}}
	manifest := map[string]any{
		"id":          addonID,
		"version":     "1.0.0",
		"name":        "Usherr",
		"description": "Your Usherr library, releases from your Usenet providers, your watchlist and your Live TV channels.",
		"logo":        s.addonBase(r) + "/usherr-mark-amber.png",
		"types":       []string{"movie", "series", "tv"},
		"resources": []any{
			"catalog",
			map[string]any{"name": "meta", "types": []string{"movie", "series", "tv"}, "idPrefixes": []string{"usherr:"}},
			map[string]any{"name": "stream", "types": []string{"movie", "series", "tv"}, "idPrefixes": []string{"tt", "usherr:"}},
			map[string]any{"name": "subtitles", "types": []string{"movie", "series"}, "idPrefixes": []string{"tt", "usherr:"}},
		},
		"idPrefixes": []string{"tt", "usherr:"},
		"catalogs": []map[string]any{
			{"type": "movie", "id": "usherr-movies", "name": "Usherr Movies", "extra": search},
			{"type": "series", "id": "usherr-tv", "name": "Usherr TV", "extra": search},
			{"type": "movie", "id": "usherr-watchlist-movies", "name": "Usherr Watchlist", "extra": skip},
			{"type": "series", "id": "usherr-watchlist-tv", "name": "Usherr Watchlist", "extra": skip},
			{"type": "tv", "id": "usherr-live", "name": "Usherr Live TV"},
		},
		"behaviorHints": map[string]any{"configurable": false},
	}
	addonJSON(w, manifest, 0)
}

// ---- ids ----

// addonRef is what an app asked about.
type addonRef struct {
	Kind    string // movie, series or tv
	TMDB    int
	IMDB    string
	Season  int
	Episode int
	Channel int
}

var addonIMDbRe = regexp.MustCompile(`^tt\d{5,12}$`)

// parseAddonID reads an id the apps send: an IMDb id (tt1234567, or
// tt1234567:1:2 for an episode), one of Usherr's own (usherr:tmdb:movie:603,
// usherr:tmdb:series:1399:1:2) or a channel (usherr:tv:4).
func parseAddonID(typ, id string) (addonRef, bool) {
	parts := strings.Split(id, ":")
	switch {
	case len(parts) >= 1 && addonIMDbRe.MatchString(parts[0]):
		ref := addonRef{Kind: typ, IMDB: parts[0]}
		if typ == "series" {
			if len(parts) != 3 {
				return addonRef{}, false
			}
			ref.Season, _ = strconv.Atoi(parts[1])
			ref.Episode, _ = strconv.Atoi(parts[2])
		}
		return ref, typ == "movie" || (typ == "series" && ref.Season >= 0 && ref.Episode > 0)
	case len(parts) == 3 && parts[0] == "usherr" && parts[1] == "tv":
		n, err := strconv.Atoi(parts[2])
		return addonRef{Kind: "tv", Channel: n}, err == nil
	case len(parts) >= 4 && parts[0] == "usherr" && parts[1] == "tmdb":
		n, err := strconv.Atoi(parts[3])
		if err != nil || n <= 0 || (parts[2] != "movie" && parts[2] != "series") {
			return addonRef{}, false
		}
		ref := addonRef{Kind: parts[2], TMDB: n}
		if ref.Kind == "series" && len(parts) == 6 {
			ref.Season, _ = strconv.Atoi(parts[4])
			ref.Episode, _ = strconv.Atoi(parts[5])
		}
		return ref, ref.Kind == "movie" || len(parts) == 4 || len(parts) == 6
	}
	return addonRef{}, false
}

func addonTitleID(kind string, tmdb int, imdb string) string {
	if addonIMDbRe.MatchString(imdb) {
		return imdb
	}
	return fmt.Sprintf("usherr:tmdb:%s:%d", kind, tmdb)
}

var imdbToTMDB sync.Map // imdb id + kind -> tmdb id

// addonTMDB finds the TMDB id of an IMDb id: in the library, among the
// streamed titles, or on TMDB.
func (s *Server) addonTMDB(ctx context.Context, ref addonRef) (int, error) {
	if ref.TMDB > 0 {
		return ref.TMDB, nil
	}
	key := ref.Kind + ":" + ref.IMDB
	if v, ok := imdbToTMDB.Load(key); ok {
		return v.(int), nil
	}
	var id int
	if ref.Kind == "series" {
		if list, err := s.MovieRepo.ListSeries(); err == nil {
			for _, sr := range list {
				if sr.IMDBID == ref.IMDB {
					id = sr.TMDBID
				}
			}
		}
	}
	if id == 0 {
		kind := ref.Kind
		_ = s.db.QueryRowContext(ctx, `SELECT tmdb_id FROM streamed_titles WHERE imdb_id = ? AND kind = ? LIMIT 1`, ref.IMDB, kind).Scan(&id)
	}
	if id == 0 {
		tm := s.TMDB()
		if tm == nil {
			return 0, errors.New("TMDB isn't set up")
		}
		movieID, showID, err := tm.FindByIMDB(ctx, ref.IMDB)
		if err != nil {
			return 0, err
		}
		if ref.Kind == "series" {
			id = showID
		} else {
			id = movieID
		}
	}
	if id == 0 {
		return 0, errors.New("that title isn't on TMDB")
	}
	imdbToTMDB.Store(key, id)
	return id, nil
}

// ---- catalogs ----

type addonVideo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
	Released string `json:"released,omitempty"`
	Overview string `json:"overview,omitempty"`
}

type addonMeta struct {
	ID          string       `json:"id"`
	Type        string       `json:"type"`
	Name        string       `json:"name"`
	Poster      string       `json:"poster,omitempty"`
	PosterShape string       `json:"posterShape,omitempty"`
	Background  string       `json:"background,omitempty"`
	Logo        string       `json:"logo,omitempty"`
	Description string       `json:"description,omitempty"`
	ReleaseInfo string       `json:"releaseInfo,omitempty"`
	Videos      []addonVideo `json:"videos,omitempty"`
}

func tmdbPoster(path string) string {
	if strings.HasPrefix(path, "http") {
		return path
	}
	return metadata.ImageURL("w342", path)
}

func yearInfo(y int) string {
	if y <= 0 {
		return ""
	}
	return strconv.Itoa(y)
}

// handleAddonCatalog: GET /addon/<key>/catalog/<type>/<id>[/<extra>].json
func (s *Server) handleAddonCatalog(w http.ResponseWriter, r *http.Request) {
	u, ok := s.addonUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, extra, _ := strings.Cut(strings.TrimSuffix(r.PathValue("rest"), ".json"), "/")
	vals, _ := url.ParseQuery(extra)
	skip, _ := strconv.Atoi(vals.Get("skip"))
	search := strings.ToLower(strings.TrimSpace(vals.Get("search")))
	var metas []addonMeta
	switch id {
	case "usherr-movies":
		metas = s.addonLibraryMetas(r.Context(), "movie")
	case "usherr-tv":
		metas = s.addonLibraryMetas(r.Context(), "series")
	case "usherr-watchlist-movies":
		metas = s.addonWatchlist(r.Context(), u.ID, "movie")
	case "usherr-watchlist-tv":
		metas = s.addonWatchlist(r.Context(), u.ID, "series")
	case "usherr-live":
		metas = s.addonChannels(r)
	default:
		http.NotFound(w, r)
		return
	}
	if search != "" {
		kept := metas[:0]
		for _, m := range metas {
			if strings.Contains(strings.ToLower(m.Name), search) {
				kept = append(kept, m)
			}
		}
		metas = kept
	}
	if skip < 0 || skip > len(metas) {
		skip = len(metas)
	}
	metas = metas[skip:]
	if len(metas) > addonPageSize && id != "usherr-live" {
		metas = metas[:addonPageSize]
	}
	if metas == nil {
		metas = []addonMeta{}
	}
	addonJSON(w, map[string]any{"metas": metas}, 60)
}

// addonLibraryMetas lists the library's titles that have files, then the
// streamed ones, without repeating a title.
func (s *Server) addonLibraryMetas(ctx context.Context, kind string) []addonMeta {
	var out []addonMeta
	seen := map[int]bool{}
	if kind == "movie" {
		if list, err := s.MovieRepo.List(); err == nil {
			for _, m := range list {
				if m.FilePath == "" || seen[m.TMDBID] {
					continue
				}
				seen[m.TMDBID] = true
				out = append(out, addonMeta{ID: addonTitleID("movie", m.TMDBID, ""), Type: "movie", Name: m.Title, Poster: tmdbPoster(m.PosterPath), PosterShape: "poster", Description: m.Overview, ReleaseInfo: yearInfo(m.Year)})
			}
		}
	} else if list, err := s.MovieRepo.ListSeries(); err == nil {
		for _, sr := range list {
			if sr.DownloadedCount == 0 || seen[sr.TMDBID] {
				continue
			}
			seen[sr.TMDBID] = true
			out = append(out, addonMeta{ID: addonTitleID("series", sr.TMDBID, sr.IMDBID), Type: "series", Name: sr.Title, Poster: tmdbPoster(sr.PosterPath), PosterShape: "poster", Description: sr.Overview, ReleaseInfo: yearInfo(sr.Year)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id, imdb_id, title, year, overview, poster_path FROM streamed_titles WHERE kind = ? ORDER BY added_at DESC, id DESC`, kind)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var tmdb, year int
		var imdb, title, overview, poster string
		if rows.Scan(&tmdb, &imdb, &title, &year, &overview, &poster) != nil || seen[tmdb] {
			continue
		}
		seen[tmdb] = true
		out = append(out, addonMeta{ID: addonTitleID(kind, tmdb, imdb), Type: kind, Name: title, Poster: tmdbPoster(poster), PosterShape: "poster", Description: overview, ReleaseInfo: yearInfo(year)})
	}
	return out
}

// addonWatchlist is the person's thumbs-up titles.
func (s *Server) addonWatchlist(ctx context.Context, userID int64, kind string) []addonMeta {
	voteKind := "movie"
	if kind == "series" {
		voteKind = "tv"
	}
	var out []addonMeta
	for _, v := range s.votesOf(ctx, userID, voteKind, 300) {
		tmdb, err := strconv.Atoi(v.Ref)
		if v.Vote <= 0 || err != nil || tmdb <= 0 {
			continue
		}
		out = append(out, addonMeta{ID: addonTitleID(kind, tmdb, ""), Type: kind, Name: v.Title, Poster: tmdbPoster(v.Image), PosterShape: "poster", ReleaseInfo: yearInfo(v.Year)})
	}
	return out
}

// addonChannels lists the enabled Live TV channels.
func (s *Server) addonChannels(r *http.Request) []addonMeta {
	chans, err := s.tvRepo().Channels()
	if err != nil {
		return nil
	}
	base := s.addonBase(r)
	var out []addonMeta
	for _, c := range s.feedChannels(chans, base, s.tvFeedKey()) {
		if !c.Enabled {
			continue
		}
		out = append(out, addonMeta{ID: fmt.Sprintf("usherr:tv:%d", c.Number), Type: "tv", Name: fmt.Sprintf("%d · %s", c.Number, c.Name), Poster: c.LogoURL, PosterShape: "square", Logo: c.LogoURL})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.SplitN(out[i].Name, " ", 2)[0])
		b, _ := strconv.Atoi(strings.SplitN(out[j].Name, " ", 2)[0])
		return a < b
	})
	return out
}

// ---- details ----

// handleAddonMeta: GET /addon/<key>/meta/<type>/<id>.json, for Usherr's own ids.
func (s *Server) handleAddonMeta(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.addonUser(r); !ok {
		http.NotFound(w, r)
		return
	}
	ref, ok := parseAddonID(r.PathValue("type"), strings.TrimSuffix(r.PathValue("id"), ".json"))
	if !ok || !strings.HasPrefix(r.PathValue("id"), "usherr:") {
		addonJSON(w, map[string]any{"meta": nil}, 60)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	switch ref.Kind {
	case "tv":
		chans, err := s.tvRepo().Channels()
		if err != nil {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		for _, c := range s.feedChannels(chans, s.addonBase(r), s.tvFeedKey()) {
			if c.Number != ref.Channel || !c.Enabled {
				continue
			}
			m := addonMeta{ID: fmt.Sprintf("usherr:tv:%d", c.Number), Type: "tv", Name: fmt.Sprintf("%d · %s", c.Number, c.Name), Poster: c.LogoURL, PosterShape: "square", Logo: c.LogoURL}
			now := time.Now()
			if p, ok, _ := s.tvRepo().At(c.ID, now); ok {
				m.Description = "On now: " + p.Title
				if p.Subtitle != "" {
					m.Description += " · " + p.Subtitle
				}
				if n, ok, _ := s.tvRepo().At(c.ID, p.End); ok {
					m.Description += "\nNext: " + n.Title
				}
			}
			addonJSON(w, map[string]any{"meta": m}, 30)
			return
		}
	case "movie":
		tm := s.TMDB()
		if tm == nil {
			break
		}
		d, err := tm.GetMovieDetail(ctx, ref.TMDB)
		if err != nil {
			break
		}
		m := addonMeta{ID: addonTitleID("movie", ref.TMDB, d.IMDBID), Type: "movie", Name: d.Title, Poster: tmdbPoster(d.PosterPath), Background: metadata.ImageURL("w1280", d.BackdropPath), Description: d.Overview}
		if len(d.ReleaseDate) >= 4 {
			m.ReleaseInfo = d.ReleaseDate[:4]
		}
		m.ID = fmt.Sprintf("usherr:tmdb:movie:%d", ref.TMDB) // the id the app asked with
		addonJSON(w, map[string]any{"meta": m}, 300)
		return
	case "series":
		tm := s.TMDB()
		if tm == nil {
			break
		}
		show, eps, err := tm.GetShowEpisodes(ctx, ref.TMDB)
		if err != nil {
			break
		}
		m := addonMeta{ID: fmt.Sprintf("usherr:tmdb:series:%d", ref.TMDB), Type: "series", Name: show.Name, Poster: tmdbPoster(show.PosterPath), Background: metadata.ImageURL("w1280", show.BackdropPath), Description: show.Overview}
		if len(show.FirstAirDate) >= 4 {
			m.ReleaseInfo = show.FirstAirDate[:4]
		}
		for _, e := range eps {
			if e.Season <= 0 {
				continue
			}
			v := addonVideo{ID: fmt.Sprintf("usherr:tmdb:series:%d:%d:%d", ref.TMDB, e.Season, e.Episode), Title: e.Name, Season: e.Season, Episode: e.Episode, Overview: e.Overview}
			if e.AirDate != "" {
				v.Released = e.AirDate + "T00:00:00.000Z"
			}
			m.Videos = append(m.Videos, v)
		}
		addonJSON(w, map[string]any{"meta": m}, 300)
		return
	}
	addonJSON(w, map[string]any{"meta": nil}, 30)
}

// ---- streams ----

type addonStream struct {
	Name          string         `json:"name"`
	Title         string         `json:"title,omitempty"`
	Description   string         `json:"description,omitempty"`
	URL           string         `json:"url"`
	BehaviorHints map[string]any `json:"behaviorHints,omitempty"`
}

// addonCand is a release offered to an app, opened when the app asks for it.
type addonCand struct {
	mu      sync.Mutex
	res     indexers.Result
	alts    []indexers.Result
	userID  int64
	at      time.Time
	session string        // the engine's session, once opened
	what    usherrPlaying // Usherr: what it is, for the Marquee
}

var (
	addonCandMu sync.Mutex
	addonCands  = map[string]*addonCand{}
)

func init() {
	go func() {
		for range time.Tick(10 * time.Minute) {
			addonCandMu.Lock()
			for k, c := range addonCands {
				if time.Since(c.at) > addonKeep {
					c.mu.Lock()
					ustreamClose(c.session)
					c.mu.Unlock()
					delete(addonCands, k)
				}
			}
			addonCandMu.Unlock()
		}
	}()
}

// addonLibraryFile finds the library file of a title: its id and kind
// ("movie" or "episode") for the file routes; "" when there is none.
func (s *Server) addonLibraryFile(ctx context.Context, ref addonRef) (kind string, id int64, quality string) {
	tmdb, err := s.addonTMDB(ctx, ref)
	if err != nil {
		return "", 0, ""
	}
	if ref.Kind == "movie" {
		if m, ok, _ := s.MovieRepo.GetByTMDBID(tmdb); ok && m.FilePath != "" {
			return "movie", m.ID, m.Quality
		}
		return "", 0, ""
	}
	if sr, ok, _ := s.MovieRepo.GetSeriesByTMDBID(tmdb); ok {
		if ep, err := s.MovieRepo.GetEpisode(sr.ID, ref.Season, ref.Episode); err == nil && ep.FilePath != "" {
			return "episode", ep.ID, ep.Quality
		}
	}
	return "", 0, ""
}

// handleAddonStream: GET /addon/<key>/stream/<type>/<id>.json
func (s *Server) handleAddonStream(w http.ResponseWriter, r *http.Request) {
	u, ok := s.addonUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ref, ok := parseAddonID(r.PathValue("type"), strings.TrimSuffix(r.PathValue("id"), ".json"))
	if !ok {
		addonJSON(w, map[string]any{"streams": []addonStream{}}, 30)
		return
	}
	base := s.addonBase(r)
	key := r.PathValue("key")
	streams := []addonStream{}
	if ref.Kind == "tv" {
		streams = append(streams, addonStream{Name: "Usherr Live TV", Title: fmt.Sprintf("Channel %d", ref.Channel), URL: fmt.Sprintf("%s/api/tvfeed/%s/stream/%d.ts", base, s.tvFeedKey(), ref.Channel), BehaviorHints: map[string]any{"notWebReady": true}})
		addonJSON(w, map[string]any{"streams": streams}, 0)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	// The library's own file first.
	if kind, id, quality := s.addonLibraryFile(ctx, ref); kind != "" {
		label := "Your library"
		if quality != "" {
			label += " · " + quality
		}
		streams = append(streams, addonStream{Name: "Usherr\nLibrary", Title: label, Description: label, URL: fmt.Sprintf("%s/addon/%s/file/%s/%d", base, key, kind, id), BehaviorHints: map[string]any{"bingeGroup": "usherr-library"}})
	}
	// Then what the streaming engine finds, best first.
	if s.ownProblem() == "" {
		if tmdb, err := s.addonTMDB(ctx, ref); err == nil {
			// Usherr: what it is, for the Marquee while an app plays it.
			what := usherrPlaying{userID: u.ID, kind: "movie", tmdbID: tmdb}
			if ref.Kind == "series" {
				what = usherrPlaying{userID: u.ID, kind: "episode", showID: tmdb, season: ref.Season, episode: ref.Episode}
			}
			if q, err := s.ownTitleQuery(ctx, ref.Kind, tmdb, ref.Season, ref.Episode, "4K"); err == nil {
				if list, err := s.ownCandidates(ctx, q); err == nil {
					if len(list) > addonStreams {
						list = list[:addonStreams]
					}
					for i, res := range list {
						rest := append(append([]indexers.Result(nil), list[i+1:]...), list[:i]...)
						token := randomID("ao_")
						addonCandMu.Lock()
						seen := what
						seen.release = res.Title
						if seen.kind == "episode" {
							seen.series = parser.Parse(res.Title).Title
						}
						addonCands[token] = &addonCand{res: res, alts: rest, userID: u.ID, at: time.Now(), what: seen}
						addonCandMu.Unlock()
						pic := "Usenet"
						if h := heightOf(res.Title); h > 0 {
							pic = map[int]string{2160: "4K", 1080: "1080p", 720: "720p", 480: "480p"}[h]
						}
						var bits []string
						if res.SizeBytes > 0 {
							bits = append(bits, fmt.Sprintf("%.1f GB", float64(res.SizeBytes)/(1<<30)))
						}
						if res.IndexerName != "" {
							bits = append(bits, res.IndexerName)
						}
						desc := res.Title
						if len(bits) > 0 {
							desc += "\n" + strings.Join(bits, " · ")
						}
						streams = append(streams, addonStream{Name: "Usherr\n" + pic, Title: desc, Description: desc, URL: fmt.Sprintf("%s/addon/%s/play/%s", base, key, token), BehaviorHints: map[string]any{"bingeGroup": "usherr-" + pic, "filename": res.Title, "videoSize": res.SizeBytes}})
					}
				}
			}
		}
	}
	addonJSON(w, map[string]any{"streams": streams}, 0)
}

// handleAddonPlay: GET /addon/<key>/play/<token>: opens the release (the
// first time) and plays it, with seeking. If it can't be opened the next
// release in line is tried.
func (s *Server) handleAddonPlay(w http.ResponseWriter, r *http.Request) {
	u, ok := s.addonUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	addonCandMu.Lock()
	c := addonCands[r.PathValue("token")]
	addonCandMu.Unlock()
	if c == nil || c.userID != u.ID {
		addonCORS(w)
		http.Error(w, "This stream has expired. Open the title again.", http.StatusNotFound)
		return
	}
	c.mu.Lock()
	ustreamMu.Lock()
	_, open := ustreamSessions[c.session]
	ustreamMu.Unlock()
	if c.session == "" || !open {
		ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
		op, err := s.ownOpenFirst(ctx, append([]indexers.Result{c.res}, c.alts...))
		cancel()
		if err != nil {
			c.mu.Unlock()
			addonCORS(w)
			http.Error(w, "Usherr couldn't start that: "+ownOpenError(err), http.StatusBadGateway)
			return
		}
		c.session = op.token
	}
	token := c.session
	what := c.what
	c.mu.Unlock()
	// Usherr: on the Marquee while the app reads it.
	ustreamMu.Lock()
	var size int64
	if sess, ok := ustreamSessions[token]; ok {
		size = sess.file.Size
	}
	ustreamMu.Unlock()
	addonPlaying(r.PathValue("token"), what, rangeStart(r), size)
	addonCORS(w)
	s.serveUStream(w, r, token)
}

// handleAddonFile: GET /addon/<key>/file/<movie|episode>/<id>: a library video.
func (s *Server) handleAddonFile(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.addonUser(r); !ok {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	kind := r.PathValue("kind")
	if err != nil || (kind != "movie" && kind != "episode") {
		http.NotFound(w, r)
		return
	}
	file, _, err := s.playFile(r, kind, id)
	if err != nil || file == "" {
		http.NotFound(w, r)
		return
	}
	// Usherr: on the Marquee while the app reads it.
	if u, ok := s.addonUser(r); ok {
		what := usherrPlaying{userID: u.ID}
		if s.usherrPlayingFromLibrary(kind, id, &what) {
			var size int64
			if fi, err := os.Stat(file); err == nil {
				size = fi.Size()
			}
			addonPlaying(kind+"-"+strconv.FormatInt(id, 10)+"-"+strconv.FormatInt(u.ID, 10), what, rangeStart(r), size)
		}
	}
	addonCORS(w)
	serveLibraryVideo(w, r, file, s.libraryRoots())
}

// ---- subtitles ----

type addonSub struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Lang string `json:"lang"`
}

// handleAddonSubtitles: GET /addon/<key>/subtitles/<type>/<id>[/<extra>].json:
// the text subtitles of the library's file.
func (s *Server) handleAddonSubtitles(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.addonUser(r); !ok {
		http.NotFound(w, r)
		return
	}
	idPart, _, _ := strings.Cut(r.PathValue("rest"), "/")
	ref, ok := parseAddonID(r.PathValue("type"), strings.TrimSuffix(idPart, ".json"))
	subs := []addonSub{}
	if !ok {
		addonJSON(w, map[string]any{"subtitles": subs}, 30)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if kind, id, _ := s.addonLibraryFile(ctx, ref); kind != "" {
		if file, _, err := s.playFile(r, kind, id); err == nil && file != "" {
			if tracks, err := probeTracks(ctx, file); err == nil {
				base := s.addonBase(r)
				for _, t := range tracks {
					if t.Kind != "text" || t.Key == "" {
						continue
					}
					lang := t.Lang
					if lang == "" {
						lang = "und"
					}
					subs = append(subs, addonSub{ID: fmt.Sprintf("%s-%d-%s", kind, id, t.Key), URL: fmt.Sprintf("%s/addon/%s/subs/%s/%d?key=%s", base, r.PathValue("key"), kind, id, url.QueryEscape(t.Key)), Lang: lang})
				}
			}
		}
	}
	addonJSON(w, map[string]any{"subtitles": subs}, 300)
}

// handleAddonSubs: GET /addon/<key>/subs/<kind>/<id>?key=: one subtitle as WebVTT.
func (s *Server) handleAddonSubs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.addonUser(r); !ok {
		http.NotFound(w, r)
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	file, _, err := s.playFile(r, r.PathValue("kind"), id)
	if err != nil || file == "" {
		http.NotFound(w, r)
		return
	}
	tracks, err := probeTracks(r.Context(), file)
	if err != nil {
		http.Error(w, "unreadable", http.StatusInternalServerError)
		return
	}
	var tr *playTrack
	for i := range tracks {
		if tracks[i].Key == r.URL.Query().Get("key") && tracks[i].Kind == "text" {
			tr = &tracks[i]
		}
	}
	if tr == nil {
		http.NotFound(w, r)
		return
	}
	vtt, _, err := subtitleVTT(r.Context(), file, *tr, tracks)
	if err != nil {
		http.Error(w, "unreadable", http.StatusInternalServerError)
		return
	}
	addonCORS(w)
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	_, _ = w.Write(vtt)
}

// ---- the setup page's side ----

// addonInfo is what the setup card shows: the person's address for the apps.
func (s *Server) addonInfo(r *http.Request, key string) (map[string]any, error) {
	base := strings.TrimRight(strings.TrimSpace(r.URL.Query().Get("base")), "/")
	if base != "" {
		u, err := url.Parse(base)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return nil, errors.New("The address must look like http://192.168.1.20:8264.")
		}
		base = u.Scheme + "://" + u.Host
	} else if addr, _ := s.Settings.Get(keyHomeAddress); strings.HasPrefix(strings.TrimSpace(addr), "http") {
		base = strings.TrimRight(strings.TrimSpace(addr), "/")
	} else {
		base = s.addonBase(r)
	}
	manifest := base + "/addon/" + key + "/manifest.json"
	out := map[string]any{"key": key, "base": base, "manifestUrl": manifest, "https": strings.HasPrefix(base, "https://")}
	if code, err := qr.Encode([]byte(manifest)); err == nil {
		out["qr"] = code.Rows()
	}
	if strings.HasPrefix(base, "https://") {
		out["stremioUrl"] = "stremio://" + strings.TrimPrefix(manifest, "https://")
	}
	if p := s.ownProblem(); p != "" {
		out["streamsProblem"] = p
	}
	return out, nil
}

// GET /api/addon[?base=]: the person's add-on address, made on first use.
func (s *Server) handleGetAddon(w http.ResponseWriter, r *http.Request) {
	key, err := s.addonKeyOf(r.Context(), requestUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := s.addonInfo(r, key)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/addon/regenerate: a new key; the old address stops working.
func (s *Server) handleRegenerateAddon(w http.ResponseWriter, r *http.Request) {
	key, err := s.addonNewKey(r.Context(), requestUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := s.addonInfo(r, key)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
