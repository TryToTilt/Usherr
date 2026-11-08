package api

import (
	"context"
	"time"
)

// CrewJobForTest runs the crew job once, as if a few minutes had passed
// (everything it does every five minutes too).
func (s *Server) CrewJobForTest(now time.Time) {
	s.linkS.crew.mu.Lock()
	s.linkS.crew.tick = 0
	s.linkS.crew.mu.Unlock()
	s.crewJob(context.Background(), now)
}
