package api

// Home fork: Settings > Extras. Everything the fork adds that needs setting
// up (the media server it works with, Mediarium's address on the network,
// StreamNZB, the streamed library, Live TV, playing on a TV, Comics and
// NSFW 18+) in one place, each with whether it's ready and what's missing.
// Nothing here assumes a particular house: an extra stays off until it has
// what it needs.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/directdl"
	"github.com/rdborg/mediarium/internal/fsinfo"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

const (
	keyHomeAddress = "home.address" // Mediarium's address on the network, e.g. http://192.168.1.20:8264
	animeRoot      = "/data/anime"  // anime shows, filed like TV
)

var errNoAddress = errors.New("Mediarium's address on your network isn't set (Settings > Extras)")

func (s *Server) registerExtrasRoutes(admin routeGroup) {
	admin.HandleFunc("GET /api/home/extras", s.handleGetExtras)
	admin.HandleFunc("PUT /api/home/extras", s.handlePutExtras)
	admin.HandleFunc("GET /api/home/streaming", s.handleGetStreamingEngine) // Usherr: the streaming engine
	admin.HandleFunc("PUT /api/home/streaming", s.handlePutStreamingEngine)
	admin.HandleFunc("POST /api/home/streaming/import/preview", s.handleSNZBImportPreview) // Usherr: from StreamNZB
	admin.HandleFunc("POST /api/home/streaming/import", s.handleSNZBImport)
	admin.HandleFunc("POST /api/home/extras/test/{what}", s.handleTestExtra)
	admin.HandleFunc("POST /api/home/extras/tv-pair", s.handlePairTV)
}

