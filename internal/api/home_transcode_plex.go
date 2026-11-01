package api

// Usherr: Plex as the player's converter, between Jellyfin (or Emby) and
// Usherr's own ffmpeg. When a file doesn't play as it is in this browser and
// no Jellyfin or Emby converts it, Plex's universal transcoder does (on the
// graphics card with Plex Pass and hardware acceleration on), and Usherr
// relays its HLS stream like Jellyfin's: the browser only ever sees
// /api/play/hls/{token}/..., never Plex or its token.
//
// The calls are the ones Plex's own HLS clients make (read from Plex's log):
// /video/:/transcode/universal/decision, then start.m3u8 with the same
// query; its playlist points at session/{session}/base/index.m3u8 and the
// pieces beside it; stop?session= ends it. What the browser plays is told in
// X-Plex-Client-Profile-Extra, on top of Plex's Generic profile.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

var plexHTTP = &http.Client{Timeout: 60 * time.Second}

// Who converts for the player first (Settings, Extras, Converting for the
// player): unset is Jellyfin or Emby, then Plex. Whichever is chosen hands
// on to the next when it can't, and Usherr's own ffmpeg is always last.
const (
	keyConvertWith  = "home.convert.with"
	convertJellyfin = "jellyfin"
	convertPlex     = "plex"
	convertUsherr   = "usherr"
)

// convertOrder is the media servers asked to convert, in order; Usherr
// itself comes after them.
func (s *Server) convertOrder() []string {
	switch v, _ := s.Settings.Get(keyConvertWith); v {
	case convertPlex:
		return []string{convertPlex, convertJellyfin}
	case convertUsherr:
		return nil
	}
	return []string{convertJellyfin, convertPlex}
}

type convertInfo struct {
	With     string `json:"with"`     // "" (Jellyfin or Emby, then Plex), jellyfin, plex or usherr
	Jellyfin string `json:"jellyfin"` // the Jellyfin or Emby server's name, when one is connected
	Plex     string `json:"plex"`     // the Plex server's name, when one is connected
}

func (s *Server) convertInfo() convertInfo {
	var c convertInfo
	c.With, _ = s.Settings.Get(keyConvertWith)
	if srv, ok := s.jellyfinServer(); ok {
		c.Jellyfin = srv.Name
	}
	if srv, ok := s.plexServerForPlay(); ok {
		c.Plex = srv.Name
	}
	return c
}

// GET /api/home/convert (administrators): who converts for the player.
func (s *Server) handleGetConvert(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.convertInfo())
}

