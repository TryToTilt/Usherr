package vpn

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestResolveEndpoint(t *testing.T) {
	old, oldProbe := lookupIP, routeProbe
	defer func() { lookupIP, routeProbe = old, oldProbe }()
	routeProbe = func(netip.Addr, string) bool { return true }
	lookupIP = func(_ context.Context, network, host string) ([]netip.Addr, error) {
		switch host {
		case "us-slc.prod.surfshark.com":
			return []netip.Addr{netip.MustParseAddr("2001:db8::7"), netip.MustParseAddr("172.216.252.7")}, nil
		case "v6only.example":
			return []netip.Addr{netip.MustParseAddr("2001:db8::9")}, nil
		case "empty.example":
			return nil, nil
		}
		return nil, errors.New("lookup " + host + ": no such host")
	}

	tests := []struct {
		name, in, want, wantErr string
	}{
		{"an IPv4 address is left alone", "203.0.113.5:51820", "203.0.113.5:51820", ""},
		{"an IPv6 address is left alone", "[2001:db8::1]:51820", "[2001:db8::1]:51820", ""},
		{"a name becomes its IPv4 address", "us-slc.prod.surfshark.com:51820", "172.216.252.7:51820", ""},
		{"a name with only IPv6 keeps it", "v6only.example:1234", "[2001:db8::9]:1234", ""},
		{"a name that finds nothing", "empty.example:51820", "", "no address found"},
		{"a name that does not exist", "nope.example:51820", "", "no such host"},
		{"no port", "us-slc.prod.surfshark.com", "", "missing port"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveEndpoint(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("resolveEndpoint(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}
}

// A config with a host name reaches wireguard-go as an address, which is what
// its config protocol accepts, and a name that can't be found says so in words
// a person can act on.
func TestUAPIConfigResolvesHostName(t *testing.T) {
	old := lookupIP
	defer func() { lookupIP = old }()
	lookupIP = func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host == "vpn.example.test" {
			return []netip.Addr{netip.MustParseAddr("198.51.100.8")}, nil
		}
		return nil, errors.New("lookup " + host + ": no such host")
	}
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	cfg := Config{PrivateKey: key, PeerPublicKey: key, Endpoint: "vpn.example.test:51820"}
	got, err := cfg.uapiConfig()
	if err != nil || !strings.Contains(got, "endpoint=198.51.100.8:51820\n") {
		t.Fatalf("uapiConfig = %q, %v; want the resolved address", got, err)
	}
	cfg.Endpoint = "gone.example.test:51820"
	_, err = cfg.uapiConfig()
	if err == nil {
		t.Fatal("a name that can't be found must fail")
	}
	if msg := Explain(err); !strings.Contains(msg, "Couldn't find the VPN server") {
		t.Fatalf("Explain = %q, want the 'couldn't find the VPN server' message", msg)
	}
}

// Without an IPv6 route, a name with both families uses its IPv4 address, and an
// IPv6 address (written out or the only one a name has) fails at once with a
// message that says what to do. With a route, IPv6 is accepted and kept as the
// fallback after IPv4.
func TestResolveEndpointIPv6Route(t *testing.T) {
	oldLookup, oldProbe := lookupIP, routeProbe
	defer func() { lookupIP, routeProbe = oldLookup, oldProbe }()
	lookupIP = func(_ context.Context, _, host string) ([]netip.Addr, error) {
		switch host {
		case "both.example":
			return []netip.Addr{netip.MustParseAddr("2001:db8::7"), netip.MustParseAddr("198.51.100.7")}, nil
		case "v6only.example":
			return []netip.Addr{netip.MustParseAddr("2001:db8::9")}, nil
		}
		return nil, errors.New("no such host")
	}
	tests := []struct {
		name    string
		route6  bool
		in      string
		want    []string
		wantErr bool
	}{
		{"both families, no IPv6 route: IPv4 only", false, "both.example:51820", []string{"198.51.100.7:51820"}, false},
		{"both families, IPv6 route: IPv4 first, IPv6 as the fallback", true, "both.example:51820", []string{"198.51.100.7:51820", "[2001:db8::7]:51820"}, false},
		{"literal IPv6, no route: fails at once", false, "[2a03:1b20:6:f011::a09f]:51820", nil, true},
		{"literal IPv6, route: accepted", true, "[2a03:1b20:6:f011::a09f]:51820", []string{"[2a03:1b20:6:f011::a09f]:51820"}, false},
		{"literal IPv4 needs no IPv6 route", false, "203.0.113.5:51820", []string{"203.0.113.5:51820"}, false},
		{"name with only IPv6, no route: fails at once", false, "v6only.example:51820", nil, true},
		{"name with only IPv6, route: accepted", true, "v6only.example:51820", []string{"[2001:db8::9]:51820"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			routeProbe = func(netip.Addr, string) bool { return tc.route6 }
			got, err := endpointCandidates(tc.in)
			if tc.wantErr {
				if !errors.Is(err, errIPv6Unreachable) {
					t.Fatalf("err = %v, want the IPv6 error", err)
				}
				if msg := Explain(err); !strings.Contains(msg, "has no IPv6 connection") {
					t.Fatalf("Explain = %q", msg)
				}
				return
			}
			if err != nil || strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("endpointCandidates(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
			}
		})
	}
}
