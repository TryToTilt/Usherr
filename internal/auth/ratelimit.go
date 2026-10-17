package auth

import (
	"sync"
	"time"
)

// LoginLimiter blunts brute-force login attempts ("a
// handful of attempts per IP/window") with a simple in-memory sliding
// window. Deliberately not persisted to the DB: a restart resetting
// attempt counts is an acceptable tradeoff for avoiding a write on every
// single login attempt, and this only needs to survive for the duration
// of an actual brute-force burst, not across restarts.
type LoginLimiter struct {
	mu          sync.Mutex
	attempts    map[string][]time.Time
	maxAttempts int
	window      time.Duration
}

func NewLoginLimiter(maxAttempts int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{
		attempts:    make(map[string][]time.Time),
		maxAttempts: maxAttempts,
		window:      window,
	}
}

// Allow reports whether key (typically a client IP) is still under the
// attempt limit for the current window.
func (l *LoginLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	return len(l.attempts[key]) < l.maxAttempts
}

// RecordFailure counts one failed attempt against key.
func (l *LoginLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.add(key)
}

// Reserve counts one attempt against key before the slow check it guards, and
// reports whether there was room for it. The check and the count happen under
// one lock, so a burst of parallel requests cannot all see "no failures yet"
// and get past the limit. The caller keeps the attempt when the check fails
// and gives it back with Refund when it turns out fine.
func (l *LoginLimiter) Reserve(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	if len(l.attempts[key]) >= l.maxAttempts {
		return false
	}
	l.add(key)
	return true
}

// Refund takes back the newest attempt counted against key (see Reserve).
func (l *LoginLimiter) Refund(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	list := l.attempts[key]
	if len(list) <= 1 {
		delete(l.attempts, key)
		return
	}
	l.attempts[key] = list[:len(list)-1]
}

// add appends one attempt, keeping the table below maxTrackedKeys. Caller
// must hold l.mu.
func (l *LoginLimiter) add(key string) {
	if _, known := l.attempts[key]; !known && len(l.attempts) >= maxTrackedKeys {
		l.sweep() // many different sources at once: forget the ones whose window is over
		if len(l.attempts) >= maxTrackedKeys {
			l.evictOldest() // still full inside one window: make room instead of growing
		}
	}
	l.prune(key)
	l.attempts[key] = append(l.attempts[key], time.Now())
}

// evictOldest drops the key whose newest attempt is the oldest. Caller must
// hold l.mu.
func (l *LoginLimiter) evictOldest() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, list := range l.attempts {
		last := list[len(list)-1]
		if first || last.Before(oldest) {
			oldestKey, oldest, first = k, last, false
		}
	}
	if !first {
		delete(l.attempts, oldestKey)
	}
}

// RecordSuccess clears key's attempt history — a successful login isn't
// itself an attack signal, and shouldn't count toward the next window.
func (l *LoginLimiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// maxTrackedKeys bounds the memory a flood of distinct sources can use.
const maxTrackedKeys = 10000

// sweep prunes every key. Caller must hold l.mu.
func (l *LoginLimiter) sweep() {
	for k := range l.attempts {
		l.prune(k)
	}
}

// prune drops attempts older than the window. Caller must hold l.mu.
func (l *LoginLimiter) prune(key string) {
	cutoff := time.Now().Add(-l.window)
	kept := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.attempts, key)
	} else {
		l.attempts[key] = kept
	}
}
