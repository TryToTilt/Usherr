package api

// Usherr: the virtual drive's WebDAV side (home_drive.go has the rest). A
// read-only drive at /dav/ that lists each streamed film and episode with a
// release kept, laid out like the pointer files (Movies/Title (Year)
// [tmdbid-1]/Title (Year).mkv, TV/…/Season 01/…), so Plex matches them. A
// read is served from Usenet by the streaming engine: only the part asked for
// is fetched. It has its own sign-in (user "usherr" and the drive's
// password), never a person's account, and it only reads.

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/webdav"

	"github.com/rdborg/mediarium/internal/download"
)

func init() {
	// So a player is told what a file is without it being read first.
	for ext, t := range map[string]string{".mkv": "video/x-matroska", ".mp4": "video/mp4", ".m4v": "video/mp4", ".avi": "video/x-msvideo", ".ts": "video/mp2t", ".m2ts": "video/mp2t", ".webm": "video/webm", ".mov": "video/quicktime"} {
		_ = mime.AddExtensionType(ext, t)
	}
}

// driveLastRead is when anything last read the drive (ms), for the Doctor and
// the settings card: a drive nobody reads is probably not mounted.
var driveLastRead atomic.Int64

func (s *Server) registerDriveRoutes(root *http.ServeMux) {
	h := &webdav.Handler{Prefix: "/dav", FileSystem: &driveFS{s: s}, LockSystem: webdav.NewMemLS()}
	serve := func(w http.ResponseWriter, r *http.Request) {
		if !s.driveOn() {
			http.NotFound(w, r)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(driveUser)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(s.drivePassword())) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Usherr drive"`)
			http.Error(w, "Usherr's drive needs its own user and password (Settings, Extras, Virtual drive).", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, "PROPFIND", "LOCK", "UNLOCK":
		default:
			http.Error(w, "Usherr's drive is read only.", http.StatusMethodNotAllowed)
			return
		}
		driveLastRead.Store(time.Now().UnixMilli())
		h.ServeHTTP(w, r)
	}
	root.HandleFunc("/dav/", serve)
	root.HandleFunc("/dav", serve)
}

// ---- the tree ----

type driveNode struct {
	dir    bool
	size   int64
	mod    time.Time
	fileID int64
	kids   []string // a folder's entries, sorted
}

type driveTree map[string]*driveNode // "/Movies/Title (Year) [tmdbid-1]/Title (Year).mkv"

var (
	driveTreeMu   sync.Mutex
	driveTreeAt   time.Time
	driveTreeNow  driveTree
	driveTreeKeep = time.Minute
	driveStarted  = time.Now()
)

// driveTreeChanged makes the next look at the drive read the list again.
func driveTreeChanged() {
	driveTreeMu.Lock()
	driveTreeAt = time.Time{}
	driveTreeMu.Unlock()
}

// driveFileName is a file's place on the drive: the pointer file's, with the
// video's own extension.
func driveFileName(kind string, anime bool, title string, year, tmdb, season, episode int, inner string) string {
	ext := strings.ToLower(filepath.Ext(inner))
	if ext == "" || !isVideoExt(ext) {
		ext = ".mkv"
	}
	return "/" + strings.TrimSuffix(streamedPointerPath(kind, anime, title, year, tmdb, season, episode), ".strm") + ext
}

func isVideoExt(ext string) bool {
	switch ext {
	case ".mkv", ".mp4", ".m4v", ".avi", ".ts", ".m2ts", ".webm", ".mov", ".wmv":
		return true
	}
	return false
}

