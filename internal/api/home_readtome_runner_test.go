package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

// fakeVoiceEngines is a runner and the Chatterbox address it controls: the
// model answering is whichever container the runner last started.
type fakeVoiceEngines struct {
	mu      sync.Mutex
	running map[string]bool
	calls   []string
	runner  *httptest.Server
	cb      *httptest.Server
	// Chatterbox's voice library (shared by both models), what it was given and asked.
	library  map[string]bool
	uploads  []string // "model:name"
	speeches []map[string]any
	seeds    []string // the X-Usherr-Seed header of each speech
	// shape, when set, is how long (seconds) the n-th reading of a text is; else every reading is the wav given.
	shape func(text string, n int) float64
	asked map[string]int
}

func newFakeVoiceEngines(t *testing.T, wav []byte) *fakeVoiceEngines {
	f := &fakeVoiceEngines{running: map[string]bool{"kokoro": true}, library: map[string]bool{}}
	state := func() map[string]string {
		out := map[string]string{}
		for _, n := range []string{"kokoro", "chatterbox", "chatterbox-turbo"} {
			out[n] = map[bool]string{true: "running", false: "stopped"}[f.running[n]]
		}
		return out
	}
	f.runner = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer a-token-that-is-long-enough-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasPrefix(r.URL.Path, "/start/"):
			name := strings.TrimPrefix(r.URL.Path, "/start/")
			if name == "chatterbox" || name == "chatterbox-turbo" {
				f.running["chatterbox"], f.running["chatterbox-turbo"] = false, false
			}
			f.running[name] = true
		case strings.HasPrefix(r.URL.Path, "/stop/"):
			f.running[strings.TrimPrefix(r.URL.Path, "/stop/")] = false
		}
		st := state()
		f.mu.Unlock()
		writeTestJSON(w, map[string]any{"engines": st})
	}))
	f.cb = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		std, turbo := f.running["chatterbox"], f.running["chatterbox-turbo"]
		f.mu.Unlock()
		if !std && !turbo {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		switch r.URL.Path {
		case "/health":
			if turbo {
				writeTestJSON(w, map[string]any{"status": "healthy", "model_loaded": true, "model": "turbo"})
			} else {
				writeTestJSON(w, map[string]any{"status": "healthy", "model_loaded": true})
			}
		case "/voices":
			f.mu.Lock()
			defer f.mu.Unlock()
			if r.Method == http.MethodPost {
				_ = r.ParseMultipartForm(1 << 20)
				name := r.FormValue("voice_name")
				if _, _, err := r.FormFile("voice_file"); err != nil || name == "" {
					http.Error(w, "no file", http.StatusBadRequest)
					return
				}
				if f.library[name] {
					http.Error(w, "exists", http.StatusConflict)
					return
				}
				f.library[name] = true
				f.uploads = append(f.uploads, map[bool]string{true: "turbo", false: "standard"}[turbo]+":"+name)
				w.WriteHeader(http.StatusCreated)
				return
			}
			var vs []any
			for n := range f.library {
				vs = append(vs, map[string]any{"name": n})
			}
			writeTestJSON(w, map[string]any{"voices": vs})
		case "/v1/audio/speech":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.speeches = append(f.speeches, body)
			f.seeds = append(f.seeds, r.Header.Get("X-Usherr-Seed"))
			text, _ := body["input"].(string)
			if f.asked == nil {
				f.asked = map[string]int{}
			}
			f.asked[text]++
			n, shape := f.asked[text], f.shape
			f.mu.Unlock()
			out := wav
			if shape != nil {
				secs := fmt.Sprintf("%.2f", shape(text, n))
				var err error
				if out, err = exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=300:duration="+secs, "-ar", "24000", "-ac", "1", "-f", "wav", "pipe:1").Output(); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write(out)
		default:
			if name, ok := strings.CutPrefix(r.URL.Path, "/voices/"); ok && r.Method == http.MethodDelete {
				f.mu.Lock()
				delete(f.library, name)
				f.calls = append(f.calls, "DELETE voice "+name)
				f.mu.Unlock()
			}
		}
	}))
	t.Cleanup(func() { f.runner.Close(); f.cb.Close() })
	return f
}

