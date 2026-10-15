package api

// Home fork: the Scenes module. Adult scenes from StashDB (or another
// stash-box server), followed by performer or studio, searched on the
// indexers' XXX categories and filed into their own folder.
//
// Privacy: every route is for administrators only, and scene downloads and
// their activity are left out of what everyone else sees (queue, dashboard,
// activity). Scenes never send notifications.
//
// Everything lives in this file and internal/scenes; upstream files only get
// one-line hooks (see the "scenes hook" comments), so merging upstream stays
// easy.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
	"github.com/rdborg/mediarium/internal/scenes"
)

// Settings keys of the module.
const (
	keyScenesEnabled = "scenes.enabled"      // "1" when the module is on
	keyScenesPath    = "scenes.path"         // library folder; SCENES_DIR when unset
	keyStashBoxURL   = "scenes.stashbox_url" // GraphQL address; StashDB when unset
	keyStashBoxKey   = "scenes.stashbox_key" // encrypted
)

// sceneCategories are the Newznab/Torznab XXX categories.
var sceneCategories = []int{6000, 6010, 6020, 6030, 6040, 6045, 6050, 6060, 6070, 6080, 6090}

// Activity event types of the module; hidden from everyone but administrators.
const (
	sceneEventGrabbed  = "scene-grabbed"
	sceneEventImported = "scene-imported"
	sceneEventFailed   = "scene-failed"
	sceneEventSearched = "scene-searched"
)

func isSceneEvent(eventType string) bool { return strings.HasPrefix(eventType, "scene-") }

// sceneState is the module's part of the Server.
type sceneState struct {
	once   sync.Once
	repo   *scenes.Repo
	stash  *scenes.StashBox
	check  sync.Mutex // one follow check at a time
	fill   sync.Mutex // one details fill at a time
	images sync.Map   // image URLs StashDB answered with, which /api/scenes/image may fetch
}

var sceneStates sync.Map // *Server -> *sceneState

func (s *Server) sceneMod() *sceneState {
	v, _ := sceneStates.LoadOrStore(s, &sceneState{})
	st := v.(*sceneState)
	st.once.Do(func() {
		st.repo = scenes.NewRepo(s.db)
		st.stash = scenes.NewStashBox("Mediarium-home/"+s.version, func() string {
			k, _ := s.Settings.Get(keyStashBoxKey)
			return k
		})
	})
	if u, _ := s.Settings.Get(keyStashBoxURL); u != "" {
		st.stash.Endpoint = u
	} else {
		st.stash.Endpoint = scenes.DefaultStashBox
	}
	return st
}

func (s *Server) sceneRepo() *scenes.Repo    { return s.sceneMod().repo }
func (s *Server) stashBox() *scenes.StashBox { return s.sceneMod().stash }
func (s *Server) scenesEnabled() bool {
	v, _ := s.Settings.Get(keyScenesEnabled)
	return v == "1" && adultAllowed() // Usherr: locked unless the server allows it
}

// Usherr: the adult module's lock. Whoever runs the server unlocks it by
// setting USHERR_ALLOW_ADULT=1 in the container's environment; until then
// it can't be switched on from the page, and is off even if it was on.
// Switching it on also takes typing adultConfirmPhrase.
const (
	adultEnv           = "USHERR_ALLOW_ADULT"
	adultConfirmPhrase = "I am 18 or older"
)

func adultAllowed() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(adultEnv)))
	return v == "1" || v == "true" || v == "yes"
}

// usherrAdultConfirm asks for the typed phrase (a variable so the fork's
// older tests, which switch it on in one go, still run).
var usherrAdultConfirm = true

func sceneLink(id int64) string { return fmt.Sprintf("/scenes/%d", id) }
func (s *Server) stashKeySet() bool {
	k, _ := s.Settings.Get(keyStashBoxKey)
	return strings.TrimSpace(k) != ""
}
func (s *Server) scenesRoot() string {
	if v, _ := s.Settings.Get(keyScenesPath); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(os.Getenv("SCENES_DIR"))
}

// ---- hooks called from upstream files ----

// registerSceneRoutes adds the module's routes, all for administrators.
func (s *Server) registerSceneRoutes(admin, member routeGroup) {
	// home fork: accounts given the NSFW 18+ switch browse, watch, find and add
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes/enabled", s.handleScenesEnabled)
	on := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !s.scenesEnabled() {
				writeError(w, http.StatusNotFound, "Scenes are switched off. Switch them on in Settings > Scenes.")
				return
			}
			h(w, r)
		}
	}
	admin.HandleFunc("GET /api/scenes/settings", s.handleGetSceneSettings)
	admin.HandleFunc("GET /api/home/season-folder", s.handleSeasonFolderStyle) // home fork: TV season folder style
	admin.HandleFunc("PUT /api/home/season-folder", s.handleSeasonFolderStyle)
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes/image", on(s.handleSceneImage))
	admin.HandleFunc("PUT /api/scenes/settings", s.handlePutSceneSettings)
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes", revalidated(on(s.handleListScenes)))
	member.Need(auth.PermNSFW).HandleFunc("POST /api/scenes", on(s.handleAddScene))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes/lookup", on(s.handleSceneLookup))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes/browse", on(s.handleSceneBrowse))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes/discover", on(s.handleSceneDiscover)) // home fork: Find with filters
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes/tags", on(s.handleSceneTags))
	admin.HandleFunc("POST /api/scenes/check", on(s.handleSceneCheckNow))
	admin.HandleFunc("POST /api/scenes/fill-details", on(s.handleFillSceneDetails))
	admin.HandleFunc("POST /api/scenes/bulk", on(s.handleSceneBulk))
	admin.HandleFunc("GET /api/scenes/search-status", on(s.handleSceneSearchStatus))
	admin.HandleFunc("POST /api/scenes/search-now", on(s.handleSceneSearchNow))
	admin.HandleFunc("GET /api/scenes/schedule", on(s.handleSceneSchedule))
	admin.HandleFunc("PUT /api/scenes/schedule", on(s.handleSceneSchedule))
	admin.HandleFunc("GET /api/home/schedules", s.handleSchedules) // home fork: schedules
	admin.HandleFunc("PUT /api/home/schedules", s.handleSchedules)
	admin.HandleFunc("POST /api/home/quiet", s.handleQuiet)
	admin.HandleFunc("POST /api/scenes/import/whisparr", on(s.handleImportWhisparr))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes/performers", on(s.handleListScenePerformers))
	member.Need(auth.PermNSFW).HandleFunc("POST /api/scenes/performers", on(s.handleFollowPerformer))
	member.Need(auth.PermNSFW).HandleFunc("DELETE /api/scenes/performers/{id}", on(s.handleUnfollowPerformer))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes/studios", on(s.handleListSceneStudios))
	member.Need(auth.PermNSFW).HandleFunc("POST /api/scenes/studios", on(s.handleFollowStudio))
	member.Need(auth.PermNSFW).HandleFunc("DELETE /api/scenes/studios/{id}", on(s.handleUnfollowStudio))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes/{id}", on(s.handleGetScene))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/scenes/{id}/stream", on(s.handleSceneStream))
	member.Need(auth.PermNSFW).HandleFunc("PUT /api/scenes/{id}/wanted", on(s.handleSetSceneWanted))
	admin.HandleFunc("DELETE /api/scenes/{id}", on(s.handleDeleteScene))
	admin.HandleFunc("POST /api/scenes/{id}/search", on(s.handleSearchSceneNow))
	admin.HandleFunc("GET /api/scenes/{id}/releases", on(s.handleSceneReleases))
	admin.HandleFunc("POST /api/scenes/{id}/grab", on(s.handleGrabScene))
}