// mediariumAddress is the address other devices reach Mediarium at: the
// setting made for one use (if any), else the general one. "" if unset.
func (s *Server) mediariumAddress(specific string) string {
	for _, k := range []string{specific, keyHomeAddress} {
		if k == "" {
			continue
		}
		if v, _ := s.Settings.Get(k); strings.TrimSpace(v) != "" {
			return strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	return ""
}

// feedAddress is the address the Live TV feeds are given out at: the one
// set, else the one this page was opened at, unless that is this computer
// only (localhost), which another device can't use.
func (s *Server) feedAddress(r *http.Request) string {
	if a := s.mediariumAddress(keyTVFeedBase); a != "" {
		return a
	}
	if r == nil || isLoopbackHost(r.Host) {
		return ""
	}
	return tvFeedBase(r)
}

// isLoopbackHost reports whether host[:port] only means "this computer".
func isLoopbackHost(hostport string) bool {
	h := hostport
	if hh, _, err := net.SplitHostPort(hostport); err == nil {
		h = hh
	}
	h = strings.Trim(strings.ToLower(h), "[]")
	if h == "" || h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// streamedHostDir is the streamed folder as the media server sees it: the
// one set, else what the folder mapping of Settings > Media servers makes
// of it. "" when neither says.
func (s *Server) streamedHostDir(srv mediaservers.Server) string {
	if v, _ := s.Settings.Get(keyStreamedHostDir); strings.TrimSpace(v) != "" {
		return strings.TrimRight(strings.TrimSpace(v), `\/`)
	}
	inside := s.streamedSetting(keyStreamedDir, defaultStreamedDir)
	if mapped := mediaservers.MapPath(inside, srv.PathMap); mapped != inside {
		return strings.TrimRight(mapped, `\/`)
	}
	return ""
}

// joinHostPath adds a folder name to a path on the media server's computer,
// with that computer's kind of separator.
func joinHostPath(dir, name string) string {
	if strings.Contains(dir, `\`) && !strings.Contains(dir, "/") {
		return strings.TrimRight(dir, `\`) + `\` + name
	}
	return path.Join(dir, name)
}

type extrasPayload struct {
	Server struct {
		Kind string `json:"kind"` // jellyfin, emby, or "" (none connected)
		Name string `json:"name"`
		Plex bool   `json:"plex"` // a Plex server is connected (the extras can't use it)
	} `json:"server"`
	Address struct {
		Value     string `json:"value"`
		Suggested string `json:"suggested"`
	} `json:"address"`
	Streaming struct {
		URL        string `json:"url"`
		DefaultURL string `json:"defaultUrl"`
		TokenSet   bool   `json:"tokenSet"`
		TokenFile  bool   `json:"tokenFile"` // read from StreamNZB's own config
		Ready      bool   `json:"ready"`
		Problem    string `json:"problem,omitempty"`
	} `json:"streaming"`
	Streamed struct {
		Folder         string `json:"folder"`
		FolderOK       bool   `json:"folderOk"`
		FolderNote     string `json:"folderNote,omitempty"`
		ServerFolder   string `json:"serverFolder"`
		ServerFolderBy string `json:"serverFolderBy"` // "set", "mapping" or ""
		Titles         int    `json:"titles"`
	} `json:"streamed"`
	LiveTV struct {
		Channels int    `json:"channels"`
		Feed     string `json:"feed,omitempty"`
	} `json:"liveTv"`
	PlayOnTV struct {
		Address string `json:"address"`
		Name    string `json:"name"`
		ADB     bool   `json:"adb"`     // adb is in this image
		KeyMade bool   `json:"keyMade"` // Mediarium has its key for the TV to allow
		Ready   bool   `json:"ready"`
	} `json:"playOnTv"`
	Comics struct {
		Enabled bool `json:"enabled"`
		KeySet  bool `json:"keySet"`
	} `json:"comics"`
	NSFW struct {
		Enabled bool `json:"enabled"`
		Allowed bool `json:"allowed"` // Usherr: the server allows it
		KeySet  bool `json:"keySet"`
	} `json:"nsfw"`
	Direct struct {
		Enabled  bool   `json:"enabled"`
		FlareURL string `json:"flareUrl"`
	} `json:"direct"`
}

// GET /api/home/extras
func (s *Server) handleGetExtras(w http.ResponseWriter, r *http.Request) {
	var out extrasPayload
	srv, ok := s.jellyfinServer()
	if ok {
		out.Server.Kind, out.Server.Name = string(srv.Kind), srv.Name
	}
	if list, err := s.MediaServers.List(); err == nil {
		for _, m := range list {
			out.Server.Plex = out.Server.Plex || (m.Enabled && m.Kind == mediaservers.KindPlex)
		}
	}

	out.Address.Value = s.mediariumAddress("")
	if !isLoopbackHost(r.Host) {
		out.Address.Suggested = tvFeedBase(r)
	}

	snzb, _ := s.Settings.Get(keyStreamNZBURL)
	out.Streaming.URL, out.Streaming.DefaultURL = strings.TrimSpace(snzb), defaultStreamNZB
	tok, _ := s.Settings.Get(keyStreamNZBToken)
	out.Streaming.TokenSet = strings.TrimSpace(tok) != ""
	out.Streaming.TokenFile = !out.Streaming.TokenSet && tokenFromStreamNZBConfig(streamNZBConfig) != ""
	out.Streaming.Ready, out.Streaming.Problem = s.streamingCheck(r.Context())

	dir := s.streamedSetting(keyStreamedDir, defaultStreamedDir)
	f := fsinfo.Inspect(dir)
	out.Streamed.Folder = dir
	out.Streamed.FolderOK = f.Exists && f.Writable && (!f.MountKnown || f.Mounted)
	switch {
	case !f.Exists:
		out.Streamed.FolderNote = "This folder isn't in the container. Map a folder on your server to " + dir + "."
	case !f.Writable:
		out.Streamed.FolderNote = "Mediarium can't write to this folder."
	case f.MountKnown && !f.Mounted:
		out.Streamed.FolderNote = "This folder isn't mapped to your server, so your media server can't see it. Map a folder to " + dir + "."
	}
	if v, _ := s.Settings.Get(keyStreamedHostDir); strings.TrimSpace(v) != "" {
		out.Streamed.ServerFolder, out.Streamed.ServerFolderBy = strings.TrimSpace(v), "set"
	} else if ok {
		if hd := s.streamedHostDir(srv); hd != "" {
			out.Streamed.ServerFolder, out.Streamed.ServerFolderBy = hd, "mapping"
		}
	}
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM streamed_titles`).Scan(&out.Streamed.Titles)

	if chans, err := s.tvRepo().Channels(); err == nil {
		out.LiveTV.Channels = len(chans)
	}
	if base := s.feedAddress(r); base != "" {
		out.LiveTV.Feed = base + "/api/tvfeed/" + s.tvFeedKey() + "/channels.m3u"
	}

	out.PlayOnTV.Address, out.PlayOnTV.Name = s.tvAddress()
	_, err := exec.LookPath("adb")
	out.PlayOnTV.ADB = err == nil
	_, err = os.Stat(tvKeyDir + "/adbkey")
	out.PlayOnTV.KeyMade = err == nil
	out.PlayOnTV.Ready = s.tvReady()

	c := s.currentComicSettings()
	out.Comics.Enabled, out.Comics.KeySet = c.Enabled, c.KeySet
	out.NSFW.Enabled, out.NSFW.Allowed, out.NSFW.KeySet = s.nsfwEnabled(), adultAllowed(), s.stashKeySet()
	out.Direct.Enabled = s.getComicsOn()
	out.Direct.FlareURL, _ = s.Settings.Get(keyFlareURL)
	writeJSON(w, http.StatusOK, out)
}

// streamingCheck asks StreamNZB for its profiles, quickly.
func (s *Server) streamingCheck(ctx context.Context) (bool, string) {
	c, err := s.streamNZB()
	if err != nil {
		return false, err.Error()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := c.profiles(ctx); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// PUT /api/home/extras: only what is sent changes. "" clears a setting.
func (s *Server) handlePutExtras(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address        *string `json:"address"`
		StreamNZBURL   *string `json:"streamnzbUrl"`
		StreamNZBToken *string `json:"streamnzbToken"`
		RemoveToken    bool    `json:"removeStreamnzbToken"`
		ServerFolder   *string `json:"streamedServerFolder"`
		TVAddress      *string `json:"tvAddress"`
		TVName         *string `json:"tvName"`
		GetComics      *bool   `json:"getcomics"`
		FlareURL       *string `json:"flareUrl"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	resync := false
	if req.Address != nil {
		a, err := cleanBaseURL(*req.Address)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Mediarium's address "+err.Error())
			return
		}
		if a != "" && isLoopbackHost(hostOf(a)) {
			writeError(w, http.StatusBadRequest, "Use the address other devices reach Mediarium at, such as http://192.168.1.20:8264. localhost only means this computer.")
			return
		}
		before := s.mediariumAddress(keyStreamedBase)
		_ = s.Settings.Set(keyHomeAddress, a, false)
		// One address for everything: the Live TV feeds and the pointer files
		// follow it from now on.
		_ = s.Settings.Set(keyTVFeedBase, "", false)
		_ = s.Settings.Set(keyStreamedBase, "", false)
		resync = a != before // pointer files hold the address
	}
	if req.StreamNZBURL != nil {
		a, err := cleanBaseURL(*req.StreamNZBURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "StreamNZB's address "+err.Error())
			return
		}
		_ = s.Settings.Set(keyStreamNZBURL, a, false)
	}
	if req.RemoveToken {
		_ = s.Settings.Set(keyStreamNZBToken, "", false)
	} else if req.StreamNZBToken != nil && strings.TrimSpace(*req.StreamNZBToken) != "" {
		_ = s.Settings.Set(keyStreamNZBToken, strings.TrimSpace(*req.StreamNZBToken), true)
	}
	if req.ServerFolder != nil {
		_ = s.Settings.Set(keyStreamedHostDir, strings.TrimRight(strings.TrimSpace(*req.ServerFolder), `\/`), false)
	}
	if req.TVAddress != nil {
		a := strings.TrimSpace(*req.TVAddress)
		if a != "" {
			if _, _, err := net.SplitHostPort(a); err != nil {
				a = net.JoinHostPort(a, "5555") // Android TV's network debugging port
			}
		}
		_ = s.Settings.Set(keyTVAddress, a, false)
	}
	if req.TVName != nil {
		_ = s.Settings.Set(keyTVName, strings.TrimSpace(*req.TVName), false)
	}
	if req.GetComics != nil {
		v := ""
		if *req.GetComics {
			v = "1"
		}
		_ = s.Settings.Set(keyGetComics, v, false)
	}
	if req.FlareURL != nil {
		a, err := cleanBaseURL(*req.FlareURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "FlareSolverr's address "+err.Error())
			return
		}
		_ = s.Settings.Set(keyFlareURL, a, false)
	}
	if resync {
		// Pointer files hold the address: write them again.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_, _ = s.db.ExecContext(ctx, `UPDATE streamed_files SET strm_path = '' WHERE strm_path != ''`)
			_, _, _ = s.syncStreamedPointers(ctx)
		}()
	}
	s.handleGetExtras(w, r)
}

// cleanBaseURL checks an http(s) address and drops any trailing slash. ""
// stays "" (the setting is cleared).
func cleanBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("must look like http://192.168.1.20:8264.")
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

func hostOf(base string) string {
	if u, err := url.Parse(base); err == nil {
		return u.Host
	}
	return base
}

// POST /api/home/extras/test/{what}: address, streaming or tv.
func (s *Server) handleTestExtra(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	switch r.PathValue("what") {
	case "address":
		a := s.mediariumAddress("")
		if a == "" {
			writeError(w, http.StatusPreconditionFailed, errNoAddress.Error())
			return
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, a+"/api/tvfeed/"+s.tvFeedKey()+"/channels.m3u", nil)
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			writeError(w, http.StatusBadGateway, "Mediarium couldn't reach itself at "+a+". Check the address and port.")
			return
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			writeError(w, http.StatusBadGateway, "Something else answered at "+a+". Check the address and port.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "Mediarium answers at " + a + "."})
	case "streaming":
		if ok, problem := s.streamingCheck(ctx); !ok {
			writeError(w, http.StatusBadGateway, problem)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "StreamNZB is connected."})
	case "tv":
		addr, name := s.tvAddress()
		if addr == "" {
			writeError(w, http.StatusPreconditionFailed, "Enter the TV's address first.")
			return
		}
		if !adbReady() {
			writeError(w, http.StatusPreconditionFailed, "Pair with the TV first.")
			return
		}
		if _, err := adb(ctx, addr, "shell", "echo", "ok"); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "The " + name + " answers."})
	case "flare":
		v, _ := s.Settings.Get(keyFlareURL)
		if strings.TrimSpace(v) == "" {
			writeError(w, http.StatusPreconditionFailed, "Enter FlareSolverr's address first.")
			return
		}
		if err := directdl.TestFlare(ctx, v); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "FlareSolverr is ready."})
	case "getcomics":
		posts, err := s.direct().Search(ctx, "batman")
		if err != nil {
			writeError(w, http.StatusBadGateway, "GetComics: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("GetComics answers (%d results for a test search).", len(posts))})
	default:
		writeError(w, http.StatusNotFound, "There's no such test.")
	}
}

// POST /api/home/extras/tv-pair: make Mediarium's key (once) and connect to
// the TV, which then asks on screen whether to allow this computer.
func (s *Server) handlePairTV(w http.ResponseWriter, r *http.Request) {
	addr, _ := s.tvAddress()
	if addr == "" {
		writeError(w, http.StatusPreconditionFailed, "Enter the TV's address first.")
		return
	}
	if _, err := exec.LookPath("adb"); err != nil {
		writeError(w, http.StatusServiceUnavailable, "adb isn't in this Mediarium image.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	if _, err := os.Stat(tvKeyDir + "/adbkey"); err != nil {
		if err := os.MkdirAll(tvKeyDir, 0o700); err != nil {
			writeError(w, http.StatusInternalServerError, "Couldn't make a folder for the key: "+err.Error())
			return
		}
		if out, err := exec.CommandContext(ctx, "adb", "keygen", tvKeyDir+"/adbkey").CombinedOutput(); err != nil {
			writeError(w, http.StatusInternalServerError, "Couldn't make a key: "+firstLine(string(out)))
			return
		}
	}
	if _, err := adb(ctx, addr, "shell", "echo", "ok"); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"paired": false, "message": "Look at the TV: allow this computer (tick \"Always allow\"), then press Test. " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paired": true, "message": "Paired. The TV allows Mediarium."})
}
