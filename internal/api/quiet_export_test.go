package api

import (
	"context"
	"time"
)

// TestHeavy runs fn the way a heavy background job runs: not during the quiet
// time. It waits for the quiet time to end, and does not run fn when ctx ends first.
func (s *Server) TestHeavy(ctx context.Context, fn func()) {
	s.heavy(func(context.Context) { fn() })(ctx)
}

// SetQuietRecheck makes held jobs look at the quiet time this often, and
// returns a function that puts the old value back.
func SetQuietRecheck(d time.Duration) (restore func()) {
	old := quietRecheck
	quietRecheck = d
	return func() { quietRecheck = old }
}
