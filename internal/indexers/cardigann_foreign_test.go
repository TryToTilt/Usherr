package indexers

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeDNS answers the name lookups of the address checks, so the tests do not
// depend on a real resolver. A name that is not listed has one public address.
var fakeDNS = struct {
	sync.Mutex
	names map[string][]string
}{names: map[string][]string{}}

func setFakeDNS(t *testing.T, names map[string][]string) {
	t.Helper()
	fakeDNS.Lock()
	fakeDNS.names = names
	fakeDNS.Unlock()
	t.Cleanup(func() {
		fakeDNS.Lock()
		fakeDNS.names = map[string][]string{}
		fakeDNS.Unlock()
	})
}

func init() {
	lookupAddrs = func(ctx context.Context, host string) ([]netip.Addr, error) {
		fakeDNS.Lock()
		list, ok := fakeDNS.names[host]
		fakeDNS.Unlock()
		if !ok {
			list = []string{"93.184.216.34"}
		}
		var out []netip.Addr
		for _, a := range list {
			if a == "fail" {
				return nil, errors.New("no such host")
			}
			out = append(out, netip.MustParseAddr(a))
		}
		return out, nil
	}
}

func publicSiteSession(t *testing.T, def Definition, settings map[string]string) *cgSession {
	t.Helper()
	base, _ := url.Parse("https://www.yts.example.org/")
	return &cgSession{name: "Fixture", base: base, def: &def, settings: settings}
}

// A site without a login may use another public host, but a name that only
// looks public and points at this machine or the home network is refused.
func TestUncredentialedSiteRefusesNamesThatResolveToTheLocalNetwork(t *testing.T) {
	setFakeDNS(t, map[string][]string{
		"rebind.attacker.example": {"127.0.0.1"},
		"router.attacker.example": {"192.168.1.1"},
		"mixed.attacker.example":  {"93.184.216.34", "10.0.0.7"},
		"meta.attacker.example":   {"169.254.169.254"},
		"v6.attacker.example":     {"::1"},
		"cgnat.attacker.example":  {"100.64.1.1"},
		"api.movies.example":      {"93.184.216.35", "2606:2800:220:1:248:1893:25c8:1946"},
		"nolookup.example":        {"fail"},
	})
	tests := []struct {
		name string
		host string
		ok   bool
	}{
		{"a public name, the YTS shape", "api.movies.example", true},
		{"a public name with two addresses", "api.movies.example", true},
		{"resolves to the machine itself", "rebind.attacker.example", false},
		{"resolves to the home network", "router.attacker.example", false},
		{"one of several addresses is private", "mixed.attacker.example", false},
		{"resolves to the metadata address", "meta.attacker.example", false},
		{"resolves to ::1", "v6.attacker.example", false},
		{"resolves to shared address space", "cgnat.attacker.example", false},
		{"cannot be looked up here", "nolookup.example", true}, // the connection is checked as it is made
		{"an address that is private", "192.168.1.5", false},
		{"a bare name", "router", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := publicSiteSession(t, Definition{}, nil)
			_, err := s.resolvePath("https://" + tt.host + "/api/list")
			if tt.ok != (err == nil) {
				t.Fatalf("resolvePath = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !strings.Contains(err.Error(), "own network") {
				t.Fatalf("the refusal should say why: %v", err)
			}
			// a link or redirect from a page goes through the same check
			s = publicSiteSession(t, Definition{}, nil)
			perr := s.checkPageAddress("https://" + tt.host + "/dl/1")
			if tt.ok != (perr == nil) {
				t.Fatalf("checkPageAddress = %v, want ok=%v", perr, tt.ok)
			}
		})
	}
}

