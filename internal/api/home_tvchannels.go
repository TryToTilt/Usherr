package api

// Home fork: Mediarium TV channels. Channels built from the library play on
// a schedule; tuning in plays what is on from the right second, and the
// player moves on to the next program by itself. See internal/tv.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/tv"
)

var (
	tvBuildMu         sync.Mutex  // one schedule build at a time (ffprobe reads files)
	liveRefreshQueued atomic.Bool // a live guide refresh is on its way
)

func (s *Server) tvRepo() *tv.Repo      { return tv.NewRepo(s.db) }
func (s *Server) tvLibrary() tv.Library { return tvLibrary{DBLibrary: tv.DBLibrary{DB: s.db}, s: s} }
func (s *Server) tvProbe() tv.Prober    { return tvProber(s.tvRepo()) }

// registerTVChannelRoutes adds Mediarium TV: watching for people who may
// play, channel editing for admins.
func (s *Server) registerTVChannelRoutes(play, admin routeGroup) {
	play.HandleFunc("GET /api/tv/channels", revalidated(s.handleTVChannels))
	play.HandleFunc("GET /api/tv/guide", revalidated(s.handleTVGuide))
	play.HandleFunc("GET /api/tv/channels/{id}/now", s.handleTVNow)
	play.HandleFunc("GET /api/tv/channels/{id}/info", revalidated(s.handleTVInfo))
	play.HandleFunc("GET /api/tv/channels/{id}/logo", s.handleTVLogo)
	play.HandleFunc("GET /api/tv/channels/{id}/filler/{start}", s.handleTVFiller) // Usherr
	play.HandleFunc("GET /api/stream/{id}/browser", s.handleStreamBrowser)
	admin.HandleFunc("POST /api/tv/channels", s.handleTVSave)
	admin.HandleFunc("POST /api/tv/ai-build", s.handleTVAIBuild)  // Usherr: the AI channel builder
	play.HandleFunc("POST /api/tv/remind", s.handleTVRemind)      // Usherr: reminders
	play.HandleFunc("GET /api/tv/reminders", s.handleTVReminders) // Usherr
	admin.HandleFunc("PUT /api/tv/channels/{id}", s.handleTVSave)
	admin.HandleFunc("DELETE /api/tv/channels/{id}", s.handleTVDelete)
	admin.HandleFunc("POST /api/tv/channels/{id}/rebuild", s.handleTVRebuild)
	admin.HandleFunc("POST /api/tv/channels/{id}/logo", s.handleTVLogoUpload)
	admin.HandleFunc("DELETE /api/tv/channels/{id}/logo", s.handleTVLogoDelete)
	admin.HandleFunc("GET /api/tv/channels/{id}/logo-ideas", s.handleTVLogoIdeas)
	admin.HandleFunc("GET /api/tv/tuner", s.handleTVTunerInfo)
	admin.HandleFunc("POST /api/tv/jellyfin", s.handleTVJellyfin)
}

// tvJob keeps every channel scheduled tv.Horizon ahead and forgets old
// programs. Measuring new files happens here, in the background.
func (s *Server) tvJob(ctx context.Context) {
	if !tvBuildMu.TryLock() {
		return
	}
	defer tvBuildMu.Unlock()
	repo := s.tvRepo()
	chans, err := repo.Channels()
	if err != nil {
		return
	}
	now := time.Now()
	for _, ch := range chans {
		if ctx.Err() != nil {
			return
		}
		if n, err := tv.Extend(repo, s.tvLibrary(), s.tvProbe(), ch, now); err != nil {
			slog.Info("tv: schedule", "channel", ch.Name, "added", n, "err", err)
		}
	}
	_ = repo.Trim(now.Add(-48 * time.Hour))
	s.refreshLiveGuides(ctx, false)
}

