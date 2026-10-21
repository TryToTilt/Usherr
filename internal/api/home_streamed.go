package api

// Home fork: the streamed library. Movies and shows Mediarium plays through
// StreamNZB instead of keeping their files. Each movie or episode has its
// saved NZB (the exact release it was collected with), and Jellyfin sees it
// through a small .strm pointer file whose address leads back here:
//
//   /api/strm/{key}/{file}   Mediarium hands the saved NZB to StreamNZB and
//                            passes the video on (seeking included); if that
//                            release has died it falls back to StreamNZB's
//                            own search for the title.
//
// Titles come in through an import bundle (the config folder's
// streamed/import: manifest.json and an NZB per file), which
// tools/home/streamed_export.py builds from the old Stream tier. Mediarium
// looks each title up on TMDB for its posters, descriptions and episode
// names, saves the NZBs, and writes the pointer files.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/parser"
)

const (
	keyStreamedDir     = "home.streamed_dir"      // where the pointer files go, inside Mediarium
	keyStreamedHostDir = "home.streamed_host_dir" // the same folder as Jellyfin sees it
	keyStreamedBase    = "home.streamed_base"     // the address pointer files lead to
	defaultStreamedDir = "/data/streamed"
)

func (s *Server) streamedSetting(key, def string) string {
	if v, _ := s.Settings.Get(key); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (s *Server) streamedNZBDir() string { return filepath.Join(s.cfg.ConfigDir, "streamed", "nzb") }
func (s *Server) streamedImportDir() string {
	return filepath.Join(s.cfg.ConfigDir, "streamed", "import")
}

// ---- the import ----

type streamedManifest struct {
	Titles []streamedImportTitle `json:"titles"`
}

// streamedImportTitle is one title to add to the streamed library: from an
// import bundle, or "Add as stream" (no release: it plays from a search).
type streamedImportTitle struct {
	Kind   string               `json:"kind"` // movie | series
	TMDBID int                  `json:"tmdbId"`
	IMDBID string               `json:"imdbId"`
	TVDBID int                  `json:"tvdbId"`
	Title  string               `json:"title"`
	Year   int                  `json:"year"`
	Anime  bool                 `json:"anime"`
	Files  []streamedImportFile `json:"files"`
}

type streamedImportFile struct {
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	Release   string `json:"release"`
	NZB       string `json:"nzb"`
	InnerFile string `json:"innerFile"`
	Size      int64  `json:"size"`
}

type streamedImportState struct {
	Running  bool      `json:"running"`
	Done     int       `json:"done"`
	Total    int       `json:"total"`
	Files    int       `json:"files"`
	Failed   []string  `json:"failed,omitempty"`
	Started  time.Time `json:"started,omitempty"`
	Finished time.Time `json:"finished,omitempty"`
	Message  string    `json:"message,omitempty"`
}

var (
	streamedImportMu sync.Mutex
	streamedImport   streamedImportState
)

// GET /api/streamed/import: how the import is going.
func (s *Server) handleStreamedImportStatus(w http.ResponseWriter, r *http.Request) {
	streamedImportMu.Lock()
	st := streamedImport
	streamedImportMu.Unlock()
	_, err := os.Stat(filepath.Join(s.streamedImportDir(), "manifest.json"))
	writeJSON(w, http.StatusOK, map[string]any{"state": st, "waiting": err == nil})
}

// POST /api/streamed/import: import the waiting bundle, in the background.
func (s *Server) handleStreamedImport(w http.ResponseWriter, r *http.Request) {
	raw, err := os.ReadFile(filepath.Join(s.streamedImportDir(), "manifest.json"))
	if err != nil {
		writeError(w, http.StatusNotFound, "There's nothing waiting to import.")
		return
	}
	var m streamedManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		writeError(w, http.StatusBadRequest, "The import list can't be read: "+err.Error())
		return
	}
	streamedImportMu.Lock()
	if streamedImport.Running {
		streamedImportMu.Unlock()
		writeError(w, http.StatusConflict, "An import is already running.")
		return
	}
	streamedImport = streamedImportState{Running: true, Total: len(m.Titles), Started: time.Now()}
	streamedImportMu.Unlock()
	s.background(func() { s.runStreamedImport(m) })
	writeJSON(w, http.StatusAccepted, map[string]any{"titles": len(m.Titles)})
}

