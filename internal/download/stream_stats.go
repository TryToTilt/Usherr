package download

// Usherr: what the streaming engine is using, for the Streaming card in
// Settings > Extras.

import (
	"sort"
	"sync/atomic"
)

// streamBytes counts the video bytes handed to players since start.
var streamBytes atomic.Int64

// StreamBytes is how many video bytes streams have served in all.
func StreamBytes() int64 { return streamBytes.Load() }

// ServerUse is one news server's connections: those in use right now (by
// streams and downloads together), the places the provider allows, and
// the idle ones streams keep ready.
type ServerUse struct {
	Host      string `json:"host"`
	InUse     int    `json:"inUse"`
	Allowed   int    `json:"allowed"`
	Configure int    `json:"configured"`
	Idle      int    `json:"idle"`
}

// StreamServerUse lists the connection use of every server seen so far.
func StreamServerUse() []ServerUse {
	gatesMu.Lock()
	gs := make([]*serverGate, 0, len(gates))
	for _, g := range gates {
		gs = append(gs, g)
	}
	gatesMu.Unlock()
	pool.mu.Lock()
	idle := map[gateKey]int{}
	for k, l := range pool.idle {
		idle[k] = len(l)
	}
	pool.mu.Unlock()
	out := make([]ServerUse, 0, len(gs))
	for _, g := range gs {
		g.mu.Lock()
		out = append(out, ServerUse{Host: g.key.host, InUse: g.inUse, Allowed: g.learned, Configure: g.configured, Idle: idle[g.key]})
		g.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}
