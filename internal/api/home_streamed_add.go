package api

// Home fork: "Add as stream". A movie or show goes into the streamed library
// straight from TMDB, with no download and no saved release: it plays from
// StreamNZB's search, like the rest of the streamed library, and shows in
// the Library and the media server like any other title. Shows get their
// aired episodes, and new ones as they air.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (s *Server) registerStreamedAddRoutes(play, admin routeGroup) {
	play.HandleFunc("GET /api/streamed/by-tmdb", s.handleStreamedByTMDB)
	admin.HandleFunc("POST /api/streamed/add", s.handleStreamedAdd)
	admin.HandleFunc("DELETE /api/streamed/titles/{id}", s.handleStreamedRemove)
}

// GET /api/streamed/by-tmdb?kind=movie|series&tmdbId=: the streamed title's
// id, or 0 when it isn't in the streamed library.
func (s *Server) handleStreamedByTMDB(w http.ResponseWriter, r *http.Request) {
	kind := streamedKind(r.URL.Query().Get("kind"))
	tmdb, _ := strconv.Atoi(r.URL.Query().Get("tmdbId"))
	var id int64
	_ = s.db.QueryRowContext(r.Context(), `SELECT id FROM streamed_titles WHERE kind = ? AND tmdb_id = ?`, kind, tmdb).Scan(&id)
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// streamedKind accepts the page's "tv" for a show.
func streamedKind(k string) string {
	if k == "tv" {
		return "series"
	}
	return k
}

// POST /api/streamed/add {kind, tmdbId}
func (s *Server) handleStreamedAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind   string `json:"kind"`
		TMDBID int    `json:"tmdbId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	req.Kind = streamedKind(req.Kind)
	if (req.Kind != "movie" && req.Kind != "series") || req.TMDBID <= 0 {
		writeError(w, http.StatusBadRequest, "Say which movie or show.")
		return
	}
	if s.TMDB() == nil || !s.TMDB().HasAPIKey() {
		writeError(w, http.StatusPreconditionFailed, "Mediarium needs its TMDB key for this (Settings > Info, lists and subtitles).")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
	defer cancel()
	id, n, err := s.addStreamedTitle(ctx, req.Kind, req.TMDBID, "")
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	go s.afterStreamedChange()
	msg := "Added to your streams."
	if req.Kind == "series" {
		msg = "Added to your streams with " + strconv.Itoa(n) + " episodes. New ones are added as they air."
		if n == 0 {
			msg = "Already in your streams, with every episode that has aired."
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "files": n, "message": msg})
}

// addStreamedTitle adds a title (or, for a show already there, the episodes
// it doesn't have yet; with after set, only those that aired after that
// date). It returns the title's id and how many files it added.
func (s *Server) addStreamedTitle(ctx context.Context, kind string, tmdb int, after string) (int64, int, error) {
	t := streamedImportTitle{Kind: kind, TMDBID: tmdb}
	if kind == "movie" {
		t.Files = []streamedImportFile{{}}
	} else {
		files, anime, err := s.airedEpisodes(ctx, tmdb, after)
		if err != nil {
			return 0, 0, err
		}
		if after != "" && len(files) == 0 {
			return 0, 0, nil // nothing new
		}
		if len(files) == 0 {
			return 0, 0, errors.New("no episode of this show has aired yet")
		}
		t.Files, t.Anime = files, anime
	}
	// Only what isn't there yet: files already in the streamed library keep
	// their saved release.
	var id int64
	_ = s.db.QueryRowContext(ctx, `SELECT id FROM streamed_titles WHERE kind = ? AND tmdb_id = ?`, kind, tmdb).Scan(&id)
	if id > 0 {
		have := map[[2]int]bool{}
		if rows, err := s.db.QueryContext(ctx, `SELECT season, episode FROM streamed_files WHERE title_id = ?`, id); err == nil {
			for rows.Next() {
				var k [2]int
				if rows.Scan(&k[0], &k[1]) == nil {
					have[k] = true
				}
			}
			rows.Close()
		}
		var keep []streamedImportFile
		for _, f := range t.Files {
			if !have[[2]int{f.Season, f.Episode}] {
				keep = append(keep, f)
			}
		}
		if len(keep) == 0 {
			return id, 0, nil
		}
		t.Files = keep
		var anime int
		_ = s.db.QueryRowContext(ctx, `SELECT anime FROM streamed_titles WHERE id = ?`, id).Scan(&anime)
		t.Anime = t.Anime || anime == 1 // keep where it's filed
	}
	n, err := s.importStreamedTitle(ctx, t)
	if err != nil {
		return id, n, err
	}
	_ = s.db.QueryRowContext(ctx, `SELECT id FROM streamed_titles WHERE kind = ? AND tmdb_id = ?`, kind, tmdb).Scan(&id)
	return id, n, nil
}

// airedEpisodes lists a show's episodes that have aired (after the date
// given, if any; specials left out), and whether it's anime (Japanese
// animation, filed with anime).
func (s *Server) airedEpisodes(ctx context.Context, tmdb int, after string) ([]streamedImportFile, bool, error) {
	tm := s.TMDB()
	if tm == nil {
		return nil, false, errors.New("Mediarium needs its TMDB key for this")
	}
	d, err := tm.GetShowFull(ctx, tmdb)
	if err != nil {
		return nil, false, err
	}
	anime := false
	if d.OriginalLanguage == "ja" {
		for _, g := range d.Genres {
			anime = anime || g.Name == "Animation"
		}
	}
	today := time.Now().Format("2006-01-02")
	var files []streamedImportFile
	for _, se := range d.Seasons {
		if se.SeasonNumber <= 0 || se.EpisodeCount == 0 || se.AirDate == "" || se.AirDate > today {
			continue
		}
		eps, err := tm.GetSeason(ctx, tmdb, se.SeasonNumber)
		if err != nil {
			return nil, false, err
		}
		for _, e := range eps {
			if e.AirDate != "" && e.AirDate <= today && e.AirDate >= after {
				files = append(files, streamedImportFile{Season: e.Season, Episode: e.Episode})
			}
		}
	}
	return files, anime, nil
}

// afterStreamedChange writes the pointer files (only for what isn't on
// disk) and has the media server look.
func (s *Server) afterStreamedChange() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if _, _, err := s.syncStreamedPointers(ctx); err != nil && !errors.Is(err, errNoAddress) {
		slog.Info("streamed: pointers after a change", "err", err)
	}
	if srv, ok := s.jellyfinServer(); ok {
		_ = jfJSON(ctx, srv, http.MethodPost, "/Library/Refresh", nil, nil)
	}
}

// DELETE /api/streamed/titles/{id}: take a title out of the streamed
// library. Its pointer files go; files on disk are never touched, and a
// saved NZB stays in the config folder.
func (s *Server) handleStreamedRemove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a title.")
		return
	}
	found, err := s.removeStreamedTitle(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "That title isn't in your streams.")
		return
	}
	go s.afterStreamedChange()
	writeJSON(w, http.StatusOK, map[string]string{"message": "Removed from your streams."})
}

// removeStreamedTitle takes a title out of the streamed library: its pointer
// files (only ever our own .strm files) and its rows. It reports whether the
// title was there.
func (s *Server) removeStreamedTitle(ctx context.Context, id int64) (bool, error) {
	dir := s.streamedSetting(keyStreamedDir, defaultStreamedDir)
	rows, err := s.db.QueryContext(ctx, `SELECT strm_path FROM streamed_files WHERE title_id = ? AND strm_path != ''`, id)
	if err != nil {
		return false, err
	}
	var pointers []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			pointers = append(pointers, p)
		}
	}
	rows.Close()
	for _, p := range pointers {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if !strings.HasSuffix(full, ".strm") || !strings.HasPrefix(filepath.Clean(full), filepath.Clean(dir)) {
			continue // only ever our own pointer files
		}
		_ = os.Remove(full)
		removeEmptyParents(dir, full)
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM streamed_files WHERE title_id = ?`, id)
	res, err := s.db.ExecContext(ctx, `DELETE FROM streamed_titles WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// streamedEpisodesJob adds newly aired episodes of the streamed shows
// still running, once a day: only episodes that aired after the newest one
// a show has (or after it was added), never the older ones it was
// collected without.
func (s *Server) streamedEpisodesJob(ctx context.Context) {
	if s.TMDB() == nil || !s.TMDB().HasAPIKey() {
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.tmdb_id, MAX(COALESCE(MAX(f.air_date), ''), substr(t.added_at, 1, 10))
		FROM streamed_titles t LEFT JOIN streamed_files f ON f.title_id = t.id WHERE t.kind = 'series' GROUP BY t.id`)
	if err != nil {
		return
	}
	type show struct {
		tmdb  int
		after string
	}
	var shows []show
	for rows.Next() {
		var sh show
		if rows.Scan(&sh.tmdb, &sh.after) == nil && sh.after != "" {
			shows = append(shows, sh)
		}
	}
	rows.Close()
	added := 0
	for _, sh := range shows {
		tmdb := sh.tmdb
		if ctx.Err() != nil {
			return
		}
		lctx, cancel := context.WithTimeout(ctx, time.Minute)
		if d, err := s.TMDB().GetShowFull(lctx, tmdb); err == nil && (d.Status == "Ended" || d.Status == "Canceled") {
			cancel()
			continue
		}
		_, n, err := s.addStreamedTitle(lctx, "series", tmdb, sh.after)
		cancel()
		if err == nil {
			added += n
		}
	}
	if added > 0 {
		slog.Info("streamed: new episodes", "added", added)
		s.afterStreamedChange()
	}
}
