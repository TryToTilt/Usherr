package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/store"
)

// sharedCache keeps one answer that is costly to work out on a big library
// (the Dashboard's counts, the sizes on the TV list) for a short while, and
// shares the work: callers that arrive while it is being worked out wait for
// that one instead of each running the same queries. The answer is dropped as
// soon as the library changes (store.LibraryVersion) or once it is older than
// ttl, whichever comes first; the ttl covers what the database cannot tell,
// such as a file appearing on disk.
type sharedCache[T any] struct {
	mu      sync.Mutex
	have    bool
	val     T
	at      time.Time
	version uint64
	running *sharedRun[T]
}

type sharedRun[T any] struct {
	done chan struct{}
	val  T
	err  error
}

// get returns the kept answer while it is fresh, or works it out with compute.
// compute runs with the context of the caller that started it; if that caller
// gives up, the queries stop, and the others who were waiting start again on
// their own.
func (c *sharedCache[T]) get(ctx context.Context, ttl time.Duration, compute func(context.Context) (T, error)) (T, error) {
	var zero T
	for {
		version := store.LibraryVersion()
		c.mu.Lock()
		if c.have && c.version == version && time.Since(c.at) < ttl {
			v := c.val
			c.mu.Unlock()
			return v, nil
		}
		if run := c.running; run != nil {
			c.mu.Unlock()
			select {
			case <-run.done:
			case <-ctx.Done():
				return zero, ctx.Err()
			}
			if run.err == nil {
				return run.val, nil
			}
			if ctx.Err() != nil {
				return zero, ctx.Err()
			}
			if errors.Is(run.err, context.Canceled) || errors.Is(run.err, context.DeadlineExceeded) {
				continue // the caller who was working it out left; try again
			}
			return zero, run.err
		}
		run := &sharedRun[T]{done: make(chan struct{})}
		c.running = run
		c.mu.Unlock()

		func() {
			defer func() { // also when compute panics, so nobody waits for ever
				c.mu.Lock()
				if run.err == nil {
					c.have, c.val, c.at, c.version = true, run.val, time.Now(), version
				}
				c.running = nil
				c.mu.Unlock()
				close(run.done)
			}()
			run.err = errors.New("the answer could not be worked out")
			run.val, run.err = compute(ctx)
		}()
		return run.val, run.err
	}
}

// forget drops the kept answer (tests, and changes the library version does
// not see).
func (c *sharedCache[T]) forget() {
	c.mu.Lock()
	c.have = false
	c.mu.Unlock()
}
