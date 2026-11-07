package api

import "github.com/rdborg/mediarium/internal/auth"

// opdsMayAdult says whether this account may see 18+ series.
func (s *Server) opdsMayAdult(user *auth.User) bool {
	perms, err := s.Auth.PermissionsOf(user.ID)
	return err == nil && perms.Allows(auth.PermNSFW)
}