// rebuildTVChannel lays a channel's schedule out again after what is on now
// (its rules changed), in the background.
func (s *Server) rebuildTVChannel(id int64) {
	if ch, err := s.tvRepo().Channel(id); err == nil && ch.Kind == "live" {
		// Several live channels saved in a row share one guide download.
		if liveRefreshQueued.CompareAndSwap(false, true) {
			s.background(func() {
				time.Sleep(3 * time.Second)
				liveRefreshQueued.Store(false)
				s.refreshLiveGuides(context.Background(), true)
			})
		}
		return
	}
	s.background(func() {
		tvBuildMu.Lock()
		defer tvBuildMu.Unlock()
		repo := s.tvRepo()
		ch, err := repo.Channel(id)
		if err != nil {
			return
		}
		now := time.Now()
		if p, ok, _ := repo.At(id, now); ok {
			_ = repo.ClearFrom(id, p.End) // what is on air keeps playing
		} else {
			_ = repo.ClearFrom(id, now)
		}
		if n, err := tv.Extend(repo, s.tvLibrary(), s.tvProbe(), ch, now); err != nil {
			slog.Info("tv: rebuild", "channel", ch.Name, "added", n, "err", err)
		}
	})
}

type tvProgramPayload struct {
	tv.Program
	Progress float64 `json:"progress"` // 0..1, for the one on air
}

type tvChannelPayload struct {
	tv.Channel
	Now  *tvProgramPayload `json:"now,omitempty"`
	Next *tv.Program       `json:"next,omitempty"`
}

