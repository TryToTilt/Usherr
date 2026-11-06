package api

import (
	"database/sql"
	"errors"
	"mime"
	"net/http"
	"path"
	"strconv"

	"github.com/rdborg/mediarium/internal/mediafiles"
)

// GET /api/music/tracks/{id}/play: one downloaded track, for the built-in
// player. It is served with ranges so the player can seek, and only from inside
// the music folder.
func (s *Server) handlePlayTrack(w http.ResponseWriter, r *http.Request) {
	id, ok := musicID(w, r, "track")
	if !ok {
		return
	}
	t, err := s.MusicRepo.GetTrack(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That track isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if t.FilePath == "" {
		writeError(w, http.StatusNotFound, "This track hasn't been downloaded yet.")
		return
	}
	folder, err := mediafiles.ForFile(t.FilePath, []string{s.musicRoot()}, false)
	if err != nil {
		writeStreamFolderError(w, err)
		return
	}
	rel, ok := folder.Rel(t.FilePath)
	if !ok {
		writeError(w, http.StatusForbidden, "This track's file isn't inside your music folder.")
		return
	}
	// Usherr: ?format=opus|aac|mp3|raw and ?maxBitRate=kbit/s convert while
	// streaming (music_transcode.go).
	q := streamQuery{Format: r.URL.Query().Get("format"), Prefer: r.URL.Query().Get("prefer")}
	q.MaxKbps, _ = strconv.Atoi(r.URL.Query().Get("maxBitRate"))
	s.serveTrackStream(w, r, requestUserID(r), folder, rel, q, float64(t.LengthMs)/1000)
}

// serveOriginal sends a track's file as it is, with ranges.
func (s *Server) serveOriginal(w http.ResponseWriter, r *http.Request, folder mediafiles.Folder, rel string) {
	ct := mediafiles.AudioContentType(rel)
	if ct == "" {
		writeError(w, http.StatusUnsupportedMediaType, "This file can't be played in the browser.")
		return
	}
	file, info, err := folder.Open(rel)
	if !s.openedOK(w, err) {
		return
	}
	defer file.Close()
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	if h.Get("Content-Disposition") == "" { // a Subsonic download has set "attachment"
		h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": path.Base(rel)}))
	}
	http.ServeContent(w, r, path.Base(rel), info.ModTime(), file)
}
