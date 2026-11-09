package api

import "time"

// TestShiftTVClock moves the clock of TV sign-in codes and phone links forward.
func (s *Server) TestShiftTVClock(d time.Duration) {
	st := s.tvs()
	st.mu.Lock()
	st.shift += d
	st.mu.Unlock()
}
