package api

// Home fork: the NSFW module. Adult videos from StashDB (or another
// stash-box server), followed by performer or studio, searched on the
// indexers' XXX categories and filed into their own folder.
//
// Privacy: every route is for administrators only, and video downloads and
// their activity are left out of what everyone else sees (queue, dashboard,
// activity). NSFW 18+ never sends notifications.
//
// Everything lives in this file and internal/nsfw; upstream files only get
// one-line hooks (see the "videos hook" comments), so merging upstream stays
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
	"github.com/rdborg/mediarium/internal/nsfw"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
)

// Settings keys of the module.
const (
	keyNsfwEnabled = "nsfw.enabled"      // "1" when the module is on
	keyNsfwPath    = "nsfw.path"         // library folder; NSFW_DIR when unset
	keyStashBoxURL = "nsfw.stashbox_url" // GraphQL address; StashDB when unset
	keyStashBoxKey = "nsfw.stashbox_key" // encrypted
)

// videoCategories are the Newznab/Torznab XXX categories.
var videoCategories = []int{6000, 6010, 6020, 6030, 6040, 6045, 6050, 6060, 6070, 6080, 6090}

// Activity event types of the module; hidden from everyone but administrators.
const (
	videoEventGrabbed  = "nsfw-grabbed"
	videoEventImported = "nsfw-imported"
	videoEventFailed   = "nsfw-failed"
	videoEventSearched = "nsfw-searched"
)

func isVideoEvent(eventType string) bool { return strings.HasPrefix(eventType, "nsfw-") }

// videoState is the module's part of the Server.
type videoState struct {
	once   sync.Once
	repo   *nsfw.Repo
	stash  *nsfw.StashBox
	check  sync.Mutex // one follow check at a time
	fill   sync.Mutex // one details fill at a time
	images sync.Map   // image URLs StashDB answered with, which /api/nsfw/image may fetch
}

var videoStates sync.Map // *Server -> *videoState

func (s *Server) videoMod() *videoState {
	v, _ := videoStates.LoadOrStore(s, &videoState{})
	st := v.(*videoState)
	st.once.Do(func() {
		st.repo = nsfw.NewRepo(s.db)
		st.stash = nsfw.NewStashBox("Mediarium-home/"+s.version, func() string {
			k, _ := s.Settings.Get(keyStashBoxKey)
			return k
		})
	})
	if u, _ := s.Settings.Get(keyStashBoxURL); u != "" {
		st.stash.Endpoint = u
	} else {
		st.stash.Endpoint = nsfw.DefaultStashBox
	}
	return st
}

func (s *Server) videoRepo() *nsfw.Repo    { return s.videoMod().repo }
func (s *Server) stashBox() *nsfw.StashBox { return s.videoMod().stash }
func (s *Server) nsfwEnabled() bool {
	v, _ := s.Settings.Get(keyNsfwEnabled)
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

func videoLink(id int64) string { return fmt.Sprintf("/nsfw/%d", id) }
func (s *Server) stashKeySet() bool {
	k, _ := s.Settings.Get(keyStashBoxKey)
	return strings.TrimSpace(k) != ""
}

// nsfwRoot: the saved folder, else NSFW_DIR, else /nsfw or /data/nsfw (Usherr, see home_extra_folders.go).
func (s *Server) nsfwRoot() string { return s.extraFolderRoot("nsfw") }

// ---- hooks called from upstream files ----

// registerVideoRoutes adds the module's routes, all for administrators.
func (s *Server) registerVideoRoutes(admin, member routeGroup) {
	// home fork: accounts given the NSFW 18+ switch browse, watch, find and add
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw/enabled", s.handleNsfwEnabled)
	on := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !s.nsfwEnabled() {
				writeError(w, http.StatusNotFound, "NSFW 18+ is switched off. Switch it on in Settings > NSFW 18+.")
				return
			}
			h(w, r)
		}
	}
	admin.HandleFunc("GET /api/nsfw/settings", s.handleGetVideoSettings)
	admin.HandleFunc("GET /api/home/season-folder", s.handleSeasonFolderStyle) // home fork: TV season folder style
	admin.HandleFunc("PUT /api/home/season-folder", s.handleSeasonFolderStyle)
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw/image", on(s.handleVideoImage))
	admin.HandleFunc("PUT /api/nsfw/settings", s.handlePutVideoSettings)
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw", revalidated(on(s.handleListNsfw)))
	member.Need(auth.PermNSFW).HandleFunc("POST /api/nsfw", on(s.handleAddVideo))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw/lookup", on(s.handleVideoLookup))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw/browse", on(s.handleVideoBrowse))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw/discover", on(s.handleVideoDiscover)) // home fork: Find with filters
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw/tags", on(s.handleVideoTags))
	admin.HandleFunc("POST /api/nsfw/check", on(s.handleVideoCheckNow))
	admin.HandleFunc("POST /api/nsfw/fill-details", on(s.handleFillVideoDetails))
	admin.HandleFunc("POST /api/nsfw/bulk", on(s.handleVideoBulk))
	admin.HandleFunc("GET /api/nsfw/search-status", on(s.handleVideoSearchStatus))
	admin.HandleFunc("POST /api/nsfw/search-now", on(s.handleVideoSearchNow))
	admin.HandleFunc("GET /api/nsfw/schedule", on(s.handleVideoSchedule))
	admin.HandleFunc("PUT /api/nsfw/schedule", on(s.handleVideoSchedule))
	admin.HandleFunc("GET /api/home/schedules", s.handleSchedules) // home fork: schedules
	admin.HandleFunc("PUT /api/home/schedules", s.handleSchedules)
	admin.HandleFunc("POST /api/home/quiet", s.handleQuiet)
	admin.HandleFunc("POST /api/nsfw/import/whisparr", on(s.handleImportWhisparr))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw/performers", on(s.handleListVideoPerformers))
	member.Need(auth.PermNSFW).HandleFunc("POST /api/nsfw/performers", on(s.handleFollowPerformer))
	member.Need(auth.PermNSFW).HandleFunc("DELETE /api/nsfw/performers/{id}", on(s.handleUnfollowPerformer))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw/studios", on(s.handleListVideoStudios))
	member.Need(auth.PermNSFW).HandleFunc("POST /api/nsfw/studios", on(s.handleFollowStudio))
	member.Need(auth.PermNSFW).HandleFunc("DELETE /api/nsfw/studios/{id}", on(s.handleUnfollowStudio))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw/{id}", on(s.handleGetVideo))
	member.Need(auth.PermNSFW).HandleFunc("GET /api/nsfw/{id}/stream", on(s.handleVideoStream))
	member.Need(auth.PermNSFW).HandleFunc("PUT /api/nsfw/{id}/wanted", on(s.handleSetVideoWanted))
	admin.HandleFunc("DELETE /api/nsfw/{id}", on(s.handleDeleteVideo))
	admin.HandleFunc("POST /api/nsfw/{id}/search", on(s.handleSearchVideoNow))
	admin.HandleFunc("GET /api/nsfw/{id}/releases", on(s.handleVideoReleases))
	admin.HandleFunc("POST /api/nsfw/{id}/grab", on(s.handleGrabVideo))
}

