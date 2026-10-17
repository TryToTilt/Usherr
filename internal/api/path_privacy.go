package api

import (
	"net/http"
	"path/filepath"
)

// fileLabel is how a file's location is shown to the person asking: the full
// path for an administrator, only the file's name for anyone else. Members
// still see which file a title has, but not how the server's folders are laid
// out.
func fileLabel(r *http.Request, path string) string {
	if path == "" || isAdminRequest(r) {
		return path
	}
	return filepath.Base(path)
}