// Everything about scenes stays behind the Scenes page, for every account,
// administrators included: shared views (queue, history, dashboard, activity,
// blocklist, logs, statistics, manual import) never show scene downloads.
// The Scenes page asks for its own view with ?scenes=only (administrators).

// scenesOnly reports whether r asks for the Scenes page's own view.
func scenesOnly(r *http.Request) bool {
	return r.URL.Query().Get("scenes") == "only" && isAdminRequest(r)
}

// sceneHider reports whether a download is a scene download (to leave out).
func (s *Server) sceneHider(r *http.Request) func(queueID int64) bool {
	ids, err := s.sceneRepo().SceneDownloadIDs()
	if err != nil {
		return func(int64) bool { return false }
	}
	return func(id int64) bool { return ids[id] }
}

// hideSceneItems keeps only the downloads this view is for: everything but
// scenes, or (Scenes page) only scenes.
func (s *Server) hideSceneItems(r *http.Request, items []queue.Item) []queue.Item {
	isScene := s.sceneHider(r)
	only := scenesOnly(r)
	out := items[:0]
	for _, it := range items {
		if isScene(it.ID) == only {
			out = append(out, it)
		}
	}
	return out
}

// hideActivity reports whether an activity entry is left out of this view.
func hideActivity(r *http.Request, eventType string) bool {
	return isSceneEvent(eventType) != scenesOnly(r)
}

// sceneBlocklistTag marks blocklist entries that belong to scenes.
const sceneBlocklistTag = "[scenes] "

// hideBlocklistEntry reports whether a blocklist entry is left out of this view.
func hideBlocklistEntry(r *http.Request, reason string) bool {
	return strings.HasPrefix(reason, sceneBlocklistTag) != scenesOnly(r)
}

// sceneFailuresSince counts failed scene downloads, so shared health
// warnings don't count them.
func (s *Server) sceneFailuresSince(since time.Time) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM download_queue q JOIN scene_downloads d ON d.queue_id = q.id
		WHERE q.status = 'failed' AND q.completed_at >= ?`, since.UTC().Format(time.RFC3339Nano)).Scan(&n)
	return n
}

// pausedSceneDownloads counts scene downloads that are paused right now.
func (s *Server) pausedSceneDownloads() int64 {
	var n int64
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM download_queue q JOIN scene_downloads d ON d.queue_id = q.id WHERE q.status = 'paused'`).Scan(&n)
	return n
}

// isSceneWorkDir reports whether path is the working folder of a scene
// download ("<downloads>/incomplete/queue-12"), which manual import skips.
func (s *Server) isSceneWorkDir(path string) bool {
	name := filepath.Base(path)
	if !strings.HasPrefix(name, "queue-") {
		return false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(name, "queue-"), 10, 64)
	return err == nil && s.sceneRepo().IsSceneDownload(id)
}

// describeSceneItem names a scene download in the queue and dashboard.
func (s *Server) describeSceneItem(queueID int64) (title, subtitle, poster string, ok bool) {
	sc, err := s.sceneRepo().SceneForDownload(queueID)
	if err != nil {
		return "", "", "", false
	}
	sub := "Scene"
	if sc.StudioName != "" {
		sub += " · " + sc.StudioName
	}
	t := sc.Title
	if t == "" {
		t = sc.Name()
	}
	return t, sub, sc.ImageURL, true
}

