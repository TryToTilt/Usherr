package api

// Home fork: the panels of a comic page, for the reader's guided view.
// The AI panel detector (internal/panelai: a YOLOv8-nano model and a
// reading-order model, run in pure Go) does the work; its two model files
// (12 MB) are downloaded once into the config folder and checked against
// fixed fingerprints. When they can't be had, the gutter method
// (comics.DetectPanels) stands in. Each page is worked out once and kept in
// comic_panels; the reader asks a few pages ahead, so it's ready in time.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // comic pages
	_ "image/jpeg" // comic pages
	_ "image/png"  // comic pages
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/panelai"
)

// The Inkwell panel models (huggingface.co/cedarrapidsboy/inkwell-panel-models):
// the detector is AGPL-3.0 (as Mediarium is), the reading-order model MIT.
var panelModelFiles = []struct{ name, sha256 string }{
	{"best.onnx", "f240e1296efd048126b26ea4ffeddc97d7c3aa667a37e3127c2afc6d5e9b9578"},
	{"panel-order-model.onnx", "84eb78fc5ec31a3e986a7a16ce10f7baa657ef5c5b4a98877b40aa9354ff3464"},
}

const panelModelURL = "https://huggingface.co/cedarrapidsboy/inkwell-panel-models/resolve/main/"

var (
	panelAIMu     sync.Mutex
	panelAI       *panelai.Detector
	panelAIFailed time.Time  // when loading last failed; tried again after a while
	panelWorkMu   sync.Mutex // one page at a time (each already uses every core)
)

func (s *Server) panelModelDir() string {
	return filepath.Join(s.cfg.ConfigDir, "models", "inkwell-panels")
}

// panelDetector returns the AI detector, downloading and loading the models
// the first time. It returns nil when they can't be had (tried again after
// ten minutes), and the gutter method is used meanwhile.
func (s *Server) panelDetector(ctx context.Context) *panelai.Detector {
	panelAIMu.Lock()
	defer panelAIMu.Unlock()
	if panelAI != nil {
		return panelAI
	}
	if time.Since(panelAIFailed) < 10*time.Minute {
		return nil
	}
	d, err := s.loadPanelModels(ctx)
	if err != nil {
		slog.Warn("comics: the AI panel models aren't available; using the gutter method", "err", err)
		panelAIFailed = time.Now()
		return nil
	}
	panelAI = d
	return d
}

func (s *Server) loadPanelModels(ctx context.Context) (*panelai.Detector, error) {
	dir := s.panelModelDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var data [][]byte
	for _, f := range panelModelFiles {
		path := filepath.Join(dir, f.name)
		b, err := os.ReadFile(path)
		if err != nil || sha256Hex(b) != f.sha256 {
			if b, err = downloadPanelModel(ctx, f.name, f.sha256); err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, b, 0o644); err != nil {
				return nil, err
			}
			slog.Info("comics: downloaded an AI panel model", "file", f.name, "bytes", len(b))
		}
		data = append(data, b)
	}
	return panelai.NewDetector(data[0], data[1])
}

func downloadPanelModel(ctx context.Context, name, want string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, panelModelURL+name, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: the server answered %d", name, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if sha256Hex(b) != want {
		return nil, fmt.Errorf("download %s: the file isn't the expected one", name)
	}
	return b, nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// pagePanels is a page's panels in reading order (none: show the page whole).
type pagePanels struct {
	Panels []panelai.Panel `json:"panels"`
	Method string          `json:"method"` // "ai" or "gutters"
}

// comicPagePanels returns page n's panels, working them out when they
// aren't kept yet.
func (s *Server) comicPagePanels(ctx context.Context, is comics.Issue, sr comics.Series, n int) (pagePanels, error) {
	var res pagePanels
	info, err := os.Stat(is.FilePath)
	if err != nil {
		return res, err
	}
	var stored string
	err = s.db.QueryRowContext(ctx, `SELECT method, panels FROM comic_panels WHERE issue_id = ? AND page = ? AND file_size = ?`, is.ID, n, info.Size()).Scan(&res.Method, &stored)
	if err == nil && json.Unmarshal([]byte(stored), &res.Panels) == nil {
		return res, nil
	}
	pages, err := s.comicPages(is.ID, is.FilePath)
	if err != nil {
		return res, err
	}
	if n < 1 || n > len(pages) {
		return res, errors.New("no such page")
	}
	panelWorkMu.Lock()
	defer panelWorkMu.Unlock()
	// Another request may have worked it out meanwhile.
	if err := s.db.QueryRowContext(ctx, `SELECT method, panels FROM comic_panels WHERE issue_id = ? AND page = ? AND file_size = ?`, is.ID, n, info.Size()).Scan(&res.Method, &stored); err == nil {
		if json.Unmarshal([]byte(stored), &res.Panels) == nil {
			return res, nil
		}
	}
	res = detectPagePanels(s.panelDetector(ctx), pages[n-1], comics.IsManga(sr))
	b, _ := json.Marshal(res.Panels)
	_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO comic_panels (issue_id, page, file_size, method, panels) VALUES (?, ?, ?, ?, ?)`, is.ID, n, info.Size(), res.Method, string(b))
	return res, nil
}

// detectPagePanels works out one page file's panels. A page that can't be
// read (an image format Go doesn't decode) has none: it's shown whole.
func detectPagePanels(ai *panelai.Detector, file string, manga bool) pagePanels {
	res := pagePanels{Panels: []panelai.Panel{}, Method: "gutters"}
	f, err := os.Open(file)
	if err != nil {
		return res
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return res
	}
	if ai != nil {
		if ps, err := ai.Panels(img, manga); err == nil {
			res.Method = "ai"
			if len(ps) > 1 { // one panel is the whole page: nothing to guide through
				res.Panels = ps
			}
			return res
		}
	}
	for _, p := range comics.DetectPanels(img, comics.PanelOptions{RightToLeft: manga}) {
		res.Panels = append(res.Panels, panelai.Panel{X: p.X, Y: p.Y, W: p.W, H: p.H})
	}
	return res
}

// GET /api/comics/issues/{id}/pages/{n}/panels: page n's panels in reading
// order, as fractions of the page. The next pages are worked out in the
// background, so turning the page finds them ready.
func (s *Server) handleComicPagePanels(w http.ResponseWriter, r *http.Request) {
	is, sr, ok := s.comicIssueFromPath(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 || is.FilePath == "" {
		writeError(w, http.StatusNotFound, "No such page.")
		return
	}
	res, err := s.comicPagePanels(r.Context(), is, sr, n)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such page.")
		return
	}
	s.background(func() {
		for k := n + 1; k <= n+3; k++ {
			if _, err := s.comicPagePanels(context.Background(), is, sr, k); err != nil {
				return
			}
		}
	})
	w.Header().Set("Cache-Control", "private, max-age=3600")
	writeJSON(w, http.StatusOK, res)
}
