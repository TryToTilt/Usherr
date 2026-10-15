package api

// Home fork: "Stream now". Mediarium doesn't stream Usenet itself: it asks
// StreamNZB (a Stremio add-on server on the same machine), which picks a
// working release, falls back when one is dead and starts fast. Two ways in:
//
//   - a title (movie, or a show's episode): StreamNZB's own ranked streams
//     for it, using the stream profile of the quality asked for (its
//     profiles are named for who uses them; their filter profile says 4K,
//     1080p or 720p);
//   - one exact release from Mediarium's release list: StreamNZB's "play
//     this NZB".
//
// The video is relayed through Mediarium (range requests, seeking), so it
// plays wherever Mediarium opens: at home, over Tailscale, on a phone.
// Nothing is downloaded to the library; "Keep it" in the page grabs the
// release (or adds the title) the normal way.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/parser"
)

const (
	keyStreamNZBURL   = "home.streamnzb_url"   // base URL Mediarium reaches StreamNZB at
	keyStreamNZBToken = "home.streamnzb_token" // admin token (encrypted); else read from its config file
	defaultStreamNZB  = "http://host.docker.internal:7000"
	// streamNZBConfig is StreamNZB's own config, mounted read-only, so the
	// admin token never has to be typed or pasted anywhere.
	streamNZBConfig = "/streamnzb/config.json"
	streamIdle      = 30 * time.Minute
)

// streamNZB talks to StreamNZB's HTTP API.
type streamNZB struct {
	base  string
	token string
	http  *http.Client
}

func (s *Server) streamNZB() (*streamNZB, error) {
	base, _ := s.Settings.Get(keyStreamNZBURL)
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = defaultStreamNZB
	}
	token, _ := s.Settings.Get(keyStreamNZBToken)
	token = strings.TrimSpace(token)
	if token == "" {
		token = tokenFromStreamNZBConfig(streamNZBConfig)
	}
	if token == "" {
		return nil, errors.New("StreamNZB isn't connected: its admin token wasn't found")
	}
	return &streamNZB{base: base, token: token, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

func tokenFromStreamNZBConfig(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var c struct {
		AdminToken string `json:"admin_token"`
	}
	if json.Unmarshal(b, &c) != nil {
		return ""
	}
	return strings.TrimSpace(c.AdminToken)
}

func (c *streamNZB) do(ctx context.Context, method, path string, body io.Reader, contentType string, into any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach StreamNZB: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &e) == nil && (e.Error != "" || e.Message != "") {
			msg = e.Error + e.Message
		}
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("StreamNZB answered %d: %s", resp.StatusCode, msg)
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(data, into)
}

// snzbProfile is one of StreamNZB's streams (it calls them streams; here
// they are the quality profiles a stream can be asked for with).
type snzbProfile struct {
	Name    string `json:"username"`
	Token   string `json:"token"`
	Filter  string `json:"filter_profile_name"`
	Quality string `json:"-"` // "4K", "1080p", "720p", or ""
}

var qualityInName = regexp.MustCompile(`(?i)(2160p|4k|1080p|720p|480p)`)

func profileQuality(filter string) string {
	m := qualityInName.FindString(filter)
	switch strings.ToLower(m) {
	case "2160p", "4k":
		return "4K"
	case "":
		return ""
	}
	return strings.ToLower(m)
}

func (c *streamNZB) profiles(ctx context.Context) ([]snzbProfile, error) {
	var out []snzbProfile
	if err := c.do(ctx, http.MethodGet, "/api/streams", nil, "", &out); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Quality = profileQuality(out[i].Filter)
	}
	return out, nil
}

// profileFor picks the profile for a quality: the first one (in StreamNZB's
// order) whose filter profile is that quality.
func profileFor(profiles []snzbProfile, quality string) (snzbProfile, bool) {
	for _, p := range profiles {
		if strings.EqualFold(p.Quality, quality) && p.Token != "" {
			return p, true
		}
	}
	return snzbProfile{}, false
}

type stremioStream struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