func (f *fakeVoiceEngines) log() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ", ")
}

func TestReadToMeRunnerStartsAndStopsChatterbox(t *testing.T) {
	tone := readyForAudio(t)
	wav, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=330:duration=0.5", "-ar", "24000", "-ac", "1", "-f", "wav", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	defer api.QuickReadToMeIdle()()
	k := newSpeakingKokoro(t, tone)
	f := newFakeVoiceEngines(t, wav)
	_, base, admin, b, _ := readToMeLibrary(t, k)

	// Without a runner, a stopped Chatterbox has no voices.
	putJSON(t, admin, base+"/api/home/readtome/settings", map[string]any{"chatterboxUrl": f.cb.URL}, http.StatusOK)
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "chatterbox", "voice": "default"}); code != http.StatusBadRequest {
		t.Fatalf("a job for a stopped Chatterbox without a runner answered %d", code)
	}

	// With the runner: a wrong token is said so; the right one, and Turbo as the household's model.
	st := putJSON(t, admin, base+"/api/home/readtome/settings", map[string]any{"runnerUrl": f.runner.URL, "runnerToken": "wrong", "chatterboxModel": "turbo"}, http.StatusOK)
	if st["runnerTokenSet"] != true || st["chatterboxModel"] != "turbo" || st["runnerToken"] != nil {
		t.Fatalf("settings: %v", st)
	}
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/test", map[string]any{"engine": "runner"}); code != http.StatusBadGateway {
		t.Fatalf("a wrong token tested %d", code)
	}
	putJSON(t, admin, base+"/api/home/readtome/settings", map[string]any{"runnerToken": "a-token-that-is-long-enough-123"}, http.StatusOK)
	if msg := postJSON[map[string]any](t, admin, base+"/api/home/readtome/test", map[string]any{"engine": "runner"}, http.StatusOK)["message"]; !strings.Contains(msg.(string), "chatterbox stopped") {
		t.Fatalf("runner test: %v", msg)
	}

	// Chatterbox's own voice is offered while it's stopped: it is started when needed.
	v := getJSON[map[string]any](t, admin, base+"/api/home/readtome/voices")
	if !strings.Contains(jsonText(v), `"onDemand":true`) || !strings.Contains(jsonText(v), `"engine":"chatterbox"`) {
		t.Fatalf("voices: %s", jsonText(v))
	}

	// A job starts Turbo, makes the book, and Chatterbox is stopped again afterwards.
	postJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "chatterbox", "voice": "default", "chapters": []int{1}, "noIntro": true}, http.StatusOK)
	if done := waitForJob(t, admin, base, b.ID); done["state"] != "done" {
		t.Fatalf("job: %v", done)
	}
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(f.log(), "POST /stop/chatterbox-turbo") && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if log := f.log(); !strings.Contains(log, "POST /start/chatterbox-turbo") || !strings.Contains(log, "POST /stop/chatterbox-turbo") || strings.Contains(log, "/start/chatterbox,") {
		t.Fatalf("runner calls: %s", log)
	}

	// By hand: start the standard model, then stop it; other containers are refused.
	eng := postJSON[map[string]any](t, admin, base+"/api/home/readtome/engines/chatterbox/start", nil, http.StatusOK)
	if eng["model"] != "standard" || eng["states"].(map[string]any)["chatterbox"] != "running" {
		t.Fatalf("engines after start: %v", eng)
	}
	postJSON[map[string]any](t, admin, base+"/api/home/readtome/engines/chatterbox/stop", nil, http.StatusOK)
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/engines/mediarium/start", nil); code != http.StatusNotFound {
		t.Fatalf("another container answered %d", code)
	}
}

func writeTestJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