// Everything about videos stays behind the NSFW page, for every account,
// administrators included: shared views (queue, history, dashboard, activity,
// blocklist, logs, statistics, manual import) never show video downloads.
// The NSFW page asks for its own view with ?videos=only (administrators).

// nsfwOnly reports whether r asks for the NSFW page's own view.
func nsfwOnly(r *http.Request) bool {
	return r.URL.Query().Get("nsfw") == "only" && isAdminRequest(r)
}

// videoHider reports whether a download is a video download (to leave out).
func (s *Server) videoHider(r *http.Request) func(queueID int64) bool {
	ids, err := s.videoRepo().VideoDownloadIDs()
	if err != nil {
		return func(int64) bool { return false }
	}
	return func(id int64) bool { return ids[id] }
}

// hideVideoItems keeps only the downloads this view is for: everything but
// videos, or (NSFW page) only videos.
func (s *Server) hideVideoItems(r *http.Request, items []queue.Item) []queue.Item {
	isVideo := s.videoHider(r)
	only := nsfwOnly(r)
	out := items[:0]
	for _, it := range items {
		if isVideo(it.ID) == only {
			out = append(out, it)
		}
	}
	return out
}

// hideActivity reports whether an activity entry is left out of this view.
func hideActivity(r *http.Request, eventType string) bool {
	return isVideoEvent(eventType) != nsfwOnly(r)
}

// videoBlocklistTag marks blocklist entries that belong to videos.
const videoBlocklistTag = "[nsfw] "

// hideBlocklistEntry reports whether a blocklist entry is left out of this view.
func hideBlocklistEntry(r *http.Request, reason string) bool {
	return strings.HasPrefix(reason, videoBlocklistTag) != nsfwOnly(r)
}

// videoFailuresSince counts failed video downloads, so shared health
// warnings don't count them.
func (s *Server) videoFailuresSince(since time.Time) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM download_queue q JOIN nsfw_downloads d ON d.queue_id = q.id
		WHERE q.status = 'failed' AND q.completed_at >= ?`, since.UTC().Format(time.RFC3339Nano)).Scan(&n)
	return n
}

// pausedVideoDownloads counts video downloads that are paused right now.
func (s *Server) pausedVideoDownloads() int64 {
	var n int64
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM download_queue q JOIN nsfw_downloads d ON d.queue_id = q.id WHERE q.status = 'paused'`).Scan(&n)
	return n
}

// isVideoWorkDir reports whether path is the working folder of a video
// download ("<downloads>/incomplete/queue-12"), which manual import skips.
func (s *Server) isVideoWorkDir(path string) bool {
	name := filepath.Base(path)
	if !strings.HasPrefix(name, "queue-") {
		return false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(name, "queue-"), 10, 64)
	return err == nil && s.videoRepo().IsVideoDownload(id)
}

