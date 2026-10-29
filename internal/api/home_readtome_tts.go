package api

// Usherr: Read-to-Me's voices. Two engines on the media PC, both with an
// OpenAI-style /v1/audio/speech: Kokoro (always on, the everyday narrator) and
// Chatterbox (started only when wanted: more expressive, slower, and it can copy
// a voice). This file is the small client for them, the splitting of a chapter
// into pieces a voice can take, and the one-at-a-time rule: there is one GPU, so
// only one piece is ever being spoken.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	engineKokoro     = "kokoro"
	engineChatterbox = "chatterbox"
)

type rtvVoice struct {
	Engine string `json:"engine"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Label  string `json:"label"`          // how a list shows it: "Heart · American · woman"
	Note   string `json:"note,omitempty"` // "expressive, slower"
	Grade  string `json:"grade,omitempty"`
}

// rtvEngine is a voice engine as the rest of Read-to-Me sees it.
type rtvEngine interface {
	name() string
	// ready says whether the engine answers and can speak; the error is for people.
	ready(ctx context.Context) error
	voices(ctx context.Context) ([]rtvVoice, error)
	// speak returns the audio and its file extension ("mp3" or "wav"). Speed
	// 1.0 is normal; an engine that can't change speed itself ignores it
	// (nativeSpeed is false) and the caller speeds the audio up.
	speak(ctx context.Context, voice, text string, speed float64) ([]byte, string, error)
	nativeSpeed() bool
	// maxPiece is the longest piece of text, in characters, the engine takes well.
	maxPiece() int
}

// gpuLock: one piece at a time, whatever asked for it (a sample, a preview, a job).
var gpuLock sync.Mutex

// rtvRetryStep is the wait before a retry, times the number of the retry.
var rtvRetryStep = 3 * time.Second

var rtvHTTP = &http.Client{Timeout: 10 * time.Minute}

func rtvGet(ctx context.Context, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := rtvHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("answered %d", resp.StatusCode)
	}
	if into == nil {
		return nil
	}
	return json.Unmarshal(body, into)
}

// rtvPostJSON posts body as JSON and returns the audio; header is name, value pairs.
func rtvPostJSON(ctx context.Context, url string, body any, header ...string) ([]byte, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := rtvHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("answered %d: %s", resp.StatusCode, msg)
	}
	if len(data) < 200 {
		return nil, errors.New("sent no audio")
	}
	return data, nil
}

// ---- Kokoro ----

type kokoroEngine struct{ base string }

func (k kokoroEngine) name() string      { return engineKokoro }
func (k kokoroEngine) nativeSpeed() bool { return true }
func (k kokoroEngine) maxPiece() int     { return 600 }

func (k kokoroEngine) ready(ctx context.Context) error {
	vs, err := k.voices(ctx)
	if err != nil {
		return err
	}
	if len(vs) == 0 {
		return errors.New("it has no voices")
	}
	return nil
}

var kokoroAccents = map[byte]string{'a': "American", 'b': "British", 'e': "Spanish", 'f': "French", 'h': "Hindi", 'i': "Italian", 'j': "Japanese", 'p': "Portuguese", 'z': "Chinese"}

// kokoroLabel names a voice from its id: "af_heart" is "Heart · American · woman".
func kokoroLabel(id string) (string, bool) {
	prefix, rest, ok := strings.Cut(id, "_")
	if !ok || len(prefix) != 2 {
		return id, false
	}
	accent, known := kokoroAccents[prefix[0]]
	who := "woman"
	if prefix[1] == 'm' {
		who = "man"
	}
	name := strings.TrimSuffix(rest, "_inno")
	if name != "" {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	if !known {
		return name + " · " + who, false
	}
	return name + " · " + accent + " · " + who, prefix[0] == 'a' || prefix[0] == 'b'
}

func (k kokoroEngine) voices(ctx context.Context) ([]rtvVoice, error) {
	var raw struct {
		Voices []json.RawMessage `json:"voices"`
	}
	if err := rtvGet(ctx, k.base+"/v1/audio/voices", &raw); err != nil {
		return nil, err
	}
	out := []rtvVoice{}
	for _, r := range raw.Voices {
		var v struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Grade string `json:"overall_grade"`
		}
		if json.Unmarshal(r, &v) != nil || v.ID == "" {
			var plain string // older versions list plain names
			if json.Unmarshal(r, &plain) != nil || plain == "" {
				continue
			}
			v.ID = plain
		}
		label, english := kokoroLabel(v.ID)
		if !english {
			continue // the book readers are English; other languages have their own books
		}
		out = append(out, rtvVoice{Engine: engineKokoro, ID: v.ID, Name: v.ID, Label: label, Grade: v.Grade})
	}
	// Best graded first, then by name.
	sort.SliceStable(out, func(i, j int) bool {
		gi, gj := gradeRank(out[i].Grade), gradeRank(out[j].Grade)
		if gi != gj {
			return gi < gj
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func gradeRank(g string) int {
	if g == "" {
		return 99
	}
	r := int(g[0]-'A') * 3
	switch {
	case strings.HasSuffix(g, "+"):
		r--
	case strings.HasSuffix(g, "-"):
		r++
	}
	return r
}

func (k kokoroEngine) speak(ctx context.Context, voice, text string, speed float64) ([]byte, string, error) {
	body := map[string]any{"model": "kokoro", "input": text, "voice": voice, "response_format": "mp3"}
	if speed > 0 && speed != 1 {
		body["speed"] = speed
	}
	data, err := rtvPostJSON(ctx, k.base+"/v1/audio/speech", body)
	return data, "mp3", err
}

// ---- Chatterbox ----

type chatterboxEngine struct {
	base string
	tune *rtvTune // how it reads (nil: its usual way)
	// give hands Chatterbox a copied voice's recording before it reads with it
	// (home_readtome_voices.go); nil: voices are only what Chatterbox has.
	give func(ctx context.Context, base, voice string) error
}

func (c chatterboxEngine) name() string      { return engineChatterbox }
func (c chatterboxEngine) nativeSpeed() bool { return false }
func (c chatterboxEngine) maxPiece() int     { return 280 }

func (c chatterboxEngine) ready(ctx context.Context) error {
	var h struct {
		Status string `json:"status"`
		Loaded bool   `json:"model_loaded"`
	}
	if err := rtvGet(ctx, c.base+"/health", &h); err != nil {
		return err
	}
	if !h.Loaded {
		return errors.New("it is still loading its model (about two minutes after it starts)")
	}
	return nil
}

func (c chatterboxEngine) voices(ctx context.Context) ([]rtvVoice, error) {
	var raw struct {
		Voices []struct {
			Name string `json:"name"`
		} `json:"voices"`
	}
	if err := c.ready(ctx); err != nil {
		return nil, err
	}
	if err := rtvGet(ctx, c.base+"/voices", &raw); err != nil {
		return nil, err
	}
	note := "expressive, slower"
	out := []rtvVoice{chatterboxDefault()}
	for _, v := range raw.Voices {
		if v.Name != "" {
			out = append(out, rtvVoice{Engine: engineChatterbox, ID: v.Name, Name: v.Name, Label: v.Name + " · copied voice", Note: note})
		}
	}
	return out, nil
}

func (c chatterboxEngine) speak(ctx context.Context, voice, text string, _ float64) ([]byte, string, error) {
	body := map[string]any{"input": text}
	if voice != "" && voice != "default" {
		body["voice"] = voice
		if c.give != nil {
			if err := c.give(ctx, c.base, voice); err != nil {
				return nil, "", err
			}
		}
	}
	var header []string
	if t := c.tune; t != nil {
		if t.Exaggeration != nil {
			body["exaggeration"] = *t.Exaggeration
		}
		if t.Cfg != nil {
			body["cfg_weight"] = *t.Cfg
		}
		if t.Temperature != nil {
			body["temperature"] = *t.Temperature
		}
		if t.Seed != 0 {
			// Turbo takes it in the body; the standard API through Usherr's patch (usherr_speed.py).
			body["seed"] = t.Seed
			header = []string{"X-Usherr-Seed", strconv.FormatInt(t.Seed, 10)}
		}
	}
	data, err := rtvPostJSON(ctx, c.base+"/v1/audio/speech", body, header...)
	return data, "wav", err
}

// chatterboxDefault is Chatterbox's own voice.
func chatterboxDefault() rtvVoice {
	return rtvVoice{Engine: engineChatterbox, ID: "default", Name: "default", Label: "Default · expressive", Note: "expressive, slower"}
}

// ---- finding the engines ----

const (
	keyRTKokoro     = "readtome.kokoro_url"
	keyRTChatterbox = "readtome.chatterbox_url"
)

// readEngine is the engine called name, when its address is set.
func (s *Server) readEngine(name string) (rtvEngine, bool) {
	key := keyRTKokoro
	if name == engineChatterbox {
		key = keyRTChatterbox
	} else if name != engineKokoro {
		return nil, false
	}
	base, _ := s.Settings.Get(key)
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, false
	}
	if name == engineChatterbox {
		return chatterboxEngine{base: base, give: s.giveVoice}, true
	}
	return kokoroEngine{base: base}, true
}

// speakPiece speaks one piece of text, trying three times, and one at a time
// with every other piece. A speed the engine can't do is done afterwards.
func speakPiece(ctx context.Context, e rtvEngine, voice, text string, speed float64) ([]byte, string, error) {
	gpuLock.Lock()
	defer gpuLock.Unlock()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			case <-time.After(time.Duration(attempt) * rtvRetryStep):
			}
		}
		pctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		data, ext, err := e.speak(pctx, voice, text, speed)
		cancel()
		if err == nil {
			if !e.nativeSpeed() && speed > 0 && speed != 1 {
				if data, ext, err = speedUp(ctx, data, ext, speed); err != nil {
					return nil, "", err
				}
			}
			return data, ext, nil
		}
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		last = err
	}
	return nil, "", fmt.Errorf("the %s voice engine couldn't speak that: %w", e.name(), last)
}

// speedUp changes the speed of audio with ffmpeg (atempo takes 0.5 to 2).
func speedUp(ctx context.Context, data []byte, ext string, speed float64) ([]byte, string, error) {
	speed = min(max(speed, 0.5), 2)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-f", ext, "-i", "pipe:0",
		"-filter:a", "atempo="+strconv.FormatFloat(speed, 'f', 3, 64), "-f", "mp3", "-b:a", "96k", "pipe:1")
	cmd.Stdin = bytes.NewReader(data)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("changing the speed failed: %s", firstLine(strings.TrimSpace(stderr.String())))
	}
	return out.Bytes(), "mp3", nil
}

// toMP3 turns audio into an MP3 (a preview and a sample are MP3s the page plays).
func toMP3(ctx context.Context, data []byte, ext string) ([]byte, error) {
	if ext == "mp3" {
		return data, nil
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-f", ext, "-i", "pipe:0", "-f", "mp3", "-b:a", "96k", "pipe:1")
	cmd.Stdin = bytes.NewReader(data)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("converting the audio failed: %s", firstLine(strings.TrimSpace(stderr.String())))
	}
	return out.Bytes(), nil
}

// ---- splitting text for a voice ----

// splitForSpeech cuts text into pieces of at most max characters, at the end of
// a sentence when it can (a paragraph end first), else at a comma or space. A
// piece never joins two paragraphs, so the voice pauses where the book does.
func splitForSpeech(text string, max int) []string {
	var out []string
	for _, p := range splitSpeech(text, max) {
		out = append(out, p.Text)
	}
	return out
}

// speechPiece is a piece of text to speak. ParaEnd is set on the last piece of
// a paragraph, where the voice pauses longer.
type speechPiece struct {
	Text     string
	ParaEnd  bool
	SceneEnd bool   // the last piece before a scene break
	Speaker  string // full cast: "" the narrator, else who says it (home_readtome_cast.go)
}

// splitSpeech is splitForSpeech that says where the paragraphs end.
func splitSpeech(text string, max int) []speechPiece {
	if max < 80 {
		max = 80
	}
	var pieces []speechPiece
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if isSceneBreak(para) {
			if n := len(pieces); n > 0 {
				pieces[n-1].SceneEnd = true
			}
			continue
		}
		for _, c := range chunkText(para, max) {
			pieces = append(pieces, speechPiece{Text: c})
		}
		if n := len(pieces); n > 0 {
			pieces[n-1].ParaEnd = true
		}
	}
	return pieces
}

// chunkText cuts one paragraph (or a part of one) into pieces of at most max
// characters, whole sentences together when they fit.
func chunkText(para string, max int) []string {
	if max < 80 {
		max = 80
	}
	var out []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, sent := range sentences(para) {
		for len([]rune(sent)) > max {
			head, rest := cutAt(sent, max)
			if cur.Len() > 0 {
				flush()
			}
			out = append(out, head)
			sent = rest
		}
		if cur.Len() > 0 && len([]rune(cur.String()))+1+len([]rune(sent)) > max {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(sent)
	}
	flush()
	return out
}

// sentences splits a paragraph after . ! ? (and a closing quote or bracket)
// followed by a space, but not after a short abbreviation like "Mr." or "St.".
func sentences(p string) []string {
	var out []string
	runes := []rune(p)
	start := 0
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if c != '.' && c != '!' && c != '?' && c != '…' {
			continue
		}
		j := i + 1
		for j < len(runes) && strings.ContainsRune(`"'”’)]`, runes[j]) {
			j++
		}
		if j < len(runes) && runes[j] != ' ' {
			continue
		}
		if c == '.' && abbreviation(string(runes[start:i+1])) {
			continue
		}
		out = append(out, strings.TrimSpace(string(runes[start:j])))
		start = j
		i = j - 1
	}
	if tail := strings.TrimSpace(string(runes[start:])); tail != "" {
		out = append(out, tail)
	}
	return out
}

