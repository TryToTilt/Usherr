package api

// Home fork: schedules. Named schedules ("Overnight", "Weeknight TV time") are
// made once, from day/hour/minute ranges, and each heavy task picks one with
// a rule: run only during it, or never during it. Downloads can also be
// slowed or given fewer connections instead of pausing, and a pause can take
// effect at once or let running downloads finish. "Quiet for…" holds every
// heavy task for a while on the spot.
//
// Searches, downloads and refreshes started by hand are never held back.
// Scene tasks are set from the Scenes page only; the shared Settings page
// never sees them.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/download"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

const (
	keyHomeSchedules  = "home.schedules"       // JSON homeSchedules
	keySchedulePaused = "home.schedule_paused" // JSON []int64: downloads the schedule paused
)

// Tasks a schedule can be given to.
const (
	taskSearch       = "search.auto"         // automatic searches for movies, TV, music and books
	taskDownloads    = "downloads"           // the download line
	taskPostprocess  = "postprocess"         // PAR2 repair and unpacking
	taskMediaRefresh = "mediaserver.refresh" // asking Jellyfin/Plex/Emby to rescan
	taskSceneSearch  = "scenes.search"       // automatic and batch scene searches
	taskSceneFollow  = "scenes.follow"       // StashDB checks for followed performers and studios
)

var (
	sharedTasks = []string{taskSearch, taskDownloads, taskPostprocess, taskMediaRefresh}
	sceneTasks  = []string{taskSceneSearch, taskSceneFollow}
)

// What a download rule does while downloads are held.
const (
	dlPause       = "pause"
	dlLimit       = "limit"
	dlConnections = "connections"
)

type schedRange struct {
	Days  []int  `json:"days"`  // 0 = Sunday … 6 = Saturday: days the range starts on
	Start string `json:"start"` // "HH:MM"
	End   string `json:"end"`   // "HH:MM"; earlier than Start runs past midnight; equal = the whole day
}

type schedule struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Ranges []schedRange `json:"ranges"`
}

type taskRule struct {
	Schedule    string `json:"schedule"`              // schedule id; "" = any time
	Mode        string `json:"mode"`                  // "only" (run only during) or "never" (never during)
	Action      string `json:"action,omitempty"`      // downloads: pause | limit | connections
	When        string `json:"when,omitempty"`        // downloads, pause: "now" or "finish"
	LimitMB     int    `json:"limitMB,omitempty"`     // downloads, limit: MB/s
	Connections int    `json:"connections,omitempty"` // downloads, connections: per Usenet server
}

type homeSchedules struct {
	Schedules  []schedule          `json:"schedules"`
	Tasks      map[string]taskRule `json:"tasks"`
	QuietUntil string              `json:"quietUntil,omitempty"` // RFC 3339
	QuietWhen  string              `json:"quietWhen,omitempty"`  // "now" or "finish": what Quiet does to running downloads
}

var schedulesMu sync.Mutex

func (s *Server) loadSchedules() homeSchedules {
	var h homeSchedules
	if v, _ := s.Settings.Get(keyHomeSchedules); v != "" {
		_ = json.Unmarshal([]byte(v), &h)
	}
	if h.Tasks == nil {
		h.Tasks = map[string]taskRule{}
	}
	return h
}

func (s *Server) saveSchedules(h homeSchedules) error {
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return s.Settings.Set(keyHomeSchedules, string(b), false)
}

func (h homeSchedules) find(id string) (schedule, bool) {
	for _, sc := range h.Schedules {
		if sc.ID == id {
			return sc, true
		}
	}
	return schedule{}, false
}

// contains reports whether t falls inside one of the schedule's ranges.
func (sc schedule) contains(t time.Time) bool {
	now := t.Hour()*60 + t.Minute()
	today := int(t.Weekday())
	yesterday := (today + 6) % 7
	for _, r := range sc.Ranges {
		start, ok1 := clockMinutes(r.Start)
		end, ok2 := clockMinutes(r.End)
		if !ok1 || !ok2 {
			continue
		}
		switch {
		case start == end:
			if slices.Contains(r.Days, today) {
				return true
			}
		case start < end:
			if slices.Contains(r.Days, today) && now >= start && now < end {
				return true
			}
		default: // runs past midnight
			if (slices.Contains(r.Days, today) && now >= start) || (slices.Contains(r.Days, yesterday) && now < end) {
				return true
			}
		}
	}
	return false
}

