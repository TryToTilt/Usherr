package api

// Usherr: the choice of streaming engine. "Stream now", the streamed library
// and streamed Live TV channels play through StreamNZB or through Usherr's
// own engine (home_ustream.go, internal/download/stream.go); one setting says
// which goes first, and the other is the fallback when the first finds
// nothing. This file holds the setting, the device profiles ("phone",
// "TV": which quality and size a release may be), the search for a title's
// releases, and the status the Streaming card in Settings > Extras shows.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
)

const (
	keyStreamEngine   = "home.streaming_engine" // "streamnzb" or "usherr"
	keyStreamProfiles = "home.stream_profiles"  // JSON list of streamProfile
	engineStreamNZB   = "streamnzb"
	engineUsherr      = "usherr"
)

// streamEngine is the engine that goes first: the one the person chose. Until
// they choose, a new install streams with Usherr's own engine, and one that
// already has StreamNZB connected keeps it.
func (s *Server) streamEngine() string {
	v, _ := s.Settings.Get(keyStreamEngine)
	switch strings.TrimSpace(v) {
	case engineUsherr:
		return engineUsherr
	case engineStreamNZB:
		return engineStreamNZB
	}
	if s.streamNZBConfigured() {
		return engineStreamNZB
	}
	return engineUsherr
}

// streamNZBConfigured is whether StreamNZB was set up here: its address or
// token saved, or its config.json mapped in.
func (s *Server) streamNZBConfigured() bool {
	for _, k := range []string{keyStreamNZBURL, keyStreamNZBToken} {
		if v, _ := s.Settings.Get(k); strings.TrimSpace(v) != "" {
			return true
		}
	}
	return tokenFromStreamNZBConfig(streamNZBConfig) != ""
}

// ---- device profiles ----

// streamProfile says what a release may be for one quality: a phone takes
// 720p, a TV 4K. A release above the quality, over the size, or with a word
// in Exclude in its name is not offered.
type streamProfile struct {
	Quality   string  `json:"quality"`   // "4K", "1080p" or "720p"
	MaxSizeGB float64 `json:"maxSizeGB"` // 0: no limit
	Exclude   string  `json:"exclude"`   // words, separated by commas
}

func defaultStreamProfiles() []streamProfile {
	// AV1 is left out: the TV can't play it.
	return []streamProfile{
		{Quality: "4K", MaxSizeGB: 90, Exclude: "av1"},
		{Quality: "1080p", MaxSizeGB: 20, Exclude: "av1"},
		{Quality: "720p", MaxSizeGB: 6, Exclude: "av1"},
	}
}

func (s *Server) streamProfiles() []streamProfile {
	def := defaultStreamProfiles()
	raw, _ := s.Settings.Get(keyStreamProfiles)
	if strings.TrimSpace(raw) == "" {
		return def
	}
	var saved []streamProfile
	if json.Unmarshal([]byte(raw), &saved) != nil {
		return def
	}
	// A profile that wasn't saved keeps its default.
	for i, d := range def {
		for _, p := range saved {
			if p.Quality == d.Quality {
				def[i] = p
			}
		}
	}
	return def
}

func qualityHeight(q string) int {
	switch strings.ToLower(q) {
	case "4k", "2160p":
		return 2160
	case "1080p":
		return 1080
	case "720p":
		return 720
	}
	return 1080
}

var (
	dvRe  = regexp.MustCompile(`(?i)(^|[^a-z0-9])(dv|dovi|dolby[ ._-]?vision)([^a-z0-9]|$)`)
	hdrRe = regexp.MustCompile(`(?i)(^|[^a-z0-9])(hdr|hdr10|hdr10\+|hdr10p|hdr10plus)([^a-z0-9]|$)`)
)

// pureDV says whether a release is Dolby Vision without an HDR10 picture
// under it ("DV" alone, not "DV HDR10" or a Hybrid with HDR).
func pureDV(title string) bool {
	return dvRe.MatchString(title) && !hdrRe.MatchString(title)
}

