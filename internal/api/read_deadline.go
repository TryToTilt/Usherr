package api

import (
	"io"
	"net/http"
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
func withBodyDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			rc := http.NewResponseController(w)
			if rc.SetReadDeadline(time.Now().Add(bodyReadTime)) == nil {
				r.Body = &deadlineBody{ReadCloser: r.Body, rc: rc}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// deadlineBody lifts the read deadline once the body is used up.
type deadlineBody struct {
	io.ReadCloser
	rc *http.ResponseController
}

func (b *deadlineBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		_ = b.rc.SetReadDeadline(time.Time{})
	}
	return n, err
}

// allowLongUpload lets the rest of this request's body take up to half an hour
// to arrive, for uploads that are big and may come over a slow connection.
func allowLongUpload(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(longUploadTime))
}