func (h homeSchedules) quietAt(t time.Time) bool {
	if h.QuietUntil == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, h.QuietUntil)
	return err == nil && t.Before(until)
}

// allowedAt reports whether task may run at t.
func (h homeSchedules) allowedAt(task string, t time.Time) bool {
	if task != taskSceneFollow && h.quietAt(t) {
		return false
	}
	rule, ok := h.Tasks[task]
	if !ok || rule.Schedule == "" {
		return true
	}
	sc, ok := h.find(rule.Schedule)
	if !ok {
		return true
	}
	in := sc.contains(t)
	if rule.Mode == "never" {
		return !in
	}
	return in
}

// nextAllowed is when task may next run after t (zero when it may run now,
// or not within a week).
func (h homeSchedules) nextAllowed(task string, t time.Time) time.Time {
	if h.allowedAt(task, t) {
		return time.Time{}
	}
	m := t.Truncate(time.Minute)
	for i := 1; i <= 8*24*60; i++ {
		m = m.Add(time.Minute)
		if h.allowedAt(task, m) {
			return m
		}
	}
	return time.Time{}
}

// taskAllowed reports whether task may run now.
func (s *Server) taskAllowed(task string) bool {
	return s.loadSchedules().allowedAt(task, time.Now())
}

// clockMinutes reads "HH:MM" (24-hour) as minutes after midnight.
func clockMinutes(v string) (int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// ---- downloads ----

// downloadsHeld reports what the schedule asks of downloads right now: the
// rule in force (Quiet counts as a pause) and whether it applies.
func (s *Server) downloadsHeld() (taskRule, bool) {
	h := s.loadSchedules()
	now := time.Now()
	if h.quietAt(now) {
		when := h.QuietWhen
		if when == "" {
			when = "finish"
		}
		return taskRule{Action: dlPause, When: when}, true
	}
	if h.allowedAt(taskDownloads, now) {
		return taskRule{}, false
	}
	rule := h.Tasks[taskDownloads]
	if rule.Action == "" {
		rule.Action = dlPause
	}
	return rule, true
}

// scheduleHoldsNewDownloads is the download line's hold: nothing new starts
// while downloads are paused by the schedule.
func (s *Server) scheduleHoldsNewDownloads() bool {
	rule, held := s.downloadsHeld()
	return held && rule.Action == dlPause
}

// scheduleSpeedMB is the schedule's speed limit right now, 0 for none.
func (s *Server) scheduleSpeedMB() int {
	if rule, held := s.downloadsHeld(); held && rule.Action == dlLimit && rule.LimitMB > 0 {
		return rule.LimitMB
	}
	return 0
}

// scheduleJob keeps downloads in step with the schedules: the speed limit,
// the connection cap, and pausing (then resuming) running downloads when a
// "pause at once" window starts (ends).
func (s *Server) scheduleJob(context.Context) {
	s.applySpeedLimit(time.Now())
	rule, held := s.downloadsHeld()
	if held && rule.Action == dlConnections && rule.Connections > 0 {
		download.SetConnectionCap(rule.Connections)
	} else {
		download.SetConnectionCap(0)
	}
	pauseNow := held && rule.Action == dlPause && rule.When == "now"
	paused := s.schedulePaused()
	switch {
	case pauseNow:
		items, err := s.QueueRepo.List()
		if err != nil {
			return
		}
		queueControlMu.Lock()
		release := s.dispatch.Hold()
		n := 0
		for _, it := range items {
			if downloadInProgress(it.Status) && s.pauseItem(it) == nil {
				paused = append(paused, it.ID)
				n++
			}
		}
		release()
		queueControlMu.Unlock()
		if n > 0 {
			slog.Info("schedules: paused running downloads", "count", n)
			s.setSchedulePaused(paused)
		}
	case !held && len(paused) > 0:
		queueControlMu.Lock()
		release := s.dispatch.Hold()
		n := 0
		for _, id := range slices.Backward(paused) {
			it, err := s.QueueRepo.Get(id)
			if err != nil || it.Status != queue.StatusPaused {
				continue
			}
			if s.resumeItem(it) == nil {
				n++
			}
		}
		release()
		queueControlMu.Unlock()
		slog.Info("schedules: resumed downloads", "count", n)
		s.setSchedulePaused(nil)
	}
}

func (s *Server) schedulePaused() []int64 {
	var ids []int64
	if v, _ := s.Settings.Get(keySchedulePaused); v != "" {
		_ = json.Unmarshal([]byte(v), &ids)
	}
	return ids
}

func (s *Server) setSchedulePaused(ids []int64) {
	v := ""
	if len(ids) > 0 {
		b, _ := json.Marshal(ids)
		v = string(b)
	}
	_ = s.Settings.Set(keySchedulePaused, v, false)
}

// ---- post-processing ----

// waitForPostprocess holds a finished download before repair and unpacking
// until the schedule allows them, noting it once in the item's activity.
func (s *Server) waitForPostprocess(ctx context.Context, queueID int64) error {
	noted := false
	for !s.taskAllowed(taskPostprocess) {
		if !noted {
			msg := "Waiting for the repair and unpack hours"
			next := s.loadSchedules().nextAllowed(taskPostprocess, time.Now())
			if !next.IsZero() {
				msg += " (" + next.Format("Mon 15:04") + ")"
			}
			s.queueEvent(queueID, "postprocess", queue.LevelInfo, msg)
			setQueueStageUntil(queueID, "Waiting for the repair and unpack hours", next)
			noted = true
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(scheduleWaitPoll):
		}
	}
	return nil
}

// scheduleWaitPoll is how often held work checks the schedule.
var scheduleWaitPoll = 30 * time.Second

// ---- API ----

type taskState struct {
	Rule      *taskRule `json:"rule,omitempty"`
	Allowed   bool      `json:"allowed"`
	NextAllow string    `json:"nextAllowed,omitempty"`
}

type schedulesView struct {
	Schedules  []schedule           `json:"schedules"`
	Tasks      map[string]taskState `json:"tasks"`
	QuietUntil string               `json:"quietUntil,omitempty"`
	QuietWhen  string               `json:"quietWhen,omitempty"`
	Now        string               `json:"now"`      // server time, RFC 3339
	TimeZone   string               `json:"timeZone"` // e.g. "CDT"
}

func (s *Server) schedulesView(tasks []string) schedulesView {
	h := s.loadSchedules()
	now := time.Now()
	v := schedulesView{Schedules: h.Schedules, Tasks: map[string]taskState{}, Now: now.Format(time.RFC3339)}
	if v.Schedules == nil {
		v.Schedules = []schedule{}
	}
	v.TimeZone, _ = now.Zone()
	if h.quietAt(now) {
		v.QuietUntil, v.QuietWhen = h.QuietUntil, h.QuietWhen
	}
	for _, t := range tasks {
		st := taskState{Allowed: h.allowedAt(t, now)}
		if r, ok := h.Tasks[t]; ok {
			r := r
			st.Rule = &r
		}
		if next := h.nextAllowed(t, now); !next.IsZero() {
			st.NextAllow = next.Format(time.RFC3339)
		}
		v.Tasks[t] = st
	}
	return v
}

func validSchedule(sc schedule) error {
	if strings.TrimSpace(sc.Name) == "" {
		return fmt.Errorf("Give the schedule a name.")
	}
	if len(sc.Ranges) == 0 {
		return fmt.Errorf("Add at least one time range to %q.", sc.Name)
	}
	for _, r := range sc.Ranges {
		if len(r.Days) == 0 {
			return fmt.Errorf("Pick at least one day for each range in %q.", sc.Name)
		}
		for _, d := range r.Days {
			if d < 0 || d > 6 {
				return fmt.Errorf("A day in %q isn't valid.", sc.Name)
			}
		}
		if _, ok := clockMinutes(r.Start); !ok {
			return fmt.Errorf("A start time in %q must look like 18:30.", sc.Name)
		}
		if _, ok := clockMinutes(r.End); !ok {
			return fmt.Errorf("An end time in %q must look like 22:45.", sc.Name)
		}
	}
	return nil
}

func validRule(task string, r taskRule, h homeSchedules) error {
	if r.Schedule == "" {
		return nil
	}
	if _, ok := h.find(r.Schedule); !ok {
		return fmt.Errorf("That schedule doesn't exist any more.")
	}
	if r.Mode != "only" && r.Mode != "never" {
		return fmt.Errorf("Choose \"only during\" or \"never during\".")
	}
	if task != taskDownloads {
		return nil
	}
	switch r.Action {
	case "", dlPause:
		if r.When != "" && r.When != "now" && r.When != "finish" {
			return fmt.Errorf("Choose whether running downloads pause at once or finish first.")
		}
	case dlLimit:
		if r.LimitMB < 1 {
			return fmt.Errorf("Set a speed of at least 1 MB/s.")
		}
	case dlConnections:
		if r.Connections < 1 {
			return fmt.Errorf("Set at least 1 connection.")
		}
	default:
		return fmt.Errorf("That download rule isn't known.")
	}
	return nil
}

type schedulesPut struct {
	Schedules *[]schedule          `json:"schedules"`
	Tasks     map[string]*taskRule `json:"tasks"` // null removes a task's rule
}

// putSchedules saves schedules and the rules of tasks (only those in
// allowed); other tasks' rules are kept.
func (s *Server) putSchedules(w http.ResponseWriter, req schedulesPut, allowed []string) {
	schedulesMu.Lock()
	defer schedulesMu.Unlock()
	h := s.loadSchedules()
	if req.Schedules != nil {
		seen := map[string]bool{}
		for i, sc := range *req.Schedules {
			if err := validSchedule(sc); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if sc.ID == "" || seen[sc.ID] {
				sc.ID = fmt.Sprintf("s%d", time.Now().UnixNano()+int64(i))
				(*req.Schedules)[i] = sc
			}
			seen[sc.ID] = true
		}
		h.Schedules = *req.Schedules
		for t, rule := range h.Tasks { // rules pointing at a deleted schedule go
			if _, ok := h.find(rule.Schedule); !ok {
				delete(h.Tasks, t)
			}
		}
	}
	for t, rule := range req.Tasks {
		if !slices.Contains(allowed, t) {
			writeError(w, http.StatusBadRequest, "That task can't be scheduled here.")
			return
		}
		if rule == nil || rule.Schedule == "" {
			delete(h.Tasks, t)
			continue
		}
		if err := validRule(t, *rule, h); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.Tasks[t] = *rule
	}
	if err := s.saveSchedules(h); err != nil {
		writeError(w, http.StatusInternalServerError, "The schedules couldn't be saved.")
		return
	}
	s.background(func() { s.scheduleJob(context.Background()) })
	writeJSON(w, http.StatusOK, s.schedulesView(allowed))
}

// GET/PUT /api/home/schedules: the schedules and the shared tasks' rules.
func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req schedulesPut
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "That request couldn't be read.")
			return
		}
		s.putSchedules(w, req, sharedTasks)
		return
	}
	writeJSON(w, http.StatusOK, s.schedulesView(sharedTasks))
}

