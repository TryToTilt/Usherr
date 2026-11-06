// Package link is the Usherr Link: two Usherrs that linked with a one-time
// code talk to each other through the Usherr Link Relay (ULR), a blind
// postman that only routes sealed envelopes between public keys. This
// package holds everything both ends need and nothing about the rest of
// Usherr: the identity keys, the envelopes (encrypted and signed end to
// end), the invite codes and their handshake, the relay client and the
// short, fixed list of message types. The protocol is written down in
// docs/home/link-protocol.md.
//
// It never logs keys, secrets, invite codes or what an envelope holds.
package link

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Identity is this Usherr's pair of keys: Ed25519 to sign, X25519 to agree
// on the keys that encrypt. Made once and kept in a file of its own.
type Identity struct {
	sign ed25519.PrivateKey
	x    *ecdh.PrivateKey
}

// NewIdentity makes a new random identity.
func NewIdentity() (*Identity, error) {
	_, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	xk, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Identity{sign: sk, x: xk}, nil
}

// ID is the public identity: base64url (no padding) of the Ed25519 public key.
func (id *Identity) ID() string { return EncodeID(id.SignPublic()) }

// SignPublic is the Ed25519 public key.
func (id *Identity) SignPublic() ed25519.PublicKey { return id.sign.Public().(ed25519.PublicKey) }

// XPublic is the X25519 public key (32 bytes).
func (id *Identity) XPublic() []byte { return id.x.PublicKey().Bytes() }

// Sign signs b with the Ed25519 key.
func (id *Identity) Sign(b []byte) []byte { return ed25519.Sign(id.sign, b) }

// Fingerprint is this identity's fingerprint for people (see Fingerprint).
func (id *Identity) Fingerprint() string { return Fingerprint(id.SignPublic()) }

// EncodeID writes an Ed25519 public key as an identity.
func EncodeID(pub ed25519.PublicKey) string { return base64.RawURLEncoding.EncodeToString(pub) }

// DecodeID reads an identity back into its Ed25519 public key.
func DecodeID(s string) (ed25519.PublicKey, error) {
	if len(s) != 43 {
		return nil, errors.New("not an identity")
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("not an identity")
	}
	return ed25519.PublicKey(b), nil
}

// Fingerprint is what two people can read to each other to check a link:
// the first 16 hex digits of SHA-256 of the Ed25519 public key, in four
// groups of four ("a1b2 c3d4 e5f6 0718").
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	h := hex.EncodeToString(sum[:8])
	return h[0:4] + " " + h[4:8] + " " + h[8:12] + " " + h[12:16]
}

// FingerprintOf is the fingerprint of an identity string, or "" when it
// isn't one.
func FingerprintOf(id string) string {
	pub, err := DecodeID(id)
	if err != nil {
		return ""
	}
	return Fingerprint(pub)
}

// identityFile is how the keys are kept on disk.
type identityFile struct {
	V    int    `json:"v"`
	Sign string `json:"sign"` // base64 of the 32-byte Ed25519 seed
	X    string `json:"x"`    // base64 of the 32-byte X25519 private key
}

// LoadOrCreateIdentity reads the identity kept at path, or makes one and
// keeps it there (0600, written whole so a crash never leaves half a key).
func LoadOrCreateIdentity(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return parseIdentity(data)
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read the link identity: %w", err)
	}
	id, err := NewIdentity()
	if err != nil {
		return nil, err
	}
	out, _ := json.Marshal(identityFile{
		V:    1,
		Sign: base64.StdEncoding.EncodeToString(id.sign.Seed()),
		X:    base64.StdEncoding.EncodeToString(id.x.Bytes()),
	})
	if err := writeSecretFile(path, out); err != nil {
		return nil, fmt.Errorf("write the link identity: %w", err)
	}
	return id, nil
}

func parseIdentity(data []byte) (*Identity, error) {
	var f identityFile
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil || f.V != 1 {
		return nil, errors.New("the link identity file can't be read")
	}
	seed, err1 := base64.StdEncoding.DecodeString(f.Sign)
	xb, err2 := base64.StdEncoding.DecodeString(f.X)
	if err1 != nil || err2 != nil || len(seed) != ed25519.SeedSize || len(xb) != 32 {
		return nil, errors.New("the link identity file can't be read")
	}
	xk, err := ecdh.X25519().NewPrivateKey(xb)
	if err != nil {
		return nil, errors.New("the link identity file can't be read")
	}
	return &Identity{sign: ed25519.NewKeyFromSeed(seed), x: xk}, nil
}

// writeSecretFile puts data at path all at once, readable by its owner only.
func writeSecretFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".link-identity-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // nothing once renamed
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
