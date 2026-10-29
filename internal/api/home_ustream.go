package api

// Usherr: its own Usenet streaming (internal/download/stream.go). An episode
// is searched for on Usherr's indexers — by its air date for daily shows
// (General Hospital, The Price Is Right, late night), which StreamNZB can't
// find, and by season and episode for the rest — a release is picked and its
// articles checked, and the video inside is read from Usenet as it's
// watched. Nothing is downloaded.
//
// A stream is served at /api/ustream/{token}/{name}, open to whoever has the
// token (the player, ffmpeg, a TV on the home network), like a stream from
// StreamNZB.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
)

type ustreamSession struct {
	file    *download.StreamFile
	release string
	last    time.Time
}

var (
	ustreamMu       sync.Mutex
	ustreamSessions = map[string]*ustreamSession{}
	// ustreamFound keeps what was found for an episode, so it's searched
	// once, not on every tune.
	ustreamFound = map[string]ustreamHit{}
)

type ustreamHit struct {
	token   string
	release string
	at      time.Time
}

const ustreamKeep = 6 * time.Hour

func init() {
	go func() {
		for range time.Tick(10 * time.Minute) {
			ustreamMu.Lock()
			for k, s := range ustreamSessions {
				if time.Since(s.last) > ustreamKeep {
					delete(ustreamSessions, k)
				}
			}
			for k, h := range ustreamFound {
				if _, ok := ustreamSessions[h.token]; !ok || time.Since(h.at) > ustreamKeep {
					delete(ustreamFound, k)
				}
			}
			ustreamMu.Unlock()
		}
	}()
}

func (s *Server) registerUStreamRoutes(public *http.ServeMux) {
	public.HandleFunc("GET /api/ustream/{token}/{name}", s.handleUStream)
}

// GET /api/ustream/{token}/{name}: the video, with seeking (Range).
func (s *Server) handleUStream(w http.ResponseWriter, r *http.Request) {
	s.serveUStream(w, r, r.PathValue("token"))
}

// serveUStream serves an open session's video, with seeking (Range); it
// answers 404 for a session that isn't there. The Stremio and Nuvio add-on
// serves its streams through it too.
func (s *Server) serveUStream(w http.ResponseWriter, r *http.Request, token string) {
	ustreamMu.Lock()
	sess, ok := ustreamSessions[token]
	if ok {
		sess.last = time.Now()
	}
	ustreamMu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(sess.file.Name)))
	switch strings.ToLower(filepath.Ext(sess.file.Name)) {
	case ".mkv":
		ct = "video/x-matroska"
	case ".ts", ".m2ts":
		ct = "video/mp2t"
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, sess.file.Name, time.Time{}, readerAtSection(sess.file))
}

// ustreamURL is a session's address as Usherr itself reaches it.
func (s *Server) ustreamURL(token, name string) string {
	port := 8264
	if s.cfg.Port > 0 {
		port = s.cfg.Port
	}
	return fmt.Sprintf("http://127.0.0.1:%d/api/ustream/%s/%s", port, token, urlPathName(name))
}

func urlPathName(name string) string {
	clean := regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(name, ".")
	if clean == "" {
		clean = "video.mkv"
	}
	return clean
}

// ---- finding an episode ----

