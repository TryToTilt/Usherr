package httpsec

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"net/netip"
	"os"
	"strings"
	"sync"
)

// The address Docker's own port forwarder (docker-proxy), rootless Podman and
// similar hand every visitor to the container from is the container's default
// gateway. Whoever connects that way, from this very machine or from the
// internet, appears to come from that one private address, and the Host header
// and any X-Forwarded-For are whatever the visitor sent. So a request whose
// TCP peer is the default gateway can never be told apart from a stranger.

var (
	gatewaysOnce sync.Once
	gatewayList  []netip.Addr

	// loadGateways reads the routing tables; tests replace it.
	loadGateways = readDefaultGateways
)

// isDefaultGateway reports whether a is the default gateway of this machine or
// container.
func isDefaultGateway(a netip.Addr) bool {
	gatewaysOnce.Do(func() { gatewayList = loadGateways() })
	a = a.Unmap().WithZone("")
	for _, g := range gatewayList {
		if g == a {
			return true
		}
	}
	return false
}

// readDefaultGateways lists the next hops of the default routes (IPv4 and
// IPv6) from /proc. Anywhere that has no such files (not Linux) it is empty.
func readDefaultGateways() []netip.Addr {
	var out []netip.Addr
	out = append(out, parseRouteV4("/proc/net/route")...)
	out = append(out, parseRouteV6("/proc/net/ipv6_route")...)
	return out
}

// parseRouteV4 reads /proc/net/route: whitespace separated columns Iface,
// Destination, Gateway, ... in little-endian hex; a default route has
// Destination 00000000.
func parseRouteV4(path string) []netip.Addr {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []netip.Addr
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		c := strings.Fields(sc.Text())
		if len(c) < 3 || c[1] != "00000000" || c[2] == "00000000" {
			continue
		}
		b, err := hex.DecodeString(c[2])
		if err != nil || len(b) != 4 {
			continue
		}
		var v [4]byte
		binary.BigEndian.PutUint32(v[:], binary.LittleEndian.Uint32(b))
		out = append(out, netip.AddrFrom4(v))
	}
	return out
}

// parseRouteV6 reads /proc/net/ipv6_route: columns Destination, prefix length,
// Source, prefix length, Next hop, ... in plain hex. A default route has an
// all-zero destination with prefix length 00.
func parseRouteV6(path string) []netip.Addr {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []netip.Addr
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		c := strings.Fields(sc.Text())
		if len(c) < 5 || c[0] != strings.Repeat("0", 32) || c[1] != "00" || c[4] == strings.Repeat("0", 32) {
			continue
		}
		b, err := hex.DecodeString(c[4])
		if err != nil || len(b) != 16 {
			continue
		}
		out = append(out, netip.AddrFrom16([16]byte(b)))
	}
	return out
}
