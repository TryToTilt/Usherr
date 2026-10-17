package api

import "context"

// TestRefreshAllSeries runs the scheduled episode-list refresh now, for
// series_refresh_stop_test.go (package api_test).
func (s *Server) TestRefreshAllSeries() { s.refreshAllSeries(context.Background()) }