func (s *Server) runStreamedImport(m streamedManifest) {
	ctx := context.Background()
	if err := os.MkdirAll(s.streamedNZBDir(), 0o755); err != nil {
		s.finishStreamedImport(err.Error())
		return
	}
	// A few at a time: each title is a handful of TMDB lookups.
	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				t := m.Titles[i]
				files, err := s.importStreamedTitle(ctx, t)
				streamedImportMu.Lock()
				streamedImport.Done++
				streamedImport.Files += files
				if err != nil {
					streamedImport.Failed = append(streamedImport.Failed, fmt.Sprintf("%s (%d): %v", t.Title, t.Year, err))
					slog.Info("streamed: import", "title", t.Title, "err", err)
				}
				streamedImportMu.Unlock()
			}
		}()
	}
	for i := range m.Titles {
		work <- i
	}
	close(work)
	wg.Wait()
	// The bundle is done with: keep it, renamed, so it isn't imported twice.
	dir := s.streamedImportDir()
	stamp := time.Now().Format("20060102-150405")
	_ = os.Rename(filepath.Join(dir, "manifest.json"), filepath.Join(dir, "manifest.done-"+stamp+".json"))
	s.finishStreamedImport("")
	// Only what isn't on disk keeps a pointer.
	if _, _, err := s.syncStreamedPointers(ctx); err != nil {
		slog.Info("streamed: sync after import", "err", err)
	}
}

func (s *Server) finishStreamedImport(msg string) {
	streamedImportMu.Lock()
	streamedImport.Running = false
	streamedImport.Finished = time.Now()
	streamedImport.Message = msg
	streamedImportMu.Unlock()
}

type streamedEpisodeInfo struct{ name, overview, airDate string }

// importStreamedTitle adds (or refreshes) one title and its files, with
// TMDB's details, and writes its pointer files. It returns how many files it
// added.
func (s *Server) importStreamedTitle(ctx context.Context, t streamedImportTitle) (int, error) {
	if t.Kind == "series" && t.TMDBID <= 0 && t.TVDBID > 0 && s.TMDB() != nil {
		// Sonarr only knew its TheTVDB id.
		lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if sh, ok, err := s.TMDB().FindShowByTVDBID(lctx, t.TVDBID); err == nil && ok {
			t.TMDBID = sh.TMDBID
		}
		cancel()
	}
	if (t.Kind != "movie" && t.Kind != "series") || t.TMDBID <= 0 {
		return 0, errors.New("not a movie or show with a TMDB id")
	}
	title, year, imdb := t.Title, t.Year, t.IMDBID
	var overview, poster, backdrop, rating string
	var score float64
	var genres []string
	episodes := map[[2]int]streamedEpisodeInfo{}
	if tm := s.TMDB(); tm != nil {
		lctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		if t.Kind == "movie" {
			if d, err := tm.GetMovieDetail(lctx, t.TMDBID); err == nil {
				title, overview, poster, backdrop, rating, score = d.Title, d.Overview, d.PosterPath, d.BackdropPath, d.Certification(), d.VoteAverage
				if y := d.Year(); y > 0 {
					year = y
				}
				if d.IMDBID != "" {
					imdb = d.IMDBID
				}
				for _, g := range d.Genres {
					genres = append(genres, g.Name)
				}
			}
		} else {
			if d, err := tm.GetShowFull(lctx, t.TMDBID); err == nil {
				title, overview, poster, backdrop, rating, score = d.Name, d.Overview, d.PosterPath, d.BackdropPath, d.ContentRating(), d.VoteAverage
				if y := d.Year(); y > 0 {
					year = y
				}
				if d.ExternalIDs.IMDBID != "" {
					imdb = d.ExternalIDs.IMDBID
				}
				for _, g := range d.Genres {
					genres = append(genres, g.Name)
				}
			}
			seasons := map[int]bool{}
			for _, f := range t.Files {
				seasons[f.Season] = true
			}
			for n := range seasons {
				if eps, err := tm.GetSeason(lctx, t.TMDBID, n); err == nil {
					for _, e := range eps {
						episodes[[2]int{e.Season, e.Episode}] = streamedEpisodeInfo{e.Name, e.Overview, e.AirDate}
					}
				}
			}
		}
		cancel()
	}
	if genres == nil {
		genres = []string{}
	}
	g, _ := json.Marshal(genres)
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO streamed_titles (kind, tmdb_id, imdb_id, tvdb_id, title, year, overview, poster_path, backdrop_path, genres, rating, score, anime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (kind, tmdb_id) DO UPDATE SET imdb_id = excluded.imdb_id, title = excluded.title, year = excluded.year,
			overview = excluded.overview, poster_path = excluded.poster_path, backdrop_path = excluded.backdrop_path,
			genres = excluded.genres, rating = excluded.rating, score = excluded.score, anime = excluded.anime
		RETURNING id`,
		t.Kind, t.TMDBID, imdb, t.TVDBID, title, year, overview, poster, backdrop, string(g), rating, score, t.Anime).Scan(&id)
	if err != nil {
		return 0, err
	}
	added := 0
	for _, f := range t.Files {
		nzb := ""
		if f.NZB != "" { // optional: titles play from StreamNZB's search either way
			data, err := os.ReadFile(filepath.Join(s.streamedImportDir(), "nzb", filepath.Base(f.NZB)))
			if err == nil && os.WriteFile(filepath.Join(s.streamedNZBDir(), filepath.Base(f.NZB)), data, 0o644) == nil {
				nzb = filepath.Base(f.NZB)
			}
		}
		ep := episodes[[2]int{f.Season, f.Episode}]
		var fid int64
		var err error
		err = s.db.QueryRowContext(ctx, `INSERT INTO streamed_files (title_id, season, episode, name, overview, air_date, release, nzb, inner_file, size)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (title_id, season, episode) DO UPDATE SET name = excluded.name, overview = excluded.overview,
				air_date = excluded.air_date, release = excluded.release, nzb = excluded.nzb, inner_file = excluded.inner_file,
				size = excluded.size, health = ''
			RETURNING id`, id, f.Season, f.Episode, ep.name, ep.overview, ep.airDate, f.Release, nzb, f.InnerFile, f.Size).Scan(&fid)
		if err != nil {
			return added, err
		}
		added++
		rel := streamedPointerPath(t.Kind, t.Anime, title, year, t.TMDBID, f.Season, f.Episode)
		if err := s.writePointer(rel, fid); errors.Is(err, errNoAddress) {
			continue // the pointer comes once Mediarium's address is set
		} else if err != nil {
			return added, err
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE streamed_files SET strm_path = ? WHERE id = ?`, rel, fid)
	}
	return added, nil
}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// cleanName makes a title safe as a Windows file or folder name.
func cleanName(s string) string {
	s = unsafeName.ReplaceAllString(s, "")
	s = strings.TrimRight(strings.TrimSpace(s), ". ")
	if s == "" {
		return "Untitled"
	}
	return s
}

