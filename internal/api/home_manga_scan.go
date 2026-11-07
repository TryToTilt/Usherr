package api

// Usherr: your own files. Looking through the manga folder finds files such as
// Series/Series v01.cbz or Series v01.cbz loose in the folder, and marks those
// volumes as on disk where they are. Nothing is moved, renamed or deleted, and a
// file of a series that isn't in the library is only listed, for you to add.
// It runs when asked, when a series is added, and once a night between 2 and 9.

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/manga"
)

const (
	mangaScanFromHour = 2 // the house rule for heavy scans: 2 AM to 9 AM
	mangaScanToHour   = 9
	mangaScanEvery    = 20 * time.Hour
	mangaScanMaxDepth = 3 // <root>/<Series>/<folder>/file
)

// mangaScanGroup is what was found for one name.
type mangaScanGroup struct {
	Name     string `json:"name"`
	SeriesID int64  `json:"seriesId,omitempty"` // in the library
	Title    string `json:"title,omitempty"`
	Files    int    `json:"files"`
	Claimed  int    `json:"claimed"`  // volumes newly marked as on disk
	Chapters int    `json:"chapters"` // chapter files, which aren't kept as volumes
	Packs    int    `json:"packs"`    // one file holding several volumes
	Unread   int    `json:"unread"`   // names that don't say which volume
	Doubles  int    `json:"doubles"`  // another file for a volume that has one; left alone
}

// mangaScanResult is one scan.
type mangaScanResult struct {
	At      string           `json:"at,omitempty"`
	Root    string           `json:"root"`
	Problem string           `json:"problem,omitempty"`
	Files   int              `json:"files"`
	Claimed int              `json:"claimed"`
	Gone    int              `json:"gone"`
	Groups  []mangaScanGroup `json:"groups"`
}

// GET /api/manga/scan: the last scan since the program started.
func (s *Server) handleGetMangaScan(w http.ResponseWriter, r *http.Request) {
	st := s.mangaMod()
	st.scan.Lock()
	last := st.lastScan
	st.scan.Unlock()
	if last.Groups == nil {
		last.Groups = []mangaScanGroup{}
		last.Root = s.mangaRoot()
	}
	last.Groups = s.mangaGroupsNow(last.Groups)
	writeJSON(w, http.StatusOK, last)
}

// mangaGroupsNow fits a kept scan's folders to the library as it is now: a series
// added or removed since shouldn't still offer "Add this series" or a dead link.
func (s *Server) mangaGroupsNow(groups []mangaScanGroup) []mangaScanGroup {
	list, err := s.mangaRepo().ListSeries()
	if err != nil {
		return groups
	}
	repo := s.mangaRepo()
	byKey := map[string]manga.Series{}
	have := map[int64]bool{}
	for _, sr := range list {
		have[sr.ID] = true
		for _, n := range repo.Names(sr) {
			if k := manga.Key(n); k != "" {
				if _, taken := byKey[k]; !taken {
					byKey[k] = sr
				}
			}
		}
	}
	out := make([]mangaScanGroup, len(groups))
	for i, g := range groups {
		if g.SeriesID != 0 && !have[g.SeriesID] {
			g.SeriesID, g.Title = 0, ""
		}
		if g.SeriesID == 0 {
			if sr, ok := byKey[manga.Key(g.Name)]; ok {
				g.SeriesID, g.Title = sr.ID, sr.Title
			}
		}
		out[i] = g
	}
	return out
}

// POST /api/manga/scan: look through the folder now.
func (s *Server) handleMangaScan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	writeJSON(w, http.StatusOK, s.scanManga(ctx, 0))
}

// mangaScanJob looks through the folder once a night, in the hours for heavy work.
func (s *Server) mangaScanJob(ctx context.Context) {
	if !s.mangaEnabled() {
		return
	}
	now := time.Now()
	if now.Hour() < mangaScanFromHour || now.Hour() >= mangaScanToHour {
		return
	}
	last, _ := s.Settings.Get(keyMangaScanLast)
	if t, err := time.Parse(time.RFC3339, last); err == nil && now.Sub(t) < mangaScanEvery {
		return
	}
	res := s.scanManga(ctx, 0)
	if res.Problem == "" {
		s.checkMangaChapterFiles() // and chapters, which that scan leaves out
	}
	_ = s.Settings.Set(keyMangaScanLast, now.UTC().Format(time.RFC3339), false)
	slog.Info("manga: nightly folder scan", "files", res.Files, "claimed", res.Claimed, "gone", res.Gone)
}