// The connection is checked as well as the address: a request that reaches the
// transport for a name that resolves inside is stopped before anything is sent.
func TestForeignRequestToALocalNameIsNeverSent(t *testing.T) {
	setFakeDNS(t, map[string][]string{"rebind.attacker.example": {"127.0.0.1"}})
	sent := 0
	m := NewCardigannManager(nil, nil)
	m.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sent++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	})
	def := &Definition{ID: "x", Name: "X", Links: []string{"https://www.example.org/"}}
	s, err := newSession(m, def, Instance{Name: "X", BaseURL: "https://www.example.org/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.doOnce(context.Background(), cgRequest{url: "https://rebind.attacker.example/x"}); err == nil || !strings.Contains(err.Error(), "own network") {
		t.Fatalf("got %v, want the request refused", err)
	}
	if sent != 0 {
		t.Fatalf("the request was sent %d times", sent)
	}
	// the site itself and a public host are fine
	for _, u := range []string{"https://www.example.org/search", "https://files.mirror.example.net/a.torrent"} {
		if _, err := s.doOnce(context.Background(), cgRequest{url: u}); err != nil {
			t.Fatalf("%s: %v", u, err)
		}
	}
	if sent != 2 {
		t.Fatalf("sent = %d, want 2", sent)
	}
	// a redirect from the site to a name that resolves inside is not followed
	m.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sent++
		h := http.Header{"Location": []string{"https://rebind.attacker.example/in"}}
		return &http.Response{StatusCode: http.StatusFound, Header: h, Body: http.NoBody, Request: r}, nil
	})
	s, _ = newSession(m, def, Instance{Name: "X", BaseURL: "https://www.example.org/"})
	sent = 0
	if _, err := s.doOnce(context.Background(), cgRequest{url: "https://www.example.org/go"}); err == nil || !strings.Contains(err.Error(), "own network") {
		t.Fatalf("got %v, want the redirect refused", err)
	}
	if sent != 1 {
		t.Fatalf("sent = %d, want only the first request", sent)
	}
}

// The site the person set up on their own network keeps working.
func TestSiteOnTheHomeNetworkStillWorks(t *testing.T) {
	setFakeDNS(t, nil)
	base, _ := url.Parse("http://192.168.1.10:9117/")
	s := &cgSession{name: "Fixture", base: base, def: &Definition{}}
	if _, err := s.resolvePath("api/v2.0/search"); err != nil {
		t.Fatalf("own path: %v", err)
	}
	if err := s.checkPageAddress("http://192.168.1.10:9117/dl/1"); err != nil {
		t.Fatalf("own link: %v", err)
	}
	m := NewCardigannManager(nil, nil)
	m.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	})
	sess, err := newSession(m, &Definition{ID: "x", Name: "X"}, Instance{Name: "X", BaseURL: "http://192.168.1.10:9117/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.doOnce(context.Background(), cgRequest{url: "http://192.168.1.10:9117/api"}); err != nil {
		t.Fatalf("a request to the site's own address: %v", err)
	}
}

func TestSendsCredentialsRecognisesUnusualSettingsAndHeaders(t *testing.T) {
	text := func(name string) Definition { return Definition{Settings: []SettingField{{Name: name, Type: "text"}}} }
	sel := Definition{Settings: []SettingField{{Name: "sessionid", Type: "select"}, {Name: "freeleech", Type: "checkbox"}}}
	header := func(k string) Definition {
		return Definition{Search: SearchBlock{Headers: map[string][]string{k: {"x"}}}}
	}
	tests := []struct {
		name     string
		def      Definition
		settings map[string]string
		want     bool
	}{
		{"a session id", text("sessionid"), map[string]string{"sessionid": "abc"}, true},
		{"sid", text("sid"), map[string]string{"sid": "abc"}, true},
		{"auth", text("auth"), map[string]string{"auth": "abc"}, true},
		{"hash", text("hash"), map[string]string{"hash": "abc"}, true},
		{"pwd", text("pwd"), map[string]string{"pwd": "abc"}, true},
		{"rss key", text("rss_key"), map[string]string{"rss_key": "abc"}, true},
		{"bearer", text("bearer"), map[string]string{"bearer": "abc"}, true},
		{"a text setting left empty", text("sessionid"), map[string]string{"sessionid": " "}, false},
		{"user agent", text("useragent"), map[string]string{"useragent": "Mozilla/5.0"}, false},
		{"language", text("language"), map[string]string{"language": "en"}, false},
		{"sort", text("sort"), map[string]string{"sort": "seeders"}, false},
		{"a choice", sel, map[string]string{"sessionid": "a", "freeleech": "true"}, false},
		{"x-auth-token header", header("X-Auth-Token"), nil, true},
		{"api-key header", header("Api-Key"), nil, true},
		{"x-session header", header("X-Session"), nil, true},
		{"cookie header", header("Cookie"), nil, true},
		{"accept-language header", header("Accept-Language"), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := publicSiteSession(t, tt.def, tt.settings)
			if got := s.sendsCredentials(); got != tt.want {
				t.Fatalf("sendsCredentials = %v, want %v", got, tt.want)
			}
		})
	}
	// and so it refuses another host
	s := publicSiteSession(t, text("sessionid"), map[string]string{"sessionid": "abc"})
	if _, err := s.resolvePath("https://movies-api.example.net/x"); err == nil || !strings.Contains(err.Error(), "not one of the site's own addresses") {
		t.Fatalf("got %v, want the other host refused", err)
	}
}
