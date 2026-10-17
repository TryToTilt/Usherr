package musicbrainz

import (
	"context"
	"errors"
	"testing"
	"time"
)

// One caller cannot book the limit far into the future: past a short queue
// the next ones are turned away, and a caller whose deadline comes before its
// turn does not take a slot at all.
func TestTokenBucketDoesNotBookFarAhead(t *testing.T) {
	b := newTokenBucket(time.Hour) // never reached: only the booking matters
	ctx := context.Background()
	if err := b.wait(ctx); err != nil { // the first token is free now
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := b.wait(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a caller that cannot be served before its deadline got %v", err)
	}
	b.mu.Lock()
	booked := b.next
	b.mu.Unlock()
	if time.Until(booked) > time.Hour+time.Minute {
		t.Fatalf("the rejected caller still booked a slot: next is %v away", time.Until(booked))
	}

	// A caller that gives up while waiting hands its slot back.
	b2 := newTokenBucket(50 * time.Millisecond)
	_ = b2.wait(ctx)
	before := func() time.Time { b2.mu.Lock(); defer b2.mu.Unlock(); return b2.next }
	was := before()
	gone, cancel2 := context.WithCancel(ctx)
	cancel2()
	_ = b2.wait(gone)
	if !before().Equal(was) {
		t.Fatalf("a cancelled caller kept its slot: %v -> %v", was, before())
	}

	// A long queue is refused.
	b3 := newTokenBucket(time.Hour)
	b3.next = time.Now().Add(time.Duration(maxQueuedSlots+1) * time.Hour)
	if err := b3.wait(ctx); !errors.Is(err, ErrBusy) {
		t.Fatalf("a caller behind a long queue got %v", err)
	}
}
