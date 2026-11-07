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
	local  string   // Usherr: a file on Usherr's own disk (the pre-show for Plex)
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
	// The pre-show for Plex (home_preshow_plex.go): Usherr's own files.
	for _, lf := range drivePreshowFiles() {
		add("/Preshow/"+lf.name, &driveNode{size: lf.size, mod: lf.mod, local: lf.path})
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

	driveOpenStrikes = map[int64]time.Time{} // when a release first wouldn't open
	driveHoleChecked = map[int64]time.Time{} // when a release's missing articles were last asked about
	// driveStrikeGap: a release that won't open is given up on when it still
	// won't this long after the first time; driveStrikeForget: that first time
	// no longer counts after this.
	driveStrikeGap    = 10 * time.Minute
	driveStrikeForget = 24 * time.Hour
)

const (
	driveOpenKeep       = 30 * time.Minute
	driveHoleCheckEvery = 10 * time.Minute
)

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
	var storedSize int64
	if err := s.db.QueryRowContext(ctx, `SELECT nzb, size FROM streamed_files WHERE id = ?`, id).Scan(&nzbName, &storedSize); err != nil {
		return nil, err
	}
	want, err := s.driveWant(ctx, id)
	if err != nil {
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
	f, err := download.OpenStreamFor(octx, servers, nzb, want)
	if err != nil {
		switch {
		case errors.Is(err, download.ErrArticleMissing):
			s.driveOpenMissed(id)
		case errors.Is(err, download.ErrNotInRelease):
			s.driveDead(id) // kept from a pack without this episode: look for another
		}
		return nil, err
	}
	if storedSize > 0 && f.Size != storedSize {
		// Kept by a version that always took a pack's biggest video, which was
		// another episode. This one is the right one, and the drive should say so.
		slog.Info("drive: the right file in a pack differs from the one kept", "file", id, "name", f.Name)
		if _, err := s.db.ExecContext(ctx, `UPDATE streamed_files SET inner_file = ?, size = ? WHERE id = ?`, f.Name, f.Size, id); err == nil {
			driveTreeChanged()
		}
	}
	driveOpenMu.Lock()
	delete(driveOpenStrikes, id)
	driveOpenTo[id] = &driveOpened{file: f, last: time.Now()}
	driveOpenMu.Unlock()
	return f, nil
}

// driveWant says what a streamed file is, for finding it in its release.
func (s *Server) driveWant(ctx context.Context, id int64) (download.StreamWant, error) {
	var w download.StreamWant
	var titleID int64
	err := s.db.QueryRowContext(ctx, `SELECT title_id, season, episode, air_date, release, inner_file FROM streamed_files WHERE id = ?`, id).
		Scan(&titleID, &w.Season, &w.Episode, &w.AirDate, &w.Release, &w.InnerFile)
	if err != nil {
		return w, err
	}
	if w.Episode > 0 {
		// Only for a whole season out: a pack can't hold what hasn't aired.
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM streamed_files WHERE title_id = ? AND season = ?`, titleID, w.Season).Scan(&w.SeasonEpisodes)
	}
	return w, nil
}

// driveReader is where a file's bytes come from: its release on Usenet (a
// test puts a stand-in here).
var driveReader = func(s *Server, ctx context.Context, id int64) (io.ReaderAt, error) {
	return s.driveStream(ctx, id)
}

// driveOpenMissed counts a release that wouldn't open because a file's first
// article is missing. A release leaves the drive only when that's still so a
// while later, so one bad answer from a provider can't take a title out of
// Plex, which would change its library.
func (s *Server) driveOpenMissed(id int64) {
	driveOpenMu.Lock()
	first, ok := driveOpenStrikes[id]
	if !ok || time.Since(first) > driveStrikeForget {
		driveOpenStrikes[id] = time.Now()
		driveOpenMu.Unlock()
		slog.Info("drive: a release wouldn't open, a first article is missing; asking again later", "file", id)
		return
	}
	stillMissing := time.Since(first) >= driveStrikeGap
	if stillMissing {
		delete(driveOpenStrikes, id)
	}
	driveOpenMu.Unlock()
	if stillMissing {
		s.driveDead(id)
	}
}

// holeConfirmer is a video that can have its missing articles asked about again.
type holeConfirmer interface {
	ConfirmHoles(ctx context.Context) download.HoleVerdict
}

// driveHolesOver is called when a read reached more missing articles than the
// video can take. The read has failed; now the servers are asked about exactly
// those articles, once per driveHoleCheckEvery. The release leaves the drive
// only if all of them answer and what's missing is still too much. A server that
// can't be asked changes nothing: "couldn't ask" is not "gone".
func (s *Server) driveHolesOver(id int64, ra io.ReaderAt) {
	c, ok := ra.(holeConfirmer)
	if !ok {
		return
	}
	driveOpenMu.Lock()
	if at, ok := driveHoleChecked[id]; ok && time.Since(at) < driveHoleCheckEvery {
		driveOpenMu.Unlock()
		return
	}
	driveHoleChecked[id] = time.Now()
	driveOpenMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		switch c.ConfirmHoles(ctx) {
		case download.HolesGone:
			s.driveDead(id)
		case download.HolesBearable:
			slog.Info("drive: most of a release's missing articles are back; it stays", "file", id)
		case download.HolesUnknown:
			slog.Info("drive: couldn't find out whether a release's articles are gone; it stays", "file", id)
		}
	}()
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
	read int      // folder entries already listed
	lf   *os.File // a local file, once opened
}

func (f *driveFile) Close() error {
	if f.lf != nil {
		return f.lf.Close()
	}
	return nil
}
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
	if f.n.local != "" {
		if f.lf == nil {
			lf, err := os.Open(f.n.local)
			if err != nil {
				return 0, err
			}
			f.lf = lf
		}
		n, err := f.lf.ReadAt(p, f.off)
		f.off += int64(n)
		if errors.Is(err, io.EOF) && n > 0 {
			err = nil
		}
		return n, err
	}
	ra, err := driveReader(f.s, f.ctx, f.n.fileID)
	if err != nil {
		return 0, err
	}
	n, err := ra.ReadAt(p, f.off)
	f.off += int64(n)
	if err != nil && errors.Is(err, download.ErrArticleMissing) {
		f.s.driveHolesOver(f.n.fileID, ra)
	}
	if errors.Is(err, io.EOF) && n > 0 {
		err = nil
	}
	return n, err
}

// ---- Usherr's own files on the drive ----

type driveLocal struct {
	name, path string
	size       int64
	mod        time.Time
}

var drivePreshow struct {
	sync.Mutex
	files []driveLocal
}

// setDrivePreshow puts the pre-show's videos in the drive's Preshow folder.
func setDrivePreshow(files []driveLocal) {
	drivePreshow.Lock()
	drivePreshow.files = files
	drivePreshow.Unlock()
	driveTreeChanged()
}

func drivePreshowFiles() []driveLocal {
	drivePreshow.Lock()
	defer drivePreshow.Unlock()
	return append([]driveLocal(nil), drivePreshow.files...)
}