// PUT /api/home/convert {"with": "" | "jellyfin" | "plex" | "usherr"}.
func (s *Server) handlePutConvert(w http.ResponseWriter, r *http.Request) {
	var in struct {
		With string `json:"with"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That couldn't be read.")
		return
	}
	switch in.With {
	case "", convertJellyfin, convertPlex, convertUsherr:
	default:
		writeError(w, http.StatusBadRequest, "Convert with Jellyfin, Plex or Usherr.")
		return
	}
	_ = s.Settings.Set(keyConvertWith, in.With, false)
	writeJSON(w, http.StatusOK, s.convertInfo())
}

// plexServerForPlay is the Plex server that converts for the player: the
// first enabled one with a token, one that didn't answer a moment ago too
// (someone just pressed Play).
func (s *Server) plexServerForPlay() (mediaservers.Server, bool) {
	if s.MediaServers == nil {
		return mediaservers.Server{}, false
	}
	list := s.plexServersAll()
	if len(list) == 0 {
		return mediaservers.Server{}, false
	}
	return list[0], true
}

// plexProfileExtra tells Plex what this browser plays, on top of its Generic
// profile: H.264 with AAC (or MP3) in HLS, at any size, so a 4K picture
// stays 4K. HEVC isn't offered: browsers play it in HLS only in MP4 pieces,
// and Plex's HLS sends MPEG-TS.
func plexProfileExtra(c browserCaps) string {
	parts := []string{
		"add-transcode-target(type=videoProfile&context=all&protocol=hls&container=mpegts&videoCodec=h264&audioCodec=aac,mp3)",
	}
	for _, dim := range []string{"width", "height"} {
		parts = append(parts, "add-limitation(scope=videoTranscodeTarget&scopeName=h264&scopeType=videoCodec&context=all&protocol=hls&type=upperBound&name=video."+dim+"&value=8192)")
	}
	return strings.Join(parts, "+")
}

// plexRequest is a request to Plex as Usherr's player, with its token in a
// header (never in an address the browser could see).
func plexRequest(ctx context.Context, srv mediaservers.Server, method, pathAndQuery, profile string) (*http.Response, error) {
	base, err := mediaservers.NormalizeURL(srv.BaseURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, base+pathAndQuery, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", srv.Token)
	req.Header.Set("X-Plex-Product", "Usherr")
	req.Header.Set("X-Plex-Client-Identifier", "usherr-player")
	req.Header.Set("X-Plex-Platform", "Generic")
	req.Header.Set("X-Plex-Device-Name", "Usherr player")
	if profile != "" {
		req.Header.Set("X-Plex-Client-Profile-Extra", profile)
	}
	return plexHTTP.Do(req)
}

// plexIndex finds Plex items by their file, one library at a time, kept for
// a while like Jellyfin's (jfIdx).
type plexIndex struct {
	mu   sync.Mutex
	libs map[string]plexLib // server address + library key -> its files
}

type plexLib struct {
	at    time.Time
	paths map[string]string // normalised path -> rating key
	tails map[string]string // the file's last folders and name -> rating key ("" when two share it)
}

var plexIdx = &plexIndex{libs: map[string]plexLib{}}

// pathTail is the last n parts of a path: Plex sees D:\Movies\Film (2020)\
// Film.mkv where Usherr sees /data/movies/Film (2020)/Film.mkv, so without a
// folder mapping they're matched by the title's folder and the file's name.
func pathTail(p string, n int) string {
	parts := strings.Split(normPath(p), "/")
	if len(parts) < n {
		return ""
	}
	return strings.Join(parts[len(parts)-n:], "/")
}

// tailParts is how much of the path tells titles apart: a film's folder and
// file; for an episode, the show's folder too (every show has a Season 01).
func tailParts(kind string) int {
	if kind == "episode" {
		return 3
	}
	return 2
}

// findPlexItem is the rating key of the Plex item for a file Usherr knows
// by its own (container) path, in Plex's film libraries for a film and its
// show libraries for an episode.
func findPlexItem(ctx context.Context, srv mediaservers.Server, kind, file string) (string, error) {
	libType, itemType := "movie", "1"
	if kind == "episode" {
		libType, itemType = "show", "4"
	}
	resp, err := plexRequest(ctx, srv, http.MethodGet, "/library/sections", "")
	if err != nil {
		return "", fmt.Errorf("couldn't reach Plex: %w", err)
	}
	var secs struct {
		MediaContainer struct {
			Directory []struct {
				Key  string `json:"key"`
				Type string `json:"type"`
			} `json:"Directory"`
		} `json:"MediaContainer"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&secs)
	resp.Body.Close()
	if err != nil || resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("Plex answered %d for its libraries", resp.StatusCode)
	}
	mapped := normPath(mediaservers.MapPath(file, srv.PathMap))
	tail := pathTail(file, tailParts(kind))
	for _, sec := range secs.MediaContainer.Directory {
		if sec.Type != libType {
			continue
		}
		lib, err := plexLibrary(ctx, srv, sec.Key, itemType, tailParts(kind))
		if err != nil {
			return "", err
		}
		if key := lib.paths[mapped]; key != "" {
			return key, nil
		}
		if key := lib.tails[tail]; key != "" && tail != "" {
			return key, nil
		}
	}
	return "", errors.New("Plex hasn't added this file (it shows up after its next library scan)")
}