// scanManga looks through the manga folder. With only > 0 it looks just for that
// series, and doesn't keep the result as the last scan.
func (s *Server) scanManga(ctx context.Context, only int64) mangaScanResult {
	st := s.mangaMod()
	st.scan.Lock()
	defer st.scan.Unlock()
	res := mangaScanResult{At: time.Now().UTC().Format(time.RFC3339), Root: s.mangaRoot(), Groups: []mangaScanGroup{}}
	repo := st.repo
	if res.Root == "" {
		res.Problem = "no-folder"
		return res
	}
	info, err := os.Stat(res.Root)
	if err != nil || !info.IsDir() {
		res.Problem = "no-access"
		return res
	}
	list, err := repo.ListSeries()
	if err != nil {
		res.Problem = "database"
		return res
	}
	byKey := map[string]*manga.Series{}
	namesOf := map[int64][]string{}
	for i := range list {
		sr := &list[i]
		names := repo.Names(*sr)
		namesOf[sr.ID] = names
		for _, n := range names {
			if k := manga.Key(n); k != "" {
				if _, taken := byKey[k]; !taken {
					byKey[k] = sr
				}
			}
		}
	}
	type volumeState struct {
		v    manga.Volume
		seen bool
	}
	volumes := map[int64]map[string]*volumeState{}
	volumesOf := func(id int64) map[string]*volumeState {
		if m, ok := volumes[id]; ok {
			return m
		}
		m := map[string]*volumeState{}
		if vs, err := repo.Volumes(id); err == nil {
			for _, v := range vs {
				m[v.Number] = &volumeState{v: v}
			}
		}
		volumes[id] = m
		return m
	}
	groups := map[string]*mangaScanGroup{}
	group := func(name string, sr *manga.Series) *mangaScanGroup {
		key := manga.Key(name)
		if sr != nil {
			key = "#" + sr.Title
		}
		g, ok := groups[key]
		if !ok {
			g = &mangaScanGroup{Name: name}
			if sr != nil {
				g.SeriesID, g.Title = sr.ID, sr.Title
			}
			groups[key] = g
		}
		return g
	}
	_ = filepath.WalkDir(res.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(res.Root, p)
		if rerr != nil {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator))
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "Chapters" || depth >= mangaScanMaxDepth) {
				return fs.SkipDir
			}
			return nil
		}
		if !manga.IsPictureFile(d.Name()) {
			return nil
		}
		// A series' folder says which it is; a loose file has only its name.
		var sr *manga.Series
		hint := ""
		if parts := strings.Split(rel, string(filepath.Separator)); len(parts) > 1 {
			hint = parts[0]
			sr = byKey[manga.Key(hint)]
		}
		var name manga.Name
		if sr != nil {
			name = manga.ParseFor(d.Name(), namesOf[sr.ID])
		}
		if sr == nil || !name.Known {
			name = manga.Parse(d.Name())
		}
		if sr == nil && hint == "" {
			sr = byKey[manga.Key(name.Series)]
			hint = name.Series
		}
		if only > 0 && (sr == nil || sr.ID != only) {
			return nil
		}
		if hint == "" {
			hint = d.Name()
		}
		res.Files++
		g := group(hint, sr)
		g.Files++
		switch name.Kind {
		case manga.KindVolume, manga.KindBare:
		case manga.KindChapter:
			g.Chapters++
			return nil
		case manga.KindPack, manga.KindRange, manga.KindWhole:
			g.Packs++
			return nil
		default:
			g.Unread++
			return nil
		}
		if sr == nil {
			return nil
		}
		m := volumesOf(sr.ID)
		state := m[name.Number]
		if state != nil && state.v.Status == manga.StatusDownloaded && state.v.FilePath != p {
			if there, _ := copyAt(state.v.FilePath, 0); there {
				state.seen = true
				g.Doubles++
				return nil
			}
		}
		if state != nil && state.v.FilePath == p {
			state.seen = true
			return nil
		}
		v, _, err := repo.EnsureVolume(sr.ID, name.Number, false)
		if err != nil {
			return nil
		}
		size := int64(0)
		if i, err := d.Info(); err == nil {
			size = i.Size()
		}
		if repo.SetVolumeFile(v.ID, p, size, string(manga.ClassOwn)) == nil {
			v.FilePath, v.Status = p, manga.StatusDownloaded
			m[name.Number] = &volumeState{v: v, seen: true}
			g.Claimed++
			res.Claimed++
			if sr.Folder == "" && len(strings.Split(rel, string(filepath.Separator))) > 1 {
				sr.Folder = filepath.Dir(p)
				_ = repo.SetFolder(sr.ID, sr.Folder)
			}
		}
		return nil
	})
	if ctx.Err() != nil {
		return res
	}
	// Files that have gone: only for a whole scan of a folder that holds something,
	// so an empty, unmounted drive can't undo the library.
	if only == 0 && res.Files > 0 {
		for i := range list {
			vs, err := repo.Volumes(list[i].ID)
			if err != nil {
				continue
			}
			for _, v := range vs {
				if v.Status != manga.StatusDownloaded || fileExists(v.FilePath) {
					continue
				}
				if repo.ClearVolumeFile(v.ID) == nil {
					res.Gone++
				}
			}
		}
	}
	for _, g := range groups {
		res.Groups = append(res.Groups, *g)
	}
	sort.Slice(res.Groups, func(i, j int) bool {
		a, b := res.Groups[i], res.Groups[j]
		if (a.SeriesID == 0) != (b.SeriesID == 0) {
			return a.SeriesID == 0 // what's not in the library first: that's what needs you
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	if only == 0 {
		st.lastScan = res
	}
	return res
}
