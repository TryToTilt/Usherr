package api

// Usherr: Read-to-Me turns the household's ebooks into audiobooks with free
// voices on the media PC (see home_readtome_tts.go for the engines). This file
// holds the settings, the list of voices, a sample of each voice, and the
// preview of a book in a voice; the audiobook-making jobs are in
// home_readtome_job.go. Everything here needs the "manage" permission except
// the settings, which are for administrators.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/books"
)

const (
	keyRTNightOnly = "readtome.night_only" // "1": audiobooks are made at night only
	keyRTNightFrom = "readtome.night_from" // "23:00"
	keyRTNightTo   = "readtome.night_to"   // "07:00"

	defaultNightFrom = "23:00"
	defaultNightTo   = "07:00"

	rtvSampleText   = "Hello. This is how I would read your book aloud, and this is the pace I would keep."
	rtvPreviewWords = 200
)

func (s *Server) registerReadToMeRoutes(manage, admin routeGroup) {
	manage.HandleFunc("GET /api/home/readtome/voices", s.handleReadToMeVoices)
	manage.HandleFunc("GET /api/home/readtome/sample", s.handleReadToMeSample)
	manage.HandleFunc("POST /api/home/readtome/preview", s.handleReadToMePreview)
	manage.HandleFunc("GET /api/home/readtome/book", s.handleReadToMeBook)
	manage.HandleFunc("POST /api/home/readtome/jobs", s.handleReadToMeMake)
	manage.HandleFunc("GET /api/home/readtome/jobs", s.handleReadToMeJobs)
	manage.HandleFunc("DELETE /api/home/readtome/jobs/{id}", s.handleReadToMeCancel)
	manage.HandleFunc("GET /api/home/readtome/jobs/{id}/chapters/{n}", s.handleReadToMeEarlyChapter)
	manage.HandleFunc("GET /api/home/readtome/made", s.handleReadToMeMade)
	manage.HandleFunc("GET /api/home/readtome/say", s.handleReadToMeSay)
	manage.HandleFunc("GET /api/home/readtome/engines", s.handleReadToMeEngines)
	manage.HandleFunc("GET /api/home/readtome/cast", s.handleReadToMeCast)
	manage.HandleFunc("POST /api/home/readtome/cast/ai", s.handleReadToMeCastAI)
	manage.HandleFunc("GET /api/home/readtome/cast/lines", s.handleReadToMeCastLines)
	manage.HandleFunc("PUT /api/home/readtome/cast/lines", s.handlePutReadToMeCastLine)
	manage.HandleFunc("POST /api/home/readtome/engines/{name}/{action}", s.handleReadToMeEngineAction)
	manage.HandleFunc("GET /api/home/readtome/presets", s.handleReadToMePresets)
	manage.HandleFunc("POST /api/home/readtome/presets", s.handleSaveReadToMePreset)
	manage.HandleFunc("PUT /api/home/readtome/presets/{id}", s.handleReadToMePresetDefault)
	manage.HandleFunc("DELETE /api/home/readtome/presets/{id}", s.handleDeleteReadToMePreset)
	manage.HandleFunc("PUT /api/home/readtome/say", s.handlePutReadToMeSay)
	manage.HandleFunc("DELETE /api/home/readtome/made/{id}", s.handleReadToMeDeleteMade)
	manage.HandleFunc("GET /api/home/readtome/made/{id}/lines", s.handleReadToMeMadeLines)
	manage.HandleFunc("GET /api/home/readtome/made/{id}/line", s.handleReadToMeMadeLine)
	manage.HandleFunc("POST /api/home/readtome/made/{id}/redo", s.handleReadToMeRedo)
	manage.HandleFunc("DELETE /api/home/readtome/made/{id}/makings", s.handleReadToMeFreeMakings)
	manage.HandleFunc("GET /api/home/readtome/text", s.handleReadToMeText)
	manage.HandleFunc("PUT /api/home/readtome/text", s.handlePutReadToMeText)
	manage.HandleFunc("DELETE /api/home/readtome/text", s.handleDeleteReadToMeText)
	manage.HandleFunc("GET /api/home/readtome/chatterbox/voices", s.handleReadToMeCopiedVoices)
	manage.HandleFunc("POST /api/home/readtome/chatterbox/voices", s.handleCopyReadToMeVoice)
	manage.HandleFunc("DELETE /api/home/readtome/chatterbox/voices/{name}", s.handleDeleteReadToMeVoice)
	manage.HandleFunc("GET /api/home/readtome/chatterbox/voices/{name}/recording", s.handleReadToMeVoiceRecording)
	admin.HandleFunc("GET /api/home/readtome/settings", s.handleGetReadToMeSettings)
	admin.HandleFunc("PUT /api/home/readtome/settings", s.handlePutReadToMeSettings)
	admin.HandleFunc("POST /api/home/readtome/test", s.handleTestReadToMe)
}

