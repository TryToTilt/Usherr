package torrentclient

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/anacrolix/torrent"
)

type dialOnly struct{}

func (dialOnly) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("no")
}

type withUDP struct{ dialOnly }

func (withUDP) ListenPacket(string, string) (net.PacketConn, error) {
	return net.ListenPacket("udp4", "127.0.0.1:0")
}

func TestTrackerSocketsNeverErrorWithVPN(t *testing.T) {
	for name, tun := range map[string]TunnelDialer{"tunnel without UDP": dialOnly{}, "tunnel with UDP": withUDP{}} {
		cfg := torrent.NewDefaultClientConfig()
		applyNetworkPolicy(cfg, tun, false)
		pc, err := cfg.TrackerListenPacket("udp4", "")
		if err != nil || pc == nil {
			t.Fatalf("%s: got %v, %v (the torrent library panics on an error)", name, pc, err)
		}
		_, werr := pc.WriteTo([]byte("x"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9})
		if name == "tunnel without UDP" && werr == nil {
			t.Errorf("%s: a write went out", name)
		}
		pc.Close()
	}
}
