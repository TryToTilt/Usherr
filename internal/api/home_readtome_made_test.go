package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

// waitForJob waits for a book's newest job to finish, and returns it.
func waitForJob(t *testing.T, admin *http.Client, base string, bookID int64) map[string]any {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		jobs := getJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs?bookId="+jsonInt(bookID))["jobs"].([]any)
		if j := jobs[0].(map[string]any); j["state"] == "done" || j["state"] == "failed" {
			return j
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("the job didn't finish")
	return nil
}

func TestReadToMeRemakeAndDelete(t *testing.T) {
	k := newSpeakingKokoro(t, readyForAudio(t))
	server, base, admin, b, audio := readToMeLibrary(t, k)

	postJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart"}, http.StatusOK)
	first := waitForJob(t, admin, base, b.ID)
	out, _ := first["outputPath"].(string)
	if first["state"] != "done" || out == "" {
		t.Fatalf("first: %v", first)
	}

	// The list of made audiobooks: this one, which the admin may change; where it is on disk stays on the server.
	made := getJSON[map[string]any](t, admin, base+"/api/home/readtome/made")["made"].([]any)
	if len(made) != 1 || made[0].(map[string]any)["canChange"] != true || made[0].(map[string]any)["outputPath"] != nil || made[0].(map[string]any)["sizeBytes"].(float64) < 100 {
		t.Fatalf("made: %v", made)
	}
	if info := getJSON[map[string]any](t, admin, base+"/api/home/readtome/book?bookId="+jsonInt(b.ID)); info["made"] == nil {
		t.Fatalf("the book doesn't say it was made: %v", info)
	}

	// Remake: it takes the old one's place, under the same name.
	before, _ := os.Stat(out)
	job := postJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart", "speed": 1.2, "remake": true}, http.StatusOK)
	if job["remake"] != true {
		t.Fatalf("remake job: %v", job)
	}
	second := waitForJob(t, admin, base, b.ID)
	if second["state"] != "done" || second["outputPath"] != out {
		t.Fatalf("remake: %v (first was %q)", second, out)
	}
	after, err := os.Stat(out)
	if err != nil || !after.ModTime().After(before.ModTime()) && after.Size() == before.Size() {
		t.Fatalf("the file wasn't replaced: %v", err)
	}
	if files, _ := filepath.Glob(filepath.Join(filepath.Dir(out), "*.m4b")); len(files) != 1 {
		t.Fatalf("files after the remake: %v", files)
	}
	made = getJSON[map[string]any](t, admin, base+"/api/home/readtome/made")["made"].([]any)
	if len(made) != 1 || made[0].(map[string]any)["id"] != second["id"] {
		t.Fatalf("made after the remake: %v", made)
	}

	// A book whose audiobook came from elsewhere is never remade.
	real, _ := server.BookRepo.Add(books.Book{OLKey: "OL91W", Title: "Real One", Author: "B. Writer", WantEbook: true, WantAudiobook: true})
	ebook, _ := server.BookRepo.Get(b.ID)
	_ = server.BookRepo.SetState(real.ID, books.Ebook, books.StatusDownloaded, "epub", ebook.EbookPath)
	_ = server.BookRepo.SetState(real.ID, books.Audiobook, books.StatusDownloaded, "m4b", audio)
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/jobs", map[string]any{"bookId": real.ID, "engine": "kokoro", "voice": "af_heart", "remake": true}); code != http.StatusConflict {
		t.Fatalf("remaking a real audiobook answered %d", code)
	}

	// Delete: the file and the library's note of it go; the ebook stays.
	id := jsonInt(int64(second["id"].(float64)))
	if code, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/readtome/made/"+id, nil); code != http.StatusOK {
		t.Fatalf("delete answered %d", code)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("the file is still there: %v", err)
	}
	got, _ := server.BookRepo.Get(b.ID)
	if got.AudioStatus == books.StatusDownloaded || got.WantAudiobook || got.EbookStatus != books.StatusDownloaded {
		t.Fatalf("book after delete: %+v", got)
	}
	if made := getJSON[map[string]any](t, admin, base+"/api/home/readtome/made")["made"].([]any); len(made) != 0 {
		t.Fatalf("made after delete: %v", made)
	}
	if code, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/readtome/made/"+id, nil); code != http.StatusNotFound {
		t.Fatalf("a second delete answered %d", code)
	}
}