func (c *streamNZB) titleStreams(ctx context.Context, profileToken, kind, id string) ([]stremioStream, error) {
	var out struct {
		Streams []stremioStream `json:"streams"`
	}
	path := fmt.Sprintf("/%s/stream/%s/%s.json", url.PathEscape(profileToken), kind, url.PathEscape(id))
	if err := c.do(ctx, http.MethodGet, path, nil, "", &out); err != nil {
		return nil, err
	}
	var keep []stremioStream
	for _, st := range out.Streams {
		if st.URL != "" {
			keep = append(keep, st)
		}
	}
	return keep, nil
}

type playNZBResult struct {
	SessionID       string `json:"session_id"`
	PlayPath        string `json:"play_path"`
	PlayURL         string `json:"play_url"`
	ExternalPlayURL string `json:"external_play_url"`
}

func (c *streamNZB) playNZB(ctx context.Context, nzbURL string) (playNZBResult, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("url", nzbURL)
	_ = mw.Close()
	var out playNZBResult
	err := c.do(ctx, http.MethodPost, "/api/play/nzb", &buf, mw.FormDataContentType(), &out)
	if err == nil && out.PlayPath == "" && out.PlayURL == "" && out.ExternalPlayURL == "" {
		err = errors.New("StreamNZB gave no play link")
	}
	return out, err
}

func (c *streamNZB) closeSession(ctx context.Context, id string) {
	if id == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"id": id})
	if err := c.do(ctx, http.MethodPost, "/api/sessions/close", bytes.NewReader(body), "application/json", nil); err != nil {
		slog.Info("streaming: close StreamNZB session", "err", err)
	}
}

// internalURL is a play link as Mediarium reaches it: StreamNZB hands out
// links for its own address on the home network; the path is what counts.
func (c *streamNZB) internalURL(link string) string {
	if link == "" {
		return ""
	}
	if strings.HasPrefix(link, "/") {
		return c.base + link
	}
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return c.base + u.RequestURI()
}

// ---- sessions ----

type streamSession struct {
	id        string
	title     string
	target    string // play link as Mediarium reaches it
	lanURL    string // play link for a device at home (the TV)
	snzbID    string // StreamNZB's session, for "play this NZB"
	userID    int64
	last      time.Time
	release   string // the release name, when known
	streamFor string // "release" or "title"
	onTV      bool   // playing on the TV, which reads StreamNZB directly
}

// streamOption is a title stream offered to the page, kept here so the
// page only sees an id.
type streamOption struct {
	url, title string
	userID     int64
	at         time.Time
}

type streamStore struct {
	mu       sync.Mutex
	sessions map[string]*streamSession
	options  map[string]streamOption
}

var streams = &streamStore{sessions: map[string]*streamSession{}, options: map[string]streamOption{}}

func randomID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// sweep ends sessions nobody has read from for a while, and forgets old
// options. It runs on each new session.
func (st *streamStore) sweep(c *streamNZB) {
	st.mu.Lock()
	var stale []*streamSession
	for id, ss := range st.sessions {
		if time.Since(ss.last) > streamIdle && (!ss.onTV || time.Since(ss.last) > 6*time.Hour) {
			stale = append(stale, ss)
			delete(st.sessions, id)
		}
	}
	for id, o := range st.options {
		if time.Since(o.at) > time.Hour {
			delete(st.options, id)
		}
	}
	st.mu.Unlock()
	for _, ss := range stale {
		if c != nil {
			c.closeSession(context.Background(), ss.snzbID)
		}
	}
}

func (st *streamStore) get(id string, userID int64) (*streamSession, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	ss, ok := st.sessions[id]
	if !ok || ss.userID != userID {
		return nil, false
	}
	ss.last = time.Now()
	return ss, true
}

// ---- handlers ----

// GET /api/stream/status: whether streaming is set up, and the qualities
// that can be asked for.
func (s *Server) handleStreamStatus(w http.ResponseWriter, r *http.Request) {
	c, err := s.streamNZB()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ready": false, "problem": err.Error()})
		return
	}
	profiles, err := c.profiles(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ready": false, "problem": err.Error()})
		return
	}
	var qualities []string
	seen := map[string]bool{}
	for _, q := range []string{"4K", "1080p", "720p"} {
		if _, ok := profileFor(profiles, q); ok && !seen[q] {
			seen[q] = true
			qualities = append(qualities, q)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": len(qualities) > 0, "qualities": qualities, "tv": s.tvStatus()})
}

type streamOptionPayload struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	Quality string `json:"quality,omitempty"`
	Codec   string `json:"codec,omitempty"`
}

