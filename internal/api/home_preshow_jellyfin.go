package api

// Usherr: the pre-show in the Jellyfin app. Usherr answers the NeXroll
// Intros plugin as a NeXroll server would (GET /plugin/intros), so the
// plugin, pointed at Usherr, puts the same pre-show before a film in every
// Jellyfin app: the cards made into short videos (home_preshow_cards.go) and
// the clips, each fetched through a signed link. While Jellyfin plays them
// the Marquee follows along. Settings → Movie night → Pre-show sets it up:
// the plugin's repository, the plugin, its settings and a restart, when
// nobody is watching.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

const (
	keyPreshowJFKey      = "home.preshow.jfKey"      // the plugin's API key
	keyPreshowJFAddress  = "home.preshow.jfAddress"  // Usherr as Jellyfin reaches it
	keyPreshowJFMovies   = "home.preshow.jfMovies"   // before films ("0" for not; on by default)
	keyPreshowJFEpisodes = "home.preshow.jfEpisodes" // before episodes ("1"; off by default)
	keyPreshowJFTheme    = "home.preshow.jfTheme"    // the theme; 0 as anywhere else

	nexrollPluginGUID = "a1b2c3d4-e5f6-7890-abcd-ae0c01100001"
	nexrollPluginName = "NeXroll Intros"
	nexrollRepo12     = "https://raw.githubusercontent.com/JFLXCLOUD/NeXroll/main/Plugins/jellyfin/manifest.json"
	nexrollRepo1011   = "https://raw.githubusercontent.com/JFLXCLOUD/NeXroll/main/Plugins/jellyfin/manifest-10.11.json"

	preshowIntroPrefix = "Usherr pre-show: " // the intros' names in Jellyfin
)

func (s *Server) preshowRoutes(root *http.ServeMux) {
	root.HandleFunc("GET /plugin/intros", s.handlePluginIntros)
	root.HandleFunc("GET /plugin/stream/{token}", s.handlePluginStream)
}

// ---- settings ----

type preshowJF struct {
	Key      string `json:"key"`
	Address  string `json:"address"`
	Movies   bool   `json:"movies"`
	Episodes bool   `json:"episodes"`
	ThemeID  int64  `json:"themeId"`
}

func (s *Server) preshowJFNow() preshowJF {
	key, _ := s.Settings.Get(keyPreshowJFKey)
	addr, _ := s.Settings.Get(keyPreshowJFAddress)
	mov, _ := s.Settings.Get(keyPreshowJFMovies)
	ep, _ := s.Settings.Get(keyPreshowJFEpisodes)
	th, _ := s.Settings.Get(keyPreshowJFTheme)
	theme, _ := strconv.ParseInt(th, 10, 64)
	return preshowJF{Key: key, Address: strings.TrimSpace(addr), Movies: mov != "0", Episodes: ep == "1", ThemeID: theme}
}

// preshowJFKeyEnsure makes the plugin's key the first time it's wanted.
func (s *Server) preshowJFKeyEnsure() (string, error) {
	if k, _ := s.Settings.Get(keyPreshowJFKey); len(k) == 32 {
		return k, nil
	}
	return s.preshowJFNewKey()
}

func (s *Server) preshowJFNewKey() (string, error) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	k := hex.EncodeToString(b)
	return k, s.Settings.Set(keyPreshowJFKey, k, false)
}

// ---- what Jellyfin last asked for ----

type preshowJFAsk struct {
	AtMs  int64  `json:"atMs"`
	Title string `json:"title"`
	Steps int    `json:"steps"`
	Note  string `json:"note,omitempty"`
}

var preshowJFLast struct {
	sync.Mutex
	ask       preshowJFAsk
	installed time.Time // the plugin was installed (Jellyfin lists it after a restart)
}

func preshowJFNote(a preshowJFAsk) {
	a.AtMs = time.Now().UnixMilli()
	preshowJFLast.Lock()
	preshowJFLast.ask = a
	preshowJFLast.Unlock()
}

