package httpsec

import (
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A request handed over by the container's default gateway (Docker's port
// forwarder, rootless Podman) is never "from home", whatever Host or
// X-Forwarded-For say, because it looks the same for a stranger.
func TestFromHomeRefusesTheDefaultGateway(t *testing.T) {
	old := loadGateways
	defer func() { loadGateways = old; gatewaysOnce = sync.Once{} }()
	loadGateways = func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("172.17.0.1")} }
	gatewaysOnce = sync.Once{}

	tests := []struct {
		name    string
		remote  string
		host    string
		headers map[string]string
		want    bool
	}{
		{"stranger via the gateway claiming localhost", "172.17.0.1:5000", "localhost:8264", nil, false},
		{"stranger via the gateway with a forged private forwarded address", "172.17.0.1:5000", "localhost:8264", map[string]string{"X-Forwarded-For": "192.168.1.5"}, false},
		{"a LAN device whose address Docker kept", "192.168.1.50:5000", "192.168.1.10:8264", nil, true},
		{"another container, not the gateway", "172.17.0.5:5000", "nas:8264", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := req("POST", tt.remote, tt.headers)
			r.Host = tt.host
			if got := mustProxies(t, "private").FromHome(r); got != tt.want {
				t.Errorf("FromHome = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseDefaultRoutes(t *testing.T) {
	dir := t.TempDir()
	v4 := filepath.Join(dir, "route")
	if err := os.WriteFile(v4, []byte("Iface\tDestination\tGateway\tFlags\neth0\t00000000\t010011AC\t0003\neth0\t000011AC\t00000000\t0001\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := parseRouteV4(v4); len(got) != 1 || got[0] != netip.MustParseAddr("172.17.0.1") {
		t.Errorf("ipv4 default route: %v", got)
	}
	v6 := filepath.Join(dir, "ipv6_route")
	if err := os.WriteFile(v6, []byte("00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000400 00000001 00000000 00000003 eth0\n"+
		"fd000000000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 00000100 00000001 00000000 00000001 eth0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := parseRouteV6(v6); len(got) != 1 || got[0] != netip.MustParseAddr("fe80::1") {
		t.Errorf("ipv6 default route: %v", got)
	}
	if got := parseRouteV4(filepath.Join(dir, "missing")); got != nil {
		t.Errorf("missing file: %v", got)
	}
}
