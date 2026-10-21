package api

import (
	"net/http"
	"path/filepath"
	"strconv"
)

// Only administrators see where the server keeps its files. Everyone else is
// told a library folder by a label instead: the folder's own name (movies,
// kids), with a number after it when two folders have the same name. A label
// is also accepted wherever a folder is chosen, so the Add dialog works the same
// for everyone.

// folderLabels names each of paths for people who don't see paths. The labels
// are unique within the list, and in the same order.
func folderLabels(paths []string) []string {
	out := make([]string, len(paths))
	used := map[string]int{}
	for i, p := range paths {
		name := baseFolderName(p)
		used[name]++
		if n := used[name]; n > 1 {
			name += " " + strconv.Itoa(n)
		}
		out[i] = name
	}
	return out
}

func baseFolderName(path string) string {
	name := filepath.Base(filepath.Clean(path))
	if name == "." || name == "/" || name == string(filepath.Separator) || name == "" {
		return "Folder"
	}
	return name
}

// resolveFolderChoice turns what a client sent into one of the offered folders:
// a full path, or the label folderLabels gives it. Anything else comes back as
// it was, for the caller to refuse.
func resolveFolderChoice(want string, offered []string) string {
	if want == "" {
		return want
	}
	c := filepath.Clean(want)
	for _, f := range offered {
		if filepath.Clean(f) == c {
			return f
		}
	}
	for i, l := range folderLabels(offered) {
		if l == want {
			return offered[i]
		}
	}
	return want
}

// folderShown is how a library folder (as stored on a title) is shown to the
// person asking: the full path for an administrator, its label for anyone else.
func folderShown(r *http.Request, offered []string, path string) string {
	if path == "" || isAdminRequest(r) {
		return path
	}
	c := filepath.Clean(path)
	for i, l := range folderLabels(offered) {
		if filepath.Clean(offered[i]) == c {
			return l
		}
	}
	return baseFolderName(path)
}
