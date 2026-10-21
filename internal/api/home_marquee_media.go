package api

// Usherr: your own Marquee slides and pre-show pieces (table marquee_media).
// Pictures and videos are uploaded and kept in the config folder
// (marquee-media); a web page or a slide of your own words needs no file.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const marqueeMediaMax = 2 << 30 // 2 GB: a 4K pre-show video fits

// What can be uploaded, by the type its first bytes show.
var marqueeMediaTypes = map[string]struct{ kind, ext string }{
	"image/png":  {"image", ".png"},
	"image/jpeg": {"image", ".jpg"},
	"image/webp": {"image", ".webp"},
	"image/gif":  {"image", ".gif"},
	"video/mp4":  {"video", ".mp4"},
	"video/webm": {"video", ".webm"},
}

type marqueeMediaItem struct {
	ID      int64  `json:"id"`
	Place   string `json:"place"` // "slide" or "preshow"
	Kind    string `json:"kind"`  // "image", "video", "web" or "text"
	Title   string `json:"title"`
	Body    string `json:"body,omitempty"`
	URL     string `json:"url,omitempty"`     // a web page
	FileURL string `json:"fileUrl,omitempty"` // a picture or video
	Size    int64  `json:"size,omitempty"`
	Seconds int    `json:"seconds"`
	Enabled bool   `json:"enabled"`
	Sort    int    `json:"sort"`
}

func (s *Server) marqueeMediaDir() string { return filepath.Join(s.cfg.ConfigDir, "marquee-media") }

// marqueeMediaList lists the slides and pre-show pieces, in their order
// (only the switched-on ones for a screen).
func (s *Server) marqueeMediaList(ctx context.Context, enabledOnly bool) []marqueeMediaItem {
	out := []marqueeMediaItem{}
	q := `SELECT id, place, kind, title, body, url, file, size_bytes, seconds, enabled, sort FROM marquee_media`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY place, sort, id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var m marqueeMediaItem
		var file string
		if rows.Scan(&m.ID, &m.Place, &m.Kind, &m.Title, &m.Body, &m.URL, &file, &m.Size, &m.Seconds, &m.Enabled, &m.Sort) != nil {
			continue
		}
		if file != "" {
			m.FileURL = fmt.Sprintf("/api/home/marquee/media/%d/file", m.ID)
		}
		out = append(out, m)
	}
	return out
}

// marqueeMediaWebOrigins are the web pages the Marquee shows in a frame, for
// the security headers (refreshAppFrames).
func (s *Server) marqueeMediaWebOrigins() []string {
	var out []string
	rows, err := s.db.Query(`SELECT url FROM marquee_media WHERE kind = 'web' AND enabled = 1`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			if origin, ok := appOrigin(u); ok {
				out = append(out, origin)
			}
		}
	}
	return out
}

func cleanPlace(p string) string {
	if p == "preshow" {
		return "preshow"
	}
	return "slide"
}

func cleanSeconds(n int) int {
	if n < 3 || n > 600 {
		return 12
	}
	return n
}

// marqueeMediaChanged tells the screens, and the security headers, about it.
func (s *Server) marqueeMediaChanged() {
	s.refreshAppFrames()
	s.marqueeLive.send("", marqueeEvent{Type: "settings"})
}

// GET /api/home/marquee/media (admin): every slide and pre-show piece.
func (s *Server) handleMarqueeMedia(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"media": s.marqueeMediaList(r.Context(), false)})
}

// POST /api/home/marquee/media (admin, multipart: place, title, seconds, then
// file): upload a picture or a video.
func (s *Server) handleMarqueeMediaUpload(w http.ResponseWriter, r *http.Request) {
	allowLongUpload(w)
	r.Body = http.MaxBytesReader(w, r.Body, marqueeMediaMax+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "Choose a picture or a video to upload.")
		return
	}
	fields := map[string]string{}
	var stored, mimeType, kind string
	var size int64
	dir := s.marqueeMediaDir()
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "The upload didn't arrive whole. Try again.")
			return
		}
		if part.FormName() != "file" {
			b, _ := io.ReadAll(io.LimitReader(part, 4096))
			fields[part.FormName()] = string(b)
			continue
		}
		if stored != "" {
			continue // one file per upload
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(part, head)
		head = head[:n]
		t, ok := marqueeMediaTypes[http.DetectContentType(head)]
		if !ok {
			writeError(w, http.StatusBadRequest, "That isn't a picture (PNG, JPG, WebP, GIF) or a video (MP4, WebM).")
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "The file couldn't be saved.")
			return
		}
		f, err := os.CreateTemp(dir, "upload-*"+t.ext)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "The file couldn't be saved.")
			return
		}
		size, err = io.Copy(f, io.MultiReader(bytes.NewReader(head), part))
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			_ = os.Remove(f.Name())
			writeError(w, http.StatusBadRequest, "The upload didn't arrive whole, or it is bigger than 2 GB.")
			return
		}
		stored, mimeType, kind = filepath.Base(f.Name()), http.DetectContentType(head), t.kind
	}
	if stored == "" {
		writeError(w, http.StatusBadRequest, "Choose a picture or a video to upload.")
		return
	}
	seconds, _ := strconv.Atoi(fields["seconds"])
	title := trimRunes(strings.TrimSpace(fields["title"]), 100)
	place := cleanPlace(fields["place"])
	var next int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(sort), 0) + 1 FROM marquee_media WHERE place = ?`, place).Scan(&next)
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO marquee_media (place, kind, title, file, mime, size_bytes, seconds, enabled, sort, created_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`, place, kind, title, stored, mimeType, size, cleanSeconds(seconds), next, time.Now().UnixMilli()); err != nil {
		_ = os.Remove(filepath.Join(dir, stored))
		writeError(w, http.StatusInternalServerError, "The file couldn't be saved.")
		return
	}
	s.marqueeMediaChanged()
	s.handleMarqueeMedia(w, r)
}