// ---- the voices ----

type voiceList struct {
	voices []rtvVoice
	at     time.Time
}

var (
	voiceMu    sync.Mutex
	voiceCache = map[string]voiceList{} // engine name + address -> its voices
)

const voiceKeep = time.Hour

// engineVoices lists an engine's voices, remembered for an hour (a failure for
// half a minute, so a stopped engine isn't asked at every page view).
func (s *Server) engineVoices(ctx context.Context, e rtvEngine, address string) ([]rtvVoice, error) {
	key := e.name() + "|" + address
	voiceMu.Lock()
	c, ok := voiceCache[key]
	voiceMu.Unlock()
	if ok && ((len(c.voices) > 0 && time.Since(c.at) < voiceKeep) || (len(c.voices) == 0 && time.Since(c.at) < 30*time.Second)) {
		return c.voices, nil
	}
	vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	vs, err := e.voices(vctx)
	if err != nil {
		vs = nil
	}
	voiceMu.Lock()
	voiceCache[key] = voiceList{voices: vs, at: time.Now()}
	voiceMu.Unlock()
	return vs, err
}

func (s *Server) engineAddress(name string) string {
	key := keyRTKokoro
	if name == engineChatterbox {
		key = keyRTChatterbox
	}
	v, _ := s.Settings.Get(key)
	return strings.TrimRight(strings.TrimSpace(v), "/")
}

type engineState struct {
	Name    string `json:"name"`
	On      bool   `json:"on"`    // an address is set
	Ready   bool   `json:"ready"` // it answers
	Problem string `json:"problem,omitempty"`
	// Chatterbox with a runner: started by Usherr when a job or a preview needs it.
	OnDemand bool `json:"onDemand,omitempty"`
}

// GET /api/home/readtome/voices: the voices of every engine that answers. An
// engine that doesn't is left out of the list and says why.
func (s *Server) handleReadToMeVoices(w http.ResponseWriter, r *http.Request) {
	out := struct {
		Engines []engineState `json:"engines"`
		Voices  []rtvVoice    `json:"voices"`
	}{Engines: []engineState{}, Voices: []rtvVoice{}}
	for _, name := range []string{engineKokoro, engineChatterbox} {
		st := engineState{Name: name}
		if e, on := s.readEngine(name); on {
			st.On = true
			vs, err := s.engineVoices(r.Context(), e, s.engineAddress(name))
			_, _, runner := s.runnerConfig()
			switch {
			case err != nil && name == engineChatterbox && runner:
				// Stopped, but started when needed: its own voice and the copied ones can be picked.
				st.OnDemand = true
				out.Voices = append(out.Voices, s.withCopied(r.Context(), []rtvVoice{chatterboxDefault()})...)
			case err != nil:
				st.Problem = engineProblem(name, err)
			case name == engineChatterbox:
				st.Ready = true
				out.Voices = append(out.Voices, s.withCopied(r.Context(), vs)...)
			default:
				st.Ready = true
				out.Voices = append(out.Voices, vs...)
			}
		}
		out.Engines = append(out.Engines, st)
	}
	writeJSON(w, http.StatusOK, out)
}

func engineProblem(name string, err error) string {
	if name == engineChatterbox {
		return "Chatterbox isn't answering (" + err.Error() + "). Start it with: docker compose --profile chatterbox up -d"
	}
	return "Kokoro isn't answering (" + err.Error() + ")."
}

