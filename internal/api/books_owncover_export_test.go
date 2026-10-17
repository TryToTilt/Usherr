package api

import "net/http"

// TestSetCoverLinkClient makes "Change cover" download from a link with c
// instead of the client that refuses the home network, so a test can use a
// server on this machine.
func (s *Server) TestSetCoverLinkClient(c *http.Client) { s.coverLink = c }
