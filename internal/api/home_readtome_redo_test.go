package api_test

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func lineList(t *testing.T, v map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range v["lines"].([]any) {
		out = append(out, l.(map[string]any))
	}
	return out
}

func TestReadToMeBadLinesRedoAndEdits(t *testing.T) {
	tone := readyForAudio(t)
	k := newSpeakingKokoro(t, tone)
	f := newFakeVoiceEngines(t, speechWAV(t, "0.5"))
	// Chatterbox reads at about the pace of a narrator, except: the first reading of the
	// long paragraph runs on (read again, then fine), and "Run now" always comes out clipped.
	f.shape = func(text string, n int) float64 {
		secs := float64(len(strings.Fields(text))) / 2.6
		switch {
		case strings.Contains(text, "Run now"):
			return 0.2
		case strings.Contains(text, "Later") && n == 1:
			return secs*4 + 3
		}
		return secs
	}
	_, base, admin, b, _ := readToMeLibrary(t, k)
	putJSON(t, admin, base+"/api/home/readtome/settings", map[string]any{"chatterboxUrl": f.cb.URL, "runnerUrl": f.runner.URL, "runnerToken": "a-token-that-is-long-enough-123"}, http.StatusOK)
	bookURL := base + "/api/home/readtome/book?bookId=" + jsonInt(b.ID)

	// The chapter's own text: the ebook says "Later", Read-to-Me reads it as put right.
	orig := getJSON[map[string]any](t, admin, base+"/api/home/readtome/text?bookId="+jsonInt(b.ID)+"&chapter=1")
	text := orig["text"].(string)
	if orig["edited"] != false || !strings.Contains(text, "Later") {
		t.Fatalf("text: %v", orig)
	}
	putJSON(t, admin, base+"/api/home/readtome/text", map[string]any{"bookId": b.ID, "chapter": 1, "text": text + "\n\nThe end, as it was put right."}, http.StatusOK)
	info := getJSON[map[string]any](t, admin, bookURL)
	if !strings.Contains(jsonText(info["chapterList"]), `"edited":true`) {
		t.Fatalf("chapters: %v", info["chapterList"])
	}

	// Made with Chatterbox, chapter two only, with a kept take.
	postJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "chatterbox", "voice": "default", "chapters": []int{1}, "noIntro": true, "tune": map[string]any{"seed": 77}}, http.StatusOK)
	if done := waitForJob(t, admin, base, b.ID); done["state"] != "done" {
		t.Fatalf("job: %v", done)
	}
	made := getJSON[map[string]any](t, admin, bookURL)["made"].(map[string]any)
	madeID := jsonInt(int64(made["id"].(float64)))
	if made["kept"] != true || made["flagged"] != float64(1) || made["keptBytes"].(float64) < 1000 {
		t.Fatalf("made: %v", made)
	}
	f.mu.Lock()
	later, run := 0, f.asked["“Run now,” said Tom."]
	for txt, n := range f.asked {
		if strings.Contains(txt, "Later") {
			later += n
		}
	}
	readEdit := f.asked["The end, as it was put right."]
	f.mu.Unlock()
	if run != 3 || later < 2 || readEdit != 1 {
		t.Fatalf("readings: run now %d, later %d, edit %d (%v)", run, later, readEdit, f.asked)
	}

	// The line to check, where it is, and how it sounds.
	lines := getJSON[map[string]any](t, admin, base+"/api/home/readtome/made/"+madeID+"/lines")
	flagged := lines["flagged"].([]any)
	if len(flagged) != 1 || !strings.Contains(jsonText(flagged[0]), "Run now") || !strings.Contains(jsonText(flagged[0]), "shorter") {
		t.Fatalf("flagged: %s", jsonText(flagged))
	}
	fl := flagged[0].(map[string]any)
	ch, li := int(fl["chapter"].(float64)), int(fl["index"].(float64))
	resp, err := admin.Get(base + "/api/home/readtome/made/" + madeID + "/line?chapter=" + strconv.Itoa(ch) + "&line=" + strconv.Itoa(li))
	if err != nil || resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("a line: %v %v", err, resp)
	}
	resp.Body.Close()
	before := lineList(t, lines)

	// Read again in other words: the line is put right, the ones after it move with it.
	postJSON[map[string]any](t, admin, base+"/api/home/readtome/made/"+madeID+"/redo", map[string]any{"chapter": ch, "line": li, "text": "Run, now, as fast as you can, said Tom."}, http.StatusOK)
	if done := waitForJob(t, admin, base, b.ID); done["state"] != "done" {
		t.Fatalf("redo: %v", done)
	}
	after := lineList(t, getJSON[map[string]any](t, admin, base+"/api/home/readtome/made/"+madeID+"/lines?chapter="+strconv.Itoa(ch)))
	if len(after) != len(before) || !strings.Contains(after[li]["text"].(string), "fast as you can") || after[li]["flag"] != nil {
		t.Fatalf("after the redo: %v", after[li])
	}
	shift := after[li]["endMs"].(float64) - before[li]["endMs"].(float64)
	if shift < 1000 || after[len(after)-1]["endMs"].(float64)-before[len(before)-1]["endMs"].(float64) != shift {
		t.Fatalf("the lines after didn't move with it: shift %v", shift)
	}
	made = getJSON[map[string]any](t, admin, bookURL)["made"].(map[string]any)
	if jsonInt(int64(made["id"].(float64))) != madeID || made["flagged"] != nil {
		t.Fatalf("made after the redo: %v", made)
	}
	// The audiobook itself is as long as its chapter now.
	jobs := getJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs?bookId="+jsonInt(b.ID))["jobs"].([]any)
	if len(jobs) != 1 {
		t.Fatalf("the redo stayed in the list: %s", jsonText(jobs))
	}
	out := jobs[0].(map[string]any)["outputPath"].(string)
	dur, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", out).Output()
	secs, _ := strconv.ParseFloat(strings.TrimSpace(string(dur)), 64)
	if want := after[len(after)-1]["endMs"].(float64) / 1000; err != nil || secs < want || secs > want+4 {
		t.Fatalf("the audiobook is %v s, its lines end at %v s (%v)", secs, want, err)
	}

	// The whole chapter again.
	postJSON[map[string]any](t, admin, base+"/api/home/readtome/made/"+madeID+"/redo", map[string]any{"chapter": 0, "line": -1}, http.StatusOK)
	if done := waitForJob(t, admin, base, b.ID); done["state"] != "done" {
		t.Fatalf("chapter redo: %v", done)
	}
	if again := lineList(t, getJSON[map[string]any](t, admin, base+"/api/home/readtome/made/"+madeID+"/lines")); len(again) != len(before) || !strings.Contains(again[li]["text"].(string), "Run now") {
		t.Fatalf("the chapter read again: %v", again)
	}

	// Back to the ebook's text; the makings let go of: no more redos.
	if code, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/readtome/text?bookId="+jsonInt(b.ID)+"&chapter=1", nil); code != http.StatusOK {
		t.Fatalf("text back answered %d", code)
	}
	if strings.Contains(jsonText(getJSON[map[string]any](t, admin, bookURL)["chapterList"]), "edited") {
		t.Fatal("the edit stayed")
	}
	if code, _ := doJSONStatus(t, admin, http.MethodDelete, base+"/api/home/readtome/made/"+madeID+"/makings", nil); code != http.StatusNoContent {
		t.Fatalf("freeing answered %d", code)
	}
	if code, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/readtome/made/"+madeID+"/redo", map[string]any{"chapter": 0, "line": 0}); code != http.StatusConflict {
		t.Fatalf("a redo without makings answered %d", code)
	}
}

