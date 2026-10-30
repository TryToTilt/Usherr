package api

// Usherr: more of Posterizarr's options in Re-Posterr (P2, P5, P7): the
// household's own overlay PNGs, their own pictures (sent from the browser,
// with their own words), TheTVDB as a source of art beside TMDB and
// fanart.tv, and the full Posterizarr app connected (when it makes the
// posters, Re-Posterr's style stands aside).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // a picture sent may be WebP

	"github.com/rdborg/mediarium/internal/netguard"
)

const (
	keyArtTVDB        = "home.art.tvdbKey"         // encrypted; a TheTVDB key (v4)
	keyArtPosterizarr = "home.art.posterizarr"     // the Posterizarr app's address
	keyArtPzOwns      = "home.art.posterizarrOwns" // "1": Posterizarr makes the posters; Re-Posterr's style stands aside

	artMaxUpload = 20 << 20
)

// ---- overlays ----

func (s *Server) artOverlayNames() []string {
	out := []string{}
	entries, err := os.ReadDir(filepath.Join(s.artDir(), "overlays"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if artFileRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// POST /api/home/art/overlays (admin, a form with "file"): a PNG to lay over
// pictures, ideally with see-through parts and the shape of a poster (or a
// background, for those).
func (s *Server) handleArtOverlayAdd(w http.ResponseWriter, r *http.Request) {
	raw, name, ok := artFormFile(w, r)
	if !ok {
		return
	}
	if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
		writeError(w, http.StatusBadRequest, "An overlay is a PNG picture.")
		return
	}
	name = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	name = strings.Trim(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name), "-_")
	if name == "" {
		name = "overlay"
	}
	name = trimRunes(name, 60) + ".png"
	dir := filepath.Join(s.artDir(), "overlays")
	if err := os.MkdirAll(dir, 0o755); err != nil || os.WriteFile(filepath.Join(dir, name), raw, 0o644) != nil {
		writeError(w, http.StatusInternalServerError, "The overlay couldn't be kept.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "overlays": s.artOverlayNames()})
}

// DELETE /api/home/art/overlays/{name} (admin)
func (s *Server) handleArtOverlayDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !artFileRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	_ = os.Remove(filepath.Join(s.artDir(), "overlays", name))
	writeJSON(w, http.StatusOK, map[string]any{"overlays": s.artOverlayNames()})
}

// GET /api/home/art/overlays/{name} (admin): to show it.
func (s *Server) handleArtOverlayGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !artFileRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	http.ServeFile(w, r, filepath.Join(s.artDir(), "overlays", name))
}

// artFormFile reads the "file" of a form sent, 20 MB at most.
func artFormFile(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, artMaxUpload+1<<20)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Send a picture (20 MB at most).")
		return nil, "", false
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, artMaxUpload+1))
	if err != nil || len(raw) > artMaxUpload {
		writeError(w, http.StatusBadRequest, "That picture is over 20 MB.")
		return nil, "", false
	}
	return raw, hdr.Filename, true
}

// ---- the household's own pictures ----

func artUploadName(n string) bool {
	if len(n) != 20 || !strings.HasSuffix(n, ".jpg") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimSuffix(n, ".jpg"))
	return err == nil
}

// artUpload reads a picture the household sent.
func (s *Server) artUpload(name string) (image.Image, error) {
	if !artUploadName(name) {
		return nil, errors.New("that picture isn't one sent here")
	}
	b, err := os.ReadFile(filepath.Join(s.artDir(), "uploads", name))
	if err != nil {
		return nil, errors.New("the picture sent isn't there any more")
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	return img, err
}

// POST /api/home/art/upload (admin, a form with "file"): a picture of the
// household's own to make a poster or background from. It's kept (as a
// JPEG, 3000 pixels at most) in the artwork folder's uploads.
func (s *Server) handleArtUpload(w http.ResponseWriter, r *http.Request) {
	raw, _, ok := artFormFile(w, r)
	if !ok {
		return
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "That picture couldn't be read (JPEG, PNG or WebP).")
		return
	}
	if b := img.Bounds(); b.Dx() > 3000 || b.Dy() > 3000 {
		sc := 3000 / float64(max(b.Dx(), b.Dy()))
		dst := image.NewRGBA(image.Rect(0, 0, max(int(float64(b.Dx())*sc), 1), max(int(float64(b.Dy())*sc), 1)))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	data, err := artJPEG(img, 92)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The picture couldn't be kept.")
		return
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:8]) + ".jpg"
	dir := filepath.Join(s.artDir(), "uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil || os.WriteFile(filepath.Join(dir, name), data, 0o644) != nil {
		writeError(w, http.StatusInternalServerError, "The picture couldn't be kept.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": "upload:" + name, "preview": "/api/home/art/uploads/" + name})
}

// GET /api/home/art/uploads/{name} (admin)
func (s *Server) handleArtUploadGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !artUploadName(name) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.artDir(), "uploads", name))
}

// ---- TheTVDB ----

var errNoTVDB = errors.New("no TheTVDB key")