// streamedPointerPath is where a file's pointer goes, relative to the
// streamed folder, named the way Jellyfin recognises:
//
//	Movies/Title (Year) [tmdbid-1]/Title (Year).strm
//	TV/Title (Year) [tmdbid-1]/Season 01/Title S01E02.strm   (Anime/ for anime)
func streamedPointerPath(kind string, anime bool, title string, year, tmdb, season, episode int) string {
	name := cleanName(title)
	folder := name
	if year > 0 && !strings.Contains(name, fmt.Sprintf("(%d)", year)) {
		folder = fmt.Sprintf("%s (%d)", name, year)
	}
	folder = fmt.Sprintf("%s [tmdbid-%d]", folder, tmdb)
	if kind == "movie" {
		file := name
		if year > 0 {
			file = fmt.Sprintf("%s (%d)", name, year)
		}
		return filepath.ToSlash(filepath.Join("Movies", folder, file+".strm"))
	}
	root := "TV"
	if anime {
		root = "Anime"
	}
	return filepath.ToSlash(filepath.Join(root, folder, fmt.Sprintf("Season %02d", season), fmt.Sprintf("%s S%02dE%02d.strm", name, season, episode)))
}

func (s *Server) writePointer(rel string, fileID int64) error {
	base := s.mediariumAddress(keyStreamedBase)
	if base == "" {
		return errNoAddress
	}
	full := filepath.Join(s.streamedSetting(keyStreamedDir, defaultStreamedDir), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(fmt.Sprintf("%s/api/strm/%s/%d\n", base, s.tvFeedKey(), fileID)), 0o644)
}

// ---- playing ----

type streamedPlay struct {
	target  string // the video, as Mediarium reaches it
	homeURL string // as a device at home reaches it
	snzbID  string // StreamNZB's session, closed when done with
	release string
	last    time.Time
}

// playKey is a file at a quality ("" is the best there is, for the TV and
// the media server).
type playKey struct {
	id      int64
	quality string
}

var (
	streamedPlaysMu sync.Mutex
	streamedPlays   = map[playKey]*streamedPlay{}
	streamedTried   = map[playKey][]string{} // releases passed over with "Try another release"
)

type streamedFileRow struct {
	id, titleID     int64
	kind            string
	tmdb            int
	imdb            string
	title           string
	season, episode int
	release, nzb    string
}

func (s *Server) streamedFile(ctx context.Context, id int64) (streamedFileRow, error) {
	var f streamedFileRow
	err := s.db.QueryRowContext(ctx, `SELECT f.id, f.title_id, t.kind, t.tmdb_id, t.imdb_id, t.title, f.season, f.episode, f.release, f.nzb
		FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id WHERE f.id = ?`, id).
		Scan(&f.id, &f.titleID, &f.kind, &f.tmdb, &f.imdb, &f.title, &f.season, &f.episode, &f.release, &f.nzb)
	return f, err
}

// playNZBData starts StreamNZB on an NZB's contents (uploaded, not fetched).
func (c *streamNZB) playNZBData(ctx context.Context, name string, data []byte) (playNZBResult, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		return playNZBResult{}, err
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	var out playNZBResult
	err = c.do(ctx, http.MethodPost, "/api/play/nzb", &buf, mw.FormDataContentType(), &out)
	if err == nil && out.PlayPath == "" && out.PlayURL == "" && out.ExternalPlayURL == "" {
		err = errors.New("StreamNZB gave no play link")
	}
	return out, err
}

