package link

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// Limits on what crosses the relay.
const (
	// MaxEnvelope is the largest envelope (decoded bytes) the relay carries.
	MaxEnvelope = 16 << 10
	// envelopeVersion is the first byte of every envelope.
	envelopeVersion = 1
	envOverhead     = 1 + chacha20poly1305.NonceSizeX + chacha20poly1305.Overhead
)

var (
	errEnvelope  = errors.New("the envelope can't be opened")
	errSignature = errors.New("the signature doesn't match")
	errMessage   = errors.New("the message can't be read")
)

// Message is what one Usherr says to another: a type from the registry, a
// random id (a UUID), when it was said (unix ms) and its body (a JSON
// object).
type Message struct {
	Type string          `json:"type"`
	ID   string          `json:"id"`
	At   int64           `json:"at"`
	Body json.RawMessage `json:"body"`
}

// signed is the plaintext inside an envelope: the message's JSON exactly as
// signed, and the signature.
type signed struct {
	M   json.RawMessage `json:"m"`
	Sig string          `json:"sig"`
}

// pairKey is the key two linked Usherrs share:
// HKDF-SHA256(X25519(mine, theirs), salt = the two Ed25519 keys sorted and
// joined, info "ulr-v1-env").
func pairKey(me *Identity, peerSign ed25519.PublicKey, peerX []byte) ([]byte, error) {
	pub, err := ecdh.X25519().NewPublicKey(peerX)
	if err != nil {
		return nil, errEnvelope
	}
	shared, err := me.x.ECDH(pub) // refuses a low-order key (all-zero result)
	if err != nil {
		return nil, errEnvelope
	}
	a, b := []byte(me.SignPublic()), []byte(peerSign)
	if bytes.Compare(a, b) > 0 {
		a, b = b, a
	}
	salt := append(append(make([]byte, 0, 64), a...), b...)
	return hkdf.Key(sha256.New, shared, salt, "ulr-v1-env", chacha20poly1305.KeySize)
}

// seal encrypts plaintext with key: version byte, 24-byte nonce, ciphertext.
func seal(key, plaintext, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 1+chacha20poly1305.NonceSizeX, envOverhead+len(plaintext))
	out[0] = envelopeVersion
	if _, err := io.ReadFull(rand.Reader, out[1:]); err != nil {
		return nil, err
	}
	out = aead.Seal(out, out[1:1+chacha20poly1305.NonceSizeX], plaintext, aad)
	if len(out) > MaxEnvelope {
		return nil, fmt.Errorf("the message is too big (%d bytes)", len(out))
	}
	return out, nil
}

func unseal(key, env, aad []byte) ([]byte, error) {
	if len(env) < envOverhead || len(env) > MaxEnvelope || env[0] != envelopeVersion {
		return nil, errEnvelope
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, errEnvelope
	}
	nonce := env[1 : 1+chacha20poly1305.NonceSizeX]
	plain, err := aead.Open(nil, nonce, env[1+chacha20poly1305.NonceSizeX:], aad)
	if err != nil {
		return nil, errEnvelope
	}
	return plain, nil
}

// SealMessage signs m with this Usherr's key and encrypts it for a linked
// peer.
func SealMessage(me *Identity, peerSign ed25519.PublicKey, peerX []byte, m Message) ([]byte, error) {
	if len(m.Body) == 0 {
		m.Body = json.RawMessage("{}")
	}
	mb, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(signed{M: mb, Sig: base64.StdEncoding.EncodeToString(me.Sign(mb))})
	if err != nil {
		return nil, err
	}
	key, err := pairKey(me, peerSign, peerX)
	if err != nil {
		return nil, err
	}
	return seal(key, plain, nil)
}

// OpenMessage decrypts an envelope from a linked peer, checks its signature
// against the peer's key and reads the message strictly (no unknown fields,
// a well-formed type and id, a JSON object body). Time and replays are
// checked by the caller (Inbox).
func OpenMessage(me *Identity, peerSign ed25519.PublicKey, peerX []byte, env []byte) (Message, error) {
	key, err := pairKey(me, peerSign, peerX)
	if err != nil {
		return Message{}, err
	}
	plain, err := unseal(key, env, nil)
	if err != nil {
		return Message{}, err
	}
	return readSigned(plain, peerSign)
}

