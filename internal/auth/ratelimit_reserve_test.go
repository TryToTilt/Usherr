package auth_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
)

// A burst of parallel attempts against a slow check gets exactly the allowed
// number of tries, not however many fit before the first failure is written
// down.
func TestLoginLimiterReserveHoldsUnderABurst(t *testing.T) {
	limiter := auth.NewLoginLimiter(5, time.Minute)
	var checked atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !limiter.Reserve("1.2.3.4") {
				return
			}
			checked.Add(1)
			time.Sleep(50 * time.Millisecond) // a slow password check that fails
		}()
	}
	wg.Wait()
	if n := checked.Load(); n != 5 {
		t.Fatalf("%d attempts reached the slow check, want 5", n)
	}
	if limiter.Reserve("1.2.3.4") {
		t.Fatal("the failed tries must stay counted")
	}
}

func TestLoginLimiterRefundGivesOneTryBack(t *testing.T) {
	limiter := auth.NewLoginLimiter(2, time.Minute)
	for i := 0; i < 2; i++ {
		if !limiter.Reserve("k") {
			t.Fatal("two tries should fit")
		}
	}
	if limiter.Reserve("k") {
		t.Fatal("a third try should not fit")
	}
	limiter.Refund("k")
	if !limiter.Reserve("k") {
		t.Fatal("a refunded try should free a place")
	}
	limiter.Refund("k")
	limiter.Refund("k")
	limiter.Refund("k") // more refunds than tries must not break anything
	if !limiter.Reserve("k") {
		t.Fatal("an empty key should take tries again")
	}
}

// Flooding the limiter with different keys inside one window cannot make the
// table grow without end.
func TestLoginLimiterTableStaysBounded(t *testing.T) {
	limiter := auth.NewLoginLimiter(5, time.Hour)
	for i := 0; i < 11000; i++ {
		limiter.RecordFailure(fmt.Sprintf("name-%d", i))
	}
	if n := limiter.Size(); n > 10000 {
		t.Fatalf("%d keys tracked, want at most 10000", n)
	}
	// The newest keys are still counted.
	for i := 0; i < 4; i++ {
		limiter.RecordFailure("name-10999")
	}
	if limiter.Allow("name-10999") {
		t.Fatal("a recent key lost its count")
	}
}