// streamedSource is where a streamed file plays from right now. Like Stremio
// with StreamNZB: StreamNZB's search picks the best release that works today
// (no AV1, as the TV can't play it; each tried before use). The saved NZB,
// when there is one, is only a last resort. The pick is remembered for a few
// hours, so seeking, resuming and coming back later start at once.
func (s *Server) streamedSource(ctx context.Context, id int64) (*streamedPlay, error) {
	return s.streamedSourceFor(ctx, id, "", false)
}

// streamedSourceFor is streamedSource at a quality (4K, 1080p, 720p; "" for
// the best), or, with next, the next release in line after the one it gave
// last time.
func (s *Server) streamedSourceFor(ctx context.Context, id int64, quality string, next bool) (*streamedPlay, error) {
	key := playKey{id, quality}
	streamedPlaysMu.Lock()
	if next {
		if p, ok := streamedPlays[key]; ok {
			streamedTried[key] = append(streamedTried[key], p.release)
			delete(streamedPlays, key)
		}
	} else if p, ok := streamedPlays[key]; !ok || time.Since(p.last) > 3*time.Hour {
		delete(streamedTried, key) // a fresh start: everything is in line again
	}
	skip := append([]string(nil), streamedTried[key]...)
	if p, ok := streamedPlays[key]; ok && time.Since(p.last) < 3*time.Hour {
		idle := time.Since(p.last)
		p.last = time.Now()
		streamedPlaysMu.Unlock()
		if idle < 20*time.Minute {
			return p, nil
		}
		// Not used for a while: make sure it still plays before handing it out.
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		err := checkStreamedStart(cctx, p.target)
		cancel()
		if err == nil {
			return p, nil
		}
		streamedPlaysMu.Lock()
		delete(streamedPlays, key)
		streamedPlaysMu.Unlock()
	} else {
		streamedPlaysMu.Unlock()
	}
	s.sweepStreamedPlays()

	f, err := s.streamedFile(ctx, id)
	if err != nil {
		return nil, err
	}
	// Usherr: the engine chosen in Settings > Extras goes first; the other
	// is tried when it finds nothing.
	c, cerr := s.streamNZB()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 150*time.Second)
	defer cancel()
	var p *streamedPlay
	var serr error
	byNZB := func() {
		if p == nil && c != nil {
			var e error
			if p, e = s.searchedSource(ctx, c, f, quality, skip); e != nil && serr == nil {
				serr = e
			}
		}
	}
	byOwn := func() {
		if p == nil && s.ownProblem() == "" {
			var e error
			if p, e = s.ownSearchedSource(ctx, f, quality, skip); e != nil && serr == nil {
				serr = e
			}
		}
	}
	if s.streamEngine() == engineUsherr {
		byOwn()
		byNZB()
	} else {
		byNZB()
		byOwn()
	}
	if p == nil && len(skip) == 0 && c != nil {
		p = s.savedSource(ctx, c, f)
	}
	if p == nil {
		if serr == nil {
			serr = cerr
		}
		if serr == nil {
			serr = errors.New("nothing playable to stream for this right now")
		}
		if len(skip) > 0 {
			serr = errors.New("There is no other release that plays right now")
		}
		return nil, serr
	}
	p.last = time.Now()
	streamedPlaysMu.Lock()
	streamedPlays[key] = p
	streamedPlaysMu.Unlock()
	return p, nil
}

// forgetStreamedSource drops a remembered pick that stopped working.
func forgetStreamedSource(id int64) {
	streamedPlaysMu.Lock()
	for k := range streamedPlays {
		if k.id == id {
			delete(streamedPlays, k)
		}
	}
	streamedPlaysMu.Unlock()
}

// searchedSource asks StreamNZB for the title, best stream first, at the
// quality asked for (or the nearest below it), passing over the releases in
// skip.
func (s *Server) searchedSource(ctx context.Context, c *streamNZB, f streamedFileRow, quality string, skip []string) (*streamedPlay, error) {
	profiles, err := c.profiles(ctx)
	if err != nil {
		return nil, err
	}
	order := []string{"4K", "1080p", "720p"}
	for i, q := range order {
		if q == quality {
			order = order[i:]
		}
	}
	var prof snzbProfile
	for _, q := range order {
		if pp, ok := profileFor(profiles, q); ok {
			prof = pp
			break
		}
	}
	imdb := f.imdb
	if imdb == "" {
		if imdb, err = s.imdbFor(ctx, f.kind, f.tmdb); err != nil {
			return nil, err
		}
	}
	kind, sid := "movie", imdb
	if f.kind == "series" {
		kind, sid = "series", fmt.Sprintf("%s:%d:%d", imdb, f.season, f.episode)
	}
	list, err := c.titleStreams(ctx, prof.Token, kind, sid)
	if err != nil {
		return nil, err
	}
	tried := 0
	for _, st := range list {
		rel := releaseLine(st.Title + "\n" + st.Description)
		if strings.Contains(strings.ToLower(rel), "av1") || slices.Contains(skip, rel) {
			continue
		}
		if tried++; tried > 4 {
			break
		}
		cand := &streamedPlay{target: c.internalURL(st.URL), homeURL: st.URL, release: rel}
		if verr := checkStreamedStart(ctx, cand.target); verr != nil {
			slog.Info("streamed: search result won't play", "title", f.title, "release", rel, "err", verr)
			continue
		}
		// StreamNZB doesn't always name the release: look at the video for
		// AV1 (which the TV can't play) without holding the start up; if it
		// is, the next play picks again.
		if parser.Parse(rel).Resolution == "" {
			go func(id int64, target string) {
				pctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				if pr, perr := probeStream(pctx, target); perr == nil && pr.video == "av1" {
					slog.Info("streamed: picked release is AV1, will pick again next time", "title", f.title)
					forgetStreamedSource(id)
				}
			}(f.id, cand.target)
		}
		return cand, nil
	}
	return nil, errors.New("StreamNZB found nothing playable for this right now")
}

