package metadata

import (
	"sync"
	"testing"
)

// Changing the key while requests read it must not race (run with -race).
func TestAPIKeyCanChangeWhileRead(t *testing.T) {
	c := New("first")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = c.key()
				_ = c.HasAPIKey()
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				c.SetAPIKey("second")
			}
		}()
	}
	wg.Wait()
	if c.key() != "second" {
		t.Fatalf("key = %q", c.key())
	}
}