func TestReadToMeChaptersAndPronunciation(t *testing.T) {
	k := newSpeakingKokoro(t, readyForAudio(t))
	_, base, admin, b, _ := readToMeLibrary(t, k)

	// A word for every book, and one for this book that wins over it.
	putJSON(t, admin, base+"/api/home/readtome/say", map[string]any{"bookId": 0, "words": []map[string]string{{"word": "Later", "say": "Everywhere"}, {"word": " ", "say": "dropped"}}}, http.StatusOK)
	putJSON(t, admin, base+"/api/home/readtome/say", map[string]any{"bookId": b.ID, "words": []map[string]string{{"word": "later", "say": "Afterwards"}}}, http.StatusOK)
	say := getJSON[map[string]any](t, admin, base+"/api/home/readtome/say?bookId="+jsonInt(b.ID))
	if len(say["all"].([]any)) != 1 || len(say["book"].([]any)) != 1 {
		t.Fatalf("say: %v", say)
	}

	// The book's chapters, for picking.
	info := getJSON[map[string]any](t, admin, base+"/api/home/readtome/book?bookId="+jsonInt(b.ID))
	if list, _ := info["chapterList"].([]any); len(list) != 2 || list[1].(map[string]any)["title"] != "Chapter Two" {
		t.Fatalf("chapters: %v", info["chapterList"])
	}

	// Only the second chapter, without the title and the chapter's name.
	job := postJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart", "chapters": []int{1}, "noIntro": true, "noTitles": true}, http.StatusOK)
	if opts, _ := job["options"].(map[string]any); opts["noIntro"] != true {
		t.Fatalf("job options: %v", job)
	}
	if done := waitForJob(t, admin, base, b.ID); done["state"] != "done" || done["chapters"] != float64(1) {
		t.Fatalf("job: %v", done)
	}
	k.mu.Lock()
	all := strings.Join(k.texts, " | ")
	k.mu.Unlock()
	if strings.Contains(all, "The Test Book") || strings.Contains(all, "First") || strings.Contains(all, "Chapter Two") || strings.Contains(all, "Later") || !strings.Contains(all, "Afterwards word number") {
		t.Fatalf("spoken: %.400s", all)
	}
}

func TestReadToMePresetsAndMixes(t *testing.T) {
	_, base, admin := loginNewServer(t)
	kokoro := newFakeKokoro(t)
	putJSON(t, admin, base+"/api/home/readtome/settings", map[string]any{"kokoroUrl": kokoro.URL}, http.StatusOK)

	// A mix is a voice: its sample is made like any other.
	resp, err := http.DefaultClient.Do(mustReq(t, admin, http.MethodGet, base+"/api/home/readtome/sample?engine=kokoro&voice="+url.QueryEscape("af_heart(7)+bm_george(3)"), nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a mix's sample answered %d", resp.StatusCode)
	}

	preset := map[string]any{"name": "  Warm   narrator ", "engine": "kokoro", "voice": "af_heart(7)+bm_george(3)", "speed": 1.1, "options": map[string]any{"noIntro": true, "paraMs": 900, "chapters": []int{1}}, "default": true}
	list := postJSON[map[string]any](t, admin, base+"/api/home/readtome/presets", preset, http.StatusOK)["presets"].([]any)
	p := list[0].(map[string]any)
	opts := p["options"].(map[string]any)
	if len(list) != 1 || p["name"] != "Warm narrator" || p["default"] != true || opts["paraMs"] != float64(900) || opts["chapters"] != nil {
		t.Fatalf("presets: %v", list)
	}
	// The same name again is the same preset; another one can become the default.
	preset["speed"] = 1.2
	postJSON[map[string]any](t, admin, base+"/api/home/readtome/presets", preset, http.StatusOK)
	list = postJSON[map[string]any](t, admin, base+"/api/home/readtome/presets", map[string]any{"name": "Thriller", "engine": "kokoro", "voice": "bm_george", "default": true}, http.StatusOK)["presets"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["name"] != "Thriller" || list[0].(map[string]any)["default"] != true || list[1].(map[string]any)["default"] != false || list[1].(map[string]any)["speed"] != 1.2 {
		t.Fatalf("after a second: %v", list)
	}
	// Refused: no name, a voice that isn't there, a mix with one that isn't.
	for name, body := range map[string]map[string]any{
		"no name":   {"name": " ", "engine": "kokoro", "voice": "af_heart"},
		"no voice":  {"name": "X", "engine": "kokoro", "voice": "zz_nobody"},
		"bad mix":   {"name": "X", "engine": "kokoro", "voice": "af_heart+zz_nobody"},
		"bad speed": {"name": "X", "engine": "kokoro", "voice": "af_heart", "speed": 3},
	} {
		if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/presets", body); code != http.StatusBadRequest {
			t.Errorf("%s answered %d", name, code)
		}
	}
	id := jsonInt(int64(list[0].(map[string]any)["id"].(float64)))
	list = putJSON(t, admin, base+"/api/home/readtome/presets/"+id, map[string]any{"default": false}, http.StatusOK)["presets"].([]any)
	if list[0].(map[string]any)["default"] != false {
		t.Fatalf("not the default any more: %v", list)
	}
	if code, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/readtome/presets/"+id, nil); code != http.StatusOK {
		t.Fatalf("delete answered %d", code)
	}
	if list := getJSON[map[string]any](t, admin, base+"/api/home/readtome/presets")["presets"].([]any); len(list) != 1 {
		t.Fatalf("after delete: %v", list)
	}
}

