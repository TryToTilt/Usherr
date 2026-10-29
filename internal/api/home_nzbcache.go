package api

// Usherr: NZB files are fetched from an indexer once. Indexers (NZBFinder
// warns, then closes the account) count every fetch of the same NZB; the
// streaming engine used to fetch it again each time a release was opened —
// a channel tuning the same episode, a game watched again, a release that
// had failed before. Each NZB is kept on disk for a week, and a release
// whose articles are gone is passed over for a day without being fetched.

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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

// cachedNZB is the NZB kept for a link, if it is there and recent.
func (s *Server) cachedNZB(link string) ([]byte, bool) {
	dir := s.nzbCacheDir()
	if dir == "" {
		return nil, false
	}
	p := filepath.Join(dir, nzbKey(link)+".nzb")
	fi, err := os.Stat(p)
	if err != nil || time.Since(fi.ModTime()) > nzbKeep {
		return nil, false
	}
	b, err := os.ReadFile(p)
	return b, err == nil && len(b) > 0
}

// keepNZB saves an NZB fetched for a link (only what looks like one).
func (s *Server) keepNZB(link string, data []byte) {
	dir := s.nzbCacheDir()
	if dir == "" || len(data) == 0 || notAnNZB(data, link) != nil {
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
func (s *Server) markNZBBad(link string) {
	dir := s.nzbCacheDir()
	if dir == "" {
		return
	}
	nzbCacheMu.Lock()
	defer nzbCacheMu.Unlock()
	if os.MkdirAll(dir, 0o755) == nil {
		_ = os.WriteFile(filepath.Join(dir, nzbKey(link)+".bad"), nil, 0o644)
	}
}

// nzbBad says whether a release was found broken in the last day.
func (s *Server) nzbBad(link string) bool {
	dir := s.nzbCacheDir()
	if dir == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, nzbKey(link)+".bad"))
	return err == nil && time.Since(fi.ModTime()) < nzbBadKeep
}