var abbreviations = map[string]bool{"mr.": true, "mrs.": true, "ms.": true, "dr.": true, "st.": true, "prof.": true, "sr.": true, "jr.": true, "vs.": true, "etc.": true, "no.": true, "mt.": true, "gen.": true, "col.": true, "capt.": true, "lt.": true, "sgt.": true, "rev.": true, "hon.": true, "e.g.": true, "i.e.": true}

func abbreviation(s string) bool {
	f := strings.Fields(s)
	if len(f) == 0 {
		return false
	}
	w := strings.ToLower(f[len(f)-1])
	return abbreviations[w] || (len(w) == 2 && w[1] == '.') // an initial: "J."
}

// cutAt cuts s (longer than max runes) into a head of at most max runes and the
// rest: at the last comma or semicolon in the second half, else the last space.
func cutAt(s string, max int) (string, string) {
	r := []rune(s)
	cut := -1
	for i := max; i > max/2; i-- {
		if r[i-1] == ',' || r[i-1] == ';' || r[i-1] == ':' || r[i-1] == '—' {
			cut = i
			break
		}
	}
	if cut < 0 {
		for i := max; i > 0; i-- {
			if r[i] == ' ' {
				cut = i
				break
			}
		}
	}
	if cut <= 0 {
		cut = max
	}
	return strings.TrimSpace(string(r[:cut])), strings.TrimSpace(string(r[cut:]))
}
