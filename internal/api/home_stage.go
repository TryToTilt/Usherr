package api

// Home fork: what a download is busy with right now (downloading, waiting
// for the repair hours, checking, repairing, unpacking, copying into the
// library) and when that step should be done, so every card can show a
// step and a countdown instead of sitting at "downloading 100%" while PAR2
// rebuilds a release.
//
// The estimate is the step's own pace so far: the fraction done since the
// step began, against the time that took. It needs a few seconds and a
// little progress before it says anything.

import (
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/organizer"
)

type queueStageInfo struct {
	text  string
	since time.Time
	until time.Time // a known end (waiting for the repair hours)
	f0    float64   // fraction done when the step began (-1: not seen yet)
	f     float64   // fraction done now (-1: unknown)
}

var (
	stageMu     sync.Mutex
	queueStages = map[int64]*queueStageInfo{}
)

// setQueueStage starts a new step for a download.
func setQueueStage(id int64, stage string) { setQueueStageUntil(id, stage, time.Time{}) }

// setQueueStageUntil starts a step whose end is known.
func setQueueStageUntil(id int64, stage string, until time.Time) {
	stageMu.Lock()
	defer stageMu.Unlock()
	queueStages[id] = &queueStageInfo{text: stage, since: time.Now(), until: until, f0: -1, f: -1}
}

func clearQueueStage(id int64) {
	stageMu.Lock()
	defer stageMu.Unlock()
	delete(queueStages, id)
}

// setStageProgress records how far the current step is (0..1). A new step
// name starts a new step.
func setStageProgress(id int64, phase string, f float64) {
	stageMu.Lock()
	defer stageMu.Unlock()
	st := queueStages[id]
	if st == nil && phase == "" {
		return
	}
	if st == nil || (phase != "" && phase != st.text) {
		st = &queueStageInfo{text: phase, since: time.Now(), f0: -1, f: -1}
		queueStages[id] = st
	}
	if st.f0 < 0 {
		st.f0 = f
	}
	st.f = f
}

// queueStage returns the current step, when it began and when it should
// end (RFC 3339; ends is "" while there is no estimate). pct is the
// download's own progress, used while the step is downloading.
func queueStage(id int64, pct float64) (stage, since, ends string) {
	stageMu.Lock()
	defer stageMu.Unlock()
	st := queueStages[id]
	if st == nil {
		return "", "", ""
	}
	if st.text == "Downloading" {
		f := pct / 100
		if st.f0 < 0 {
			st.f0 = f
		}
		st.f = f
	}
	now := time.Now()
	var end time.Time
	switch {
	case !st.until.IsZero():
		end = st.until
	case st.f >= 0 && st.f0 >= 0:
		elapsed := now.Sub(st.since)
		gained := st.f - st.f0
		if elapsed >= 5*time.Second && gained >= 0.005 && st.f < 1 {
			end = now.Add(time.Duration(float64(elapsed) * (1 - st.f) / gained))
		}
	}
	since = st.since.UTC().Format(time.RFC3339)
	if !end.IsZero() {
		ends = end.UTC().Format(time.RFC3339)
	}
	return st.text, since, ends
}

var stageQueueDir = regexp.MustCompile(`queue-(\d+)(?:/|\\|$)`)

func init() {
	// File steps (PAR2, unpacking, copying) report by path; the download is
	// the queue-N folder the path is in.
	organizer.ProgressHook = func(path, phase string, done, total int64) {
		m := stageQueueDir.FindStringSubmatch(path)
		if m == nil {
			return
		}
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return
		}
		setStageProgress(id, phase, float64(done)/float64(total))
	}
}
