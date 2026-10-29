package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/books"
)

// fakeKokoro answers like Kokoro-FastAPI: a list of voices and speech (a made-up
// MP3 body) for any text, and counts what it was asked to say.
type fakeKokoro struct {
	*httptest.Server
	speeches atomic.Int32
	mu       sync.Mutex
	bodies   []string // every speech request, as text
}

func newFakeKokoro(t *testing.T) *fakeKokoro {
	t.Helper()
	k := &fakeKokoro{}
	k.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/voices":
			_, _ = w.Write([]byte(`{"voices":[{"id":"af_heart","name":"af_heart","overall_grade":"A"},{"id":"bm_george","name":"bm_george","overall_grade":"C"},{"id":"ff_siwis","name":"ff_siwis"}]}`))
		case "/v1/audio/speech":
			b, _ := io.ReadAll(r.Body)
			k.speeches.Add(1)
			k.mu.Lock()
			k.bodies = append(k.bodies, string(b))
			k.mu.Unlock()
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("ID3" + strings.Repeat("\x00audio", 80)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(k.Close)
	return k
}

func TestReadToMeSettingsVoicesSamplePreview(t *testing.T) {
	server, base, admin := loginNewServer(t)
	kokoro := newFakeKokoro(t)

	// Off until an address is set.
	st := getJSON[map[string]any](t, admin, base+"/api/home/readtome/settings")
	if st["kokoroUrl"] != "" || st["nightOnly"] != false || st["nightFrom"] != "23:00" || st["nightTo"] != "07:00" {
		t.Fatalf("defaults: %v", st)
	}
	if v := getJSON[map[string]any](t, admin, base+"/api/home/readtome/voices"); len(v["voices"].([]any)) != 0 {
		t.Fatalf("voices with nothing set up: %v", v)
	}

	// A bad address, a bad time.
	if code, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/readtome/settings", map[string]any{"kokoroUrl": "ftp://x"}); code != http.StatusBadRequest {
		t.Fatalf("a bad address answered %d", code)
	}
	if code, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/readtome/settings", map[string]any{"nightFrom": "25:00"}); code != http.StatusBadRequest {
		t.Fatalf("a bad time answered %d", code)
	}
	st = putJSON(t, admin, base+"/api/home/readtome/settings", map[string]any{"kokoroUrl": kokoro.URL, "nightOnly": true, "nightFrom": "22:30"}, http.StatusOK)
	if st["kokoroUrl"] != kokoro.URL || st["nightOnly"] != true || st["nightFrom"] != "22:30" {
		t.Fatalf("saved: %v", st)
	}
	engines := st["engines"].([]any)
	if k := engines[0].(map[string]any); k["name"] != "kokoro" || k["ready"] != true {
		t.Fatalf("kokoro isn't ready: %v", engines)
	}
	if c := engines[1].(map[string]any); c["on"] != false {
		t.Fatalf("chatterbox on without an address: %v", engines)
	}

	// The voices: English ones, best first, with readable names; Chatterbox left out.
	v := getJSON[map[string]any](t, admin, base+"/api/home/readtome/voices")
	voices := v["voices"].([]any)
	if len(voices) != 2 || voices[0].(map[string]any)["id"] != "af_heart" || voices[0].(map[string]any)["label"] != "Heart · American · woman" || voices[0].(map[string]any)["engine"] != "kokoro" {
		t.Fatalf("voices: %v", voices)
	}

	// A sample is made once and kept.
	sample := func(voice string) (int, string, int) {
		resp, err := http.DefaultClient.Do(mustReq(t, admin, http.MethodGet, base+"/api/home/readtome/sample?engine=kokoro&voice="+voice, nil))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), len(b)
	}
	if code, ctype, n := sample("af_heart"); code != http.StatusOK || ctype != "audio/mpeg" || n < 100 {
		t.Fatalf("sample: %d %s %d", code, ctype, n)
	}
	sample("af_heart")
	if n := kokoro.speeches.Load(); n != 1 {
		t.Fatalf("the sample was made %d times", n)
	}
	if code, _, _ := sample("../../etc/passwd"); code != http.StatusBadRequest {
		t.Fatalf("a made-up voice answered %d", code)
	}
	if code, _, _ := sample("ff_siwis"); code != http.StatusBadRequest {
		t.Fatalf("a French voice (not listed) answered %d", code)
	}

	// The preview: the first words of the book's first chapter, spoken at the speed asked for.
	epub := api.ReadToMeTestEPUB(t)
	b, err := server.BookRepo.Add(books.Book{OLKey: "OL77W", Title: "The Test Book", Author: "A. Writer", WantEbook: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.BookRepo.SetState(b.ID, books.Ebook, books.StatusDownloaded, "epub", epub); err != nil {
		t.Fatal(err)
	}
	before := kokoro.speeches.Load()
	resp, err := http.DefaultClient.Do(mustReq(t, admin, http.MethodPost, base+"/api/home/readtome/preview", map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart", "speed": 1.2}))
	if err != nil {
		t.Fatal(err)
	}
	audio, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "audio/mpeg" || len(audio) < 100 {
		t.Fatalf("preview: %d %s %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(audio))
	}
	if kokoro.speeches.Load() <= before {
		t.Fatal("the preview spoke nothing")
	}
	kokoro.mu.Lock()
	asked := append([]string(nil), kokoro.bodies[before:]...)
	kokoro.mu.Unlock()
	var all strings.Builder
	for _, body := range asked {
		var spoken struct {
			Input string  `json:"input"`
			Voice string  `json:"voice"`
			Speed float64 `json:"speed"`
		}
		_ = json.Unmarshal([]byte(body), &spoken)
		if spoken.Voice != "af_heart" || spoken.Speed != 1.2 {
			t.Fatalf("a piece was spoken as %+v", spoken)
		}
		all.WriteString(spoken.Input + " ")
	}
	// The first chapter, not the second, and not the cover or the copyright page.
	if !strings.Contains(all.String(), "First word") || strings.Contains(all.String(), "Later") || strings.Contains(all.String(), "Rights") || strings.Contains(all.String(), "cover") {
		t.Fatalf("what was spoken: %q", all.String())
	}

	// Refusals: a speed out of range, a book that isn't one, a book without an EPUB.
	for name, body := range map[string]map[string]any{
		"speed":     {"bookId": b.ID, "engine": "kokoro", "voice": "af_heart", "speed": 2.5},
		"no book":   {"bookId": 99999, "engine": "kokoro", "voice": "af_heart"},
		"no engine": {"bookId": b.ID, "engine": "chatterbox", "voice": "default"},
	} {
		if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/preview", body); code != http.StatusBadRequest {
			t.Errorf("%s answered %d", name, code)
		}
	}
	plain, _ := server.BookRepo.Add(books.Book{OLKey: "OL78W", Title: "No EPUB", WantEbook: true})
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/preview", map[string]any{"bookId": plain.ID, "engine": "kokoro", "voice": "af_heart"}); code != http.StatusBadRequest {
		t.Errorf("a book without an EPUB answered %d", code)
	}

	// The test button.
	if msg := postJSON[map[string]any](t, admin, base+"/api/home/readtome/test", map[string]any{"engine": "kokoro"}, http.StatusOK)["message"]; !strings.Contains(msg.(string), "2 voices") {
		t.Fatalf("test: %v", msg)
	}
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/test", map[string]any{"engine": "chatterbox"}); code != http.StatusBadRequest {
		t.Fatalf("testing an engine with no address answered %d", code)
	}
}

func mustReq(t *testing.T, client *http.Client, method, url string, body any) *http.Request {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// The signed-in client's cookies.
	if client.Jar != nil {
		for _, c := range client.Jar.Cookies(req.URL) {
			req.AddCookie(c)
		}
	}
	return req
}
