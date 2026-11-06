package link

import (
	"context"
	"sync"
	"time"
)

// Peer is a linked Usherr.
type Peer struct {
	ID       string    // its identity (Ed25519 public key, base64url)
	XPub     []byte    // its X25519 public key
	Name     string    // what it calls itself (link.hello, link.rename)
	Version  string    // its Usherr version
	Relay    string    // the relay the link uses
	LastSeen time.Time // the last message from it
}

// Store keeps the links and the invites (Usherr keeps them in its database).
type Store interface {
	// Peer finds a linked Usherr by its identity.
	Peer(ctx context.Context, id string) (Peer, bool, error)
	// Peers lists the linked Usherrs.
	Peers(ctx context.Context) ([]Peer, error)
	// SavePeer adds a link, or updates one (keys, name, version, relay).
	SavePeer(ctx context.Context, p Peer) error
	// ForgetPeer ends a link: its keys are forgotten. by is "me" or "them".
	ForgetPeer(ctx context.Context, id, by string) error
	// SeenPeer records that a message came from a linked Usherr.
	SeenPeer(ctx context.Context, id string, at time.Time) error
	// Invites lists the invites not finished yet (both roles).
	Invites(ctx context.Context) ([]Invite, error)
	// Invite finds an invite by its mailbox, finished or not.
	Invite(ctx context.Context, box string) (Invite, bool, error)
	// SaveInvite adds an invite, or replaces the one with the same mailbox
	// (a code typed in again).
	SaveInvite(ctx context.Context, in Invite) error
	// FinishInvite marks an invite finished with how it ended. It reports
	// false when it was finished already (one use only).
	FinishInvite(ctx context.Context, box, state string) (bool, error)
}

// MemStore is a Store in memory (tests, and the conformance test).
type MemStore struct {
	mu      sync.Mutex
	peers   map[string]Peer
	invites map[string]Invite
	Forgot  map[string]string // id -> by
}

// NewMemStore makes an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{peers: map[string]Peer{}, invites: map[string]Invite{}, Forgot: map[string]string{}}
}

func (m *MemStore) Peer(_ context.Context, id string) (Peer, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.peers[id]
	return p, ok, nil
}

func (m *MemStore) Peers(context.Context) ([]Peer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Peer, 0, len(m.peers))
	for _, p := range m.peers {
		out = append(out, p)
	}
	return out, nil
}

func (m *MemStore) SavePeer(_ context.Context, p Peer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers[p.ID] = p
	return nil
}

func (m *MemStore) ForgetPeer(_ context.Context, id, by string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.peers, id)
	m.Forgot[id] = by
	return nil
}

func (m *MemStore) SeenPeer(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.peers[id]; ok {
		p.LastSeen = at
		m.peers[id] = p
	}
	return nil
}

func (m *MemStore) Invites(context.Context) ([]Invite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Invite
	for _, in := range m.invites {
		if !in.Used {
			out = append(out, in)
		}
	}
	return out, nil
}

func (m *MemStore) Invite(_ context.Context, box string) (Invite, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	in, ok := m.invites[box]
	return in, ok, nil
}

func (m *MemStore) SaveInvite(_ context.Context, in Invite) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invites[in.Box] = in
	return nil
}

func (m *MemStore) FinishInvite(_ context.Context, box, state string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	in, ok := m.invites[box]
	if !ok || in.Used {
		return false, nil
	}
	in.Used, in.State = true, state
	m.invites[box] = in
	return true, nil
}
