package api

// Usherr: Watch history and stats (internal/watchlog). Usherr asks Plex,
// Jellyfin and Emby what is playing every half minute (every minute while
// nothing is) and keeps one row per playback: who, what, which app and
// device, how it played, how long it was actually watched and how far it got.
// The pages are Now playing, History and Stats; an administrator sees
// everyone, a member only their own unless "members see everyone's" is on.
// It does the job of Streamystats.

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/settings"
	"github.com/rdborg/mediarium/internal/watchlog"
)

const (
	watchEvery     = 30 * time.Second // while something plays
	watchIdleEvery = time.Minute      // while nothing does
	watchFresh     = 10 * time.Second // Now playing asks again when the last check is older
	watchKeepDays  = 365
)

type watchState struct {
	once    sync.Once
	repo    *watchlog.Repo
	tracker *watchlog.Tracker

	round   sync.Mutex // one check at a time
	mu      sync.Mutex
	checked time.Time
	who     map[string]watchWho // server kind + user id or name -> the account
	now     func() time.Time    // the clock (tests set it); nil is time.Now
}

func (s *Server) watchNow() time.Time {
	if s.watch.now != nil {
		return s.watch.now()
	}
	return time.Now()
}

type watchWho struct {
	id int64
	at time.Time
}

func (s *Server) watchLog() (*watchlog.Repo, *watchlog.Tracker) {
	s.watch.once.Do(func() {
		s.watch.repo = watchlog.NewRepo(s.db)
		s.watch.tracker = watchlog.NewTracker(s.watch.repo)
		s.watch.who = map[string]watchWho{}
	})
	return s.watch.repo, s.watch.tracker
}

func (s *Server) registerWatchRoutes(member, admin routeGroup) {
	member.HandleFunc("GET /api/watch/now", s.handleWatchNow)
	member.HandleFunc("GET /api/watch/history", s.handleWatchHistory)
	member.HandleFunc("GET /api/watch/stats", s.handleWatchStats)
	member.HandleFunc("GET /api/watch/title", s.handleWatchTitle)
	admin.HandleFunc("GET /api/watch/settings", s.handleGetWatchSettings)
	admin.HandleFunc("PUT /api/watch/settings", s.handlePutWatchSettings)
	admin.HandleFunc("DELETE /api/watch/addresses", s.handleClearWatchAddresses)
}

func (s *Server) watchRecordOn() bool { return s.settingOn(settings.KeyWatchRecord, true) }

func (s *Server) watchRetentionDays() int {
	v, _ := s.Settings.Get(settings.KeyWatchRetentionDays)
	if v == "" {
		return watchKeepDays
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return watchKeepDays
	}
	return n
}

// watchInterval is how long until the next check: sooner while something plays.
func (s *Server) watchInterval() time.Duration {
	if _, tr := s.watchLog(); tr.Active() {
		return watchEvery
	}
	return watchIdleEvery
}

// watchSessionsJob asks every media server what it plays and records it.
func (s *Server) watchSessionsJob(ctx context.Context) {
	_, tr := s.watchLog()
	s.watch.round.Lock()
	defer s.watch.round.Unlock()
	defer func() {
		s.watch.mu.Lock()
		s.watch.checked = time.Now()
		s.watch.mu.Unlock()
	}()
	keep := map[int64]bool{}
	if !s.watchRecordOn() || s.MediaServers == nil || s.mediaClient == nil {
		tr.Forget(ctx, keep)
		return
	}
	list, err := s.MediaServers.List()
	if err != nil {
		return
	}
	keepAddr := s.settingOn(settings.KeyWatchKeepAddresses, false)
	for _, m := range list {
		if !m.Enabled || (m.Kind != mediaservers.KindPlex && m.Kind != mediaservers.KindJellyfin && m.Kind != mediaservers.KindEmby) {
			continue
		}
		keep[m.ID] = true
		ask, cancel := context.WithTimeout(ctx, 10*time.Second)
		playing, err := s.mediaClient.NowPlaying(ask, m)
		cancel()
		if err != nil {
			slog.Debug("watch history: couldn't ask a media server", "server", m.Name, "err", err)
			continue // its playbacks stay open until it answers again
		}
		seen := make([]watchlog.Seen, 0, len(playing))
		for _, p := range playing {
			if p.Session.Audio {
				continue
			}
			seen = append(seen, s.watchSeen(m, p, keepAddr))
		}
		tr.Observe(ctx, watchlog.Server{ID: m.ID, Name: m.Name, Kind: string(m.Kind)}, seen, s.watchNow())
	}
	tr.Forget(ctx, keep)
}

