package api_test

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

// stalledPost sends a request head promising length bytes, then the first part of
// the body, waits, and sends the rest. It answers the status line and the body
// the server sent back, or an error when the server closed the connection.
func stalledPost(t *testing.T, base, path, cookie, contentType string, length int, first, rest string, stall time.Duration) (string, string, error) {
	t.Helper()
	u, _ := url.Parse(base)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: %s\r\nContent-Length: %d\r\n", path, u.Host, contentType, length)
	if cookie != "" {
		head += "Cookie: " + cookie + "\r\n"
	}
	head += "\r\n"
	if _, err := io.WriteString(conn, head+first); err != nil {
		t.Fatal(err)
	}
	time.Sleep(stall)
	if rest != "" {
		_, _ = io.WriteString(conn, rest) // may fail when the server already hung up
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.Status, string(b), nil
}

// A client that promises a body and then goes quiet must not hold the
// connection open, but a backup upload on a slow line may take its time.
func TestSlowRequestBodyIsCutOffExceptForBigUploads(t *testing.T) {
	defer api.SetBodyTimesForTest(300*time.Millisecond, time.Minute)()
	_, base, admin := loginNewServer(t)
	u, _ := url.Parse(base)
	var cookie []string
	for _, c := range admin.Jar.Cookies(u) {
		cookie = append(cookie, c.Name+"="+c.Value)
	}

	t.Run("a stalled body is dropped within the limit, not minutes later", func(t *testing.T) {
		u2, _ := url.Parse(base)
		conn, err := net.Dial("tcp", u2.Host)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_, _ = fmt.Fprintf(conn, "POST /api/auth/login HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{", u2.Host)
		start := time.Now()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, _ = io.ReadAll(conn) // returns when the server answers and closes
		if took := time.Since(start); took > 3*time.Second {
			t.Fatalf("the server kept a stalled request open for %v", took)
		}
	})

	t.Run("a backup upload may stall longer than the normal limit", func(t *testing.T) {
		boundary := "xBOUNDARYx"
		part1 := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"b.zip\"\r\nContent-Type: application/zip\r\n\r\nnot "
		part2 := "a zip at all\r\n--" + boundary + "--\r\n"
		status, body, err := stalledPost(t, base, "/api/system/restore", strings.Join(cookie, "; "),
			"multipart/form-data; boundary="+boundary, len(part1)+len(part2), part1, part2, time.Second)
		if err != nil {
			t.Fatalf("the upload was cut off after a pause: %v", err)
		}
		if !strings.HasPrefix(status, "400") || !strings.Contains(body, "not a valid Mediarium backup") {
			t.Fatalf("the upload should have arrived whole and been judged as a bad backup: %s %s", status, body)
		}
	})
}
