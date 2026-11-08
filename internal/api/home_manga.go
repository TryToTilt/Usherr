package api

// Usherr: the Manga section. A media type of its own, on the switchboard, off
// until switched on, and kept apart from Comics: add a series by its English
// title (AniList), get official English volumes through the usual indexers and
// download line, and see in plain words why nothing has come. See docs/manga.md.
// This file: the module, its settings, the library, adding a series. Searching
// is in home_manga_search.go, filing in home_manga_file.go, the folder scan in
// home_manga_scan.go.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/manga"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/settings"
)

const (
	keyMangaPath           = "manga.path"            // library folder; MANGA_DIR when unset
	keyMangaSourceOfficial = "manga.source_official" // "0" turns official English editions off
	keyMangaSourceFan      = "manga.source_fan"      // "1" allows fan scans from the indexers
	keyMangaSourceDex      = "manga.source_mangadex" // "1" allows chapters from MangaDex
	keyMangaSourceNyaa     = "manga.source_nyaa"     // "1" allows Nyaa's results
	keyMangaScanLast       = "manga.scan_last"       // when the nightly folder scan last ran
)

// mangaAniListURL points the AniList client somewhere else, for a test copy.
const mangaAniListURL = "USHERR_ANILIST_URL"

// mangaDexURL points the MangaDex client somewhere else, for a test copy.
const mangaDexURL = "USHERR_MANGADEX_URL"

type mangaState struct {
	once     sync.Once
	repo     *manga.Repo
	anilist  *manga.AniList
	images   sync.Map
	imgHost  string // an extra allowed address for covers, with a test copy's AniList
	hunt     sync.Mutex
	scan     sync.Mutex
	move     sync.Mutex // one move or undo at a time
	lastScan mangaScanResult
	dex      *manga.MangaDex
	dexJob   sync.Mutex    // one look at MangaDex at a time
	addLook  sync.Mutex    // one look after an Add at a time, however fast series are added
	dexSlot  chan struct{} // one batch of chapters at a time, whatever the download line allows
}

var mangaStates sync.Map

func (s *Server) mangaMod() *mangaState {
	v, _ := mangaStates.LoadOrStore(s, &mangaState{})
	st := v.(*mangaState)
	st.once.Do(func() {
		st.repo = manga.NewRepo(s.db)
		base := strings.TrimSpace(os.Getenv(mangaAniListURL))
		st.anilist = manga.NewAniList("Usherr/"+s.version, base)
		st.dex = manga.NewMangaDex("Usherr/"+s.version, strings.TrimSpace(os.Getenv(mangaDexURL)))
		st.dexSlot = make(chan struct{}, 1)
		s.restoreDexPause(st.dex)
		if base != "" {
			if u, err := url.Parse(base); err == nil {
				st.imgHost = u.Scheme + "://" + u.Host + "/"
			}
		}
	})
	return st
}

func (s *Server) mangaRepo() *manga.Repo { return s.mangaMod().repo }

// mangaEnabled says whether the Manga media type is on. It's off until switched on.
func (s *Server) mangaEnabled() bool {
	v, _ := s.Settings.Get(settings.KeyModulesManga)
	return v == "1"
}

