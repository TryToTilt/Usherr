package api

// Usherr: a game's download, from the line to the Sports folder. The same
// download, repair and unpack as everything else (prepareDownload), then the
// game's video is filed as
//
//	<Sports>/<League>/Season <Year>/<League> - S<Year>E<NN> - <Game>[ - <Part>] - <Quality>[ - ptN].<ext>
//
// with a tvshow.nfo for the league and an .nfo for the game, so Plex and
// Jellyfin show it as a TV library (the league a show, the season's year a
// season, the game an episode); then the media servers are told, as for an
// episode. Upstream code is touched only through one-line hooks (the
// "sports hook" comments).

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/sports"
)

// sportDownload is the game (and part) a download is for.
func (s *Server) sportDownload(queueID int64) (int64, string, bool) {
	var event int64
	var part string
	err := s.db.QueryRow(`SELECT event_id, part FROM sports_downloads WHERE queue_id = ?`, queueID).Scan(&event, &part)
	return event, part, err == nil
}

// prepareSportsPipeline is preparePipeline's case for a game's download.
// ok is false when the download isn't one.
func (s *Server) prepareSportsPipeline(item queue.Item, protocol indexers.Protocol) (run func(), ok bool, err error) {
	var event int64
	var part string
	found := false
	// The link is written right after the download is queued; give it a
	// moment in case the line picked the item up in between.
	for i := 0; i < 20 && !found; i++ {
		if event, part, found = s.sportDownload(item.ID); !found {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !found {
		return nil, false, nil
	}
	g, err := s.sportGame(context.Background(), event)
	if err != nil {
		return nil, true, errors.New("That game isn't in Sports any more.")
	}
	return func() {
		if err := s.runSportsPipeline(item.ID, g, part, item.ReleaseTitle, item.NZBURL, protocol); err != nil {
			s.stopFailedTorrent(item.ID)
			slog.Info("queue: game download ended", "queueId", item.ID, "err", err)
		}
	}, true, nil
}

// describeSportItem names a game's download in the queue and on the
// Dashboard.
func (s *Server) describeSportItem(queueID int64) (title, subtitle, poster string, ok bool) {
	event, part, found := s.sportDownload(queueID)
	if !found {
		return "", "", "", false
	}
	g, err := s.sportGame(context.Background(), event)
	if err != nil {
		return "", "", "", false
	}
	sub := "Sports · " + g.ev.League.Name
	if part != "" {
		sub += " · " + part
	}
	img := g.image
	if img == "" {
		img = g.leagueBadge
	}
	if img != "" {
		img = "/api/home/sports/image?u=" + url.QueryEscape(img)
	}
	return g.ev.Title, sub, img, true
}

// retrySportDownload retries a failed or stopped game download: the same
// release, queued again for the same game. It reports false when item isn't
// a game's download.
func (s *Server) retrySportDownload(w http.ResponseWriter, item queue.Item) bool {
	event, part, found := s.sportDownload(item.ID)
	if !found {
		return false
	}
	g, err := s.sportGame(context.Background(), event)
	if err != nil {
		writeError(w, http.StatusNotFound, "That game isn't in Sports any more.")
		return true
	}
	keptDir, reuse := s.keptDownload(item)
	release := s.dispatch.Hold()
	newID, err := s.grabSportGame(g, part, item.ReleaseTitle, item.NZBURL, item.SizeBytes, indexers.Protocol(item.Protocol), queue.PriorityManual)
	if err != nil {
		release()
		writeError(w, http.StatusConflict, err.Error())
		return true
	}
	if reuse {
		s.moveKeptDownload(keptDir, newID)
	}
	release()
	_ = s.QueueRepo.Delete(item.ID)
	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": newID})
	return true
}

// sportRetryBudget counts the automatic searches again after a bad release,
// per game and part, over the last retryWindow (as for books).
type sportRetryBudget struct {
	mu sync.Mutex
	at map[string][]time.Time
}

var sportRetries sync.Map // *Server -> *sportRetryBudget

func (s *Server) sportRetryAllowed(event int64, part string, now time.Time) (int, bool) {
	v, _ := sportRetries.LoadOrStore(s, &sportRetryBudget{at: map[string][]time.Time{}})
	b := v.(*sportRetryBudget)
	key := fmt.Sprintf("%d|%s", event, part)
	b.mu.Lock()
	defer b.mu.Unlock()
	recent := b.at[key][:0]
	for _, t := range b.at[key] {
		if now.Sub(t) < retryWindow {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	b.at[key] = recent
	return len(recent), len(recent) <= maxAutoRetries
}

// sportEpisode is a game's place in its league's season (1 for the first).
func (s *Server) sportEpisode(ctx context.Context, g sportGame) int {
	at := int64(0)
	if !g.ev.At.IsZero() {
		at = g.ev.At.UnixMilli()
	}
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sports_events WHERE league_id = ? AND season = ? AND (at_ms < ? OR (at_ms = ? AND id <= ?))`,
		g.leagueID, g.season, at, at, g.id).Scan(&n)
	return max(n, 1)
}

// sportDest is where a game's video goes.
func (s *Server) sportDest(ctx context.Context, g sportGame, part, releaseTitle, video string) (string, error) {
	root := s.sportsRoot()
	if root == "" {
		return "", errors.New("the Sports folder isn't set. Choose one in Settings > Extras > Sports")
	}
	mode, repl := s.illegalCharSettings()
	season := sports.SeasonNumber(g.season, g.ev.At)
	league, seasonDir := sports.Folder(g.ev.League.Name, season)
	name := sports.FileName(sports.FileInfo{League: g.ev.League.Name, Season: season, Episode: s.sportEpisode(ctx, g), Title: g.ev.Title, Part: part,
		Quality: sports.QualityLabel(sports.Parse(releaseTitle)), Ext: filepath.Ext(video)})
	return filepath.Join(root, organizer.Sanitize(league, mode, repl), organizer.Sanitize(seasonDir, mode, repl), organizer.Sanitize(name, mode, repl)), nil
}

func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// writeSportNFOs writes the league's tvshow.nfo (once) and the game's .nfo
// next to its file, so Jellyfin (and Plex with an NFO agent) show the
// league, the game's name, day and details. A problem never fails the
// import.
func (s *Server) writeSportNFOs(ctx context.Context, g sportGame, part, file string) {
	leagueDir := filepath.Dir(filepath.Dir(file))
	show := filepath.Join(leagueDir, "tvshow.nfo")
	if _, err := os.Stat(show); errors.Is(err, os.ErrNotExist) {
		var desc string
		_ = s.db.QueryRowContext(ctx, `SELECT description FROM sports_leagues WHERE id = ?`, g.leagueID).Scan(&desc)
		body := "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n<tvshow>\n  <title>" + xmlText(g.ev.League.Name) + "</title>\n" +
			"  <plot>" + xmlText(desc) + "</plot>\n  <genre>Sports</genre>\n  <genre>" + xmlText(g.ev.League.Sport) + "</genre>\n</tvshow>\n"
		if err := os.WriteFile(show, []byte(body), 0o644); err != nil {
			slog.Info("sports: write tvshow.nfo", "path", show, "err", err)
		}
	}
	title := g.ev.Title
	if part != "" {
		title += " (" + part + ")"
	}
	var plot []string
	if g.ev.Home != "" && g.ev.Away != "" {
		plot = append(plot, g.ev.Home+" v "+g.ev.Away)
	}
	if g.venue != "" {
		plot = append(plot, g.venue)
	}
	if n, _ := strconv.Atoi(g.ev.Round); n > 0 && n < 100 {
		plot = append(plot, "Round "+g.ev.Round)
	}
	aired := ""
	if !g.ev.At.IsZero() {
		aired = g.ev.LocalDay().Format("2006-01-02")
	}
	body := "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n<episodedetails>\n  <title>" + xmlText(title) + "</title>\n" +
		"  <showtitle>" + xmlText(g.ev.League.Name) + "</showtitle>\n" +
		fmt.Sprintf("  <season>%d</season>\n  <episode>%d</episode>\n", sports.SeasonNumber(g.season, g.ev.At), s.sportEpisode(ctx, g)) +
		"  <aired>" + aired + "</aired>\n  <plot>" + xmlText(strings.Join(plot, " · ")) + "</plot>\n" +
		"  <uniqueid type=\"thesportsdb\">" + xmlText(g.externalID) + "</uniqueid>\n</episodedetails>\n"
	nfo := strings.TrimSuffix(file, filepath.Ext(file)) + ".nfo"
	if err := os.WriteFile(nfo, []byte(body), 0o644); err != nil {
		slog.Info("sports: write the game's nfo", "path", nfo, "err", err)
	}
}

func (s *Server) runSportsPipeline(queueID int64, g sportGame, part, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}
	label := g.label()
	if part != "" {
		label += " (" + part + ")"
	}
	fail := func(stepErr error) error {
		if run.cancelled.Load() {
			_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, errRemovedFromLibrary.Error())
			return errRemovedFromLibrary
		}
		if handled, err := s.settleInterrupted(queueID, run); handled {
			return err
		}
		if dup := asDuplicate(stepErr); dup != nil {
			s.settleDuplicate(queueID, dup, nil)
			return stepErr
		}
		plain := plainerror.Message(stepErr)
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, plain)
		noteDownloadFailure(queueID, label, "/sports/league/"+strconv.FormatInt(g.leagueID, 10), stepErr)
		_ = s.QueueRepo.LogActivity(0, "failed", fmt.Sprintf("%s: %s", label, plain))
		if s.failureNotifyWanted(stepErr, downloadURL, releaseTitle) {
			s.notifyItem("failed", notify.Item{Media: "episode", Title: g.ev.League.Name, Episode: g.ev.Title, Reason: plain, Release: releaseTitle})
		}
		if isBadRelease(stepErr) {
			if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: stepErr.Error()}); err == nil {
				if n, ok := s.sportRetryAllowed(g.id, part, time.Now()); ok {
					s.background(func() {
						sctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
						defer cancel()
						s.searchSportGame(sctx, g.id, queue.PriorityAutomatic)
					})
				} else {
					_ = s.QueueRepo.LogActivity(0, "failed", fmt.Sprintf("%s: %s", label, retryPausedMessage(n)))
				}
			}
		}
		return stepErr
	}

	if err := s.QueueRepo.SetStatus(queueID, queue.StatusDownloading, ""); err != nil {
		return fail(err)
	}
	dir, err := s.prepareDownload(ctx, queueID, downloadURL, protocol)
	if err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	video, err := organizer.FindLargestVideoFile(dir)
	if err != nil {
		return fail(badRelease(errors.New("the download has no video file")))
	}
	dest, err := s.sportDest(ctx, g, part, releaseTitle, video)
	if err != nil {
		return fail(err)
	}
	tier := sports.Parse(releaseTitle).Tier
	oldPath, oldTier := s.sportPartFile(ctx, g.id, part)
	better := func() bool {
		ps, err := s.loadProfiles()
		if err != nil {
			return true
		}
		p := s.sportProfile(ctx, ps, g)
		return p.TierRank(tier) >= p.TierRank(quality.Tier(oldTier))
	}
	res, err := s.importDownloaded(video, dest, organizer.ConflictOverwriteIfBetter, better)
	if err != nil {
		return fail(fmt.Errorf("couldn't move the game into your library: %w", err))
	}
	if res.Skipped {
		return fail(fmt.Errorf("a file of the same or better quality is already at %s", dest))
	}
	info, _ := os.Stat(res.DestPath)
	var size int64
	if info != nil {
		size = info.Size()
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sports_files (event_id, part, path, quality, size, release, imported_ms) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(event_id, part) DO UPDATE SET path = excluded.path, quality = excluded.quality, size = excluded.size, release = excluded.release, imported_ms = excluded.imported_ms`,
		g.id, part, res.DestPath, string(tier), size, releaseTitle, time.Now().UnixMilli()); err != nil {
		return fail(err)
	}
	if oldPath != "" && filepath.Clean(oldPath) != filepath.Clean(res.DestPath) {
		// A better copy under another name (its quality is in the name).
		s.removeReplacedFile(s.sportsRoot(), oldPath, res.DestPath, 0, 0, label)
	}
	s.writeSportNFOs(ctx, g, part, res.DestPath)
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(dir, protocol)
	s.mediaServersImported(mediaservers.MediaTV, res.DestPath)
	_ = s.QueueRepo.LogActivity(0, "imported", fmt.Sprintf("%s (%s) imported to %s", label, tier, res.DestPath))
	s.notifyItem("imported", notify.Item{Media: "episode", Title: g.ev.League.Name, Episode: g.ev.Title, Quality: string(tier), SizeBytes: size, Path: res.DestPath, Release: releaseTitle,
		LinkPath: "/sports/league/" + strconv.FormatInt(g.leagueID, 10)})
	slog.Info("sports: imported", "game", g.id, "part", part, "quality", string(tier))
	return nil
}

// ---- bringing the followed leagues over from Sportarr ----

// POST /api/home/sports/import-sportarr: the leagues in the household's
// Sportarr, and the teams it follows, put in Usherr's own engine. Only
// which leagues and teams, and whether a league gets its new games: never
// Sportarr's profiles or files. Sportarr is only read.
func (s *Server) handleSportsImportSportarr(w http.ResponseWriter, r *http.Request) {
	if !s.ownSportsOnly(w) {
		return
	}
	base, _ := s.Settings.Get(keySportsSportarrURL)
	key, _ := s.Settings.Get(keySportsSportarrKey)
	if strings.TrimSpace(base) == "" || strings.TrimSpace(key) == "" {
		writeError(w, http.StatusPreconditionFailed, "Enter Sportarr's address and API key first, to read its leagues.")
		return
	}
	sa := &sportarrBackend{base: strings.TrimRight(strings.TrimSpace(base), "/"), key: strings.TrimSpace(key)}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	leagues, err := sa.leagues(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Sportarr: "+plainerror.Message(err))
		return
	}
	teams, _ := sa.followedTeams(ctx)
	own := &ownSports{s: s}
	out := struct {
		Leagues int      `json:"leagues"`
		Teams   int      `json:"teams"`
		Already int      `json:"already"`
		Failed  []string `json:"failed"`
		Message string   `json:"message"`
	}{Failed: []string{}}
	for _, l := range leagues {
		if strings.TrimSpace(l.ExternalID) == "" {
			out.Failed = append(out.Failed, l.Name)
			continue
		}
		var have int64
		err := s.db.QueryRowContext(ctx, `SELECT id FROM sports_leagues WHERE external_id = ?`, l.ExternalID).Scan(&have)
		if err == nil {
			out.Already++
			if l.Monitored {
				_, _ = s.db.ExecContext(ctx, `UPDATE sports_leagues SET monitored = 1 WHERE id = ?`, have)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			out.Failed = append(out.Failed, l.Name)
			continue
		}
		l.ID = ""
		if err := own.addLeague(ctx, l, l.Monitored); err != nil {
			out.Failed = append(out.Failed, l.Name)
			continue
		}
		out.Leagues++
	}
	for _, t := range teams {
		if strings.TrimSpace(t.ExternalID) == "" {
			continue
		}
		res, err := s.db.ExecContext(ctx, `INSERT INTO sports_teams (external_id, name, sport, country, badge, added_ms) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(external_id) DO NOTHING`, t.ExternalID, t.Name, t.Sport, t.Country, t.Badge, time.Now().UnixMilli())
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				out.Teams++
			}
		}
	}
	out.Message = fmt.Sprintf("Brought over %d leagues and %d teams from Sportarr.", out.Leagues, out.Teams)
	if out.Already > 0 {
		out.Message += fmt.Sprintf(" %d were already in Sports.", out.Already)
	}
	if len(out.Failed) > 0 {
		out.Message += " Not brought over: " + strings.Join(out.Failed, ", ") + "."
	}
	writeJSON(w, http.StatusOK, out)
}
