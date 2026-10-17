package api

// Usherr: an anime folder, like the comics folder. New anime shows go there
// unless another folder is picked, it's offered with the TV folders, and the
// Library's Anime tab finds anime by it. Set in Settings > Library > Folders,
// or with ANIME_DIR (see home_extra_folders.go).

import (
	"context"

	"github.com/rdborg/mediarium/internal/metadata"
)

const keyAnimePath = "home.anime_path"

// animeRoot is the anime folder, "" when there is none (see home_extra_folders.go).
func (s *Server) animeRoot() string { return s.extraFolderRoot("anime") }

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