// mangaFolderProblem says, in plain words, why volumes couldn't be filed in
// the manga folder, or "" when they can. It really writes, because a folder
// can look fine and still refuse: a default inside the container that no drive
// is connected to is the usual case. Checked before anything is fetched, so a
// search never spends grabs on files that have nowhere to go.
func (s *Server) mangaFolderProblem() string {
	root := s.mangaRoot()
	if root == "" {
		return "Choose the manga folder in Settings > Library > Folders first."
	}
	refused := "Usherr can't save to the manga folder (" + root + "), so it won't look for or download manga yet. Choose another folder in Settings > Library > Folders, or connect one to the container."
	if err := os.MkdirAll(root, 0o755); err != nil {
		return refused
	}
	f, err := os.CreateTemp(root, ".usherr-write-check-*")
	if err != nil {
		return refused
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return ""
}

// mangaRoot: the saved folder, else MANGA_DIR, else /manga or /data/manga.
func (s *Server) mangaRoot() string { return s.extraFolderRoot("manga") }

func (s *Server) registerMangaRoutes(admin, member routeGroup) {
	on := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !s.mangaEnabled() {
				writeError(w, http.StatusNotFound, "Manga is switched off. Switch it on in Settings > Media types.")
				return
			}
			h(w, r)
		}
	}
	member.HandleFunc("GET /api/manga", on(s.handleListManga))
	member.HandleFunc("GET /api/manga/image", on(s.handleMangaImage))
	member.HandleFunc("GET /api/manga/{id}", on(s.handleGetManga))
	member.HandleFunc("GET /api/manga/volumes/{id}/file", on(s.handleMangaVolumeFile))
	member.Need(auth.PermManga).HandleFunc("GET /api/manga/lookup", on(s.handleMangaLookup))
	member.Need(auth.PermManga).HandleFunc("POST /api/manga", on(s.handleAddManga))
	admin.HandleFunc("GET /api/manga/settings", s.handleGetMangaSettings)
	admin.HandleFunc("PUT /api/manga/settings", s.handlePutMangaSettings)
	admin.HandleFunc("PUT /api/manga/{id}", on(s.handleUpdateManga))
	admin.HandleFunc("DELETE /api/manga/{id}", on(s.handleDeleteManga))
	admin.HandleFunc("POST /api/manga/{id}/refresh", on(s.handleRefreshManga))
	admin.HandleFunc("POST /api/manga/{id}/search", on(s.handleSearchManga))
	admin.HandleFunc("POST /api/manga/{id}/volumes", on(s.handleAddMangaVolume))
	admin.HandleFunc("PUT /api/manga/volumes/wanted", on(s.handleSetMangaWanted))
	admin.HandleFunc("POST /api/manga/volumes/{id}/search", on(s.handleSearchMangaVolume))
	admin.HandleFunc("GET /api/manga/volumes/{id}/releases", on(s.handleMangaVolumeReleases))
	admin.HandleFunc("POST /api/manga/volumes/{id}/grab", on(s.handleGrabMangaVolume))
	admin.HandleFunc("GET /api/manga/reviews", on(s.handleMangaReviews))
	admin.HandleFunc("POST /api/manga/reviews/{id}/accept", on(s.handleAcceptMangaReview))
	admin.HandleFunc("POST /api/manga/reviews/{id}/reject", on(s.handleRejectMangaReview))
	admin.HandleFunc("GET /api/manga/scan", on(s.handleGetMangaScan))
	admin.HandleFunc("POST /api/manga/scan", on(s.handleMangaScan))
	s.registerMangaReaderRoutes(member, on)
	s.registerMangaFindRoutes(member, on)
	s.registerMangaChapterRoutes(admin, member, on)
	s.registerMangaMoveRoutes(admin, on)
	s.registerMangaAccountRoutes(member, on)       // each person's own MangaDex account
	s.registerMangaBrowseRoutes(admin, member, on) // MangaDex in Discover, and reading online
}

// ---- settings ----

// mangaSourceSettings are the choices in Settings > Manga: what's allowed for
// every series that doesn't choose for itself.
type mangaSourceSettings struct {
	Official bool `json:"official"`
	Fan      bool `json:"fan"`
	MangaDex bool `json:"mangadex"`
	Nyaa     bool `json:"nyaa"`
}

func (s *Server) currentMangaSources() mangaSourceSettings {
	flag := func(key string) bool { v, _ := s.Settings.Get(key); return v == "1" }
	official, _ := s.Settings.Get(keyMangaSourceOfficial)
	return mangaSourceSettings{Official: official != "0", Fan: flag(keyMangaSourceFan), MangaDex: flag(keyMangaSourceDex), Nyaa: flag(keyMangaSourceNyaa)}
}

// mangaSourcesFor is what a series may take: the settings, and its own choice on top.
func (s *Server) mangaSourcesFor(sr manga.Series) manga.Sources {
	c := s.currentMangaSources()
	out := manga.Sources{Official: c.Official, Fan: c.Fan, Nyaa: c.Nyaa, MangaDex: c.MangaDex}
	switch sr.SourceMode {
	case "official":
		out.Official, out.Fan, out.MangaDex = true, false, false
	case "fan":
		out.Fan = true
	}
	return out
}

func (s *Server) handleGetMangaSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mangaSettingsPayload())
}

func (s *Server) mangaSettingsPayload() map[string]any {
	return map[string]any{"sources": s.currentMangaSources(), "path": s.mangaRoot(), "nyaa": s.mangaNyaaNow(), "folderProblem": s.mangaFolderProblem(), "chapters": s.mangaChapterSettings()}
}