// ---- the plugin's calls ----

type pluginIntro struct {
	Path      string `json:"Path"`
	Name      string `json:"Name"`
	StreamURL string `json:"StreamUrl"`
	Version   string `json:"Version"`
}

// GET /plugin/intros?media_type=Movie|Episode&item_id=<Jellyfin's id>
// (X-Api-Key): what plays before the item, in order.
func (s *Server) handlePluginIntros(w http.ResponseWriter, r *http.Request) {
	set := s.preshowJFNow()
	got := r.Header.Get("X-Api-Key")
	if len(set.Key) != 32 || subtle.ConstantTimeCompare([]byte(got), []byte(set.Key)) != 1 {
		writeError(w, http.StatusUnauthorized, "That key isn't Usherr's pre-show key.")
		return
	}
	none := func(note string) {
		preshowJFNote(preshowJFAsk{Note: note})
		writeJSON(w, http.StatusOK, map[string]any{"Items": []pluginIntro{}, "TotalRecordCount": 0, "Mode": "sequence", "Exact": true})
	}
	q := r.URL.Query()
	kind := strings.ToLower(q.Get("media_type"))
	if (kind == "movie" && !set.Movies) || (kind == "episode" && !set.Episodes) || (kind != "movie" && kind != "episode") {
		none("The pre-show is off for " + kind + "s.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	srv, ok := s.jellyfinServer()
	if !ok {
		none(connectServerFirst)
		return
	}
	film := s.preshowJFFilm(ctx, srv, kind, q.Get("item_id"))
	film.Files = true
	plan := s.planPreshow(ctx, film, set.ThemeID, 0, time.Now())
	base := preshowBaseURL(r, s.cfg.URLBase)
	items := []pluginIntro{}
	var cards []string
	var names []preshowJFStep
	for i, st := range plan.Steps {
		var it pluginIntro
		switch st.Kind {
		case "clip":
			id := strings.TrimSuffix(strings.TrimPrefix(st.URL, "/api/home/preshow/clips/"), "/file")
			c, ok := s.preshowClipByID(ctx, id)
			if !ok {
				continue
			}
			p, ok := s.preshowClipFile(c)
			if !ok {
				continue
			}
			fi, err := os.Stat(p)
			if err != nil || time.Since(fi.ModTime()) < preshowSettle { // gone, or still being written
				continue
			}
			it = pluginIntro{Path: "usherr/clip/" + id + filepath.Ext(p), Name: preshowIntroPrefix + st.Title,
				StreamURL: base + "/plugin/stream/" + preshowSignToken(set.Key, "clip", id), Version: fmt.Sprintf("%d-%d", fi.Size(), fi.ModTime().Unix())}
		case "trailer": // a downloaded one (YouTube's are left out for Jellyfin)
			id := strings.TrimSuffix(strings.TrimPrefix(st.URL, "/api/home/preshow/trailers/"), "/file")
			t, ok := s.trailerByID(ctx, id)
			if !ok || t.State != "ready" {
				continue
			}
			fi, err := os.Stat(filepath.Join(s.trailersDir(), t.File))
			if err != nil {
				continue
			}
			it = pluginIntro{Path: "usherr/trailer/" + id + ".mp4", Name: preshowIntroPrefix + "Coming soon: " + t.Title,
				StreamURL: base + "/plugin/stream/" + preshowSignToken(set.Key, "trailer", id), Version: fmt.Sprintf("%d-%d", fi.Size(), fi.ModTime().Unix())}
		case "card":
			h := preshowCardRemember(s.preshowCardFor(st, film))
			cards = append(cards, h)
			name := preshowCardName[st.Card]
			if st.Card == "soon" {
				name += ": " + st.Title
			}
			it = pluginIntro{Path: "usherr/card/" + h + ".mp4", Name: preshowIntroPrefix + name,
				StreamURL: base + "/plugin/stream/" + preshowSignToken(set.Key, "card", h), Version: h}
		default:
			continue
		}
		// Each its own name, so the Marquee knows which is playing.
		it.Name += " (" + strconv.Itoa(i+1) + ")"
		items = append(items, it)
		names = append(names, preshowJFStep{name: it.Name, step: st})
	}
	s.recordPlayed(ctx, plan)
	preshowJFNote(preshowJFAsk{Title: film.Title, Steps: len(items)})
	slog.Info("preshow: Jellyfin asked", "kind", kind, "title", film.Title, "steps", len(items), "theme", plan.Theme.Name)
	// The cards are made now, ahead of the plugin fetching them.
	go func() {
		bg, done := context.WithTimeout(context.Background(), 3*time.Minute)
		defer done()
		for _, h := range cards {
			if _, err := s.preshowCardFile(bg, h); err != nil {
				slog.Warn("preshow: card", "err", err)
			}
		}
	}()
	if len(names) > 0 {
		go s.preshowJFFollow(srv, film, names)
		go preshowJFWarm(srv, q.Get("item_id")) // the film gets ready while the pre-show plays
	}
	writeJSON(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": len(items), "Mode": "sequence", "Exact": true})
}

var preshowCardName = map[string]string{
	"welcome": "Welcome", "popcorn": "Grab your popcorn", "silence": "Please silence your phones", "coming": "Coming attractions",
	"rating": "The rating", "countdown": "Countdown", "feature": "Feature presentation",
	"soon": "Coming soon", "reel": "Coming soon reel", "announce": "Announcement", "qr": "Scan for Usherr",
}

// preshowBaseURL is Usherr's address as the caller reached it.
func preshowBaseURL(r *http.Request, urlBase string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + strings.TrimRight(urlBase, "/")
}

// preshowJFFilm is what Jellyfin knows of the item, with Usherr's library
// and TMDB on top.
func (s *Server) preshowJFFilm(ctx context.Context, srv mediaservers.Server, kind, itemID string) preshowFilm {
	var res struct {
		Items []struct {
			Name           string
			SeriesName     string
			OfficialRating string
			Genres         []string
			ProviderIds    map[string]string
			MediaStreams   []struct {
				Type           string
				Height         int
				DisplayTitle   string
				Profile        string
				VideoRangeType string
			}
		}
	}
	var name, rated string
	var genres []string
	var tmdbID int
	var resolution, hdr, sound string
	if itemID != "" && len(itemID) <= 64 {
		if jfJSON(ctx, srv, http.MethodGet, "/Items?Ids="+url.QueryEscape(itemID)+"&Fields=ProviderIds,Genres,OfficialRating,MediaStreams", nil, &res) == nil && len(res.Items) > 0 {
			it := res.Items[0]
			name, rated, genres = it.Name, it.OfficialRating, it.Genres
			if kind == "episode" && it.SeriesName != "" {
				name = it.SeriesName
			}
			tmdbID, _ = strconv.Atoi(it.ProviderIds["Tmdb"])
			for _, ms := range it.MediaStreams {
				switch ms.Type {
				case "Video":
					if ms.Height >= 1600 {
						resolution = "4K"
					}
					switch v := strings.ToUpper(ms.VideoRangeType); {
					case strings.Contains(v, "DOVI"):
						hdr = "Dolby Vision"
					case strings.Contains(v, "HDR"):
						hdr = "HDR10"
					}
				case "Audio":
					if sound == "" {
						if k := soundKey(ms.DisplayTitle + " " + ms.Profile); k != "" {
							sound = ms.DisplayTitle + " " + ms.Profile
						}
					}
				}
			}
		}
	}
	var libraryID int64
	if kind == "movie" && tmdbID > 0 {
		if m, ok, err := s.MovieRepo.GetByTMDBID(tmdbID); err == nil && ok && m.FilePath != "" {
			libraryID = m.ID
		}
	}
	movieTMDB := tmdbID
	if kind != "movie" {
		movieTMDB = 0
	}
	f := s.preshowFilmFor(ctx, kind, libraryID, movieTMDB, "")
	f.Title = firstNonEmptyStr(f.Title, name)
	if f.Rated == "" {
		f.Rated = rated
	}
	if len(f.Genres) == 0 {
		f.Genres = genres
	}
	// The copy Jellyfin plays decides the sound and picture (the library's
	// may be another release of the film).
	if sound != "" {
		f.Sound = sound
	}
	if resolution != "" || hdr != "" {
		imax := false
		for _, p := range f.Picture {
			imax = imax || p == "imax"
		}
		f.Picture = pictureOf(resolution, hdr, nil)
		if imax {
			f.Picture = append(f.Picture, "imax")
		}
	}
	return f
}

// ---- signed links ----

func preshowSignToken(key, kind, id string) string {
	return kind + "-" + id + "-" + liveSign(key, "preshow:"+kind+":"+id)
}

// GET /plugin/stream/{token}: a clip or a card, for the plugin to fetch.
func (s *Server) handlePluginStream(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(r.PathValue("token"), "-", 3)
	key := s.preshowJFNow().Key
	if len(parts) != 3 || len(key) != 32 || subtle.ConstantTimeCompare([]byte(parts[2]), []byte(liveSign(key, "preshow:"+parts[0]+":"+parts[1]))) != 1 {
		http.NotFound(w, r)
		return
	}
	var file string
	switch parts[0] {
	case "clip":
		c, ok := s.preshowClipByID(r.Context(), parts[1])
		if !ok {
			http.NotFound(w, r)
			return
		}
		if file, ok = s.preshowClipFile(c); !ok {
			http.NotFound(w, r)
			return
		}
	case "trailer":
		t, ok := s.trailerByID(r.Context(), parts[1])
		if !ok || t.State != "ready" || t.File == "" {
			http.NotFound(w, r)
			return
		}
		file = filepath.Join(s.trailersDir(), filepath.Base(t.File))
	case "card":
		var err error
		if file, err = s.preshowCardFile(r.Context(), parts[1]); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(file)
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
	ct := preshowVideoExt[strings.ToLower(filepath.Ext(file))]
	if ct == "" {
		ct = "video/mp4"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// ---- the film gets ready ----

// preshowJFWarm reads the start and the end of the film through Jellyfin
// (where an MKV keeps its index), so a streamed one (nzbdav's, or a .strm
// of the streamed library) is fetching while the pre-show plays. Jellyfin
// opens the film's file only when its turn comes.
func preshowJFWarm(srv mediaservers.Server, itemID string) {
	if itemID == "" || len(itemID) > 64 || strings.ContainsAny(itemID, "/?&#") {
		return
	}
	time.Sleep(3 * time.Second) // the plugin fetches the pre-show first
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	id := url.PathEscape(itemID)
	path := "/Videos/" + id + "/stream?static=true&mediaSourceId=" + id
	read := func(rng string) (total int64) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(srv.BaseURL, "/")+path, nil)
		if err != nil {
			return 0
		}
		req.Header.Set("X-Emby-Token", srv.Token)
		req.Header.Set("Range", rng)
		resp, err := streamRelayClient.Do(req)
		if err != nil {
			slog.Info("preshow: film not warmed", "err", err)
			return 0
		}
		defer resp.Body.Close()
		n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<20))
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if i := strings.LastIndexByte(cr, '/'); i >= 0 {
				total, _ = strconv.ParseInt(cr[i+1:], 10, 64)
			}
		}
		slog.Info("preshow: film warming", "range", rng, "status", resp.StatusCode, "read", n)
		return total
	}
	start := time.Now()
	if total := read("bytes=0-4194303"); total > 8<<20 {
		read(fmt.Sprintf("bytes=%d-", total-2<<20))
	}
	slog.Info("preshow: film warmed", "took", time.Since(start).Round(100*time.Millisecond))
}

// ---- the Marquee follows Jellyfin ----

type preshowJFStep struct {
	name string
	step preshowStep
}

// preshowJFFollow watches what Jellyfin plays for a while and tells the
// Marquee each piece of the pre-show as it starts, and its end.
func (s *Server) preshowJFFollow(srv mediaservers.Server, film preshowFilm, steps []preshowJFStep) {
	byName := map[string]preshowStep{}
	total := 0.0
	for _, st := range steps {
		byName[st.name] = st.step
		total += st.step.Seconds
	}
	deadline := time.Now().Add(time.Duration(total+240) * time.Second)
	shown, started := "", false
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		playing, err := s.mediaClient.NowPlaying(ctx, srv)
		cancel()
		if err != nil {
			continue
		}
		now := ""
		for _, p := range playing {
			if _, ok := byName[p.Title]; ok {
				now = p.Title
				break
			}
		}
		if now != "" && now != shown {
			st := byName[now]
			s.preshowTell(preshowNowMsg{Kind: st.Kind, Card: st.Card, Title: st.Title, PosterURL: st.PosterURL, BackdropURL: st.BackdropURL,
				FilmTitle: film.Title, FilmPoster: film.PosterURL, Seconds: st.Seconds})
			shown, started = now, true
		}
		if now == "" && started {
			break // on to the film (or stopped)
		}
	}
	if started {
		s.preshowTell(preshowNowMsg{End: true})
	}
}

