package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/settings"
)

// A fake Kokoro that really speaks: any text comes back as a short real MP3, so
// the job's ffmpeg steps have audio to work on. failAfter makes it answer 500
// once that many pieces were spoken (0: never); hold blocks every request until
// it is closed.
type speakingKokoro struct {
	*httptest.Server
	spoken    atomic.Int32
	failAfter atomic.Int32
	mu        sync.Mutex
	texts     []string
	voices    []string // the voice each text was spoken in
	hold      chan struct{}
}

func newSpeakingKokoro(t *testing.T, tone []byte) *speakingKokoro {
	t.Helper()
	k := &speakingKokoro{}
	k.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/voices":
			_, _ = w.Write([]byte(`{"voices":[{"id":"af_heart","name":"af_heart","overall_grade":"A"},{"id":"bm_george","name":"bm_george","overall_grade":"C"}]}`))
		case "/v1/audio/speech":
			if k.hold != nil {
				select {
				case <-k.hold:
				case <-r.Context().Done():
					return
				}
			}
			b, _ := io.ReadAll(r.Body)
			if fa := k.failAfter.Load(); fa > 0 && k.spoken.Load() >= fa {
				http.Error(w, "out of memory", http.StatusInternalServerError)
				return
			}
			k.spoken.Add(1)
			var body struct {
				Input string `json:"input"`
				Voice string `json:"voice"`
			}
			_ = json.Unmarshal(b, &body)
			k.mu.Lock()
			k.texts = append(k.texts, body.Input)
			k.voices = append(k.voices, body.Voice)
			k.mu.Unlock()
			_, _ = w.Write(tone)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(k.Close)
	return k
}

// readyForAudio skips the test without ffmpeg, and makes a second of tone as an MP3.
func readyForAudio(t *testing.T) []byte {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " isn't installed")
		}
	}
	tone, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.6", "-ar", "24000", "-ac", "1", "-f", "mp3", "pipe:1").Output()
	if err != nil || len(tone) < 200 {
		t.Fatalf("no tone: %v", err)
	}
	return tone
}

