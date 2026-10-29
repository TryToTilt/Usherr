package api

// Usherr: coming-soon trailers for the pre-show, downloaded ahead (after
// NeXroll's NeX-Up). The titles on their way, a wanted film, one coming to
// cinemas, a show's new season, are looked at every few hours; when one is
// within the window (60 days before its release by default) its trailer is
// fetched from YouTube with yt-dlp into the trailers folder, so it plays at
// once, and in the Jellyfin app too. It goes again on a schedule: some days
// after the release (14), or when the film is in the library; a size cap
// on top. Kept ones stay.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	keyTrailersOn      = "home.trailers.on"      // "0": none downloaded (on by default)
	keyTrailersFolder  = "home.trailers.folder"  // where they're kept
	keyTrailersDays    = "home.trailers.days"    // fetched this many days before the release (60)
	keyTrailersAfter   = "home.trailers.after"   // kept this many days after it (14)
	keyTrailersCapGB   = "home.trailers.capGB"   // at most this many GB (10)
	keyTrailersQuality = "home.trailers.quality" // 720, 1080, 1440 or 2160 (2160: the best each trailer has)
	keyTrailersCard    = "home.trailers.card"    // "0": no coming-soon card before each trailer (on by default)

	trailersPerRun = 6 // downloads at a time, gentle on YouTube

	// The cinemas' films taken, the most talked about first: the household's
	// own (wanted films, new seasons) always come.
	trailersCinemaComing = 12
	trailersCinemaNow    = 4
)

type trailerSettings struct {
	On      bool   `json:"on"`
	Folder  string `json:"folder"`
	Days    int    `json:"days"`
	After   int    `json:"after"`
	CapGB   int    `json:"capGB"`
	Quality int    `json:"quality"`
	Card    bool   `json:"card"`
}

func (s *Server) trailerSettingsNow() trailerSettings {
	get := func(k string) string { v, _ := s.Settings.Get(k); return strings.TrimSpace(v) }
	num := func(k string, def, lo, hi int) int {
		n, err := strconv.Atoi(get(k))
		if err != nil || n < lo || n > hi {
			return def
		}
		return n
	}
	q := num(keyTrailersQuality, 2160, 720, 2160)
	if q != 720 && q != 1080 && q != 1440 {
		q = 2160
	}
	return trailerSettings{On: get(keyTrailersOn) != "0", Folder: s.trailersDir(), Days: num(keyTrailersDays, 60, 7, 365),
		After: num(keyTrailersAfter, 14, 0, 365), CapGB: num(keyTrailersCapGB, 10, 1, 2000), Quality: q, Card: get(keyTrailersCard) != "0"}
}

// trailersDir is the folder set, else /data/trailers when it's mounted, else
// trailers in the config folder.
func (s *Server) trailersDir() string {
	if v, _ := s.Settings.Get(keyTrailersFolder); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if st, err := os.Stat("/data/trailers"); err == nil && st.IsDir() {
		return "/data/trailers"
	}
	return filepath.Join(s.cfg.ConfigDir, "trailers")
}

type preshowTrailer struct {
	ID          int64   `json:"id"`
	Kind        string  `json:"kind"`
	TMDBID      int     `json:"tmdbId"`
	Season      int     `json:"season,omitempty"`
	Title       string  `json:"title"`
	ReleaseDate string  `json:"releaseDate"`
	Why         string  `json:"why"`
	PosterURL   string  `json:"posterUrl,omitempty"`
	YouTube     string  `json:"youtube,omitempty"`
	File        string  `json:"-"`
	Size        int64   `json:"size"`
	Seconds     float64 `json:"seconds"`
	State       string  `json:"state"`
	Error       string  `json:"error,omitempty"`
	Keep        bool    `json:"keep"`
	AddedMs     int64   `json:"addedMs"`
	FetchedMs   int64   `json:"fetchedMs,omitempty"`
	FileURL     string  `json:"fileUrl,omitempty"`
}

// item is its key in preshow_played (shared with YouTube's, so they take turns).
func (t preshowTrailer) item() string {
	if t.Kind == "tv" {
		return "trailer:tv" + strconv.Itoa(t.TMDBID) + "s" + strconv.Itoa(t.Season)
	}
	return "trailer:" + strconv.Itoa(t.TMDBID)
}

const trailerCols = `id, kind, tmdb_id, season, title, release_date, why, poster_url, youtube, file, size, seconds, state, error, keep, added_ms, fetched_ms`