// ---- the admin's page: setting it up ----

type preshowJFState struct {
	preshowJF
	Suggested   string        `json:"suggested"` // the address, if none is set
	Server      string        `json:"server"`    // Jellyfin's name and version
	Connected   bool          `json:"connected"` // Jellyfin is connected to Usherr
	Repo        bool          `json:"repo"`      // the plugin's repository is added
	Plugin      string        `json:"plugin"`    // "", "installed" (restart), "active", or Jellyfin's word for it
	PluginVer   string        `json:"pluginVersion,omitempty"`
	Configured  bool          `json:"configured"` // the plugin points at Usherr with the key
	Watching    int           `json:"watching"`   // people playing something now
	LastAsk     *preshowJFAsk `json:"lastAsk,omitempty"`
	Problem     string        `json:"problem,omitempty"`
	pluginID    string
	pluginOn    bool
	repoURL     string
	repoList    []jfRepo
	serverMajor int
}

type jfRepo struct {
	Name    string `json:"Name"`
	URL     string `json:"Url"`
	Enabled bool   `json:"Enabled"`
}

type nexrollConfig struct {
	NexrollUrl        string `json:"NexrollUrl"`
	ApiKey            string `json:"ApiKey"`
	PathPrefixFrom    string `json:"PathPrefixFrom"`
	PathPrefixTo      string `json:"PathPrefixTo"`
	EnableForMovies   bool   `json:"EnableForMovies"`
	EnableForEpisodes bool   `json:"EnableForEpisodes"`
	MaxIntros         int    `json:"MaxIntros"`
	TimeoutSeconds    int    `json:"TimeoutSeconds"`
}