// watchSeen turns what a server says into a playback, matched to the
// library and to an Usherr account where it can be.
func (s *Server) watchSeen(m mediaservers.Server, p mediaservers.Playing, keepAddr bool) watchlog.Seen {
	si := p.Session
	key := si.ID + "|" + si.ItemID
	if si.ID == "" && si.ItemID == "" {
		key = si.UserName + "|" + p.Device + "|" + p.Series + "|" + p.Title
	}
	v := watchlog.Seen{Key: key, ServerUserID: si.UserID, UserName: si.UserName, Kind: p.Kind, Title: p.Title, Series: p.Series,
		Season: p.Season, Episode: p.Episode, Year: p.Year, Client: si.Client, Device: p.Device, PlayMethod: p.PlayMethod,
		TranscodeReasons: si.TranscodeReasons, VideoIn: si.VideoIn, VideoOut: si.VideoOut, AudioIn: si.AudioIn, AudioOut: si.AudioOut,
		ResolutionIn: p.Resolution, ResolutionOut: si.ResolutionOut, BandwidthKbps: si.BandwidthKbps, Location: si.Location,
		PositionSec: si.PositionSec, RuntimeSec: si.RuntimeSec, Paused: p.Paused}
	if keepAddr {
		v.Address = trimRunes(si.Address, 64)
	}
	if v.PlayMethod != "Transcode" {
		v.TranscodeReasons = nil
	}
	switch p.Kind {
	case "movie":
		v.TMDBID = p.TMDBID
		if p.TMDBID > 0 && s.MovieRepo != nil {
			if mv, ok, err := s.MovieRepo.GetByTMDBID(p.TMDBID); err == nil && ok {
				v.LibraryID = mv.ID
			}
		}
	case "episode":
		v.TMDBID = p.SeriesTMDB
		if p.SeriesTMDB > 0 && s.MovieRepo != nil {
			if sr, ok, err := s.MovieRepo.GetSeriesByTMDBID(p.SeriesTMDB); err == nil && ok {
				v.LibraryID = sr.ID
			}
		}
	}
	v.UserID = s.watchAccount(m.Kind, si.UserID, si.UserName)
	return v
}

// watchAccount is the Usherr account of a media server's user: the one
// linked to it (signed in with Jellyfin or Plex), else the one with the same
// name. 0 when there is none. Remembered for ten minutes.
func (s *Server) watchAccount(kind mediaservers.Kind, serverUserID, name string) int64 {
	if s.Auth == nil || (serverUserID == "" && name == "") {
		return 0
	}
	key := string(kind) + "|" + serverUserID + "|" + strings.ToLower(name)
	s.watch.mu.Lock()
	w, ok := s.watch.who[key]
	s.watch.mu.Unlock()
	if ok && time.Since(w.at) < 10*time.Minute {
		return w.id
	}
	var id int64
	source := ""
	switch kind {
	case mediaservers.KindJellyfin:
		source = auth.SourceJellyfin
	case mediaservers.KindPlex:
		source = auth.SourcePlex
	}
	if source != "" && serverUserID != "" {
		if u, err := s.Auth.UserByIdentity(source, serverUserID); err == nil && !u.Disabled {
			id = u.ID
		}
	}
	if id == 0 && name != "" {
		if u, err := s.Auth.UserByName(name); err == nil && u != nil && !u.Disabled {
			id = u.ID
		}
	}
	s.watch.mu.Lock()
	if len(s.watch.who) > 500 {
		s.watch.who = map[string]watchWho{}
	}
	s.watch.who[key] = watchWho{id: id, at: time.Now()}
	s.watch.mu.Unlock()
	return id
}

