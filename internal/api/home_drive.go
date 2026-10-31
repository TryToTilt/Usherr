package api

// Usherr: the virtual drive, for Plex. Plex can't play the streamed
// library's .strm pointer files, so the same titles are also served as a
// read-only WebDAV drive of ordinary video files (home_drive_dav.go). The
// household mounts it with rclone (and WinFsp on Windows), which they install
// themselves, and adds the folder to Plex.
//
// A file is on the drive once its release is kept: its NZB saved and the
// video's size read, because a file on a drive needs its size before anyone
// plays it. A release is kept for free whenever someone plays the title
// (the engine found one that works), and the drive's own search gets the
// rest ready one title at a time, within a daily budget of searches, so the
// indexers' daily limits are never spent in a burst.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

const (
	keyDriveOn     = "home.drive.enabled"  // "1" on, "0" off; unset: on when Plex is the media server
	keyDrivePass   = "home.drive.password" // the drive's own password (kept encrypted)
	keyDriveBudget = "home.drive.budget"   // searches a day for titles nobody has played yet
	keyDriveUsed   = "home.drive.used"     // today's searches: "2026-10-31:7"
	keyDriveWhere  = "home.drive.where"    // where the media server sees the drive: "U:", "/mnt/usherr"
	driveUser      = "usherr"

	driveBudgetDefault = 30
	driveBudgetMax     = 500
	// driveEvery is how often the drive's search gets one more title ready.
	driveEvery = 10 * time.Minute
	// driveRetryAfter: a title with nothing on the indexers is tried again after this.
	driveRetryAfter = 7 * 24 * time.Hour
	// driveQuality is what the drive's search looks for: what most screens
	// play directly, and a sensible size to read from Usenet.
	driveQuality = "1080p"
	// driveWhereDefault is the drive letter the card's commands mount it as.
	driveWhereDefault = "U:"
	// driveScanAfter: a media server is asked to look at a new title once
	// rclone's folder list has caught up (it keeps one for a minute).
	driveScanAfter = 75 * time.Second
)

// driveOn is whether the virtual drive is served. Until it's set, it's on
// when Plex is the media server and no Jellyfin or Emby is (Jellyfin plays
// the pointer files).
func (s *Server) driveOn() bool {
	switch v, _ := s.Settings.Get(keyDriveOn); v {
	case "1":
		return true
	case "0":
		return false
	}
	return s.plexOnly()
}

// plexOnly is whether Plex is a media server here and Jellyfin and Emby aren't.
func (s *Server) plexOnly() bool {
	if s.MediaServers == nil {
		return false
	}
	list, err := s.MediaServers.List()
	if err != nil {
		return false
	}
	plex := false
	for _, m := range list {
		switch m.Kind {
		case mediaservers.KindPlex:
			plex = true
		case mediaservers.KindJellyfin, mediaservers.KindEmby:
			return false
		}
	}
	return plex
}

// drivePassword is the drive's own password, made the first time it's asked
// for. It's never a person's password: it only opens the drive, read only.
func (s *Server) drivePassword() string {
	if v, _ := s.Settings.Get(keyDrivePass); strings.TrimSpace(v) != "" {
		return v
	}
	return s.newDrivePassword()
}

func (s *Server) newDrivePassword() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	p := base64.RawURLEncoding.EncodeToString(b)
	_ = s.Settings.Set(keyDrivePass, p, true)
	return p
}

func (s *Server) driveBudget() int {
	v, _ := s.Settings.Get(keyDriveBudget)
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return min(n, driveBudgetMax)
	}
	return driveBudgetDefault
}

var driveSpendMu sync.Mutex

// driveUsedToday is how many searches the drive made today.
func (s *Server) driveUsedToday() int {
	v, _ := s.Settings.Get(keyDriveUsed)
	day, n, ok := strings.Cut(v, ":")
	if !ok || day != time.Now().Format("2006-01-02") {
		return 0
	}
	c, _ := strconv.Atoi(n)
	return c
}

