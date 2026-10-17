package subtitles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A subtitle file on the add-on's own host is fetched as it is (an add-on can
// live on the home network), but an add-on that points to another host on the
// home network is refused.
func TestAddonDownloadRefusesOtherPrivateHosts(t *testing.T) {
	file := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nHello there\n"))
	}))
	defer file.Close()
	c := NewAddonClient()
	ctx := context.Background()

	if _, err := c.DownloadFrom(ctx, file.URL, file.URL+"/a.srt", "en", 0, 0); err != nil {
		t.Fatalf("a file on the add-on's own host should download: %v", err)
	}
	_, err := c.DownloadFrom(ctx, "https://addon.example.com", file.URL+"/a.srt", "en", 0, 0)
	if err == nil {
		t.Fatal("a file on a private address of another host was fetched")
	}
	if !strings.Contains(err.Error(), "not a public address") {
		t.Fatalf("unexpected error: %v", err)
	}
}