// POST /api/home/marquee/media/page (admin): a web page or a slide of words.
func (s *Server) handleMarqueeMediaPage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Place   string `json:"place"`
		Kind    string `json:"kind"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		URL     string `json:"url"`
		Seconds int    `json:"seconds"`
	}
	if err := decodeJSON(r, &body); err != nil || (body.Kind != "web" && body.Kind != "text") {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	body.Title = trimRunes(strings.TrimSpace(body.Title), 100)
	body.Body = trimRunes(strings.TrimSpace(body.Body), 600)
	body.URL = strings.TrimSpace(body.URL)
	if body.Kind == "web" {
		if _, ok := appOrigin(body.URL); !ok {
			writeError(w, http.StatusBadRequest, "Give the page's full address, starting with http:// or https://.")
			return
		}
	} else if body.Title == "" && body.Body == "" {
		writeError(w, http.StatusBadRequest, "Write something for the slide.")
		return
	}
	place := cleanPlace(body.Place)
	var next int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(sort), 0) + 1 FROM marquee_media WHERE place = ?`, place).Scan(&next)
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO marquee_media (place, kind, title, body, url, seconds, enabled, sort, created_ms) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		place, body.Kind, body.Title, body.Body, body.URL, cleanSeconds(body.Seconds), next, time.Now().UnixMilli()); err != nil {
		writeError(w, http.StatusInternalServerError, "The slide couldn't be saved.")
		return
	}
	s.marqueeMediaChanged()
	s.handleMarqueeMedia(w, r)
}

// PUT /api/home/marquee/media/{id} (admin): change one.
func (s *Server) handlePutMarqueeMedia(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Place   string `json:"place"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		URL     string `json:"url"`
		Seconds int    `json:"seconds"`
		Enabled bool   `json:"enabled"`
		Sort    int    `json:"sort"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	var kind string
	if err := s.db.QueryRowContext(r.Context(), `SELECT kind FROM marquee_media WHERE id = ?`, id).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That slide isn't there any more.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "The slide couldn't be saved.")
		return
	}
	body.URL = strings.TrimSpace(body.URL)
	if kind == "web" {
		if _, ok := appOrigin(body.URL); !ok {
			writeError(w, http.StatusBadRequest, "Give the page's full address, starting with http:// or https://.")
			return
		}
	} else {
		body.URL = ""
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE marquee_media SET place = ?, title = ?, body = ?, url = ?, seconds = ?, enabled = ?, sort = ? WHERE id = ?`,
		cleanPlace(body.Place), trimRunes(strings.TrimSpace(body.Title), 100), trimRunes(strings.TrimSpace(body.Body), 600), body.URL, cleanSeconds(body.Seconds), body.Enabled, body.Sort, id); err != nil {
		writeError(w, http.StatusInternalServerError, "The slide couldn't be saved.")
		return
	}
	s.marqueeMediaChanged()
	s.handleMarqueeMedia(w, r)
}

// DELETE /api/home/marquee/media/{id} (admin): remove one, and its file.
func (s *Server) handleDeleteMarqueeMedia(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var file string
	_ = s.db.QueryRowContext(r.Context(), `SELECT file FROM marquee_media WHERE id = ?`, id).Scan(&file)
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM marquee_media WHERE id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "The slide couldn't be removed.")
		return
	}
	if file != "" && filepath.Base(file) == file {
		_ = os.Remove(filepath.Join(s.marqueeMediaDir(), file))
	}
	s.marqueeMediaChanged()
	s.handleMarqueeMedia(w, r)
}

// GET /api/home/marquee/media/{id}/file (member): a picture or a video, for
// the screens (a video can be read from any point).
func (s *Server) handleMarqueeMediaFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var file, mimeType string
	if err := s.db.QueryRowContext(r.Context(), `SELECT file, mime FROM marquee_media WHERE id = ?`, id).Scan(&file, &mimeType); err != nil || file == "" || filepath.Base(file) != file {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.marqueeMediaDir(), file))
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
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", st.ModTime(), f)
}
