package link

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func mustID(t *testing.T) *Identity {
	t.Helper()
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLinkIdentityIsKeptAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf", "link-identity.key")
	a, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID() != b.ID() || !bytes.Equal(a.XPublic(), b.XPublic()) {
		t.Fatal("the identity changed when read back")
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("the identity file is %v, want 0600", fi.Mode().Perm())
		}
	}
	if len(a.ID()) != 43 {
		t.Fatalf("an identity is 43 base64url characters: %q", a.ID())
	}
	if !regexp.MustCompile(`^[0-9a-f]{4} [0-9a-f]{4} [0-9a-f]{4} [0-9a-f]{4}$`).MatchString(a.Fingerprint()) {
		t.Fatalf("fingerprint %q", a.Fingerprint())
	}
	if FingerprintOf(a.ID()) != a.Fingerprint() || FingerprintOf("nope") != "" {
		t.Fatal("FingerprintOf")
	}
	if err := os.WriteFile(path, []byte(`{"v":1,"sign":"AAAA","x":"AAAA"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateIdentity(path); err == nil {
		t.Fatal("a broken identity file was read")
	}
}

func TestLinkEnvelopeOpensOnlyForThePair(t *testing.T) {
	a, b, c := mustID(t), mustID(t), mustID(t)
	m := Message{Type: TypePing, ID: NewMessageID(), At: time.Now().UnixMilli(), Body: json.RawMessage(`{"x":1}`)}
	env, err := SealMessage(a, b.SignPublic(), b.XPublic(), m)
	if err != nil {
		t.Fatal(err)
	}
	if env[0] != 1 || len(env) > MaxEnvelope {
		t.Fatalf("envelope shape: version %d, %d bytes", env[0], len(env))
	}
	got, err := OpenMessage(b, a.SignPublic(), a.XPublic(), env)
	if err != nil || got.Type != m.Type || got.ID != m.ID || got.At != m.At || string(got.Body) != `{"x":1}` {
		t.Fatalf("open: %+v %v", got, err)
	}
	// Someone else can't open it.
	if _, err := OpenMessage(c, a.SignPublic(), a.XPublic(), env); err == nil {
		t.Fatal("a third key opened the envelope")
	}
	// Every flipped byte is noticed.
	for i := range env {
		bad := bytes.Clone(env)
		bad[i] ^= 0x01
		if _, err := OpenMessage(b, a.SignPublic(), a.XPublic(), bad); err == nil {
			t.Fatalf("a flipped byte %d went unnoticed", i)
		}
	}
	// Signed by someone other than the sender: refused.
	forged, err := ForgeMessage(a, c, b.SignPublic(), b.XPublic(), m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenMessage(b, a.SignPublic(), a.XPublic(), forged); !errors.Is(err, errSignature) {
		t.Fatalf("a wrong signature: %v", err)
	}
	// Too big to send.
	big := Message{Type: TypePing, ID: NewMessageID(), At: 1, Body: json.RawMessage(`{"x":"` + strings.Repeat("a", MaxEnvelope) + `"}`)}
	if _, err := SealMessage(a, b.SignPublic(), b.XPublic(), big); err == nil {
		t.Fatal("an envelope over 16 KB was made")
	}
	// A low-order X25519 key is refused.
	if _, err := SealMessage(a, b.SignPublic(), make([]byte, 32), m); err == nil {
		t.Fatal("an all-zero key was used")
	}
}

func TestLinkMessagesAreReadStrictly(t *testing.T) {
	a, b := mustID(t), mustID(t)
	key, err := pairKey(a, b.SignPublic(), b.XPublic())
	if err != nil {
		t.Fatal(err)
	}
	id := NewMessageID()
	sign := func(m string) []byte {
		return []byte(`{"m":` + m + `,"sig":"` + b64(a.Sign([]byte(m))) + `"}`)
	}
	for name, plain := range map[string][]byte{
		"unknown field":      sign(`{"type":"link.ping","id":"` + id + `","at":5,"body":{},"extra":1}`),
		"bad type":           sign(`{"type":"Link.Ping!","id":"` + id + `","at":5,"body":{}}`),
		"bad id":             sign(`{"type":"link.ping","id":"x","at":5,"body":{}}`),
		"body not an object": sign(`{"type":"link.ping","id":"` + id + `","at":5,"body":[1]}`),
		"no time":            sign(`{"type":"link.ping","id":"` + id + `","body":{}}`),
		"outer extra":        []byte(`{"m":{},"sig":"","x":1}`),
		"trailing":           append(sign(`{"type":"link.ping","id":"`+id+`","at":5,"body":{}}`), []byte(` {}`)...),
	} {
		env, err := seal(key, plain, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := OpenMessage(b, a.SignPublic(), a.XPublic(), env); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestLinkReplayCache(t *testing.T) {
	c := NewReplayCache()
	now := time.Now()
	if !c.Fresh("a/1", now) || c.Fresh("a/1", now.Add(time.Hour)) {
		t.Fatal("a repeat within 48 h wasn't noticed")
	}
	if !c.Fresh("a/1", now.Add(ReplayKeep+time.Second)) {
		t.Fatal("an id is forgotten after 48 h")
	}
	if !c.Fresh("b/1", now) {
		t.Fatal("the same id from another peer is a different message")
	}
}

func TestLinkInviteCodes(t *testing.T) {
	for range 50 {
		s := NewSecret()
		if s[0]&0xf0 != 0 {
			t.Fatal("the secret has more than 100 bits")
		}
		code := EncodeCode(s, "")
		if !regexp.MustCompile(`^USH(-[0-9A-HJKMNP-TV-Z]{4}){5}-[0-9A-HJKMNP-TV-Z]$`).MatchString(code) {
			t.Fatalf("code shape %q", code)
		}
		got, relay, err := DecodeCode(code)
		if err != nil || relay != "" || !bytes.Equal(got, s) {
			t.Fatalf("decode %q: %x %q %v", code, got, relay, err)
		}
		// Lower case, spaces, O for 0, inside a message: all fine.
		loose := "Here's my code: " + strings.ToLower(strings.ReplaceAll(code, "0", "O")) + " (24 h)"
		if got, _, err := DecodeCode(loose); err != nil || !bytes.Equal(got, s) {
			t.Fatalf("decode %q: %v", loose, err)
		}
	}
	s := NewSecret()
	code := EncodeCode(s, "")
	// One character changed: a typo, not a different code.
	i := 5
	r := []byte(code)
	r[i] = codeAlphabet[(strings.IndexByte(codeAlphabet, r[i])+1)%32]
	if _, _, err := DecodeCode(string(r)); !errors.Is(err, ErrCodeTypo) {
		t.Fatalf("a typo: %v", err)
	}
	if _, _, err := DecodeCode(code[:len(code)-3]); !errors.Is(err, ErrNotACode) {
		t.Fatalf("too short: %v", err)
	}
	if _, _, err := DecodeCode("hello"); !errors.Is(err, ErrNotACode) {
		t.Fatalf("not a code: %v", err)
	}

	// A custom relay goes into the code; the default one doesn't.
	for _, tc := range []struct{ relay, inCode string }{
		{"wss://my-relay.example.com", "my-relay.example.com"},
		{"wss://relay.example.com:8443", "relay.example.com:8443"},
		{"ws://192.168.1.5:8787", "ws://192.168.1.5:8787"},
	} {
		in := RelayInCode(tc.relay, DefaultRelay)
		if in != tc.inCode {
			t.Fatalf("relay in code %q, want %q", in, tc.inCode)
		}
		code := EncodeCode(s, in)
		if !strings.HasPrefix(code, "USH@"+tc.inCode+"-") {
			t.Fatalf("code %q", code)
		}
		got, relay, err := DecodeCode(code)
		if err != nil || relay != tc.relay || !bytes.Equal(got, s) {
			t.Fatalf("decode %q: %q %v", code, relay, err)
		}
	}
	if RelayInCode(DefaultRelay, DefaultRelay) != "" {
		t.Fatal("the default relay went into the code")
	}
	if _, _, err := DecodeCode("USH@bad host!-" + code[4:]); err == nil {
		t.Fatal("a bad relay was read")
	}
}

func TestLinkCleanRelay(t *testing.T) {
	for in, want := range map[string]string{
		"wss://Relay.Example.com/":  "wss://relay.example.com",
		" ws://10.0.0.2:8787 ":      "ws://10.0.0.2:8787",
		"wss://relay.usherr.app":    "wss://relay.usherr.app",
		"https://relay.example.com": "",
		"wss://relay.example.com/x": "",
		"wss://u:p@relay.example":   "",
		"wss://relay.example?x=1":   "",
		"wss://:8080":               "",
		"wss://relay.example:99999": "",
	} {
		got, err := CleanRelay(in)
		if (want == "") != (err != nil) || got != want {
			t.Errorf("CleanRelay(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestLinkHandshakePayloads(t *testing.T) {
	host, joiner, other := mustID(t), mustID(t), mustID(t)
	secret := NewSecret()
	env, err := NewJoinRequest(joiner, secret, "Sam's Usherr", "2.52.0")
	if err != nil {
		t.Fatal(err)
	}
	req, x, err := OpenJoinRequest(secret, joiner.ID(), env)
	if err != nil || req.Name != "Sam's Usherr" || !bytes.Equal(x, joiner.XPublic()) {
		t.Fatalf("join request: %+v %v", req, err)
	}
	// Posted by someone else than the keys inside: refused.
	if _, _, err := OpenJoinRequest(secret, other.ID(), env); err == nil {
		t.Fatal("a request relayed from another identity was taken")
	}
	// The wrong secret can't read it.
	if _, _, err := OpenJoinRequest(NewSecret(), joiner.ID(), env); err == nil {
		t.Fatal("the wrong secret read the request")
	}
	rep, err := NewJoinReply(host, secret, joiner.ID(), "Robin's Usherr", "2.52.0")
	if err != nil {
		t.Fatal(err)
	}
	got, hx, err := OpenJoinReply(joiner, secret, host.ID(), rep)
	if err != nil || !got.OK || got.Name != "Robin's Usherr" || !bytes.Equal(hx, host.XPublic()) {
		t.Fatalf("reply: %+v %v", got, err)
	}
	// A reply made for another joiner doesn't verify here.
	if _, _, err := OpenJoinReply(other, secret, host.ID(), rep); err == nil {
		t.Fatal("a reply for another joiner was taken")
	}
	// A request can't be read back as a reply (different associated data).
	if _, _, err := OpenJoinReply(joiner, secret, joiner.ID(), env); err == nil {
		t.Fatal("a request was read as a reply")
	}
	no, _ := NewJoinRefusal(secret, "used")
	if got, _, err := OpenJoinReply(joiner, secret, host.ID(), no); err != nil || got.OK || got.Why != "used" {
		t.Fatalf("refusal: %+v %v", got, err)
	}
	if BoxOf(secret) == BoxOf(NewSecret()) || !ValidBox(BoxOf(secret)) {
		t.Fatal("box")
	}
}

func TestLinkRegistry(t *testing.T) {
	r := NewRegistry()
	ok := Kind{Type: "chat", MaxBody: 1024, Handle: func(context.Context, Incoming) error { return nil }}
	if err := r.Register(ok); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(ok); err == nil {
		t.Fatal("registered twice")
	}
	if err := r.Register(Kind{Type: "Bad Type", MaxBody: 1, Handle: ok.Handle}); err == nil {
		t.Fatal("a bad name was registered")
	}
	if _, found := r.Lookup("chat"); !found {
		t.Fatal("lookup")
	}
	if CleanName("  Robin\x00's ‮Usherr\n ") != "Robin's Usherr" || CleanName("") != "Usherr" || len([]rune(CleanName(strings.Repeat("é", 100)))) != 60 {
		t.Fatal("CleanName")
	}
}

// Any one mistyped character in a code is caught by the check, wherever it
// is and whatever it was changed to.
func TestLinkInviteCodeCatchesEveryTypo(t *testing.T) {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	for range 20 {
		code := EncodeCode(NewSecret(), "")
		for i := len("USH-"); i < len(code); i++ {
			if code[i] == '-' {
				continue
			}
			for _, c := range []byte(alphabet) {
				if c == code[i] {
					continue
				}
				typo := []byte(code)
				typo[i] = c
				if _, _, err := DecodeCode(string(typo)); err == nil {
					t.Fatalf("%s with %c at %d still reads as a code", code, c, i)
				}
			}
		}
	}
}