// savedSource plays the NZB saved when the title was collected, if any.
func (s *Server) savedSource(ctx context.Context, c *streamNZB, f streamedFileRow) *streamedPlay {
	if f.nzb == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(s.streamedNZBDir(), filepath.Base(f.nzb)))
	if err != nil {
		return nil
	}
	res, err := c.playNZBData(ctx, f.release+".nzb", data)
	if err != nil {
		s.markStreamedHealth(f.id, "dead")
		return nil
	}
	link := res.PlayURL
	if link == "" {
		link = res.PlayPath
	}
	home := res.ExternalPlayURL
	if home == "" {
		home = res.PlayURL
	}
	p := &streamedPlay{target: c.internalURL(link), homeURL: home, snzbID: res.SessionID, release: f.release}
	if err := checkStreamedStart(ctx, p.target); err != nil {
		c.closeSession(context.Background(), res.SessionID)
		s.markStreamedHealth(f.id, "dead")
		return nil
	}
	s.markStreamedHealth(f.id, "ok")
	return p
}

// checkStreamedStart reads the first byte of a new StreamNZB session,
// waiting while it gets going: an error means it won't play (StreamNZB sends
// players to its error video).
func checkStreamedStart(ctx context.Context, target string) error {
	for tries := 0; tries < 25; tries++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Range", "bytes=0-0")
		resp, err := streamRelayClient.Do(req)
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if strings.Contains(resp.Request.URL.Path, "/error/") {
			return errors.New("StreamNZB sent its error video")
		}
		switch {
		case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent:
			return nil
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode >= 500:
			if !sleepCtx(ctx, time.Second) {
				return ctx.Err()
			}
		default:
			return fmt.Errorf("StreamNZB answered %s", resp.Status)
		}
	}
	return errors.New("StreamNZB didn't get it going in time")
}

func (s *Server) markStreamedHealth(id int64, health string) {
	_, _ = s.db.Exec(`UPDATE streamed_files SET health = ?, health_at = ? WHERE id = ?`, health, time.Now().UTC().Format(time.RFC3339), id)
}

// sweepStreamedPlays closes StreamNZB sessions nobody has read from lately.
func (s *Server) sweepStreamedPlays() {
	var stale []string
	streamedPlaysMu.Lock()
	for k, p := range streamedPlays {
		if time.Since(p.last) > 45*time.Minute {
			if p.snzbID != "" {
				stale = append(stale, p.snzbID)
			}
			delete(streamedPlays, k)
		}
	}
	streamedPlaysMu.Unlock()
	if len(stale) == 0 {
		return
	}
	if c, err := s.streamNZB(); err == nil {
		for _, id := range stale {
			c.closeSession(context.Background(), id)
		}
	}
}

func (s *Server) registerStreamedRoutes(public *http.ServeMux) {
	public.HandleFunc("GET /api/strm/{key}/{file}", s.handleStrm)
	public.HandleFunc("HEAD /api/strm/{key}/{file}", s.handleStrm)
}