func TestReadToMeListenWhileItIsMade(t *testing.T) {
	k := newSpeakingKokoro(t, readyForAudio(t))
	server, base, admin, b, _ := readToMeLibrary(t, k)
	id := server.TestEnqueueReadToMe(b.ID, "kokoro", "af_heart", 1)
	server.TestSetReadToMeState(id, "running")
	file := server.TestJobChapterFile(id, 1)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=duration=1", "-ar", "24000", "-ac", "1", "-c:a", "flac", file).Run(); err != nil {
		t.Fatal(err)
	}
	jobs := getJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs?bookId="+jsonInt(b.ID))["jobs"].([]any)
	if jobs[0].(map[string]any)["readyChapters"] != float64(1) {
		t.Fatalf("jobs: %s", jsonText(jobs))
	}
	early := base + "/api/home/readtome/jobs/" + jsonInt(id) + "/chapters/"
	resp, err := admin.Get(early + "1")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "audio/mpeg" || len(data) < 1000 {
		t.Fatalf("chapter 1: %d %s, %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(data))
	}
	if code, _ := doJSONStatus(t, admin, http.MethodGet, early+"2", nil); code != http.StatusNotFound {
		t.Fatalf("a chapter not made yet answered %d", code)
	}
	server.TestSetReadToMeState(id, "done")
	if code, _ := doJSONStatus(t, admin, http.MethodGet, early+"1", nil); code != http.StatusNotFound {
		t.Fatalf("a finished job's chapter answered %d", code)
	}
}
