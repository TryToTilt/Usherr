package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// A news server with a self-signed certificate gets a plain answer from the
// Test button, not Go's x509 text.
func TestNNTPTestExplainsASelfSignedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler()) // self-signed; the handshake fails before any NNTP
	t.Cleanup(srv.Close)
	host, portText, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)

	res := testNNTP(host, port, true, "ryan", "secret")
	if res.OK {
		t.Fatal("the test must fail")
	}
	if !strings.HasPrefix(res.Message, "The server's certificate isn't trusted") {
		t.Fatalf("message = %q", res.Message)
	}
	if strings.Contains(res.Message, "x509") || strings.Contains(res.Message, "tls:") {
		t.Fatalf("no raw Go text in the message: %q", res.Message)
	}
}
