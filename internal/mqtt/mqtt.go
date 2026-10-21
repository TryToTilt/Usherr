// Package mqtt is a small MQTT 3.1.1 client: connect (with a last will),
// publish and subscribe at QoS 0, and keep the connection alive. It is all
// Usherr's Marquee needs to talk to Home Assistant through a broker such as
// Mosquitto, without a library.
package mqtt

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Message is something published to a topic.
type Message struct {
	Topic   string
	Payload []byte
	Retain  bool
}

// Options say where and how to connect.
type Options struct {
	Addr      string // host:port
	TLS       bool
	Username  string
	Password  string
	ClientID  string
	KeepAlive time.Duration // 0: 30 seconds
	Will      *Message      // published by the broker when the connection is lost
}

// Client is a connection to a broker. Its methods may be called from any
// goroutine.
type Client struct {
	conn      net.Conn
	keepAlive time.Duration
	onMessage func(Message)
	wmu       sync.Mutex
	nextID    uint16
	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

const (
	typeConnect    = 1
	typeConnack    = 2
	typePublish    = 3
	typePuback     = 4
	typeSubscribe  = 8
	typeSuback     = 9
	typePingreq    = 12
	typePingresp   = 13
	typeDisconnect = 14
)

var connackErrors = map[byte]string{
	1: "the broker doesn't speak MQTT 3.1.1",
	2: "the broker refused the client id",
	3: "the broker is unavailable",
	4: "the user name or password is wrong",
	5: "this user isn't allowed to connect",
}

// Dial connects to the broker and waits for its answer. onMessage is called
// for every message on a subscribed topic, from the client's own goroutine.
func Dial(ctx context.Context, o Options, onMessage func(Message)) (*Client, error) {
	if o.KeepAlive <= 0 {
		o.KeepAlive = 30 * time.Second
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if o.TLS {
		host, _, _ := net.SplitHostPort(o.Addr)
		conn, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", o.Addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", o.Addr)
	}
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, keepAlive: o.KeepAlive, onMessage: onMessage, done: make(chan struct{})}
	if err := c.write(connectPacket(o)); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	kind, body, err := readPacket(r)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("no answer from the broker: %w", err)
	}
	if kind>>4 != typeConnack || len(body) < 2 {
		conn.Close()
		return nil, errors.New("the broker answered something other than MQTT")
	}
	if rc := body[1]; rc != 0 {
		conn.Close()
		if msg, ok := connackErrors[rc]; ok {
			return nil, errors.New(msg)
		}
		return nil, fmt.Errorf("the broker refused the connection (code %d)", rc)
	}
	go c.readLoop(r)
	go c.pingLoop()
	return c, nil
}

// Done is closed when the connection ends; Err then says why.
func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) Err() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.err
}

func (c *Client) fail(err error) {
	c.closeOnce.Do(func() {
		c.errMu.Lock()
		c.err = err
		c.errMu.Unlock()
		c.conn.Close()
		close(c.done)
	})
}

// Close says goodbye and closes the connection (the last will is not sent).
func (c *Client) Close() {
	_ = c.write([]byte{typeDisconnect << 4, 0})
	c.fail(errors.New("closed"))
}

// Publish sends a message at QoS 0.
func (c *Client) Publish(topic string, payload []byte, retain bool) error {
	var flags byte
	if retain {
		flags = 1
	}
	body := appendString(nil, topic)
	body = append(body, payload...)
	return c.write(packet(typePublish<<4|flags, body))
}

// Subscribe asks for the messages on these topic filters, at QoS 0.
func (c *Client) Subscribe(filters ...string) error {
	c.wmu.Lock()
	c.nextID++
	if c.nextID == 0 {
		c.nextID = 1
	}
	id := c.nextID
	c.wmu.Unlock()
	body := binary.BigEndian.AppendUint16(nil, id)
	for _, f := range filters {
		body = appendString(body, f)
		body = append(body, 0)
	}
	return c.write(packet(typeSubscribe<<4|2, body))
}

func (c *Client) write(b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := c.conn.Write(b)
	if err != nil {
		go c.fail(err)
	}
	return err
}

func (c *Client) readLoop(r *bufio.Reader) {
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.keepAlive * 3 / 2))
		kind, body, err := readPacket(r)
		if err != nil {
			c.fail(err)
			return
		}
		if kind>>4 != typePublish {
			continue // acknowledgements and ping answers
		}
		qos := (kind >> 1) & 3
		if len(body) < 2 {
			continue
		}
		n := int(binary.BigEndian.Uint16(body))
		if len(body) < 2+n {
			continue
		}
		m := Message{Topic: string(body[2 : 2+n]), Retain: kind&1 == 1}
		rest := body[2+n:]
		if qos > 0 {
			if len(rest) < 2 {
				continue
			}
			_ = c.write(packet(typePuback<<4, rest[:2]))
			rest = rest[2:]
		}
		m.Payload = append([]byte(nil), rest...)
		if c.onMessage != nil {
			c.onMessage(m)
		}
	}
}

func (c *Client) pingLoop() {
	t := time.NewTicker(c.keepAlive / 2)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if c.write([]byte{typePingreq << 4, 0}) != nil {
				return
			}
		}
	}
}

func connectPacket(o Options) []byte {
	body := appendString(nil, "MQTT")
	body = append(body, 4) // 3.1.1
	flags := byte(0x02)    // a clean session
	if o.Will != nil {
		flags |= 0x04
		if o.Will.Retain {
			flags |= 0x20
		}
	}
	if o.Username != "" {
		flags |= 0x80
		if o.Password != "" {
			flags |= 0x40
		}
	}
	body = append(body, flags)
	body = binary.BigEndian.AppendUint16(body, uint16(o.KeepAlive/time.Second))
	body = appendString(body, o.ClientID)
	if o.Will != nil {
		body = appendString(body, o.Will.Topic)
		body = appendBytes(body, o.Will.Payload)
	}
	if o.Username != "" {
		body = appendString(body, o.Username)
		if o.Password != "" {
			body = appendString(body, o.Password)
		}
	}
	return packet(typeConnect<<4, body)
}

func packet(first byte, body []byte) []byte {
	out := []byte{first}
	n := len(body)
	for {
		b := byte(n % 128)
		n /= 128
		if n > 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			break
		}
	}
	return append(out, body...)
}

func appendString(b []byte, s string) []byte { return appendBytes(b, []byte(s)) }

func appendBytes(b, s []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, uint16(len(s)))
	return append(b, s...)
}

// maxPacket keeps a broken or hostile broker from making us read a huge packet.
const maxPacket = 1 << 20

// readPacket reads one packet: its first byte and its body.
func readPacket(r *bufio.Reader) (byte, []byte, error) {
	first, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	n, mult := 0, 1
	for i := 0; ; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		n += int(b&0x7f) * mult
		if b&0x80 == 0 {
			break
		}
		mult *= 128
		if i >= 3 {
			return 0, nil, errors.New("a malformed packet")
		}
	}
	if n > maxPacket {
		return 0, nil, errors.New("a packet too big")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return first, body, nil
}

// ReadPacket and Packet are for tests that play the broker.
func ReadPacket(r *bufio.Reader) (byte, []byte, error) { return readPacket(r) }
func Packet(first byte, body []byte) []byte            { return packet(first, body) }
func AppendString(b []byte, s string) []byte           { return appendString(b, s) }
