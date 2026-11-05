package servicecheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
	"github.com/rdborg/mediarium/internal/plainerror"
)

// ProbeTimeout is how long one check waits for an answer, like the watchdog's
// ten seconds.
const ProbeTimeout = 10 * time.Second

// maxBody is how much of an answer is read to look for the keyword.
const maxBody = 1 << 20

// NewClient is the client the checks use: through netguard (LAN addresses
// are fine, cloud metadata addresses are not), and it does not follow
// redirects, so a 3xx answer is judged as it is.
func NewClient() *http.Client {
	return &http.Client{
		Transport: netguard.Transport(),
		Timeout:   ProbeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Probe asks c's address once and judges the answer.
func Probe(ctx context.Context, client *http.Client, c Check, now func() time.Time) Result {
	if now == nil {
		now = time.Now
	}
	start := now()
	res := Result{At: start}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		res.Error = "the address is not valid"
		return res
	}
	req.Header.Set("User-Agent", "Usherr service check")
	resp, err := client.Do(req)
	if err != nil {
		res.Ms = elapsedMs(start, now())
		res.Error = reason(err)
		return res
	}
	defer resp.Body.Close()
	res.Code = resp.StatusCode
	var body []byte
	if c.Keyword != "" {
		body, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
		if err != nil && !errors.Is(err, io.EOF) {
			res.Ms = elapsedMs(start, now())
			res.Error = "the answer broke off: " + reason(err)
			return res
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	}
	res.Ms = elapsedMs(start, now())
	if !statusOK(c.Expect, resp.StatusCode) {
		res.Error = fmt.Sprintf("it answered %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		return res
	}
	if c.Keyword != "" && !bytes.Contains(bytes.ToLower(body), bytes.ToLower([]byte(c.Keyword))) {
		res.Error = fmt.Sprintf("the answer does not contain %q", c.Keyword)
		return res
	}
	res.OK = true
	return res
}

// statusOK reports whether code is a good answer for expect.
func statusOK(expect string, code int) bool {
	switch expect {
	case Expect200:
		return code == http.StatusOK
	case ExpectAny:
		return code > 0
	default:
		return code >= 200 && code < 400
	}
}

// reason says in plain words why a request failed, without the address (it
// may hold a key).
func reason(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return fmt.Sprintf("no answer within %d seconds", int(ProbeTimeout/time.Second))
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err // the request's address is left out
	}
	return plainerror.Message(netguard.CleanError(err))
}

func elapsedMs(from, to time.Time) int {
	ms := int(to.Sub(from) / time.Millisecond)
	if ms < 0 {
		return 0
	}
	return ms
}
