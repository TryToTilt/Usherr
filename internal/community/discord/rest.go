package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdborg/mediarium/internal/community"
)

// APIError is an answer from Discord that isn't a success.
type APIError struct {
	Status  int
	Code    int    // Discord's own error code (50013: missing permissions, …)
	Message string // Discord's words
}

func (e *APIError) Error() string {
	return fmt.Sprintf("discord: %d %s (code %d)", e.Status, e.Message, e.Code)
}

// rest calls Discord's HTTP API, waiting out its rate limits. Discord says
// how many calls each route has left in the answer's headers, and how long to
// wait after a 429; a bot that keeps going anyway is banned for a while, so
// this waits as told, and stops for good once the token is refused.
type rest struct {
	base   string // "https://discord.com/api/v10"
	token  string
	agent  string
	client *http.Client

	mu      sync.Mutex
	waits   map[string]time.Time // route → no calls before
	global  time.Time            // no calls at all before
	refused atomic.Bool          // the token was refused (401)
	// onRefused is told once when the token is refused.
	onRefused func()
}

// route is the rate-limit key of a call: its method and path, with ids
// replaced, except the channel, server or webhook it acts on (Discord limits
// each of those on its own).
func route(method, path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range parts {
		if p == "" || p[0] < '0' || p[0] > '9' {
			continue
		}
		if i > 0 && (parts[i-1] == "channels" || parts[i-1] == "guilds" || parts[i-1] == "webhooks") {
			continue
		}
		parts[i] = "{id}"
	}
	return method + " /" + strings.Join(parts, "/")
}

func (r *rest) wait(ctx context.Context, key string) error {
	r.mu.Lock()
	until := r.waits[key]
	if r.global.After(until) {
		until = r.global
	}
	r.mu.Unlock()
	d := time.Until(until)
	if d <= 0 {
		return nil
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

func (r *rest) holdOff(key string, d time.Duration, global bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	until := time.Now().Add(d)
	if global {
		if until.After(r.global) {
			r.global = until
		}
		return
	}
	if r.waits == nil {
		r.waits = map[string]time.Time{}
	}
	if until.After(r.waits[key]) {
		r.waits[key] = until
	}
}

// seconds reads a header or field Discord gives in seconds ("1.25").
func seconds(v string) time.Duration {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 0 {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}

// do calls Discord: body (when not nil) goes as JSON, and a JSON answer is
// read into out (when not nil).
func (r *rest) do(ctx context.Context, method, path string, body, out any) error {
	if r.refused.Load() {
		return community.ErrBadToken
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	key := route(method, path)
	for try := 0; ; try++ {
		if err := r.wait(ctx, key); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, r.base+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bot "+r.token)
		req.Header.Set("User-Agent", r.agent)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			r.holdOff(key, seconds(resp.Header.Get("X-RateLimit-Reset-After")), false)
		}
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			var limit struct {
				RetryAfter float64 `json:"retry_after"`
				Global     bool    `json:"global"`
			}
			_ = json.Unmarshal(data, &limit)
			d := time.Duration(limit.RetryAfter * float64(time.Second))
			if d <= 0 {
				d = seconds(resp.Header.Get("Retry-After"))
			}
			r.holdOff(key, max(d, 100*time.Millisecond), limit.Global || resp.Header.Get("X-RateLimit-Global") == "true")
			if try < 3 {
				continue
			}
			return &APIError{Status: resp.StatusCode, Message: "rate limited"}
		case resp.StatusCode == http.StatusUnauthorized:
			if r.refused.CompareAndSwap(false, true) && r.onRefused != nil {
				r.onRefused()
			}
			return community.ErrBadToken
		case resp.StatusCode >= 300:
			e := &APIError{Status: resp.StatusCode}
			var answer struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(data, &answer) == nil {
				e.Code, e.Message = answer.Code, answer.Message
			}
			switch resp.StatusCode {
			case http.StatusForbidden:
				return errors.Join(community.ErrNotAllowed, e)
			case http.StatusNotFound:
				return errors.Join(community.ErrNotFound, e)
			}
			return e
		}
		if out != nil && len(data) > 0 {
			return json.Unmarshal(data, out)
		}
		return nil
	}
}