var heightRe = regexp.MustCompile(`(?i)\b(2160p|4k|uhd|1080p|1080i|720p|576p|480p)\b`)

// heightOf is the picture height a release name says; 0 when it doesn't.
func heightOf(title string) int {
	switch strings.ToLower(heightRe.FindString(title)) {
	case "2160p", "4k", "uhd":
		return 2160
	case "1080p", "1080i":
		return 1080
	case "720p":
		return 720
	case "576p", "480p":
		return 480
	}
	return 0
}

func (p streamProfile) excluded(title string) bool {
	low := strings.ToLower(title)
	for _, w := range strings.Split(p.Exclude, ",") {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" && strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// allows says whether a release suits the profile.
func (p streamProfile) allows(title string, size int64) bool {
	if h := heightOf(title); h > qualityHeight(p.Quality) {
		return false
	}
	if p.MaxSizeGB > 0 && size > 0 && float64(size) > p.MaxSizeGB*(1<<30) {
		return false
	}
	return !p.excluded(title)
}

// less orders releases for the profile: the biggest picture that suits it
// first, a release that doesn't say its size last, WEB before the rest, then
// the bigger file (a better bitrate).
func (p streamProfile) less(a, b indexers.Result) bool {
	// Dolby Vision alone (profile 5, most WEB "DV" releases) plays with
	// purple and green colours everywhere but a Dolby Vision screen, and
	// can't be converted right: any other release goes first.
	if da, db := pureDV(a.Title), pureDV(b.Title); da != db {
		return db
	}
	ha, hb := heightOf(a.Title), heightOf(b.Title)
	if ha != hb {
		return ha > hb
	}
	wa, wb := strings.Contains(strings.ToLower(a.Title), "web"), strings.Contains(strings.ToLower(b.Title), "web")
	if wa != wb {
		return wa
	}
	return a.SizeBytes > b.SizeBytes
}

func (s *Server) streamProfile(quality string) streamProfile {
	for _, p := range s.streamProfiles() {
		if strings.EqualFold(p.Quality, quality) {
			return p
		}
	}
	return streamProfile{Quality: quality}
}

// ---- searching for a title ----

// ownQuery is a title to find releases for: a movie, or an episode (with its
// air date when it's a daily show).
type ownQuery struct {
	Kind    string // "movie" or "series"
	Title   string
	Year    int
	Season  int
	Episode int
	AirDate string
	Quality string // the profile's; "" for the engine's usual order
}

func (q ownQuery) key() string {
	return fmt.Sprintf("%s|%s|%d|%d|%d|%s|%s", q.Kind, strings.ToLower(q.Title), q.Year, q.Season, q.Episode, q.AirDate, q.Quality)
}

type ownFound struct {
	list []indexers.Result
	at   time.Time
}

var (
	ownMu    sync.Mutex
	ownCache = map[string]ownFound{}
)

const (
	ownKeepFound = 20 * time.Minute // what was found: the same title isn't searched again and again
	ownKeepNone  = 2 * time.Minute  // nothing found: an episode may be posted any minute
)

var errNoOwnServers = errors.New("no Usenet server is set up")

var yearWord = regexp.MustCompile(`^(19|20)\d\d$`)

// movieReleaseFor says whether a release title is this film: its name first,
// then its year (a year off is allowed: posters and TMDB differ).
func movieReleaseFor(title, name string, year int) bool {
	t := normWords(title)
	if epNum.MatchString(t) {
		return false
	}
	for _, n := range showNames(name) {
		rest, ok := strings.CutPrefix(t+" ", n+" ")
		if !ok {
			continue
		}
		words := strings.Fields(rest)
		if len(words) == 0 {
			return false
		}
		// The year comes right after the name.
		if y, err := strconv.Atoi(words[0]); err == nil && yearWord.MatchString(words[0]) {
			return year == 0 || (y >= year-1 && y <= year+1)
		}
		return false
	}
	return false
}

// ownCandidates lists the releases of a title on the indexers that suit the
// quality, best first. Searches are remembered for a few minutes.
func (s *Server) ownCandidates(ctx context.Context, q ownQuery) ([]indexers.Result, error) {
	key := q.key()
	ownMu.Lock()
	if f, ok := ownCache[key]; ok {
		ttl := ownKeepFound
		if len(f.list) == 0 {
			ttl = ownKeepNone
		}
		if time.Since(f.at) < ttl {
			ownMu.Unlock()
			return s.reliableFirst(ctx, append([]indexers.Result(nil), f.list...)), nil
		}
	}
	ownMu.Unlock()

	instances, err := s.usenetIndexers()
	if err != nil {
		return nil, err
	}
	var queries []string
	cats := tvCategory
	switch q.Kind {
	case "movie":
		cats = movieCategory
		if q.Year > 0 {
			queries = append(queries, fmt.Sprintf("%s %d", q.Title, q.Year))
		}
		queries = append(queries, q.Title)
	default:
		if q.AirDate != "" {
			if d, err := time.Parse("2006-01-02", q.AirDate); err == nil {
				name := searchName(q.Title)
				queries = append(queries, fmt.Sprintf("%s %d", name, d.Year()), fmt.Sprintf("%s %04d %02d %02d", name, d.Year(), int(d.Month()), d.Day()))
			}
		}
		if q.Season > 0 && q.Episode > 0 {
			queries = append(queries, tvSearchQuery(q.Title, q.Season, q.Episode))
		}
	}
	prof := s.streamProfile(q.Quality)
	seen := map[string]bool{}
	var all []indexers.Result
	for _, query := range queries {
		var found []indexers.Result
		for _, o := range indexers.SearchAll(ctx, instances, query, cats) {
			for _, r := range o.Results {
				if r.Torrent || r.Protocol == indexers.ProtocolTorrent || isMagnetURI(r.DownloadURL) || seen[r.Title] {
					continue
				}
				if r.SizeBytes > 0 && r.SizeBytes < 50<<20 {
					continue // a sample or a clip
				}
				ok := false
				if q.Kind == "movie" {
					ok = movieReleaseFor(r.Title, q.Title, q.Year)
				} else {
					ok = releaseFor(r.Title, q.Title, q.Season, q.Episode, q.AirDate)
				}
				if !ok || (q.Quality != "" && !prof.allows(r.Title, r.SizeBytes)) {
					continue
				}
				seen[r.Title] = true
				found = append(found, r)
			}
		}
		if q.Quality != "" {
			sort.SliceStable(found, func(i, j int) bool { return prof.less(found[i], found[j]) })
		} else {
			sort.SliceStable(found, func(i, j int) bool { return rankRelease(found[i], found[j]) })
		}
		all = append(all, found...)
	}
	ownMu.Lock()
	ownCache[key] = ownFound{list: all, at: time.Now()}
	for k, f := range ownCache { // forget the old
		if time.Since(f.at) > time.Hour {
			delete(ownCache, k)
		}
	}
	ownMu.Unlock()
	return s.reliableFirst(ctx, append([]indexers.Result(nil), all...)), nil // groups and indexers that keep failing go last
}

// ownServers are the Usenet servers streams read from.
func (s *Server) ownServers() ([]download.ClientConfig, error) {
	servers, err := s.ClientRepo.List()
	if err != nil || len(servers) == 0 {
		return nil, errNoOwnServers
	}
	configs := make([]download.ClientConfig, len(servers))
	for i, sc := range servers {
		configs[i] = sc.Config
	}
	return configs, nil
}

// ownOpened is a release opened for streaming.
type ownOpened struct {
	token   string // the session, closed when the stream ends
	url     string // as Usherr itself reaches it
	lanURL  string // as a device at home reaches it; "" until an address is set
	release string
	rest    []indexers.Result // what's left in line, for "another release"
}

// ownOpenFirst opens the first release in the list that can be streamed
// (up to four tries), and answers the rest of the list.
func (s *Server) ownOpenFirst(ctx context.Context, list []indexers.Result) (ownOpened, error) {
	servers, err := s.ownServers()
	if err != nil {
		return ownOpened{}, err
	}
	var last error
	for i, r := range list {
		if i >= 4 {
			break
		}
		u, err := s.ustreamOpen(ctx, servers, r)
		if err != nil {
			last = err
			if ctx.Err() == nil {
				s.noteStream(r.Title, r.IndexerName, false) // it wouldn't open
			}
			continue
		}
		s.noteStream(r.Title, r.IndexerName, true)
		return ownOpened{token: ustreamToken(u), url: u, lanURL: s.ownLANURL(u), release: r.Title, rest: list[i+1:]}, nil
	}
	if last == nil {
		last = errNoRelease
	}
	return ownOpened{}, last
}

// ownLANURL is a stream's address for a device at home, when Usherr's
// address on the network is set (Settings > Extras).
func (s *Server) ownLANURL(local string) string {
	addr, _ := s.Settings.Get(keyHomeAddress)
	addr = strings.TrimRight(strings.TrimSpace(addr), "/")
	_, tail, ok := strings.Cut(local, "/api/ustream/")
	if addr == "" || !ok {
		return ""
	}
	return addr + "/api/ustream/" + tail
}

// ownTitleQuery makes the query for a title from TMDB.
func (s *Server) ownTitleQuery(ctx context.Context, kind string, tmdbID, season, episode int, quality string) (ownQuery, error) {
	tm := s.TMDB()
	if tm == nil {
		return ownQuery{}, errors.New("TMDB isn't set up")
	}
	q := ownQuery{Kind: kind, Season: season, Episode: episode, Quality: quality}
	if kind == "movie" {
		d, err := tm.GetMovieDetail(ctx, tmdbID)
		if err != nil {
			return ownQuery{}, fmt.Errorf("couldn't look the movie up: %w", err)
		}
		q.Title = d.Title
		if len(d.ReleaseDate) >= 4 {
			q.Year, _ = strconv.Atoi(d.ReleaseDate[:4])
		}
		return q, nil
	}
	show, err := tm.GetShowFull(ctx, tmdbID)
	if err != nil {
		return ownQuery{}, fmt.Errorf("couldn't look the show up: %w", err)
	}
	q.Title = show.Name
	// A daily show (a season of more than 40 episodes) is posted by date.
	if eps, err := tm.GetSeason(ctx, tmdbID, season); err == nil && len(eps) > 40 {
		for _, e := range eps {
			if e.Episode == episode {
				q.AirDate = e.AirDate
			}
		}
	}
	return q, nil
}

// ---- the setting and the card's status ----

type streamingEngineStatus struct {
	Ready   bool   `json:"ready"`
	Problem string `json:"problem,omitempty"`
}

// GET /api/home/streaming: the engine, what each can do, the profiles, and
// what streaming is using right now.
func (s *Server) handleGetStreamingEngine(w http.ResponseWriter, r *http.Request) {
	own := streamingEngineStatus{Ready: true}
	servers, err := s.ClientRepo.List()
	switch {
	case err != nil || len(servers) == 0:
		own = streamingEngineStatus{Problem: "Add a Usenet server first (Settings > Downloading > Usenet)."}
	default:
		if inst, _ := s.searchableIndexers(); len(inst) == 0 {
			own = streamingEngineStatus{Problem: "Add an indexer first (Settings > Downloading > Indexers)."}
		}
	}
	nzb := streamingEngineStatus{Ready: true}
	if ok, problem := s.streamingCheck(r.Context()); !ok {
		nzb = streamingEngineStatus{Problem: problem}
	}
	now := download.StreamBytes()
	streamSpeedMu.Lock()
	speed := 0.0
	if !streamSpeedAt.IsZero() && time.Since(streamSpeedAt) > 0 && time.Since(streamSpeedAt) < time.Minute {
		speed = float64(now-streamSpeedBytes) / time.Since(streamSpeedAt).Seconds()
	}
	streamSpeedBytes, streamSpeedAt = now, time.Now()
	streamSpeedMu.Unlock()
	ustreamMu.Lock()
	type sess struct {
		Release string `json:"release"`
		File    string `json:"file"`
		SizeMB  int64  `json:"sizeMB"`
		IdleSec int    `json:"idleSec"`
	}
	sessions := []sess{} // a list even when empty: the page reads its length
	for _, se := range ustreamSessions {
		sessions = append(sessions, sess{Release: se.release, File: se.file.Name, SizeMB: se.file.Size >> 20, IdleSec: int(time.Since(se.last).Seconds())})
	}
	ustreamMu.Unlock()
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].IdleSec < sessions[j].IdleSec })
	writeJSON(w, http.StatusOK, map[string]any{
		"engine":   s.streamEngine(),
		"engines":  map[string]streamingEngineStatus{engineStreamNZB: nzb, engineUsherr: own},
		"profiles": s.streamProfiles(),
		"servers":  download.StreamServerUse(),
		"sessions": sessions,
		"speed":    speed,
		"indexers": s.streamingIndexerLines(),
	})
}