// GET /api/strm/{key}/{file}: a pointer file being played (by Jellyfin, or a
// player it hands the address to).
func (s *Server) handleStrm(w http.ResponseWriter, r *http.Request) {
	if !s.tvFeedOK(r) {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("file"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.streamedSource(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.readyNextEpisode(id)
	relayVideo(w, r, p.target, func() {
		streamedPlaysMu.Lock()
		p.last = time.Now()
		streamedPlaysMu.Unlock()
	})
}

// relayVideo passes a video on with its range requests, so players can seek.
func relayVideo(w http.ResponseWriter, r *http.Request, target string, touch func()) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, h := range []string{"Range", "If-Range"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	resp, err := streamRelayClient.Do(req)
	// A session StreamNZB has only just made answers "not found" for a few
	// seconds while it reads the release: wait for it.
	for tries := 0; err == nil && resp.StatusCode == http.StatusNotFound && tries < 20; tries++ {
		resp.Body.Close()
		if !sleepCtx(r.Context(), time.Second) {
			return
		}
		req2 := req.Clone(r.Context())
		resp, err = streamRelayClient.Do(req2)
	}
	if err != nil {
		if r.Context().Err() == nil {
			http.Error(w, "StreamNZB didn't answer: "+err.Error(), http.StatusBadGateway)
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
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			touch()
		}
		if rerr != nil {
			if rerr != io.EOF {
				slog.Debug("streamed: relay ended", "err", rerr)
			}
			return
		}
	}
}

// ---- the app's pages ----

type streamedTitlePayload struct {
	ID       int64    `json:"id"`
	Kind     string   `json:"kind"`
	TMDBID   int      `json:"tmdbId"`
	Title    string   `json:"title"`
	Year     int      `json:"year"`
	Overview string   `json:"overview,omitempty"`
	Poster   string   `json:"poster,omitempty"`
	Backdrop string   `json:"backdrop,omitempty"`
	Genres   []string `json:"genres"`
	Rating   string   `json:"rating,omitempty"`
	Score    float64  `json:"score,omitempty"`
	Anime    bool     `json:"anime,omitempty"`
	Files    int      `json:"files"`
	Release  string   `json:"release,omitempty"` // movies: the release
}

// GET /api/streamed: every streamed title, as a card needs it (the overview
// and backdrop come with the title itself, so the list stays small).
func (s *Server) handleStreamedList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT t.id, t.kind, t.tmdb_id, t.title, t.year, t.poster_path, t.genres,
		t.rating, t.score, t.anime, COUNT(f.id), COALESCE(MAX(f.release), '')
		FROM streamed_titles t LEFT JOIN streamed_files f ON f.title_id = t.id GROUP BY t.id ORDER BY t.title COLLATE NOCASE`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []streamedTitlePayload{}
	for rows.Next() {
		var p streamedTitlePayload
		var poster, genres, release string
		var anime int
		if err := rows.Scan(&p.ID, &p.Kind, &p.TMDBID, &p.Title, &p.Year, &poster, &genres, &p.Rating, &p.Score, &anime, &p.Files, &release); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		p.Poster, p.Anime = metadata.ImageURL("w342", poster), anime == 1
		_ = json.Unmarshal([]byte(genres), &p.Genres)
		if p.Kind == "movie" {
			p.Release = release
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

type streamedFilePayload struct {
	ID       int64  `json:"id"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
	Name     string `json:"name,omitempty"`
	Overview string `json:"overview,omitempty"`
	AirDate  string `json:"airDate,omitempty"`
	Release  string `json:"release"`
	Size     int64  `json:"size"`
	Health   string `json:"health,omitempty"`
}

// GET /api/streamed/{id}: a title and its files.
func (s *Server) handleStreamedTitle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a title.")
		return
	}
	var p streamedTitlePayload
	var poster, backdrop, genres string
	var anime int
	err = s.db.QueryRowContext(r.Context(), `SELECT id, kind, tmdb_id, title, year, overview, poster_path, backdrop_path, genres, rating, score, anime
		FROM streamed_titles WHERE id = ?`, id).Scan(&p.ID, &p.Kind, &p.TMDBID, &p.Title, &p.Year, &p.Overview, &poster, &backdrop, &genres, &p.Rating, &p.Score, &anime)
	if err != nil {
		writeError(w, http.StatusNotFound, "That title isn't in the streamed library.")
		return
	}
	p.Poster, p.Backdrop, p.Anime = metadata.ImageURL("w342", poster), metadata.ImageURL("w1280", backdrop), anime == 1
	_ = json.Unmarshal([]byte(genres), &p.Genres)
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, season, episode, name, overview, air_date, release, size, health
		FROM streamed_files WHERE title_id = ? ORDER BY season, episode`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	files := []streamedFilePayload{}
	for rows.Next() {
		var f streamedFilePayload
		if err := rows.Scan(&f.ID, &f.Season, &f.Episode, &f.Name, &f.Overview, &f.AirDate, &f.Release, &f.Size, &f.Health); err == nil {
			files = append(files, f)
		}
	}
	p.Files = len(files)
	writeJSON(w, http.StatusOK, map[string]any{"title": p, "files": files})
}

// POST /api/streamed/files/{id}/watch: play a streamed file in Mediarium's
// player (a Stream now session of the viewer's).
func (s *Server) handleStreamedWatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a file.")
		return
	}
	f, err := s.streamedFile(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That isn't in the streamed library.")
		return
	}
	var opt struct {
		Quality string `json:"quality"` // 4K, 1080p, 720p; "" for the best
		Next    bool   `json:"next"`    // the next release in line
	}
	if r.ContentLength > 0 {
		_ = decodeJSON(r, &opt)
	}
	p, err := s.streamedSourceFor(r.Context(), id, opt.Quality, opt.Next)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	title := f.title
	if f.kind == "series" {
		title = fmt.Sprintf("%s · S%02dE%02d", f.title, f.season, f.episode)
	}
	ss := &streamSession{id: randomID("str_"), userID: requestUserID(r), last: time.Now(), title: title,
		target: p.target, lanURL: p.homeURL, streamFor: "release", release: p.release}
	s.readyNextEpisode(id)
	c, _ := s.streamNZB()
	streams.sweep(c) // Usherr: c is nil without StreamNZB; own streams are swept too
	streams.mu.Lock()
	streams.sessions[ss.id] = ss
	streams.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"id": ss.id, "videoUrl": "/api/stream/" + ss.id + "/video", "title": ss.title, "release": ss.release,
		"hdr": hdrOf(ss.release), "audio": audioOf(ss.release),
	})
}

// POST /api/streamed/jellyfin: add the streamed folders to Jellyfin as
// libraries (once), and have it scan them.
func (s *Server) handleStreamedJellyfin(w http.ResponseWriter, r *http.Request) {
	srv, ok := s.jellyfinServer()
	if !ok {
		writeError(w, http.StatusPreconditionFailed, connectServerFirst)
		return
	}
	host := s.streamedHostDir(srv)
	if host == "" {
		writeError(w, http.StatusPreconditionFailed, "Say where "+serverLabel(srv)+" sees the streamed folder, under Settings > Extras.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	var folders []struct {
		Name      string   `json:"Name"`
		Locations []string `json:"Locations"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/Library/VirtualFolders", nil, &folders); err != nil {
		writeError(w, http.StatusBadGateway, "Couldn't read "+serverLabel(srv)+"'s libraries: "+err.Error())
		return
	}
	inside := s.streamedSetting(keyStreamedDir, defaultStreamedDir)
	added := []string{}
	for _, lib := range []struct{ name, kind, sub string }{
		{"Streamed Movies", "movies", "Movies"}, {"Streamed TV", "tvshows", "TV"}, {"Streamed Anime", "tvshows", "Anime"},
	} {
		if _, err := os.Stat(filepath.Join(inside, lib.sub)); err != nil {
			continue // nothing of that kind yet
		}
		path := joinHostPath(host, lib.sub)
		exists := false
		for _, f := range folders {
			if strings.EqualFold(f.Name, lib.name) {
				exists = true
			}
		}
		if exists {
			continue
		}
		q := fmt.Sprintf("/Library/VirtualFolders?name=%s&collectionType=%s&refreshLibrary=true", url.QueryEscape(lib.name), lib.kind)
		body := map[string]any{"LibraryOptions": map[string]any{
			"PathInfos":                               []map[string]string{{"Path": path}},
			"EnableRealtimeMonitor":                   true,
			"PreferredMetadataLanguage":               "en",
			"MetadataCountryCode":                     "US",
			"EnableChapterImageExtraction":            false,
			"ExtractChapterImagesDuringLibraryScan":   false,
			"EnableTrickplayImageExtraction":          false,
			"ExtractTrickplayImagesDuringLibraryScan": false,
		}}
		if err := jfJSON(ctx, srv, http.MethodPost, q, body, nil); err != nil {
			writeError(w, http.StatusBadGateway, serverLabel(srv)+" didn't add "+lib.name+": "+err.Error())
			return
		}
		added = append(added, lib.name)
	}
	if len(added) == 0 {
		_ = jfJSON(ctx, srv, http.MethodPost, "/Library/Refresh", nil, nil)
	}
	quiet := s.quietStreamedLibraries(ctx, srv)
	excluded := s.excludeStreamedFromIntroSkipper(ctx, srv, host)
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "quieted": quiet, "introSkipperExcluded": excluded})
}