// prepareScenePipeline is preparePipeline's case for a scene download.
// ok is false when the download isn't a scene download.
func (s *Server) prepareScenePipeline(item queue.Item, protocol indexers.Protocol) (run func(), ok bool, err error) {
	repo := s.sceneRepo()
	// The link is written right after the download is queued; give it a
	// moment in case the line picked the item up in between.
	var sc scenes.Scene
	for i := 0; i < 20; i++ {
		sc, err = repo.SceneForDownload(item.ID)
		if !errors.Is(err, scenes.ErrNotFound) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if errors.Is(err, scenes.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, errors.New("That scene isn't in your library any more.")
	}
	_ = repo.SetState(sc.ID, scenes.StatusDownloading, "", "")
	return func() {
		if err := s.runScenePipeline(item.ID, sc, item.ReleaseTitle, item.NZBURL, protocol); err != nil {
			slog.Info("queue: scene download ended", "queueId", item.ID, "err", err)
		}
	}, true, nil
}

// ---- settings ----

type sceneSettings struct {
	Enabled  bool   `json:"enabled"`
	Allowed  bool   `json:"allowed"` // Usherr: the server allows it (USHERR_ALLOW_ADULT)
	Path     string `json:"path"`
	Endpoint string `json:"endpoint"`
	KeySet   bool   `json:"keySet"`
}

func (s *Server) currentSceneSettings() sceneSettings {
	ep, _ := s.Settings.Get(keyStashBoxURL)
	if ep == "" {
		ep = scenes.DefaultStashBox
	}
	return sceneSettings{Enabled: s.scenesEnabled(), Allowed: adultAllowed(), Path: s.scenesRoot(), Endpoint: ep, KeySet: s.stashKeySet()}
}

// GET /api/scenes/settings
func (s *Server) handleGetSceneSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.currentSceneSettings())
}

// PUT /api/scenes/settings {"enabled":true,"path":"/data/scenes","endpoint":"...","key":"..."}.
// Only the fields sent change. A key is checked with the server before it is
// saved (encrypted); "removeKey":true removes it.
func (s *Server) handlePutSceneSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled   *bool   `json:"enabled"`
		Path      *string `json:"path"`
		Endpoint  *string `json:"endpoint"`
		Key       *string `json:"key"`
		RemoveKey bool    `json:"removeKey"`
		Confirm   string  `json:"confirm"` // Usherr: the typed phrase, to switch it on
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.Enabled != nil && *req.Enabled { // Usherr: the lock and the double confirm
		if !adultAllowed() {
			writeError(w, http.StatusForbidden, "NSFW 18+ is locked on this server. Whoever runs it can unlock it by adding "+adultEnv+"=1 to the container's environment and restarting it.")
			return
		}
		if usherrAdultConfirm && !strings.EqualFold(strings.TrimSpace(req.Confirm), adultConfirmPhrase) {
			writeError(w, http.StatusBadRequest, "Type "+adultConfirmPhrase+" to confirm.")
			return
		}
	}
	if req.Path != nil {
		p := strings.TrimSpace(*req.Path)
		if p != "" && !filepath.IsAbs(p) {
			writeError(w, http.StatusBadRequest, "The scenes folder must be a full path inside the container, like /data/scenes.")
			return
		}
		if err := s.Settings.Set(keyScenesPath, p, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The folder couldn't be saved.")
			return
		}
	}
	if req.Endpoint != nil {
		ep := strings.TrimSpace(*req.Endpoint)
		if ep != "" && !strings.HasPrefix(ep, "https://") && !strings.HasPrefix(ep, "http://") {
			writeError(w, http.StatusBadRequest, "The stash-box address must start with https://, like https://stashdb.org/graphql.")
			return
		}
		if ep == scenes.DefaultStashBox {
			ep = ""
		}
		if err := s.Settings.Set(keyStashBoxURL, ep, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The address couldn't be saved.")
			return
		}
	}
	if req.RemoveKey {
		if err := s.Settings.Set(keyStashBoxKey, "", true); err != nil {
			writeError(w, http.StatusInternalServerError, "The key couldn't be removed.")
			return
		}
	} else if req.Key != nil {
		key := strings.TrimSpace(*req.Key)
		if key == "" || len(key) > 4096 || strings.ContainsAny(key, " \r\n\t") {
			writeError(w, http.StatusBadRequest, "That doesn't look like an API key. Copy it again from your StashDB profile.")
			return
		}
		test := *s.stashBox()
		test.Key = func() string { return key }
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := test.Test(ctx); err != nil {
			if errors.Is(err, scenes.ErrKeyRefused) {
				writeError(w, http.StatusBadRequest, err.Error()+".")
				return
			}
			writeUpstreamError(w, "check the key with StashDB", err)
			return
		}
		if err := s.Settings.Set(keyStashBoxKey, key, true); err != nil {
			writeError(w, http.StatusInternalServerError, "The key couldn't be saved.")
			return
		}
	}
	if req.Enabled != nil {
		v := ""
		if *req.Enabled {
			v = "1"
		}
		if err := s.Settings.Set(keyScenesEnabled, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The switch couldn't be saved.")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.currentSceneSettings())
}

// ---- library ----

func (s *Server) sceneFromPath(w http.ResponseWriter, r *http.Request) (scenes.Scene, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a scene id.")
		return scenes.Scene{}, false
	}
	sc, err := s.sceneRepo().GetScene(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That scene isn't in your library.")
		return sc, false
	}
	return sc, true
}