// PUT /api/manga/settings {"official","fan","mangadex","nyaa"}: only what is sent changes.
func (s *Server) handlePutMangaSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Official *bool `json:"official"`
		Fan      *bool `json:"fan"`
		MangaDex *bool `json:"mangadex"`
		Nyaa     *bool `json:"nyaa"`
		mangaChapterSettingsChange
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if problem := s.saveMangaChapterSettings(req.mangaChapterSettingsChange); problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	// Turning Nyaa on adds it to the indexers, once. If that can't be done, or
	// torrents have nowhere to go, the switch stays off and says why.
	if req.Nyaa != nil && *req.Nyaa {
		if !s.torrentReleasesOn() {
			writeError(w, http.StatusConflict, "Nyaa is torrents only, and torrents are switched off. Switch them on in Settings > Downloading > Torrents first.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		err := s.ensureMangaNyaa(ctx)
		cancel()
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}
	for _, c := range []struct {
		key string
		v   *bool
	}{{keyMangaSourceOfficial, req.Official}, {keyMangaSourceFan, req.Fan}, {keyMangaSourceDex, req.MangaDex}, {keyMangaSourceNyaa, req.Nyaa}} {
		if c.v == nil {
			continue
		}
		v := "0"
		if *c.v {
			v = "1"
		}
		if err := s.Settings.Set(c.key, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The choice couldn't be saved.")
			return
		}
	}
	// A new source is a new reason to look.
	if list, err := s.mangaRepo().ListSeries(); err == nil {
		for _, sr := range list {
			_ = s.mangaRepo().ResetBackoff(sr.ID)
		}
	}
	writeJSON(w, http.StatusOK, s.mangaSettingsPayload())
}

// ---- the library ----

func (s *Server) canSeeAdultManga(r *http.Request) bool { return s.userAllows(r, auth.PermNSFW) }

// mangaForPage dresses a series for the page.
func (s *Server) mangaForPage(sr manga.Series, admin bool) manga.Series {
	sr.CoverURL = s.mangaImage(sr.CoverURL)
	if !admin {
		sr.Folder = ""
	}
	return sr
}

// GET /api/manga: the library, and how many releases wait for a person.
func (s *Server) handleListManga(w http.ResponseWriter, r *http.Request) {
	list, err := s.mangaRepo().ListSeries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The manga couldn't be read.")
		return
	}
	admin, adult := isAdminRequest(r), s.canSeeAdultManga(r)
	out := make([]manga.Series, 0, len(list))
	for _, sr := range list {
		if sr.IsAdult && !adult {
			continue
		}
		out = append(out, s.mangaForPage(sr, admin))
	}
	base := s.currentMangaSources()
	writeJSON(w, http.StatusOK, map[string]any{"series": out, "problems": s.mangaProblems(), "sources": map[string]bool{"official": base.Official, "fan": base.Fan, "nyaa": base.Nyaa, "mangadex": base.MangaDex}})
}

// mangaProblems are what stops anything arriving, whatever the series: the page says them.
func (s *Server) mangaProblems() []string {
	out := []string{}
	if s.mangaRoot() == "" {
		out = append(out, "no-folder")
	}
	if list, err := s.searchableIndexers(); err == nil && len(list) == 0 {
		out = append(out, "no-indexers")
	}
	return out
}

func (s *Server) mangaFromPath(w http.ResponseWriter, r *http.Request) (manga.Series, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a series id.")
		return manga.Series{}, false
	}
	sr, err := s.mangaRepo().GetSeries(id)
	if err != nil || (sr.IsAdult && !s.canSeeAdultManga(r)) {
		writeError(w, http.StatusNotFound, "That series isn't in your library.")
		return manga.Series{}, false
	}
	return sr, true
}

func (s *Server) mangaVolumeFromPath(w http.ResponseWriter, r *http.Request) (manga.Volume, manga.Series, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a volume id.")
		return manga.Volume{}, manga.Series{}, false
	}
	v, err := s.mangaRepo().GetVolume(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That volume isn't in your library.")
		return manga.Volume{}, manga.Series{}, false
	}
	sr, err := s.mangaRepo().GetSeries(v.SeriesID)
	if err != nil || (sr.IsAdult && !s.canSeeAdultManga(r)) {
		writeError(w, http.StatusNotFound, "That volume isn't in your library.")
		return manga.Volume{}, manga.Series{}, false
	}
	return v, sr, true
}

