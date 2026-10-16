package api

// Usherr: an anime folder, like the comics folder. New anime shows go there
// unless another folder is picked, it's offered with the TV folders, and the
// Library's Anime tab finds anime by it. Set in Settings > Library > Folders,
// or with ANIME_DIR.

import (
	"context"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/rdborg/mediarium/internal/metadata"
)

const keyAnimePath = "home.anime_path"

// animeRoot is the anime folder, "" when there is none.
func (s *Server) animeRoot() string {
	if v, _ := s.Settings.Get(keyAnimePath); strings.TrimSpace(v) != "" {
		return path.Clean(strings.TrimSpace(v))
	}
	if v := strings.TrimSpace(os.Getenv("ANIME_DIR")); v != "" {
		return path.Clean(v)
	}
	return ""
}

// isAnimeShow: animation made in Japan, or in Japanese.
func isAnimeShow(sh metadata.Show) bool {
	animated := false
	for _, g := range sh.Genres {
		animated = animated || g.ID == 16
	}
	for _, g := range sh.GenreIDs {
		animated = animated || g == 16
	}
	if !animated {
		return false
	}
	if sh.OriginalLanguage == "ja" {
		return true
	}
	for _, c := range sh.OriginCountry {
		if c == "JP" {
			return true
		}
	}
	return false
}

// animeFolderFor is the folder a newly added show goes to when none was
// picked: the anime folder for anime, else "" (the main TV folder).
func (s *Server) animeFolderFor(ctx context.Context, tmdbID int) string {
	root := s.animeRoot()
	if root == "" || !s.TMDB().HasAPIKey() {
		return ""
	}
	sh, err := s.TMDB().GetShow(ctx, tmdbID)
	if err != nil || !isAnimeShow(sh.Show) {
		return ""
	}
	return root
}

// GET /api/home/anime-folder (admin).
func (s *Server) handleGetAnimeFolder(w http.ResponseWriter, r *http.Request) {
	saved, _ := s.Settings.Get(keyAnimePath)
	writeJSON(w, http.StatusOK, map[string]any{"path": strings.TrimSpace(saved), "env": strings.TrimSpace(os.Getenv("ANIME_DIR")), "inUse": s.animeRoot()})
}

// PUT /api/home/anime-folder {path} (admin): "" goes back to ANIME_DIR, or none.
func (s *Server) handlePutAnimeFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	p := strings.TrimSpace(req.Path)
	if p != "" {
		if !strings.HasPrefix(p, "/") {
			writeError(w, http.StatusBadRequest, "The anime folder is a path inside the container, starting with /, such as /data/anime.")
			return
		}
		p = path.Clean(p)
	}
	if err := s.Settings.Set(keyAnimePath, p, false); err != nil {
		writeError(w, http.StatusInternalServerError, "The anime folder couldn't be saved.")
		return
	}
	s.handleGetAnimeFolder(w, r)
}
