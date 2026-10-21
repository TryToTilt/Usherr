package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

type countingBody struct {
	io.ReadCloser
	n *atomic.Int64
}

func (c countingBody) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// An answer sent in many writes, to a request whose body the handler never
// reads, drains at most one lot of the body (256 KB) in total, not one per write.
func TestUnreadBodyIsDrainedOnceHoweverManyWrites(t *testing.T) {
	const maxDrain = 256 << 10
	for _, tc := range []struct {
		name   string
		writes int
	}{
		{"one write", 1},
		{"three writes", 3},
		{"ten writes", 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var read atomic.Int64
			handler := api.WithBodyDeadlineForTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for i := 0; i < tc.writes; i++ {
					_, _ = io.WriteString(w, "x")
					w.(http.Flusher).Flush()
				}
			}))
			// The counter sits outside the middleware, so it sees every byte it reads.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Body = countingBody{ReadCloser: r.Body, n: &read}
				handler.ServeHTTP(w, r)
			}))
			defer srv.Close()

			pr, pw := io.Pipe()
			go func() {
				chunk := make([]byte, 32<<10)
				for i := 0; i < 64; i++ { // 2 MB, far more than one drain
					if _, err := pw.Write(chunk); err != nil {
						return
					}
				}
				_ = pw.Close()
			}()
			req, _ := http.NewRequest(http.MethodPost, srv.URL, pr)
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			_ = pr.Close()
			// A little more than one drain can be in the server's own buffers.
			if got := read.Load(); got > maxDrain+(64<<10) {
				t.Fatalf("the server read %d bytes of a body nobody asked for, more than one drain of %d", got, maxDrain)
			}
		})
	}
}