// GET /api/scenes?status=wanted|downloaded&performer=<stashId>&studio=<stashId>
func (s *Server) handleListScenes(w http.ResponseWriter, r *http.Request) {
	repo := s.sceneRepo()
	var list []scenes.Scene
	var err error
	q := r.URL.Query()
	switch {
	case q.Get("performer") != "":
		list, err = repo.ScenesWithPerformer(q.Get("performer"))
	case q.Get("studio") != "":
		list, err = repo.ScenesFromStudio(q.Get("studio"))
	default:
		list, err = repo.ListScenes()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if st := q.Get("status"); st != "" {
		kept := list[:0]
		for _, sc := range list {
			if (st == "wanted" && sc.Wanted && sc.Status != scenes.StatusDownloaded) ||
				(st == "notwanted" && !sc.Wanted && sc.Status != scenes.StatusDownloaded) || sc.Status == st {
				kept = append(kept, sc)
			}
		}
		list = kept
	}
	counts, _ := repo.Count()
	writeJSON(w, http.StatusOK, map[string]any{"counts": counts, "scenes": list})
}

// GET /api/scenes/{id}
func (s *Server) handleGetScene(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.sceneFromPath(w, r)
	if !ok {
		return
	}
	busy, _ := s.sceneRepo().HasActiveDownload(sc.ID)
	writeJSON(w, http.StatusOK, map[string]any{"scene": sc, "downloading": busy})
}

// POST /api/scenes {"stashId":"...","search":true}: adds a scene from StashDB.
func (s *Server) handleAddScene(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StashID string `json:"stashId"`
		Search  bool   `json:"search"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.StashID) == "" {
		writeError(w, http.StatusBadRequest, "Say which scene to add (its StashDB id).")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	found, err := s.stashBox().FindScene(ctx, strings.TrimSpace(req.StashID))
	if err != nil {
		s.writeStashError(w, err)
		return
	}
	found.AddedBy = scenes.AddedManual
	found.Wanted = true
	sc, _, err := s.sceneRepo().AddScene(found)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Search {
		id := sc.ID
		s.background(func() { s.searchScene(id, false, queue.PriorityManual) })
	}
	writeJSON(w, http.StatusOK, sc)
}

// PUT /api/scenes/{id}/wanted {"wanted":false}
func (s *Server) handleSetSceneWanted(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.sceneFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Wanted bool `json:"wanted"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if err := s.sceneRepo().SetWanted(sc.ID, req.Wanted); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sc.Wanted = req.Wanted
	writeJSON(w, http.StatusOK, sc)
}

// DELETE /api/scenes/{id}: removes the scene from the library. Its file
// stays on disk; Mediarium never deletes a scene's file.
func (s *Server) handleDeleteScene(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.sceneFromPath(w, r)
	if !ok {
		return
	}
	if busy, _ := s.sceneRepo().HasActiveDownload(sc.ID); busy {
		writeError(w, http.StatusConflict, "A download for this scene is running. Stop it in Activity first.")
		return
	}
	if err := s.sceneRepo().DeleteScene(sc.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/scenes/bulk {"ids":[1,2],"action":"want"|"unwant"|"removeEmpty"}.
// removeEmpty only removes scenes without a file; files are never deleted.
func (s *Server) handleSceneBulk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []int64 `json:"ids"`
		Action string  `json:"action"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "Select some scenes first.")
		return
	}
	if len(req.IDs) > 20000 {
		writeError(w, http.StatusBadRequest, "That is too many at once.")
		return
	}
	repo := s.sceneRepo()
	var res scenes.BulkResult
	var err error
	switch req.Action {
	case "want":
		res, err = repo.SetWantedMany(req.IDs, true)
	case "unwant":
		res, err = repo.SetWantedMany(req.IDs, false)
	case "removeEmpty":
		res, err = repo.RemoveEmpty(req.IDs)
	case "search":
		// Wanted scenes without a file are searched in the background;
		// the others are skipped (downloaded, downloading, not wanted).
		ok := s.searchableScenes(req.IDs)
		queued := s.queueSceneSearches(ok)
		res = scenes.BulkResult{Changed: queued, Skipped: len(req.IDs) - queued}
	default:
		writeError(w, http.StatusBadRequest, `Choose "want", "unwant", "removeEmpty" or "search".`)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GET /api/scenes/{id}/stream: plays the scene's file in the browser, with
// seeking. Only the scene's own file inside the scenes folder is served.
func (s *Server) handleSceneStream(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.sceneFromPath(w, r)
	if !ok {
		return
	}
	serveLibraryVideo(w, r, sc.FilePath, []string{s.scenesRoot()})
}

// ---- StashDB lookups ----

func (s *Server) writeStashError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, scenes.ErrNoKey), errors.Is(err, scenes.ErrKeyRefused):
		writeError(w, http.StatusBadRequest, err.Error()+".")
	case errors.Is(err, scenes.ErrNotFound):
		writeError(w, http.StatusNotFound, "StashDB doesn't know that one.")
	default:
		writeUpstreamError(w, "ask StashDB", err)
	}
}

type lookedUpScene struct {
	scenes.Scene
	InLibrary bool  `json:"inLibrary"`
	LibraryID int64 `json:"libraryId,omitempty"`
}

func (s *Server) markInLibrary(list []scenes.Scene) []lookedUpScene {
	out := make([]lookedUpScene, 0, len(list))
	for _, sc := range list {
		s.allowSceneImage(sc.ImageURL)
		l := lookedUpScene{Scene: sc}
		if have, err := s.sceneRepo().GetSceneByStashID(sc.StashID); err == nil {
			l.InLibrary, l.LibraryID, l.Scene.Status, l.Scene.Wanted = true, have.ID, have.Status, have.Wanted
		}
		out = append(out, l)
	}
	return out
}

// GET /api/scenes/lookup?kind=scene|performer|studio&q=...
func (s *Server) handleSceneLookup(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 200 {
		writeError(w, http.StatusBadRequest, "Type something to look for.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	sb := s.stashBox()
	switch r.URL.Query().Get("kind") {
	case scenes.KindPerformer:
		found, err := sb.SearchPerformers(ctx, q)
		if err != nil {
			s.writeStashError(w, err)
			return
		}
		followed := map[string]bool{}
		if list, err := s.sceneRepo().Performers(true); err == nil {
			for _, p := range list {
				followed[p.StashID] = true
			}
		}
		for i := range found {
			found[i].Followed = followed[found[i].StashID]
			s.allowSceneImage(found[i].ImageURL)
		}
		writeJSON(w, http.StatusOK, map[string]any{"performers": found})
	case scenes.KindStudio:
		found, err := sb.SearchStudios(ctx, q)
		if err != nil {
			s.writeStashError(w, err)
			return
		}
		followed := map[string]bool{}
		if list, err := s.sceneRepo().Studios(true); err == nil {
			for _, st := range list {
				followed[st.StashID] = true
			}
		}
		for i := range found {
			found[i].Followed = followed[found[i].StashID]
			s.allowSceneImage(found[i].ImageURL)
		}
		writeJSON(w, http.StatusOK, map[string]any{"studios": found})
	default:
		found, err := sb.SearchScenes(ctx, q)
		if err != nil {
			s.writeStashError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"scenes": s.markInLibrary(found)})
	}
}

// GET /api/scenes/browse?kind=performer|studio&stashId=...&page=1: a
// performer's or studio's scenes on StashDB, marked when in the library.
func (s *Server) handleSceneBrowse(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	id := strings.TrimSpace(r.URL.Query().Get("stashId"))
	if (kind != scenes.KindPerformer && kind != scenes.KindStudio) || id == "" {
		writeError(w, http.StatusBadRequest, "Say whose scenes to show: kind=performer or studio, and a stashId.")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res, err := s.stashBox().ScenesOf(ctx, kind, id, page, 40, "")
	if err != nil {
		s.writeStashError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": res.Count, "scenes": s.markInLibrary(res.Scenes)})
}

// ---- following ----

// POST /api/scenes/performers {"stashId":"...","backfill":false}: follows a
// performer. New scenes from now on are added as wanted; with backfill, the
// scenes already out are added too.
func (s *Server) handleFollowPerformer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StashID  string `json:"stashId"`
		Backfill bool   `json:"backfill"`
		Search   bool   `json:"search"` // search the wanted, missing scenes now
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.StashID) == "" {
		writeError(w, http.StatusBadRequest, "Say which performer to follow (their StashDB id).")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	p, err := s.stashBox().FindPerformer(ctx, strings.TrimSpace(req.StashID))
	if err != nil {
		s.writeStashError(w, err)
		return
	}
	saved, err := s.sceneRepo().SavePerformer(p, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	added := 0
	if req.Backfill {
		// Add the whole catalogue now (not left to the next follow check,
		// which could skip it), then check from today on.
		bctx, bcancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer bcancel()
		n, err := s.backfillSource(bctx, scenes.KindPerformer, saved.StashID, scenes.AddedPerformer)
		if err != nil && n == 0 {
			s.writeStashError(w, err)
			return
		}
		added = n
	}
	_ = s.sceneRepo().PerformerChecked(saved.ID)
	saved.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	searching := 0
	if req.Search {
		searching = s.searchSourceScenes(scenes.KindPerformer, saved.StashID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"performer": saved, "added": added, "searching": searching})
}

// DELETE /api/scenes/performers/{id}: stops following. Scenes already added stay.
func (s *Server) handleUnfollowPerformer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a performer id.")
		return
	}
	if err := s.sceneRepo().SetPerformerFollowed(id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/scenes/performers?all=1
func (s *Server) handleListScenePerformers(w http.ResponseWriter, r *http.Request) {
	list, err := s.sceneRepo().Performers(r.URL.Query().Get("all") == "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/scenes/studios {"stashId":"...","backfill":false}
func (s *Server) handleFollowStudio(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StashID  string `json:"stashId"`
		Backfill bool   `json:"backfill"`
		Search   bool   `json:"search"` // search the wanted, missing scenes now
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.StashID) == "" {
		writeError(w, http.StatusBadRequest, "Say which studio to follow (its StashDB id).")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	st, err := s.stashBox().FindStudio(ctx, strings.TrimSpace(req.StashID))
	if err != nil {
		s.writeStashError(w, err)
		return
	}
	saved, err := s.sceneRepo().SaveStudio(st, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	added := 0
	if req.Backfill {
		bctx, bcancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer bcancel()
		n, err := s.backfillSource(bctx, scenes.KindStudio, saved.StashID, scenes.AddedStudio)
		if err != nil && n == 0 {
			s.writeStashError(w, err)
			return
		}
		added = n
	}
	_ = s.sceneRepo().StudioChecked(saved.ID)
	saved.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	searching := 0
	if req.Search {
		searching = s.searchSourceScenes(scenes.KindStudio, saved.StashID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"studio": saved, "added": added, "searching": searching})
}

// backfillSource adds every scene of a performer or studio as wanted (up to
// 3,000, newest first) and reports how many were new. Scenes already in the
// library keep their wanted flag and status.
func (s *Server) backfillSource(ctx context.Context, kind, stashID, addedBy string) (int, error) {
	repo, sb := s.sceneRepo(), s.stashBox()
	added := 0
	for page := 1; page <= 30; page++ {
		res, err := sb.ScenesOf(ctx, kind, stashID, page, 100, "")
		if err != nil {
			return added, err
		}
		for _, sc := range res.Scenes {
			sc.AddedBy, sc.Wanted = addedBy, true
			if _, isNew, err := repo.AddScene(sc); err == nil && isNew {
				added++
			}
		}
		if len(res.Scenes) == 0 || page*100 >= res.Count {
			break
		}
	}
	if added > 0 {
		slog.Info("scenes: added a catalogue", "kind", kind, "added", added)
	}
	return added, nil
}

// DELETE /api/scenes/studios/{id}
func (s *Server) handleUnfollowStudio(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a studio id.")
		return
	}
	if err := s.sceneRepo().SetStudioFollowed(id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/scenes/studios?all=1
func (s *Server) handleListSceneStudios(w http.ResponseWriter, r *http.Request) {
	list, err := s.sceneRepo().Studios(r.URL.Query().Get("all") == "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/scenes/check: looks for new scenes of followed performers and
// studios now, instead of waiting for the next automatic run.
func (s *Server) handleSceneCheckNow(w http.ResponseWriter, r *http.Request) {
	s.background(func() { s.checkFollowed(context.Background()) })
	writeJSON(w, http.StatusAccepted, map[string]any{"message": "Looking for new scenes now."})
}

// sinceDate is the date a follow check starts from: the day before the last
// check (so a scene dated late on that day isn't missed), or "" for all.
func sinceDate(checkedAt string) string {
	if checkedAt == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, checkedAt)
	if err != nil {
		if t, err = time.Parse("2006-01-02T15:04:05.000Z", checkedAt); err != nil {
			return ""
		}
	}
	return t.AddDate(0, 0, -2).Format("2006-01-02")
}

// checkFollowed adds the new scenes of followed performers and studios as
// wanted. A first check of a follow made with backfill adds everything; at
// most a few pages per source per run, so a big back catalogue fills in over
// a few runs instead of all at once.
func (s *Server) checkFollowed(ctx context.Context) {
	if !s.scenesEnabled() || !s.stashKeySet() {
		return
	}
	st := s.sceneMod()
	if !st.check.TryLock() {
		return
	}
	defer st.check.Unlock()
	repo, sb := st.repo, st.stash
	added := 0
	addAll := func(kind, stashID, checkedAt, addedBy string) error {
		since := sinceDate(checkedAt)
		for page := 1; page <= 5; page++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			res, err := sb.ScenesOf(ctx, kind, stashID, page, 50, since)
			if err != nil {
				return err
			}
			for _, sc := range res.Scenes {
				sc.AddedBy, sc.Wanted = addedBy, true
				if _, isNew, err := repo.AddScene(sc); err == nil && isNew {
					added++
				}
			}
			if page*50 >= res.Count {
				break
			}
		}
		return nil
	}
	if performers, err := repo.Performers(true); err == nil {
		for _, p := range performers {
			if err := addAll(scenes.KindPerformer, p.StashID, p.CheckedAt, scenes.AddedPerformer); err != nil {
				slog.Info("scenes: follow check", "performer", p.Name, "err", err)
				continue
			}
			_ = repo.PerformerChecked(p.ID)
		}
	}
	if studios, err := repo.Studios(true); err == nil {
		for _, stu := range studios {
			if err := addAll(scenes.KindStudio, stu.StashID, stu.CheckedAt, scenes.AddedStudio); err != nil {
				slog.Info("scenes: follow check", "studio", stu.Name, "err", err)
				continue
			}
			_ = repo.StudioChecked(stu.ID)
		}
	}
	if added > 0 {
		slog.Info("scenes: new scenes from follows", "added", added)
	}
}

// huntScenes is the module's part of the automatic search: new scenes from
// follows first, then a few wanted scenes per run (the same limit of ten
// grabs per hour as everything else applies through the download line).
func (s *Server) huntScenes(ctx context.Context) {
	if !s.scenesEnabled() || !s.stashKeySet() {
		return
	}
	if s.taskAllowed(taskSceneFollow) {
		s.checkFollowed(ctx)
		s.fillSceneDetails(ctx, 60)
	}
	if !s.taskAllowed(taskSceneSearch) {
		return // the schedule holds automatic scene searches
	}
	wanted, err := s.sceneRepo().Wanted()
	if err != nil {
		return
	}
	now := time.Now()
	n := 0
	for _, sc := range wanted {
		if ctx.Err() != nil || n >= 10 {
			return
		}
		if !sc.Released(now) {
			continue
		}
		n++
		s.searchScene(sc.ID, false, queue.PriorityAutomatic)
	}
}

// fillSceneDetails reads tags and performer genders from StashDB for up to n
// scenes that don't have them yet (Whisparr imports), one at a time with a
// short pause, so StashDB isn't hammered. It reports how many it filled.
func (s *Server) fillSceneDetails(ctx context.Context, n int) int {
	if !s.scenesEnabled() || !s.stashKeySet() {
		return 0
	}
	st := s.sceneMod()
	if !st.fill.TryLock() {
		return 0
	}
	defer st.fill.Unlock()
	todo, err := st.repo.MissingDetails(n)
	if err != nil {
		return 0
	}
	done := 0
	for _, sc := range todo {
		if ctx.Err() != nil {
			break
		}
		fresh, err := st.stash.FindScene(ctx, sc.StashID)
		if errors.Is(err, scenes.ErrNotFound) {
			// Gone from StashDB: mark it read so it isn't asked for again.
			fresh = sc
			fresh.DetailsAt = time.Now().UTC().Format(time.RFC3339)
		} else if err != nil {
			slog.Info("scenes: fill details", "scene", sc.ID, "err", err)
			break
		}
		fresh.AddedBy = sc.AddedBy
		if _, _, err := st.repo.AddScene(fresh); err == nil {
			done++
		}
		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
		}
	}
	if done > 0 {
		slog.Info("scenes: filled in details", "scenes", done)
	}
	return done
}

// POST /api/scenes/fill-details: fills in tags and genders for every scene
// that lacks them, in the background.
func (s *Server) handleFillSceneDetails(w http.ResponseWriter, r *http.Request) {
	s.background(func() {
		for s.fillSceneDetails(context.Background(), 100) > 0 {
		}
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"message": "Getting tags and performers for your scenes now."})
}

// ---- searching and grabbing ----

// sceneRelease is a release found for a scene. It holds the real download
// URL, so it is never sent to the page as it is (see handleSceneReleases).
type sceneRelease struct {
	indexers.Result
	Quality string
	Score   int
	Matches bool
	Blocked bool
}

// sceneReleases searches the indexers for a scene and returns the releases
// that are for it, best first; with all, the others are included too,
// marked as not matching (for choosing by hand).
func (s *Server) sceneReleases(ctx context.Context, sc scenes.Scene, all bool) ([]sceneRelease, error) {
	list, err := s.searchableIndexers()
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("there are no indexers to search. Add one in Settings > Indexers & Search")
	}
	// Other scenes of the same studio around the same day tell apart
	// releases that only name the studio and the date.
	siblings, _ := s.sceneRepo().ScenesFromStudio(sc.StudioStashID)
	blocked := s.blockedKeys()
	seen := map[string]bool{}
	var out []sceneRelease
	for _, q := range scenes.SearchQueries(sc) {
		if ctx.Err() != nil {
			break
		}
		for _, res := range indexers.MergeResults(indexers.SearchAll(ctx, list, q, sceneCategories)) {
			if seen[res.DownloadURL] {
				continue
			}
			seen[res.DownloadURL] = true
			rel := scenes.ParseRelease(res.Title)
			sr := sceneRelease{Result: res, Quality: rel.Quality(), Score: scenes.MatchScore(rel, sc)}
			sr.Matches = scenes.Matches(rel, sc, siblings)
			sr.Blocked = blocked[blocklist.Key(res.Title)]
			if sr.Matches || all {
				out = append(out, sr)
			}
		}
		if !all && len(out) > 0 {
			break // the most specific search found it
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Matches != out[j].Matches {
			return out[i].Matches
		}
		qi, qj := scenes.QualityRank(scenes.ParseRelease(out[i].Title)), scenes.QualityRank(scenes.ParseRelease(out[j].Title))
		if qi != qj {
			return qi > qj
		}
		if out[i].Priority != out[j].Priority {
			return indexerRank(out[i].Priority) < indexerRank(out[j].Priority)
		}
		return out[i].Protocol == indexers.ProtocolUsenet && out[j].Protocol != indexers.ProtocolUsenet
	})
	return out, nil
}

// searchScene looks for a scene and grabs the best release that isn't
// blocklisted. It reports whether something was grabbed.
func (s *Server) searchScene(sceneID int64, retry bool, prio queue.Priority) bool {
	sc, err := s.sceneRepo().GetScene(sceneID)
	if err != nil || !sc.Wanted || (sc.Status == scenes.StatusDownloaded && !retry) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results, err := s.sceneReleases(ctx, sc, false)
	if err != nil {
		slog.Info("scenes: search", "scene", sceneID, "err", err)
		return false
	}
	sources := s.sourcesFor("")
	for _, res := range results {
		if res.Blocked || len(filterSources([]indexers.Result{res.Result}, sources)) == 0 {
			continue
		}
		_, err := s.grabScene(sc, res.Title, res.DownloadURL, res.SizeBytes, res.Protocol, prio)
		return err == nil
	}
	if retry || prio == queue.PriorityManual {
		_ = s.QueueRepo.LogActivity(0, sceneEventSearched, fmt.Sprintf("No release found for %s", sc.Name()))
	}
	return false
}

// grabScene puts a release of a scene in the download line.
func (s *Server) grabScene(sc scenes.Scene, title, url string, size int64, protocol indexers.Protocol, prio queue.Priority) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentsEnabled() {
		return 0, errTorrentsDisabled
	}
	if s.scenesRoot() == "" {
		return 0, errors.New("Choose the scenes folder in Settings > Scenes first.")
	}
	repo := s.sceneRepo()
	s.grabMu.Lock()
	busy, err := repo.HasActiveDownload(sc.ID)
	if err == nil && busy {
		err = errors.New("A download for this scene is already running or waiting.")
	}
	var id int64
	if err == nil {
		id, err = s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: title, NZBURL: url, SizeBytes: size, Protocol: queue.Protocol(protocol), Priority: prio})
	}
	if err == nil {
		if err = repo.LinkDownload(id, sc.ID); err != nil {
			_ = s.QueueRepo.SetStatus(id, queue.StatusFailed, "couldn't link the download to its scene")
		}
	}
	if err == nil {
		_ = repo.SetState(sc.ID, scenes.StatusDownloading, "", "")
	}
	s.grabMu.Unlock()
	if err != nil {
		return 0, err
	}
	_ = s.QueueRepo.LogActivity(0, sceneEventGrabbed, fmt.Sprintf("Downloading %s: %s", sc.Name(), title))
	s.dispatch.Kick()
	return id, nil
}

// POST /api/scenes/{id}/search
func (s *Server) handleSearchSceneNow(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.sceneFromPath(w, r)
	if !ok {
		return
	}
	if s.searchScene(sc.ID, sc.Status == scenes.StatusDownloaded, queue.PriorityManual) {
		writeJSON(w, http.StatusOK, map[string]any{"grabbed": 1, "message": "Found a release. It is downloading now."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grabbed": 0, "message": "Nothing suitable was found right now."})
}

// GET /api/scenes/{id}/releases?all=1
func (s *Server) handleSceneReleases(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.sceneFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	list, err := s.sceneReleases(ctx, sc, r.URL.Query().Get("all") != "")
	if err != nil {
		writeError(w, http.StatusBadGateway, plainerror.Message(err))
		return
	}
	// The real download URL can carry the indexer's API key: the page gets
	// an opaque reference instead, as everywhere else.
	type payload struct {
		searchResultPayload
		Quality string `json:"quality"`
		Score   int    `json:"score"`
		Matches bool   `json:"matches"`
	}
	out := make([]payload, 0, len(list))
	for _, rel := range list {
		p := payload{searchResultPayload: s.toSearchResultPayload(rel.Result), Quality: rel.Quality, Score: rel.Score, Matches: rel.Matches}
		p.Blocklisted = rel.Blocked
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/scenes/{id}/grab {"title","downloadUrl","sizeBytes","protocol"}
func (s *Server) handleGrabScene(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.sceneFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Title       string `json:"title"`
		DownloadURL string `json:"downloadUrl"`
		SizeBytes   int64  `json:"sizeBytes"`
		Protocol    string `json:"protocol"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Title == "" || req.DownloadURL == "" {
		writeError(w, http.StatusBadRequest, "Choose a release to download.")
		return
	}
	if !s.checkGrabURL(w, r, &req.DownloadURL) {
		return
	}
	protocol := indexers.Protocol(req.Protocol)
	if protocol == "" {
		protocol = indexers.ProtocolUsenet
	}
	id, err := s.grabScene(sc, req.Title, req.DownloadURL, req.SizeBytes, protocol, queue.PriorityManual)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queueId": id})
}

// retrySceneDownload retries a failed or stopped scene download: the same
// release is queued again for the same scene, and the old entry goes. It
// reports false when item isn't a scene download.
func (s *Server) retrySceneDownload(w http.ResponseWriter, item queue.Item) bool {
	sc, err := s.sceneRepo().SceneForDownload(item.ID)
	if errors.Is(err, scenes.ErrNotFound) {
		return false
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "That scene isn't in your library any more.")
		return true
	}
	// Like a movie retry: reuse what was already downloaded when it's still there.
	keptDir, reuse := s.keptDownload(item)
	// The new item must not start before the earlier files are in its folder.
	release := s.dispatch.Hold()
	newID, err := s.grabScene(sc, item.ReleaseTitle, item.NZBURL, item.SizeBytes, indexers.Protocol(item.Protocol), queue.PriorityManual)
	if err != nil {
		release()
		writeError(w, http.StatusConflict, err.Error())
		return true
	}
	if reuse {
		s.moveKeptDownload(keptDir, s.workDirFor(newID))
	}
	release()
	_ = s.QueueRepo.Delete(item.ID)
	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": newID})
	return true
}

// ---- the download pipeline ----

// sceneDest is where a scene's video goes: "<root>/<Studio>/<file name>.<ext>".
func (s *Server) sceneDest(sc scenes.Scene, videoFile string) (string, error) {
	root := s.scenesRoot()
	if root == "" {
		return "", errors.New("the scenes folder isn't set. Choose one in Settings > Scenes")
	}
	mode, repl := s.illegalCharSettings()
	ext := strings.ToLower(filepath.Ext(videoFile))
	return filepath.Join(root, organizer.Sanitize(scenes.Folder(sc), mode, repl), organizer.Sanitize(scenes.FileName(sc), mode, repl)+ext), nil
}

func (s *Server) runScenePipeline(queueID int64, sc scenes.Scene, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}
	repo := s.sceneRepo()
	restore := scenes.StatusMissing
	if sc.FilePath != "" && fileExists(sc.FilePath) {
		restore = scenes.StatusDownloaded
	}
	label := sc.Name()
	fail := func(stepErr error) error {
		if run.cancelled.Load() {
			_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, errRemovedFromLibrary.Error())
			return errRemovedFromLibrary
		}
		if handled, err := s.settleInterrupted(queueID, run); handled {
			return err
		}
		plain := plainerror.Message(stepErr)
		_ = s.QueueRepo.SetStatus(queueID, queue.StatusFailed, plain)
		// Not in the shared "Logs and errors": scene failures stay on the Scenes page.
		_ = repo.SetState(sc.ID, restore, "", "")
		_ = s.QueueRepo.LogActivity(0, sceneEventFailed, fmt.Sprintf("%s: %s", label, plain))
		if isBadRelease(stepErr) {
			if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: sceneBlocklistTag + stepErr.Error()}); err == nil {
				s.background(func() { s.searchScene(sc.ID, true, queue.PriorityAutomatic) })
			}
		}
		return stepErr
	}

	_ = repo.SetState(sc.ID, scenes.StatusDownloading, "", "")
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusDownloading, ""); err != nil {
		return fail(err)
	}
	dir, err := s.prepareDownload(ctx, queueID, downloadURL, protocol)
	if err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	video, err := organizer.FindLargestVideoFile(dir)
	if err != nil {
		return fail(badRelease(errors.New("the download has no video file")))
	}
	dest, err := s.sceneDest(sc, video)
	if err != nil {
		return fail(err)
	}
	quality := scenes.ParseRelease(releaseTitle).Quality()
	// Scenes never overwrite or delete a file already in the folder (it may
	// be one Whisparr put there): a name that is taken gets " (2)", " (3)"...
	dest = freeName(dest)
	res, err := organizer.Import(video, dest, organizer.ConflictSkip, nil)
	if err != nil {
		return fail(fmt.Errorf("couldn't move the scene into your library: %w", err))
	}
	if res.Skipped {
		return fail(fmt.Errorf("a file is already at %s", dest))
	}
	if err := repo.SetState(sc.ID, scenes.StatusDownloaded, res.DestPath, quality); err != nil {
		return fail(err)
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(dir, protocol)
	// Only the folder of the hidden library is scanned (the refresh goes by path).
	s.mediaServersImported(mediaservers.MediaMovie, res.DestPath)
	_ = s.QueueRepo.LogActivity(0, sceneEventImported, fmt.Sprintf("%s (%s) imported", label, quality))
	slog.Info("scenes: imported", "scene", sc.ID, "quality", quality)
	return nil
}

