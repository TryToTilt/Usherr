package api

// Home fork: per-account switches for audiobooks, comics and NSFW 18+.

import (
	"net/http"

	"github.com/rdborg/mediarium/internal/auth"
)

// userAllows reports whether the signed-in account has the permission
// (administrators have them all).
func (s *Server) userAllows(r *http.Request, perm string) bool {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	p, err := s.Auth.PermissionsOf(u.ID)
	return err == nil && p.Allows(perm)
}

// GET /api/nsfw/enabled: whether the NSFW 18+ section is set up, for an
// account allowed to see it.
func (s *Server) handleNsfwEnabled(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": s.nsfwEnabled()})
}