// plexLibrary lists a library's files, or gives the list read in the last
// ten minutes.
func plexLibrary(ctx context.Context, srv mediaservers.Server, section, itemType string, n int) (plexLib, error) {
	id := srv.BaseURL + "|" + section
	plexIdx.mu.Lock()
	lib, ok := plexIdx.libs[id]
	plexIdx.mu.Unlock()
	if ok && time.Since(lib.at) < 10*time.Minute {
		return lib, nil
	}
	resp, err := plexRequest(ctx, srv, http.MethodGet, "/library/sections/"+url.PathEscape(section)+"/all?type="+itemType, "")
	if err != nil {
		return plexLib{}, fmt.Errorf("couldn't reach Plex: %w", err)
	}
	defer resp.Body.Close()
	var doc struct {
		MediaContainer struct {
			Metadata []struct {
				RatingKey string `json:"ratingKey"`
				Media     []struct {
					Part []struct {
						File string `json:"file"`
					} `json:"Part"`
				} `json:"Media"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if resp.StatusCode/100 != 2 {
		return plexLib{}, fmt.Errorf("Plex answered %d for a library", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(&doc); err != nil {
		return plexLib{}, fmt.Errorf("Plex's library couldn't be read: %w", err)
	}
	lib = plexLib{at: time.Now(), paths: map[string]string{}, tails: map[string]string{}}
	for _, m := range doc.MediaContainer.Metadata {
		for _, md := range m.Media {
			for _, p := range md.Part {
				lib.paths[normPath(p.File)] = m.RatingKey
				t := pathTail(p.File, n)
				if old, seen := lib.tails[t]; seen && old != m.RatingKey {
					lib.tails[t] = "" // two titles share it: only the full path will do
				} else if !seen {
					lib.tails[t] = m.RatingKey
				}
			}
		}
	}
	plexIdx.mu.Lock()
	plexIdx.libs[id] = lib
	plexIdx.mu.Unlock()
	return lib, nil
}

// plexPlayQuery is the query of Plex's decision and start calls for a title.
// directStream lets Plex copy a picture the browser plays (4K stays 4K);
// soundCopy lets it copy a sound the browser plays (off when it played
// silently before).
func plexPlayQuery(ratingKey, session string, soundCopy bool) url.Values {
	copySound := "1"
	if !soundCopy {
		copySound = "0"
	}
	return url.Values{
		"hasMDE": {"1"}, "path": {"/library/metadata/" + ratingKey}, "mediaIndex": {"0"}, "partIndex": {"0"},
		"protocol": {"hls"}, "fastSeek": {"1"}, "directPlay": {"0"}, "directStream": {"1"}, "directStreamAudio": {copySound},
		"videoQuality": {"100"}, "subtitleSize": {"100"}, "audioBoost": {"100"}, "location": {"lan"},
		"autoAdjustQuality": {"0"}, "mediaBufferSize": {"102400"}, "subtitles": {"none"}, "session": {session},
	}
}

// preparePlex asks Plex whether it can convert the file for this browser.
// What it answers is the session to relay; an error sends the player on to
// Usherr's own conversion.
func (s *Server) preparePlex(ctx context.Context, srv mediaservers.Server, kind, file string, caps browserCaps, soundCopy bool, userID int64) (*hlsSession, error) {
	ratingKey, err := findPlexItem(ctx, srv, kind, file)
	if err != nil {
		return nil, err
	}
	session := newToken()[:24]
	q := plexPlayQuery(ratingKey, session, soundCopy)
	profile := plexProfileExtra(caps)
	resp, err := plexRequest(ctx, srv, http.MethodGet, "/video/:/transcode/universal/decision?"+q.Encode(), profile)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach Plex: %w", err)
	}
	var dec struct {
		MediaContainer struct {
			GeneralCode int    `json:"generalDecisionCode"`
			GeneralText string `json:"generalDecisionText"`
		} `json:"MediaContainer"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&dec)
	resp.Body.Close()
	if resp.StatusCode/100 != 2 || err != nil {
		return nil, fmt.Errorf("Plex answered %d when asked to convert it", resp.StatusCode)
	}
	if dec.MediaContainer.GeneralCode >= 2000 {
		return nil, fmt.Errorf("Plex can't convert it: %s", dec.MediaContainer.GeneralText)
	}
	return &hlsSession{
		token: newToken(), itemID: ratingKey, playSession: session, plexProfile: profile,
		master: "/video/:/transcode/universal/start.m3u8?" + q.Encode(),
		userID: userID, srv: srv, expires: time.Now().Add(8 * time.Hour),
	}, nil
}

// plexTarget is where on Plex a relayed address of a Plex stream goes: its
// start playlist, or something under its own session only.
func plexTarget(sess *hlsSession, rest string, q url.Values) (string, bool) {
	if rest == "master.m3u8" {
		return sess.master, true
	}
	if !strings.HasPrefix(rest, "session/"+sess.playSession+"/") {
		return "", false
	}
	target := "/video/:/transcode/universal/" + rest
	q.Del("X-Plex-Token")
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	return target, true
}

// stopPlex ends Plex's conversion when the player closes.
func stopPlex(sess *hlsSession) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if resp, err := plexRequest(ctx, sess.srv, http.MethodGet, "/video/:/transcode/universal/stop?session="+url.QueryEscape(sess.playSession), ""); err == nil {
		resp.Body.Close()
	}
}

// plexPlaylistPaths points a playlist's links from Plex's root
// (/video/:/transcode/universal/session/...) at the relay; relative links
// already go there.
func plexPlaylistPaths(b []byte, token string) []byte {
	return []byte(strings.ReplaceAll(string(b), "/video/:/transcode/universal/", "/api/play/hls/"+token+"/"))
}