// GET /api/manga/{id}: a series, its volumes, and all the page needs to say
// what's happening and why.
func (s *Server) handleGetManga(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	repo := s.mangaRepo()
	volumes, err := repo.Volumes(sr.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The volumes couldn't be read.")
		return
	}
	admin := isAdminRequest(r)
	if !admin {
		for i := range volumes {
			volumes[i].FilePath = ""
		}
	}
	_ = repo.FillReading(requestUserID(r), sr.ID, volumes)
	aliases, _ := repo.Aliases(sr.ID)
	log, _ := repo.SearchLog(sr.ID)
	reviews := []manga.Review{}
	if admin {
		reviews, _ = repo.Reviews(sr.ID)
	}
	src := s.mangaSourcesFor(sr)
	writeJSON(w, http.StatusOK, map[string]any{
		"series": s.mangaForPage(sr, admin), "volumes": volumes, "aliases": aliases, "reviews": reviews,
		"search":   mangaWhy(log, volumes),
		"sources":  map[string]bool{"official": src.Official, "fan": src.Fan, "nyaa": src.Nyaa, "mangadex": src.MangaDex},
		"problems": s.mangaProblems(),
		"reading":  s.mangaContinueFor(r, sr.ID),
		"chapters": s.mangaChapterSummary(sr),
	})
}

// mangaSearchState is the last search worth telling about, and the next look.
type mangaSearchState struct {
	Last     *manga.SearchEntry `json:"last,omitempty"`
	Number   string             `json:"number,omitempty"` // the volume it was for
	NextLook string             `json:"nextLook,omitempty"`
}

// mangaWhy picks what the "Why nothing yet?" sentence is about: the last search,
// by volume or by name, and when Usherr looks next.
func mangaWhy(log []manga.SearchEntry, volumes []manga.Volume) mangaSearchState {
	var st mangaSearchState
	number := map[int64]string{}
	wanted := map[int64]bool{0: true}
	for _, v := range volumes {
		number[v.ID] = v.Number
		wanted[v.ID] = v.Wanted && v.Status != manga.StatusDownloaded
	}
	for i := range log { // newest first
		e := log[i]
		if !wanted[e.VolumeID] {
			continue
		}
		if st.Last == nil {
			st.Last, st.Number = &log[i], number[e.VolumeID]
		}
		if e.NextAt != "" && (st.NextLook == "" || e.NextAt < st.NextLook) {
			st.NextLook = e.NextAt
		}
	}
	return st
}

// GET /api/manga/lookup?q=: series on AniList. Nothing is saved; an answer that
// has nothing to show says why.
func (s *Server) handleMangaLookup(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}
	if len(q) > 200 {
		q = q[:200]
	}
	st := s.mangaMod()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	found, err := st.anilist.Search(ctx, q)
	if err != nil {
		problem := "AniList couldn't be reached, so nothing can be looked up right now. Your library is fine: try again in a few minutes."
		if errors.Is(err, manga.ErrAniListBusy) {
			problem = "AniList asked Usherr to slow down. Try again in a minute."
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}, "problem": problem})
		return
	}
	adult := s.canSeeAdultManga(r)
	hiddenAdult, hiddenNovels := 0, 0
	out := make([]manga.Found, 0, len(found))
	for _, f := range found {
		switch {
		case f.IsNovel:
			hiddenNovels++
			continue
		case f.IsAdult && !adult:
			hiddenAdult++
			continue
		}
		if have, err := st.repo.SeriesByAniList(f.AniListID); err == nil {
			f.InLibrary = have.ID
		}
		f.CoverURL = s.mangaImage(f.CoverURL)
		if !s.dexOn() {
			f.MangaDexID = ""
		}
		out = append(out, f)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out, "hiddenAdult": hiddenAdult, "hiddenNovels": hiddenNovels})
}

