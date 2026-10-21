package opds

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestCacheVersionAndLimits(t *testing.T) {
	body := func(n int) Cached { return Cached{Body: []byte(strings.Repeat("x", n)), ETag: `"e"`} }
	tests := []struct {
		name string
		run  func(t *testing.T, c *Cache)
	}{
		{"hit then new version misses and drops", func(t *testing.T, c *Cache) {
			c.Put("a", 1, body(10))
			if _, ok := c.Lookup("a", 1); !ok {
				t.Fatal("same version should hit")
			}
			if _, ok := c.Lookup("a", 2); ok {
				t.Fatal("other version should miss")
			}
			if c.Len() != 0 {
				t.Fatalf("stale entry kept: %d", c.Len())
			}
		}},
		{"entry limit drops the least recently used", func(t *testing.T, c *Cache) {
			c.Put("a", 1, body(1))
			c.Put("b", 1, body(1))
			c.Put("c", 1, body(1))
			c.Lookup("a", 1) // a is now the freshest
			c.Put("d", 1, body(1))
			if _, ok := c.Lookup("b", 1); ok {
				t.Fatal("b should be gone")
			}
			if _, ok := c.Lookup("a", 1); !ok {
				t.Fatal("a should stay")
			}
			if c.Len() != 3 {
				t.Fatalf("len %d", c.Len())
			}
		}},
		{"byte limit", func(t *testing.T, c *Cache) {
			c.Put("a", 1, body(400))
			c.Put("b", 1, body(400))
			c.Put("c", 1, body(400))
			if c.Len() != 2 {
				t.Fatalf("len %d", c.Len())
			}
			c.Put("huge", 1, body(5000))
			if _, ok := c.Lookup("huge", 1); ok {
				t.Fatal("an oversized feed must not be kept")
			}
		}},
		{"replacing keeps the byte count right", func(t *testing.T, c *Cache) {
			for i := 0; i < 50; i++ {
				c.Put("a", 1, body(300))
			}
			if c.Len() != 1 || c.bytes > 400 {
				t.Fatalf("len %d bytes %d", c.Len(), c.bytes)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.run(t, NewCache(3, 1000)) })
	}
}

func TestCacheConcurrent(t *testing.T) {
	c := NewCache(8, 4096)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				k := fmt.Sprint(i % 20)
				if _, ok := c.Lookup(k, int64(i%3)); !ok {
					c.Put(k, int64(i%3), Cached{Body: []byte(k)})
				}
			}
		}()
	}
	wg.Wait()
	if c.Len() > 8 {
		t.Fatalf("len %d", c.Len())
	}
}
