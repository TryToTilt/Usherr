package api

import (
	"context"
	"testing"
	"time"
)

// ReadToMeTestEPUB makes a small EPUB file for the Read-to-Me tests in api_test.
func ReadToMeTestEPUB(t *testing.T) string { return testEPUB(t, false) }

// QuickReadToMeRetries makes a failed piece be tried again at once.
func QuickReadToMeRetries() func() {
	old := rtvRetryStep
	rtvRetryStep = time.Millisecond
	return func() { rtvRetryStep = old }
}

// TestEnqueueReadToMe puts a job in line without waking the runner.
func (s *Server) TestEnqueueReadToMe(bookID int64, engine, voice string, speed float64) int64 {
	res, err := s.db.Exec(`INSERT INTO readtome_jobs (book_id, engine, voice, speed, created_ms) VALUES (?, ?, ?, ?, ?)`, bookID, engine, voice, speed, time.Now().UnixMilli())
	if err != nil {
		panic(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// TestRunReadToMeQueue runs the oldest job in line, to its end, and says whether there was one.
func (s *Server) TestRunReadToMeQueue(ctx context.Context) bool { return s.runNextReadToMeJob(ctx) }

// TestRequeueReadToMe puts a stopped job back in line, as a restart does.
func (s *Server) TestRequeueReadToMe(id int64) {
	_, _ = s.db.Exec(`UPDATE readtome_jobs SET state = 'queued', error = '' WHERE id = ?`, id)
}

// QuickReadToMeHold makes a held job look again every few milliseconds.
func QuickReadToMeHold() func() {
	old := rtvHoldPoll
	rtvHoldPoll = 20 * time.Millisecond
	return func() { rtvHoldPoll = old }
}

// ReadToMeSomeonePlays pretends a player in the browser is playing, until the returned function is called.
func ReadToMeSomeonePlays(paused bool) func() {
	usherrNowSet("readtome-test", &usherrPlaying{title: "A film", kind: "movie", paused: paused})
	return func() {
		usherrNow.mu.Lock()
		delete(usherrNow.m, "readtome-test")
		usherrNow.mu.Unlock()
	}
}

// QuickReadToMeIdle stops a Chatterbox Usherr started as soon as nothing needs it.
func QuickReadToMeIdle() func() {
	old := rtvIdleStop.Swap(0)
	return func() { rtvIdleStop.Store(old) }
}

// TestSetReadToMeState sets a job's state, as the runner would.
func (s *Server) TestSetReadToMeState(id int64, state string) {
	_, _ = s.db.Exec(`UPDATE readtome_jobs SET state = ? WHERE id = ?`, state, id)
}

// TestJobChapterFile is where a job keeps its n-th chapter once it is spoken.
func (s *Server) TestJobChapterFile(id int64, n int) string { return s.jobChapterFile(id, n) }

func (s *Server) TestVoiceDir() string { return s.cbVoiceDir() }