// quietStreamedLibraries turns off what would read whole streamed files
// from Usenet in the background: trickplay previews and chapter images.
func (s *Server) quietStreamedLibraries(ctx context.Context, srv mediaservers.Server) int {
	var folders []struct {
		Name    string         `json:"Name"`
		ItemID  string         `json:"ItemId"`
		Options map[string]any `json:"LibraryOptions"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/Library/VirtualFolders", nil, &folders); err != nil {
		return 0
	}
	n := 0
	for _, f := range folders {
		if !strings.HasPrefix(f.Name, "Streamed ") || f.Options == nil {
			continue
		}
		for _, k := range []string{"EnableTrickplayImageExtraction", "ExtractTrickplayImagesDuringLibraryScan", "EnableChapterImageExtraction", "ExtractChapterImagesDuringLibraryScan"} {
			f.Options[k] = false
		}
		if jfJSON(ctx, srv, http.MethodPost, "/Library/VirtualFolders/LibraryOptions", map[string]any{"Id": f.ItemID, "LibraryOptions": f.Options}, nil) == nil {
			n++
		}
	}
	return n
}

// excludeStreamedFromIntroSkipper keeps the Intro Skipper plugin (when
// Jellyfin has it) out of the streamed folders: it reads a quarter of each
// episode to find intros, which here would come from Usenet.
func (s *Server) excludeStreamedFromIntroSkipper(ctx context.Context, srv mediaservers.Server, host string) bool {
	var plugins []struct {
		Name string `json:"Name"`
		ID   string `json:"Id"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/Plugins", nil, &plugins); err != nil {
		return false
	}
	for _, p := range plugins {
		if !strings.Contains(strings.ToLower(strings.ReplaceAll(p.Name, " ", "")), "introskipper") {
			continue
		}
		var cfg map[string]any
		if err := jfJSON(ctx, srv, http.MethodGet, "/Plugins/"+p.ID+"/Configuration", nil, &cfg); err != nil {
			return false
		}
		list, _ := cfg["PathExclusions"].([]any)
		for _, v := range list {
			if str, _ := v.(string); strings.EqualFold(strings.TrimRight(str, `\/`), host) {
				return true
			}
		}
		cfg["PathExclusions"] = append(list, host)
		return jfJSON(ctx, srv, http.MethodPost, "/Plugins/"+p.ID+"/Configuration", cfg, nil) == nil
	}
	return false
}

