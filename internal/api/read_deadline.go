package api

import (
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// A request that carries a body has to deliver it in good time, or a client
// that promises a body and then sends nothing holds a connection open for ever.
// The deadline is set here for the body only, not on the server as a whole: a
// server-wide read timeout also fires on a long video stream or event stream
// that has no body to read, and cancels it.
//
// Routes that really take a long upload (a backup, a program update, a cover
// picture, an NZB or torrent file) ask for longer with allowLongUpload.
var (
	bodyReadTime   = 60 * time.Second
	longUploadTime = 30 * time.Minute
)

// withBodyDeadline gives every request that has a body a limited time to send
// it. The limit is lifted again as soon as the whole body has arrived, so a
// handler that then runs or streams for a long time is not cut short.
//
// An answer that goes out before the whole body has arrived (a refusal, or an
// error because the body stalled) also closes the connection. Without that the
// Go server first tries to read and throw away what is left of the body, which
// waits for the very client that stopped sending, so the answer only arrives
// when the deadline runs out; and a connection left open after a cut-off body
// would take the rest of it for the start of the next request.
func withBodyDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			rc := http.NewResponseController(w)
			if rc.SetReadDeadline(time.Now().Add(bodyReadTime)) == nil {
				body := &deadlineBody{ReadCloser: r.Body, rc: rc}
				r.Body = body
				w = &closeIfBodyUnread{ResponseWriter: w, body: body}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// deadlineBody lifts the read deadline once the body is used up.
type deadlineBody struct {
	io.ReadCloser
	rc     *http.ResponseController
	done   atomic.Bool // the end of the body has been read
	failed atomic.Bool // a read failed: the client stalled or hung up
}

func (b *deadlineBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.done.Store(true)
		_ = b.rc.SetReadDeadline(time.Time{})
	} else if err != nil {
		b.failed.Store(true)
	}
	return n, err
}

// The Go server reads and throws away up to 256 KB of an unread body before it
// answers, so a connection can be kept for the next request. Our wrapper hides
// the body from it, so that step is done here instead, but for a short time
// only: a client that has stopped sending must not hold the answer back.
var drainTime = time.Second

const maxDrain = 256 << 10

// leftOver reads what is left of an unread body when an answer is about to go
// out, and says whether the connection must be closed after it: the body
// ended in an error, or the rest of it did not arrive in time.
func (b *deadlineBody) leftOver() bool {
	if b.done.Load() {
		return false
	}
	if b.failed.Load() {
		return true
	}
	_ = b.rc.SetReadDeadline(time.Now().Add(drainTime))
	_, err := io.CopyN(io.Discard, b, maxDrain+1)
	// More data than that is coming and the client is sending it: the server
	// deals with that itself (it closes after the answer, and waits a moment
	// so the client can still read it). Only a client that has gone quiet or
	// hung up needs the close asked for here.
	return err != nil && err != io.EOF
}

// closeIfBodyUnread asks for the connection to be closed after the answer when
// the answer is sent before the request body has been read to its end.
type closeIfBodyUnread struct {
	http.ResponseWriter
	body    *deadlineBody
	checked bool // the leftover body has been dealt with; once is enough
}

func (c *closeIfBodyUnread) check() {
	// Done once, at the first part of the answer. Later writes of a long
	// answer must not read another 256 KB of the body each time.
	if c.checked {
		return
	}
	c.checked = true
	if c.body.leftOver() {
		c.Header().Set("Connection", "close")
	}
}

func (c *closeIfBodyUnread) WriteHeader(code int) {
	// An interim answer (100 Continue and the like) is not the answer yet.
	if code >= 200 {
		c.check()
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *closeIfBodyUnread) Write(p []byte) (int, error) {
	c.check()
	return c.ResponseWriter.Write(p)
}

func (c *closeIfBodyUnread) Flush() {
	c.check()
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the real writer.
func (c *closeIfBodyUnread) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// allowLongUpload lets the rest of this request's body take up to half an hour
// to arrive, for uploads that are big and may come over a slow connection.
func allowLongUpload(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(longUploadTime))
}
