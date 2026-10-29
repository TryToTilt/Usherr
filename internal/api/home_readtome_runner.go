package api

// Usherr: Read-to-Me's runner. Chatterbox takes about 3.5 GB of the graphics
// card, so it shouldn't run all the time. The runner (a small container next to
// the voice engines, docs/home/read-to-me/runner) starts and stops them when
// Usherr asks: a job that needs Chatterbox starts the model it wants
// (standard, or Turbo, which is faster), waits until it answers, and when the
// queue is empty, or a job has been held back a long while, Chatterbox is
// stopped again. Usherr only stops what it started itself. Without a runner
// everything works as before: Chatterbox is started and stopped by hand.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	keyRTRunnerURL   = "readtome.runner_url"
	keyRTRunnerToken = "readtome.runner_token" // secret
	keyRTModel       = "readtome.chatterbox_model"

	modelStandard = "standard"
	modelTurbo    = "turbo"

	rtvEngineStartWait = 6 * time.Minute // the model loads in about two minutes

	rtvHeldStop = 10 * time.Minute
)

// rtvIdleStop: Chatterbox is stopped this long after its last piece, with
// nothing queued (atomic: the job runner reads it while tests change it).
var rtvIdleStop atomic.Int64

func init() { rtvIdleStop.Store(int64(2 * time.Minute)) }

// The containers of the two Chatterbox models (they answer on one address,
// so only one runs at a time; the runner stops the other).
var modelContainer = map[string]string{modelStandard: "chatterbox", modelTurbo: "chatterbox-turbo"}

func validModel(m string) bool { return m == modelStandard || m == modelTurbo }

// defaultModel is the Chatterbox model a job uses unless it asks for another.
func (s *Server) defaultModel() string {
	if m, _ := s.Settings.Get(keyRTModel); validModel(m) {
		return m
	}
	return modelStandard
}

// rtvEngines is what Usherr has started, and when Chatterbox last spoke.
var rtvEngines = struct {
	sync.Mutex
	started  map[string]bool // container -> started by Usherr
	lastUsed time.Time
}{started: map[string]bool{}}

func (s *Server) runnerConfig() (string, string, bool) {
	u, _ := s.Settings.Get(keyRTRunnerURL)
	tok, _ := s.Settings.Get(keyRTRunnerToken)
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	return u, tok, u != "" && tok != ""
}

// runnerCall asks the runner; the answer is every engine's state.
func (s *Server) runnerCall(ctx context.Context, method, path string) (map[string]string, error) {
	base, tok, ok := s.runnerConfig()
	if !ok {
		return nil, errors.New("the runner isn't set up")
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, method, base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the runner isn't answering: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var out struct {
		Engines map[string]string `json:"engines"`
		Error   string            `json:"error"`
	}
	_ = json.Unmarshal(body, &out)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errors.New("the runner refused the token")
	case resp.StatusCode != http.StatusOK:
		if out.Error == "" {
			out.Error = resp.Status
		}
		return out.Engines, errors.New("the runner: " + out.Error)
	}
	return out.Engines, nil
}

func (s *Server) runnerStatus(ctx context.Context) (map[string]string, error) {
	return s.runnerCall(ctx, http.MethodGet, "/status")
}