// The TheTVDB token, kept the day it lasts (a new key, a new token).
var tvdbToken struct {
	mu    sync.Mutex
	key   string
	token string
	at    time.Time
}

var tvdbBase = "https://api4.thetvdb.com/v4"

func (s *Server) tvdbAuth(ctx context.Context) (string, error) {
	key, _ := s.Settings.Get(keyArtTVDB)
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errNoTVDB
	}
	tvdbToken.mu.Lock()
	defer tvdbToken.mu.Unlock()
	if tvdbToken.key == key && time.Since(tvdbToken.at) < 20*time.Hour {
		return tvdbToken.token, nil
	}
	body, _ := json.Marshal(map[string]string{"apikey": key})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tvdbBase+"/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := artHTTP().Do(req)
	if err != nil {
		return "", fmt.Errorf("TheTVDB: %w", netguard.CleanError(err))
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out) != nil || out.Data.Token == "" {
		return "", fmt.Errorf("TheTVDB didn't take the key (status %d)", resp.StatusCode)
	}
	tvdbToken.key, tvdbToken.token, tvdbToken.at = key, out.Data.Token, time.Now()
	return out.Data.Token, nil
}

// TheTVDB's languages are three letters; the ones art comes in most.
var tvdbLangs = map[string]string{"eng": "en", "deu": "de", "fra": "fr", "spa": "es", "ita": "it", "nld": "nl", "por": "pt", "pol": "pl", "rus": "ru", "jpn": "ja", "kor": "ko", "zho": "zh", "swe": "sv", "dan": "da", "nor": "no", "fin": "fi", "ces": "cs", "hun": "hu", "tur": "tr", "heb": "he", "ara": "ar"}

var tvdbAnswers = newTTLBytes(300, 30*time.Minute)

// tvdbImages is a show's posters, backgrounds and logos on TheTVDB.
func (s *Server) tvdbImages(ctx context.Context, tvdbID int) (map[string][]artCand, error) {
	if tvdbID <= 0 {
		return nil, nil
	}
	path := "/series/" + strconv.Itoa(tvdbID) + "/artworks"
	raw, ok := tvdbAnswers.get(path)
	if !ok {
		token, err := s.tvdbAuth(ctx)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, tvdbBase+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := artHTTP().Do(req)
		if err != nil {
			return nil, fmt.Errorf("TheTVDB: %w", netguard.CleanError(err))
		}
		raw, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusNotFound {
			raw = []byte(`{}`)
		} else if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("TheTVDB answered %d", resp.StatusCode)
		}
		tvdbAnswers.put(path, raw)
	}
	var out struct {
		Data struct {
			Artworks []struct {
				Image     string  `json:"image"`
				Thumbnail string  `json:"thumbnail"`
				Language  *string `json:"language"`
				Type      int     `json:"type"`
				Width     int     `json:"width"`
				Height    int     `json:"height"`
				Score     float64 `json:"score"`
			} `json:"artworks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	types := map[int]string{2: "poster", 3: "backdrop", 23: "logo"}
	got := map[string][]artCand{}
	for _, a := range out.Data.Artworks {
		typ := types[a.Type]
		if typ == "" || !strings.HasPrefix(a.Image, "https://artworks.thetvdb.com/") {
			continue
		}
		lang := ""
		if a.Language != nil && *a.Language != "" {
			if l, ok := tvdbLangs[*a.Language]; ok {
				lang = l
			} else {
				lang = *a.Language
			}
		}
		prev := a.Thumbnail
		if !strings.HasPrefix(prev, "https://artworks.thetvdb.com/") {
			prev = a.Image
		}
		// TheTVDB's scores run to the hundreds of thousands: a little lift, no more.
		got[typ] = append(got[typ], artCand{Type: typ, Source: "tvdb", URL: a.Image, Preview: prev, Width: a.Width, Height: a.Height, Lang: lang, Score: min(a.Score/100000, 2)})
	}
	return got, nil
}

// POST /api/home/art/test-tvdb (admin): asks TheTVDB for a well-known show's
// art with the key.
func (s *Server) handleArtTestTVDB(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	tvdbAnswers.clear()
	got, err := s.tvdbImages(ctx, 81189) // Breaking Bad
	if errors.Is(err, errNoTVDB) {
		writeError(w, http.StatusBadRequest, "Put in a TheTVDB key first.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	n := 0
	for _, l := range got {
		n += len(l)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pictures": n})
}

// ---- the full Posterizarr app ----

// POST /api/home/art/test-posterizarr (admin): is Posterizarr there?
func (s *Server) handleArtTestPosterizarr(w http.ResponseWriter, r *http.Request) {
	addr, _ := s.Settings.Get(keyArtPosterizarr)
	addr = strings.TrimRight(strings.TrimSpace(addr), "/")
	if addr == "" {
		writeError(w, http.StatusBadRequest, "Put in Posterizarr's address first.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/", nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That address isn't right.")
		return
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Posterizarr didn't answer: "+netguard.CleanError(err).Error())
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Posterizarr answered %d.", resp.StatusCode))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
