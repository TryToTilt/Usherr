package api

// Home fork: file new episodes where the show already lives. Upstream always
// renders "<Series Title> (<Year>)/Season NN", which splits an imported show
// whose folder is named differently ("9-1-1 - Nashville", "Season 1") into a
// second folder. Here the show's folder is taken from its existing episode
// files, and an existing season folder of the same number is reused
// ("Season 1", "Season 01", "Specials").

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rdborg/mediarium/internal/organizer"
)

// keySeasonFolder chooses how new season folders are named: "padded"
// ("Season 01", upstream's default) or "plain" ("Season 1").
const keySeasonFolder = "home.tv_season_folder"

func (s *Server) seasonFolderFormat() string {
	if v, _ := s.Settings.Get(keySeasonFolder); v == "plain" {
		return "Season {Season}"
	}
	return organizer.TVSeasonFolder
}

// GET/PUT /api/home/season-folder {"style":"plain"|"padded"}
func (s *Server) handleSeasonFolderStyle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req struct {
			Style string `json:"style"`
		}
		if err := decodeJSON(r, &req); err != nil || (req.Style != "plain" && req.Style != "padded") {
			writeError(w, http.StatusBadRequest, `Choose "plain" (Season 1) or "padded" (Season 01).`)
			return
		}
		if err := s.Settings.Set(keySeasonFolder, req.Style, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
			return
		}
	}
	style := "padded"
	if v, _ := s.Settings.Get(keySeasonFolder); v == "plain" {
		style = "plain"
	}
	writeJSON(w, http.StatusOK, map[string]string{"style": style})
}

// existingShowFolder is the folder below root that already holds episodes
// of the series, or "" when it has none yet.
func (s *Server) existingShowFolder(seriesID int64, root string) string {
	eps, err := s.MovieRepo.ListEpisodes(seriesID)
	if err != nil || root == "" {
		return ""
	}
	cleanRoot := filepath.Clean(root)
	counts := map[string]int{}
	for _, ep := range eps {
		if ep.FilePath == "" {
			continue
		}
		rel, err := filepath.Rel(cleanRoot, filepath.Clean(ep.FilePath))
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if first == "" || first == rel {
			continue // a file directly in the root has no show folder
		}
		counts[first]++
	}
	best, bestN := "", 0
	for dir, n := range counts {
		if n > bestN || (n == bestN && dir < best) {
			best, bestN = dir, n
		}
	}
	if best == "" {
		return ""
	}
	dir := filepath.Join(cleanRoot, best)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ""
	}
	return dir
}

var seasonDirRe = regexp.MustCompile(`(?i)^season[ ._-]*0*(\d+)$`)

// existingSeasonFolder is the folder inside showDir already used for this
// season ("Season 1", "Season 01", "season 1"; "Specials" for season 0), or "".
func existingSeasonFolder(showDir string, season int) string {
	entries, err := os.ReadDir(showDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if season == 0 && strings.EqualFold(name, "Specials") {
			return filepath.Join(showDir, name)
		}
		if m := seasonDirRe.FindStringSubmatch(name); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n == season {
				return filepath.Join(showDir, name)
			}
		}
	}
	return ""
}
