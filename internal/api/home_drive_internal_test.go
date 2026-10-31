package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/crypto"
	"github.com/rdborg/mediarium/internal/settings"
)

func TestDriveFileName(t *testing.T) {
	for _, c := range []struct {
		kind                   string
		anime                  bool
		title                  string
		year, tmdb, season, ep int
		inner, want            string
	}{
		{"movie", false, "Arrival", 2016, 329865, 0, 0, "Arrival.2016.1080p.mkv", "/Movies/Arrival (2016) [tmdbid-329865]/Arrival (2016).mkv"},
		{"series", false, "Severance", 2022, 95396, 1, 2, "x.MP4", "/TV/Severance (2022) [tmdbid-95396]/Season 01/Severance S01E02.mp4"},
		{"series", true, "Frieren", 2023, 209867, 1, 1, "scrambled", "/Anime/Frieren (2023) [tmdbid-209867]/Season 01/Frieren S01E01.mkv"},
	} {
		if got := driveFileName(c.kind, c.anime, c.title, c.year, c.tmdb, c.season, c.ep, c.inner); got != c.want {
			t.Errorf("%s: got %q, want %q", c.title, got, c.want)
		}
	}
}

// driveTestServer has the drive on, two films (one ready, one not) and a
// dead episode, and reads every file from a stand-in of 1000 bytes.
func driveTestServer(t *testing.T) (*Server, *httptest.Server, []byte) {
	t.Helper()
	s, db := optionsServer(t)
	s.db = db
	box, err := crypto.LoadOrCreateKey(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	s.Settings = settings.New(db, box) // the drive's password is kept encrypted
	driveTreeChanged()
	for _, q := range []string{
		`INSERT INTO streamed_titles (id, kind, tmdb_id, title, year) VALUES (1, 'movie', 329865, 'Arrival', 2016), (2, 'movie', 11, 'Not Ready', 1999), (3, 'series', 95396, 'Severance', 2022)`,
		`INSERT INTO streamed_files (id, title_id, nzb, inner_file, size, health, health_at) VALUES (10, 1, 'drive-10.nzb', 'Arrival.mkv', 1000, 'ok', '2026-10-31T05:00:00Z')`,
		`INSERT INTO streamed_files (id, title_id) VALUES (11, 2)`,
		`INSERT INTO streamed_files (id, title_id, season, episode, nzb, inner_file, size, health) VALUES (12, 3, 1, 1, 'drive-12.nzb', 'e.mkv', 500, 'dead')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.Settings.Set(keyDriveOn, "1", false)
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i % 251)
	}
	old := driveReader
	driveReader = func(_ *Server, _ context.Context, id int64) (io.ReaderAt, error) { return bytes.NewReader(data), nil }
	t.Cleanup(func() { driveReader = old; driveTreeChanged() })
	ts := httptest.NewServer(s.Routes()) // the whole app, so the drive's address is checked as it really is
	t.Cleanup(ts.Close)
	return s, ts, data
}

func driveDo(t *testing.T, s *Server, ts *httptest.Server, method, path, pass string, h map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, nil)
	if pass == "" {
		pass = s.drivePassword()
	}
	req.SetBasicAuth(driveUser, pass)
	for k, v := range h {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestDriveListsOnlyReadyFiles(t *testing.T) {
	s, ts, _ := driveTestServer(t)
	resp, body := driveDo(t, s, ts, "PROPFIND", "/dav/Movies/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("PROPFIND: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Arrival (2016) [tmdbid-329865]") {
		t.Errorf("the ready film isn't listed: %s", body)
	}
	if strings.Contains(body, "Not Ready") {
		t.Errorf("a film with no release kept is listed")
	}
	resp, body = driveDo(t, s, ts, "PROPFIND", "/dav/TV/", "", map[string]string{"Depth": "1"})
	if resp.StatusCode != http.StatusMultiStatus || strings.Contains(body, "Severance") {
		t.Errorf("TV should be there, empty: a dead episode is listed (status %d)", resp.StatusCode)
	}
	resp, body = driveDo(t, s, ts, "PROPFIND", "/dav/", "", map[string]string{"Depth": "1"})
	for _, top := range []string{"/dav/Movies/", "/dav/TV/", "/dav/Anime/"} {
		if resp.StatusCode != http.StatusMultiStatus || !strings.Contains(body, top) {
			t.Errorf("%s isn't at the top of the drive (status %d)", top, resp.StatusCode)
		}
	}
	resp, body = driveDo(t, s, ts, "PROPFIND", "/dav/Movies/Arrival%20(2016)%20%5Btmdbid-329865%5D/Arrival%20(2016).mkv", "", map[string]string{"Depth": "0"})
	if resp.StatusCode != http.StatusMultiStatus || !strings.Contains(body, "<D:getcontentlength>1000</D:getcontentlength>") {
		t.Errorf("the file's size: %d %s", resp.StatusCode, body)
	}
}

func TestDriveReadsARange(t *testing.T) {
	s, ts, data := driveTestServer(t)
	resp, body := driveDo(t, s, ts, http.MethodGet, "/dav/Movies/Arrival%20(2016)%20%5Btmdbid-329865%5D/Arrival%20(2016).mkv", "", map[string]string{"Range": "bytes=100-199"})
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("GET with a range: %d", resp.StatusCode)
	}
	if body != string(data[100:200]) {
		t.Errorf("wrong bytes for the range")
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/x-matroska" {
		t.Errorf("Content-Type %q", ct)
	}
	resp, body = driveDo(t, s, ts, http.MethodGet, "/dav/Movies/Arrival%20(2016)%20%5Btmdbid-329865%5D/Arrival%20(2016).mkv", "", nil)
	if resp.StatusCode != http.StatusOK || body != string(data) {
		t.Errorf("the whole file: %d, %d bytes", resp.StatusCode, len(body))
	}
}

func TestDriveSignInAndReadOnly(t *testing.T) {
	s, ts, _ := driveTestServer(t)
	if resp, _ := driveDo(t, s, ts, "PROPFIND", "/dav/", "wrong", map[string]string{"Depth": "0"}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a wrong password: %d", resp.StatusCode)
	}
	for _, m := range []string{http.MethodPut, http.MethodDelete, "MKCOL", "MOVE", "COPY", "PROPPATCH"} {
		if resp, _ := driveDo(t, s, ts, m, "/dav/Movies/x.mkv", "", nil); resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d, want 405", m, resp.StatusCode)
		}
	}
	_ = s.Settings.Set(keyDriveOn, "0", false)
	if resp, _ := driveDo(t, s, ts, "PROPFIND", "/dav/", "", map[string]string{"Depth": "0"}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("the drive switched off still answers: %d", resp.StatusCode)
	}
}

func TestDriveSkipsFilmsNotOutYet(t *testing.T) {
	s, _, _ := driveTestServer(t)
	if _, err := s.db.Exec(`INSERT INTO streamed_titles (id, kind, tmdb_id, title, year, added_at) VALUES (4, 'movie', 99, 'Next Year', ?, '2999-01-01T00:00:00Z')`, time.Now().Year()+1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO streamed_files (id, title_id) VALUES (13, 4)`); err != nil {
		t.Fatal(err)
	}
	id, err := s.driveNext(context.Background())
	if err != nil || id != 11 {
		t.Errorf("next: %d, %v; want the film that's out (11), not next year's", id, err)
	}
}

func TestDriveServerFolder(t *testing.T) {
	for _, c := range []struct{ where, rel, want string }{
		{"U:", "/Movies/Nobody 2 (2025) [tmdbid-1]/Nobody 2 (2025).mkv", `U:\Movies\Nobody 2 (2025) [tmdbid-1]`},
		{`U:\`, "/TV/Severance (2022) [tmdbid-2]/Season 01/Severance S01E01.mkv", `U:\TV\Severance (2022) [tmdbid-2]`},
		{"/mnt/usherr/", "/Anime/Frieren (2023) [tmdbid-3]/Season 01/Frieren S01E01.mkv", "/mnt/usherr/Anime/Frieren (2023) [tmdbid-3]"},
	} {
		if got := driveServerFolder(c.where, c.rel); got != c.want {
			t.Errorf("driveServerFolder(%q, %q) = %q, want %q", c.where, c.rel, got, c.want)
		}
	}
}

func TestDriveBudget(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	_ = s.Settings.Set(keyDriveBudget, "2", false)
	if !s.driveSpend() || !s.driveSpend() {
		t.Fatal("the first two searches should be allowed")
	}
	if s.driveSpend() {
		t.Error("a third search went past the budget of 2")
	}
	if s.driveUsedToday() != 2 {
		t.Errorf("used today: %d", s.driveUsedToday())
	}
}

func TestDriveOnByDefaultOnlyWithPlex(t *testing.T) {
	s, db := optionsServer(t)
	s.db = db
	if s.driveOn() {
		t.Error("on with no media server")
	}
	_ = s.Settings.Set(keyDriveOn, "1", false)
	if !s.driveOn() {
		t.Error("switched on, but off")
	}
}

// TestDriveServeForRclone serves the drive with a real video file, for trying
// it with rclone by hand: DRIVE_SERVE=path/to/video.mkv go test -run
// TestDriveServeForRclone (it serves on :8099 until stopped).
func TestDriveServeForRclone(t *testing.T) {
	video := os.Getenv("DRIVE_SERVE")
	if video == "" {
		t.Skip("set DRIVE_SERVE to a video file")
	}
	s, _, _ := driveTestServer(t)
	data, err := os.ReadFile(video)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE streamed_files SET size = ? WHERE id = 10`, len(data)); err != nil {
		t.Fatal(err)
	}
	driveTreeChanged()
	driveReader = func(_ *Server, _ context.Context, id int64) (io.ReaderAt, error) { return bytes.NewReader(data), nil }
	_ = os.WriteFile(os.Getenv("DRIVE_PASS_OUT"), []byte(s.drivePassword()), 0o600)
	t.Log("serving the drive on :8099")
	_ = http.ListenAndServe(":8099", s.Routes())
}
