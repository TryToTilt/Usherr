package link

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
)

// DefaultRelay is the relay the Usherr project runs for everyone.
const DefaultRelay = "wss://relay.usherr.app"

// Errors from Join.
var (
	ErrOwnCode     = errors.New("That's a code from this Usherr. Send it to your friend, and paste theirs here.")
	ErrCodeGone    = errors.New("That code doesn't work anymore: it was used already, cancelled, or it's more than 24 hours old. Ask your friend for a new one.")
	ErrTooFast     = errors.New("Too many tries. Wait a minute and try again.")
	ErrRelayDown   = errors.New("Can't reach the relay right now. Try again in a minute.")
	ErrNotLinked   = errors.New("That Usherr isn't linked.")
	ErrUnknownType = errors.New("link: that message type isn't registered")
)

// RefusedError: the inviting Usherr said no (the code was used, expired or
// cancelled).
type RefusedError struct{ Why string }

func (e RefusedError) Error() string {
	switch e.Why {
	case "used":
		return "That code was used already. Ask your friend for a new one."
	case "expired":
		return "That code is more than 24 hours old. Ask your friend for a new one."
	default:
		return "Your friend cancelled that code. Ask them for a new one."
	}
}

// NodeOptions sets up a node.
type NodeOptions struct {
	Identity     *Identity
	Store        Store
	Relay        string // this Usherr's relay
	DefaultRelay string // "" for DefaultRelay
	Name         func() string
	Version      string
	// Dial picks how to reach a relay (nil: DefaultDial).
	Dial func(relay string) DialFunc
	Now  func() time.Time
	// Logf logs what happened, never keys, codes or what messages say.
	Logf func(format string, args ...any)
	// OnChange is told when links or the connection change.
	OnChange  func()
	PingEvery time.Duration // tests
	// GroupKey finds a screening key this Usherr was given, by its id (nil:
	// group messages are dropped).
	GroupKey func(ctx context.Context, kid []byte) (GroupKey, bool)
}

// GroupKey is a screening key a host gave this Usherr: the key, the host's
// identity (the only one who may seal with it) and its scope (the
// screening's id), which handlers check the message against.
type GroupKey struct {
	Key   []byte
	Owner string
	Scope string
}

// Node is this Usherr's end of its links: its relay connections, the
// handshakes, and taking in messages (open, check, then hand to a handler).
type Node struct {
	opt    NodeOptions
	reg    *Registry
	replay *ReplayCache

	mu      sync.Mutex
	closed  bool
	clients map[string]*Client
	pending map[string]Invite            // joins posted, not saved yet
	waiters map[string]chan joinResult   // box -> the Join waiting for an answer
	drops   map[string]int               // dropped messages, by reason
	pings   map[string][2]time.Time      // peer -> last ping, last echo
	hellos  map[string]time.Time         // peer -> last hello sent
	onRecv  func(peer string, m Message) // tests
}

type joinResult struct {
	peer Peer
	err  error
}

// JoinResult is how a Join ended: linked, or still waiting for the other
// Usherr (it's offline; the relay keeps the request for 24 hours).
type JoinResult struct {
	State string // linked, waiting
	Peer  Peer
}

