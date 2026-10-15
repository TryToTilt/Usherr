package api

// Home fork: Mediarium TV's live channels, free streams such as Pluto and
// PBS. Mediarium relays them: it fetches each playlist, points every link
// in it back at itself, and passes the video through untouched. That way
// browsers can play streams whose sites don't allow other pages to, and
// Jellyfin gets the same addresses as the rest of the tuner. Links in a
// relayed playlist are ids Mediarium hands out, so the relay only fetches
// what a channel's own playlists name.
//
//   /api/tvlive/{key}/{n}/index.m3u8      channel n's stream
//   /api/tvlive/{key}/{n}/x/{id}/{name}   anything it links to
//
// Pluto streams need a session first (an anonymous one, as its website
// makes); see plutoSession.

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/tv"
)

var liveClient = &http.Client{Timeout: 30 * time.Second}

const liveUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0 Safari/537.36"

func liveGet(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", liveUA)
	return liveClient.Do(req)
}

// ---- Pluto ----

var plutoIDRe = regexp.MustCompile(`(?:plu-|/channel/)([0-9a-f]{24})`)

// plutoChannel is the Pluto channel id in a stream address, if it is one.
func plutoChannel(stream string) string {
	if !strings.Contains(stream, "pluto") && !strings.Contains(stream, "jmp2.uk/plu-") {
		return ""
	}
	if m := plutoIDRe.FindStringSubmatch(stream); m != nil {
		return m[1]
	}
	return ""
}

type plutoSess struct {
	stitcher, params, token string
	until                   time.Time
}

var (
	plutoMu     sync.Mutex
	plutoCur    plutoSess
	plutoClient = func() string {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		h := hex.EncodeToString(b)
		return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	}()
)