// chatterboxModel is the model the Chatterbox address answers with now, or
// "" when it doesn't answer.
func (s *Server) chatterboxModel(ctx context.Context) string {
	base := s.engineAddress(engineChatterbox)
	if base == "" {
		return ""
	}
	var h struct {
		Loaded bool   `json:"model_loaded"`
		Model  string `json:"model"`
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if rtvGet(cctx, base+"/health", &h) != nil || !h.Loaded {
		return ""
	}
	if h.Model == modelTurbo {
		return modelTurbo
	}
	return modelStandard
}

// ensureChatterbox makes sure the Chatterbox model asked for answers, starting
// it through the runner when there is one (and the other model is stopped by
// it). say tells the job what it is waiting for.
func (s *Server) ensureChatterbox(ctx context.Context, model string, say func(string)) error {
	if !validModel(model) {
		model = s.defaultModel()
	}
	rtvEngines.Lock()
	rtvEngines.lastUsed = time.Now()
	rtvEngines.Unlock()
	if _, _, ok := s.runnerConfig(); !ok {
		return nil // started by hand: whatever runs is used
	}
	if s.chatterboxModel(ctx) == model {
		return nil
	}
	name := modelContainer[model]
	if say != nil {
		say(fmt.Sprintf("Starting Chatterbox (%s)", model))
	}
	if _, err := s.runnerCall(ctx, http.MethodPost, "/start/"+name); err != nil {
		return err
	}
	rtvEngines.Lock()
	rtvEngines.started[name] = true
	for m, other := range modelContainer {
		if m != model {
			delete(rtvEngines.started, other) // the runner stopped it
		}
	}
	rtvEngines.Unlock()
	forgetVoices()
	deadline := time.Now().Add(rtvEngineStartWait)
	for time.Now().Before(deadline) {
		if s.chatterboxModel(ctx) == model {
			rtvEngines.Lock()
			rtvEngines.lastUsed = time.Now()
			rtvEngines.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("Chatterbox (%s) was started but didn't answer within %d minutes", model, int(rtvEngineStartWait.Minutes()))
}

// stopStartedChatterbox stops the Chatterbox containers Usherr started.
func (s *Server) stopStartedChatterbox(ctx context.Context) {
	rtvEngines.Lock()
	var names []string
	for _, n := range modelContainer {
		if rtvEngines.started[n] {
			names = append(names, n)
			delete(rtvEngines.started, n)
		}
	}
	rtvEngines.Unlock()
	for _, n := range names {
		_, _ = s.runnerCall(ctx, http.MethodPost, "/stop/"+n)
	}
	if len(names) > 0 {
		forgetVoices()
	}
}

// readToMeIdle (the job runner, when nothing is queued) stops Chatterbox a
// couple of minutes after it last spoke, if Usherr started it.
func (s *Server) readToMeIdle(ctx context.Context) {
	s.tidyMakings(ctx)
	rtvEngines.Lock()
	idle := time.Since(rtvEngines.lastUsed) > time.Duration(rtvIdleStop.Load())
	any := len(rtvEngines.started) > 0
	rtvEngines.Unlock()
	if !idle || !any {
		return
	}
	var active int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM readtome_jobs WHERE state IN (?, ?, ?)`, jobQueued, jobRunning, jobWaiting).Scan(&active)
	if active == 0 {
		s.stopStartedChatterbox(ctx)
	}
}

func forgetVoices() {
	voiceMu.Lock()
	voiceCache = map[string]voiceList{}
	voiceMu.Unlock()
}

// ---- routes ----

type enginesView struct {
	Runner  bool              `json:"runner"`            // a runner is set up
	States  map[string]string `json:"states,omitempty"`  // container -> running, stopped, missing
	Model   string            `json:"model"`             // the Chatterbox model answering now ("" when none)
	Default string            `json:"default"`           // the model jobs use unless they ask
	Started []string          `json:"started,omitempty"` // the containers Usherr started
	Problem string            `json:"problem,omitempty"`
}

func (s *Server) enginesView(ctx context.Context) enginesView {
	v := enginesView{Model: s.chatterboxModel(ctx), Default: s.defaultModel()}
	if _, _, ok := s.runnerConfig(); ok {
		v.Runner = true
		st, err := s.runnerStatus(ctx)
		if err != nil {
			v.Problem = err.Error()
		}
		v.States = st
	}
	rtvEngines.Lock()
	for n := range rtvEngines.started {
		v.Started = append(v.Started, n)
	}
	rtvEngines.Unlock()
	return v
}

// GET /api/home/readtome/engines: which voice engines run, and the runner.
func (s *Server) handleReadToMeEngines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.enginesView(r.Context()))
}

// POST /api/home/readtome/engines/{name}/{action}: start or stop a voice
// engine through the runner (name: kokoro, chatterbox, chatterbox-turbo).
func (s *Server) handleReadToMeEngineAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	if (name != "kokoro" && name != modelContainer[modelStandard] && name != modelContainer[modelTurbo]) || (action != "start" && action != "stop") {
		writeError(w, http.StatusNotFound, "That isn't a voice engine.")
		return
	}
	if _, _, ok := s.runnerConfig(); !ok {
		writeError(w, http.StatusBadRequest, "The runner isn't set up (Settings > Extras > Read-to-Me).")
		return
	}
	if action == "stop" {
		var running int
		_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM readtome_jobs WHERE state = ?`, jobRunning).Scan(&running)
		if running > 0 && name != "kokoro" {
			writeError(w, http.StatusConflict, "An audiobook is being made. Stop it first, or let it finish.")
			return
		}
	}
	if _, err := s.runnerCall(r.Context(), http.MethodPost, "/"+action+"/"+name); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	rtvEngines.Lock()
	if action == "start" {
		rtvEngines.lastUsed = time.Now()
		rtvEngines.started[name] = true // stopped again when idle, like one a job started
		for _, other := range modelContainer {
			if other != name && name != "kokoro" {
				delete(rtvEngines.started, other) // the runner stopped it
			}
		}
	} else {
		delete(rtvEngines.started, name)
	}
	rtvEngines.Unlock()
	forgetVoices()
	writeJSON(w, http.StatusOK, s.enginesView(r.Context()))
}