// voiceExists checks a voice against what the engine lists, so what reaches a
// file name or an engine is always a real voice.
func (s *Server) voiceExists(ctx context.Context, engine, voice string) (rtvEngine, error) {
	e, on := s.readEngine(engine)
	if !on {
		return nil, errors.New("That voice engine isn't set up (Settings > Extras > Read-to-Me).")
	}
	if _, _, runner := s.runnerConfig(); runner && engine == engineChatterbox && voice == "default" {
		return e, nil // its own voice, there whether it runs now or is started for the job
	}
	if engine == engineChatterbox && s.isCopiedVoice(ctx, voice) {
		return e, nil // a copied voice: Chatterbox is given it when it reads
	}
	vs, err := s.engineVoices(ctx, e, s.engineAddress(engine))
	if err != nil {
		return nil, errors.New(engineProblem(engine, err))
	}
	has := func(id string) bool {
		for _, v := range vs {
			if v.ID == id {
				return true
			}
		}
		return false
	}
	if has(voice) {
		return e, nil
	}
	// A mix of Kokoro voices: each of them must be there.
	if parts, ok := parseMix(voice); ok && engine == engineKokoro {
		for _, p := range parts {
			if !has(p.ID) {
				return nil, errors.New("A voice in that mix isn't there.")
			}
		}
		return e, nil
	}
	return nil, errors.New("That voice isn't there.")
}

// ---- samples ----

var rtvUnsafeName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func (s *Server) readToMeCache(parts ...string) string {
	return filepath.Join(append([]string{s.cfg.ConfigDir, "cache", "readtome"}, parts...)...)
}