// freeName returns p, or p with " (2)", " (3)"... before the extension when
// a file is already there, so nothing is ever overwritten.
func freeName(p string) string {
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	stem := strings.TrimSuffix(p, ext)
	for i := 2; i < 1000; i++ {
		c := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := os.Lstat(c); err != nil {
			return c
		}
	}
	return p
}

// ---- images ----

// allowSceneImage records an image URL StashDB answered with, so the page
// may have it fetched through /api/scenes/image.
func (s *Server) allowSceneImage(u string) {
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		s.sceneMod().images.Store(u, struct{}{})
	}
}

// sceneImageKnown reports whether u is an image StashDB gave us: one seen in
// an answer since Mediarium started, or one stored with the library.
func (s *Server) sceneImageKnown(u string) bool {
	if _, ok := s.sceneMod().images.Load(u); ok {
		return true
	}
	var n int
	_ = s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM scenes WHERE image_url = ?) + (SELECT COUNT(*) FROM scene_performers WHERE image_url = ?) + (SELECT COUNT(*) FROM scene_studios WHERE image_url = ?)`, u, u, u).Scan(&n)
	return n > 0
}

var sceneImageClient = &http.Client{Timeout: 20 * time.Second}

// GET /api/scenes/image?u=<url>: fetches a StashDB image for the page, so
// the browser never contacts StashDB itself and the page's security policy
// stays as it is. Only URLs StashDB answered with are fetched.
func (s *Server) handleSceneImage(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	if u == "" || len(u) > 2048 || !s.sceneImageKnown(u) {
		writeError(w, http.StatusNotFound, "Unknown image.")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad image address.")
		return
	}
	req.Header.Set("User-Agent", "Mediarium-home/"+s.version)
	resp, err := sceneImageClient.Do(req)
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
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 15<<20))
}