func (s *Server) preshowJFState(ctx context.Context, r *http.Request) preshowJFState {
	st := preshowJFState{preshowJF: s.preshowJFNow()}
	if st.Key == "" {
		st.Key, _ = s.preshowJFKeyEnsure()
	}
	st.Suggested = preshowBaseURL(r, s.cfg.URLBase)
	preshowJFLast.Lock()
	if preshowJFLast.ask.AtMs > 0 {
		a := preshowJFLast.ask
		st.LastAsk = &a
	}
	preshowJFLast.Unlock()
	srv, ok := s.jellyfinServer()
	if !ok || srv.Kind != mediaservers.KindJellyfin {
		st.Problem = "Connect Jellyfin under Settings > Connections > Media servers first."
		return st
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var info struct {
		ServerName string
		Version    string
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/System/Info", nil, &info); err != nil {
		st.Problem = err.Error()
		return st
	}
	st.Connected = true
	st.Server = strings.TrimSpace(info.ServerName + " (Jellyfin " + info.Version + ")")
	st.serverMajor, _ = strconv.Atoi(strings.SplitN(info.Version, ".", 2)[0])
	st.repoURL = nexrollRepo12
	if st.serverMajor > 0 && st.serverMajor < 12 {
		st.repoURL = nexrollRepo1011
	}
	if jfJSON(ctx, srv, http.MethodGet, "/Repositories", nil, &st.repoList) == nil {
		for _, rp := range st.repoList {
			if strings.Contains(rp.URL, "JFLXCLOUD/NeXroll") && rp.Enabled {
				st.Repo = true
			}
		}
	}
	var plugins []struct {
		Name    string
		Version string
		Id      string
		Status  string
	}
	if jfJSON(ctx, srv, http.MethodGet, "/Plugins", nil, &plugins) == nil {
		for _, p := range plugins {
			if strings.EqualFold(strings.ReplaceAll(p.Id, "-", ""), strings.ReplaceAll(nexrollPluginGUID, "-", "")) || p.Name == nexrollPluginName {
				st.pluginID, st.PluginVer = p.Id, p.Version
				st.Plugin = strings.ToLower(p.Status)
				st.pluginOn = p.Status == "Active"
			}
		}
	}
	preshowJFLast.Lock()
	installed := preshowJFLast.installed
	preshowJFLast.Unlock()
	if st.Plugin == "" && time.Since(installed) < 24*time.Hour {
		st.Plugin = "installed"
	}
	if st.Plugin == "" {
		var installing []struct{ Name string }
		if jfJSON(ctx, srv, http.MethodGet, "/Packages/Installing", nil, &installing) == nil {
			for _, p := range installing {
				if p.Name == nexrollPluginName {
					st.Plugin = "installing"
				}
			}
		}
	}
	if st.pluginOn {
		var cfg nexrollConfig
		if jfJSON(ctx, srv, http.MethodGet, "/Plugins/"+url.PathEscape(st.pluginID)+"/Configuration", nil, &cfg) == nil {
			addr := firstNonEmptyStr(st.Address, st.Suggested)
			st.Configured = strings.TrimRight(cfg.NexrollUrl, "/") == strings.TrimRight(addr, "/") && cfg.ApiKey == st.Key
		}
	}
	if playing, err := s.mediaClient.NowPlaying(ctx, srv); err == nil {
		st.Watching = len(playing)
	}
	return st
}

// GET /api/home/preshow/jellyfin (admin)
func (s *Server) handlePreshowJF(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.preshowJFState(r.Context(), r))
}