// GET /api/home/readtome/sample?engine=&voice=: a fixed sentence in the voice,
// as an MP3, made once and kept.
func (s *Server) handleReadToMeSample(w http.ResponseWriter, r *http.Request) {
	engine, voice := r.URL.Query().Get("engine"), r.URL.Query().Get("voice")
	e, err := s.voiceExists(r.Context(), engine, voice)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if engine == engineChatterbox {
		if err := s.ensureChatterbox(r.Context(), r.URL.Query().Get("model"), nil); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		engine += "-" + s.chatterboxModel(r.Context()) // each model has its own samples
	}
	file := s.readToMeCache("samples", rtvUnsafeName.ReplaceAllString(engine+"-"+voice, "_")+".mp3")
	if data, err := os.ReadFile(file); err == nil && len(data) > 0 {
		sendAudio(w, data)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	raw, ext, err := speakPiece(ctx, e, voice, rtvSampleText, 1)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	data, err := toMP3(ctx, raw, ext)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err == nil {
		_ = os.WriteFile(file, data, 0o644)
	}
	sendAudio(w, data)
}

func sendAudio(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}

// ---- the preview ----

type readRequest struct {
	BookID int64   `json:"bookId"`
	Engine string  `json:"engine"`
	Voice  string  `json:"voice"`
	Speed  float64 `json:"speed"`
	Remake bool    `json:"remake"` // make again the audiobook Read-to-Me made of it
	// What is read (rtvOptions).
	Chapters  []int    `json:"chapters"`
	NoIntro   bool     `json:"noIntro"`
	NoTitles  bool     `json:"noTitles"`
	ParaMs    int      `json:"paraMs"`
	SceneMs   int      `json:"sceneMs"`
	ChapterMs int      `json:"chapterMs"`
	CbModel   string   `json:"cbModel"`
	Cast      *rtvCast `json:"cast"`
	Tune      *rtvTune `json:"tune"`
	// A preview's length in words (30 to 200; 0: the usual); A/B uses a short one.
	Words int `json:"words"`
}

func (r readRequest) options() rtvOptions {
	o := rtvOptions{Chapters: r.Chapters, NoIntro: r.NoIntro, NoTitles: r.NoTitles, ParaMs: r.ParaMs, SceneMs: r.SceneMs, ChapterMs: r.ChapterMs, Cast: r.Cast, Tune: r.Tune}.clamped()
	if validModel(r.CbModel) {
		o.CbModel = r.CbModel
	}
	return o
}

// checkRead reads and checks what a person asks for: a book with an EPUB, a
// voice that exists, a speed from 0.8 to 1.3.
func (s *Server) checkRead(r *http.Request) (readRequest, books.Book, rtvEngine, error) {
	var req readRequest
	if err := decodeJSON(r, &req); err != nil {
		return req, books.Book{}, nil, errors.New("That request couldn't be read.")
	}
	if req.Speed == 0 {
		req.Speed = 1
	}
	if req.Speed < 0.8 || req.Speed > 1.3 {
		return req, books.Book{}, nil, errors.New("The speed is from 0.8 to 1.3.")
	}
	b, err := s.BookRepo.Get(req.BookID)
	if err != nil {
		return req, b, nil, errors.New("That book isn't in the library.")
	}
	if b.EbookPath == "" || !strings.EqualFold(b.EbookFormat, "epub") || b.EbookStatus != books.StatusDownloaded {
		return req, b, nil, errors.New("Read-to-Me needs the book as an EPUB in the library.")
	}
	e, err := s.voiceExists(r.Context(), req.Engine, req.Voice)
	if err != nil {
		return req, b, nil, err
	}
	return req, b, e, nil
}

// POST /api/home/readtome/preview {bookId, engine, voice, speed}: the first
// words of the book in the voice, as an MP3.
func (s *Server) handleReadToMePreview(w http.ResponseWriter, r *http.Request) {
	req, b, e, err := s.checkRead(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	book, err := s.readingBook(r.Context(), b)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if req.Engine == engineChatterbox {
		if err := s.ensureChatterbox(r.Context(), req.CbModel, nil); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	// The first chapter picked, read as the audiobook will read it.
	rules := s.sayRules(r.Context(), b.ID)
	first := book.Chapters[req.options().chapterIndexes(len(book.Chapters))[0]]
	words := rtvPreviewWords
	if req.Words > 0 {
		words = max(30, min(rtvPreviewWords, req.Words))
	}
	text := firstWords(speakable(first.Text, rules), words)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	cast := req.options().Cast
	if err := s.checkCast(r, cast); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A preview is cut into pieces like the book will be (with its cast), and joined.
	pieces := splitSpeech(text, e.maxPiece())
	genders := map[string]string{}
	if cast != nil {
		paras, lines := castChapter(text)
		takeTurns(paras, lines)
		pieces = castPieces(paras, lines, s.castMax(e, cast))
	}
	var parts [][]byte
	for _, p := range pieces {
		pe, pv := e, req.Voice
		if ref := cast.voiceFor(p.Speaker, genders); ref != nil {
			if ce, ok := s.readEngine(ref.Engine); ok {
				pe, pv = ce, ref.Voice
			}
		}
		if pe.name() == engineChatterbox {
			if err := s.ensureChatterbox(ctx, req.CbModel, nil); err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
		}
		raw, ext, err := speakPiece(ctx, tuned(pe, req.options().Tune), pv, p.Text, req.Speed)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		mp3, err := toMP3(ctx, raw, ext)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		parts = append(parts, mp3)
	}
	sendAudio(w, joinMP3(parts))
}

// joinMP3 puts MP3 pieces end to end (MP3 frames can simply follow each other).
func joinMP3(parts [][]byte) []byte {
	var n int
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// ---- settings ----

type readToMeSettings struct {
	KokoroURL     string        `json:"kokoroUrl"`
	ChatterboxURL string        `json:"chatterboxUrl"`
	NightOnly     bool          `json:"nightOnly"`
	NightFrom     string        `json:"nightFrom"`
	NightTo       string        `json:"nightTo"`
	Engines       []engineState `json:"engines"`
	// The runner (home_readtome_runner.go), and the Chatterbox model jobs use.
	RunnerURL      string `json:"runnerUrl"`
	RunnerTokenSet bool   `json:"runnerTokenSet"`
	Model          string `json:"chatterboxModel"`
}

func (s *Server) currentReadToMeSettings(ctx context.Context) readToMeSettings {
	night, _ := s.Settings.Get(keyRTNightOnly)
	from, _ := s.Settings.Get(keyRTNightFrom)
	to, _ := s.Settings.Get(keyRTNightTo)
	out := readToMeSettings{
		KokoroURL: s.engineAddress(engineKokoro), ChatterboxURL: s.engineAddress(engineChatterbox),
		NightOnly: night == "1", NightFrom: defaultNightFrom, NightTo: defaultNightTo, Engines: []engineState{},
	}
	if validClock(from) {
		out.NightFrom = from
	}
	out.RunnerURL, _ = s.Settings.Get(keyRTRunnerURL)
	tok, _ := s.Settings.Get(keyRTRunnerToken)
	out.RunnerTokenSet = tok != ""
	out.Model = s.defaultModel()
	if validClock(to) {
		out.NightTo = to
	}
	for _, name := range []string{engineKokoro, engineChatterbox} {
		st := engineState{Name: name}
		if e, on := s.readEngine(name); on {
			st.On = true
			cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
			if err := e.ready(cctx); err != nil {
				st.Problem = engineProblem(name, err)
			} else {
				st.Ready = true
			}
			cancel()
		}
		out.Engines = append(out.Engines, st)
	}
	return out
}

var clockRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func validClock(s string) bool { return clockRe.MatchString(s) }

func (s *Server) handleGetReadToMeSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.currentReadToMeSettings(r.Context()))
}

// PUT /api/home/readtome/settings: only what is sent changes.
func (s *Server) handlePutReadToMeSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		KokoroURL     *string `json:"kokoroUrl"`
		ChatterboxURL *string `json:"chatterboxUrl"`
		NightOnly     *bool   `json:"nightOnly"`
		NightFrom     *string `json:"nightFrom"`
		NightTo       *string `json:"nightTo"`
		RunnerURL     *string `json:"runnerUrl"`
		RunnerToken   *string `json:"runnerToken"` // "" clears it
		Model         *string `json:"chatterboxModel"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	for _, f := range []struct {
		val  *string
		key  string
		what string
	}{{req.KokoroURL, keyRTKokoro, "Kokoro's address"}, {req.ChatterboxURL, keyRTChatterbox, "Chatterbox's address"}} {
		if f.val == nil {
			continue
		}
		a, err := cleanBaseURL(*f.val)
		if err != nil {
			writeError(w, http.StatusBadRequest, f.what+" "+err.Error())
			return
		}
		_ = s.Settings.Set(f.key, a, false)
	}
	for _, f := range []struct {
		val *string
		key string
	}{{req.NightFrom, keyRTNightFrom}, {req.NightTo, keyRTNightTo}} {
		if f.val == nil {
			continue
		}
		if !validClock(*f.val) {
			writeError(w, http.StatusBadRequest, "A time looks like 23:00.")
			return
		}
		_ = s.Settings.Set(f.key, *f.val, false)
	}
	if req.RunnerURL != nil {
		a, err := cleanBaseURL(*req.RunnerURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "The runner's address "+err.Error())
			return
		}
		_ = s.Settings.Set(keyRTRunnerURL, a, false)
	}
	if req.RunnerToken != nil {
		_ = s.Settings.Set(keyRTRunnerToken, strings.TrimSpace(*req.RunnerToken), strings.TrimSpace(*req.RunnerToken) != "")
	}
	if req.Model != nil {
		if !validModel(*req.Model) {
			writeError(w, http.StatusBadRequest, "The Chatterbox model is standard or turbo.")
			return
		}
		_ = s.Settings.Set(keyRTModel, *req.Model, false)
	}
	if req.NightOnly != nil {
		v := "0"
		if *req.NightOnly {
			v = "1"
		}
		_ = s.Settings.Set(keyRTNightOnly, v, false)
	}
	// A new address means a new list of voices.
	voiceMu.Lock()
	voiceCache = map[string]voiceList{}
	voiceMu.Unlock()
	writeJSON(w, http.StatusOK, s.currentReadToMeSettings(r.Context()))
}

// POST /api/home/readtome/test {engine}: the engine answers and speaks.
func (s *Server) handleTestReadToMe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Engine string `json:"engine"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.Engine == "runner" {
		st, err := s.runnerStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		var parts []string
		for _, n := range []string{"kokoro", "chatterbox", "chatterbox-turbo"} {
			parts = append(parts, n+" "+st[n])
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "The runner answers: " + strings.Join(parts, ", ") + "."})
		return
	}
	e, on := s.readEngine(req.Engine)
	if !on {
		writeError(w, http.StatusBadRequest, "Enter its address first.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	voiceMu.Lock()
	voiceCache = map[string]voiceList{}
	voiceMu.Unlock()
	vs, err := s.engineVoices(ctx, e, s.engineAddress(req.Engine))
	if err != nil {
		writeError(w, http.StatusBadGateway, engineProblem(req.Engine, err))
		return
	}
	if len(vs) == 0 {
		writeError(w, http.StatusBadGateway, "It answers, but has no voices.")
		return
	}
	start := time.Now()
	if _, _, err := speakPiece(ctx, e, vs[0].ID, "This is a test.", 1); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("It works: %d voices, and it spoke a sentence in %.1f seconds.", len(vs), time.Since(start).Seconds())})
}

// hashOf makes a short name for a piece of text, for files kept by their text.
func hashOf(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:6])
}