// GET /api/stream/options?kind=movie|series&tmdbId=&season=&episode=&quality=
// lists StreamNZB's streams for a title, best first.
func (s *Server) handleStreamOptions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
	tmdbID, _ := strconv.Atoi(q.Get("tmdbId"))
	season, _ := strconv.Atoi(q.Get("season"))
	episode, _ := strconv.Atoi(q.Get("episode"))
	if (kind != "movie" && kind != "series") || tmdbID <= 0 || (kind == "series" && (season <= 0 || episode <= 0)) {
		writeError(w, http.StatusBadRequest, "Say which movie, or which episode of which show.")
		return
	}
	c, err := s.streamNZB()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	// Not tied to the page's request: when someone switches quality the page
	// drops its question, and a cancelled request made StreamNZB stop its
	// indexer searches halfway and keep the empty answer. Let the search
	// finish, so StreamNZB remembers what is really there.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
	defer cancel()
	profiles, err := c.profiles(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	p, ok := profileFor(profiles, q.Get("quality"))
	if !ok {
		writeError(w, http.StatusBadRequest, "StreamNZB has no stream set up for "+q.Get("quality")+".")
		return
	}
	imdb, err := s.imdbFor(ctx, kind, tmdbID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	id := imdb
	if kind == "series" {
		id = fmt.Sprintf("%s:%d:%d", imdb, season, episode)
	}
	list, err := c.titleStreams(ctx, p.Token, kind, id)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	user := requestUserID(r)
	out := make([]streamOptionPayload, 0, len(list))
	streams.mu.Lock()
	for _, st := range list {
		oid := randomID("opt_")
		label := strings.TrimSpace(st.Title)
		if label == "" {
			label = strings.TrimSpace(st.Description)
		}
		streams.options[oid] = streamOption{url: st.URL, title: label, userID: user, at: time.Now()}
		rel := parser.Parse(firstLine(label))
		out = append(out, streamOptionPayload{ID: oid, Name: strings.TrimSpace(st.Name), Title: label, Quality: rel.Resolution, Codec: rel.Codec})
	}
	streams.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"profile": p.Name, "options": out})
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// imdbFor looks a title's IMDb id up on TMDB.
func (s *Server) imdbFor(ctx context.Context, kind string, tmdbID int) (string, error) {
	var imdb string
	if kind == "movie" {
		d, err := s.TMDB().GetMovieDetail(ctx, tmdbID)
		if err != nil {
			return "", fmt.Errorf("couldn't look the movie up: %w", err)
		}
		imdb = d.IMDBID
	} else {
		d, err := s.TMDB().GetShowFull(ctx, tmdbID)
		if err != nil {
			return "", fmt.Errorf("couldn't look the show up: %w", err)
		}
		imdb = d.ExternalIDs.IMDBID
	}
	if !strings.HasPrefix(imdb, "tt") {
		return "", errors.New("this title has no IMDb id, which StreamNZB needs")
	}
	return imdb, nil
}

type streamStartRequest struct {
	Option       string `json:"option"`       // a title stream from /api/stream/options
	DownloadURL  string `json:"downloadUrl"`  // or a release from a release list (its rel_ reference)
	ReleaseTitle string `json:"releaseTitle"` // the release name, for the player's title
	Title        string `json:"title"`        // what the player shows
}