// readSigned reads a signed message strictly and checks its signature.
func readSigned(plain []byte, peerSign ed25519.PublicKey) (Message, error) {
	var s signed
	if err := strictJSON(plain, &s); err != nil || len(s.M) == 0 {
		return Message{}, errMessage
	}
	sig, err := base64.StdEncoding.DecodeString(s.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(peerSign, s.M, sig) {
		return Message{}, errSignature
	}
	var m Message
	if err := strictJSON(s.M, &m); err != nil {
		return Message{}, errMessage
	}
	if !validType(m.Type) || !validUUID(m.ID) || m.At <= 0 {
		return Message{}, errMessage
	}
	if b := bytes.TrimSpace(m.Body); len(b) == 0 || b[0] != '{' {
		return Message{}, errMessage
	}
	return m, nil
}

// Group envelopes (step 2). A host gives each Usherr invited to a screening
// a random 32-byte screening key inside their own link; a message for all of
// them is then sealed once with that key and fanned out by the relay:
//
//	version (1 byte, = 2) || key id (16 bytes) || nonce (24 bytes) || XChaCha20-Poly1305 ciphertext
//
// key id = the first 16 bytes of SHA-256("ulr-v1-kid" || key), so the
// receiver finds the key (and whose screening it is) without trying them
// all. The associated data is "ulr-v1-group" || key id. The plaintext is the
// same signed message as in a pair envelope, signed with the host's key.
const (
	groupVersion = 2
	// GroupKeySize is the size of a screening key.
	GroupKeySize = chacha20poly1305.KeySize
	// GroupKIDSize is the size of a key id.
	GroupKIDSize   = 16
	groupHead      = 1 + GroupKIDSize + chacha20poly1305.NonceSizeX
	groupOverhead  = groupHead + chacha20poly1305.Overhead
	groupAADPrefix = "ulr-v1-group"
)

// NewGroupKey makes a random screening key.
func NewGroupKey() []byte {
	k := make([]byte, GroupKeySize)
	_, _ = rand.Read(k)
	return k
}

// GroupKID is a screening key's id.
func GroupKID(key []byte) []byte {
	h := sha256.Sum256(append([]byte("ulr-v1-kid"), key...))
	return h[:GroupKIDSize]
}

// GroupEnvelopeKID reports whether an envelope is sealed with a group key,
// and its key id.
func GroupEnvelopeKID(env []byte) ([]byte, bool) {
	if len(env) < groupOverhead || env[0] != groupVersion {
		return nil, false
	}
	return env[1 : 1+GroupKIDSize], true
}

// SealGroup signs m with this Usherr's key and encrypts it with a screening
// key, once for everyone who has the key.
func SealGroup(me *Identity, key []byte, m Message) ([]byte, error) {
	if len(key) != GroupKeySize {
		return nil, errEnvelope
	}
	if len(m.Body) == 0 {
		m.Body = json.RawMessage("{}")
	}
	mb, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(signed{M: mb, Sig: base64.StdEncoding.EncodeToString(me.Sign(mb))})
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	kid := GroupKID(key)
	out := make([]byte, groupHead, groupOverhead+len(plain))
	out[0] = groupVersion
	copy(out[1:], kid)
	if _, err := io.ReadFull(rand.Reader, out[1+GroupKIDSize:groupHead]); err != nil {
		return nil, err
	}
	out = aead.Seal(out, out[1+GroupKIDSize:groupHead], plain, append([]byte(groupAADPrefix), kid...))
	if len(out) > MaxEnvelope {
		return nil, fmt.Errorf("the message is too big (%d bytes)", len(out))
	}
	return out, nil
}

// OpenGroup decrypts a group envelope with a screening key, checks the
// signature against the key's owner (the host) and reads the message as
// strictly as OpenMessage does.
func OpenGroup(key []byte, ownerSign ed25519.PublicKey, env []byte) (Message, error) {
	kid, ok := GroupEnvelopeKID(env)
	if !ok || len(env) > MaxEnvelope || len(key) != GroupKeySize || !bytes.Equal(kid, GroupKID(key)) {
		return Message{}, errEnvelope
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return Message{}, errEnvelope
	}
	plain, err := aead.Open(nil, env[1+GroupKIDSize:groupHead], env[groupHead:], append([]byte(groupAADPrefix), kid...))
	if err != nil {
		return Message{}, errEnvelope
	}
	return readSigned(plain, ownerSign)
}

// StrictJSON decodes exactly one JSON value with no unknown fields (message
// bodies are read this way).
func StrictJSON(b []byte, v any) error { return strictJSON(b, v) }

// ValidMessageID reports whether s is a well-formed id (a lower-case UUID).
func ValidMessageID(s string) bool { return validUUID(s) }

// strictJSON decodes exactly one JSON value with no unknown fields.
func strictJSON(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

// validType: lower-case letters and dots, at most 40 characters.
func validType(t string) bool {
	if t == "" || len(t) > 40 {
		return false
	}
	for _, r := range t {
		if (r < 'a' || r > 'z') && r != '.' {
			return false
		}
	}
	return true
}

// NewMessageID makes a random (version 4) UUID.
func NewMessageID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	const hexd = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, c := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hexd[c>>4], hexd[c&15])
	}
	return string(out)
}

func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
				return false
			}
		}
	}
	return true
}