func (s *Server) driveTree(ctx context.Context) (driveTree, error) {
	driveTreeMu.Lock()
	defer driveTreeMu.Unlock()
	if driveTreeNow != nil && time.Since(driveTreeAt) < driveTreeKeep {
		return driveTreeNow, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, t.kind, t.anime, t.title, t.year, t.tmdb_id, f.season, f.episode, f.inner_file, f.size, f.health_at, t.added_at
		FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id
		WHERE f.nzb != '' AND f.size > 0 AND f.health != 'dead'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tree := driveTree{"/": {dir: true}}
	var add func(p string, n *driveNode)
	add = func(p string, n *driveNode) {
		if _, ok := tree[p]; ok {
			return
		}
		tree[p] = n
		parent := path.Dir(p)
		if _, ok := tree[parent]; !ok {
			add(parent, &driveNode{dir: true, mod: n.mod})
		}
		tree[parent].kids = append(tree[parent].kids, path.Base(p))
	}
	// The three top folders are always there, even empty, so Plex can be
	// given them before their first film or episode is ready.
	for _, top := range []string{"/Movies", "/TV", "/Anime"} {
		add(top, &driveNode{dir: true, mod: driveStarted})
	}
	for rows.Next() {
		var (
			id                                    int64
			kind, title, inner, healthAt, addedAt string
			anime                                 bool
			year, tmdb, season, episode           int
			size                                  int64
		)
		if err := rows.Scan(&id, &kind, &anime, &title, &year, &tmdb, &season, &episode, &inner, &size, &healthAt, &addedAt); err != nil {
			return nil, err
		}
		k := "movie"
		if kind == "series" {
			k = "series"
		}
		mod, err := time.Parse(time.RFC3339, healthAt)
		if err != nil {
			mod, _ = time.Parse("2006-01-02T15:04:05.000Z", addedAt)
		}
		add(driveFileName(k, anime, title, year, tmdb, season, episode, inner), &driveNode{size: size, mod: mod, fileID: id})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, n := range tree {
		if n.dir {
			sort.Strings(n.kids)
		}
	}
	driveTreeNow, driveTreeAt = tree, time.Now()
	return tree, nil
}

// ---- opened releases ----

type driveOpened struct {
	file *download.StreamFile
	last time.Time
}

var (
	driveOpenMu sync.Mutex
	driveOpenTo = map[int64]*driveOpened{}
	driveOpenIn = map[int64]*sync.Mutex{} // one opening at a time per file
)

const driveOpenKeep = 30 * time.Minute

func init() {
	go func() {
		for range time.Tick(5 * time.Minute) {
			driveOpenMu.Lock()
			for id, o := range driveOpenTo {
				if time.Since(o.last) > driveOpenKeep {
					delete(driveOpenTo, id)
				}
			}
			driveOpenMu.Unlock()
		}
	}()
}

// driveForget drops a file's opened release (it was replaced).
func driveForget(id int64) {
	driveOpenMu.Lock()
	delete(driveOpenTo, id)
	driveOpenMu.Unlock()
}

// driveStream opens a file's kept release for reading, once, and keeps it
// open while it's read.
func (s *Server) driveStream(ctx context.Context, id int64) (*download.StreamFile, error) {
	driveOpenMu.Lock()
	if o, ok := driveOpenTo[id]; ok {
		o.last = time.Now()
		driveOpenMu.Unlock()
		return o.file, nil
	}
	in, ok := driveOpenIn[id]
	if !ok {
		in = &sync.Mutex{}
		driveOpenIn[id] = in
	}
	driveOpenMu.Unlock()
	in.Lock()
	defer in.Unlock()
	driveOpenMu.Lock()
	if o, ok := driveOpenTo[id]; ok { // opened while this one waited
		o.last = time.Now()
		driveOpenMu.Unlock()
		return o.file, nil
	}
	driveOpenMu.Unlock()

	var nzbName string
	if err := s.db.QueryRowContext(ctx, `SELECT nzb FROM streamed_files WHERE id = ?`, id).Scan(&nzbName); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.streamedNZBDir(), filepath.Base(nzbName)))
	if err != nil {
		return nil, err
	}
	nzb, err := download.ParseNZB(data)
	if err != nil {
		return nil, err
	}
	servers, err := s.ownServers()
	if err != nil {
		return nil, err
	}
	octx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
	defer cancel()
	f, err := download.OpenStream(octx, servers, nzb)
	if err != nil {
		if errors.Is(err, download.ErrArticleMissing) {
			s.driveDead(id)
		}
		return nil, err
	}
	driveOpenMu.Lock()
	driveOpenTo[id] = &driveOpened{file: f, last: time.Now()}
	driveOpenMu.Unlock()
	return f, nil
}

// driveReader is where a file's bytes come from: its release on Usenet (a
// test puts a stand-in here).
var driveReader = func(s *Server, ctx context.Context, id int64) (io.ReaderAt, error) {
	return s.driveStream(ctx, id)
}