func TestReadToMeCastVoices(t *testing.T) {
	k := newSpeakingKokoro(t, readyForAudio(t))
	_, base, admin, b, _ := readToMeLibrary(t, k)

	// The book's cast: Tom (tagged twice) is a character; Mary, once, isn't yet.
	cast := getJSON[map[string]any](t, admin, base+"/api/home/readtome/cast?bookId="+jsonInt(b.ID))
	if cast["quotes"] != float64(3) || !strings.Contains(jsonText(cast["people"]), `"lines":2,"name":"Tom"`) || strings.Contains(jsonText(cast["people"]), "Mary") {
		t.Fatalf("cast: %s", jsonText(cast))
	}
	// Who says the second line, set by hand; and checked.
	putJSON(t, admin, base+"/api/home/readtome/cast/lines", map[string]any{"bookId": b.ID, "chapter": 1, "index": 1, "speaker": "Mary"}, http.StatusOK)
	lines := getJSON[map[string]any](t, admin, base+"/api/home/readtome/cast/lines?bookId="+jsonInt(b.ID)+"&chapter=1")["lines"].([]any)
	if l := lines[1].(map[string]any); l["speaker"] != "Mary" || l["how"] != "you" {
		t.Fatalf("lines: %v", lines)
	}
	// A voice that isn't there, and the AI when it isn't set up, are refused.
	bad := map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart", "cast": map[string]any{"dialogue": map[string]any{"engine": "kokoro", "voice": "zz_nobody"}}}
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/jobs", bad); code != http.StatusBadRequest {
		t.Fatalf("a cast with a voice that isn't there answered %d", code)
	}
	bad["cast"] = map[string]any{"dialogue": map[string]any{"engine": "kokoro", "voice": "bm_george"}, "ai": true}
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/jobs", bad); code != http.StatusBadRequest {
		t.Fatalf("a cast asking the AI that isn't set up answered %d", code)
	}

	// Tom in George's voice, everyone else in a mix; the narration stays Heart.
	postJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart", "chapters": []int{1}, "noIntro": true, "noTitles": true,
		"cast": map[string]any{"dialogue": map[string]any{"engine": "kokoro", "voice": "af_heart(7)+bm_george(3)"}, "people": map[string]any{"Tom": map[string]any{"engine": "kokoro", "voice": "bm_george"}}}}, http.StatusOK)
	if done := waitForJob(t, admin, base, b.ID); done["state"] != "done" {
		t.Fatalf("job: %v", done)
	}
	k.mu.Lock()
	said := map[string]string{}
	for i, text := range k.texts {
		said[text] = k.voices[i]
	}
	k.mu.Unlock()
	for text, voice := range map[string]string{"Run now,": "bm_george", "said Tom.": "af_heart", "Where?": "af_heart(7)+bm_george(3)", "Mary asked.": "af_heart", "Away,": "bm_george"} {
		if said[text] != voice {
			t.Errorf("%q was read by %q, want %q (all: %v)", text, said[text], voice, said)
		}
	}
}

func TestReadToMeCastAI(t *testing.T) {
	k := newSpeakingKokoro(t, readyForAudio(t))
	server, base, admin, b, _ := readToMeLibrary(t, k)
	var asked atomic.Int32
	fakeAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `[Q1] “Where?”`) || !strings.Contains(string(body), "Known characters: Tom") {
			t.Errorf("the AI was asked: %s", body)
		}
		// It gets line 0 wrong (the text's tag wins) and line 1 right.
		writeTestJSON(w, map[string]any{"message": map[string]any{"content": `{"lines": {"0": "Wrong", "1": "mary", "2": "?"}, "people": {"Mary": "f", "Tom": "m"}}`}})
	}))
	defer fakeAI.Close()
	for key, v := range map[string]string{"home.ai.provider": "ollama", "home.ai.url": fakeAI.URL, "home.ai.model": "test"} {
		if err := server.Settings.Set(key, v, false); err != nil {
			t.Fatal(err)
		}
	}
	postJSON[map[string]any](t, admin, base+"/api/home/readtome/cast/ai", map[string]any{"bookId": b.ID}, http.StatusAccepted)
	var cast map[string]any
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		cast = getJSON[map[string]any](t, admin, base+"/api/home/readtome/cast?bookId="+jsonInt(b.ID))
		if cast["aiRunning"] == "" && cast["chapters"].([]any)[1].(map[string]any)["ai"] == true {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	people := jsonText(cast["people"])
	if cast["aiError"] != "" || cast["known"] != float64(3) || !strings.Contains(people, `"gender":"m","lines":2,"name":"Tom"`) || !strings.Contains(people, `"gender":"f","lines":1,"name":"Mary"`) {
		t.Fatalf("cast after the AI: %s", jsonText(cast))
	}
	// Done once: asking again doesn't ask the AI again.
	n := asked.Load()
	postJSON[map[string]any](t, admin, base+"/api/home/readtome/cast/ai", map[string]any{"bookId": b.ID}, http.StatusAccepted)
	time.Sleep(500 * time.Millisecond)
	if asked.Load() != n {
		t.Fatalf("the AI was asked again about a chapter it had done")
	}
}
