package link

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/chacha20poly1305"
)

// An invite code: USH-XXXX-XXXX-XXXX-XXXX-XXXX-C. Twenty characters of
// Crockford's base 32 hold a 100-bit random secret; the last is a check, so
// a mistyped code is refused before anything is sent. When the inviting
// Usherr uses its own relay, the relay is written after USH@
// (USH@relay.example.com-XXXX-…-C, or USH@ws://192.168.1.5:8787-… for a
// relay without TLS on a home network).

// Crockford's base 32: no I, L, O or U.
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const (
	codeDigits  = 20 // 100 bits
	secretBytes = 13 // the 100 bits, the first 4 bits zero
	// InviteLife is how long a code can be used.
	InviteLife = 24 * time.Hour
)

// Errors reading a code.
var (
	ErrNotACode  = errors.New("That isn't an Usherr invite code. It starts with USH and has five groups of four letters and numbers.")
	ErrCodeTypo  = errors.New("That code has a typo: one of its letters or numbers is wrong. Copy it again.")
	ErrCodeRelay = errors.New("The relay address in that code can't be read. Copy the code again.")
)

// Invite is a code made here (Role "host") or typed in here (Role "join"),
// until its handshake is done.
type Invite struct {
	Box     string    // hex(SHA-256("ulr-box" || secret)): all the relay sees
	Secret  []byte    // the 13 bytes of the secret
	Relay   string    // the relay both Usherrs use for the link
	Role    string    // "host" or "join"
	Created time.Time //
	Expires time.Time // a host's code stops working then
	Used    bool      // the handshake finished (or the code was cancelled)
	State   string    // "", or how it ended: linked, cancelled, used, expired, gone
}

// NewSecret makes a random 100-bit secret.
func NewSecret() []byte {
	b := make([]byte, secretBytes)
	_, _ = rand.Read(b)
	b[0] &= 0x0f
	return b
}

// BoxOf is the invite mailbox's name for a secret.
func BoxOf(secret []byte) string {
	sum := sha256.Sum256(append([]byte("ulr-box"), secret...))
	return hex.EncodeToString(sum[:])
}

func digitsOf(secret []byte) []int {
	// 104 bits, the first 4 zero: read 5 bits at a time from bit 4.
	d := make([]int, codeDigits)
	for i := range codeDigits {
		bit := 4 + i*5
		v := 0
		for j := range 5 {
			p := bit + j
			v = v<<1 | int(secret[p/8]>>(7-p%8)&1)
		}
		d[i] = v
	}
	return d
}

func secretOf(d []int) []byte {
	b := make([]byte, secretBytes)
	for i, v := range d {
		for j := range 5 {
			if v>>(4-j)&1 == 1 {
				p := 4 + i*5 + j
				b[p/8] |= 1 << (7 - p%8)
			}
		}
	}
	return b
}

func codeCheck(d []int) int {
	sum := 0
	for i, v := range d {
		sum += v * (i + 1)
	}
	return sum % 32
}

// EncodeCode writes an invite code. relayInCode is "" for the default relay,
// else what RelayInCode gives.
func EncodeCode(secret []byte, relayInCode string) string {
	d := digitsOf(secret)
	var b strings.Builder
	b.WriteString("USH")
	if relayInCode != "" {
		b.WriteString("@" + relayInCode)
	}
	for i, v := range d {
		if i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(codeAlphabet[v])
	}
	b.WriteByte('-')
	b.WriteByte(codeAlphabet[codeCheck(d)])
	return b.String()
}

// RelayInCode is how a relay address is written in a code: "" for the
// default relay, host[:port] for wss://, ws://host[:port] for ws://.
func RelayInCode(relay, defaultRelay string) string {
	if relay == "" || relay == defaultRelay {
		return ""
	}
	if rest, ok := strings.CutPrefix(relay, "wss://"); ok {
		return rest
	}
	return relay
}

