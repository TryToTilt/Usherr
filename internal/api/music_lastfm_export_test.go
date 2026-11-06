package api

import (
	"context"
	"net/http"
)

// TestSetLastFM points Last.fm calls at a fake server.
func (s *Server) TestSetLastFM(baseURL string) {
	s.music.lfMu.Lock()
	defer s.music.lfMu.Unlock()
	s.music.lfBase, s.music.lfHTTP = baseURL, http.DefaultClient
}

// RunScrobbleRetry runs the job that sends kept plays.
func (s *Server) RunScrobbleRetry() { s.scrobbleRetryJob(context.Background()) }
