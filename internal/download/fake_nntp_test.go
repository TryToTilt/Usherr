package download

import (
	"bufio"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeNNTPServer is a minimal in-process NNTP server for tests, so nothing
// here needs a live Usenet provider (tests use local fixtures, not live
// network calls).
type fakeNNTPServer struct {
	listener net.Listener
	bodies   map[string][]byte // message-id (without <>) -> raw article body to serve
	addr     string
	port     int
	conns    atomic.Int32 // connections accepted so far
	// dropBodies is how many more BODY requests are answered by hanging up,
	// as a connection reset would.
	dropBodies atomic.Int32

	// latency is how long a BODY takes to start answering, in nanoseconds.
	latency atomic.Int64
	// refuseStat makes STAT answer that the server doesn't know the command.
	refuseStat atomic.Bool

	hitsMu sync.Mutex
	hits   map[string]int // BODY requests answered, by message-id
	drops  map[string]int // how many more requests for a message-id are hung up on
	hangs  map[string]int // how many more requests for a message-id are never answered
	stats  map[string]int // STAT requests answered, by message-id
}

// hangRequests makes the next n requests for message-id go unanswered with the
// connection left open, the way a stuck socket behaves.
func (s *fakeNNTPServer) hangRequests(id string, n int) {
	s.hitsMu.Lock()
	defer s.hitsMu.Unlock()
	if s.hangs == nil {
		s.hangs = map[string]int{}
	}
	s.hangs[id] = n
}

// setBody puts an article on the server, or with nil takes it away.
func (s *fakeNNTPServer) setBody(id string, body []byte) {
	s.hitsMu.Lock()
	defer s.hitsMu.Unlock()
	if body == nil {
		delete(s.bodies, id)
		return
	}
	s.bodies[id] = body
}

func (s *fakeNNTPServer) body(id string) ([]byte, bool) {
	s.hitsMu.Lock()
	defer s.hitsMu.Unlock()
	b, ok := s.bodies[id]
	return b, ok
}

// dropRequests makes the next n requests for message-id end with the server
// hanging up, as a connection reset would.
func (s *fakeNNTPServer) dropRequests(id string, n int) {
	s.hitsMu.Lock()
	defer s.hitsMu.Unlock()
	if s.drops == nil {
		s.drops = map[string]int{}
	}
	s.drops[id] = n
}

// hitCount is how many times message-id was requested.
func (s *fakeNNTPServer) hitCount(id string) int {
	s.hitsMu.Lock()
	defer s.hitsMu.Unlock()
	return s.hits[id]
}

// totalHits is how many articles were requested in all.
func (s *fakeNNTPServer) totalHits() int {
	s.hitsMu.Lock()
	defer s.hitsMu.Unlock()
	n := 0
	for _, v := range s.hits {
		n += v
	}
	return n
}

func newFakeNNTPServer(t *testing.T, bodies map[string][]byte) *fakeNNTPServer {
	t.Helper()
	return newFakeNNTPServerTB(t, bodies)
}

func newFakeNNTPServerTB(t testing.TB, bodies map[string][]byte) *fakeNNTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	s := &fakeNNTPServer{listener: ln, bodies: bodies, addr: "127.0.0.1", port: port}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeNNTPServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.conns.Add(1)
		go s.handle(conn)
	}
}

func (s *fakeNNTPServer) handle(conn net.Conn) {
	defer conn.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	rw.WriteString("200 fake nntp server ready\r\n")
	rw.Flush()

	for {
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "AUTHINFO USER"):
			rw.WriteString("381 password required\r\n")
		case strings.HasPrefix(upper, "AUTHINFO PASS"):
			rw.WriteString("281 authentication accepted\r\n")
		case strings.HasPrefix(upper, "GROUP"):
			rw.WriteString("211 0 0 0 group selected\r\n")
		case strings.HasPrefix(upper, "BODY"):
			msgID := strings.Trim(strings.TrimSpace(line[len("BODY"):]), "<>")
			if s.dropBodies.Add(-1) >= 0 {
				return // hang up without an answer
			}
			s.hitsMu.Lock()
			if s.drops[msgID] > 0 {
				s.drops[msgID]--
				s.hitsMu.Unlock()
				return
			}
			hang := s.hangs[msgID] > 0
			if hang {
				s.hangs[msgID]--
			}
			if s.hits == nil {
				s.hits = map[string]int{}
			}
			s.hits[msgID]++
			s.hitsMu.Unlock()
			if hang {
				_, _ = io.Copy(io.Discard, conn) // until the client gives up
				return
			}
			if d := s.latency.Load(); d > 0 {
				time.Sleep(time.Duration(d))
			}
			body, ok := s.body(msgID)
			if !ok {
				rw.WriteString("430 no such article\r\n")
				break
			}
			rw.WriteString("222 body follows\r\n")
			writeDotTerminated(rw, body)
		case strings.HasPrefix(upper, "STAT"):
			msgID := strings.Trim(strings.TrimSpace(line[len("STAT"):]), "<>")
			if s.refuseStat.Load() {
				rw.WriteString("500 command not recognized\r\n")
				break
			}
			s.hitsMu.Lock()
			if s.stats == nil {
				s.stats = map[string]int{}
			}
			s.stats[msgID]++
			s.hitsMu.Unlock()
			if _, ok := s.body(msgID); ok {
				rw.WriteString("223 0 <" + msgID + ">\r\n")
			} else {
				rw.WriteString("430 no such article\r\n")
			}
		case upper == "QUIT":
			rw.WriteString("205 bye\r\n")
			rw.Flush()
			return
		default:
			rw.WriteString("500 command not recognized\r\n")
		}
		rw.Flush()
	}
}

// writeDotTerminated writes body as an NNTP multi-line block: dot-stuffing
// any line that starts with '.', then a lone "." terminator line.
func writeDotTerminated(rw *bufio.ReadWriter, body []byte) {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, ".") {
			rw.WriteString(".")
		}
		rw.WriteString(line)
		rw.WriteString("\r\n")
	}
	rw.WriteString(".\r\n")
}
