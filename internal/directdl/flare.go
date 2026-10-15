package directdl

// FlareSolverr (https://github.com/FlareSolverr/FlareSolverr) opens a page in
// a real browser, passes Cloudflare's check, and hands the page back. Only
// used when a page is behind that check.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (c *Client) flarePage(ctx context.Context, u string) (string, error) {
	base := ""
	if c.Flare != nil {
		base = strings.TrimRight(strings.TrimSpace(c.Flare()), "/")
	}
	if base == "" {
		return "", ErrChallenge
	}
	body, _ := json.Marshal(map[string]any{"cmd": "request.get", "url": u, "maxTimeout": 60000})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("couldn't reach FlareSolverr: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Solution struct {
			Status   int    `json:"status"`
			Response string `json:"response"`
		} `json:"solution"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("FlareSolverr's answer couldn't be read: %w", err)
	}
	if out.Status != "ok" || out.Solution.Status != http.StatusOK {
		return "", fmt.Errorf("FlareSolverr couldn't open the page: %s", out.Message)
	}
	return out.Solution.Response, nil
}

// TestFlare checks FlareSolverr answers at base.
func TestFlare(ctx context.Context, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach FlareSolverr: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Msg string `json:"msg"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || !strings.Contains(out.Msg, "FlareSolverr") {
		return fmt.Errorf("something other than FlareSolverr answered at %s", base)
	}
	return nil
}