// a library with the test book as a downloaded EPUB, an audiobooks folder, and Kokoro set up.
func readToMeLibrary(t *testing.T, k *speakingKokoro) (*api.Server, string, *http.Client, books.Book, string) {
	t.Helper()
	server, base, admin := loginNewServer(t)
	audio := t.TempDir()
	if err := server.Settings.Set(settings.KeyAudiobooksPath, audio, false); err != nil {
		t.Fatal(err)
	}
	putJSON(t, admin, base+"/api/home/readtome/settings", map[string]any{"kokoroUrl": k.URL}, http.StatusOK)
	epub := api.ReadToMeTestEPUB(t)
	b, err := server.BookRepo.Add(books.Book{OLKey: "OL90W", Title: "The Test Book", Author: "A. Writer", Year: 1999, WantEbook: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.BookRepo.SetState(b.ID, books.Ebook, books.StatusDownloaded, "epub", epub); err != nil {
		t.Fatal(err)
	}
	return server, base, admin, b, audio
}

func TestReadToMeMakesAnAudiobook(t *testing.T) {
	tone := readyForAudio(t)
	k := newSpeakingKokoro(t, tone)
	server, base, admin, b, audio := readToMeLibrary(t, k)

	// What making it involves.
	info := getJSON[map[string]any](t, admin, base+"/api/home/readtome/book?bookId="+jsonInt(b.ID))
	if info["canMake"] != true || info["chapters"] != float64(2) || info["hasAudiobook"] != false {
		t.Fatalf("book info: %v", info)
	}

	// Make it: the job is queued, the runner picks it up, the audiobook is filed.
	job := postJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart", "speed": 1.1}, http.StatusOK)
	id := job["id"].(float64)
	if job["title"] != "The Test Book" {
		t.Fatalf("job: %v", job)
	}
	// A second one for the same book is refused while it's being made.
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart"}); code != http.StatusConflict {
		t.Fatalf("a second job answered %d", code)
	}
	var final map[string]any
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		jobs := getJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs?bookId="+jsonInt(b.ID))["jobs"].([]any)
		if j := jobs[0].(map[string]any); j["state"] == "done" || j["state"] == "failed" {
			final = j
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if final == nil || final["state"] != "done" {
		t.Fatalf("the job didn't finish: %v", final)
	}
	if final["id"] != id || final["chapters"] != float64(2) || final["piecesDone"] != final["piecesTotal"] || final["piecesTotal"].(float64) < 6 {
		t.Fatalf("finished job: %v", final)
	}

	// Filed with the audiobooks, as one M4B, with the chapters marked.
	out, _ := final["outputPath"].(string)
	if !strings.HasPrefix(out, audio) || filepath.Ext(out) != ".m4b" || !strings.Contains(out, "A. Writer") {
		t.Fatalf("filed as %q", out)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	chapters, err := books.ReadChapters(f, st.Size())
	if err != nil || len(chapters) != 2 || chapters[0].Title != "Chapter One" || chapters[1].Title != "Chapter Two" || chapters[1].Start < 3 {
		t.Fatalf("chapters: %+v %v", chapters, err)
	}
	probe, _ := exec.Command("ffprobe", "-v", "error", "-show_entries", "format_tags=composer,title,artist:stream=codec_name,disposition", "-of", "json", out).Output()
	for _, want := range []string{"Read by Usherr", "The Test Book", "A. Writer", `"aac"`, "mjpeg"} {
		if !strings.Contains(string(probe), want) {
			t.Errorf("the file has no %q: %s", want, probe)
		}
	}
	// The library knows it, and the work folder is gone.
	got, _ := server.BookRepo.Get(b.ID)
	if got.AudioStatus != books.StatusDownloaded || got.AudioFormat != "m4b" || got.AudioPath != filepath.Dir(out) || !strings.Contains(got.Narrators, "Usherr") || got.RuntimeMin < 0 {
		t.Fatalf("book after: %+v", got)
	}
	// What was spoken: the book's name first, each chapter's title, the text; never the copyright page.
	k.mu.Lock()
	all := strings.Join(k.texts, " | ")
	k.mu.Unlock()
	if !strings.HasPrefix(all, "The Test Book, by A. Writer.") || !strings.Contains(all, "Chapter One.") || !strings.Contains(all, "Chapter Two.") || strings.Contains(all, "Rights") {
		t.Fatalf("spoken: %.400s", all)
	}
	// It already has one now.
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart"}); code != http.StatusConflict {
		t.Fatalf("a job for a book with an audiobook answered %d", code)
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestReadToMeCarriesOnAfterAStop(t *testing.T) {
	tone := readyForAudio(t)
	defer api.QuickReadToMeRetries()()
	k := newSpeakingKokoro(t, tone)
	server, _, _, b, _ := readToMeLibrary(t, k)

	id := server.TestEnqueueReadToMe(b.ID, "kokoro", "af_heart", 1)
	k.failAfter.Store(4) // the engine falls over after four pieces
	if !server.TestRunReadToMeQueue(context.Background()) {
		t.Fatal("the job wasn't run")
	}
	got, _ := server.BookRepo.Get(b.ID)
	if got.AudioStatus == books.StatusDownloaded {
		t.Fatal("an audiobook was made from a failed job")
	}
	first := k.spoken.Load()
	if first != 4 {
		t.Fatalf("%d pieces were spoken before the stop", first)
	}

	// Back in line (as after a restart), the engine well again: only what is left is spoken.
	k.failAfter.Store(0)
	server.TestRequeueReadToMe(id)
	if !server.TestRunReadToMeQueue(context.Background()) {
		t.Fatal("the job wasn't run again")
	}
	got, _ = server.BookRepo.Get(b.ID)
	if got.AudioStatus != books.StatusDownloaded {
		t.Fatalf("the job didn't finish the second time: %+v", got)
	}
	k.mu.Lock()
	texts := append([]string(nil), k.texts...)
	k.mu.Unlock()
	seen := map[string]int{}
	for _, tx := range texts {
		seen[tx]++
	}
	for tx, n := range seen {
		if n > 1 {
			t.Errorf("%q was spoken %d times", tx, n)
		}
	}
}

func TestReadToMeCancel(t *testing.T) {
	tone := readyForAudio(t)
	k := newSpeakingKokoro(t, tone)
	k.hold = make(chan struct{})
	_, base, admin, b, _ := readToMeLibrary(t, k)
	defer close(k.hold)

	job := postJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart"}, http.StatusOK)
	id := jsonInt(int64(job["id"].(float64)))
	// Wait until it is running (blocked in the engine).
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if j := getJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs")["jobs"].([]any)[0].(map[string]any); j["state"] == "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if code, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/readtome/jobs/"+id, nil); code != http.StatusOK {
		t.Fatalf("cancel answered %d", code)
	}
	deadline = time.Now().Add(20 * time.Second)
	state := ""
	for time.Now().Before(deadline) {
		state, _ = getJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs")["jobs"].([]any)[0].(map[string]any)["state"].(string)
		if state == "cancelled" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if state != "cancelled" {
		t.Fatalf("state after cancelling: %q", state)
	}
	// A cancelled job is taken off the list by deleting it again.
	doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/readtome/jobs/"+id, nil)
	if jobs := getJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs")["jobs"].([]any); len(jobs) != 0 {
		t.Fatalf("jobs left: %v", jobs)
	}
}