// driveSpend takes one search from today's budget, or says there's none left.
func (s *Server) driveSpend() bool {
	driveSpendMu.Lock()
	defer driveSpendMu.Unlock()
	used := s.driveUsedToday()
	if used >= s.driveBudget() {
		return false
	}
	_ = s.Settings.Set(keyDriveUsed, fmt.Sprintf("%s:%d", time.Now().Format("2006-01-02"), used+1), false)
	return true
}

// ---- keeping a release ----

// driveRememberPlay keeps the release a streamed file just played with, when
// Usherr's own engine opened it and the file has no working release kept yet.
func (s *Server) driveRememberPlay(fileID int64, target string) {
	tok := ustreamToken(target)
	if tok == "" {
		return
	}
	ustreamMu.Lock()
	sess := ustreamSessions[tok]
	ustreamMu.Unlock()
	if sess == nil || len(sess.nzb) == 0 || sess.file == nil {
		return
	}
	var nzb, health string
	if err := s.db.QueryRow(`SELECT nzb, health FROM streamed_files WHERE id = ?`, fileID).Scan(&nzb, &health); err != nil {
		return
	}
	if nzb != "" && health != "dead" {
		return // keep the one the drive already shows: a new size would make Plex look again
	}
	go func() {
		if err := s.driveKeep(fileID, sess.release, sess.nzb, sess.file); err != nil {
			slog.Info("drive: couldn't keep a played release", "file", fileID, "err", err)
		}
	}()
}

// driveKeep saves a release's NZB for a streamed file and records the video
// in it, which puts the file on the drive.
func (s *Server) driveKeep(fileID int64, release string, data []byte, f *download.StreamFile) error {
	dir := s.streamedNZBDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("drive-%d.nzb", fileID)
	tmp := filepath.Join(dir, name+".part")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE streamed_files SET nzb = ?, release = ?, inner_file = ?, size = ?, health = 'ok', health_at = ? WHERE id = ?`,
		name, release, f.Name, f.Size, time.Now().UTC().Format(time.RFC3339), fileID)
	driveForget(fileID)
	driveTreeChanged()
	if err == nil {
		s.driveTellServers(fileID)
	}
	return err
}

// driveWhere is where the media server's computer has the drive mounted.
func (s *Server) driveWhere() string {
	if v, _ := s.Settings.Get(keyDriveWhere); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return driveWhereDefault
}

// driveServerFolder is a title's folder on the drive as the media server's
// computer sees it: rel is the drive's own path ("/Movies/Title (2025)
// [tmdbid-1]/Title (2025).mkv"), where is the mount ("U:" or "/mnt/usherr").
func driveServerFolder(where, rel string) string {
	parts := strings.Split(strings.TrimPrefix(rel, "/"), "/")
	if len(parts) > 2 {
		parts = parts[:2] // Movies or TV, then the title's own folder
	}
	if (len(where) >= 2 && where[1] == ':') || strings.HasPrefix(where, `\\`) {
		return strings.TrimRight(where, `\/`) + `\` + strings.Join(parts, `\`)
	}
	return strings.TrimRight(where, "/") + "/" + strings.Join(parts, "/")
}

// driveTellServers asks the media servers to look at a title that just came
// onto the drive, so Plex has it within minutes instead of at its next
// scan. Only a server with a library on the drive is asked, and only for
// that title's folder.
func (s *Server) driveTellServers(fileID int64) {
	if s.MediaServers == nil || s.mediaClient == nil {
		return
	}
	var (
		kind, title, inner          string
		anime                       bool
		year, tmdb, season, episode int
	)
	err := s.db.QueryRow(`SELECT t.kind, t.anime, t.title, t.year, t.tmdb_id, f.season, f.episode, f.inner_file
		FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id WHERE f.id = ?`, fileID).
		Scan(&kind, &anime, &title, &year, &tmdb, &season, &episode, &inner)
	if err != nil {
		return
	}
	k, media := "movie", mediaservers.MediaMovie
	if kind == "series" {
		k, media = "series", mediaservers.MediaTV
	}
	folder := driveServerFolder(s.driveWhere(), driveFileName(k, anime, title, year, tmdb, season, episode, inner))
	time.AfterFunc(driveScanAfter, func() {
		servers, err := s.MediaServers.List()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, ms := range servers {
			if !ms.Enabled || !ms.RefreshAfterImport {
				continue
			}
			done, err := s.mediaClient.RefreshServerFolders(ctx, ms, media, []string{folder})
			if err != nil {
				slog.Warn("virtual drive: media server scan failed", "server", ms.Name, "folder", folder, "err", err)
			} else if len(done) > 0 {
				slog.Info("virtual drive: media server scan started", "server", ms.Name, "folder", folder)
			}
		}
	})
}

// ---- the drive's own search ----

// errDriveNothingToDo: every streamed file has a release kept (or was tried lately).
var errDriveNothingToDo = errors.New("every streamed title is on the drive, or was looked for this week")

// errDriveNoBudget: today's searches are spent.
var errDriveNoBudget = errors.New("today's searches for the drive are used up")

// driveNext is the next file the drive's search should get ready: one with
// no release kept (or a dead one), not tried this week, already out, newest
// titles first.
func (s *Server) driveNext(ctx context.Context) (int64, error) {
	cut := time.Now().Add(-driveRetryAfter).UTC().Format(time.RFC3339)
	now := time.Now()
	today := now.Format("2006-01-02")
	// A film from a later year isn't out, and an episode that hasn't aired
	// has nothing to find: neither is worth a search yet.
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT f.id FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id
		WHERE (f.nzb = '' OR f.health = 'dead') AND (f.drive_tried_at = '' OR f.drive_tried_at < ?)
		  AND ((t.kind = 'movie' AND t.year <= ?) OR (t.kind != 'movie' AND f.air_date != '' AND f.air_date <= ?))
		ORDER BY t.added_at DESC, f.season, f.episode LIMIT 1`, cut, now.Year(), today).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errDriveNothingToDo
	}
	return id, err
}