// PUT /api/home/preshow/jellyfin (admin): the address, films, episodes, theme.
func (s *Server) handlePutPreshowJF(w http.ResponseWriter, r *http.Request) {
	var b preshowJF
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	b.Address = strings.TrimRight(strings.TrimSpace(b.Address), "/")
	if b.Address != "" {
		u, err := url.Parse(b.Address)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeError(w, http.StatusBadRequest, "The address is http:// or https://, then Usherr's host and port.")
			return
		}
	}
	bit := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	for k, v := range map[string]string{keyPreshowJFAddress: b.Address, keyPreshowJFMovies: bit(b.Movies), keyPreshowJFEpisodes: bit(b.Episodes), keyPreshowJFTheme: strconv.FormatInt(max(b.ThemeID, 0), 10)} {
		if err := s.Settings.Set(k, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
			return
		}
	}
	s.preshowJFConfigure(r.Context(), r) // the plugin, if it's there, gets the new address
	writeJSON(w, http.StatusOK, s.preshowJFState(r.Context(), r))
}

// POST /api/home/preshow/jellyfin/key (admin): a new key (the old stops working).
func (s *Server) handlePreshowJFKey(w http.ResponseWriter, r *http.Request) {
	if _, err := s.preshowJFNewKey(); err != nil {
		writeError(w, http.StatusInternalServerError, "A new key couldn't be made.")
		return
	}
	s.preshowJFConfigure(r.Context(), r)
	writeJSON(w, http.StatusOK, s.preshowJFState(r.Context(), r))
}