// describeVideoItem names a video download in the queue and dashboard.
func (s *Server) describeVideoItem(queueID int64) (title, subtitle, poster string, ok bool) {
	sc, err := s.videoRepo().VideoForDownload(queueID)
	if err != nil {
		return "", "", "", false
	}
	sub := "Video"
	if sc.StudioName != "" {
		sub += " · " + sc.StudioName
	}
	t := sc.Title
	if t == "" {
		t = sc.Name()
	}
	return t, sub, sc.ImageURL, true
}

// prepareVideoPipeline is preparePipeline's case for a video download.
// ok is false when the download isn't a video download.
func (s *Server) prepareVideoPipeline(item queue.Item, protocol indexers.Protocol) (run func(), ok bool, err error) {
	repo := s.videoRepo()
	// The link is written right after the download is queued; give it a
	// moment in case the line picked the item up in between.
	var sc nsfw.Video
	for i := 0; i < 20; i++ {
		sc, err = repo.VideoForDownload(item.ID)
		if !errors.Is(err, nsfw.ErrNotFound) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if errors.Is(err, nsfw.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, errors.New("That video isn't in your library any more.")
	}
	_ = repo.SetState(sc.ID, nsfw.StatusDownloading, "", "")
	return func() {
		if err := s.runVideoPipeline(item.ID, sc, item.ReleaseTitle, item.NZBURL, protocol); err != nil {
			slog.Info("queue: video download ended", "queueId", item.ID, "err", err)
		}
	}, true, nil
}

// ---- settings ----

type videoSettings struct {
	Enabled  bool   `json:"enabled"`
	Allowed  bool   `json:"allowed"` // Usherr: the server allows it (USHERR_ALLOW_ADULT)
	Path     string `json:"path"`
	Endpoint string `json:"endpoint"`
	KeySet   bool   `json:"keySet"`
}

func (s *Server) currentVideoSettings() videoSettings {
	ep, _ := s.Settings.Get(keyStashBoxURL)
	if ep == "" {
		ep = nsfw.DefaultStashBox
	}
	return videoSettings{Enabled: s.nsfwEnabled(), Allowed: adultAllowed(), Path: s.nsfwRoot(), Endpoint: ep, KeySet: s.stashKeySet()}
}

// GET /api/nsfw/settings
func (s *Server) handleGetVideoSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.currentVideoSettings())
}

