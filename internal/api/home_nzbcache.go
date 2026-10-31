package api

// Usherr: NZB files are fetched from an indexer once. Indexers (NZBFinder
// warns, then closes the account) count every fetch of the same NZB; the
// streaming engine used to fetch it again each time a release was opened —
// a channel tuning the same episode, a game watched again, a release that
// had failed before. Each NZB is kept on disk for a week, and a release
// whose articles are gone is passed over for a day without being fetched.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
)

const (
	nzbKeep      = 7 * 24 * time.Hour
	nzbBadKeep   = 24 * time.Hour
	nzbCacheSize = 512 << 20 // the folder's most; the oldest go first
)

var nzbCacheMu sync.Mutex

func (s *Server) nzbCacheDir() string {
	if s.cfg.ConfigDir == "" {
		return ""
	}
	return filepath.Join(s.cfg.ConfigDir, "cache", "nzb")
}

// nzbKey names a release's NZB by its link without the host (the same
// Prowlarr link reached by two names is one NZB).
func nzbKey(link string) string {
	k := link
	if u, err := url.Parse(link); err == nil && u.Host != "" {
		k = u.Path + "?" + u.RawQuery
	}
	sum := sha256.Sum256([]byte(k))
	return hex.EncodeToString(sum[:16])
}

// releaseKey names a release itself, whatever link it came with: its
// indexer, name and exact size. Prowlarr hands out a new link for the same
// release at every search, so a link alone let the same NZB be fetched again
// and again (NZBFinder counts those, and warned). "" for a result without a
// name or size.
func releaseKey(r indexers.Result) string {
	if strings.TrimSpace(r.Title) == "" || r.SizeBytes <= 0 {
		return ""
	}
	return "release|" + strings.ToLower(strings.TrimSpace(r.IndexerName)) + "|" + strings.ToLower(strings.TrimSpace(r.Title)) + "|" + strconv.FormatInt(r.SizeBytes, 10)
}

// nzbFetching makes the parts of Usherr asking for the same release at once
// (a channel and the player, the drive) wait for one fetch.
var nzbFetching sync.Map // release key -> *sync.Mutex

// fetchReleaseOnce is a search result's NZB: kept from earlier (by the
// release, or by its link), or fetched once and kept.
func (s *Server) fetchReleaseOnce(ctx context.Context, r indexers.Result) ([]byte, error) {
	key := releaseKey(r)
	if key == "" {
		return s.fetchRelease(ctx, r.DownloadURL)
	}
	mu, _ := nzbFetching.LoadOrStore(key, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	if b, ok := s.cachedNZBKey(key); ok {
		return b, nil
	}
	b, err := s.fetchRelease(ctx, r.DownloadURL)
	if err == nil {
		s.keepNZBKey(key, b)
	}
	return b, err
}

// cachedNZB is the NZB kept for a link, if it is there and recent.
func (s *Server) cachedNZB(link string) ([]byte, bool) { return s.cachedNZBKey(link) }

// cachedNZBKey is the NZB kept under a key (a link or a release), if it is
// there and recent.
func (s *Server) cachedNZBKey(key string) ([]byte, bool) {
	dir := s.nzbCacheDir()
	if dir == "" {
		return nil, false
	}
	p := filepath.Join(dir, nzbKey(key)+".nzb")
	fi, err := os.Stat(p)
	if err != nil || time.Since(fi.ModTime()) > nzbKeep {
		return nil, false
	}
	b, err := os.ReadFile(p)
	return b, err == nil && len(b) > 0
}

// keepNZB saves an NZB fetched for a link (only what looks like one).
func (s *Server) keepNZB(link string, data []byte) { s.keepNZBKey(link, data) }

// keepNZBKey saves an NZB under a key: a link, or a release.
func (s *Server) keepNZBKey(link string, data []byte) {
	dir := s.nzbCacheDir()
	if dir == "" || link == "" || len(data) == 0 || notAnNZB(data, link) != nil {
		return
	}
	nzbCacheMu.Lock()
	defer nzbCacheMu.Unlock()
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	tmp := filepath.Join(dir, nzbKey(link)+".tmp")
	if os.WriteFile(tmp, data, 0o644) != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(dir, nzbKey(link)+".nzb"))
	pruneNZBCache(dir)
}

// pruneNZBCache drops what is older than a week, and the oldest while the
// folder is over its size.
func pruneNZBCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		at   time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil || fi.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		keep := nzbKeep
		if strings.HasSuffix(e.Name(), ".bad") {
			keep = nzbBadKeep
		}
		if time.Since(fi.ModTime()) > keep {
			_ = os.Remove(p)
			continue
		}
		files = append(files, file{p, fi.Size(), fi.ModTime()})
		total += fi.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	for _, f := range files {
		if total <= nzbCacheSize {
			break
		}
		_ = os.Remove(f.path)
		total -= f.size
	}
}

// markNZBBad remembers that a release's articles are gone (a day).
func (s *Server) markNZBBad(link string) { s.markNZBBadKey(link) }

// markNZBBadKey is markNZBBad under a key: a link, or a release.
func (s *Server) markNZBBadKey(link string) {
	dir := s.nzbCacheDir()
	if dir == "" || link == "" {
		return
	}
	nzbCacheMu.Lock()
	defer nzbCacheMu.Unlock()
	if os.MkdirAll(dir, 0o755) == nil {
		_ = os.WriteFile(filepath.Join(dir, nzbKey(link)+".bad"), nil, 0o644)
	}
}

// nzbBad says whether a release was found broken in the last day.
func (s *Server) nzbBad(link string) bool { return s.nzbBadKey(link) }

// nzbBadKey is nzbBad under a key: a link, or a release.
func (s *Server) nzbBadKey(link string) bool {
	dir := s.nzbCacheDir()
	if dir == "" || link == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, nzbKey(link)+".bad"))
	return err == nil && time.Since(fi.ModTime()) < nzbBadKeep
}
