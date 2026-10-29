package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// copyVoice uploads a recording as a copied voice; it returns the status and the answer.
func copyVoice(t *testing.T, c *http.Client, base, name, rights string, wav []byte) (int, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("name", name)
	_ = mw.WriteField("rights", rights)
	part, _ := mw.CreateFormFile("file", "sample.wav")
	_, _ = part.Write(wav)
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, base+"/api/home/readtome/chatterbox/voices", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func speechWAV(t *testing.T, secs string) []byte {
	t.Helper()
	wav, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=220:duration="+secs, "-ar", "44100", "-ac", "2", "-f", "wav", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	return wav
}

func previewStatus(t *testing.T, c *http.Client, base string, body map[string]any) (int, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := c.Post(base+"/api/home/readtome/preview", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, string(out)
	}
	return resp.StatusCode, ""
}

func TestReadToMeCopiedVoicesAndTuning(t *testing.T) {
	tone := readyForAudio(t)
	k := newSpeakingKokoro(t, tone)
	f := newFakeVoiceEngines(t, speechWAV(t, "0.5"))
	server, base, admin, b, _ := readToMeLibrary(t, k)
	putJSON(t, admin, base+"/api/home/readtome/settings", map[string]any{"chatterboxUrl": f.cb.URL, "runnerUrl": f.runner.URL, "runnerToken": "a-token-that-is-long-enough-123", "chatterboxModel": "standard"}, http.StatusOK)

	// Copying a voice: the rights box, a usable name, enough sound, one of a name.
	six := speechWAV(t, "6")
	for _, c := range []struct {
		name, rights string
		wav          []byte
		want         int
	}{
		{"Grandpa", "", six, http.StatusBadRequest},
		{"default", "yes", six, http.StatusBadRequest},
		{"../etc", "yes", six, http.StatusBadRequest},
		{"Short", "yes", speechWAV(t, "2"), http.StatusBadRequest},
		{"Grandpa", "yes", six, http.StatusOK},
		{"grandpa", "yes", six, http.StatusConflict},
	} {
		if code, msg := copyVoice(t, admin, base, c.name, c.rights, c.wav); code != c.want {
			t.Fatalf("copying %q (rights %q): %d %s, want %d", c.name, c.rights, code, msg, c.want)
		}
	}
	list := getJSON[map[string]any](t, admin, base+"/api/home/readtome/chatterbox/voices")
	if txt := jsonText(list); !strings.Contains(txt, `"name":"Grandpa"`) || !strings.Contains(txt, `"canChange":true`) {
		t.Fatalf("copied voices: %s", txt)
	}
	// It can be picked while Chatterbox is stopped.
	if v := jsonText(getJSON[map[string]any](t, admin, base+"/api/home/readtome/voices")); !strings.Contains(v, "Grandpa · copied voice") {
		t.Fatalf("voices: %s", v)
	}

	// A short preview with Chatterbox's dials: Chatterbox is given the voice once, and reads as asked.
	req := map[string]any{"bookId": b.ID, "engine": "chatterbox", "voice": "Grandpa", "speed": 1, "chapters": []int{1}, "words": 40,
		"tune": map[string]any{"exaggeration": 0.7, "cfg": 0.3, "temperature": 0.9, "seed": 42}}
	for i := 0; i < 2; i++ {
		if code, msg := previewStatus(t, admin, base, req); code != http.StatusOK {
			t.Fatalf("preview: %d %s", code, msg)
		}
	}
	f.mu.Lock()
	uploads, last, seed, n := strings.Join(f.uploads, ","), jsonText(f.speeches[len(f.speeches)-1]), f.seeds[len(f.seeds)-1], len(f.speeches)
	f.mu.Unlock()
	if uploads != "standard:Grandpa" {
		t.Fatalf("uploads: %s", uploads)
	}
	for _, want := range []string{`"voice":"Grandpa"`, `"exaggeration":0.7`, `"cfg_weight":0.3`, `"temperature":0.9`, `"seed":42`} {
		if !strings.Contains(last, want) {
			t.Fatalf("speech %s has no %s", last, want)
		}
	}
	if seed != "42" || n > 10 {
		t.Fatalf("seed header %q, %d pieces for two 40-word previews", seed, n)
	}

	// Turbo is given it as well when it reads with it; dials out of range are kept in range.
	req["cbModel"] = "turbo"
	req["tune"] = map[string]any{"exaggeration": 5, "cfg": -1}
	if code, msg := previewStatus(t, admin, base, req); code != http.StatusOK {
		t.Fatalf("turbo preview: %d %s", code, msg)
	}
	f.mu.Lock()
	uploads, last = strings.Join(f.uploads, ","), jsonText(f.speeches[len(f.speeches)-1])
	f.mu.Unlock()
	if uploads != "standard:Grandpa,turbo:Grandpa" || !strings.Contains(last, `"exaggeration":2`) || !strings.Contains(last, `"cfg_weight":0`) || strings.Contains(last, "seed") {
		t.Fatalf("turbo: uploads %s, speech %s", uploads, last)
	}

	// The recording can be heard; deleting it takes it from Chatterbox too, and from the lists.
	resp, err := admin.Get(base + "/api/home/readtome/chatterbox/voices/Grandpa/recording")
	if err != nil || resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "audio/wav" {
		t.Fatalf("recording: %v %v", err, resp)
	}
	resp.Body.Close()
	if code, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/readtome/chatterbox/voices/Grandpa", nil); code != http.StatusNoContent {
		t.Fatalf("delete answered %d", code)
	}
	f.mu.Lock()
	kept := f.library["Grandpa"]
	f.mu.Unlock()
	if kept {
		t.Fatalf("Chatterbox kept it: %s", f.log())
	}
	if v := jsonText(getJSON[map[string]any](t, admin, base+"/api/home/readtome/voices")); strings.Contains(v, "Grandpa") {
		t.Fatalf("a deleted voice is offered: %s", v)
	}
	if code, _ := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/readtome/chatterbox/voices/Grandpa/recording", nil); code != http.StatusNotFound {
		t.Fatalf("a deleted recording answered %d", code)
	}
	// The name can be used again.
	if code, msg := copyVoice(t, admin, base, "Grandpa", "yes", six); code != http.StatusOK {
		t.Fatalf("copying again: %d %s", code, msg)
	}

	// A WAV put in the voices folder by hand is taken in; one too short is put aside.
	dir := server.TestVoiceDir()
	if err := os.WriteFile(filepath.Join(dir, "UK woman - Ruth.wav"), speechWAV(t, "8"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "Tiny.wav"), speechWAV(t, "1"), 0o644)
	// They are taken in in the background; the list shows them once done.
	voicesUntil := func(ok func(string) bool) string {
		var txt string
		for range 100 {
			txt = jsonText(getJSON[map[string]any](t, admin, base+"/api/home/readtome/chatterbox/voices"))
			if ok(txt) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		return txt
	}
	if txt := voicesUntil(func(s string) bool { return strings.Contains(s, "UK woman - Ruth") }); !strings.Contains(txt, `"name":"UK woman - Ruth"`) || strings.Contains(txt, "Tiny") {
		t.Fatalf("voices from the folder: %s", txt)
	}
	if _, err := os.Stat(filepath.Join(dir, "Tiny.wav.unusable")); err != nil {
		t.Fatalf("the short file wasn't put aside: %v", err)
	}
	// Taking the file out deletes the voice; an uploaded voice stays.
	if err := os.Remove(filepath.Join(dir, "UK woman - Ruth.wav")); err != nil {
		t.Fatal(err)
	}
	if txt := voicesUntil(func(s string) bool { return !strings.Contains(s, "Ruth") }); strings.Contains(txt, "Ruth") || !strings.Contains(txt, `"name":"Grandpa"`) {
		t.Fatalf("after taking the file out: %s", txt)
	}
}
