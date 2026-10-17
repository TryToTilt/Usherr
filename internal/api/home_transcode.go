package api

// Home fork: playing files the browser can't decode. Before a title plays,
// the page says what this browser can decode; Jellyfin (the first enabled
// Jellyfin server under Settings > Connections > Media servers) decides
// whether that is enough. If it is, the file plays straight from Mediarium.
// If not, Jellyfin converts it (on its GPU, with tone mapping) and Mediarium
// relays the HLS stream, so the browser never talks to Jellyfin and never
// sees its key. Closing the player stops the conversion.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

// browserCaps is what the page found this browser can decode.
type browserCaps struct {
	HEVC   bool `json:"hevc"`
	AV1    bool `json:"av1"`
	VP9    bool `json:"vp9"`
	DV     bool `json:"dv"`
	HDR    bool `json:"hdr"`
	AC3    bool `json:"ac3"`
	EAC3   bool `json:"eac3"`
	Opus   bool `json:"opus"`
	FLAC   bool `json:"flac"`
	MKV    bool `json:"mkv"`
	Native bool `json:"nativeHls"`
}

// deviceProfile describes the browser to Jellyfin.
func (c browserCaps) deviceProfile() map[string]any {
	video := []string{"h264"}
	if c.HEVC {
		video = append(video, "hevc")
	}
	if c.AV1 {
		video = append(video, "av1")
	}
	audio := []string{"aac", "mp3"}
	if c.AC3 {
		audio = append(audio, "ac3")
	}
	if c.EAC3 {
		audio = append(audio, "eac3")
	}
	if c.Opus {
		audio = append(audio, "opus")
	}
	if c.FLAC {
		audio = append(audio, "flac")
	}
	containers := "mp4,m4v,mov"
	if c.MKV {
		containers += ",mkv"
	}
	direct := []map[string]any{{"Container": containers, "Type": "Video", "VideoCodec": strings.Join(video, ","), "AudioCodec": strings.Join(audio, ",")}}
	if c.VP9 {
		direct = append(direct, map[string]any{"Container": "webm", "Type": "Video", "VideoCodec": "vp8,vp9,av1", "AudioCodec": "vorbis,opus"})
	}
	ranges := "SDR"
	if c.HDR {
		ranges += "|HDR10|HLG"
	}
	if c.DV {
		ranges += "|DOVI|DOVIWithHDR10|DOVIWithHLG|DOVIWithSDR"
	}
	rangeRule := []map[string]any{{"Condition": "EqualsAny", "Property": "VideoRangeType", "Value": ranges, "IsRequired": false}}
	return map[string]any{
		"MaxStreamingBitrate": 120_000_000,
		"DirectPlayProfiles":  direct,
		"TranscodingProfiles": []map[string]any{{
			"Container": "ts", "Type": "Video", "VideoCodec": "h264", "AudioCodec": "aac",
			"Protocol": "hls", "Context": "Streaming", "MaxAudioChannels": "2", "MinSegments": 1, "BreakOnNonKeyFrames": true,
		}},
		"CodecProfiles": []map[string]any{
			{"Type": "Video", "Codec": "hevc", "Conditions": rangeRule},
			{"Type": "Video", "Codec": "av1", "Conditions": rangeRule},
			{"Type": "Video", "Codec": "h264", "Conditions": rangeRule},
		},
		"SubtitleProfiles": []map[string]any{{"Format": "vtt", "Method": "External"}, {"Format": "srt", "Method": "External"}},
	}
}

// ---- Jellyfin (or Emby) ----

// jellyfinServer is the media server the fork's extras work with: Jellyfin,
// else Emby, which shares its API (Jellyfin began as a fork of Emby).
func (s *Server) jellyfinServer() (mediaservers.Server, bool) {
	list, err := s.MediaServers.List()
	if err != nil {
		return mediaservers.Server{}, false
	}
	for _, kind := range []mediaservers.Kind{mediaservers.KindJellyfin, mediaservers.KindEmby} {
		for _, m := range list {
			if m.Enabled && m.Kind == kind && m.Token != "" {
				return m, true
			}
		}
	}
	return mediaservers.Server{}, false
}