// DecodeCode reads a code typed or pasted (alone, or in a message), and
// gives its secret and its relay ("" for the default one). Letters people
// mix up are read as what they look like (O as 0, I and L as 1).
func DecodeCode(s string) (secret []byte, relay string, err error) {
	s = strings.TrimSpace(s)
	up := strings.ToUpper(s)
	at := strings.Index(up, "USH-")
	if i := strings.Index(up, "USH@"); i >= 0 && (at < 0 || i < at) {
		at = i
	}
	if at < 0 {
		// Without its USH, the code alone is fine too.
		return decodeDigits(s)
	}
	s = s[at+3:]
	if end := strings.IndexFunc(s, unicode.IsSpace); end >= 0 {
		s = s[:end]
	}
	s = strings.TrimRight(s, ".,;)`*")
	if rest, ok := strings.CutPrefix(s, "@"); ok {
		parts := strings.Split(rest, "-")
		if len(parts) < 7 {
			return nil, "", ErrNotACode
		}
		host := strings.Join(parts[:len(parts)-6], "-")
		relay, err = relayFromCode(host)
		if err != nil {
			return nil, "", err
		}
		secret, _, err = decodeDigits(strings.Join(parts[len(parts)-6:], ""))
		return secret, relay, err
	}
	return decodeDigits(s)
}

func decodeDigits(s string) ([]byte, string, error) {
	var d []int
	for _, r := range strings.ToUpper(s) {
		switch r {
		case '-', ' ':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		i := strings.IndexRune(codeAlphabet, r)
		if i < 0 {
			return nil, "", ErrNotACode
		}
		d = append(d, i)
	}
	if len(d) != codeDigits+1 {
		return nil, "", ErrNotACode
	}
	if codeCheck(d[:codeDigits]) != d[codeDigits] {
		return nil, "", ErrCodeTypo
	}
	return secretOf(d[:codeDigits]), "", nil
}

func relayFromCode(host string) (string, error) {
	scheme := "wss://"
	if rest, ok := strings.CutPrefix(strings.ToLower(host), "ws://"); ok {
		scheme, host = "ws://", rest
	}
	relay, err := CleanRelay(scheme + host)
	if err != nil {
		return "", ErrCodeRelay
	}
	return relay, nil
}

// CleanRelay checks a relay address and writes it the one way:
// ws(s)://host[:port], lower case, no path.
func CleanRelay(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("A relay address looks like wss://relay.example.com (ws:// only on your home network).")
	}
	host := u.Hostname()
	if host == "" || len(host) > 253 {
		return "", errors.New("A relay address needs a host name.")
	}
	for _, r := range host {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == ':') {
			return "", errors.New("A relay address needs a host name.")
		}
	}
	out := u.Scheme + "://" + strings.ToLower(host)
	if strings.Contains(host, ":") {
		out = u.Scheme + "://[" + strings.ToLower(host) + "]"
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "", errors.New("The relay address's port isn't a number from 1 to 65535.")
		}
		out += ":" + p
	}
	return out, nil
}

// The handshake. The joiner posts a JoinRequest to the host's mailbox; the
// host answers with a JoinReply sent straight to the joiner's identity. Both
// are encrypted with a key only the two holders of the code have.

// JoinRequest is what the joiner sends: its keys and name, and a proof that
// it knows the secret, bound to its keys.
type JoinRequest struct {
	V       int    `json:"v"`
	Sign    string `json:"sign"` // Ed25519 public key (an identity)
	X       string `json:"x"`    // X25519 public key, base64url
	Name    string `json:"name"`
	Version string `json:"version"`
	Proof   string `json:"proof"` // base64 HMAC-SHA256
}

