package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/music"
)

// Usherr: bringing in the .m3u and .m3u8 playlists that sit in the music
// folder, and writing a playlist back out as .m3u8. Each file becomes an
// Usherr playlist of the administrator who brought it in, its entries matched
// to the library's tracks by path, then by the end of the path, then by
// artist, title and length (internal/music/m3u_match.go). The files are only
// read, never changed. Reading them again updates the playlists made from
// them; a playlist someone deleted stays deleted unless they ask for it back.

const (
	keyPlaylistImportAuto   = "music.playlist_import_auto"   // "1": read the playlist files again each day
	keyPlaylistImportUser   = "music.playlist_import_user"   // the account the daily import works for
	keyPlaylistImportShared = "music.playlist_import_shared" // "0": new playlists are private; else shared with everyone
	keyPlaylistImportLast   = "music.playlist_import_last"   // when it last ran (RFC 3339)

	// playlistFilesMax caps how many playlist files one import reads.
	playlistFilesMax = 2000
	// unmatchedKept is how many entries that matched nothing are kept per file.
	unmatchedKept = 200
	// playlistImportEvery is how often the daily import is due.
	playlistImportEvery = 24 * time.Hour
)

type unmatchedEntry struct {
	Line  int    `json:"line"`
	Entry string `json:"entry"`
}

// playlistFileReport is what happened to one playlist file.
type playlistFileReport struct {
	File       string           `json:"file"` // relative to the music folder
	Status     string           `json:"status"`
	PlaylistID int64            `json:"playlistId,omitempty"`
	Name       string           `json:"name,omitempty"`
	Entries    int              `json:"entries"`
	Matched    int              `json:"matched"`
	Unmatched  []unmatchedEntry `json:"unmatched"`
	Error      string           `json:"error,omitempty"`
}

// Statuses of a file in the report.
const (
	plImportNew     = "new"     // a playlist was made
	plImportUpdated = "updated" // its playlist now has other tracks
	plImportSame    = "same"    // nothing changed
	plImportRemoved = "removed" // its playlist was deleted; left alone
	plImportEmpty   = "empty"   // no entry matched a track
	plImportFailed  = "failed"  // the file couldn't be read
)

type playlistImportReport struct {
	Found   int                  `json:"found"`
	New     int                  `json:"new"`
	Updated int                  `json:"updated"`
	Files   []playlistFileReport `json:"files"`
	RanAt   string               `json:"ranAt,omitempty"`
}

// findPlaylistFiles lists the .m3u and .m3u8 files under root, skipping
// hidden folders.
func findPlaylistFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil // a folder that can't be read is skipped
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if (ext == ".m3u" || ext == ".m3u8") && d.Type().IsRegular() {
			out = append(out, p)
			if len(out) >= playlistFilesMax {
				return filepath.SkipAll
			}
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}

type importRow struct {
	id         int64
	userID     int64
	playlistID sql.NullInt64
}

// importPlaylists reads every playlist file in the music folder for userID.
// again brings back playlists that were deleted after an earlier import.
func (s *Server) importPlaylists(ctx context.Context, userID int64, shared, again bool) (playlistImportReport, error) {
	s.music.importMu.Lock()
	defer s.music.importMu.Unlock()
	rep := playlistImportReport{Files: []playlistFileReport{}}
	root := s.musicRoot()
	if root == "" {
		return rep, errors.New("no music folder is set")
	}
	files, err := findPlaylistFiles(root)
	if err != nil {
		return rep, fmt.Errorf("look for playlists: %w", err)
	}
	rep.Found = len(files)
	lib, err := s.MusicRepo.LibraryTracks()
	if err != nil {
		return rep, err
	}
	matcher := music.NewM3UMatcher(lib)
	names := map[string]bool{}
	if mine, err := s.MusicRepo.ListPlaylists(userID); err == nil {
		for _, p := range mine {
			if p.UserID == userID {
				names[strings.ToLower(p.Name)] = true
			}
		}
	}
	for _, f := range files {
		if ctx.Err() != nil {
			return rep, ctx.Err()
		}
		fr := s.importPlaylistFile(root, f, userID, shared, again, matcher, names)
		switch fr.Status {
		case plImportNew:
			rep.New++
		case plImportUpdated:
			rep.Updated++
		}
		rep.Files = append(rep.Files, fr)
	}
	rep.RanAt = time.Now().UTC().Format(time.RFC3339)
	_ = s.Settings.Set(keyPlaylistImportLast, rep.RanAt, false)
	slog.Info("music: playlists brought in", "found", rep.Found, "new", rep.New, "updated", rep.Updated)
	return rep, nil
}