// serverLabel is the media server's product name, for messages.
func serverLabel(srv mediaservers.Server) string { return srv.Kind.Label() }

// connectServerFirst is what to say when no Jellyfin or Emby is connected.
const connectServerFirst = "Connect Jellyfin or Emby under Settings > Connections > Media servers first."

var jfHTTP = &http.Client{Timeout: 60 * time.Second}

func jfRequest(ctx context.Context, srv mediaservers.Server, method, pathAndQuery string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(srv.BaseURL, "/")+pathAndQuery, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Emby-Token", srv.Token)
	req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Token=%q, Client="Mediarium", Device="Mediarium player", DeviceId="mediarium-player", Version="1"`, srv.Token))
	return jfHTTP.Do(req)
}

func jfJSON(ctx context.Context, srv mediaservers.Server, method, pathAndQuery string, body, out any) error {
	resp, err := jfRequest(ctx, srv, method, pathAndQuery, body)
	if err != nil {
		return fmt.Errorf("couldn't reach %s: %w", serverLabel(srv), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s answered %d for %s", serverLabel(srv), resp.StatusCode, strings.SplitN(pathAndQuery, "?", 2)[0])
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(out)
}

// normPath makes Jellyfin and mapped paths comparable.
func normPath(p string) string {
	return strings.TrimRight(strings.ToLower(strings.ReplaceAll(p, `\`, "/")), "/")
}

// jfIndex finds Jellyfin items by file path, one library at a time, kept
// for a while so pressing Play doesn't list a whole library every time.
type jfIndex struct {
	mu   sync.Mutex
	libs map[string]jfLib // library item id -> its files
}

type jfLib struct {
	at    time.Time
	paths map[string]string // normalised path -> item id
}

var jfIdx = &jfIndex{libs: map[string]jfLib{}}

// findJellyfinItem returns the Jellyfin item for a file Mediarium knows by
// its own (container) path.
func (s *Server) findJellyfinItem(ctx context.Context, srv mediaservers.Server, file string) (string, error) {
	mapped := normPath(mediaservers.MapPath(file, srv.PathMap))
	var folders []struct {
		ItemID    string   `json:"ItemId"`
		Locations []string `json:"Locations"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/Library/VirtualFolders", nil, &folders); err != nil {
		return "", err
	}
	libID := ""
	best := -1
	for _, f := range folders {
		for _, loc := range f.Locations {
			l := normPath(loc)
			if (mapped == l || strings.HasPrefix(mapped, l+"/")) && len(l) > best {
				libID, best = f.ItemID, len(l)
			}
		}
	}
	if libID == "" {
		return "", fmt.Errorf("no %s library holds %s (check the folder mapping under Settings > Connections > Media servers)", serverLabel(srv), mapped)
	}
	for attempt := 0; attempt < 2; attempt++ {
		jfIdx.mu.Lock()
		lib, ok := jfIdx.libs[libID]
		jfIdx.mu.Unlock()
		fresh := ok && time.Since(lib.at) < 10*time.Minute
		if fresh {
			if id := lib.paths[mapped]; id != "" {
				return id, nil
			}
			if attempt == 0 && time.Since(lib.at) < 30*time.Second {
				break // just listed: it really isn't there
			}
		}
		var res struct {
			Items []struct {
				ID   string `json:"Id"`
				Path string `json:"Path"`
			} `json:"Items"`
		}
		q := url.Values{"ParentId": {libID}, "Recursive": {"true"}, "Fields": {"Path"}, "IncludeItemTypes": {"Movie,Episode,Video"}, "EnableImages": {"false"}, "EnableUserData": {"false"}}
		if err := jfJSON(ctx, srv, http.MethodGet, "/Items?"+q.Encode(), nil, &res); err != nil {
			return "", err
		}
		paths := make(map[string]string, len(res.Items))
		for _, it := range res.Items {
			paths[normPath(it.Path)] = it.ID
		}
		jfIdx.mu.Lock()
		jfIdx.libs[libID] = jfLib{at: time.Now(), paths: paths}
		jfIdx.mu.Unlock()
		if id := paths[mapped]; id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("%s hasn't added this file yet (it shows up after its next library scan)", serverLabel(srv))
}