// drivePrepOne gets one more streamed file ready for the drive: one search
// (from the day's budget), then up to three releases tried. It says what it
// got ready.
func (s *Server) drivePrepOne(ctx context.Context) (string, error) {
	id, err := s.driveNext(ctx)
	if err != nil {
		return "", err
	}
	f, err := s.streamedFile(ctx, id)
	if err != nil {
		return "", err
	}
	label := f.title
	kind := "movie"
	if f.kind == "series" {
		kind = "series"
		label = fmt.Sprintf("%s S%02dE%02d", f.title, f.season, f.episode)
	}
	servers, err := s.ownServers()
	if err != nil {
		return label, err
	}
	q, err := s.ownTitleQuery(ctx, kind, f.tmdb, f.season, f.episode, driveQuality)
	if err != nil {
		return label, err
	}
	// Only now is a search made: it counts against the day's budget, and the
	// title isn't tried again this week whatever comes of it.
	if !s.driveSpend() {
		return label, errDriveNoBudget
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE streamed_files SET drive_tried_at = ?, drive_tries = drive_tries + 1 WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), id)
	list, err := s.ownCandidates(ctx, q)
	if err != nil {
		return label, err
	}
	if len(list) == 0 {
		return label, errors.New("no release of it on your indexers right now")
	}
	var last error
	for i, r := range list {
		if i >= 3 {
			break
		}
		data, sf, err := s.ustreamFetch(ctx, servers, r)
		if err != nil {
			last = err
			continue
		}
		if err := s.driveKeep(id, r.Title, data, sf); err != nil {
			return label, err
		}
		slog.Info("drive: ready", "title", label, "release", r.Title, "size", strconv.FormatInt(sf.Size>>20, 10)+" MB")
		return label, nil
	}
	return label, fmt.Errorf("none of the releases would open: %w", last)
}

