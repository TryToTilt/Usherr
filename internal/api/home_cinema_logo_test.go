package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/config"
)

// A site's logo is served only when the household put one in the config
// folder, and only for the sites it is meant for.
func TestLinkLogo(t *testing.T) {
	dir := t.TempDir()
	s := &Server{cfg: config.Config{ConfigDir: dir}}
	get := func(name string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/home/link-logo/"+name, nil)
		req.SetPathValue("name", name)
		rec := httptest.NewRecorder()
		s.handleLinkLogo(rec, req)
		return rec
	}
	if s.linkLogoFile("fandango") != "" || get("fandango").Code != http.StatusNotFound {
		t.Fatal("a logo without a file")
	}
	png := []byte("\x89PNG\r\n\x1a\nlogo")
	if err := os.MkdirAll(filepath.Join(dir, "link-logos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "link-logos", "fandango.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	rec := get("fandango")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Body.String() != string(png) {
		t.Fatalf("logo: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if err := os.WriteFile(filepath.Join(dir, "link-logos", "secret.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	// Each service its own picture: Cinemark has none yet.
	if s.linkLogoFile("cinemark") != "" || !s.linkLogos()["fandango"] || s.linkLogos()["cinemark"] {
		t.Fatalf("logos: %v", s.linkLogos())
	}
	for _, name := range []string{"secret", "../link-logos/fandango", "", "off"} {
		if get(name).Code != http.StatusNotFound {
			t.Fatalf("%q was served", name)
		}
	}
}

// An administrator uploads a picture for the button, and takes it away again.
func TestLinkLogoUpload(t *testing.T) {
	dir := t.TempDir()
	s := &Server{cfg: config.Config{ConfigDir: dir}}
	png := append([]byte{0x89}, []byte("PNG")...)
	png = append(png, 0x0D, 0x0A, 0x1A, 0x0A)
	png = append(png, []byte("first")...)
	put := func(data []byte) int {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		fw, _ := mw.CreateFormFile("file", "logo")
		_, _ = fw.Write(data)
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPut, "/api/home/link-logo/fandango", &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.SetPathValue("name", "fandango")
		rec := httptest.NewRecorder()
		s.handlePutLinkLogo(rec, req)
		return rec.Code
	}
	if put([]byte("<svg onload=alert(1)>")) != http.StatusBadRequest {
		t.Fatal("not a picture, but kept")
	}
	if put(png) != http.StatusOK || filepath.Base(s.linkLogoFile("fandango")) != "fandango.png" {
		t.Fatal("PNG not kept")
	}
	// A JPG replaces the PNG.
	if put(append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("jpeg")...)) != http.StatusOK || filepath.Base(s.linkLogoFile("fandango")) != "fandango.jpg" {
		t.Fatalf("JPG didn't replace it: %s", s.linkLogoFile("fandango"))
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/home/link-logo/fandango", nil)
	req.SetPathValue("name", "fandango")
	s.handleDeleteLinkLogo(httptest.NewRecorder(), req)
	if s.linkLogoFile("fandango") != "" {
		t.Fatal("still there after remove")
	}
}
