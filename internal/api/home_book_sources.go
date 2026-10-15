package api

// Home fork: books may use torrents while everything else stays on Usenet.
// Usenet carries few ebooks, so with the torrent client on (behind the VPN)
// book searches take releases from both, Usenet first when equally good.
// The default sources setting still governs movies, TV, music and the rest.

import "net/http"

const keyBookSources = "home.book_sources" // usenet, torrent or both (default both)

func (s *Server) bookSources() string {
	if !s.torrentsEnabled() {
		return sourcesUsenet
	}
	switch v, _ := s.Settings.Get(keyBookSources); v {
	case sourcesUsenet, sourcesTorrent, sourcesBoth:
		return v
	}
	return sourcesBoth
}

// GET/PUT /api/home/book-sources {"sources":"both"}
func (s *Server) handleBookSources(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req struct {
			Sources string `json:"sources"`
		}
		if err := decodeJSON(r, &req); err != nil || (req.Sources != sourcesUsenet && req.Sources != sourcesTorrent && req.Sources != sourcesBoth) {
			writeError(w, http.StatusBadRequest, `Choose "usenet", "torrent" or "both".`)
			return
		}
		if err := s.Settings.Set(keyBookSources, req.Sources, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The setting couldn't be saved.")
			return
		}
	}
	v, _ := s.Settings.Get(keyBookSources)
	if v == "" {
		v = sourcesBoth
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": v, "inEffect": s.bookSources(), "torrentsOn": s.torrentsEnabled()})
}
