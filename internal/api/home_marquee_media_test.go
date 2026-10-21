package api_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// Your own Marquee slides and pre-show pieces: a picture is uploaded, served
// back and listed for the screens; a web page and a slide of words need no
// file; one can be switched off and removed; anything else is turned away.
func TestMarqueeMedia(t *testing.T) {
	_, base, admin := loginNewServer(t)
	list := func() []any {
		return getJSON[map[string]any](t, admin, base+"/api/home/marquee/media")["media"].([]any)
	}
	if n := len(list()); n != 0 {
		t.Fatalf("%d media at the start", n)
	}

	upload := func(name string, data []byte, place string) int {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		_ = mw.WriteField("place", place)
		_ = mw.WriteField("title", "Lobby card")
		_ = mw.WriteField("seconds", "9")
		fw, _ := mw.CreateFormFile("file", name)
		_, _ = fw.Write(data)
		_ = mw.Close()
		req, _ := http.NewRequest(http.MethodPost, base+"/api/home/marquee/media", &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := admin.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	png := append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{7}, 200)...)
	if st := upload("card.png", png, "preshow"); st != http.StatusOK {
		t.Fatalf("upload: %d", st)
	}
	if st := upload("notes.txt", []byte("just some words, not a picture"), "slide"); st != http.StatusBadRequest {
		t.Fatalf("a text file answered %d", st)
	}
	items := list()
	if len(items) != 1 {
		t.Fatalf("media: %v", items)
	}
	pic := items[0].(map[string]any)
	if pic["kind"] != "image" || pic["place"] != "preshow" || pic["seconds"] != float64(9) || pic["title"] != "Lobby card" {
		t.Fatalf("picture: %v", pic)
	}
	resp, err := admin.Get(base + pic["fileUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, png) || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("file: %d %q %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(got))
	}

	// A web page and words.
	if st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/media/page", map[string]any{"kind": "web", "url": "javascript:alert(1)"}); st != http.StatusBadRequest {
		t.Fatalf("a bad address answered %d %v", st, body)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/media/page", map[string]any{"kind": "web", "title": "Weather", "url": "https://weather.example.com/now"}); st != http.StatusOK {
		t.Fatalf("web page: %d", st)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/marquee/media/page", map[string]any{"kind": "text", "title": "Movie night", "body": "Friday at 8"}); st != http.StatusOK {
		t.Fatalf("words: %d", st)
	}
	if n := len(list()); n != 3 {
		t.Fatalf("%d media", n)
	}
	// The web page may now be shown in a frame.
	resp, err = admin.Get(base + "/marquee")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "https://weather.example.com") {
		t.Fatalf("frame not allowed: %s", csp)
	}

	// The screens get the switched-on ones.
	screens := getJSON[map[string]any](t, admin, base+"/api/home/marquee")["media"].([]any)
	if len(screens) != 3 {
		t.Fatalf("screens see %d", len(screens))
	}
	id := pic["id"].(float64)
	if st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/marquee/media/"+strconv.Itoa(int(id)), map[string]any{"place": "preshow", "title": "Lobby card", "seconds": 9, "enabled": false}); st != http.StatusOK {
		t.Fatalf("switch off: %d", st)
	}
	if n := len(getJSON[map[string]any](t, admin, base+"/api/home/marquee")["media"].([]any)); n != 2 {
		t.Fatalf("screens see %d after switching one off", n)
	}
	if st, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/marquee/media/"+strconv.Itoa(int(id)), nil); st != http.StatusOK {
		t.Fatalf("delete: %d", st)
	}
	resp, err = admin.Get(base + pic["fileUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("file after delete: %d", resp.StatusCode)
	}
	if n := len(list()); n != 2 {
		t.Fatalf("%d media after delete", n)
	}
}