var (
	wordsOnly = regexp.MustCompile(`[^a-z0-9]+`)
	resRank   = map[string]int{"1080p": 0, "720p": 1, "2160p": 2}
	resRe     = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p)\b`)
)

func normWords(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "&", " and "))
	return strings.TrimSpace(wordsOnly.ReplaceAllString(s, " "))
}

// showNames are the ways a show's name starts a release title: as it is,
// without "The", and without a trailing "Live" (Jimmy Kimmel Live! is
// posted as Jimmy.Kimmel).
func showNames(show string) []string {
	n := normWords(show)
	out := []string{n}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	add(strings.TrimPrefix(n, "the "))
	add(strings.TrimSuffix(n, " live"))
	add(strings.TrimSuffix(strings.TrimPrefix(n, "the "), " live"))
	// Talk shows are posted under the host: The Tonight Show Starring Jimmy
	// Fallon is Jimmy.Fallon, Late Night with Seth Meyers is Seth.Meyers.
	for _, sep := range []string{" starring ", " with "} {
		if _, host, ok := strings.Cut(n, sep); ok && len(strings.Fields(host)) >= 2 {
			add(host)
		}
	}
	return out
}

// searchName is the name a show is searched by: its host's for a talk show,
// else its name without "The" and "Live".
func searchName(show string) string {
	n := normWords(show)
	for _, sep := range []string{" starring ", " with "} {
		if _, host, ok := strings.Cut(n, sep); ok && len(strings.Fields(host)) == 2 {
			return host
		}
	}
	return strings.TrimSuffix(strings.TrimPrefix(n, "the "), " live")
}

var (
	months = []string{"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}
	epNum  = regexp.MustCompile(`\bs0*(\d+)e0*(\d+)\b`)
)

// hasDate finds day d in words: 2026 10 16, October 16 2026, 16 Oct 2026,
// Friday 16 October 2026 (the year near the day and month).
func hasDate(words []string, d time.Time) bool {
	y, m, day := strconv.Itoa(d.Year()), int(d.Month()), d.Day()
	isDay := func(w string) bool { n, err := strconv.Atoi(w); return err == nil && n == day }
	isMonth := func(w string) bool {
		full := months[m-1]
		return w == full || (len(w) >= 3 && strings.HasPrefix(full, w))
	}
	for i, w := range words {
		if w == y && i+2 < len(words) {
			if n, err := strconv.Atoi(words[i+1]); err == nil && n == m && isDay(words[i+2]) {
				return true
			}
		}
		if !isMonth(w) {
			continue
		}
		dayNear := (i > 0 && isDay(words[i-1])) || (i+1 < len(words) && isDay(words[i+1]))
		yearNear := false
		for j := max(i-3, 0); j <= min(i+3, len(words)-1); j++ {
			if words[j] == y {
				yearNear = true
			}
		}
		if dayNear && yearNear {
			return true
		}
	}
	return false
}

var weekdays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

func episodeLead(w string) bool {
	if _, err := strconv.Atoi(w); err == nil || w == "episode" || w == "ep" || epNum.MatchString(w) {
		return true
	}
	for _, m := range months {
		if w == m || (len(w) >= 3 && strings.HasPrefix(m, w)) {
			return true
		}
	}
	for _, d := range weekdays {
		if w == d || (len(w) >= 3 && strings.HasPrefix(d, w)) {
			return true
		}
	}
	return false
}

// releaseFor says whether a release title is this show's episode: the
// show's name first, then its air date (in any of the ways posters write
// it) or its season and episode.
func releaseFor(title, show string, season, episode int, airDate string) bool {
	t := normWords(title)
	var rest string
	matched := false
	for _, name := range showNames(show) {
		for _, lead := range []string{"", "the "} {
			if strings.HasPrefix(t+" ", lead+name+" ") {
				rest, matched = strings.TrimPrefix(t, lead+name), true
			}
		}
	}
	if !matched {
		return false
	}
	words := strings.Fields(rest)
	// Right after the name comes the episode: a date, a number, S01E02,
	// "Episode": not more words of another show's name ("The Price Is
	// Right At Night").
	if len(words) == 0 || !episodeLead(words[0]) {
		return false
	}
	if airDate != "" {
		if d, err := time.Parse("2006-01-02", airDate); err == nil && hasDate(words, d) {
			return true
		}
	}
	if season > 0 && episode > 0 {
		for _, m := range epNum.FindAllStringSubmatch(rest, -1) {
			if m[1] == strconv.Itoa(season) && m[2] == strconv.Itoa(episode) {
				return true
			}
		}
	}
	return false
}

// rankRelease orders candidates: 1080p, 720p, then 4K; WEB before others;
// then bigger (better bitrate) within the same kind.
func rankRelease(a, b indexers.Result) bool {
	if da, db := pureDV(a.Title), pureDV(b.Title); da != db {
		return db // Dolby Vision alone: wrong colours in a browser
	}
	ra, rb := resOf(a.Title), resOf(b.Title)
	if ra != rb {
		return ra < rb
	}
	wa, wb := strings.Contains(strings.ToLower(a.Title), "web"), strings.Contains(strings.ToLower(b.Title), "web")
	if wa != wb {
		return wa
	}
	return a.SizeBytes > b.SizeBytes
}

func resOf(title string) int {
	if m := resRe.FindString(title); m != "" {
		if r, ok := resRank[strings.ToLower(m)]; ok {
			return r
		}
		return 3
	}
	return 4
}

// usenetIndexers are the indexers a Usenet stream can come from: torrent
// sites are left out (they can't stream from Usenet, and some sit behind a
// slow Cloudflare check).
func (s *Server) usenetIndexers() ([]indexers.Instance, error) {
	all, err := s.searchableIndexers()
	if err != nil {
		return nil, err
	}
	out := make([]indexers.Instance, 0, len(all))
	for _, inst := range all {
		if inst.Protocol != indexers.ProtocolTorrent {
			out = append(out, inst)
		}
	}
	return out, nil
}

// errNoRelease means nothing on the indexers is this episode (yet).
var errNoRelease = errors.New("no release of this episode on the indexers")

// ustreamEpisode finds an episode on Usenet and opens it for streaming:
// by date first for a daily show, then by season and episode. It returns
// the stream's address and the release's name.
func (s *Server) ustreamEpisode(ctx context.Context, show string, season, episode int, airDate string) (string, string, error) {
	key := fmt.Sprintf("%s|%d|%d|%s", strings.ToLower(show), season, episode, airDate)
	ustreamMu.Lock()
	if h, ok := ustreamFound[key]; ok {
		if sess, ok := ustreamSessions[h.token]; ok {
			sess.last = time.Now()
			ustreamMu.Unlock()
			return s.ustreamURL(h.token, sess.file.Name), h.release, nil
		}
	}
	ustreamMu.Unlock()

	instances, err := s.usenetIndexers()
	if err != nil {
		return "", "", err
	}
	servers, err := s.ClientRepo.List()
	if err != nil || len(servers) == 0 {
		return "", "", errors.New("no Usenet server is set up")
	}
	configs := make([]download.ClientConfig, len(servers))
	for i, sc := range servers {
		configs[i] = sc.Config
	}
	// A daily show: its name and year finds its newest releases, however
	// they're named; then the date as scene releases write it.
	var queries []string
	if airDate != "" {
		if d, err := time.Parse("2006-01-02", airDate); err == nil {
			name := searchName(show)
			queries = append(queries, fmt.Sprintf("%s %d", name, d.Year()), fmt.Sprintf("%s %04d %02d %02d", name, d.Year(), int(d.Month()), d.Day()))
		}
	}
	if season > 0 && episode > 0 {
		queries = append(queries, tvSearchQuery(show, season, episode))
	}
	tried := map[string]bool{}
	for _, q := range queries {
		var found []indexers.Result
		for _, o := range indexers.SearchAll(ctx, instances, q, tvCategory) {
			for _, r := range o.Results {
				if r.Torrent || r.Protocol == indexers.ProtocolTorrent || isMagnetURI(r.DownloadURL) || tried[r.DownloadURL] || tried[r.Title] {
					continue
				}
				if r.SizeBytes > 0 && r.SizeBytes < 50<<20 {
					continue // a sample or a clip
				}
				if releaseFor(r.Title, show, season, episode, airDate) {
					found = append(found, r)
				}
			}
		}
		sort.SliceStable(found, func(i, j int) bool { return rankRelease(found[i], found[j]) })
		attempts := 0
		for _, r := range found {
			if attempts >= 3 {
				break
			}
			if tried[r.Title] {
				continue // the same release from another indexer
			}
			attempts++
			tried[r.DownloadURL], tried[r.Title] = true, true
			url, err := s.ustreamOpen(ctx, configs, r)
			if err != nil {
				slog.Info("ustream: release skipped", "release", r.Title, "err", err)
				continue
			}
			ustreamMu.Lock()
			for k, sess := range ustreamSessions {
				if sess.release == r.Title {
					ustreamFound[key] = ustreamHit{token: k, release: r.Title, at: time.Now()}
				}
			}
			ustreamMu.Unlock()
			return url, r.Title, nil
		}
	}
	return "", "", errNoRelease
}

// ustreamOpen fetches a release's NZB, checks its articles are there and
// opens the video in it.
func (s *Server) ustreamOpen(ctx context.Context, servers []download.ClientConfig, r indexers.Result) (string, error) {
	if s.nzbBad(r.DownloadURL) { // its articles were gone today: not fetched again
		return "", fmt.Errorf("articles missing (found earlier today)")
	}
	data, err := s.fetchRelease(ctx, r.DownloadURL)
	if err != nil {
		return "", err
	}
	if err := notAnNZB(data, r.DownloadURL); err != nil {
		return "", err
	}
	nzb, err := download.ParseNZB(data)
	if err != nil {
		return "", err
	}
	if res := download.Precheck(ctx, servers, nzb, download.PrecheckOptions{Timeout: 20 * time.Second}); res.Hopeless() {
		s.markNZBBad(r.DownloadURL)
		return "", fmt.Errorf("articles missing: %s", res.Summary())
	}
	octx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	f, err := download.OpenStream(octx, servers, nzb)
	if err != nil {
		return "", err
	}
	token := randomID("us_")
	ustreamMu.Lock()
	ustreamSessions[token] = &ustreamSession{file: f, release: r.Title, last: time.Now()}
	ustreamMu.Unlock()
	slog.Info("ustream: opened", "release", r.Title, "file", f.Name, "size", strconv.FormatInt(f.Size>>20, 10)+" MB")
	return s.ustreamURL(token, f.Name), nil
}

// tvStreamOwn finds a streamed episode with Usherr's own engine: by its air
// date when it's a daily show (a season of more than 40 episodes), and by
// season and episode.
func (s *Server) tvStreamOwn(ctx context.Context, path string, tmdbID int64, season, episode int) (tvStreamSrc, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	tm := s.TMDB()
	if tm == nil {
		return tvStreamSrc{}, errors.New("TMDB isn't set up")
	}
	show, err := tm.GetShowFull(ctx, int(tmdbID))
	if err != nil {
		return tvStreamSrc{}, err
	}
	airDate := ""
	if eps, err := tm.GetSeason(ctx, int(tmdbID), season); err == nil && len(eps) > 40 {
		for _, e := range eps {
			if e.Episode == episode {
				airDate = e.AirDate
			}
		}
	}
	u, release, err := s.ustreamEpisode(ctx, show.Name, season, episode, airDate)
	if err != nil {
		return tvStreamSrc{}, err
	}
	pr, err := probeStreamTwice(ctx, u)
	if err != nil {
		return tvStreamSrc{}, fmt.Errorf("the stream didn't play: %v", err)
	}
	if pr.secs < 60 {
		return tvStreamSrc{}, fmt.Errorf("the stream didn't play: it is only %.0f seconds long", pr.secs)
	}
	s.tvRepo().SetDuration(path, 0, pr.secs)
	return tvStreamSrc{url: u, release: release, at: time.Now(), browserOK: pr.browserOK(), copyVideo: pr.copyVideo()}, nil
}

func readerAtSection(f *download.StreamFile) io.ReadSeeker { return io.NewSectionReader(f, 0, f.Size) }