func (s *Server) importPlaylistFile(root, file string, userID int64, shared, again bool, matcher *music.M3UMatcher, names map[string]bool) playlistFileReport {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		rel = filepath.Base(file)
	}
	fr := playlistFileReport{File: filepath.ToSlash(rel), Unmatched: []unmatchedEntry{}}
	var row importRow
	err = s.db.QueryRow(`SELECT id, user_id, playlist_id FROM music_playlist_imports WHERE path = ?`, file).Scan(&row.id, &row.userID, &row.playlistID)
	known := err == nil
	if known && !row.playlistID.Valid && !again {
		fr.Status = plImportRemoved
		return fr
	}
	info, err := os.Stat(file)
	if err != nil {
		fr.Status, fr.Error = plImportFailed, "Couldn't read the file."
		return fr
	}
	b, err := readCapped(file, music.MaxM3UBytes)
	if err != nil {
		slog.Info("music: read playlist file", "file", fr.File, "err", err)
		fr.Status, fr.Error = plImportFailed, "Couldn't read the file."
		return fr
	}
	list := music.ParseM3U(b)
	dir := filepath.ToSlash(filepath.Dir(file))
	var ids []int64
	for _, e := range list.Entries {
		id, _ := matcher.Match(dir, e)
		if id == 0 {
			if len(fr.Unmatched) < unmatchedKept {
				fr.Unmatched = append(fr.Unmatched, unmatchedEntry{Line: e.Line, Entry: e.Display()})
			}
			continue
		}
		ids = append(ids, id)
	}
	fr.Entries, fr.Matched = len(list.Entries), len(ids)
	if len(ids) > music.MaxPlaylistTracks {
		ids = ids[:music.MaxPlaylistTracks]
	}
	unmatched, _ := json.Marshal(fr.Unmatched)
	record := func(playlistID any, owner int64) {
		_, err := s.db.Exec(`INSERT INTO music_playlist_imports (path, user_id, playlist_id, mod_time, size, entries, matched, unmatched)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET user_id = excluded.user_id, playlist_id = excluded.playlist_id, mod_time = excluded.mod_time,
				size = excluded.size, entries = excluded.entries, matched = excluded.matched, unmatched = excluded.unmatched,
				imported_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
			file, owner, playlistID, info.ModTime().Unix(), info.Size(), fr.Entries, fr.Matched, string(unmatched))
		if err != nil {
			slog.Warn("music: keep playlist import", "file", fr.File, "err", err)
		}
	}

	// A playlist made from this file before: bring its tracks up to date.
	if known && row.playlistID.Valid {
		pid := row.playlistID.Int64
		if p, err := s.MusicRepo.GetPlaylist(pid, row.userID); err == nil && p.UserID == row.userID {
			fr.PlaylistID, fr.Name = pid, p.Name
			have, _ := s.MusicRepo.PlaylistTrackIDs(pid)
			fr.Status = plImportSame
			if len(ids) > 0 && !slices.Equal(have, ids) {
				if err := s.MusicRepo.ReplacePlaylistTracks(pid, ids); err != nil {
					slog.Warn("music: update playlist from file", "file", fr.File, "err", err)
					fr.Status, fr.Error = plImportFailed, "Couldn't update its playlist."
					return fr
				}
				fr.Status = plImportUpdated
			}
			record(pid, row.userID)
			return fr
		}
	}
	if len(ids) == 0 {
		fr.Status = plImportEmpty
		record(nil, userID)
		return fr
	}
	name := list.Name
	if name == "" {
		name = s.albumPlaylistName(ids)
	}
	if name == "" {
		name = music.PlaylistNameFromFile(file)
	}
	name = freePlaylistName(name, names)
	p, err := s.MusicRepo.CreatePlaylist(userID, name, shared)
	if err != nil {
		slog.Warn("music: make playlist from file", "file", fr.File, "err", err)
		fr.Status, fr.Error = plImportFailed, "Couldn't make its playlist."
		return fr
	}
	if err := s.MusicRepo.ReplacePlaylistTracks(p.ID, ids); err != nil {
		_ = s.MusicRepo.DeletePlaylist(p.ID, userID)
		slog.Warn("music: fill playlist from file", "file", fr.File, "err", err)
		fr.Status, fr.Error = plImportFailed, "Couldn't make its playlist."
		return fr
	}
	names[strings.ToLower(p.Name)] = true
	fr.Status, fr.PlaylistID, fr.Name = plImportNew, p.ID, p.Name
	record(p.ID, userID)
	return fr
}

func readCapped(file string, max int64) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max))
}

// albumPlaylistName is "Artist - Album" when every track is from one album
// ("… (Disc 2)" when they are all on one disc of several), else "".
func (s *Server) albumPlaylistName(ids []int64) string {
	var albumID int64
	discs := map[int]bool{}
	for _, id := range ids {
		t, err := s.MusicRepo.GetTrack(id)
		if err != nil {
			return ""
		}
		if albumID != 0 && t.AlbumID != albumID {
			return ""
		}
		albumID = t.AlbumID
		discs[t.Disc] = true
	}
	album, err := s.MusicRepo.GetAlbum(albumID)
	if err != nil {
		return ""
	}
	artist, err := s.MusicRepo.GetArtist(album.ArtistID)
	if err != nil {
		return ""
	}
	name := artist.Name + " - " + album.Title
	if len(discs) == 1 {
		all, _ := s.MusicRepo.ListTracks(albumID)
		several := false
		for _, t := range all {
			if !discs[t.Disc] {
				several = true
				break
			}
		}
		if several {
			for d := range discs {
				name += fmt.Sprintf(" (Disc %d)", d)
			}
		}
	}
	return name
}

// freeName adds " (2)", " (3)"… to a name the account already uses.
func freePlaylistName(name string, used map[string]bool) string {
	clean, ok := music.CleanPlaylistName(name)
	if !ok {
		clean = "Playlist"
	}
	if !used[strings.ToLower(clean)] {
		return clean
	}
	for n := 2; ; n++ {
		c := fmt.Sprintf("%s (%d)", clean, n)
		if !used[strings.ToLower(c)] {
			return c
		}
	}
}

// lastPlaylistImport rebuilds the report of the files read before.
func (s *Server) lastPlaylistImport() (playlistImportReport, error) {
	rep := playlistImportReport{Files: []playlistFileReport{}}
	rep.RanAt, _ = s.Settings.Get(keyPlaylistImportLast)
	root := s.musicRoot()
	rows, err := s.db.Query(`SELECT i.path, i.playlist_id, COALESCE(p.name, ''), i.entries, i.matched, i.unmatched
		FROM music_playlist_imports i LEFT JOIN music_playlists p ON p.id = i.playlist_id ORDER BY i.path`)
	if err != nil {
		return rep, fmt.Errorf("read playlist imports: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var fr playlistFileReport
		var pid sql.NullInt64
		var unmatched string
		var file string
		if err := rows.Scan(&file, &pid, &fr.Name, &fr.Entries, &fr.Matched, &unmatched); err != nil {
			return rep, fmt.Errorf("read playlist imports: %w", err)
		}
		fr.File = filepath.ToSlash(file)
		if rel, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rel, "..") {
			fr.File = filepath.ToSlash(rel)
		}
		_ = json.Unmarshal([]byte(unmatched), &fr.Unmatched)
		if fr.Unmatched == nil {
			fr.Unmatched = []unmatchedEntry{}
		}
		switch {
		case pid.Valid:
			fr.Status, fr.PlaylistID = plImportSame, pid.Int64
		case fr.Matched == 0:
			fr.Status = plImportEmpty
		default:
			fr.Status = plImportRemoved
		}
		rep.Files = append(rep.Files, fr)
	}
	rep.Found = len(rep.Files)
	return rep, rows.Err()
}

type playlistImportSettings struct {
	Auto   bool `json:"auto"`
	Shared bool `json:"shared"`
}

func (s *Server) playlistImportSettings() playlistImportSettings {
	auto, _ := s.Settings.Get(keyPlaylistImportAuto)
	shared, _ := s.Settings.Get(keyPlaylistImportShared)
	return playlistImportSettings{Auto: auto == "1", Shared: shared != "0"}
}

// GET /api/music/playlist-files: the settings and what the last import found.
func (s *Server) handlePlaylistImportStatus(w http.ResponseWriter, r *http.Request) {
	rep, err := s.lastPlaylistImport()
	if err != nil {
		slog.Warn("read playlist imports", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the playlist files. Check the log for details.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s.playlistImportSettings(), "last": rep})
}

// PUT /api/music/playlist-files {auto?, shared?}
func (s *Server) handlePlaylistImportSettings(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		Auto   *bool `json:"auto"`
		Shared *bool `json:"shared"`
	}
	if err := decodeJSON(r, &req); err != nil || (req.Auto == nil && req.Shared == nil) {
		writeError(w, http.StatusBadRequest, "Say whether to bring playlists in each day, or whether to share them.")
		return
	}
	set := func(key string, on bool) error {
		v := "0"
		if on {
			v = "1"
		}
		return s.Settings.Set(key, v, false)
	}
	if req.Auto != nil {
		if err := set(keyPlaylistImportAuto, *req.Auto); err != nil {
			writeError(w, http.StatusInternalServerError, "Couldn't save the setting. Check the log for details.")
			return
		}
		if *req.Auto {
			_ = s.Settings.Set(keyPlaylistImportUser, strconv.FormatInt(me, 10), false)
		}
	}
	if req.Shared != nil {
		if err := set(keyPlaylistImportShared, *req.Shared); err != nil {
			writeError(w, http.StatusInternalServerError, "Couldn't save the setting. Check the log for details.")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.playlistImportSettings())
}

// POST /api/music/playlist-files/import {again?}: read the files now.
func (s *Server) handlePlaylistImportRun(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	var req struct {
		Again bool `json:"again"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "That request isn't right.")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	rep, err := s.importPlaylists(ctx, me, s.playlistImportSettings().Shared, req.Again)
	if err != nil {
		slog.Warn("music: bring in playlists", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read the playlist files in your music folder. Check the log for details.")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// playlistImportJob reads the playlist files again once a day, when switched on.
func (s *Server) playlistImportJob(ctx context.Context) {
	if !s.musicEnabled() || !s.playlistImportSettings().Auto {
		return
	}
	if last, _ := s.Settings.Get(keyPlaylistImportLast); last != "" {
		if t, err := time.Parse(time.RFC3339, last); err == nil && time.Since(t) < playlistImportEvery {
			return
		}
	}
	v, _ := s.Settings.Get(keyPlaylistImportUser)
	userID, _ := strconv.ParseInt(v, 10, 64)
	if userID <= 0 {
		return
	}
	if u, err := s.Auth.GetUser(userID); err != nil || u == nil || u.Disabled || !u.IsAdmin {
		return
	}
	if _, err := s.importPlaylists(ctx, userID, s.playlistImportSettings().Shared, false); err != nil {
		slog.Warn("music: daily playlist import", "err", err)
	}
}

// GET /api/music/playlists/{id}/m3u: the playlist as an .m3u8 file. Paths are
// as the server sees them, or relative to the music folder with ?relative=1.
func (s *Server) handleExportPlaylist(w http.ResponseWriter, r *http.Request) {
	me, ok := signedInAccount(w, r)
	if !ok {
		return
	}
	id, ok := musicID(w, r, "playlist")
	if !ok {
		return
	}
	p, err := s.MusicRepo.GetPlaylist(id, me)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	entries, err := s.MusicRepo.PlaylistEntries(id)
	if err != nil {
		writePlaylistError(w, err)
		return
	}
	relative := r.URL.Query().Get("relative") == "1"
	root := s.musicRoot()
	var b strings.Builder
	b.WriteString("#EXTM3U\n#PLAYLIST:" + oneLine(p.Name) + "\n")
	for _, e := range entries {
		if e.Track.FilePath == "" {
			continue
		}
		file := e.Track.FilePath
		if relative {
			if rel, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rel, "..") {
				file = filepath.ToSlash(rel)
			}
		}
		fmt.Fprintf(&b, "#EXTINF:%d,%s - %s\n%s\n", (e.Track.LengthMs+500)/1000, oneLine(e.Artist), oneLine(e.Title), file)
	}
	w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeFileName(p.Name) + ".m3u8"}))
	_, _ = io.WriteString(w, b.String())
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

// safeFileName keeps a name usable as a file name.
func safeFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, oneLine(s))
	if s == "" {
		return "playlist"
	}
	return s
}
