package api

// Home fork: Mediarium TV channel logos. A channel's logo is a picture
// uploaded to Mediarium (kept in the config folder, under tv-logos), a link
// to one, or one of its shows' logos from TMDB. Uploaded ones are served to
// the app at /api/tv/channels/{id}/logo and to Jellyfin, which can't sign
// in, at /api/tvlogo/{key}/{id}.

import (
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

	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/tv"
)

// tvLogoUploaded is what a channel's logo address says when the picture was
// uploaded to Mediarium.
const tvLogoUploaded = "mediarium:logo"

var tvLogoTypes = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}

func (s *Server) tvLogoDir() string { return filepath.Join(s.cfg.ConfigDir, "tv-logos") }

// tvLogoFile is a channel's uploaded logo on disk, if it has one.
func (s *Server) tvLogoFile(id int64) (string, bool) {
	for _, ext := range []string{".png", ".jpg", ".webp", ".gif"} {
		p := filepath.Join(s.tvLogoDir(), strconv.FormatInt(id, 10)+ext)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// appLogo is a channel's logo address for the app's pages.
func (s *Server) appLogo(c tv.Channel) string {
	if c.LogoURL != tvLogoUploaded {
		return c.LogoURL
	}
	p, ok := s.tvLogoFile(c.ID)
	if !ok {
		return ""
	}
	v := ""
	if fi, err := os.Stat(p); err == nil {
		v = "?v=" + strconv.FormatInt(fi.ModTime().Unix(), 10)
	}
	return fmt.Sprintf("/api/tv/channels/%d/logo%s", c.ID, v)
}

// appChannels gives channels their logo addresses for the app.
func (s *Server) appChannels(chans []tv.Channel) []tv.Channel {
	out := make([]tv.Channel, len(chans))
	for i, c := range chans {
		c.LogoURL = s.appLogo(c)
		out[i] = c
	}
	return out
}

// feedChannels gives channels logo addresses Jellyfin can reach.
func (s *Server) feedChannels(chans []tv.Channel, origin, key string) []tv.Channel {
	out := make([]tv.Channel, len(chans))
	for i, c := range chans {
		if c.LogoURL == tvLogoUploaded {
			c.LogoURL = ""
			if _, ok := s.tvLogoFile(c.ID); ok {
				c.LogoURL = fmt.Sprintf("%s/api/tvlogo/%s/%d", origin, key, c.ID)
			}
		}
		out[i] = c
	}
	return out
}

// storedLogo is what to keep for a logo address the app sends back: its own
// address for an uploaded logo means "keep the upload".
func storedLogo(sent string, current string) string {
	if strings.HasPrefix(sent, "/api/tv/channels/") && strings.Contains(sent, "/logo") {
		return tvLogoUploaded
	}
	if sent == "" || strings.HasPrefix(sent, "https://") || strings.HasPrefix(sent, "http://") {
		return sent
	}
	return current
}

func (s *Server) registerTVLogoRoutes(public *http.ServeMux) {
	public.HandleFunc("GET /api/tvlogo/{key}/{id}", s.handleTVLogoFeed)
}

func (s *Server) serveTVLogo(w http.ResponseWriter, r *http.Request, id int64) {
	p, ok := s.tvLogoFile(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, p)
}

// GET /api/tv/channels/{id}/logo
func (s *Server) handleTVLogo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.serveTVLogo(w, r, id)
}

// GET /api/tvlogo/{key}/{id}
func (s *Server) handleTVLogoFeed(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || !s.tvFeedOK(r) {
		http.NotFound(w, r)
		return
	}
	s.serveTVLogo(w, r, id)
}

// POST /api/tv/channels/{id}/logo (multipart "file"): upload a logo.
func (s *Server) handleTVLogoUpload(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a channel.")
		return
	}
	repo := s.tvRepo()
	ch, err := repo.Channel(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20+64<<10)
	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Choose a picture of 2 MB or less.")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 2<<20+1))
	if err != nil || len(data) > 2<<20 {
		writeError(w, http.StatusBadRequest, "Choose a picture of 2 MB or less.")
		return
	}
	ext, ok := tvLogoTypes[http.DetectContentType(data)]
	if !ok {
		writeError(w, http.StatusBadRequest, "That isn't a PNG, JPG, WebP or GIF picture.")
		return
	}
	if err := os.MkdirAll(s.tvLogoDir(), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.removeTVLogo(id)
	if err := os.WriteFile(filepath.Join(s.tvLogoDir(), strconv.FormatInt(id, 10)+ext), data, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ch.LogoURL = tvLogoUploaded
	saved, err := repo.Save(ch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	saved.LogoURL = s.appLogo(saved)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) removeTVLogo(id int64) {
	for {
		p, ok := s.tvLogoFile(id)
		if !ok {
			return
		}
		if err := os.Remove(p); err != nil {
			return
		}
	}
}

type logoIdea struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// GET /api/tv/channels/{id}/logo-ideas: its shows' logos, to pick one.
func (s *Server) handleTVLogoIdeas(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a channel.")
		return
	}
	ch, err := s.tvRepo().Channel(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if s.TMDB() == nil {
		writeError(w, http.StatusServiceUnavailable, "TMDB isn't set up.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	type show struct {
		tmdb  int
		title string
	}
	var shows []show
	for _, sid := range ch.Rules.Series {
		var t sql.NullInt64
		var title sql.NullString
		if err := s.db.QueryRowContext(ctx, `SELECT tmdb_id, title FROM series WHERE id = ?`, sid).Scan(&t, &title); err == nil && t.Int64 > 0 {
			shows = append(shows, show{int(t.Int64), title.String})
		}
	}
	for _, st := range ch.Rules.Stream {
		shows = append(shows, show{int(st.TMDBID), st.Title})
	}
	out := []logoIdea{}
	for _, sh := range shows {
		if len(out) >= 12 || ctx.Err() != nil {
			break
		}
		if p, err := s.TMDB().TitleLogo(ctx, "tv", sh.tmdb); err == nil && p != "" {
			out = append(out, logoIdea{Title: sh.title, URL: metadata.ImageURL("w300", p)})
		}
	}
	if len(out) == 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "TMDB took too long. Try again.")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// DELETE /api/tv/channels/{id}/logo
func (s *Server) handleTVLogoDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a channel.")
		return
	}
	repo := s.tvRepo()
	ch, err := repo.Channel(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.removeTVLogo(id)
	ch.LogoURL = ""
	saved, err := repo.Save(ch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}
