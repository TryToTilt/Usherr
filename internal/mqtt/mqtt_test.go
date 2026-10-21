package mqtt

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

// A broker that accepts one client, checks its CONNECT, and records what it
// publishes; it can send the client a message.
func fakeBroker(t *testing.T, refuse byte) (addr string, got chan []byte, send chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got, send = make(chan []byte, 16), make(chan []byte, 4)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		kind, body, err := readPacket(r)
		if err != nil || kind>>4 != typeConnect {
			return
		}
		got <- append([]byte{kind}, body...)
		_, _ = conn.Write([]byte{typeConnack << 4, 2, 0, refuse})
		if refuse != 0 {
			return
		}
		go func() {
			for b := range send {
				_, _ = conn.Write(b)
			}
		}()
		for {
			kind, body, err := readPacket(r)
			if err != nil {
				return
			}
			got <- append([]byte{kind}, body...)
		}
	}()
	return ln.Addr().String(), got, send
}

func next(t *testing.T, got chan []byte) []byte {
	t.Helper()
	select {
	case b := <-got:
		return b
	case <-time.After(3 * time.Second):
		t.Fatal("the broker got nothing")
	}
	return nil
}

func TestClientTalksToABroker(t *testing.T) {
	addr, got, send := fakeBroker(t, 0)
	msgs := make(chan Message, 4)
	c, err := Dial(context.Background(), Options{Addr: addr, Username: "ha", Password: "pw", ClientID: "usherr-test",
		Will: &Message{Topic: "usherr/marquee/status", Payload: []byte("offline"), Retain: true}}, func(m Message) { msgs <- m })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	connect := next(t, got)
	if !strings.Contains(string(connect), "MQTT") || !strings.Contains(string(connect), "usherr-test") || !strings.Contains(string(connect), "offline") || !strings.Contains(string(connect), "pw") {
		t.Fatalf("connect: %q", connect)
	}
	if flags := connect[1+2+4+1]; flags&0xC6 != 0xC6 || flags&0x20 == 0 {
		t.Fatalf("connect flags %08b", flags)
	}

	if err := c.Publish("usherr/marquee/a/state", []byte(`{"title":"Heat"}`), true); err != nil {
		t.Fatal(err)
	}
	pub := next(t, got)
	if pub[0] != typePublish<<4|1 || !strings.HasSuffix(string(pub), `usherr/marquee/a/state{"title":"Heat"}`) {
		t.Fatalf("publish: %q", pub)
	}

	if err := c.Subscribe("usherr/marquee/+/cmd/+"); err != nil {
		t.Fatal(err)
	}
	sub := next(t, got)
	if sub[0] != typeSubscribe<<4|2 || !strings.Contains(string(sub), "usherr/marquee/+/cmd/+") {
		t.Fatalf("subscribe: %q", sub)
	}

	// The broker sends a command, once at QoS 0 and once at QoS 1 (acknowledged).
	send <- packet(typePublish<<4, append(appendString(nil, "usherr/marquee/a/cmd/next"), []byte("PRESS")...))
	body := appendString(nil, "usherr/marquee/a/set/dim")
	body = binary.BigEndian.AppendUint16(body, 7)
	send <- packet(typePublish<<4|2, append(body, []byte("ON")...))
	for _, want := range []string{"usherr/marquee/a/cmd/next=PRESS", "usherr/marquee/a/set/dim=ON"} {
		select {
		case m := <-msgs:
			if m.Topic+"="+string(m.Payload) != want {
				t.Fatalf("message %s=%s, want %s", m.Topic, m.Payload, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no %s", want)
		}
	}
	if ack := next(t, got); ack[0] != typePuback<<4 || binary.BigEndian.Uint16(ack[1:]) != 7 {
		t.Fatalf("puback: %v", ack)
	}
}

func TestBrokerRefusesTheLogin(t *testing.T) {
	addr, _, _ := fakeBroker(t, 4)
	_, err := Dial(context.Background(), Options{Addr: addr, ClientID: "x"}, nil)
	if err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("err = %v", err)
	}
}
