package api

// Home fork: "Play on the TV". Mediarium starts playback on the living-room
// Android TV (an Nvidia Shield) over network debugging (adb), so a phone can
// pick something and the TV plays it:
//
//   - a title: Stremio opens on that movie or episode, where StreamNZB's
//     stream for the TV (4K, Dolby Vision, Atmos) is one press away;
//   - a stream already started in Mediarium: VLC on the TV plays its link
//     on the home network.
//
// The TV allowed this computer once ("Always allow"); the key it allowed
// lives in /config/adb.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	keyTVAddress = "home.tv_address" // host:port of the TV's network debugging
	keyTVName    = "home.tv_name"
	tvKeyDir     = "/config/adb" // adbkey, adbkey.pub (allowed on the TV)
)

var tvMu sync.Mutex // one adb conversation at a time

func (s *Server) tvAddress() (addr, name string) {
	addr, _ = s.Settings.Get(keyTVAddress)
	name, _ = s.Settings.Get(keyTVName)
	if strings.TrimSpace(name) == "" {
		name = "Living room TV"
	}
	return strings.TrimSpace(addr), name
}

// tvReady reports whether playing on the TV is set up: its address, adb and
// the key the TV allowed.
func (s *Server) tvReady() bool {
	if addr, _ := s.tvAddress(); addr == "" {
		return false
	}
	return adbReady()
}

// adbReady reports whether adb and the allowed key are here.
func adbReady() bool {
	if _, err := exec.LookPath("adb"); err != nil {
		return false
	}
	_, err := os.Stat(tvKeyDir + "/adbkey")
	return err == nil
}

// adb runs one adb command against the TV, connecting first.
func adb(ctx context.Context, addr string, args ...string) (string, error) {
	tvMu.Lock()
	defer tvMu.Unlock()
	env := append(os.Environ(), "HOME="+tvKeyDir+"/home", "ADB_VENDOR_KEYS="+tvKeyDir+"/adbkey")
	_ = os.MkdirAll(tvKeyDir+"/home/.android", 0o700)
	run := func(a ...string) (string, error) {
		c, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(c, "adb", a...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := run("connect", addr); err != nil || strings.Contains(out, "failed") || strings.Contains(out, "unable") {
		return out, fmt.Errorf("couldn't reach the TV at %s: %s", addr, firstLine(out))
	}
	out, err := run(append([]string{"-s", addr}, args...)...)
	if err != nil {
		return out, fmt.Errorf("the TV didn't take it: %s", firstLine(out))
	}
	if strings.Contains(out, "unauthorized") {
		return out, errors.New("the TV hasn't allowed this computer yet: accept the prompt on the TV")
	}
	if strings.Contains(out, "Error") {
		return out, fmt.Errorf("the TV said: %s", firstLine(out))
	}
	return out, nil
}

// shellQuote quotes a value for the TV's shell (adb shell joins its
// arguments into one command line).
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

var imdbID = regexp.MustCompile(`^tt\d+$`)

// stremioLink is the Stremio link to a movie's or an episode's page.
func stremioLink(kind, imdb string, season, episode int) (string, error) {
	if !imdbID.MatchString(imdb) {
		return "", errors.New("not an IMDb id")
	}
	if kind == "movie" {
		return fmt.Sprintf("stremio:///detail/movie/%s/%s", imdb, imdb), nil
	}
	return fmt.Sprintf("stremio:///detail/series/%s/%s:%d:%d", imdb, imdb, season, episode), nil
}

// POST /api/stream/tv {kind, tmdbId, season, episode}: open the title in
// Stremio on the TV.
func (s *Server) handleStreamTVTitle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind    string `json:"kind"`
		TMDBID  int    `json:"tmdbId"`
		Season  int    `json:"season"`
		Episode int    `json:"episode"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Kind != "movie" && req.Kind != "series") || req.TMDBID <= 0 ||
		(req.Kind == "series" && (req.Season <= 0 || req.Episode <= 0)) {
		writeError(w, http.StatusBadRequest, "Say which movie, or which episode of which show.")
		return
	}
	if !s.tvReady() {
		writeError(w, http.StatusServiceUnavailable, "Playing on the TV isn't set up here (Settings > Extras).")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	imdb, err := s.imdbFor(ctx, req.Kind, req.TMDBID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	link, err := stremioLink(req.Kind, imdb, req.Season, req.Episode)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	addr, name := s.tvAddress()
	if _, err := adb(ctx, addr, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", shellQuote(link), "com.stremio.one"); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tv": name, "message": "Opened in Stremio on the " + name + ". Press OK on the remote to play."})
}

// POST /api/stream/{id}/tv: play a started stream on the TV, in VLC.
func (s *Server) handleStreamTV(w http.ResponseWriter, r *http.Request) {
	ss, ok := streams.get(r.PathValue("id"), requestUserID(r))
	if !ok {
		writeError(w, http.StatusNotFound, "This stream has ended. Start it again.")
		return
	}
	if !s.tvReady() {
		writeError(w, http.StatusServiceUnavailable, "Playing on the TV isn't set up here (Settings > Extras).")
		return
	}
	if ss.lanURL == "" || !(strings.HasPrefix(ss.lanURL, "http://") || strings.HasPrefix(ss.lanURL, "https://")) {
		writeError(w, http.StatusBadGateway, "This stream has no link the TV can open.")
		return
	}
	streams.mu.Lock()
	ss.onTV = true // the TV reads StreamNZB directly now: don't end it as idle
	streams.mu.Unlock()
	addr, name := s.tvAddress()
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if _, err := adb(ctx, addr, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-t", "video/*", "-d", shellQuote(ss.lanURL), "org.videolan.vlc"); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tv": name, "message": "Playing on the " + name + " in VLC."})
}

// tvStatus is the TV part of /api/stream/status.
func (s *Server) tvStatus() map[string]any {
	if !s.tvReady() {
		return nil
	}
	addr, name := s.tvAddress()
	return map[string]any{"name": name, "address": addr}
}
