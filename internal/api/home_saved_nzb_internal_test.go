package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/download"
)

const savedTestNZB = `<?xml version="1.0" encoding="utf-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
 <file poster="p" date="1" subject="[1/1] &quot;Arrival.2016.1080p.mkv&quot; yEnc (1/1)">
  <groups><group>alt.binaries.test</group></groups>
  <segments><segment bytes="1000" number="1">seg1@example</segment></segments>
 </file>
</nzb>`

// savedTestServer has a streamed film (10) and an episode (12) with saved
// NZBs, and stand-ins for the Usenet servers: the check answers check(), and
// a release opens as 1000 bytes. It counts the checks and the openings.
func savedTestServer(t *testing.T, check func() download.PrecheckResult) (*Server, *atomic.Int32, *atomic.Int32, []byte) {
	t.Helper()
	s, db := optionsServer(t)
	s.db = db
	s.cfg.ConfigDir = t.TempDir()
	s.ClientRepo = download.NewRepo(db, nil)
	for _, q := range []string{
		`INSERT INTO streamed_titles (id, kind, tmdb_id, imdb_id, title, year) VALUES (1, 'movie', 329865, 'tt2543164', 'Arrival', 2016), (3, 'series', 95396, 'tt11280740', 'Severance', 2022)`,
		`INSERT INTO streamed_files (id, title_id, release, nzb, inner_file, size) VALUES (10, 1, 'Arrival.2016.1080p.WEB-DL', 'drive-10.nzb', 'Arrival.mkv', 1000)`,
		`INSERT INTO streamed_files (id, title_id, season, episode, release, nzb, inner_file, size) VALUES (12, 3, 1, 1, 'Severance.S01E01.2160p', 'drive-12.nzb', 'e.mkv', 1000)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	dir := s.streamedNZBDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"drive-10.nzb", "drive-12.nzb"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(savedTestNZB), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i % 251)
	}
	var checks, opens atomic.Int32
	oldServers, oldCheck, oldOpen := savedServers, savedCheck, savedOpen
	savedServers = func(*Server) ([]download.ClientConfig, error) {
		return []download.ClientConfig{{Host: "primary.invalid", Connections: 2}, {Host: "backup.invalid", Connections: 1}}, nil
	}
	savedCheck = func(context.Context, []download.ClientConfig, *download.NZB) download.PrecheckResult {
		checks.Add(1)
		return check()
	}
	savedOpen = func(_ *Server, _ context.Context, id int64) (*download.StreamFile, error) {
		opens.Add(1)
		return download.StreamFileFrom("Arrival.mkv", bytes.NewReader(data), int64(len(data))), nil
	}
	t.Cleanup(func() {
		savedServers, savedCheck, savedOpen = oldServers, oldCheck, oldOpen
		savedMu.Lock()
		savedTokens = map[int64]string{}
		savedMu.Unlock()
		streamedPlaysMu.Lock()
		streamedPlays = map[playKey]*streamedPlay{}
		streamedPlaysMu.Unlock()
		driveTreeChanged()
	})
	return s, &checks, &opens, data
}

func allFound() download.PrecheckResult { return download.PrecheckResult{Total: 1, Checked: 1} }

func healthOf(t *testing.T, s *Server, id int64) string {
	t.Helper()
	var h string
	if err := s.db.QueryRow(`SELECT health FROM streamed_files WHERE id = ?`, id).Scan(&h); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestSavedHealthChecksOnceAndRemembers(t *testing.T) {
	s, checks, _, _ := savedTestServer(t, allFound)
	ctx := context.Background()
	f, _ := s.streamedFile(ctx, 10)
	if h, err := s.savedHealth(ctx, f); err != nil || h != "ok" {
		t.Fatalf("first check: %q %v", h, err)
	}
	if healthOf(t, s, 10) != "ok" {
		t.Fatal("the check wasn't kept")
	}
	f, _ = s.streamedFile(ctx, 10)
	if h, err := s.savedHealth(ctx, f); err != nil || h != "ok" || checks.Load() != 1 {
		t.Fatalf("second look: %q %v, %d checks (want 1)", h, err, checks.Load())
	}
	// An old check is made again.
	_, _ = s.db.Exec(`UPDATE streamed_files SET health_at = ? WHERE id = 10`, time.Now().Add(-13*time.Hour).UTC().Format(time.RFC3339))
	f, _ = s.streamedFile(ctx, 10)
	if _, err := s.savedHealth(ctx, f); err != nil || checks.Load() != 2 {
		t.Fatalf("an old check: %v, %d checks (want 2)", err, checks.Load())
	}
}

func TestSavedHealthGoneIsDead(t *testing.T) {
	s, checks, opens, _ := savedTestServer(t, func() download.PrecheckResult {
		return download.PrecheckResult{Total: 100, Checked: 10, Missing: 5, EstimatedMissingBytes: 500}
	})
	ctx := context.Background()
	f, _ := s.streamedFile(ctx, 10)
	if _, err := s.savedHealth(ctx, f); !errors.Is(err, errSavedDead) {
		t.Fatalf("want dead, got %v", err)
	}
	if healthOf(t, s, 10) != "dead" {
		t.Fatal("a dead release isn't marked")
	}
	f, _ = s.streamedFile(ctx, 10)
	if _, _, err := s.openSavedSession(ctx, f); !errors.Is(err, errSavedDead) || opens.Load() != 0 || checks.Load() != 1 {
		t.Fatalf("a dead release opened (%v, %d opens, %d checks)", err, opens.Load(), checks.Load())
	}
}

func TestSavedHealthUncheckedStillPlays(t *testing.T) {
	s, _, _, _ := savedTestServer(t, func() download.PrecheckResult {
		return download.PrecheckResult{Skipped: true, SkipReason: "no STAT"}
	})
	ctx := context.Background()
	f, _ := s.streamedFile(ctx, 10)
	if h, err := s.savedHealth(ctx, f); err != nil || h != "" {
		t.Fatalf("a check nobody could answer: %q %v", h, err)
	}
	if healthOf(t, s, 10) != "" {
		t.Fatal("an unanswered check changed the health")
	}
	if _, _, err := s.openSavedSession(ctx, f); err != nil {
		t.Fatalf("it should still play: %v", err)
	}
}

func TestSavedSessionOpensOnceAndServesRanges(t *testing.T) {
	s, _, opens, data := savedTestServer(t, allFound)
	ctx := context.Background()
	f, _ := s.streamedFile(ctx, 10)
	tok, file, err := s.openSavedSession(ctx, f)
	if err != nil || file.Size != 1000 {
		t.Fatalf("open: %v", err)
	}
	if tok2, _, err := s.openSavedSession(ctx, f); err != nil || tok2 != tok || opens.Load() != 1 {
		t.Fatalf("opened again: %v %q %q, %d opens", err, tok, tok2, opens.Load())
	}
	defer ustreamClose(tok)
	req := httptest.NewRequest(http.MethodGet, "/api/ustream/"+tok+"/Arrival.mkv", nil)
	req.Header.Set("Range", "bytes=100-199")
	rec := httptest.NewRecorder()
	s.serveUStream(rec, req, tok)
	if rec.Code != http.StatusPartialContent || !bytes.Equal(rec.Body.Bytes(), data[100:200]) {
		t.Fatalf("range: %d, %d bytes", rec.Code, rec.Body.Len())
	}
}

// With Usherr's engine chosen, a streamed file plays its saved release: no
// search, no StreamNZB.
func TestStreamedPlaysTheSavedReleaseFirst(t *testing.T) {
	s, _, opens, _ := savedTestServer(t, allFound)
	_ = s.Settings.Set(keyStreamEngine, engineUsherr, false)
	p, err := s.streamedSourceFor(context.Background(), 10, "", false)
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if p.release != "Arrival.2016.1080p.WEB-DL" || !strings.Contains(p.target, "/api/ustream/") || opens.Load() != 1 {
		t.Fatalf("played %+v (%d opens)", p, opens.Load())
	}
	// A 720p play doesn't start with a 1080p release, but when nothing else
	// is found it is still the last resort (and the open one is reused).
	p, err = s.streamedSourceFor(context.Background(), 10, "720p", false)
	if err != nil || p.release != "Arrival.2016.1080p.WEB-DL" || opens.Load() != 1 {
		t.Fatalf("last resort: %v %+v (%d opens)", err, p, opens.Load())
	}
}

func TestSavedSuits(t *testing.T) {
	f := streamedFileRow{nzb: "a.nzb", release: "X.2016.2160p.WEB", size: 30 << 30}
	if !savedSuits(f, "", streamProfile{}) {
		t.Error("the best there is: anything saved suits")
	}
	if savedSuits(f, "1080p", streamProfile{Quality: "1080p"}) {
		t.Error("4K suits a 1080p profile")
	}
	if !savedSuits(f, "4K", streamProfile{Quality: "4K", MaxSizeGB: 90}) {
		t.Error("4K doesn't suit a 4K profile")
	}
	f.health = "dead"
	if savedSuits(f, "", streamProfile{}) {
		t.Error("a dead release suits")
	}
}

func TestAddonOffersCheckedSavedReleases(t *testing.T) {
	s, _, _, _ := savedTestServer(t, allFound)
	ctx := context.Background()
	got := s.addonSavedStreams(ctx, addonRef{Kind: "series", IMDB: "tt11280740", Season: 1, Episode: 1}, 95396, "http://u", "k")
	if len(got) != 1 || got[0].URL != "http://u/addon/k/saved/12" || !strings.Contains(got[0].Name, "4K") || !strings.Contains(got[0].Title, "articles checked") {
		t.Fatalf("episode: %+v", got)
	}
	if got := s.addonSavedStreams(ctx, addonRef{Kind: "series", Season: 1, Episode: 2}, 95396, "http://u", "k"); len(got) != 0 {
		t.Fatalf("another episode: %+v", got)
	}
	_, _ = s.db.Exec(`UPDATE streamed_files SET health = 'dead' WHERE id = 10`)
	if got := s.addonSavedStreams(ctx, addonRef{Kind: "movie"}, 329865, "http://u", "k"); len(got) != 0 {
		t.Fatalf("a dead release offered: %+v", got)
	}
}
