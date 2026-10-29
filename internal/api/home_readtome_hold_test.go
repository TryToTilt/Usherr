package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
)

// jobState is the newest job's state and note.
func jobState(t *testing.T, admin *http.Client, base string) (string, string) {
	t.Helper()
	jobs := getJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs")["jobs"].([]any)
	if len(jobs) == 0 {
		return "", ""
	}
	j := jobs[0].(map[string]any)
	note, _ := j["note"].(string)
	return j["state"].(string), note
}

func waitForState(t *testing.T, admin *http.Client, base string, want ...string) (string, string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		st, note := jobState(t, admin, base)
		for _, w := range want {
			if st == w {
				return st, note
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	st, note := jobState(t, admin, base)
	t.Fatalf("the job is %q (%s), not one of %v", st, note, want)
	return "", ""
}

func TestReadToMeWaitsForNightAndForPlayback(t *testing.T) {
	tone := readyForAudio(t)
	defer api.QuickReadToMeHold()()
	k := newSpeakingKokoro(t, tone)
	_, base, admin, b, _ := readToMeLibrary(t, k)

	// Night only, with a window that is never now: 12 hours ahead, for a minute.
	now := time.Now()
	from := now.Add(12 * time.Hour).Format("15:04")
	to := now.Add(12*time.Hour + time.Minute).Format("15:04")
	putJSON(t, admin, base+"/api/home/readtome/settings", map[string]any{"nightOnly": true, "nightFrom": from, "nightTo": to}, http.StatusOK)
	postJSON[map[string]any](t, admin, base+"/api/home/readtome/jobs", map[string]any{"bookId": b.ID, "engine": "kokoro", "voice": "af_heart"}, http.StatusOK)
	state, note := waitForState(t, admin, base, "waiting")
	if state != "waiting" || !strings.Contains(note, "night hours") {
		t.Fatalf("held for the night: %s %q", state, note)
	}
	if k.spoken.Load() != 0 {
		t.Fatalf("%d pieces were spoken while it should wait for the night", k.spoken.Load())
	}

	// Night comes (a window that holds now), but a film is playing: still held, with its own reason.
	stop := api.ReadToMeSomeonePlays(false)
	putJSON(t, admin, base+"/api/home/readtome/settings", map[string]any{"nightFrom": now.Add(-time.Hour).Format("15:04"), "nightTo": now.Add(time.Hour).Format("15:04")}, http.StatusOK)
	time.Sleep(200 * time.Millisecond)
	waitForState(t, admin, base, "waiting")
	var why string
	for i := 0; i < 100 && !strings.Contains(why, "playing"); i++ {
		_, why = jobState(t, admin, base)
		time.Sleep(30 * time.Millisecond)
	}
	if !strings.Contains(why, "something is playing") || k.spoken.Load() != 0 {
		t.Fatalf("held for playback: %q, %d spoken", why, k.spoken.Load())
	}

	// The film ends: the job goes ahead and finishes.
	stop()
	waitForState(t, admin, base, "done")
	got := getJSON[map[string]any](t, admin, base+"/api/home/readtome/book?bookId="+jsonInt(b.ID))
	if got["hasAudiobook"] != true {
		t.Fatalf("no audiobook after the wait: %v", got)
	}
}
