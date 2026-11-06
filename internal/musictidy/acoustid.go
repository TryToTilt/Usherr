// Adapted from DroppedNeedle (AGPL-3.0), backend/infrastructure/audio/fingerprinter.py

// Package musictidy tidies an existing music folder the way MusicBrainz
// Picard does: it works out which album and track each file is (from what
// Usherr already knows, the files' tags and names, and AcoustID audio
// fingerprints), proposes new names and tags, writes them only when an
// administrator applies them, and can put everything back.
package musictidy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AcoustIDURL is the AcoustID lookup service.
const AcoustIDURL = "https://api.acoustid.org/v2/lookup"

const (
	// MinScore is the lowest AcoustID score taken as a match; a weaker best
	// result is no match at all.
	MinScore = 0.70
	// acoustIDInterval keeps lookups to three a second, AcoustID's limit.
	acoustIDInterval = time.Second / 3
	acoustIDTimeout  = 15 * time.Second
	// maxRetryAfter caps how long a 429 is waited out.
	maxRetryAfter = 60 * time.Second
)

// ErrNoKey is returned when no AcoustID application key is set.
var ErrNoKey = errors.New("no AcoustID application key")

// ErrRejected is a lookup AcoustID refused (a bad key, a malformed request);
// asking again would not help.
var ErrRejected = errors.New("AcoustID refused the lookup")

// FPRecording is a recording AcoustID knows a fingerprint as.
type FPRecording struct {
	ID            string
	Title         string
	ReleaseGroups []FPReleaseGroup
}

// FPReleaseGroup is a release group (album) a recording is on.
type FPReleaseGroup struct {
	ID    string
	Title string
	Type  string
}

// FPMatch is the best AcoustID result for a fingerprint.
type FPMatch struct {
	Score      float64
	Recordings []FPRecording
}

// AcoustID looks fingerprints up. Key is read on every call, so a key saved
// in the settings is used at once.
type AcoustID struct {
	Key     func() string
	BaseURL string       // AcoustIDURL when empty
	HTTP    *http.Client // a client with a 15 second timeout when nil
	// Sleep waits; tests replace it. nil waits for real.
	Sleep func(ctx context.Context, d time.Duration) error

	mu   sync.Mutex
	next time.Time // the earliest the next request may go
}

func (c *AcoustID) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// wait is the token bucket: one request every acoustIDInterval.
func (c *AcoustID) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	at := c.next
	if at.Before(now) {
		at = now
	}
	c.next = at.Add(acoustIDInterval)
	c.mu.Unlock()
	return c.sleep(ctx, time.Until(at))
}

// Lookup asks AcoustID which recordings a fingerprint is. ok is false when
// there is no confident match (no result, or the best scores under
// MinScore). Errors are for the caller to log and carry on without
// fingerprints: ErrNoKey, ErrRejected, a network failure.
func (c *AcoustID) Lookup(ctx context.Context, fingerprint string, durationSec int) (FPMatch, bool, error) {
	key := ""
	if c.Key != nil {
		key = strings.TrimSpace(c.Key())
	}
	if key == "" {
		return FPMatch{}, false, ErrNoKey
	}
	if fingerprint == "" || durationSec <= 0 {
		return FPMatch{}, false, nil // AcoustID answers nothing useful without both
	}
	form := url.Values{
		"client":      {key},
		"duration":    {strconv.Itoa(durationSec)},
		"fingerprint": {fingerprint},
		"meta":        {"recordings releasegroups"},
	}
	for attempt := 0; ; attempt++ {
		if err := c.wait(ctx); err != nil {
			return FPMatch{}, false, err
		}
		body, retryAfter, err := c.post(ctx, form)
		if retryAfter > 0 && attempt < 2 {
			if err := c.sleep(ctx, retryAfter); err != nil {
				return FPMatch{}, false, err
			}
			continue
		}
		if err != nil {
			return FPMatch{}, false, err
		}
		return parseLookup(body)
	}
}

// post sends one lookup. retryAfter is set on a 429.
func (c *AcoustID) post(ctx context.Context, form url.Values) ([]byte, time.Duration, error) {
	base := c.BaseURL
	if base == "" {
		base = AcoustIDURL
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: acoustIDTimeout}
	}
	rctx, cancel := context.WithTimeout(ctx, acoustIDTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, base, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("AcoustID: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("AcoustID: read answer: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, retryAfterOf(resp.Header.Get("Retry-After")), errors.New("AcoustID: too many requests")
	case resp.StatusCode >= 500:
		return nil, 0, fmt.Errorf("AcoustID: server error %d", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, 0, fmt.Errorf("%w (%d)", ErrRejected, resp.StatusCode)
	}
	return body, 0, nil
}

// retryAfterOf reads a Retry-After header in seconds; a missing or odd one
// waits a few seconds, and a long one is capped.
func retryAfterOf(h string) time.Duration {
	d := 5 * time.Second
	if v, err := strconv.ParseFloat(strings.TrimSpace(h), 64); err == nil && v > 0 {
		d = time.Duration(v * float64(time.Second))
	}
	return min(d, maxRetryAfter)
}

type lookupAnswer struct {
	Status string `json:"status"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
	Results []struct {
		Score      float64 `json:"score"`
		Recordings []struct {
			ID            string `json:"id"`
			Title         string `json:"title"`
			ReleaseGroups []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"releasegroups"`
		} `json:"recordings"`
	} `json:"results"`
}

// parseLookup reads an answer: only the first (best) result counts, and
// only when it scores at least MinScore and names a recording.
func parseLookup(body []byte) (FPMatch, bool, error) {
	var a lookupAnswer
	if err := json.Unmarshal(body, &a); err != nil {
		return FPMatch{}, false, fmt.Errorf("AcoustID: malformed answer: %w", err)
	}
	if a.Status != "" && a.Status != "ok" {
		return FPMatch{}, false, fmt.Errorf("%w: %s", ErrRejected, a.Error.Message)
	}
	if len(a.Results) == 0 {
		return FPMatch{}, false, nil
	}
	best := a.Results[0]
	if best.Score < MinScore {
		return FPMatch{Score: best.Score}, false, nil
	}
	m := FPMatch{Score: best.Score}
	for _, r := range best.Recordings {
		if r.ID == "" {
			continue
		}
		rec := FPRecording{ID: r.ID, Title: r.Title}
		for _, g := range r.ReleaseGroups {
			if g.ID != "" {
				rec.ReleaseGroups = append(rec.ReleaseGroups, FPReleaseGroup{ID: g.ID, Title: g.Title, Type: g.Type})
			}
		}
		m.Recordings = append(m.Recordings, rec)
	}
	if len(m.Recordings) == 0 {
		return m, false, nil
	}
	return m, true, nil
}