// NewNode makes a node; Start connects it.
func NewNode(opt NodeOptions) *Node {
	if opt.DefaultRelay == "" {
		opt.DefaultRelay = DefaultRelay
	}
	if opt.Relay == "" {
		opt.Relay = opt.DefaultRelay
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Name == nil {
		opt.Name = func() string { return "Usherr" }
	}
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	n := &Node{
		opt: opt, reg: NewRegistry(), replay: NewReplayCache(),
		clients: map[string]*Client{}, pending: map[string]Invite{}, waiters: map[string]chan joinResult{},
		drops: map[string]int{}, pings: map[string][2]time.Time{}, hellos: map[string]time.Time{},
	}
	n.registerBuiltins()
	return n
}

// Registry is the node's list of message types; later steps add theirs.
func (n *Node) Registry() *Registry { return n.reg }

// Identity is this Usherr's identity.
func (n *Node) Identity() *Identity { return n.opt.Identity }

// Relay is this Usherr's own relay.
func (n *Node) Relay() string { return n.opt.Relay }

// Start connects to this Usherr's relay and to the relays its links and
// invites use, and opens the invite mailboxes still waiting.
func (n *Node) Start(ctx context.Context) {
	n.client(n.opt.Relay)
	if peers, err := n.opt.Store.Peers(ctx); err == nil {
		for _, p := range peers {
			n.client(p.Relay)
		}
	}
	n.openInvites(ctx)
}

func (n *Node) openInvites(ctx context.Context) {
	invites, err := n.opt.Store.Invites(ctx)
	if err != nil {
		return
	}
	now := n.opt.Now()
	for _, in := range invites {
		if in.Role == "host" && now.Before(in.Expires) {
			if c := n.client(in.Relay); c != nil {
				c.OpenBox(in.Box, in.Expires)
			}
		}
		if in.Role == "join" && now.Before(in.Created.Add(2*InviteLife)) {
			n.client(in.Relay)
		}
	}
}

// Close disconnects from every relay.
func (n *Node) Close() {
	n.mu.Lock()
	n.closed = true
	clients := n.clients
	n.clients = map[string]*Client{}
	n.mu.Unlock()
	for _, c := range clients {
		c.Close()
	}
}

// client is the connection to a relay, started if needed.
func (n *Node) client(relay string) *Client {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed || relay == "" {
		return nil
	}
	if c, ok := n.clients[relay]; ok {
		return c
	}
	dial := DialFunc(DefaultDial)
	if n.opt.Dial != nil {
		dial = n.opt.Dial(relay)
	}
	c := NewClient(ClientOptions{
		URL: relay, Identity: n.opt.Identity, Dial: dial, Now: n.opt.Now, PingEvery: n.opt.PingEvery,
		OnFrame:  n.onFrame,
		OnReady:  n.onReady,
		OnStatus: n.changed,
		OnReject: func(mid, code string) { n.opt.Logf("link: the relay refused a message (%s)", code) },
	})
	n.clients[relay] = c
	c.Start()
	return c
}

func (n *Node) changed() {
	if n.opt.OnChange != nil {
		n.opt.OnChange()
	}
}

// Status is the connection to this Usherr's own relay.
func (n *Node) Status() Status {
	n.mu.Lock()
	c := n.clients[n.opt.Relay]
	n.mu.Unlock()
	if c == nil {
		return Status{State: "off"}
	}
	return c.Status()
}

// RelayStatus is the connection to one relay ("off" when there's none).
func (n *Node) RelayStatus(relay string) Status {
	n.mu.Lock()
	c := n.clients[relay]
	n.mu.Unlock()
	if c == nil {
		return Status{State: "off"}
	}
	return c.Status()
}

// WaitConnected waits a little for this Usherr's relay to connect.
func (n *Node) WaitConnected(ctx context.Context, most time.Duration) bool {
	return n.waitRelay(ctx, n.opt.Relay, most)
}

func (n *Node) waitRelay(ctx context.Context, relay string, most time.Duration) bool {
	c := n.client(relay)
	if c == nil {
		return false
	}
	deadline := time.Now().Add(most)
	for {
		if c.Status().State == "connected" {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Dropped is how many messages were dropped for a reason: open (can't be
// opened or the signature is wrong), stale (its time is too far off),
// replay, type (not in the registry, or not in the envelope its type comes
// in), size, sender (not linked, or not the screening key's owner), group
// (sealed with a screening key this Usherr doesn't have), refused (its
// handler said no: not the host, not invited, a body that can't be read).
func (n *Node) Dropped(reason string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.drops[reason]
}

func (n *Node) drop(reason string) {
	n.mu.Lock()
	n.drops[reason]++
	n.mu.Unlock()
}

// LastPing is when a peer last pinged this Usherr, and when it last answered
// a ping from here.
func (n *Node) LastPing(peer string) (got, echoed time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	p := n.pings[peer]
	return p[0], p[1]
}

// onReady says hello to the links on a relay that just connected (at most
// every 12 hours each, so names and versions stay fresh).
func (n *Node) onReady(c *Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	peers, err := n.opt.Store.Peers(ctx)
	if err != nil {
		return
	}
	now := n.opt.Now()
	for _, p := range peers {
		if p.Relay != c.URL() {
			continue
		}
		n.mu.Lock()
		last := n.hellos[p.ID]
		n.mu.Unlock()
		if now.Sub(last) > 12*time.Hour {
			_ = n.sendTo(c, p, TypeHello, n.helloBody())
		}
	}
}

func (n *Node) onFrame(c *Client, f Frame) {
	env, err := base64.StdEncoding.DecodeString(f.Env)
	if err != nil || len(env) > MaxEnvelope {
		n.drop("size")
		return
	}
	if _, err := DecodeID(f.From); err != nil {
		n.drop("sender")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch f.T {
	case FMsg:
		n.receive(ctx, c, f.From, env)
	case FInviteMsg:
		if ValidBox(f.Box) {
			n.inviteMsg(ctx, c, f.Box, f.From, env)
		}
	}
}

// receive takes in an envelope from a linked Usherr: open it, check the
// signature, the time and the id, and the type, and only then hand it to
// the type's handler.
func (n *Node) receive(ctx context.Context, c *Client, from string, env []byte) {
	peer, ok, err := n.opt.Store.Peer(ctx, from)
	if err != nil {
		return
	}
	if !ok {
		if !n.joinReply(ctx, c, from, env) {
			n.drop("sender")
		}
		return
	}
	pub, _ := DecodeID(peer.ID)
	if kid, group := GroupEnvelopeKID(env); group {
		n.receiveGroup(ctx, peer, pub, kid, env)
		return
	}
	m, err := OpenMessage(n.opt.Identity, pub, peer.XPub, env)
	if err != nil {
		n.drop("open")
		return
	}
	n.take(ctx, peer, m, "")
}

// receiveGroup takes in an envelope sealed with a screening key: the key
// must be one this Usherr was given by the sender (the relay checked who
// sent it), and the message must be signed by the sender.
func (n *Node) receiveGroup(ctx context.Context, peer Peer, pub []byte, kid, env []byte) {
	if n.opt.GroupKey == nil {
		n.drop("group")
		return
	}
	gk, ok := n.opt.GroupKey(ctx, kid)
	if !ok {
		n.drop("group")
		return
	}
	if gk.Owner != peer.ID {
		n.drop("sender")
		return
	}
	m, err := OpenGroup(gk.Key, pub, env)
	if err != nil {
		n.drop("open")
		return
	}
	if gk.Scope == "" {
		n.drop("group")
		return
	}
	n.take(ctx, peer, m, gk.Scope)
}

// take checks an opened message's time, type, size and id, and only then
// hands it to the type's handler. group is the screening key's scope for a
// group message.
func (n *Node) take(ctx context.Context, peer Peer, m Message, group string) {
	now := n.opt.Now()
	if d := now.Sub(time.UnixMilli(m.At)); d > MaxClockOff || d < -MaxClockOff {
		n.drop("stale")
		return
	}
	kind, ok := n.reg.Lookup(m.Type)
	if !ok || kind.Group != (group != "") {
		n.drop("type")
		return
	}
	if len(m.Body) > kind.MaxBody {
		n.drop("size")
		return
	}
	if !n.replay.Fresh(peer.ID+"/"+m.ID, now) {
		n.drop("replay")
		return
	}
	_ = n.opt.Store.SeenPeer(ctx, peer.ID, now)
	peer.LastSeen = now
	n.mu.Lock()
	hook := n.onRecv
	n.mu.Unlock()
	if hook != nil {
		hook(peer.ID, m)
	}
	if err := kind.Handle(ctx, Incoming{Peer: peer, Msg: m, Group: group}); err != nil {
		n.drop("refused")
		n.opt.Logf("link: a %s message from a linked Usherr couldn't be used", m.Type)
	}
}

// Send sends a message of a registered type to a linked Usherr.
func (n *Node) Send(ctx context.Context, peerID, typ string, body any) error {
	peer, ok, err := n.opt.Store.Peer(ctx, peerID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotLinked
	}
	if k, ok := n.reg.Lookup(typ); !ok || k.Group {
		return ErrUnknownType
	}
	c := n.client(peer.Relay)
	if c == nil {
		return ErrRelayDown
	}
	return n.sendTo(c, peer, typ, body)
}

func (n *Node) sendTo(c *Client, peer Peer, typ string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	env, err := n.seal(peer, typ, b)
	if err != nil {
		return err
	}
	if typ == TypeHello {
		n.mu.Lock()
		n.hellos[peer.ID] = n.opt.Now()
		n.mu.Unlock()
	}
	_, err = c.Send(peer.ID, env)
	return err
}

func (n *Node) seal(peer Peer, typ string, body []byte) ([]byte, error) {
	pub, err := DecodeID(peer.ID)
	if err != nil {
		return nil, err
	}
	return SealMessage(n.opt.Identity, pub, peer.XPub, Message{Type: typ, ID: NewMessageID(), At: n.opt.Now().UnixMilli(), Body: body})
}

// SendGroup seals a message of a group type once with a screening key and
// has the relay deliver it to each linked Usherr in peerIDs (one fanout
// frame per relay and per FanoutBatch recipients; one send each through a
// relay that doesn't know fanout). Identities that aren't linked are
// skipped.
func (n *Node) SendGroup(ctx context.Context, key []byte, peerIDs []string, typ string, body any) error {
	if k, ok := n.reg.Lookup(typ); !ok || !k.Group {
		return ErrUnknownType
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	env, err := SealGroup(n.opt.Identity, key, Message{Type: typ, ID: NewMessageID(), At: n.opt.Now().UnixMilli(), Body: b})
	if err != nil {
		return err
	}
	byRelay := map[string][]string{}
	seen := map[string]bool{}
	for _, id := range peerIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		p, ok, err := n.opt.Store.Peer(ctx, id)
		if err != nil {
			return err
		}
		if ok {
			byRelay[p.Relay] = append(byRelay[p.Relay], p.ID)
		}
	}
	var first error
	for relay, ids := range byRelay {
		c := n.client(relay)
		if c == nil {
			first = cmp.Or(first, ErrRelayDown)
			continue
		}
		for len(ids) > 0 {
			part := ids[:min(len(ids), FanoutBatch)]
			ids = ids[len(part):]
			if err := c.SendMany(part, env); err != nil {
				first = cmp.Or(first, err)
			}
		}
	}
	return first
}

// SendRaw sends an envelope as it is (tests: tampered, replayed or forged
// envelopes).
func (n *Node) SendRaw(relay, to string, env []byte) error {
	c := n.client(relay)
	if c == nil {
		return ErrRelayDown
	}
	_, err := c.Send(to, env)
	return err
}

// Seal makes an envelope for a linked Usherr without sending it (tests).
func (n *Node) Seal(ctx context.Context, peerID string, m Message) ([]byte, error) {
	peer, ok, err := n.opt.Store.Peer(ctx, peerID)
	if err != nil || !ok {
		return nil, ErrNotLinked
	}
	pub, _ := DecodeID(peer.ID)
	return SealMessage(n.opt.Identity, pub, peer.XPub, m)
}

// Ping asks a linked Usherr to answer.
func (n *Node) Ping(ctx context.Context, peerID string) error {
	return n.Send(ctx, peerID, TypePing, pingBody{})
}

// Unlink ends a link: the other Usherr is told, then its keys are
// forgotten here. A message from it afterwards is dropped.
func (n *Node) Unlink(ctx context.Context, peerID string) error {
	peer, ok, err := n.opt.Store.Peer(ctx, peerID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotLinked
	}
	if c := n.client(peer.Relay); c != nil {
		_ = n.sendTo(c, peer, TypeUnlink, struct{}{})
	}
	if err := n.opt.Store.ForgetPeer(ctx, peer.ID, "me"); err != nil {
		return err
	}
	n.changed()
	return nil
}

// Renamed tells every linked Usherr this Usherr's new name.
func (n *Node) Renamed(ctx context.Context) {
	peers, err := n.opt.Store.Peers(ctx)
	if err != nil {
		return
	}
	for _, p := range peers {
		if c := n.client(p.Relay); c != nil {
			_ = n.sendTo(c, p, TypeRename, renameBody{Name: CleanName(n.opt.Name())})
		}
	}
}

// CreateInvite makes a one-time code that links another Usherr with this
// one, valid for 24 hours.
func (n *Node) CreateInvite(ctx context.Context) (Invite, string, error) {
	secret := NewSecret()
	now := n.opt.Now()
	in := Invite{Box: BoxOf(secret), Secret: secret, Relay: n.opt.Relay, Role: "host", Created: now, Expires: now.Add(InviteLife)}
	if err := n.opt.Store.SaveInvite(ctx, in); err != nil {
		return Invite{}, "", err
	}
	if c := n.client(in.Relay); c != nil {
		// Open before the code is shown, so it works at once. When the relay
		// isn't connected, the mailbox opens as soon as it is.
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := c.OpenBoxWait(wctx, in.Box, in.Expires)
		cancel()
		if err != nil && !errors.Is(err, ErrNotConnected) && !errors.Is(err, context.DeadlineExceeded) {
			_, _ = n.opt.Store.FinishInvite(ctx, in.Box, "cancelled")
			return Invite{}, "", err
		}
	}
	return in, n.CodeOf(in), nil
}

// CodeOf writes an invite's code.
func (n *Node) CodeOf(in Invite) string {
	return EncodeCode(in.Secret, RelayInCode(in.Relay, n.opt.DefaultRelay))
}

// CancelInvite stops a code from working (or forgets a join still waiting).
func (n *Node) CancelInvite(ctx context.Context, box string) error {
	in, ok, err := n.opt.Store.Invite(ctx, box)
	if err != nil || !ok {
		return err
	}
	if _, err := n.opt.Store.FinishInvite(ctx, box, "cancelled"); err != nil {
		return err
	}
	if in.Role == "host" {
		if c := n.client(in.Relay); c != nil {
			c.CloseBox(box)
		}
	}
	n.mu.Lock()
	if ch, ok := n.waiters[box]; ok {
		delete(n.waiters, box)
		close(ch)
	}
	n.mu.Unlock()
	return nil
}

// Join uses a friend's code: the request goes to their Usherr's mailbox,
// and Join waits up to wait for the answer.
func (n *Node) Join(ctx context.Context, code string, wait time.Duration) (JoinResult, error) {
	secret, relay, err := DecodeCode(code)
	if err != nil {
		return JoinResult{}, err
	}
	if relay == "" {
		relay = n.opt.DefaultRelay
	}
	box := BoxOf(secret)
	if old, ok, err := n.opt.Store.Invite(ctx, box); err != nil {
		return JoinResult{}, err
	} else if ok && old.Role == "host" {
		return JoinResult{}, ErrOwnCode
	} else if ok && old.Used && old.State == "linked" {
		return JoinResult{}, RefusedError{Why: "used"}
	}
	if !n.waitRelay(ctx, relay, 10*time.Second) {
		return JoinResult{}, ErrRelayDown
	}
	c := n.client(relay)
	env, err := NewJoinRequest(n.opt.Identity, secret, CleanName(n.opt.Name()), n.opt.Version)
	if err != nil {
		return JoinResult{}, err
	}
	now := n.opt.Now()
	in := Invite{Box: box, Secret: secret, Relay: relay, Role: "join", Created: now, Expires: now.Add(InviteLife)}
	ch := make(chan joinResult, 1)
	n.mu.Lock()
	n.pending[box] = in
	n.waiters[box] = ch
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.pending, box)
		if n.waiters[box] == ch {
			delete(n.waiters, box)
		}
		n.mu.Unlock()
	}()
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	resp, err := c.Request(rctx, Frame{T: FInvitePost, Box: box, Env: base64.StdEncoding.EncodeToString(env)})
	cancel()
	if err != nil {
		return JoinResult{}, ErrRelayDown
	}
	if resp.T == FErr {
		switch resp.Code {
		case CodeUnknown:
			return JoinResult{}, ErrCodeGone
		case CodeRate:
			return JoinResult{}, ErrTooFast
		default:
			return JoinResult{}, ErrRelayDown
		}
	}
	// Kept, so an answer that comes after a restart still links. Unless the
	// answer was quicker than this.
	n.mu.Lock()
	if _, still := n.pending[box]; still {
		err = n.opt.Store.SaveInvite(ctx, in)
	}
	n.mu.Unlock()
	if err != nil {
		return JoinResult{}, err
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case r, ok := <-ch:
		if !ok {
			return JoinResult{}, RefusedError{Why: "cancelled"}
		}
		if r.err != nil {
			return JoinResult{}, r.err
		}
		return JoinResult{State: "linked", Peer: r.peer}, nil
	case <-t.C:
		return JoinResult{State: "waiting"}, nil
	case <-ctx.Done():
		return JoinResult{State: "waiting"}, nil
	}
}

// joinReply tries an envelope from an Usherr not linked yet as the answer
// to one of this Usherr's joins.
func (n *Node) joinReply(ctx context.Context, c *Client, from string, env []byte) bool {
	type cand struct {
		in      Invite
		pending bool
	}
	var cands []cand
	n.mu.Lock()
	for _, in := range n.pending {
		if in.Relay == c.URL() {
			cands = append(cands, cand{in, true})
		}
	}
	n.mu.Unlock()
	if list, err := n.opt.Store.Invites(ctx); err == nil {
		for _, in := range list {
			if in.Role == "join" && in.Relay == c.URL() {
				cands = append(cands, cand{in, false})
			}
		}
	}
	for _, cd := range cands {
		in := cd.in
		rep, x, err := OpenJoinReply(n.opt.Identity, in.Secret, from, env)
		if err != nil {
			continue
		}
		if cd.pending {
			n.mu.Lock()
			_, still := n.pending[in.Box]
			delete(n.pending, in.Box)
			n.mu.Unlock()
			if still {
				_ = n.opt.Store.SaveInvite(ctx, in)
			}
		}
		if !rep.OK {
			if first, _ := n.opt.Store.FinishInvite(ctx, in.Box, rep.Why); first {
				n.answer(in.Box, joinResult{err: RefusedError{Why: rep.Why}})
				n.changed()
			}
			return true
		}
		first, err := n.opt.Store.FinishInvite(ctx, in.Box, "linked")
		if err != nil || !first {
			return true
		}
		peer := Peer{ID: from, XPub: x, Name: CleanName(rep.Name), Version: cleanVersion(rep.Version), Relay: in.Relay, LastSeen: n.opt.Now()}
		if err := n.opt.Store.SavePeer(ctx, peer); err != nil {
			return true
		}
		n.opt.Logf("link: linked with another Usherr")
		_ = n.sendTo(c, peer, TypeHello, n.helloBody())
		n.answer(in.Box, joinResult{peer: peer})
		n.changed()
		return true
	}
	return false
}

func (n *Node) answer(box string, r joinResult) {
	n.mu.Lock()
	ch, ok := n.waiters[box]
	delete(n.waiters, box)
	n.mu.Unlock()
	if ok {
		ch <- r
	}
}

// inviteMsg is the host side: a join request came to one of this Usherr's
// mailboxes.
func (n *Node) inviteMsg(ctx context.Context, c *Client, box, from string, env []byte) {
	in, ok, err := n.opt.Store.Invite(ctx, box)
	if err != nil || !ok || in.Role != "host" {
		n.drop("sender")
		return
	}
	req, x, err := OpenJoinRequest(in.Secret, from, env)
	if err != nil {
		n.drop("open")
		return
	}
	refuse := func(why string) {
		if out, err := NewJoinRefusal(in.Secret, why); err == nil {
			_, _ = c.Send(from, out)
		}
	}
	now := n.opt.Now()
	switch {
	case in.Used && in.State == "cancelled":
		refuse("cancelled")
		return
	case in.Used:
		refuse("used")
		return
	case !now.Before(in.Expires):
		_, _ = n.opt.Store.FinishInvite(ctx, box, "expired")
		c.CloseBox(box)
		refuse("expired")
		return
	}
	first, err := n.opt.Store.FinishInvite(ctx, box, "linked")
	if err != nil {
		return
	}
	if !first {
		refuse("used")
		return
	}
	peer := Peer{ID: from, XPub: x, Name: CleanName(req.Name), Version: cleanVersion(req.Version), Relay: in.Relay, LastSeen: now}
	if err := n.opt.Store.SavePeer(ctx, peer); err != nil {
		return
	}
	out, err := NewJoinReply(n.opt.Identity, in.Secret, from, CleanName(n.opt.Name()), n.opt.Version)
	if err != nil {
		return
	}
	_, _ = c.Send(from, out)
	c.CloseBox(box)
	_ = n.sendTo(c, peer, TypeHello, n.helloBody())
	n.opt.Logf("link: linked with another Usherr")
	n.changed()
}

// CleanName makes a name safe to show: no control characters, trimmed, at
// most 60 characters.
func CleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		s = strings.TrimSpace(string(r[:60]))
	}
	if s == "" {
		s = "Usherr"
	}
	return s
}

func cleanVersion(s string) string {
	if len(s) > 32 {
		s = s[:32]
	}
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '.' || r == '-' || r == '+' {
			return r
		}
		return -1
	}, s)
}

