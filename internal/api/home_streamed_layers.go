package api

// Home fork: one library, in layers. A movie or episode is played from the
// best place it can be: the file on disk, else a stream (StreamNZB). So a
// streamed title only gets a pointer file (and so only shows in Jellyfin)
// while nothing better is on disk; download it and the pointer goes, and
// Jellyfin switches to the file. A show partly on disk keeps its streamed
// episodes in a folder named like the one on disk, so Jellyfin shows one
// show, not two.

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

var streamedSyncMu sync.Mutex

// streamedSyncJob keeps the pointer files in step with what's on disk.
func (s *Server) streamedSyncJob(ctx context.Context) {
	if n, removed, err := s.syncStreamedPointers(ctx); err != nil {
		slog.Info("streamed: sync pointers", "err", err)
	} else if removed > 0 || n > 0 {
		slog.Info("streamed: pointers in step with the disk", "written", n, "removed", removed)
	}
}

// diskShowFolder is the folder a show has on disk ("Bluey (2018)") and
// whether it is under the anime folder, from its downloaded episodes.
func (s *Server) diskShowFolder(ctx context.Context, tmdb int) (folder string, anime bool) {
	var p sql.NullString
	_ = s.db.QueryRowContext(ctx, `SELECT e.file_path FROM episodes e JOIN series s ON s.id = e.series_id
		WHERE s.tmdb_id = ? AND e.file_path != '' LIMIT 1`, tmdb).Scan(&p)
	fp := filepath.ToSlash(p.String)
	for _, root := range []string{"/data/tv/", "/data/anime/"} {
		if rest, ok := strings.CutPrefix(fp, root); ok {
			if i := strings.Index(rest, "/"); i > 0 {
				return rest[:i], root == "/data/anime/"
			}
		}
	}
	return "", false
}

// syncStreamedPointers writes a pointer for every streamed file that has
// nothing on disk, and removes the ones that do. It returns how many it
// wrote and removed.
func (s *Server) syncStreamedPointers(ctx context.Context) (written, removed int, err error) {
	if !streamedSyncMu.TryLock() {
		return 0, 0, nil
	}
	defer streamedSyncMu.Unlock()
	type row struct {
		id              int64
		kind            string
		tmdb, year      int
		title           string
		anime           bool
		season, episode int
		strm            string
	}
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, t.kind, t.tmdb_id, t.year, t.title, t.anime, f.season, f.episode, f.strm_path
		FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id`)
	if err != nil {
		return 0, 0, err
	}
	var list []row
	for rows.Next() {
		var r row
		var anime int
		if err := rows.Scan(&r.id, &r.kind, &r.tmdb, &r.year, &r.title, &anime, &r.season, &r.episode, &r.strm); err == nil {
			r.anime = anime == 1
			list = append(list, r)
		}
	}
	rows.Close()

	// What's on disk, in one go each.
	movieOnDisk := map[int]bool{}
	if mr, err := s.db.QueryContext(ctx, `SELECT tmdb_id FROM movies WHERE status = 'downloaded'`); err == nil {
		for mr.Next() {
			var t int
			if mr.Scan(&t) == nil {
				movieOnDisk[t] = true
			}
		}
		mr.Close()
	}
	epOnDisk := map[string]bool{}
	if er, err := s.db.QueryContext(ctx, `SELECT s.tmdb_id, e.season, e.episode FROM episodes e JOIN series s ON s.id = e.series_id WHERE e.status = 'downloaded'`); err == nil {
		for er.Next() {
			var t, se, ep int
			if er.Scan(&t, &se, &ep) == nil {
				epOnDisk[fmt.Sprintf("%d:%d:%d", t, se, ep)] = true
			}
		}
		er.Close()
	}
	folders := map[int]struct {
		name  string
		anime bool
	}{}
	dir := s.streamedSetting(keyStreamedDir, defaultStreamedDir)
	for _, r := range list {
		if ctx.Err() != nil {
			return written, removed, ctx.Err()
		}
		onDisk := false
		want := ""
		if r.kind == "movie" {
			onDisk = movieOnDisk[r.tmdb]
			want = streamedPointerPath("movie", false, r.title, r.year, r.tmdb, 0, 0)
		} else {
			onDisk = epOnDisk[fmt.Sprintf("%d:%d:%d", r.tmdb, r.season, r.episode)]
			f, seen := folders[r.tmdb]
			if !seen {
				f.name, f.anime = s.diskShowFolder(ctx, r.tmdb)
				folders[r.tmdb] = f
			}
			want = streamedPointerPath("series", r.anime || f.anime, r.title, r.year, r.tmdb, r.season, r.episode)
			if f.name != "" {
				// Beside the show's own folder name, so Jellyfin makes one show.
				root := "TV"
				if r.anime || f.anime {
					root = "Anime"
				}
				want = path.Join(root, cleanName(f.name), fmt.Sprintf("Season %02d", r.season), path.Base(want))
			}
		}
		if onDisk {
			if r.strm != "" {
				if os.Remove(filepath.Join(dir, filepath.FromSlash(r.strm))) == nil {
					removed++
				}
				removeEmptyParents(dir, filepath.Join(dir, filepath.FromSlash(r.strm)))
				_, _ = s.db.ExecContext(ctx, `UPDATE streamed_files SET strm_path = '' WHERE id = ?`, r.id)
			}
			continue
		}
		if r.strm == want {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(want))); err == nil {
				continue
			}
		} else if r.strm != "" {
			_ = os.Remove(filepath.Join(dir, filepath.FromSlash(r.strm)))
			removeEmptyParents(dir, filepath.Join(dir, filepath.FromSlash(r.strm)))
		}
		if err := s.writePointer(want, r.id); err != nil {
			return written, removed, err
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE streamed_files SET strm_path = ? WHERE id = ?`, want, r.id)
		written++
	}
	return written, removed, nil
}

