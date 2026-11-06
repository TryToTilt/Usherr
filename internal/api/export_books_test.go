package api

import "github.com/rdborg/mediarium/internal/comics"

// TestComicRepo is the Comics library, for tests that fill it.
func (s *Server) TestComicRepo() *comics.Repo { return s.comicRepo() }

// TestComicsOn switches the Comics module on or off.
func (s *Server) TestComicsOn(on bool) {
	v := ""
	if on {
		v = "1"
	}
	_ = s.Settings.Set(keyComicsEnabled, v, false)
}