// GET/PUT /api/scenes/schedule: the scene tasks' rules (schedules are made
// on the shared page and only picked here).
func (s *Server) handleSceneSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var req schedulesPut
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "That request couldn't be read.")
			return
		}
		if req.Schedules != nil {
			writeError(w, http.StatusBadRequest, "Schedules are made in Settings > Schedules.")
			return
		}
		s.putSchedules(w, req, sceneTasks)
		return
	}
	writeJSON(w, http.StatusOK, s.schedulesView(sceneTasks))
}

// POST /api/home/quiet {"minutes":120,"when":"now"|"finish"} holds every heavy
// task for a while; {"minutes":0} ends it. "untilMorning":true lasts until 7:00.
func (s *Server) handleQuiet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Minutes      int    `json:"minutes"`
		UntilMorning bool   `json:"untilMorning"`
		When         string `json:"when"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.When != "now" {
		req.When = "finish"
	}
	schedulesMu.Lock()
	h := s.loadSchedules()
	now := time.Now()
	switch {
	case req.UntilMorning:
		m := time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, now.Location())
		if !m.After(now) {
			m = m.AddDate(0, 0, 1)
		}
		h.QuietUntil, h.QuietWhen = m.Format(time.RFC3339), req.When
	case req.Minutes > 0 && req.Minutes <= 24*60:
		h.QuietUntil, h.QuietWhen = now.Add(time.Duration(req.Minutes)*time.Minute).Format(time.RFC3339), req.When
	default:
		h.QuietUntil, h.QuietWhen = "", ""
	}
	err := s.saveSchedules(h)
	schedulesMu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Quiet couldn't be saved.")
		return
	}
	// Upstream's own "Quiet for a while" (3.1.0) holds the background jobs;
	// this one sets it too, so one button covers both.
	_ = s.Settings.Set(settings.KeyQuietUntil, h.QuietUntil, false)
	s.scheduleJob(r.Context())
	writeJSON(w, http.StatusOK, s.schedulesView(sharedTasks))
}
