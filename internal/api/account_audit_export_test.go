package api

import "context"

// TestRunWatchlistJob runs the scheduled Plex watchlist check once, for
// account_audit_test.go (package api_test).
func (s *Server) TestRunWatchlistJob() { s.watchlistJob(context.Background()) }
