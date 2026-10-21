package api

// Home fork: "Play" for movies and episodes. The server finds the title's
// own video file, so the page only needs the title's id. Same rules as
// /api/files/stream: only files inside the library folders, no links out,
// range requests for seeking, served inline and sandboxed.

import (
	"database/sql"
	"errors"
	"mime"
	"net/http"
	"path"
	"strconv"

	"github.com/rdborg/mediarium/internal/mediafiles"
)

// registerPlayRoutes adds the play routes for people who may play titles.
func (s *Server) registerPlayRoutes(play routeGroup) {
	play.HandleFunc("GET /api/play/movie/{id}", s.handlePlayMovie)
	play.HandleFunc("GET /api/play/episode/{id}", s.handlePlayEpisode)
	play.HandleFunc("POST /api/play/prepare", s.handlePlayPrepare)
	play.HandleFunc("GET /api/play/convert/{kind}/{id}", s.handlePlayConvert) // Usherr: converted here
	play.HandleFunc("GET /api/play/tracks/{kind}/{id}", s.handlePlayTracks)   // Usherr: sound and subtitles
	play.HandleFunc("GET /api/play/subs/{kind}/{id}", s.handlePlaySubs)
	play.HandleFunc("GET /api/play/hls/{token}/{rest...}", s.handlePlayHLS)
	play.HandleFunc("DELETE /api/play/hls/{token}", s.handleStopHLS)
	// Stream now (home_streaming.go)
	play.HandleFunc("GET /api/stream/status", s.handleStreamStatus)
	play.HandleFunc("GET /api/stream/options", s.handleStreamOptions)
	play.HandleFunc("POST /api/stream/start", s.handleStreamStart)
	play.HandleFunc("GET /api/stream/{id}/video", s.handleStreamVideo)
	play.HandleFunc("GET /api/stream/{id}/link", s.handleStreamLink)
	play.HandleFunc("POST /api/stream/{id}/stop", s.handleStreamStop)
	play.HandleFunc("POST /api/stream/{id}/next", s.handleStreamNext) // Usherr: another release
	play.HandleFunc("GET /api/addon", s.handleGetAddon)               // Usherr: the Stremio and Nuvio add-on
	play.HandleFunc("POST /api/addon/regenerate", s.handleRegenerateAddon)
	play.HandleFunc("POST /api/stream/tv", s.handleStreamTVTitle)
	play.HandleFunc("POST /api/stream/{id}/tv", s.handleStreamTV)
}

// serveLibraryVideo streams file, which must be a video inside one of roots.
func serveLibraryVideo(w http.ResponseWriter, r *http.Request, file string, roots []string) {
	if file == "" {
		writeError(w, http.StatusNotFound, "This title has no file yet.")
		return
	}
	folder, err := mediafiles.ForFile(file, roots, false)
	if err != nil {
		writeError(w, http.StatusNotFound, "Couldn't find this file in your library folders.")
		return
	}
	rel, inside := folder.Rel(file)
	contentType := mediafiles.VideoContentType(rel)
	if !inside || contentType == "" {
		writeError(w, http.StatusUnsupportedMediaType, "This file can't be played here.")
		return
	}
	f, info, err := folder.Open(rel)
	if err != nil {
		writeError(w, http.StatusNotFound, "Couldn't open the file. It may have been moved or deleted.")
		return
	}
	defer f.Close()
	name := path.Base(rel)
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// GET /api/play/movie/{id}
func (s *Server) handlePlayMovie(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return
	}
	m, err := s.MovieRepo.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	serveLibraryVideo(w, r, m.FilePath, s.libraryRoots())
}

// GET /api/play/episode/{id}
func (s *Server) handlePlayEpisode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid episode ID.")
		return
	}
	ep, err := s.MovieRepo.GetEpisodeByID(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That episode isn't in your library.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	serveLibraryVideo(w, r, ep.FilePath, s.libraryRoots())
}