// POST /api/stream/start {option} or {downloadUrl, releaseTitle}
func (s *Server) handleStreamStart(w http.ResponseWriter, r *http.Request) {
	var req streamStartRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request.")
		return
	}
	c, err := s.streamNZB()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	streams.sweep(c)
	user := requestUserID(r)
	ss := &streamSession{id: randomID("str_"), userID: user, last: time.Now(), title: strings.TrimSpace(req.Title)}
	switch {
	case req.Option != "":
		streams.mu.Lock()
		o, ok := streams.options[req.Option]
		streams.mu.Unlock()
		if !ok || o.userID != user {
			writeError(w, http.StatusNotFound, "That stream has expired. Look again.")
			return
		}
		ss.target, ss.lanURL, ss.streamFor, ss.release = c.internalURL(o.url), o.url, "title", firstLine(o.title)
	case req.DownloadURL != "":
		if !s.checkGrabURL(w, r, &req.DownloadURL) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		res, err := c.playNZB(ctx, req.DownloadURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		link := res.PlayURL
		if link == "" {
			link = res.PlayPath
		}
		ss.target, ss.snzbID, ss.streamFor, ss.release = c.internalURL(link), res.SessionID, "release", req.ReleaseTitle
		ss.lanURL = res.ExternalPlayURL
		if ss.lanURL == "" {
			ss.lanURL = res.PlayURL
		}
	default:
		writeError(w, http.StatusBadRequest, "Pick a stream or a release.")
		return
	}
	if ss.target == "" {
		writeError(w, http.StatusBadGateway, "StreamNZB gave no play link.")
		return
	}
	if ss.title == "" {
		ss.title = ss.release
	}
	streams.mu.Lock()
	streams.sessions[ss.id] = ss
	streams.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"id": ss.id, "videoUrl": "/api/stream/" + ss.id + "/video", "title": ss.title, "release": ss.release,
		"codec": parser.Parse(ss.release).Codec, "hdr": hdrOf(ss.release), "audio": audioOf(ss.release),
	})
}

var (
	hdrWords   = regexp.MustCompile(`(?i)\b(DV|DoVi|Dolby\.?Vision|HDR10\+?|HDR)\b`)
	audioWords = regexp.MustCompile(`(?i)\b(TrueHD|Atmos|DTS(-?HD|-?X)?|DDP?5\.1|EAC3|AC3|AAC|FLAC|Opus)\b`)
)

func hdrOf(release string) string   { return hdrWords.FindString(release) }
func audioOf(release string) string { return audioWords.FindString(release) }

// GET /api/stream/{id}/video relays the video, passing range requests on so
// the player can seek.
func (s *Server) handleStreamVideo(w http.ResponseWriter, r *http.Request) {
	ss, ok := streams.get(r.PathValue("id"), requestUserID(r))
	if !ok {
		writeError(w, http.StatusNotFound, "This stream has ended. Start it again.")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, ss.target, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, h := range []string{"Range", "If-Range"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	// No overall timeout: a film runs for hours. Redirects (StreamNZB moving
	// to another release) are followed here, so the player never sees a
	// home-network address.
	resp, err := streamRelayClient.Do(req)
	if err != nil {
		if r.Context().Err() == nil {
			writeError(w, http.StatusBadGateway, "StreamNZB didn't answer: "+err.Error())
		}
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	if w.Header().Get("Accept-Ranges") == "" {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	buf := make([]byte, 256<<10)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			streams.touch(ss.id)
		}
		if err != nil {
			return
		}
	}
}

var streamRelayClient = &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 45 * time.Second, IdleConnTimeout: 90 * time.Second}}

func (st *streamStore) touch(id string) {
	st.mu.Lock()
	if ss, ok := st.sessions[id]; ok {
		ss.last = time.Now()
	}
	st.mu.Unlock()
}

// GET /api/stream/{id}/link: the play link for a device at home (the TV,
// or a player app on the home network).
func (s *Server) handleStreamLink(w http.ResponseWriter, r *http.Request) {
	ss, ok := streams.get(r.PathValue("id"), requestUserID(r))
	if !ok {
		writeError(w, http.StatusNotFound, "This stream has ended. Start it again.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"homeUrl": ss.lanURL})
}

// POST /api/stream/{id}/stop ends a stream (the player closed).
func (s *Server) handleStreamStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := requestUserID(r)
	streams.mu.Lock()
	ss, ok := streams.sessions[id]
	if ok && ss.userID == user {
		delete(streams.sessions, id)
	}
	streams.mu.Unlock()
	if ok && ss.userID == user && ss.snzbID != "" {
		if c, err := s.streamNZB(); err == nil {
			c.closeSession(r.Context(), ss.snzbID)
		}
	}
	writeJSON(w, http.StatusOK, nil)
}
