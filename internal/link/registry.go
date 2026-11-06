package link

import (
	"context"
	"errors"
	"sync"
	"time"
)

// The message types of step 1. Later steps add their own to the registry:
// screening.offer, screening.update, screening.cancel, rsvp, ready,
// playback, chat and reward (docs/home/link-protocol.md). Anything not in
// the registry is dropped before any handler runs.
const (
	TypeHello  = "link.hello"
	TypeUnlink = "link.unlink"
	TypeRename = "link.rename"
	TypePing   = "link.ping"
)

// Reserved names: step 2 registers the screening ones (package
// internal/link/screening); later steps the rest.
var Reserved = []string{"screening.offer", "screening.update", "screening.cancel", "screening.roster", "rsvp", "ready", "playback", "chat", "reward"}

// Incoming is one message from a linked Usherr, after its envelope was
// opened, its signature checked, and its time and id found fresh.
type Incoming struct {
	Peer Peer
	Msg  Message
	// Group is the scope of the screening key a group message was sealed
	// with (the screening's id); "" for a message in the pair's own
	// envelope.
	Group string
}

// Handler deals with one type of message. It sees only the message and the
// peer it came from.
type Handler func(ctx context.Context, in Incoming) error

// Kind is one message type: its name, the largest body it may have, and its
// handler.
type Kind struct {
	Type    string
	MaxBody int
	Handle  Handler
	// Group: the message comes only sealed with a screening key, from the
	// key's owner, fanned out to everyone at the screening. Every other type
	// comes only in the pair's own envelope.
	Group bool
}

// Registry is the fixed list of message types a Usherr understands.
type Registry struct {
	mu    sync.RWMutex
	kinds map[string]Kind
}

// NewRegistry makes an empty registry.
func NewRegistry() *Registry { return &Registry{kinds: map[string]Kind{}} }

// Register adds a message type. A type is registered once.
func (r *Registry) Register(k Kind) error {
	if !validType(k.Type) || k.Handle == nil || k.MaxBody <= 0 || k.MaxBody > MaxEnvelope {
		return errors.New("link: a message type needs a name, a size limit and a handler")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.kinds[k.Type]; dup {
		return errors.New("link: " + k.Type + " is registered already")
	}
	r.kinds[k.Type] = k
	return nil
}

// Lookup finds a message type.
func (r *Registry) Lookup(t string) (Kind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.kinds[t]
	return k, ok
}

// Types lists the registered types (link.hello tells the peer).
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.kinds))
	for t := range r.kinds {
		out = append(out, t)
	}
	return out
}

// How fresh a message must be.
const (
	MaxClockOff = 10 * time.Minute // a message's time may be this far off
	ReplayKeep  = 48 * time.Hour   // ids are remembered this long
	replayMax   = 200_000          // ids remembered at most
)

// ReplayCache remembers the ids of messages already taken in.
type ReplayCache struct {
	mu   sync.Mutex
	seen map[string]time.Time // id -> when to forget it
}

// NewReplayCache makes an empty cache.
func NewReplayCache() *ReplayCache { return &ReplayCache{seen: map[string]time.Time{}} }

// Fresh records key and reports whether it was new. When the cache is full
// of ids still to keep, it says no (a message is dropped rather than risk
// taking one in twice).
func (c *ReplayCache) Fresh(key string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if until, ok := c.seen[key]; ok && now.Before(until) {
		return false
	}
	if len(c.seen) >= replayMax {
		for k, until := range c.seen {
			if !now.Before(until) {
				delete(c.seen, k)
			}
		}
		if len(c.seen) >= replayMax {
			return false
		}
	}
	c.seen[key] = now.Add(ReplayKeep)
	return true
}