// driveJob is the background search: one title at a time, while the drive is
// on and the day's budget lasts.
func (s *Server) driveJob(ctx context.Context) {
	if !s.driveOn() || s.driveBudget() == 0 {
		return
	}
	label, err := s.drivePrepOne(ctx)
	switch {
	case err == nil, errors.Is(err, errDriveNothingToDo), errors.Is(err, errDriveNoBudget):
	default:
		slog.Info("drive: couldn't get a title ready", "title", label, "err", err)
	}
}

// ---- the API ----

type driveStatus struct {
	On        bool   `json:"on"`
	OnByPlex  bool   `json:"onByPlex"` // not chosen yet: on because Plex is the media server
	Plex      bool   `json:"plex"`     // Plex is the media server (and no Jellyfin)
	Address   string `json:"address"`  // the drive's address, as a device at home reaches it
	User      string `json:"user"`
	Password  string `json:"password"`
	Ready     int    `json:"ready"`
	Waiting   int    `json:"waiting"`
	Dead      int    `json:"dead"`
	Budget    int    `json:"budget"`
	UsedToday int    `json:"usedToday"`
	LastRead  int64  `json:"lastRead,omitempty"` // ms: the last time anything read the drive
	Where     string `json:"where"`              // where the media server's computer has it mounted
}

func (s *Server) driveStatus(ctx context.Context) driveStatus {
	v, _ := s.Settings.Get(keyDriveOn)
	st := driveStatus{On: s.driveOn(), OnByPlex: v == "" && s.plexOnly(), Plex: s.plexOnly(), User: driveUser, Budget: s.driveBudget(), UsedToday: s.driveUsedToday()}
	if base := s.mediariumAddress(keyHomeAddress); base != "" {
		st.Address = strings.TrimRight(base, "/") + "/dav/"
	}
	if st.On {
		st.Password = s.drivePassword()
	}
	_ = s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(nzb != '' AND size > 0 AND health != 'dead'), 0),
		COALESCE(SUM(nzb = ''), 0),
		COALESCE(SUM(health = 'dead'), 0) FROM streamed_files`).Scan(&st.Ready, &st.Waiting, &st.Dead)
	st.LastRead = driveLastRead.Load()
	st.Where = s.driveWhere()
	return st
}

// GET /api/home/drive (administrators): the drive's state, address and sign-in.
func (s *Server) handleDrive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.driveStatus(r.Context()))
}

// PUT /api/home/drive {"on","budget","where","newPassword"}.
func (s *Server) handlePutDrive(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On          *bool   `json:"on"`
		Budget      *int    `json:"budget"`
		NewPassword bool    `json:"newPassword"`
		Where       *string `json:"where"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "That couldn't be read.")
		return
	}
	if in.On != nil {
		v := "0"
		if *in.On {
			v = "1"
		}
		_ = s.Settings.Set(keyDriveOn, v, false)
	}
	if in.Budget != nil {
		if *in.Budget < 0 || *in.Budget > driveBudgetMax {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("The searches a day go from 0 to %d.", driveBudgetMax))
			return
		}
		_ = s.Settings.Set(keyDriveBudget, strconv.Itoa(*in.Budget), false)
	}
	if in.Where != nil {
		where := strings.TrimSpace(*in.Where)
		if len(where) > 200 || strings.ContainsAny(where, "\r\n") {
			writeError(w, http.StatusBadRequest, "Where the drive is mounted should be a drive letter (U:) or a folder (/mnt/usherr).")
			return
		}
		_ = s.Settings.Set(keyDriveWhere, where, false)
	}
	if in.NewPassword {
		s.newDrivePassword()
	}
	writeJSON(w, http.StatusOK, s.driveStatus(r.Context()))
}

// POST /api/home/drive/prepare: get one more title ready now (it counts
// against the day's searches).
func (s *Server) handleDrivePrepare(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	label, err := s.drivePrepOne(ctx)
	out := map[string]any{"title": label}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}