// ---- relay sessions ----

type hlsSession struct {
	token, itemID, master, deviceID, playSession string
	userID                                       int64
	srv                                          mediaservers.Server
	expires                                      time.Time
}

var hlsSessions sync.Map // token -> *hlsSession

func newToken() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func requestUserID(r *http.Request) int64 {
	if u := auth.UserFromContext(r.Context()); u != nil {
		return u.ID
	}
	return 0
}

// playFile is the file of a title, if the person may play it.
func (s *Server) playFile(r *http.Request, kind string, id int64) (string, string, error) {
	switch kind {
	case "movie":
		m, err := s.MovieRepo.Get(id)
		if err != nil {
			return "", "", errors.New("That movie isn't in your library.")
		}
		return m.FilePath, fmt.Sprintf("/api/play/movie/%d", id), nil
	case "episode":
		ep, err := s.MovieRepo.GetEpisodeByID(id)
		if err != nil {
			return "", "", errors.New("That episode isn't in your library.")
		}
		return ep.FilePath, fmt.Sprintf("/api/play/episode/%d", id), nil
	case "video":
		if !s.userAllows(r, auth.PermNSFW) || !s.nsfwEnabled() { // home fork: or the NSFW 18+ switch
			return "", "", errors.New("That video isn't available.")
		}
		sc, err := s.videoRepo().GetVideo(id)
		if err != nil {
			return "", "", errors.New("That video isn't in your library.")
		}
		return sc.FilePath, fmt.Sprintf("/api/nsfw/%d/stream", id), nil
	}
	return "", "", errors.New("Say what to play: a movie, an episode or a video.")
}