// GET /api/tv/channels: every channel with what's on now and next.
func (s *Server) handleTVChannels(w http.ResponseWriter, r *http.Request) {
	repo := s.tvRepo()
	chans, err := repo.Channels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	out := make([]tvChannelPayload, 0, len(chans))
	for _, ch := range s.appChannels(chans) {
		p := tvChannelPayload{Channel: ch}
		if list, err := repo.Programs(ch.ID, now, now.Add(6*time.Hour)); err == nil && len(shows(list)) > 0 {
			list = shows(list) // Usherr: filler and cards aren't shows
			cur := list[0]
			if !cur.Start.After(now) {
				p.Now = &tvProgramPayload{Program: cur, Progress: float64(now.Sub(cur.Start)) / float64(cur.End.Sub(cur.Start))}
				if len(list) > 1 {
					p.Next = &list[1]
				}
			} else {
				p.Next = &cur
			}
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/tv/guide?hours=6: each channel's programs from the last half hour on.
func (s *Server) handleTVGuide(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 72 {
		hours = 6
	}
	repo := s.tvRepo()
	chans, err := repo.Channels()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	// From half an hour back, so what just finished shows too (Usherr: or
	// ?back= hours, up to 24, for catch-up).
	from := now.Truncate(30 * time.Minute).Add(-30 * time.Minute)
	if back, _ := strconv.Atoi(r.URL.Query().Get("back")); back > 0 && back <= 24 {
		from = now.Truncate(30 * time.Minute).Add(-time.Duration(back) * time.Hour)
	}
	type row struct {
		Channel  tv.Channel     `json:"channel"`
		Programs []guideProgram `json:"programs"`
	}
	badges := s.newGuideBadges(r.Context())
	out := make([]row, 0, len(chans))
	for _, ch := range s.appChannels(chans) {
		progs := []guideProgram{}
		if ch.Enabled { // a switched-off channel keeps its row (admins see it) but no guide
			list, _ := repo.Programs(ch.ID, from, from.Add(time.Duration(hours)*time.Hour))
			for _, p := range shows(list) { // Usherr: filler and cards aren't in the guide
				progs = append(progs, badges.of(p))
			}
		}
		out = append(out, row{Channel: ch, Programs: progs})
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "hours": hours, "now": now, "channels": out})
}

// GET /api/tv/channels/{id}/now: what to play to tune in, and from where.
func (s *Server) handleTVNow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a channel.")
		return
	}
	repo := s.tvRepo()
	ch, err := repo.Channel(id)
	if errors.Is(err, tv.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	ch.LogoURL = s.appLogo(ch)
	now := time.Now()
	p, ok, err := repo.At(id, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ch.Kind == "live" {
		// A live channel plays its stream, through the relay.
		out := map[string]any{"channel": ch, "live": s.liveStreamPath(ch.Number)}
		if ok {
			out["program"] = p
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "Nothing is on "+ch.Name+" yet: its schedule is still being made. Try again in a minute.")
		return
	}
	out := map[string]any{"channel": ch, "program": p, "offset": (now.Sub(p.Start) + p.Offset).Seconds(), "remaining": p.End.Sub(now).Seconds()}
	// Usherr: part of a show split by breaks: the player stops at the part's end.
	if s.isSplitPart(id, p) {
		out["cut"] = true
	}
	// Usherr: between shows, a card says what's next; filler plays from the channel's folder.
	if p.Kind == "card" || p.Kind == "filler" {
		if next, ok := s.upNext(id, p.End); ok {
			out["upNext"] = next
		}
		if p.Kind == "filler" {
			out["filler"] = fmt.Sprintf("/api/tv/channels/%d/filler/%d", id, p.Start.UnixMilli())
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if p.Kind == "stream" {
		// A streamed episode plays through a Stream now session of the
		// viewer's, like any stream.
		src, err := s.tvStreamSource(r.Context(), p.FilePath)
		if err != nil {
			out["offAir"] = true
			writeJSON(w, http.StatusOK, out)
			return
		}
		ss := &streamSession{id: randomID("str_"), userID: requestUserID(r), last: time.Now(), title: p.Title,
			target: src.url, lanURL: src.homeURL, streamFor: "title", release: src.release}
		if c, err := s.streamNZB(); err == nil {
			streams.sweep(c)
		}
		streams.mu.Lock()
		streams.sessions[ss.id] = ss
		streams.mu.Unlock()
		st := map[string]any{
			"id": ss.id, "videoUrl": "/api/stream/" + ss.id + "/video", "title": ss.title, "release": ss.release,
			"codec": parser.Parse(ss.release).Codec, "hdr": hdrOf(ss.release), "audio": audioOf(ss.release),
		}
		if !src.browserOK {
			// Browsers can't play it as it is: a converted copy, already
			// started at the right moment.
			st["videoUrl"] = fmt.Sprintf("/api/stream/%s/browser?at=%.1f", ss.id, now.Sub(p.Start).Seconds())
			st["converted"] = true
		}
		out["stream"] = st
	}
	// Whoever is watching gets the next program ready if it's streamed.
	s.tvPrewarmBefore(context.Background(), id, p)
	writeJSON(w, http.StatusOK, out)
}

type tvSaveRequest struct {
	Number  int      `json:"number"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // library (default) or live, for a new channel
	LogoURL string   `json:"logoUrl"`
	Rules   tv.Rules `json:"rules"`
	Enabled *bool    `json:"enabled"`
}

// POST /api/tv/channels or PUT /api/tv/channels/{id}
func (s *Server) handleTVSave(w http.ResponseWriter, r *http.Request) {
	var req tvSaveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that channel.")
		return
	}
	repo := s.tvRepo()
	ch := tv.Channel{Kind: "library", Enabled: true}
	if req.Kind == "live" {
		ch.Kind = "live"
	}
	if v := r.PathValue("id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "That isn't a channel.")
			return
		}
		if ch, err = repo.Channel(id); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
	}
	ch.Name, ch.LogoURL, ch.Rules = req.Name, storedLogo(req.LogoURL, ch.LogoURL), req.Rules
	if req.Number > 0 {
		ch.Number = req.Number
	}
	if req.Enabled != nil {
		ch.Enabled = *req.Enabled
	}
	if ch.Kind == "live" {
		u, err := url.Parse(strings.TrimSpace(ch.Rules.StreamURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeError(w, http.StatusBadRequest, "Give the live channel its stream address (an http or https link to an .m3u8 playlist).")
			return
		}
		ch.Rules.StreamURL = u.String()
	} else if len(ch.Rules.Series) == 0 && len(ch.Rules.Movies) == 0 && len(ch.Rules.Genres) == 0 && len(ch.Rules.Decades) == 0 && len(ch.Rules.Stream) == 0 {
		writeError(w, http.StatusBadRequest, "Pick at least one show, movie, genre or decade.")
		return
	}
	saved, err := repo.Save(ch)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.rebuildTVChannel(saved.ID)
	saved.LogoURL = s.appLogo(saved)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleTVDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a channel.")
		return
	}
	s.removeTVLogo(id)
	if err := s.tvRepo().Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nil)
}

func (s *Server) handleTVRebuild(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a channel.")
		return
	}
	s.rebuildTVChannel(id)
	writeJSON(w, http.StatusAccepted, nil)
}