// POST /api/manga {"anilistId" or "title", "want": "all"|"new"|"none", "allowFan"}:
// adds a series. Only now is anything of it saved.
func (s *Server) handleAddManga(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AniListID int64  `json:"anilistId"`
		Title     string `json:"title"`
		Want      string `json:"want"`
		AllowFan  bool   `json:"allowFan"`
		Chapters  string `json:"chapters"` // "new" or "all" also follows its chapters
	}
	if err := decodeJSON(r, &req); err != nil || (req.AniListID <= 0 && strings.TrimSpace(req.Title) == "") {
		writeError(w, http.StatusBadRequest, "Choose a series to add.")
		return
	}
	if req.Want != "all" && req.Want != "none" {
		req.Want = "new"
	}
	repo := s.mangaRepo()
	info := manga.Series{Title: strings.TrimSpace(req.Title), LicensedEN: -1}
	var aliases []manga.Alias
	if req.AniListID > 0 {
		if have, err := repo.SeriesByAniList(req.AniListID); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"series": have, "volumes": have.Total, "already": true})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		f, err := s.mangaMod().anilist.Get(ctx, req.AniListID)
		cancel()
		if err != nil {
			writeUpstreamError(w, "read the series from AniList", err)
			return
		}
		if f.IsNovel {
			writeError(w, http.StatusBadRequest, "That's a novel, and Manga keeps volumes of pictures. It belongs in Ebooks.")
			return
		}
		if f.IsAdult && !s.canSeeAdultManga(r) {
			writeError(w, http.StatusForbidden, "That series is for accounts allowed 18+ content.")
			return
		}
		info, aliases = f.AsSeries(), f.Aliases
		info.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	}
	info.MonitorNew = req.Want != "none"
	info.WantAll = req.Want == "all"
	if req.AllowFan {
		info.SourceMode = "fan"
	}
	sr, err := repo.AddSeries(info, aliases)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The series couldn't be added.")
		return
	}
	for n := 1; n <= sr.VolumeCount; n++ {
		_, _, _ = repo.EnsureVolume(sr.ID, strconv.Itoa(n), info.WantAll)
	}
	found := s.scanManga(r.Context(), sr.ID).Claimed // files already in the folder
	if (req.Chapters == manga.FollowNew || req.Chapters == manga.FollowAll) && s.mangaSourcesFor(sr).MangaDex && isAdminRequest(r) {
		s.followMangaChapters(sr, req.Chapters)
	}
	if sr.WantAll {
		s.background(func() {
			s.pacedAddLook(func(ctx context.Context) {
				s.lookForManga(ctx, sr.ID, queue.PriorityAutomatic, maxMangaGrabsAtOnce)
			})
		})
	}
	sr, _ = repo.GetSeries(sr.ID)
	writeJSON(w, http.StatusOK, map[string]any{"series": sr, "volumes": sr.Total, "found": found})
}