// plutoSession starts (or reuses) an anonymous Pluto session.
func plutoSession(ctx context.Context) (plutoSess, error) {
	plutoMu.Lock()
	defer plutoMu.Unlock()
	if plutoCur.token != "" && time.Now().Before(plutoCur.until) {
		return plutoCur, nil
	}
	q := url.Values{
		"appName": {"web"}, "appVersion": {"9.0.0"}, "deviceVersion": {"129.0.0"}, "deviceModel": {"web"},
		"deviceMake": {"chrome"}, "deviceType": {"web"}, "clientID": {plutoClient}, "clientModelNumber": {"1.0.0"},
		"serverSideAds": {"false"},
	}
	resp, err := liveGet(ctx, "https://boot.pluto.tv/v4/start?"+q.Encode())
	if err != nil {
		return plutoSess{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return plutoSess{}, fmt.Errorf("pluto: %s", resp.Status)
	}
	var boot struct {
		Servers struct {
			Stitcher string `json:"stitcher"`
		} `json:"servers"`
		StitcherParams string `json:"stitcherParams"`
		SessionToken   string `json:"sessionToken"`
		RefreshInSec   int    `json:"refreshInSec"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&boot); err != nil {
		return plutoSess{}, err
	}
	if boot.SessionToken == "" || boot.Servers.Stitcher == "" {
		return plutoSess{}, errors.New("pluto: no session")
	}
	life := time.Duration(boot.RefreshInSec) * time.Second
	if life <= 0 || life > 4*time.Hour {
		life = 4 * time.Hour
	}
	plutoCur = plutoSess{stitcher: boot.Servers.Stitcher, params: boot.StitcherParams, token: boot.SessionToken, until: time.Now().Add(life - 5*time.Minute)}
	return plutoCur, nil
}

// liveSource is where channel ch's stream really comes from right now.
func liveSource(ctx context.Context, ch tv.Channel) (string, error) {
	if id := plutoChannel(ch.Rules.StreamURL); id != "" {
		ps, err := plutoSession(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s/v2/stitch/hls/channel/%s/master.m3u8?%s&jwt=%s&masterJWTPassthrough=true", ps.stitcher, id, ps.params, url.QueryEscape(ps.token)), nil
	}
	if ch.Rules.StreamURL == "" {
		return "", errors.New("this channel has no stream address")
	}
	return ch.Rules.StreamURL, nil
}

// ---- the relay ----

func liveSign(key, u string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(u))
	return hex.EncodeToString(m.Sum(nil)[:12])
}

// Relay links are short ids that stand for the upstream address, kept here
// while players use them. (Upstream addresses can be thousands of
// characters, more than ffmpeg, and so Jellyfin, will follow.)
var liveLinks = struct {
	sync.Mutex
	m     map[string]liveLink
	clean time.Time
}{m: map[string]liveLink{}}

type liveLink struct {
	url  string
	seen time.Time
}

func liveRemember(key, u string) string {
	id := liveSign(key, u)
	now := time.Now()
	liveLinks.Lock()
	defer liveLinks.Unlock()
	liveLinks.m[id] = liveLink{u, now}
	if now.Sub(liveLinks.clean) > 5*time.Minute {
		for k, l := range liveLinks.m {
			if now.Sub(l.seen) > 30*time.Minute {
				delete(liveLinks.m, k)
			}
		}
		liveLinks.clean = now
	}
	return id
}

func liveRecall(id string) (string, bool) {
	liveLinks.Lock()
	defer liveLinks.Unlock()
	l, ok := liveLinks.m[id]
	if ok {
		l.seen = time.Now()
		liveLinks.m[id] = l
	}
	return l.url, ok
}

// livePath is the relay address for upstream link u of channel n. Its last
// part is the upstream file name, given an extension when it has none (ext,
// from what the playlist says it is), since players go by it.
func livePath(key string, n int, u, ext string) string {
	name := path.Base(strings.SplitN(u, "?", 2)[0])
	if name == "" || name == "." || name == "/" {
		name = "file"
	}
	if len(name) > 60 {
		name = name[len(name)-60:]
	}
	if path.Ext(name) == "" {
		name += ext
	}
	return fmt.Sprintf("/api/tvlive/%s/%d/x/%s/%s", key, n, liveRemember(key, u), url.PathEscape(name))
}

var uriAttr = regexp.MustCompile(`URI="([^"]+)"`)

// rewritePlaylist points every link in an HLS playlist at the relay.
func rewritePlaylist(body, base, key string, n int) string {
	b, _ := url.Parse(base)
	abs := func(ref string) string {
		r, err := url.Parse(strings.TrimSpace(ref))
		if err != nil || b == nil {
			return ref
		}
		return b.ResolveReference(r).String()
	}
	var out strings.Builder
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	next := ".ts" // what the next plain line is: a segment, or a playlist
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case strings.HasPrefix(line, "#"):
			ext := ".m3u8" // EXT-X-MEDIA, EXT-X-I-FRAME-STREAM-INF
			switch {
			case strings.HasPrefix(line, "#EXT-X-KEY"), strings.HasPrefix(line, "#EXT-X-SESSION-KEY"):
				ext = ".key"
			case strings.HasPrefix(line, "#EXT-X-MAP"):
				ext = ".mp4"
			case strings.HasPrefix(line, "#EXT-X-STREAM-INF"):
				next = ".m3u8"
			case strings.HasPrefix(line, "#EXTINF"):
				next = ".ts"
			}
			line = uriAttr.ReplaceAllStringFunc(line, func(m string) string {
				return `URI="` + livePath(key, n, abs(uriAttr.FindStringSubmatch(m)[1]), ext) + `"`
			})
		case strings.TrimSpace(line) != "":
			line = livePath(key, n, abs(line), next)
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

func (s *Server) registerTVLiveRoutes(public *http.ServeMux) {
	public.HandleFunc("GET /api/tvlive/{key}/{n}/index.m3u8", s.handleTVLiveIndex)
	public.HandleFunc("GET /api/tvlive/{key}/{n}/x/{id}/{name}", s.handleTVLiveRelay)
}

// liveChannel finds enabled live channel number n for a relay request.
func (s *Server) liveChannel(r *http.Request) (tv.Channel, bool) {
	if !s.tvFeedOK(r) {
		return tv.Channel{}, false
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		return tv.Channel{}, false
	}
	chans, err := s.tvRepo().Channels()
	if err != nil {
		return tv.Channel{}, false
	}
	for _, c := range chans {
		if c.Number == n && c.Kind == "live" && c.Enabled {
			return c, true
		}
	}
	return tv.Channel{}, false
}

func (s *Server) handleTVLiveIndex(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.liveChannel(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	src, err := liveSource(r.Context(), ch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.relayLive(w, r, ch, src)
}

func (s *Server) handleTVLiveRelay(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.liveChannel(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	u, ok := liveRecall(r.PathValue("id"))
	if !ok {
		// Forgotten (Mediarium restarted): the player asks for the playlist again.
		http.NotFound(w, r)
		return
	}
	s.relayLive(w, r, ch, u)
}

// relayLive fetches u and passes it on: playlists with their links pointed
// back here, everything else as it is.
func (s *Server) relayLive(w http.ResponseWriter, r *http.Request, ch tv.Channel, u string) {
	resp, err := liveGet(r.Context(), u)
	if err != nil {
		http.Error(w, "the channel's stream didn't answer", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "the channel's stream answered "+resp.Status, http.StatusBadGateway)
		return
	}
	br := bufio.NewReaderSize(resp.Body, 64*1024)
	// A playlist is what reads as one: some streams label their keys and
	// segments as playlists too.
	head, _ := br.Peek(16)
	ct := resp.Header.Get("Content-Type")
	if isPlaylist(head) {
		body, err := io.ReadAll(io.LimitReader(br, 8<<20))
		if err != nil {
			http.Error(w, "the channel's stream broke off", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, rewritePlaylist(string(body), resp.Request.URL.String(), r.PathValue("key"), ch.Number))
		return
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, br)
}

func isPlaylist(head []byte) bool {
	s := strings.TrimLeft(string(head), "\ufeff \t\r\n")
	return strings.HasPrefix(s, "#EXTM3U")
}

// liveStreamPath is where a live channel plays from, for players.
func (s *Server) liveStreamPath(n int) string {
	return fmt.Sprintf("/api/tvlive/%s/%d/index.m3u8", s.tvFeedKey(), n)
}

// ---- the guide ----

var (
	liveGuideMu   sync.Mutex
	liveGuideNext time.Time
)

const liveGuideAhead = 36 * time.Hour

// refreshLiveGuides reads the live channels' guides (every few hours, or
// when forced) and lays them out as their schedules.
func (s *Server) refreshLiveGuides(ctx context.Context, force bool) {
	liveGuideMu.Lock()
	defer liveGuideMu.Unlock()
	now := time.Now()
	if !force && now.Before(liveGuideNext) {
		return
	}
	liveGuideNext = now.Add(4 * time.Hour)
	repo := s.tvRepo()
	chans, err := repo.Channels()
	if err != nil {
		return
	}
	from, to := now.Add(-time.Hour), now.Add(liveGuideAhead)
	byFeed := map[string]map[string]bool{}
	var live []tv.Channel
	for _, c := range chans {
		if c.Kind != "live" {
			continue
		}
		live = append(live, c)
		if c.Rules.GuideURL != "" && c.Rules.GuideID != "" {
			if byFeed[c.Rules.GuideURL] == nil {
				byFeed[c.Rules.GuideURL] = map[string]bool{}
			}
			byFeed[c.Rules.GuideURL][c.Rules.GuideID] = true
		}
	}
	if len(live) == 0 {
		return
	}
	guides := map[string]map[string][]tv.GuideProgram{}
	for feed, want := range byFeed {
		g, err := fetchGuide(ctx, feed, want, from, to)
		if err != nil {
			slog.Info("tv: live guide", "feed", feed, "err", err)
			liveGuideNext = now.Add(30 * time.Minute) // try again sooner
		}
		guides[feed] = g
	}
	for _, c := range live {
		var g []tv.GuideProgram
		if m := guides[c.Rules.GuideURL]; m != nil {
			g = m[c.Rules.GuideID]
		}
		if len(g) == 0 && c.Rules.GuideID != "" {
			// Keep a guide we already have rather than blank it on a bad day.
			if last, _ := repo.LastEnd(c.ID); last.After(now.Add(12 * time.Hour)) {
				continue
			}
		}
		if err := repo.Replace(c.ID, tv.LiveSchedule(c, g, from, to)); err != nil {
			slog.Info("tv: live schedule", "channel", c.Name, "err", err)
		}
	}
}

func fetchGuide(ctx context.Context, feed string, want map[string]bool, from, to time.Time) (map[string][]tv.GuideProgram, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", liveUA)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	var body io.Reader = bufio.NewReaderSize(resp.Body, 256*1024)
	br := body.(*bufio.Reader)
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		body = gz
	}
	return tv.ReadXMLTV(body, want, from, to)
}