// JoinReply is the host's answer: its keys and name (OK), or why not.
type JoinReply struct {
	V       int    `json:"v"`
	OK      bool   `json:"ok"`
	Why     string `json:"why,omitempty"` // used, expired, cancelled
	Sign    string `json:"sign,omitempty"`
	X       string `json:"x,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Proof   string `json:"proof,omitempty"`
}

func inviteKeys(secret []byte) (enc, proof []byte) {
	enc, _ = hkdf.Key(sha256.New, secret, nil, "ulr-v1-invite", chacha20poly1305.KeySize)
	proof, _ = hkdf.Key(sha256.New, secret, nil, "ulr-v1-invite-proof", 32)
	return enc, proof
}

func inviteProof(secret []byte, parts ...[]byte) []byte {
	_, k := inviteKeys(secret)
	m := hmac.New(sha256.New, k)
	for _, p := range parts {
		m.Write(p)
	}
	return m.Sum(nil)
}

const (
	aadJoin = "ulr-v1-join"
	aadHost = "ulr-v1-host"
)

// NewJoinRequest makes the joiner's request for a code's secret.
func NewJoinRequest(me *Identity, secret []byte, name, version string) ([]byte, error) {
	x := me.XPublic()
	req := JoinRequest{
		V: 1, Sign: me.ID(), X: base64.RawURLEncoding.EncodeToString(x), Name: name, Version: version,
		Proof: base64.StdEncoding.EncodeToString(inviteProof(secret, []byte(aadJoin), me.SignPublic(), x)),
	}
	b, _ := json.Marshal(req)
	enc, _ := inviteKeys(secret)
	return seal(enc, b, []byte(aadJoin))
}

// OpenJoinRequest is the host reading a request posted by from (the
// identity the relay vouches for). The keys inside must be from's.
func OpenJoinRequest(secret []byte, from string, env []byte) (JoinRequest, []byte, error) {
	enc, _ := inviteKeys(secret)
	plain, err := unseal(enc, env, []byte(aadJoin))
	if err != nil {
		return JoinRequest{}, nil, err
	}
	var req JoinRequest
	if err := strictJSON(plain, &req); err != nil || req.V != 1 || req.Sign != from {
		return JoinRequest{}, nil, errMessage
	}
	signPub, err := DecodeID(req.Sign)
	if err != nil {
		return JoinRequest{}, nil, errMessage
	}
	x, err := base64.RawURLEncoding.DecodeString(req.X)
	if err != nil || len(x) != 32 {
		return JoinRequest{}, nil, errMessage
	}
	proof, err := base64.StdEncoding.DecodeString(req.Proof)
	if err != nil || !hmac.Equal(proof, inviteProof(secret, []byte(aadJoin), signPub, x)) {
		return JoinRequest{}, nil, errSignature
	}
	return req, x, nil
}

// NewJoinReply makes the host's answer to a joiner.
func NewJoinReply(me *Identity, secret []byte, joiner string, name, version string) ([]byte, error) {
	jp, err := DecodeID(joiner)
	if err != nil {
		return nil, err
	}
	x := me.XPublic()
	rep := JoinReply{
		V: 1, OK: true, Sign: me.ID(), X: base64.RawURLEncoding.EncodeToString(x), Name: name, Version: version,
		Proof: base64.StdEncoding.EncodeToString(inviteProof(secret, []byte(aadHost), me.SignPublic(), x, jp)),
	}
	b, _ := json.Marshal(rep)
	enc, _ := inviteKeys(secret)
	return seal(enc, b, []byte(aadHost))
}

// NewJoinRefusal says the code can't be used (used, expired, cancelled).
func NewJoinRefusal(secret []byte, why string) ([]byte, error) {
	b, _ := json.Marshal(JoinReply{V: 1, Why: why})
	enc, _ := inviteKeys(secret)
	return seal(enc, b, []byte(aadHost))
}

// OpenJoinReply is the joiner reading the host's answer, sent by from.
func OpenJoinReply(me *Identity, secret []byte, from string, env []byte) (JoinReply, []byte, error) {
	enc, _ := inviteKeys(secret)
	plain, err := unseal(enc, env, []byte(aadHost))
	if err != nil {
		return JoinReply{}, nil, err
	}
	var rep JoinReply
	if err := strictJSON(plain, &rep); err != nil || rep.V != 1 {
		return JoinReply{}, nil, errMessage
	}
	if !rep.OK {
		switch rep.Why {
		case "used", "expired", "cancelled":
			return rep, nil, nil
		}
		return JoinReply{}, nil, errMessage
	}
	if rep.Sign != from {
		return JoinReply{}, nil, errMessage
	}
	signPub, err := DecodeID(rep.Sign)
	if err != nil {
		return JoinReply{}, nil, errMessage
	}
	x, err := base64.RawURLEncoding.DecodeString(rep.X)
	if err != nil || len(x) != 32 {
		return JoinReply{}, nil, errMessage
	}
	proof, err := base64.StdEncoding.DecodeString(rep.Proof)
	if err != nil || !hmac.Equal(proof, inviteProof(secret, []byte(aadHost), signPub, x, me.SignPublic())) {
		return JoinReply{}, nil, errSignature
	}
	return rep, x, nil
}