// PUT /api/nsfw/settings {"enabled":true,"path":"/data/nsfw","endpoint":"...","key":"..."}.
// Only the fields sent change. A key is checked with the server before it is
// saved (encrypted); "removeKey":true removes it.
func (s *Server) handlePutVideoSettings(w http.ResponseWriter, r *http.Request) {
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
			writeError(w, http.StatusBadRequest, "The NSFW folder must be a full path inside the container, like /data/nsfw.")
			return
		}
		if err := s.Settings.Set(keyNsfwPath, p, false); err != nil {
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
		if ep == nsfw.DefaultStashBox {
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
			if errors.Is(err, nsfw.ErrKeyRefused) {
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
		if err := s.Settings.Set(keyNsfwEnabled, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The switch couldn't be saved.")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.currentVideoSettings())
}

// ---- library ----

func (s *Server) videoFromPath(w http.ResponseWriter, r *http.Request) (nsfw.Video, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a video id.")
		return nsfw.Video{}, false
	}
	sc, err := s.videoRepo().GetVideo(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That video isn't in your library.")
		return sc, false
	}
	return sc, true
}

// GET /api/nsfw?status=wanted|downloaded&performer=<stashId>&studio=<stashId>
func (s *Server) handleListNsfw(w http.ResponseWriter, r *http.Request) {
	repo := s.videoRepo()
	var list []nsfw.Video
	var err error
	q := r.URL.Query()
	switch {
	case q.Get("performer") != "":
		list, err = repo.NsfwWithPerformer(q.Get("performer"))
	case q.Get("studio") != "":
		list, err = repo.NsfwFromStudio(q.Get("studio"))
	default:
		list, err = repo.ListNsfw()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if st := q.Get("status"); st != "" {
		kept := list[:0]
		for _, sc := range list {
			if (st == "wanted" && sc.Wanted && sc.Status != nsfw.StatusDownloaded) ||
				(st == "notwanted" && !sc.Wanted && sc.Status != nsfw.StatusDownloaded) || sc.Status == st {
				kept = append(kept, sc)
			}
		}
		list = kept
	}
	counts, _ := repo.Count()
	writeJSON(w, http.StatusOK, map[string]any{"counts": counts, "nsfw": list})
}

// GET /api/nsfw/{id}
func (s *Server) handleGetVideo(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.videoFromPath(w, r)
	if !ok {
		return
	}
	busy, _ := s.videoRepo().HasActiveDownload(sc.ID)
	writeJSON(w, http.StatusOK, map[string]any{"video": sc, "downloading": busy})
}

// POST /api/nsfw {"stashId":"...","search":true}: adds a video from StashDB.
func (s *Server) handleAddVideo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StashID string `json:"stashId"`
		Search  bool   `json:"search"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.StashID) == "" {
		writeError(w, http.StatusBadRequest, "Say which video to add (its StashDB id).")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	found, err := s.stashBox().FindVideo(ctx, strings.TrimSpace(req.StashID))
	if err != nil {
		s.writeStashError(w, err)
		return
	}
	found.AddedBy = nsfw.AddedManual
	found.Wanted = true
	sc, _, err := s.videoRepo().AddVideo(found)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Search {
		id := sc.ID
		s.background(func() { s.searchVideo(id, false, queue.PriorityManual) })
	}
	writeJSON(w, http.StatusOK, sc)
}

// PUT /api/nsfw/{id}/wanted {"wanted":false}
func (s *Server) handleSetVideoWanted(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.videoFromPath(w, r)
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
	if err := s.videoRepo().SetWanted(sc.ID, req.Wanted); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sc.Wanted = req.Wanted
	writeJSON(w, http.StatusOK, sc)
}

// DELETE /api/nsfw/{id}: removes the video from the library. Its file
// stays on disk; Mediarium never deletes a video's file.
func (s *Server) handleDeleteVideo(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.videoFromPath(w, r)
	if !ok {
		return
	}
	if busy, _ := s.videoRepo().HasActiveDownload(sc.ID); busy {
		writeError(w, http.StatusConflict, "A download for this video is running. Stop it in Activity first.")
		return
	}
	if err := s.videoRepo().DeleteVideo(sc.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/nsfw/bulk {"ids":[1,2],"action":"want"|"unwant"|"removeEmpty"}.
// removeEmpty only removes videos without a file; files are never deleted.
func (s *Server) handleVideoBulk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []int64 `json:"ids"`
		Action string  `json:"action"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "Select some videos first.")
		return
	}
	if len(req.IDs) > 20000 {
		writeError(w, http.StatusBadRequest, "That is too many at once.")
		return
	}
	repo := s.videoRepo()
	var res nsfw.BulkResult
	var err error
	switch req.Action {
	case "want":
		res, err = repo.SetWantedMany(req.IDs, true)
	case "unwant":
		res, err = repo.SetWantedMany(req.IDs, false)
	case "removeEmpty":
		res, err = repo.RemoveEmpty(req.IDs)
	case "search":
		// Wanted videos without a file are searched in the background;
		// the others are skipped (downloaded, downloading, not wanted).
		ok := s.searchableNsfw(req.IDs)
		queued := s.queueVideoSearches(ok)
		res = nsfw.BulkResult{Changed: queued, Skipped: len(req.IDs) - queued}
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

// GET /api/nsfw/{id}/stream: plays the video's file in the browser, with
// seeking. Only the video's own file inside the NSFW folder is served.
func (s *Server) handleVideoStream(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.videoFromPath(w, r)
	if !ok {
		return
	}
	serveLibraryVideo(w, r, sc.FilePath, []string{s.nsfwRoot()})
}

// ---- StashDB lookups ----

func (s *Server) writeStashError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, nsfw.ErrNoKey), errors.Is(err, nsfw.ErrKeyRefused):
		writeError(w, http.StatusBadRequest, err.Error()+".")
	case errors.Is(err, nsfw.ErrNotFound):
		writeError(w, http.StatusNotFound, "StashDB doesn't know that one.")
	default:
		writeUpstreamError(w, "ask StashDB", err)
	}
}

type lookedUpVideo struct {
	nsfw.Video
	InLibrary bool  `json:"inLibrary"`
	LibraryID int64 `json:"libraryId,omitempty"`
}

func (s *Server) markInLibrary(list []nsfw.Video) []lookedUpVideo {
	out := make([]lookedUpVideo, 0, len(list))
	for _, sc := range list {
		s.allowVideoImage(sc.ImageURL)
		l := lookedUpVideo{Video: sc}
		if have, err := s.videoRepo().GetVideoByStashID(sc.StashID); err == nil {
			l.InLibrary, l.LibraryID, l.Video.Status, l.Video.Wanted = true, have.ID, have.Status, have.Wanted
		}
		out = append(out, l)
	}
	return out
}