// POST /api/play/prepare {"kind":"movie","id":1,"caps":{...}} answers how to
// play: {"mode":"direct","url":...} or {"mode":"hls","url":...,"session":...}.
func (s *Server) handlePlayPrepare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string      `json:"kind"`
		ID   int64       `json:"id"`
		Caps browserCaps `json:"caps"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	file, direct, err := s.playFile(r, req.Kind, req.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if file == "" {
		writeError(w, http.StatusNotFound, "This title has no file yet.")
		return
	}
	directAnswer := func(note string) {
		writeJSON(w, http.StatusOK, map[string]any{"mode": "direct", "url": direct, "note": note})
	}
	srv, ok := s.jellyfinServer()
	if !ok {
		directAnswer("Connect Jellyfin or Emby under Settings > Connections > Media servers to play files this browser can't decode.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	itemID, err := s.findJellyfinItem(ctx, srv, file)
	if err != nil {
		directAnswer(err.Error())
		return
	}
	var info struct {
		PlaySessionID string `json:"PlaySessionId"`
		MediaSources  []struct {
			ID                string   `json:"Id"`
			SupportsDirect    bool     `json:"SupportsDirectPlay"`
			TranscodingURL    string   `json:"TranscodingUrl"`
			TranscodeReasons  []string `json:"TranscodeReasons"`
			TranscodingSubPro string   `json:"TranscodingSubProtocol"`
		} `json:"MediaSources"`
	}
	body := map[string]any{
		"DeviceProfile": req.Caps.deviceProfile(), "MaxStreamingBitrate": 120_000_000,
		"EnableDirectPlay": true, "EnableDirectStream": false, "EnableTranscoding": true,
		"AllowVideoStreamCopy": false, "AllowAudioStreamCopy": false, "AutoOpenLiveStream": false,
	}
	if err := jfJSON(ctx, srv, http.MethodPost, "/Items/"+url.PathEscape(itemID)+"/PlaybackInfo", body, &info); err != nil {
		directAnswer(err.Error())
		return
	}
	if len(info.MediaSources) == 0 {
		directAnswer(serverLabel(srv) + " has no playable version of this file.")
		return
	}
	src := info.MediaSources[0]
	if src.SupportsDirect || src.TranscodingURL == "" {
		directAnswer("")
		return
	}
	u, err := url.Parse(src.TranscodingURL)
	if err != nil {
		directAnswer(serverLabel(srv) + "'s conversion address couldn't be read.")
		return
	}
	q := u.Query()
	q.Del("api_key")
	q.Del("ApiKey")
	sess := &hlsSession{
		token: newToken(), itemID: itemID, deviceID: q.Get("DeviceId"), playSession: info.PlaySessionID,
		userID: requestUserID(r), srv: srv, expires: time.Now().Add(8 * time.Hour),
	}
	if sess.playSession == "" {
		sess.playSession = q.Get("PlaySessionId")
	}
	sess.master = u.Path + "?" + q.Encode()
	hlsSessions.Store(sess.token, sess)
	writeJSON(w, http.StatusOK, map[string]any{
		"mode": "hls", "url": "/api/play/hls/" + sess.token + "/master.m3u8", "session": sess.token,
		"reasons": src.TranscodeReasons,
	})
}

var hlsRest = regexp.MustCompile(`^[A-Za-z0-9_.\-/]+$`)

func (s *Server) hlsSessionFor(w http.ResponseWriter, r *http.Request) (*hlsSession, bool) {
	v, ok := hlsSessions.Load(r.PathValue("token"))
	if !ok {
		writeError(w, http.StatusNotFound, "This stream has ended. Press Play again.")
		return nil, false
	}
	sess := v.(*hlsSession)
	if time.Now().After(sess.expires) || sess.userID != requestUserID(r) {
		writeError(w, http.StatusNotFound, "This stream has ended. Press Play again.")
		return nil, false
	}
	return sess, true
}

// GET /api/play/hls/{token}/{rest...}: relays Jellyfin's HLS playlists and
// segments for this stream only.
func (s *Server) handlePlayHLS(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.hlsSessionFor(w, r)
	if !ok {
		return
	}
	rest := r.PathValue("rest")
	if !hlsRest.MatchString(rest) || strings.Contains(rest, "..") {
		writeError(w, http.StatusBadRequest, "Bad stream address.")
		return
	}
	target := "/videos/" + url.PathEscape(sess.itemID) + "/" + rest
	if r.URL.RawQuery != "" {
		q := r.URL.Query()
		q.Del("api_key")
		q.Del("ApiKey")
		target += "?" + q.Encode()
	}
	if rest == "master.m3u8" {
		target = sess.master
	}
	resp, err := jfRequest(r.Context(), sess.srv, http.MethodGet, target, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, serverLabel(sess.srv)+" didn't answer.")
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Length", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasSuffix(rest, ".m3u8") {
		// Playlists can carry the server's key in their links (Emby always
		// adds it): take it out, the relay adds its own.
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			writeError(w, http.StatusBadGateway, serverLabel(sess.srv)+" didn't answer.")
			return
		}
		body = stripPlaylistKeys(body)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

var playlistKey = regexp.MustCompile(`(?i)([?&])(api_key|apikey|X-Emby-Token)=[^&\s"]*&?`)

// stripPlaylistKeys removes media-server keys from the links in a playlist.
func stripPlaylistKeys(b []byte) []byte {
	b = playlistKey.ReplaceAll(b, []byte("$1"))
	b = bytes.ReplaceAll(b, []byte("?\n"), []byte("\n"))
	b = bytes.ReplaceAll(b, []byte("&\n"), []byte("\n"))
	return b
}

// DELETE /api/play/hls/{token}: stops the conversion.
func (s *Server) handleStopHLS(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.hlsSessionFor(w, r)
	if !ok {
		return
	}
	hlsSessions.Delete(sess.token)
	q := url.Values{"DeviceId": {sess.deviceID}, "PlaySessionId": {sess.playSession}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if resp, err := jfRequest(ctx, sess.srv, http.MethodDelete, "/Videos/ActiveEncodings?"+q.Encode(), nil); err == nil {
		resp.Body.Close()
	}
	w.WriteHeader(http.StatusNoContent)
}

// expireHLSSessions forgets streams nobody closed.
func expireHLSSessions() {
	now := time.Now()
	hlsSessions.Range(func(k, v any) bool {
		if now.After(v.(*hlsSession).expires) {
			hlsSessions.Delete(k)
		}
		return true
	})
}

func init() {
	go func() {
		for range time.Tick(30 * time.Minute) {
			expireHLSSessions()
		}
	}()
}
