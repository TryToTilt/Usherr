package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Routes registered on a group that needs a permission are listed too, with
// the access of the group they were made from.
func TestAPIDocSeesRoutesThatNeedAPermission(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package api

func (s *Server) routes() {
	member := t.group(accessMember)
	admin := t.group(accessAdmin)
	releases := member.Need(auth.PermReleases)
	member.HandleFunc("GET /api/one", s.handleOne)
	member.Need(auth.PermMovies).HandleFunc("POST /api/movies", s.handleAddMovie)
	member.Need(auth.PermBooks, auth.PermManage).HandleFunc("PUT /api/follow", s.handleFollow)
	releases.HandleFunc("GET /api/releases", s.handleReleases)
	admin.Need(auth.PermX).HandleFunc("DELETE /api/thing", s.handleThing)
}

// handleAddMovie adds a movie to the library.
func (s *Server) handleAddMovie() {}
`
	if err := os.WriteFile(filepath.Join(dir, "server.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := apiDoc(root)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	for _, want := range []string{
		"| GET | `/api/one` | any account |",
		"| POST | `/api/movies` | any account | adds a movie to the library. |",
		"| PUT | `/api/follow` | any account |",
		"| GET | `/api/releases` | any account |",
		"| DELETE | `/api/thing` | admin |",
		"5 routes.",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q in:\n%s", want, doc)
		}
	}
}