var (
	streamSpeedMu    sync.Mutex
	streamSpeedBytes int64
	streamSpeedAt    time.Time
)

type indexerLine struct {
	Name  string `json:"name"`
	Used  int    `json:"used"`
	Limit int    `json:"limit"` // 0: no daily limit known
	Note  string `json:"note,omitempty"`
}

// streamingIndexerLines are the searches each indexer has done today and
// what it allows.
func (s *Server) streamingIndexerLines() []indexerLine {
	list, err := s.IndexerRepo.List()
	if err != nil {
		return nil
	}
	var out []indexerLine
	for _, inst := range list {
		if !inst.Enabled || inst.Protocol == indexers.ProtocolTorrent {
			continue
		}
		p := s.toIndexerPayload(inst)
		out = append(out, indexerLine{Name: inst.Name, Used: p.RequestsToday, Limit: p.DailyLimit, Note: p.LimitNote})
	}
	return out
}

// PUT /api/home/streaming {engine, profiles}: only what is sent changes.
func (s *Server) handlePutStreamingEngine(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Engine   *string          `json:"engine"`
		Profiles *[]streamProfile `json:"profiles"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if req.Engine != nil {
		e := strings.TrimSpace(*req.Engine)
		if e != engineStreamNZB && e != engineUsherr {
			writeError(w, http.StatusBadRequest, "Choose StreamNZB or Usherr.")
			return
		}
		if err := s.Settings.Set(keyStreamEngine, e, false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.Profiles != nil {
		for i, p := range *req.Profiles {
			if p.MaxSizeGB < 0 || p.MaxSizeGB > 500 || len(p.Exclude) > 200 {
				writeError(w, http.StatusBadRequest, "A profile's size limit is between 0 and 500 GB, and its excluded words are short.")
				return
			}
			(*req.Profiles)[i].Quality = strings.TrimSpace(p.Quality)
		}
		b, _ := json.Marshal(*req.Profiles)
		if err := s.Settings.Set(keyStreamProfiles, string(b), false); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.handleGetStreamingEngine(w, r)
}

// ---- Stream now with the own engine ----

// ownProblem says what is missing for the own engine; "" when it can stream.
func (s *Server) ownProblem() string {
	if servers, err := s.ClientRepo.List(); err != nil || len(servers) == 0 {
		return "Add a Usenet server first (Settings > Downloading > Usenet)."
	}
	if inst, _ := s.searchableIndexers(); len(inst) == 0 {
		return "Add an indexer first (Settings > Downloading > Indexers)."
	}
	return ""
}

// streamNZBReady is whether StreamNZB is set up and answering.
func (s *Server) streamNZBReady(ctx context.Context) bool {
	ok, _ := s.streamingCheck(ctx)
	return ok
}

func ownOpenError(err error) string {
	if errors.Is(err, errNoRelease) {
		return "no release was found."
	}
	return err.Error()
}

// ownStreamOptions answers /api/stream/options with the own engine's
// releases. It reports false (and why) when it found none, so the caller can
// try StreamNZB; it writes nothing then.
func (s *Server) ownStreamOptions(w http.ResponseWriter, r *http.Request, kind string, tmdbID, season, episode int, quality string) (bool, error) {
	if p := s.ownProblem(); p != "" {
		return false, errors.New(p)
	}
	// Not tied to the page's request, like StreamNZB's: a search that is
	// dropped halfway is searched again.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
	defer cancel()
	q, err := s.ownTitleQuery(ctx, kind, tmdbID, season, episode, quality)
	if err != nil {
		return false, err
	}
	list, err := s.ownCandidates(ctx, q)
	if err != nil {
		return false, err
	}
	if len(list) == 0 {
		return false, errors.New("Usherr found no release of this on your indexers right now.")
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": "Usherr " + quality, "options": s.offerOwnOptions(r, list)})
	return true, nil
}

// offerOwnOptions keeps the first releases of a list as stream options for
// the person asking (each with the rest in line behind it) and answers them.
func (s *Server) offerOwnOptions(r *http.Request, list []indexers.Result) []streamOptionPayload {
	if len(list) > 12 {
		list = list[:12]
	}
	user := requestUserID(r)
	out := make([]streamOptionPayload, 0, len(list))
	streams.mu.Lock()
	for i, c := range list {
		res := c
		// The others, the next in line first, then the ones before it.
		rest := append(append([]indexers.Result(nil), list[i+1:]...), list[:i]...)
		oid := randomID("opt_")
		label := c.Title
		var bits []string
		if c.SizeBytes > 0 {
			bits = append(bits, fmt.Sprintf("%.1f GB", float64(c.SizeBytes)/(1<<30)))
		}
		if c.IndexerName != "" {
			bits = append(bits, c.IndexerName)
		}
		if len(bits) > 0 {
			label += "\n" + strings.Join(bits, " · ")
		}
		streams.options[oid] = streamOption{title: label, userID: user, at: time.Now(), res: &res, alts: rest}
		rel := parser.Parse(c.Title)
		out = append(out, streamOptionPayload{ID: oid, Name: "Usherr", Title: label, Quality: rel.Resolution, Codec: rel.Codec})
	}
	streams.mu.Unlock()
	return out
}

// ownSearchedSource is the streamed library's way to a playing file with the
// own engine: the title's releases at the quality (the best there is when it
// isn't given), passing over those in skip.
func (s *Server) ownSearchedSource(ctx context.Context, f streamedFileRow, quality string, skip []string) (*streamedPlay, error) {
	if quality == "" {
		quality = "4K"
	}
	kind := "movie"
	if f.kind == "series" {
		kind = "series"
	}
	q, err := s.ownTitleQuery(ctx, kind, f.tmdb, f.season, f.episode, quality)
	if err != nil {
		return nil, err
	}
	list, err := s.ownCandidates(ctx, q)
	if err != nil {
		return nil, err
	}
	var fresh []indexers.Result
	for _, r := range list {
		if !slices.Contains(skip, r.Title) {
			fresh = append(fresh, r)
		}
	}
	if len(fresh) == 0 {
		return nil, errors.New("Usherr found no release of this on your indexers right now.")
	}
	op, err := s.ownOpenFirst(ctx, fresh)
	if err != nil {
		return nil, err
	}
	return &streamedPlay{target: op.url, homeURL: op.lanURL, release: op.release}, nil
}

// ustreamToken is the session token in a stream's address.
func ustreamToken(u string) string {
	_, tail, ok := strings.Cut(u, "/api/ustream/")
	if !ok {
		return ""
	}
	tok, _, _ := strings.Cut(tail, "/")
	return tok
}

// ustreamClose lets go of a stream's session (and the video it holds in
// memory) once nobody watches it.
func ustreamClose(token string) {
	if token == "" {
		return
	}
	ustreamMu.Lock()
	delete(ustreamSessions, token)
	ustreamMu.Unlock()
}