func scanTrailer(sc interface{ Scan(...any) error }) (preshowTrailer, error) {
	var t preshowTrailer
	err := sc.Scan(&t.ID, &t.Kind, &t.TMDBID, &t.Season, &t.Title, &t.ReleaseDate, &t.Why, &t.PosterURL, &t.YouTube, &t.File, &t.Size, &t.Seconds, &t.State, &t.Error, &t.Keep, &t.AddedMs, &t.FetchedMs)
	if t.State == "ready" {
		t.FileURL = "/api/home/preshow/trailers/" + strconv.FormatInt(t.ID, 10) + "/file"
	}
	return t, err
}

func (s *Server) preshowTrailers(ctx context.Context, where string, args ...any) ([]preshowTrailer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+trailerCols+` FROM preshow_trailers `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []preshowTrailer{}
	for rows.Next() {
		if t, err := scanTrailer(rows); err == nil {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

// readyTrailers are the downloaded ones that are still coming (or only just out).
func (s *Server) readyTrailers(ctx context.Context, now time.Time) []preshowTrailer {
	set := s.trailerSettingsNow()
	list, _ := s.preshowTrailers(ctx, `WHERE state = 'ready' AND (keep = 1 OR release_date >= ?) ORDER BY release_date`, now.AddDate(0, 0, -set.After).Format("2006-01-02"))
	dir := s.trailersDir()
	out := list[:0]
	for _, t := range list {
		if _, err := os.Stat(filepath.Join(dir, t.File)); err == nil {
			out = append(out, t)
		}
	}
	return out
}

// ---- what's coming ----

type trailerWanted struct {
	kind, title, release, why, poster string
	tmdbID, season                    int
}

// trailerCandidates are the titles whose release is within the window: the
// library's wanted films, the cinemas' coming and new ones, and the
// monitored shows' new seasons.
func (s *Server) trailerCandidates(ctx context.Context, set trailerSettings, now time.Time) []trailerWanted {
	from, to := now.AddDate(0, 0, -set.After).Format("2006-01-02"), now.AddDate(0, 0, set.Days).Format("2006-01-02")
	var out []trailerWanted
	seen := map[string]bool{}
	add := func(w trailerWanted) bool {
		k := w.kind + strconv.Itoa(w.tmdbID) + "s" + strconv.Itoa(w.season)
		if w.tmdbID <= 0 || w.release < from || w.release > to || seen[k] {
			return false
		}
		seen[k] = true
		out = append(out, w)
		return true
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT tmdb_id, title, COALESCE(release_date, ''), COALESCE(poster_path, '') FROM movies
		WHERE monitored = 1 AND copy_of IS NULL AND status != 'downloaded' AND release_date BETWEEN ? AND ?`, from, to); err == nil {
		for rows.Next() {
			var w trailerWanted
			var poster string
			if rows.Scan(&w.tmdbID, &w.title, &w.release, &poster) == nil {
				w.kind, w.why, w.poster = "movie", "wanted", metadata.ImageURL("w500", poster)
				add(w)
			}
		}
		rows.Close()
	}
	if s.TMDB().HasAPIKey() {
		nowShowing, upcoming := s.cinemaFilms(ctx)
		for list, most := range map[*[]metadata.Movie]int{&upcoming: trailersCinemaComing, &nowShowing: trailersCinemaNow} {
			films := append([]metadata.Movie(nil), (*list)...)
			sort.SliceStable(films, func(i, j int) bool { return films[i].Popularity > films[j].Popularity })
			taken := 0
			for _, m := range films {
				if taken >= most {
					break
				}
				if add(trailerWanted{kind: "movie", tmdbID: m.TMDBID, title: m.Title, release: m.ReleaseDate, why: "cinema", poster: metadata.ImageURL("w500", m.PosterPath)}) {
					taken++
				}
			}
		}
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT s.tmdb_id, s.title, COALESCE(s.poster_path, ''), e.season, e.air_date FROM episodes e JOIN series s ON s.id = e.series_id
		WHERE e.episode = 1 AND s.monitored = 1 AND s.copy_of IS NULL AND e.air_date BETWEEN ? AND ?`, from, to); err == nil {
		for rows.Next() {
			var w trailerWanted
			var poster string
			if rows.Scan(&w.tmdbID, &w.title, &poster, &w.season, &w.release) == nil {
				w.kind, w.why, w.poster = "tv", "season", metadata.ImageURL("w500", poster)
				add(w)
			}
		}
		rows.Close()
	}
	return out
}

// trailerYouTube finds the title's trailer: the film's, or the show's for
// that season (by its name), else the show's newest.
func (s *Server) trailerYouTube(ctx context.Context, t preshowTrailer) string {
	if t.Kind == "movie" {
		if ft, ok := s.marqueeFeature(ctx, t.TMDBID, "coming"); ok {
			return ft.Trailer
		}
		return ""
	}
	show, err := s.TMDB().GetShowFull(ctx, t.TMDBID)
	if err != nil {
		return ""
	}
	list := show.Trailers()
	n := strconv.Itoa(t.Season)
	for _, tr := range list {
		l := strings.ToLower(tr.Name)
		if strings.Contains(l, "season "+n) || strings.Contains(l, "s"+n+" ") || strings.Contains(l, "part "+n) {
			return tr.Key
		}
	}
	if len(list) > 0 {
		return list[0].Key
	}
	return ""
}

// ---- the job ----

var trailersBusy sync.Mutex

// trailersJob brings the trailers in line: new titles noted, the ones past
// their time removed, the waiting ones downloaded (a few at a time), and the
// cap kept.
func (s *Server) trailersJob(ctx context.Context) {
	if !trailersBusy.TryLock() {
		return
	}
	defer trailersBusy.Unlock()
	set := s.trailerSettingsNow()
	if !set.On {
		return
	}
	now := time.Now()
	nowMs := now.UnixMilli()
	wanted := s.trailerCandidates(ctx, set, now)
	picked := map[string]bool{}
	for _, w := range wanted {
		picked[w.kind+strconv.Itoa(w.tmdbID)+"s"+strconv.Itoa(w.season)] = true
	}
	// Titles no longer picked (a cinema film fallen off the list) aren't fetched.
	if len(wanted) > 0 {
		if stale, err := s.preshowTrailers(ctx, `WHERE state IN ('waiting', 'failed') AND keep = 0`); err == nil {
			for _, t := range stale {
				if !picked[t.Kind+strconv.Itoa(t.TMDBID)+"s"+strconv.Itoa(t.Season)] {
					_, _ = s.db.ExecContext(ctx, `DELETE FROM preshow_trailers WHERE id = ?`, t.ID)
				}
			}
		}
	}
	for _, w := range wanted {
		_, _ = s.db.ExecContext(ctx, `INSERT INTO preshow_trailers (kind, tmdb_id, season, title, release_date, why, poster_url, added_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (kind, tmdb_id, season) DO UPDATE SET title = excluded.title, release_date = excluded.release_date, poster_url = excluded.poster_url`,
			w.kind, w.tmdbID, w.season, trimRunes(w.title, 200), w.release, w.why, w.poster, nowMs)
	}
	s.trailersTidy(ctx, set, now)
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return
	}
	// Still to come first, the household's own before the cinemas', soonest first.
	waiting, _ := s.preshowTrailers(ctx, `WHERE state = 'waiting' OR (state = 'failed' AND tries < 3 AND fetched_ms < ?)
		ORDER BY release_date < ?, why = 'cinema', release_date LIMIT ?`, nowMs-int64(12*time.Hour/time.Millisecond), now.Format("2006-01-02"), trailersPerRun)
	for _, t := range waiting {
		if ctx.Err() != nil {
			return
		}
		s.trailerFetch(ctx, set, t)
	}
	s.trailersTidy(ctx, set, now)
}

// trailersTidy removes what's past its time (not kept): some days after the
// release, or a film now in the library; then the oldest releases until the
// folder is under the cap.
func (s *Server) trailersTidy(ctx context.Context, set trailerSettings, now time.Time) {
	past := now.AddDate(0, 0, -set.After).Format("2006-01-02")
	gone, _ := s.preshowTrailers(ctx, `WHERE keep = 0 AND (release_date < ? OR (kind = 'movie' AND tmdb_id IN (SELECT tmdb_id FROM movies WHERE status = 'downloaded')))`, past)
	for _, t := range gone {
		s.trailerDrop(ctx, t, true)
	}
	ready, _ := s.preshowTrailers(ctx, `WHERE state = 'ready' ORDER BY release_date`)
	var total int64
	for _, t := range ready {
		total += t.Size
	}
	limit := int64(set.CapGB) << 30
	for _, t := range ready {
		if total <= limit {
			break
		}
		if t.Keep {
			continue
		}
		s.trailerDrop(ctx, t, false)
		total -= t.Size
	}
}

// trailerDrop deletes the trailer's file; forget also forgets the title
// (past its time), else it stays, removed, so it isn't fetched again.
func (s *Server) trailerDrop(ctx context.Context, t preshowTrailer, forget bool) {
	if t.File != "" {
		_ = os.Remove(filepath.Join(s.trailersDir(), t.File))
	}
	if forget {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM preshow_trailers WHERE id = ?`, t.ID)
	} else {
		_, _ = s.db.ExecContext(ctx, `UPDATE preshow_trailers SET state = 'removed', file = '', size = 0 WHERE id = ?`, t.ID)
	}
	slog.Info("preshow: trailer removed", "title", t.Title, "release", t.ReleaseDate)
}

// trailerFetch downloads one trailer under a hidden name, then names it: up
// to 1080p H.264 where YouTube has it (every TV plays it); above, YouTube's
// VP9 (then AV1), the best the trailer has up to the quality set. A TV that
// can't play it has Jellyfin convert it.
func (s *Server) trailerFetch(ctx context.Context, set trailerSettings, t preshowTrailer) {
	fail := func(msg string) {
		_, _ = s.db.ExecContext(ctx, `UPDATE preshow_trailers SET state = 'failed', error = ?, tries = tries + 1, fetched_ms = ? WHERE id = ?`, trimRunes(msg, 300), time.Now().UnixMilli(), t.ID)
		slog.Info("preshow: trailer not fetched", "title", t.Title, "err", msg)
	}
	key := t.YouTube
	if key == "" {
		if key = s.trailerYouTube(ctx, t); key == "" {
			fail("TMDB has no trailer for it yet.")
			return
		}
	}
	dir := s.trailersDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail("The trailers folder can't be made: " + err.Error())
		return
	}
	name := fmt.Sprintf("%s-%d", t.Kind, t.TMDBID)
	if t.Kind == "tv" {
		name += fmt.Sprintf("-s%d", t.Season)
	}
	tmp := filepath.Join(dir, ".dl-"+name)
	q := strconv.Itoa(set.Quality)
	format := "bv*[height<=" + q + "][vcodec^=avc1]+ba[acodec^=mp4a]/bv*[height<=" + q + "]+ba/b[height<=" + q + "]/b"
	if set.Quality > 1080 {
		format = "bv*[height<=" + q + "][vcodec^=vp09]+ba[acodec^=mp4a]/bv*[height<=" + q + "][vcodec^=av01]+ba[acodec^=mp4a]/bv*[height<=" + q + "]+ba/b"
	}
	run, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(run, "yt-dlp", "--no-warnings", "--no-playlist", "--no-progress", "--no-mtime", "-f", format,
		"--merge-output-format", "mp4", "-o", tmp+".%(ext)s", "https://www.youtube.com/watch?v="+key).CombinedOutput()
	got := tmp + ".mp4"
	if err != nil {
		matches, _ := filepath.Glob(tmp + ".*")
		for _, m := range matches {
			_ = os.Remove(m)
		}
		fail("YouTube: " + lastLines(string(out), 1))
		return
	}
	final := name + ".mp4"
	if err := os.Rename(got, filepath.Join(dir, final)); err != nil {
		fail("The download couldn't be saved: " + err.Error())
		return
	}
	st, _ := os.Stat(filepath.Join(dir, final))
	secs, _, _ := probeClip(ctx, filepath.Join(dir, final))
	var size int64
	if st != nil {
		size = st.Size()
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE preshow_trailers SET state = 'ready', youtube = ?, file = ?, size = ?, seconds = ?, error = '', fetched_ms = ? WHERE id = ?`,
		key, final, size, secs, time.Now().UnixMilli(), t.ID)
	slog.Info("preshow: trailer fetched", "title", t.Title, "release", t.ReleaseDate, "mb", size>>20)
}

// ---- the pages ----

// GET /api/home/preshow/trailers (admin): the trailers, the settings, the space.
func (s *Server) handlePreshowTrailers(w http.ResponseWriter, r *http.Request) {
	// Still to come first (soonest first), then the ones just out.
	list, err := s.preshowTrailers(r.Context(), `WHERE state != 'removed' ORDER BY release_date < ?, release_date, title`, time.Now().Format("2006-01-02"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The trailers couldn't be read.")
		return
	}
	var used int64
	for _, t := range list {
		if t.State == "ready" {
			used += t.Size
		}
	}
	_, ytErr := exec.LookPath("yt-dlp")
	trailersBusyNow := !trailersBusy.TryLock()
	if !trailersBusyNow {
		trailersBusy.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]any{"trailers": list, "settings": s.trailerSettingsNow(), "used": used, "ytdlp": ytErr == nil, "busy": trailersBusyNow})
}

// PUT /api/home/preshow/trailers/settings (admin)
func (s *Server) handlePutTrailerSettings(w http.ResponseWriter, r *http.Request) {
	var b trailerSettings
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if b.Days < 7 || b.Days > 365 || b.After < 0 || b.After > 365 || b.CapGB < 1 || b.CapGB > 2000 || (b.Quality != 720 && b.Quality != 1080 && b.Quality != 1440 && b.Quality != 2160) {
		writeError(w, http.StatusBadRequest, "Those numbers are out of range.")
		return
	}
	bit := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	folder := strings.TrimSpace(b.Folder)
	if folder == s.trailersDir() {
		folder, _ = s.Settings.Get(keyTrailersFolder) // unchanged: kept as it was set (or not)
	}
	for k, v := range map[string]string{keyTrailersOn: bit(b.On), keyTrailersFolder: folder, keyTrailersDays: strconv.Itoa(b.Days), keyTrailersAfter: strconv.Itoa(b.After),
		keyTrailersCapGB: strconv.Itoa(b.CapGB), keyTrailersQuality: strconv.Itoa(b.Quality), keyTrailersCard: bit(b.Card)} {
		if err := s.Settings.Set(k, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.trailerSettingsNow())
}

// POST /api/home/preshow/trailers/fetch (admin): look and download now.
func (s *Server) handleTrailersFetchNow(w http.ResponseWriter, r *http.Request) {
	go s.trailersJob(context.Background())
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) trailerByID(ctx context.Context, idText string) (preshowTrailer, bool) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		return preshowTrailer{}, false
	}
	t, err := scanTrailer(s.db.QueryRowContext(ctx, `SELECT `+trailerCols+` FROM preshow_trailers WHERE id = ?`, id))
	return t, err == nil
}

// PUT /api/home/preshow/trailers/{id} (admin): {keep}, or {again: true} to fetch it again.
func (s *Server) handlePutTrailer(w http.ResponseWriter, r *http.Request) {
	t, ok := s.trailerByID(r.Context(), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "That trailer isn't known.")
		return
	}
	var b struct {
		Keep  *bool `json:"keep"`
		Again bool  `json:"again"`
	}
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if b.Keep != nil {
		_, _ = s.db.ExecContext(r.Context(), `UPDATE preshow_trailers SET keep = ? WHERE id = ?`, *b.Keep, t.ID)
	}
	if b.Again && t.State != "ready" {
		_, _ = s.db.ExecContext(r.Context(), `UPDATE preshow_trailers SET state = 'waiting', tries = 0, error = '' WHERE id = ?`, t.ID)
		go s.trailersJob(context.Background())
	}
	t, _ = s.trailerByID(r.Context(), r.PathValue("id"))
	writeJSON(w, http.StatusOK, t)
}

// DELETE /api/home/preshow/trailers/{id} (admin): its file goes, and it isn't fetched again.
func (s *Server) handleDeleteTrailer(w http.ResponseWriter, r *http.Request) {
	t, ok := s.trailerByID(r.Context(), r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "That trailer isn't known.")
		return
	}
	s.trailerDrop(r.Context(), t, false)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/home/preshow/trailers/{id}/file: the downloaded trailer.
func (s *Server) handlePreshowTrailerFile(w http.ResponseWriter, r *http.Request) {
	t, ok := s.trailerByID(r.Context(), r.PathValue("id"))
	if !ok || t.State != "ready" || t.File == "" {
		http.NotFound(w, r)
		return
	}
	s.serveVideoFile(w, r, filepath.Join(s.trailersDir(), filepath.Base(t.File)))
}

// serveVideoFile sends a video file with ranges, or 404.
func (s *Server) serveVideoFile(w http.ResponseWriter, r *http.Request, file string) {
	f, err := os.Open(file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := preshowVideoExt[strings.ToLower(filepath.Ext(file))]
	if ct == "" {
		ct = "video/mp4"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", st.ModTime(), f)
}