// removeEmptyParents tidies folders a removed pointer leaves empty, up to
// (not including) the streamed folder itself.
func removeEmptyParents(root, file string) {
	root = filepath.Clean(root)
	for d := filepath.Dir(file); d != root && strings.HasPrefix(d, root); d = filepath.Dir(d) {
		if os.Remove(d) != nil { // not empty (or gone): stop
			return
		}
	}
}

// GET /api/home/anime-series: the library shows that are anime (their files
// are in the anime folder, or they're set to anime numbering), for the
// Library's Anime tab.
func (s *Server) handleAnimeSeries(w http.ResponseWriter, r *http.Request) {
	// Usherr: the anime folder set in Settings (or ANIME_DIR); /data/anime before there was one.
	root := s.animeRoot()
	if root == "" {
		root = "/data/anime"
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id FROM series WHERE series_type = 'anime' OR root_path = ?
		UNION SELECT DISTINCT series_id FROM episodes WHERE file_path LIKE ? || '/%'`, root, root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	writeJSON(w, http.StatusOK, ids)
}

// mergeStreamedIntoLibraries puts the streamed folders into Jellyfin's own
// Movies, TV and Anime libraries (found by the disk folders they hold) and
// removes the separate Streamed libraries. It says what it did.
func (s *Server) mergeStreamedIntoLibraries(ctx context.Context, srv mediaservers.Server, host string) ([]string, error) {
	var folders []struct {
		Name      string   `json:"Name"`
		ItemID    string   `json:"ItemId"`
		Locations []string `json:"Locations"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, "/Library/VirtualFolders", nil, &folders); err != nil {
		return nil, err
	}
	has := func(locs []string, p string) bool {
		for _, l := range locs {
			if strings.EqualFold(strings.TrimRight(l, `\/`), strings.TrimRight(p, `\/`)) {
				return true
			}
		}
		return false
	}
	var done []string
	// The libraries that hold the folders on disk, as the media server
	// sees them (through the folder mapping of Settings > Media servers).
	for _, m := range []struct{ sub, disk string }{
		{"Movies", s.moviesRoot()}, {"TV", s.tvRoot()}, {"Anime", animeRoot},
	} {
		m.disk = mediaservers.MapPath(m.disk, srv.PathMap)
		streamed := joinHostPath(host, m.sub)
		for _, f := range folders {
			if !has(f.Locations, m.disk) || has(f.Locations, streamed) {
				continue
			}
			// Emby finds the library by Id; Jellyfin by Name.
			body := map[string]any{"Name": f.Name, "Id": f.ItemID, "PathInfo": map[string]string{"Path": streamed}}
			if err := jfJSON(ctx, srv, http.MethodPost, "/Library/VirtualFolders/Paths?refreshLibrary=false", body, nil); err != nil {
				return done, fmt.Errorf("add %s to %s: %w", streamed, f.Name, err)
			}
			done = append(done, fmt.Sprintf("%s now includes streamed titles", f.Name))
		}
	}
	for _, f := range folders {
		if strings.HasPrefix(f.Name, "Streamed ") {
			if err := jfJSON(ctx, srv, http.MethodDelete, "/Library/VirtualFolders?name="+url.QueryEscape(f.Name)+"&refreshLibrary=false", nil, nil); err == nil {
				done = append(done, "removed the separate "+f.Name+" library")
			}
		}
	}
	_ = jfJSON(ctx, srv, http.MethodPost, "/Library/Refresh", nil, nil)
	return done, nil
}

// POST /api/streamed/merge: one library in Jellyfin, and pointers only for
// what isn't on disk.
func (s *Server) handleStreamedMerge(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
	defer cancel()
	written, removed, err := s.syncStreamedPointers(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := map[string]any{"written": written, "removed": removed}
	if srv, ok := s.jellyfinServer(); ok {
		if host := s.streamedHostDir(srv); host == "" {
			out["jellyfinError"] = "Say where " + serverLabel(srv) + " sees the streamed folder, under Settings > Extras."
		} else {
			done, err := s.mergeStreamedIntoLibraries(ctx, srv, host)
			out["jellyfin"] = done
			if err != nil {
				out["jellyfinError"] = err.Error()
			}
			out["introSkipperExcluded"] = s.excludeStreamedFromIntroSkipper(ctx, srv, host)
		}
	}
	writeJSON(w, http.StatusOK, out)
}
