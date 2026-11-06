package link

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Frame is one JSON text frame between an Usherr and the relay. Which
// fields are set depends on T (docs/home/link-protocol.md):
//
//	relay → client: challenge, ready, msg, invite.msg, ack, err, pong
//	client → relay: hello, send, fanout, invite.open, invite.post, invite.close, ping
type Frame struct {
	T      string   `json:"t"`
	Nonce  string   `json:"nonce,omitempty"`  // challenge: base64 of 32 random bytes
	Time   int64    `json:"time,omitempty"`   // challenge: the relay's clock, unix ms
	ID     string   `json:"id,omitempty"`     // hello: the identity
	Sig    string   `json:"sig,omitempty"`    // hello: base64 Ed25519 signature of "ulr-v1-hello" || nonce
	V      int      `json:"v,omitempty"`      // hello: protocol version (1); ready: what the relay speaks (2: fanout too)
	To     string   `json:"to,omitempty"`     // send: the recipient's identity
	Dests  []string `json:"dests,omitempty"`  // fanout: the recipients' identities (at most MaxFanout)
	From   string   `json:"from,omitempty"`   // msg, invite.msg: the sender's identity
	Env    string   `json:"env,omitempty"`    // send, msg, invite.post, invite.msg: base64 envelope
	Mid    string   `json:"mid,omitempty"`    // send, invite.post, msg, ack, err: the sender's id for the frame
	Code   string   `json:"code,omitempty"`   // err: rate, size, unknown, full, busy, bad
	Box    string   `json:"box,omitempty"`    // invite.*: hex SHA-256 of "ulr-box" || secret
	Exp    int64    `json:"exp,omitempty"`    // invite.open: when the box closes, unix ms (at most 24 h)
	Queued int      `json:"queued,omitempty"` // ready: how many queued messages follow
}

// Frame types.
const (
	FChallenge   = "challenge"
	FHello       = "hello"
	FReady       = "ready"
	FSend        = "send"
	FFanout      = "fanout"
	FMsg         = "msg"
	FAck         = "ack"
	FErr         = "err"
	FInviteOpen  = "invite.open"
	FInvitePost  = "invite.post"
	FInviteClose = "invite.close"
	FInviteMsg   = "invite.msg"
	FPing        = "ping"
	FPong        = "pong"
)

// Error codes in err frames.
const (
	CodeRate    = "rate"    // too many frames too fast
	CodeSize    = "size"    // the envelope is too big
	CodeUnknown = "unknown" // no such invite mailbox, or a frame the relay doesn't know
	CodeFull    = "full"    // the recipient's queue (or the sender's mailboxes) is full
	CodeBusy    = "busy"    // the identity already has 3 live connections
	CodeBad     = "bad"     // the frame can't be read
)

// Fan-out (step 2): one envelope sealed with a screening key, delivered by
// the relay to every identity in dests as an ordinary msg frame.
const (
	// MaxFanout is the most recipients one fanout frame may list.
	MaxFanout = 100
	// FanoutBatch is how many recipients an Usherr puts in one fanout frame.
	// It stays under 50 because the Cloudflare relay on the free plan may
	// make at most 50 calls to other Durable Objects per incoming message.
	FanoutBatch = 40
	// RelayLevel is what a relay that knows fanout says in ready's v.
	RelayLevel = 2
)

// FanoutCost is how many messages a fanout to n recipients counts as against
// the sender's rate limit: one for every ten recipients, at least one.
func FanoutCost(n int) int {
	return max(1, (n+9)/10)
}

// ValidDests checks a fanout's list: 1 to MaxFanout identities, none twice.
func ValidDests(dests []string) bool {
	if len(dests) == 0 || len(dests) > MaxFanout {
		return false
	}
	seen := make(map[string]bool, len(dests))
	for _, d := range dests {
		if _, err := DecodeID(d); err != nil || seen[d] {
			return false
		}
		seen[d] = true
	}
	return true
}

// HelloPrefix is what is signed with the challenge's nonce.
const HelloPrefix = "ulr-v1-hello"

// DecodeFrame reads one frame strictly (no unknown fields).
func DecodeFrame(b []byte) (Frame, error) {
	var f Frame
	err := strictJSON(b, &f)
	return f, err
}

// EncodeFrame writes a frame.
func EncodeFrame(f Frame) []byte {
	b, _ := json.Marshal(f)
	return b
}

// VerifyHello checks a hello's signature over the nonce the relay sent.
func VerifyHello(id, sig string, nonce []byte) bool {
	pub, err := DecodeID(id)
	if err != nil {
		return false
	}
	s, err := base64.StdEncoding.DecodeString(sig)
	if err != nil || len(s) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pub, append([]byte(HelloPrefix), nonce...), s)
}

// ValidBox reports whether s looks like a mailbox name (64 hex digits).
func ValidBox(s string) bool {
	if len(s) != 64 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}

// ValidMid reports whether s is a fine frame id: 1 to 64 of A-Z a-z 0-9 _ -.
func ValidMid(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