// PUT /api/manga/{id} {"monitorNew", "wantAll", "sourceMode"}
func (s *Server) handleUpdateManga(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		MonitorNew *bool   `json:"monitorNew"`
		SourceMode *string `json:"sourceMode"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	repo := s.mangaRepo()
	if req.SourceMode != nil {
		if m := *req.SourceMode; m != "" && m != "official" && m != "fan" {
			writeError(w, http.StatusBadRequest, "That isn't a choice of sources.")
			return
		}
		if err := repo.SetSourceMode(sr.ID, *req.SourceMode); err != nil {
			writeError(w, http.StatusInternalServerError, "The choice couldn't be saved.")
			return
		}
		_ = repo.ResetBackoff(sr.ID)
	}
	if req.MonitorNew != nil {
		if err := repo.SetMonitorNew(sr.ID, *req.MonitorNew); err != nil {
			writeError(w, http.StatusInternalServerError, "The choice couldn't be saved.")
			return
		}
	}
	sr, _ = repo.GetSeries(sr.ID)
	writeJSON(w, http.StatusOK, s.mangaForPage(sr, true))
}

// DELETE /api/manga/{id}: out of the library; its files stay.
func (s *Server) handleDeleteManga(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	if err := s.mangaRepo().DeleteSeries(sr.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "The series couldn't be removed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": sr.Title + " was removed from the library. Its files are untouched."})
}

// refreshMangaSeries asks AniList about a series again. More volumes than before
// are new rows, wanted when the series follows new volumes, and a reason to look again.
func (s *Server) refreshMangaSeries(ctx context.Context, sr manga.Series) (added int, err error) {
	repo := s.mangaRepo()
	if sr.AniListID <= 0 {
		_ = repo.MarkChecked(sr.ID)
		return 0, nil
	}
	f, err := s.mangaMod().anilist.Get(ctx, sr.AniListID)
	if err != nil {
		return 0, err
	}
	if err := repo.UpdateDetails(sr.ID, f.AsSeries(), f.Aliases); err != nil {
		return 0, err
	}
	before, _ := repo.Volumes(sr.ID)
	highest := -1.0
	for _, v := range before {
		highest = max(highest, manga.SortKey(v.Number))
	}
	var found []manga.Volume
	for n := 1; n <= f.Volumes; n++ {
		if v, isNew, err := repo.EnsureVolume(sr.ID, strconv.Itoa(n), sr.MonitorNew); err == nil && isNew {
			added++
			found = append(found, v)
		}
	}
	if added > 0 {
		_ = repo.ResetBackoff(sr.ID)
		s.noteNewMangaVolumes(sr, manga.NewVolumesAfter(found, highest, len(before) > 0))
	}
	return added, nil
}

// POST /api/manga/{id}/refresh: AniList again, and the series' folder.
func (s *Server) handleRefreshManga(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	added, err := s.refreshMangaSeries(ctx, sr)
	if err != nil {
		writeUpstreamError(w, "refresh the series from AniList", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "found": s.scanManga(ctx, sr.ID).Claimed})
}

// POST /api/manga/{id}/volumes {"number"}: a volume AniList doesn't list yet.
func (s *Server) handleAddMangaVolume(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Number string `json:"number"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(req.Number), 64)
	if err != nil || n < 0 || n > 9999 {
		writeError(w, http.StatusBadRequest, "Give a volume number, like 12 or 12.5.")
		return
	}
	v, isNew, err := s.mangaRepo().EnsureVolume(sr.ID, strings.TrimSpace(req.Number), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The volume couldn't be added.")
		return
	}
	if !isNew {
		writeError(w, http.StatusConflict, fmt.Sprintf("Volume %s is already on the list.", v.Number))
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// PUT /api/manga/volumes/wanted {"ids":[...], "wanted":true}
func (s *Server) handleSetMangaWanted(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []int64 `json:"ids"`
		Wanted bool    `json:"wanted"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 || len(req.IDs) > 5000 {
		writeError(w, http.StatusBadRequest, "Choose some volumes.")
		return
	}
	n, err := s.mangaRepo().SetVolumesWanted(req.IDs, req.Wanted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The volumes couldn't be changed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"changed": n})
}

// GET /api/manga/volumes/{id}/file: the volume's file, to save or open elsewhere.
func (s *Server) handleMangaVolumeFile(w http.ResponseWriter, r *http.Request) {
	v, _, ok := s.mangaVolumeFromPath(w, r)
	if !ok {
		return
	}
	if v.FilePath == "" {
		writeError(w, http.StatusNotFound, "This volume has no file yet.")
		return
	}
	f, err := os.Open(v.FilePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "The file isn't there any more.")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, "The file isn't there any more.")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(v.FilePath)))
	http.ServeContent(w, r, filepath.Base(v.FilePath), st.ModTime(), f)
}

// ---- covers ----

// mangaImage turns an AniList cover address into the proxied one the page uses.
func (s *Server) mangaImage(u string) string {
	if u == "" || strings.HasPrefix(u, "/api/manga/image") {
		return u
	}
	s.mangaMod().images.Store(u, struct{}{})
	return "/api/manga/image?u=" + url.QueryEscape(u)
}

func (s *Server) mangaImageKnown(u string) bool {
	if _, ok := s.mangaMod().images.Load(u); ok {
		return true
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM manga_series WHERE cover_url = ?`, u).Scan(&n)
	return n > 0
}

var mangaImageClient = &http.Client{Timeout: 20 * time.Second}

// GET /api/manga/image?u=: an AniList cover the library or a lookup knows, for the page.
func (s *Server) handleMangaImage(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	st := s.mangaMod()
	allowed := strings.HasPrefix(u, "https://s4.anilist.co/") || (st.imgHost != "" && strings.HasPrefix(u, st.imgHost))
	if u == "" || len(u) > 2048 || !allowed || !s.mangaImageKnown(u) {
		writeError(w, http.StatusNotFound, "Unknown image.")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad image address.")
		return
	}
	req.Header.Set("User-Agent", "Usherr/"+s.version)
	resp, err := mangaImageClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "The image couldn't be fetched.")
		return
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") || strings.Contains(ct, "svg") {
		writeError(w, http.StatusBadGateway, "The image couldn't be fetched.")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 15<<20))
}
