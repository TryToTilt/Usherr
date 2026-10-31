package mediaservers

// Usherr: making a library in Plex, so the virtual drive's films and shows
// get theirs without anyone doing it by hand in Plex.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// PlexAddLibrary makes a library on the Plex server: kind "movie" or
// "show", with Plex's own agent and scanner for it, holding the folders
// given (as the Plex computer sees them).
func (c *Client) PlexAddLibrary(ctx context.Context, s Server, name, kind string, folders []string) error {
	q := url.Values{}
	q.Set("name", name)
	q.Set("type", kind)
	q.Set("language", "en-US")
	switch kind {
	case "movie":
		q.Set("agent", "tv.plex.agents.movie")
		q.Set("scanner", "Plex Movie")
	case "show":
		q.Set("agent", "tv.plex.agents.series")
		q.Set("scanner", "Plex TV Series")
	default:
		return userErr(nil, "A Plex library holds films or shows, not %q.", kind)
	}
	for _, f := range folders {
		q.Add("location", f)
	}
	if err := c.do(ctx, s, http.MethodPost, "/library/sections", q, nil, nil); err != nil {
		return explain(s, err)
	}
	return nil
}

// PlexLibraryFor is the library of that kind already holding the folder
// (or a folder inside it), if there is one.
func PlexLibraryFor(libs []Library, kind, folder string) (Library, bool) {
	for _, l := range libs {
		if l.Type != kind {
			continue
		}
		for _, loc := range l.Locations {
			if _, ok := within(folder, loc); ok || strings.EqualFold(comparable(loc), comparable(folder)) {
				return l, true
			}
		}
	}
	return Library{}, false
}

// PlexLibraries lists the Plex server's libraries.
func (c *Client) PlexLibraries(ctx context.Context, s Server) ([]Library, error) {
	return c.plexSections(ctx, s)
}