// preshowJFConfigure points the plugin at Usherr, when it's running.
func (s *Server) preshowJFConfigure(ctx context.Context, r *http.Request) error {
	st := s.preshowJFState(ctx, r)
	if !st.pluginOn {
		return errors.New("the plugin isn't running yet")
	}
	srv, _ := s.jellyfinServer()
	cfg := nexrollConfig{NexrollUrl: firstNonEmptyStr(st.Address, st.Suggested), ApiKey: st.Key, EnableForMovies: true, EnableForEpisodes: true, TimeoutSeconds: 20}
	return jfJSON(ctx, srv, http.MethodPost, "/Plugins/"+url.PathEscape(st.pluginID)+"/Configuration", cfg, nil)
}

// POST /api/home/preshow/jellyfin/setup (admin): the next step of setting it
// up: the repository and the plugin, then (after a restart) its settings.
func (s *Server) handlePreshowJFSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st := s.preshowJFState(ctx, r)
	if !st.Connected {
		writeError(w, http.StatusConflict, firstNonEmptyStr(st.Problem, "Jellyfin isn't reachable."))
		return
	}
	srv, _ := s.jellyfinServer()
	fail := func(what string, err error) {
		slog.Warn("preshow: Jellyfin setup", "step", what, "err", err)
		writeError(w, http.StatusBadGateway, "Jellyfin didn't "+what+": "+err.Error())
	}
	if !st.Repo {
		list := append([]jfRepo{}, st.repoList...)
		found := false
		for i := range list {
			if strings.Contains(list[i].URL, "JFLXCLOUD/NeXroll") {
				list[i].URL, list[i].Enabled, found = st.repoURL, true, true
			}
		}
		if !found {
			list = append(list, jfRepo{Name: nexrollPluginName, URL: st.repoURL, Enabled: true})
		}
		if err := jfJSON(ctx, srv, http.MethodPost, "/Repositories", list, nil); err != nil {
			fail("add the plugin's repository", err)
			return
		}
	}
	switch {
	case st.Plugin == "":
		q := url.Values{"assemblyGuid": {nexrollPluginGUID}, "repositoryUrl": {st.repoURL}}
		if err := jfJSON(ctx, srv, http.MethodPost, "/Packages/Installed/"+url.PathEscape(nexrollPluginName)+"?"+q.Encode(), nil, nil); err != nil {
			fail("install the plugin", err)
			return
		}
		slog.Info("preshow: NeXroll Intros plugin installing in Jellyfin")
		preshowJFLast.Lock()
		preshowJFLast.installed = time.Now()
		preshowJFLast.Unlock()
	case st.pluginOn:
		if err := s.preshowJFConfigure(ctx, r); err != nil {
			fail("take the plugin's settings", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.preshowJFState(ctx, r))
}

// POST /api/home/preshow/jellyfin/restart (admin): restarts Jellyfin to
// load the plugin, only when nobody is playing anything.
func (s *Server) handlePreshowJFRestart(w http.ResponseWriter, r *http.Request) {
	srv, ok := s.jellyfinServer()
	if !ok {
		writeError(w, http.StatusConflict, connectServerFirst)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	playing, err := s.mediaClient.NowPlaying(ctx, srv)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if len(playing) > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("%d playing on Jellyfin now; restart it when nobody is watching.", len(playing)))
		return
	}
	if err := jfJSON(ctx, srv, http.MethodPost, "/System/Restart", nil, nil); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	slog.Info("preshow: restarting Jellyfin to load the plugin")
	w.WriteHeader(http.StatusNoContent)
}