// GET /api/nsfw/lookup?kind=video|performer|studio&q=...
func (s *Server) handleVideoLookup(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 200 {
		writeError(w, http.StatusBadRequest, "Type something to look for.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	sb := s.stashBox()
	switch r.URL.Query().Get("kind") {
	case nsfw.KindPerformer:
		found, err := sb.SearchPerformers(ctx, q)
		if err != nil {
			s.writeStashError(w, err)
			return
		}
		followed := map[string]bool{}
		if list, err := s.videoRepo().Performers(true); err == nil {
			for _, p := range list {
				followed[p.StashID] = true
			}
		}
		for i := range found {
			found[i].Followed = followed[found[i].StashID]
			s.allowVideoImage(found[i].ImageURL)
		}
		writeJSON(w, http.StatusOK, map[string]any{"performers": found})
	case nsfw.KindStudio:
		found, err := sb.SearchStudios(ctx, q)
		if err != nil {
			s.writeStashError(w, err)
			return
		}
		followed := map[string]bool{}
		if list, err := s.videoRepo().Studios(true); err == nil {
			for _, st := range list {
				followed[st.StashID] = true
			}
		}
		for i := range found {
			found[i].Followed = followed[found[i].StashID]
			s.allowVideoImage(found[i].ImageURL)
		}
		writeJSON(w, http.StatusOK, map[string]any{"studios": found})
	default:
		found, err := sb.SearchNsfw(ctx, q)
		if err != nil {
			s.writeStashError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"nsfw": s.markInLibrary(found)})
	}
}

// GET /api/nsfw/browse?kind=performer|studio&stashId=...&page=1: a
// performer's or studio's videos on StashDB, marked when in the library.
func (s *Server) handleVideoBrowse(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	id := strings.TrimSpace(r.URL.Query().Get("stashId"))
	if (kind != nsfw.KindPerformer && kind != nsfw.KindStudio) || id == "" {
		writeError(w, http.StatusBadRequest, "Say whose videos to show: kind=performer or studio, and a stashId.")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	res, err := s.stashBox().NsfwOf(ctx, kind, id, page, 40, "")
	if err != nil {
		s.writeStashError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": res.Count, "nsfw": s.markInLibrary(res.Nsfw)})
}

// ---- following ----

// POST /api/nsfw/performers {"stashId":"...","backfill":false}: follows a
// performer. New videos from now on are added as wanted; with backfill, the
// videos already out are added too.
func (s *Server) handleFollowPerformer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StashID  string `json:"stashId"`
		Backfill bool   `json:"backfill"`
		Search   bool   `json:"search"` // search the wanted, missing videos now
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
	saved, err := s.videoRepo().SavePerformer(p, true)
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
		n, err := s.backfillSource(bctx, nsfw.KindPerformer, saved.StashID, nsfw.AddedPerformer)
		if err != nil && n == 0 {
			s.writeStashError(w, err)
			return
		}
		added = n
	}
	_ = s.videoRepo().PerformerChecked(saved.ID)
	saved.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	searching := 0
	if req.Search {
		searching = s.searchSourceNsfw(nsfw.KindPerformer, saved.StashID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"performer": saved, "added": added, "searching": searching})
}

// DELETE /api/nsfw/performers/{id}: stops following. NSFW already added stay.
func (s *Server) handleUnfollowPerformer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a performer id.")
		return
	}
	if err := s.videoRepo().SetPerformerFollowed(id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/nsfw/performers?all=1
func (s *Server) handleListVideoPerformers(w http.ResponseWriter, r *http.Request) {
	list, err := s.videoRepo().Performers(r.URL.Query().Get("all") == "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/nsfw/studios {"stashId":"...","backfill":false}
func (s *Server) handleFollowStudio(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StashID  string `json:"stashId"`
		Backfill bool   `json:"backfill"`
		Search   bool   `json:"search"` // search the wanted, missing videos now
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
	saved, err := s.videoRepo().SaveStudio(st, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	added := 0
	if req.Backfill {
		bctx, bcancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer bcancel()
		n, err := s.backfillSource(bctx, nsfw.KindStudio, saved.StashID, nsfw.AddedStudio)
		if err != nil && n == 0 {
			s.writeStashError(w, err)
			return
		}
		added = n
	}
	_ = s.videoRepo().StudioChecked(saved.ID)
	saved.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	searching := 0
	if req.Search {
		searching = s.searchSourceNsfw(nsfw.KindStudio, saved.StashID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"studio": saved, "added": added, "searching": searching})
}

// backfillSource adds every video of a performer or studio as wanted (up to
// 3,000, newest first) and reports how many were new. NSFW already in the
// library keep their wanted flag and status.
func (s *Server) backfillSource(ctx context.Context, kind, stashID, addedBy string) (int, error) {
	repo, sb := s.videoRepo(), s.stashBox()
	added := 0
	for page := 1; page <= 30; page++ {
		res, err := sb.NsfwOf(ctx, kind, stashID, page, 100, "")
		if err != nil {
			return added, err
		}
		for _, sc := range res.Nsfw {
			sc.AddedBy, sc.Wanted = addedBy, true
			if _, isNew, err := repo.AddVideo(sc); err == nil && isNew {
				added++
			}
		}
		if len(res.Nsfw) == 0 || page*100 >= res.Count {
			break
		}
	}
	if added > 0 {
		slog.Info("videos: added a catalogue", "kind", kind, "added", added)
	}
	return added, nil
}

// DELETE /api/nsfw/studios/{id}
func (s *Server) handleUnfollowStudio(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a studio id.")
		return
	}
	if err := s.videoRepo().SetStudioFollowed(id, false); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/nsfw/studios?all=1
func (s *Server) handleListVideoStudios(w http.ResponseWriter, r *http.Request) {
	list, err := s.videoRepo().Studios(r.URL.Query().Get("all") == "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/nsfw/check: looks for new videos of followed performers and
// studios now, instead of waiting for the next automatic run.
func (s *Server) handleVideoCheckNow(w http.ResponseWriter, r *http.Request) {
	s.background(func() { s.checkFollowed(context.Background()) })
	writeJSON(w, http.StatusAccepted, map[string]any{"message": "Looking for new videos now."})
}

// sinceDate is the date a follow check starts from: the day before the last
// check (so a video dated late on that day isn't missed), or "" for all.
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

// checkFollowed adds the new videos of followed performers and studios as
// wanted. A first check of a follow made with backfill adds everything; at
// most a few pages per source per run, so a big back catalogue fills in over
// a few runs instead of all at once.
func (s *Server) checkFollowed(ctx context.Context) {
	if !s.nsfwEnabled() || !s.stashKeySet() {
		return
	}
	st := s.videoMod()
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
			res, err := sb.NsfwOf(ctx, kind, stashID, page, 50, since)
			if err != nil {
				return err
			}
			for _, sc := range res.Nsfw {
				sc.AddedBy, sc.Wanted = addedBy, true
				if _, isNew, err := repo.AddVideo(sc); err == nil && isNew {
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
			if err := addAll(nsfw.KindPerformer, p.StashID, p.CheckedAt, nsfw.AddedPerformer); err != nil {
				slog.Info("videos: follow check", "performer", p.Name, "err", err)
				continue
			}
			_ = repo.PerformerChecked(p.ID)
		}
	}
	if studios, err := repo.Studios(true); err == nil {
		for _, stu := range studios {
			if err := addAll(nsfw.KindStudio, stu.StashID, stu.CheckedAt, nsfw.AddedStudio); err != nil {
				slog.Info("videos: follow check", "studio", stu.Name, "err", err)
				continue
			}
			_ = repo.StudioChecked(stu.ID)
		}
	}
	if added > 0 {
		slog.Info("videos: new videos from follows", "added", added)
	}
}

// huntNsfw is the module's part of the automatic search: new videos from
// follows first, then a few wanted videos per run (the same limit of ten
// grabs per hour as everything else applies through the download line).
func (s *Server) huntNsfw(ctx context.Context) {
	if !s.nsfwEnabled() || !s.stashKeySet() {
		return
	}
	if s.taskAllowed(taskVideoFollow) {
		s.checkFollowed(ctx)
		s.fillVideoDetails(ctx, 60)
	}
	if !s.taskAllowed(taskVideoSearch) {
		return // the schedule holds automatic video searches
	}
	wanted, err := s.videoRepo().Wanted()
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
		s.searchVideo(sc.ID, false, queue.PriorityAutomatic)
	}
}

// fillVideoDetails reads tags and performer genders from StashDB for up to n
// videos that don't have them yet (Whisparr imports), one at a time with a
// short pause, so StashDB isn't hammered. It reports how many it filled.
func (s *Server) fillVideoDetails(ctx context.Context, n int) int {
	if !s.nsfwEnabled() || !s.stashKeySet() {
		return 0
	}
	st := s.videoMod()
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
		fresh, err := st.stash.FindVideo(ctx, sc.StashID)
		if errors.Is(err, nsfw.ErrNotFound) {
			// Gone from StashDB: mark it read so it isn't asked for again.
			fresh = sc
			fresh.DetailsAt = time.Now().UTC().Format(time.RFC3339)
		} else if err != nil {
			slog.Info("videos: fill details", "video", sc.ID, "err", err)
			break
		}
		fresh.AddedBy = sc.AddedBy
		if _, _, err := st.repo.AddVideo(fresh); err == nil {
			done++
		}
		select {
		case <-ctx.Done():
		case <-time.After(300 * time.Millisecond):
		}
	}
	if done > 0 {
		slog.Info("videos: filled in details", "nsfw", done)
	}
	return done
}

// POST /api/nsfw/fill-details: fills in tags and genders for every video
// that lacks them, in the background.
func (s *Server) handleFillVideoDetails(w http.ResponseWriter, r *http.Request) {
	s.background(func() {
		for s.fillVideoDetails(context.Background(), 100) > 0 {
		}
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"message": "Getting tags and performers for your videos now."})
}

// ---- searching and grabbing ----

// videoRelease is a release found for a video. It holds the real download
// URL, so it is never sent to the page as it is (see handleVideoReleases).
type videoRelease struct {
	indexers.Result
	Quality string
	Score   int
	Matches bool
	Blocked bool
}

// videoReleases searches the indexers for a video and returns the releases
// that are for it, best first; with all, the others are included too,
// marked as not matching (for choosing by hand).
func (s *Server) videoReleases(ctx context.Context, sc nsfw.Video, all bool) ([]videoRelease, error) {
	list, err := s.searchableIndexers()
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("there are no indexers to search. Add one in Settings > Indexers & Search")
	}
	// Other videos of the same studio around the same day tell apart
	// releases that only name the studio and the date.
	siblings, _ := s.videoRepo().NsfwFromStudio(sc.StudioStashID)
	blocked := s.blockedKeys()
	seen := map[string]bool{}
	var out []videoRelease
	for _, q := range nsfw.SearchQueries(sc) {
		if ctx.Err() != nil {
			break
		}
		for _, res := range indexers.MergeResults(indexers.SearchAll(ctx, list, q, videoCategories)) {
			if seen[res.DownloadURL] {
				continue
			}
			seen[res.DownloadURL] = true
			rel := nsfw.ParseRelease(res.Title)
			sr := videoRelease{Result: res, Quality: rel.Quality(), Score: nsfw.MatchScore(rel, sc)}
			sr.Matches = nsfw.Matches(rel, sc, siblings)
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
		qi, qj := nsfw.QualityRank(nsfw.ParseRelease(out[i].Title)), nsfw.QualityRank(nsfw.ParseRelease(out[j].Title))
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

// searchVideo looks for a video and grabs the best release that isn't
// blocklisted. It reports whether something was grabbed.
func (s *Server) searchVideo(videoID int64, retry bool, prio queue.Priority) bool {
	sc, err := s.videoRepo().GetVideo(videoID)
	if err != nil || !sc.Wanted || (sc.Status == nsfw.StatusDownloaded && !retry) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results, err := s.videoReleases(ctx, sc, false)
	if err != nil {
		slog.Info("videos: search", "video", videoID, "err", err)
		return false
	}
	sources := s.sourcesFor("")
	for _, res := range results {
		if res.Blocked || len(filterSources([]indexers.Result{res.Result}, sources)) == 0 {
			continue
		}
		_, err := s.grabVideo(sc, res.Title, res.DownloadURL, res.SizeBytes, res.Protocol, prio)
		return err == nil
	}
	if retry || prio == queue.PriorityManual {
		_ = s.QueueRepo.LogActivity(0, videoEventSearched, fmt.Sprintf("No release found for %s", sc.Name()))
	}
	return false
}

// grabVideo puts a release of a video in the download line.
func (s *Server) grabVideo(sc nsfw.Video, title, url string, size int64, protocol indexers.Protocol, prio queue.Priority) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentsEnabled() {
		return 0, errTorrentsDisabled
	}
	if s.nsfwRoot() == "" {
		return 0, errors.New("Choose the NSFW folder in Settings > NSFW 18+ first.")
	}
	repo := s.videoRepo()
	s.grabMu.Lock()
	busy, err := repo.HasActiveDownload(sc.ID)
	if err == nil && busy {
		err = errors.New("A download for this video is already running or waiting.")
	}
	var id int64
	if err == nil {
		id, err = s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: title, NZBURL: url, SizeBytes: size, Protocol: queue.Protocol(protocol), Priority: prio})
	}
	if err == nil {
		if err = repo.LinkDownload(id, sc.ID); err != nil {
			_ = s.QueueRepo.SetStatus(id, queue.StatusFailed, "couldn't link the download to its video")
		}
	}
	if err == nil {
		_ = repo.SetState(sc.ID, nsfw.StatusDownloading, "", "")
	}
	s.grabMu.Unlock()
	if err != nil {
		return 0, err
	}
	_ = s.QueueRepo.LogActivity(0, videoEventGrabbed, fmt.Sprintf("Downloading %s: %s", sc.Name(), title))
	s.dispatch.Kick()
	return id, nil
}

// POST /api/nsfw/{id}/search
func (s *Server) handleSearchVideoNow(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.videoFromPath(w, r)
	if !ok {
		return
	}
	if s.searchVideo(sc.ID, sc.Status == nsfw.StatusDownloaded, queue.PriorityManual) {
		writeJSON(w, http.StatusOK, map[string]any{"grabbed": 1, "message": "Found a release. It is downloading now."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grabbed": 0, "message": "Nothing suitable was found right now."})
}

// GET /api/nsfw/{id}/releases?all=1
func (s *Server) handleVideoReleases(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.videoFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	list, err := s.videoReleases(ctx, sc, r.URL.Query().Get("all") != "")
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

// POST /api/nsfw/{id}/grab {"title","downloadUrl","sizeBytes","protocol"}
func (s *Server) handleGrabVideo(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.videoFromPath(w, r)
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
	id, err := s.grabVideo(sc, req.Title, req.DownloadURL, req.SizeBytes, protocol, queue.PriorityManual)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queueId": id})
}

// retryVideoDownload retries a failed or stopped video download: the same
// release is queued again for the same video, and the old entry goes. It
// reports false when item isn't a video download.
func (s *Server) retryVideoDownload(w http.ResponseWriter, item queue.Item) bool {
	sc, err := s.videoRepo().VideoForDownload(item.ID)
	if errors.Is(err, nsfw.ErrNotFound) {
		return false
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "That video isn't in your library any more.")
		return true
	}
	// Like a movie retry: reuse what was already downloaded when it's still there.
	keptDir, reuse := s.keptDownload(item)
	// The new item must not start before the earlier files are in its folder.
	release := s.dispatch.Hold()
	newID, err := s.grabVideo(sc, item.ReleaseTitle, item.NZBURL, item.SizeBytes, indexers.Protocol(item.Protocol), queue.PriorityManual)
	if err != nil {
		release()
		writeError(w, http.StatusConflict, err.Error())
		return true
	}
	if reuse {
		s.moveKeptDownload(keptDir, newID)
	}
	release()
	_ = s.QueueRepo.Delete(item.ID)
	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": newID})
	return true
}

// ---- the download pipeline ----

// videoDest is where a video's video goes: "<root>/<Studio>/<file name>.<ext>".
func (s *Server) videoDest(sc nsfw.Video, videoFile string) (string, error) {
	root := s.nsfwRoot()
	if root == "" {
		return "", errors.New("the NSFW folder isn't set. Choose one in Settings > NSFW")
	}
	mode, repl := s.illegalCharSettings()
	ext := strings.ToLower(filepath.Ext(videoFile))
	return filepath.Join(root, organizer.Sanitize(nsfw.Folder(sc), mode, repl), organizer.Sanitize(nsfw.FileName(sc), mode, repl)+ext), nil
}

func (s *Server) runVideoPipeline(queueID int64, sc nsfw.Video, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}
	repo := s.videoRepo()
	restore := nsfw.StatusMissing
	if sc.FilePath != "" && fileExists(sc.FilePath) {
		restore = nsfw.StatusDownloaded
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
		// Not in the shared "Logs and errors": video failures stay on the NSFW page.
		_ = repo.SetState(sc.ID, restore, "", "")
		_ = s.QueueRepo.LogActivity(0, videoEventFailed, fmt.Sprintf("%s: %s", label, plain))
		if isBadRelease(stepErr) {
			if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: videoBlocklistTag + stepErr.Error()}); err == nil {
				s.background(func() { s.searchVideo(sc.ID, true, queue.PriorityAutomatic) })
			}
		}
		return stepErr
	}

	_ = repo.SetState(sc.ID, nsfw.StatusDownloading, "", "")
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
	dest, err := s.videoDest(sc, video)
	if err != nil {
		return fail(err)
	}
	quality := nsfw.ParseRelease(releaseTitle).Quality()
	// NSFW never overwrite or delete a file already in the folder (it may
	// be one Whisparr put there): a name that is taken gets " (2)", " (3)"...
	dest = freeName(dest)
	res, err := organizer.Import(video, dest, organizer.ConflictSkip, nil)
	if err != nil {
		return fail(fmt.Errorf("couldn't move the video into your library: %w", err))
	}
	if res.Skipped {
		return fail(fmt.Errorf("a file is already at %s", dest))
	}
	if err := repo.SetState(sc.ID, nsfw.StatusDownloaded, res.DestPath, quality); err != nil {
		return fail(err)
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(dir, protocol)
	// Only the folder of the hidden library is scanned (the refresh goes by path).
	s.mediaServersImported(mediaservers.MediaMovie, res.DestPath)
	_ = s.QueueRepo.LogActivity(0, videoEventImported, fmt.Sprintf("%s (%s) imported", label, quality))
	slog.Info("videos: imported", "video", sc.ID, "quality", quality)
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

// allowVideoImage records an image URL StashDB answered with, so the page
// may have it fetched through /api/nsfw/image.
func (s *Server) allowVideoImage(u string) {
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		s.videoMod().images.Store(u, struct{}{})
	}
}

// videoImageKnown reports whether u is an image StashDB gave us: one seen in
// an answer since Mediarium started, or one stored with the library.
func (s *Server) videoImageKnown(u string) bool {
	if _, ok := s.videoMod().images.Load(u); ok {
		return true
	}
	var n int
	_ = s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM nsfw_videos WHERE image_url = ?) + (SELECT COUNT(*) FROM nsfw_performers WHERE image_url = ?) + (SELECT COUNT(*) FROM nsfw_studios WHERE image_url = ?)`, u, u, u).Scan(&n)
	return n > 0
}

var videoImageClient = &http.Client{Timeout: 20 * time.Second}

// GET /api/nsfw/image?u=<url>: fetches a StashDB image for the page, so
// the browser never contacts StashDB itself and the page's security policy
// stays as it is. Only URLs StashDB answered with are fetched.
func (s *Server) handleVideoImage(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	if u == "" || len(u) > 2048 || !s.videoImageKnown(u) {
		writeError(w, http.StatusNotFound, "Unknown image.")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad image address.")
		return
	}
	req.Header.Set("User-Agent", "Mediarium-home/"+s.version)
	resp, err := videoImageClient.Do(req)
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