// The built-in message types.

type helloBody struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Caps    []string `json:"caps"`
}

type renameBody struct {
	Name string `json:"name"`
}

type pingBody struct {
	Echo string `json:"echo,omitempty"` // the id of the ping this answers
}

func (n *Node) helloBody() helloBody {
	return helloBody{Name: CleanName(n.opt.Name()), Version: n.opt.Version, Caps: n.reg.Types()}
}

func (n *Node) registerBuiltins() {
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must(n.reg.Register(Kind{Type: TypeHello, MaxBody: 4 << 10, Handle: func(ctx context.Context, in Incoming) error {
		var b helloBody
		if err := strictJSON(in.Msg.Body, &b); err != nil || len(b.Caps) > 64 {
			return errMessage
		}
		p := in.Peer
		p.Name, p.Version = CleanName(b.Name), cleanVersion(b.Version)
		if err := n.opt.Store.SavePeer(ctx, p); err != nil {
			return err
		}
		n.changed()
		return nil
	}}))
	must(n.reg.Register(Kind{Type: TypeRename, MaxBody: 1 << 10, Handle: func(ctx context.Context, in Incoming) error {
		var b renameBody
		if err := strictJSON(in.Msg.Body, &b); err != nil {
			return errMessage
		}
		p := in.Peer
		p.Name = CleanName(b.Name)
		if err := n.opt.Store.SavePeer(ctx, p); err != nil {
			return err
		}
		n.changed()
		return nil
	}}))
	must(n.reg.Register(Kind{Type: TypeUnlink, MaxBody: 256, Handle: func(ctx context.Context, in Incoming) error {
		if err := n.opt.Store.ForgetPeer(ctx, in.Peer.ID, "them"); err != nil {
			return err
		}
		n.opt.Logf("link: a linked Usherr unlinked")
		n.changed()
		return nil
	}}))
	must(n.reg.Register(Kind{Type: TypePing, MaxBody: 256, Handle: func(ctx context.Context, in Incoming) error {
		var b pingBody
		if err := strictJSON(in.Msg.Body, &b); err != nil {
			return errMessage
		}
		now := n.opt.Now()
		n.mu.Lock()
		p := n.pings[in.Peer.ID]
		if b.Echo == "" {
			p[0] = now
		} else {
			p[1] = now
		}
		n.pings[in.Peer.ID] = p
		n.mu.Unlock()
		if b.Echo == "" {
			return n.Send(ctx, in.Peer.ID, TypePing, pingBody{Echo: in.Msg.ID})
		}
		return nil
	}}))
}

// OnReceive sets a function told about every message taken in (tests).
func (n *Node) OnReceive(fn func(peer string, m Message)) {
	n.mu.Lock()
	n.onRecv = fn
	n.mu.Unlock()
}
