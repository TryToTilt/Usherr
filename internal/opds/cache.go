package opds

import (
	"container/list"
	"sync"
)

// Cached is one built feed, ready to send.
type Cached struct {
	Body        []byte
	ContentType string
	ETag        string
}

type cacheItem struct {
	key     string
	version int64
	val     Cached
}

// Cache keeps built feeds in memory. Every entry remembers the version of the
// data it was built from; asking with another version is a miss and drops the
// entry. The cache holds at most MaxEntries feeds and MaxBytes of bodies, the
// least recently used going first. It is safe for concurrent use.
type Cache struct {
	MaxEntries int
	MaxBytes   int

	mu    sync.Mutex
	order *list.List // front = most recently used
	items map[string]*list.Element
	bytes int
}

// NewCache returns a cache with the given limits.
func NewCache(maxEntries, maxBytes int) *Cache {
	return &Cache{MaxEntries: maxEntries, MaxBytes: maxBytes, order: list.New(), items: map[string]*list.Element{}}
}

// Lookup returns the feed stored under key if it was built from version.
func (c *Cache) Lookup(key string, version int64) (Cached, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return Cached{}, false
	}
	it := el.Value.(*cacheItem)
	if it.version != version {
		c.removeLocked(el)
		return Cached{}, false
	}
	c.order.MoveToFront(el)
	return it.val, true
}

// Put stores a feed. One bigger than the byte limit is not kept.
func (c *Cache) Put(key string, version int64, val Cached) {
	size := len(val.Body) + len(key)
	if size > c.MaxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.removeLocked(el)
	}
	c.items[key] = c.order.PushFront(&cacheItem{key: key, version: version, val: val})
	c.bytes += size
	for c.order.Len() > c.MaxEntries || c.bytes > c.MaxBytes {
		c.removeLocked(c.order.Back())
	}
}

// Len is the number of feeds held.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

func (c *Cache) removeLocked(el *list.Element) {
	it := el.Value.(*cacheItem)
	c.order.Remove(el)
	delete(c.items, it.key)
	c.bytes -= len(it.val.Body) + len(it.key)
}