// watchPruneJob removes the history older than the retention setting.
func (s *Server) watchPruneJob(ctx context.Context) {
	days := s.watchRetentionDays()
	if days <= 0 {
		return
	}
	repo, _ := s.watchLog()
	if n, err := repo.Prune(ctx, s.watchNow().AddDate(0, 0, -days)); err != nil {
		slog.Warn("watch history: couldn't remove old playbacks", "err", err)
	} else if n > 0 {
		slog.Info("watch history: removed old playbacks", "removed", n, "days", days)
	}
}

// watchScope is the account whose history a request may see: 0 for everyone's.
func (s *Server) watchScope(r *http.Request) (only int64, everyone bool) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return -1, false
	}
	if u.IsAdmin || s.settingOn(settings.KeyWatchMembersSeeAll, false) {
		return 0, true
	}
	return u.ID, false
}

// scrub leaves out what only an administrator sees.
func watchScrub(list []watchlog.Session, admin bool) []watchlog.Session {
	if admin {
		return list
	}
	for i := range list {
		list[i].Address = ""
	}
	return list
}

// GET /api/watch/now: what is playing right now.
func (s *Server) handleWatchNow(w http.ResponseWriter, r *http.Request) {
	only, everyone := s.watchScope(r)
	_, tr := s.watchLog()
	if s.watchRecordOn() {
		s.watch.mu.Lock()
		stale := time.Since(s.watch.checked) > watchFresh
		s.watch.mu.Unlock()
		if stale {
			s.watchSessionsJob(r.Context())
		}
	}
	list := []watchlog.Session{}
	for _, p := range tr.Playing() {
		if only == 0 || p.UserID == only {
			list = append(list, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"playing": watchScrub(list, isAdminRequest(r)), "everyone": everyone, "recording": s.watchRecordOn()})
}

// watchDay reads a "2026-11-04" date as the start of that day here.
func watchDay(v string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(v), time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}

// GET /api/watch/history: one page of playbacks, newest first.
func (s *Server) handleWatchHistory(w http.ResponseWriter, r *http.Request) {
	only, everyone := s.watchScope(r)
	if only < 0 {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	q := r.URL.Query()
	f := watchlog.Filter{OnlyUser: only, Query: trimRunes(q.Get("q"), 100), Device: trimRunes(q.Get("device"), 100), Method: q.Get("method")}
	if everyone {
		f.User = trimRunes(q.Get("user"), 100)
	}
	f.ServerID, _ = strconv.ParseInt(q.Get("server"), 10, 64)
	f.From = watchDay(q.Get("from"))
	if to := watchDay(q.Get("to")); !to.IsZero() {
		f.To = to.AddDate(0, 0, 1)
	}
	size, _ := strconv.Atoi(q.Get("size"))
	if size <= 0 || size > 100 {
		size = 25
	}
	page, _ := strconv.Atoi(q.Get("page"))
	page = max(page, 1)
	f.Limit, f.Offset = size, (page-1)*size
	repo, _ := s.watchLog()
	list, total, err := repo.List(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The watch history couldn't be read.")
		return
	}
	choices, _ := repo.Choices(r.Context(), only)
	writeJSON(w, http.StatusOK, map[string]any{"items": watchScrub(list, isAdminRequest(r)), "total": total, "page": page, "size": size, "choices": choices, "everyone": everyone})
}

// GET /api/watch/stats?days=30: the numbers for a period.
func (s *Server) handleWatchStats(w http.ResponseWriter, r *http.Request) {
	only, everyone := s.watchScope(r)
	if only < 0 {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	switch days {
	case 7, 30, 90, 365:
	default:
		days = 30
	}
	now := time.Now()
	first := now.AddDate(0, 0, -(days - 1))
	from := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.Local)
	f := watchlog.Filter{OnlyUser: only, From: from}
	if everyone {
		f.User = trimRunes(r.URL.Query().Get("user"), 100)
	}
	repo, _ := s.watchLog()
	list, err := repo.All(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The watch history couldn't be read.")
		return
	}
	st := watchlog.Sum(list, days, now, time.Local)
	if !everyone {
		st.Users = []watchlog.Count{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": st, "everyone": everyone})
}

// GET /api/watch/title?kind=movie&id=12: how often a library film or show
// was watched, and by whom.
func (s *Server) handleWatchTitle(w http.ResponseWriter, r *http.Request) {
	only, everyone := s.watchScope(r)
	if only < 0 {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	kind := r.URL.Query().Get("kind")
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if (kind != "movie" && kind != "tv") || err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Say which film or show.")
		return
	}
	repo, _ := s.watchLog()
	sum, err := repo.Title(r.Context(), kind, id, only)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The watch history couldn't be read.")
		return
	}
	if !everyone {
		sum.People = []string{}
	}
	writeJSON(w, http.StatusOK, sum)
}

type watchSettings struct {
	Record        bool `json:"record"`
	RetentionDays int  `json:"retentionDays"`
	MembersSeeAll bool `json:"membersSeeAll"`
	KeepAddresses bool `json:"keepAddresses"`
	Addresses     int  `json:"addresses"` // playbacks that still have an address
}

func (s *Server) watchSettings(ctx context.Context) watchSettings {
	repo, _ := s.watchLog()
	n, _ := repo.Addresses(ctx)
	return watchSettings{Record: s.watchRecordOn(), RetentionDays: s.watchRetentionDays(),
		MembersSeeAll: s.settingOn(settings.KeyWatchMembersSeeAll, false), KeepAddresses: s.settingOn(settings.KeyWatchKeepAddresses, false), Addresses: n}
}

func (s *Server) handleGetWatchSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.watchSettings(r.Context()))
}

func (s *Server) handlePutWatchSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Record        *bool `json:"record"`
		RetentionDays *int  `json:"retentionDays"`
		MembersSeeAll *bool `json:"membersSeeAll"`
		KeepAddresses *bool `json:"keepAddresses"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if body.RetentionDays != nil && (*body.RetentionDays < 0 || *body.RetentionDays > 36500) {
		writeError(w, http.StatusBadRequest, "Keep it for 0 (for ever) to 36500 days.")
		return
	}
	flag := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	set := map[string]string{}
	if body.Record != nil {
		set[settings.KeyWatchRecord] = flag(*body.Record)
	}
	if body.RetentionDays != nil {
		set[settings.KeyWatchRetentionDays] = strconv.Itoa(*body.RetentionDays)
	}
	if body.MembersSeeAll != nil {
		set[settings.KeyWatchMembersSeeAll] = flag(*body.MembersSeeAll)
	}
	if body.KeepAddresses != nil {
		set[settings.KeyWatchKeepAddresses] = flag(*body.KeepAddresses)
	}
	for k, v := range set {
		if err := s.Settings.Set(k, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
			return
		}
	}
	if body.RetentionDays != nil {
		s.watchPruneJob(r.Context())
	}
	writeJSON(w, http.StatusOK, s.watchSettings(r.Context()))
}

// DELETE /api/watch/addresses: forget every player's IP address.
func (s *Server) handleClearWatchAddresses(w http.ResponseWriter, r *http.Request) {
	repo, _ := s.watchLog()
	n, err := repo.ClearAddresses(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The addresses couldn't be cleared.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cleared": n})
}
