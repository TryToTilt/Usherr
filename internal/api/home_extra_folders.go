package api

// Usherr: the folders the base app doesn't have (anime, comics, NSFW 18+),
// set in one place, Settings > Library > Folders, next to the movie and TV
// folders. Each is the saved folder, else its *_DIR variable, else a default
// a new install has without setting anything: the kind's own mount (/comics),
// else a folder of that name in /data, the one folder the example compose
// file maps. Anime has no made-up default: without a folder of its own it
// goes in the TV folder.

import (
	"net/http"
	"os"
	"path"
	"strings"
)

type extraFolder struct {
	key, env string
	// made: the /data default may be a folder that doesn't exist yet (it is
	// made on the first download); otherwise only an existing folder counts.
	made bool
	// old: the names it had before (Usherr 1.0.3 renamed scenes to nsfw), still read.
	oldEnv, oldDir string
}

var extraFolders = map[string]extraFolder{
	"anime":  {key: keyAnimePath, env: "ANIME_DIR"},
	"comics": {key: keyComicsPath, env: "COMICS_DIR", made: true},
	"nsfw":   {key: keyNsfwPath, env: "NSFW_DIR", made: true, oldEnv: "SCENES_DIR", oldDir: "scenes"},
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// extraFolderDefault is where a kind goes when nothing is set: its own mount
// (/comics), else /data/<kind>.
func extraFolderDefault(kind string) string {
	f := extraFolders[kind]
	at := func(p string) string { return path.Join(extraFolderBase, p) } // the folder as the container sees it
	if isDir(at(kind)) {
		return "/" + kind
	}
	if f.oldDir != "" && isDir(at(f.oldDir)) {
		return "/" + f.oldDir
	}
	if f.oldDir != "" && isDir(at("data/"+f.oldDir)) && !isDir(at("data/"+kind)) {
		return "/data/" + f.oldDir
	}
	if isDir(at("data/" + kind)) {
		return "/data/" + kind
	}
	if f.made && isDir(at("data")) {
		return "/data/" + kind
	}
	return ""
}

// extraFolderBase is "/" in the container; tests point it at a temporary folder.
var extraFolderBase = "/"

// extraFolderRoot is the folder in use for a kind.
func (s *Server) extraFolderRoot(kind string) string {
	f := extraFolders[kind]
	if v, _ := s.Settings.Get(f.key); strings.TrimSpace(v) != "" {
		return path.Clean(strings.TrimSpace(v))
	}
	if v := strings.TrimSpace(os.Getenv(f.env)); v != "" {
		return path.Clean(v)
	}
	if v := strings.TrimSpace(os.Getenv(f.oldEnv)); f.oldEnv != "" && v != "" {
		return path.Clean(v)
	}
	return extraFolderDefault(kind)
}

// GET /api/home/folders/{kind} (admin): {path (saved), env, fallback, inUse}.
func (s *Server) handleGetExtraFolder(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	f, ok := extraFolders[kind]
	if !ok {
		writeError(w, http.StatusNotFound, "There is no such folder.")
		return
	}
	saved, _ := s.Settings.Get(f.key)
	env, envName := strings.TrimSpace(os.Getenv(f.env)), f.env
	if env == "" && f.oldEnv != "" {
		env, envName = strings.TrimSpace(os.Getenv(f.oldEnv)), f.oldEnv
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": strings.TrimSpace(saved), "env": env, "envName": envName, "fallback": extraFolderDefault(kind), "inUse": s.extraFolderRoot(kind)})
}

// PUT /api/home/folders/{kind} {path} (admin): "" goes back to the variable or the default.
func (s *Server) handlePutExtraFolder(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	f, ok := extraFolders[kind]
	if !ok {
		writeError(w, http.StatusNotFound, "There is no such folder.")
		return
	}
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
			writeError(w, http.StatusBadRequest, "The folder is a path inside the container, starting with /, such as /data/comics.")
			return
		}
		p = path.Clean(p)
	}
	if err := s.Settings.Set(f.key, p, false); err != nil {
		writeError(w, http.StatusInternalServerError, "The folder couldn't be saved.")
		return
	}
	s.handleGetExtraFolder(w, r)
}