func (s *Server) registerStreamedAppRoutes(play, admin routeGroup) {
	play.HandleFunc("GET /api/streamed", revalidated(s.handleStreamedList))
	play.HandleFunc("GET /api/streamed/lookup", s.handleStreamedLookup)
	play.HandleFunc("GET /api/streamed/{id}", revalidated(s.handleStreamedTitle))
	play.HandleFunc("POST /api/streamed/files/{id}/watch", s.handleStreamedWatch)
	play.HandleFunc("POST /api/streamed/files/{id}/ready", s.handleStreamedReady)
	admin.HandleFunc("GET /api/streamed/import", s.handleStreamedImportStatus)
	admin.HandleFunc("POST /api/streamed/import", s.handleStreamedImport)
	admin.HandleFunc("POST /api/streamed/jellyfin", s.handleStreamedJellyfin)
	admin.HandleFunc("POST /api/streamed/merge", s.handleStreamedMerge)
	play.HandleFunc("GET /api/home/anime-series", revalidated(s.handleAnimeSeries))
}

// GET /api/streamed/lookup?kind=movie|series&tmdbId=&season=&episode=: the
// (Without season, a show's id in the streamed library.)
// saved release for a movie or episode, when it's in the streamed library
// (so Stream now can offer it before searching).
func (s *Server) handleStreamedLookup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tmdb, _ := strconv.Atoi(q.Get("tmdbId"))
	season, _ := strconv.Atoi(q.Get("season"))
	episode, _ := strconv.Atoi(q.Get("episode"))
	kind := q.Get("kind")
	if (kind != "movie" && kind != "series") || tmdb <= 0 {
		writeError(w, http.StatusBadRequest, "Say which movie or episode.")
		return
	}
	// Usherr: a show without an episode: just its id in the streamed library,
	// so its page can open the episode list.
	if kind == "series" && q.Get("season") == "" {
		var titleID int64
		if err := s.db.QueryRowContext(r.Context(), `SELECT id FROM streamed_titles WHERE kind = 'series' AND tmdb_id = ?`, tmdb).Scan(&titleID); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"found": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"found": true, "titleId": titleID})
		return
	}
	var id int64
	var release, health string
	err := s.db.QueryRowContext(r.Context(), `SELECT f.id, f.release, f.health FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id
		WHERE t.kind = ? AND t.tmdb_id = ? AND f.season = ? AND f.episode = ?`, kind, tmdb, season, episode).Scan(&id, &release, &health)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"found": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": true, "fileId": id, "release": release, "health": health})
}

// POST /api/streamed/files/{id}/ready: get a file ready to play (the page
// asks when a title is opened, as Stremio does), so Play starts at once.
func (s *Server) handleStreamedReady(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a file.")
		return
	}
	quality := r.URL.Query().Get("quality")
	s.background(func() {
		if _, err := s.streamedSourceFor(context.Background(), id, quality, false); err != nil {
			slog.Info("streamed: get ready", "file", id, "err", err)
		}
	})
	writeJSON(w, http.StatusAccepted, nil)
}

var streamedNextReady sync.Map // file id -> when its next episode was got ready

// readyNextEpisode gets the episode after this one ready in the background,
// so watching a show straight through never waits.
func (s *Server) readyNextEpisode(id int64) {
	if v, ok := streamedNextReady.Load(id); ok && time.Since(v.(time.Time)) < time.Hour {
		return
	}
	streamedNextReady.Store(id, time.Now())
	s.background(func() {
		var next int64
		err := s.db.QueryRow(`SELECT n.id FROM streamed_files f JOIN streamed_files n ON n.title_id = f.title_id
			WHERE f.id = ? AND f.season > 0 AND (n.season > f.season OR (n.season = f.season AND n.episode > f.episode))
			ORDER BY n.season, n.episode LIMIT 1`, id).Scan(&next)
		if err != nil {
			return
		}
		if _, err := s.streamedSource(context.Background(), next); err != nil {
			slog.Info("streamed: get next episode ready", "file", next, "err", err)
		}
	})
}