// driveDead marks a file's release as gone from Usenet: it leaves the drive
// until the drive's search finds another.
func (s *Server) driveDead(id int64) {
	_, _ = s.db.Exec(`UPDATE streamed_files SET health = 'dead', health_at = ?, drive_tried_at = '' WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), id)
	driveForget(id)
	driveTreeChanged()
	slog.Info("drive: a release is gone from Usenet; another will be looked for", "file", id)
}

// ---- webdav.FileSystem ----

type driveFS struct{ s *Server }

var errDriveReadOnly = &os.PathError{Op: "write", Path: "/", Err: os.ErrPermission}

func (d *driveFS) Mkdir(context.Context, string, os.FileMode) error { return errDriveReadOnly }
func (d *driveFS) RemoveAll(context.Context, string) error          { return errDriveReadOnly }
func (d *driveFS) Rename(context.Context, string, string) error     { return errDriveReadOnly }

func (d *driveFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	tree, err := d.s.driveTree(ctx)
	if err != nil {
		return nil, err
	}
	name = path.Clean("/" + name)
	n, ok := tree[name]
	if !ok {
		return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
	}
	return driveInfo{name: path.Base(name), n: n}, nil
}

func (d *driveFS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return nil, errDriveReadOnly
	}
	tree, err := d.s.driveTree(ctx)
	if err != nil {
		return nil, err
	}
	name = path.Clean("/" + name)
	n, ok := tree[name]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
	}
	return &driveFile{s: d.s, ctx: context.WithoutCancel(ctx), name: name, n: n, tree: tree}, nil
}

type driveInfo struct {
	name string
	n    *driveNode
}

func (i driveInfo) Name() string { return i.name }
func (i driveInfo) Size() int64  { return i.n.size }
func (i driveInfo) Mode() fs.FileMode {
	if i.n.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i driveInfo) ModTime() time.Time { return i.n.mod }
func (i driveInfo) IsDir() bool        { return i.n.dir }
func (i driveInfo) Sys() any           { return nil }

// driveFile is one file or folder of the drive, opened.
type driveFile struct {
	s    *Server
	ctx  context.Context
	name string
	n    *driveNode
	tree driveTree
	off  int64
	read int // folder entries already listed
}

func (f *driveFile) Close() error              { return nil }
func (f *driveFile) Write([]byte) (int, error) { return 0, errDriveReadOnly }
func (f *driveFile) Stat() (fs.FileInfo, error) {
	return driveInfo{name: path.Base(f.name), n: f.n}, nil
}

func (f *driveFile) Readdir(count int) ([]fs.FileInfo, error) {
	if !f.n.dir {
		return nil, &os.PathError{Op: "readdir", Path: f.name, Err: errors.New("not a folder")}
	}
	var out []fs.FileInfo
	for f.read < len(f.n.kids) && (count <= 0 || len(out) < count) {
		k := f.n.kids[f.read]
		f.read++
		if c, ok := f.tree[path.Join(f.name, k)]; ok {
			out = append(out, driveInfo{name: k, n: c})
		}
	}
	if count > 0 && len(out) == 0 {
		return nil, io.EOF
	}
	return out, nil
}

func (f *driveFile) Seek(offset int64, whence int) (int64, error) {
	if f.n.dir {
		return 0, errors.New("a folder can't be read")
	}
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += f.off
	case io.SeekEnd:
		offset += f.n.size
	default:
		return 0, errors.New("bad seek")
	}
	if offset < 0 {
		return 0, errors.New("seek before the start")
	}
	f.off = offset
	return offset, nil
}

func (f *driveFile) Read(p []byte) (int, error) {
	if f.n.dir {
		return 0, errors.New("a folder can't be read")
	}
	if f.off >= f.n.size {
		return 0, io.EOF
	}
	if rest := f.n.size - f.off; int64(len(p)) > rest {
		p = p[:rest]
	}
	ra, err := driveReader(f.s, f.ctx, f.n.fileID)
	if err != nil {
		return 0, err
	}
	n, err := ra.ReadAt(p, f.off)
	f.off += int64(n)
	if err != nil && errors.Is(err, download.ErrArticleMissing) {
		f.s.driveDead(f.n.fileID)
	}
	if errors.Is(err, io.EOF) && n > 0 {
		err = nil
	}
	return n, err
}
