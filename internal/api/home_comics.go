package api

// Home fork: the Comics module. Series and issues come from ComicVine (a free
// API key the person pastes in Comics settings); issues are searched on the
// indexers' comics category, downloaded through the usual pipeline and filed
// as <root>/<Series>/Volume 01 (<Year>)/<Series> (<Year>) Volume 01 Issue 001.cbz,
// the layout Kapowarr used. Upstream code is touched only through one-line
// hooks (see the "comics hook" comments).

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/plainerror"
	"github.com/rdborg/mediarium/internal/queue"
)

const (
	keyComicsEnabled = "comics.enabled" // "1" when the module is on
	keyComicsPath    = "comics.path"    // library folder; COMICS_DIR when unset
	keyComicVineKey  = "comics.cv_key"  // encrypted
)

// Newznab: 7030 is Books > Comics.
var comicCategories = []int{7030}

type comicState struct {
	once   sync.Once
	repo   *comics.Repo
	cv     *comics.ComicVine
	images sync.Map
	hunt   sync.Mutex
}

var comicStates sync.Map

func (s *Server) comicMod() *comicState {
	v, _ := comicStates.LoadOrStore(s, &comicState{})
	st := v.(*comicState)
	st.once.Do(func() {
		st.repo = comics.NewRepo(s.db)
		st.cv = comics.NewComicVine("Mediarium-home/"+s.version, func() string {
			k, _ := s.Settings.Get(keyComicVineKey)
			return k
		})
	})
	return st
}

func (s *Server) comicRepo() *comics.Repo      { return s.comicMod().repo }
func (s *Server) comicVine() *comics.ComicVine { return s.comicMod().cv }
func (s *Server) comicsEnabled() bool          { v, _ := s.Settings.Get(keyComicsEnabled); return v == "1" }
func (s *Server) comicKeySet() bool {
	k, _ := s.Settings.Get(keyComicVineKey)
	return strings.TrimSpace(k) != ""
}
func (s *Server) comicsRoot() string {
	if v, _ := s.Settings.Get(keyComicsPath); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(os.Getenv("COMICS_DIR"))
}

func (s *Server) registerComicRoutes(admin, member routeGroup) {
	on := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !s.comicsEnabled() {
				writeError(w, http.StatusNotFound, "Comics are switched off. Switch them on in Comics settings.")
				return
			}
			h(w, r)
		}
	}
	admin.HandleFunc("GET /api/comics/settings", s.handleGetComicSettings)
	admin.HandleFunc("GET /api/home/book-sources", s.handleBookSources) // home fork: book sources
	admin.HandleFunc("GET /api/home/hardcover-match", s.handleHardcoverMatch)
	admin.HandleFunc("GET /api/home/digest-preview", s.handleDigestPreview)
	s.loadHardcoverCovers()
	admin.HandleFunc("GET /api/home/torrent-debug", s.handleTorrentDebug)
	admin.HandleFunc("GET /api/home/vpn-udp-test", s.handleVPNUDPTest)
	admin.HandleFunc("PUT /api/home/book-sources", s.handleBookSources)
	admin.HandleFunc("PUT /api/comics/settings", s.handlePutComicSettings)
	member.HandleFunc("GET /api/comics/enabled", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": s.comicsEnabled()})
	})
	member.HandleFunc("GET /api/comics", revalidated(on(s.handleListComics)))
	member.HandleFunc("GET /api/comics/image", on(s.handleComicImage))
	member.HandleFunc("GET /api/comics/{id}", on(s.handleGetComicSeries))
	member.HandleFunc("GET /api/comics/issues/{id}/file", on(s.handleComicIssueFile))
	member.HandleFunc("GET /api/comics/issues/{id}/pages", on(s.handleComicPages))
	member.HandleFunc("GET /api/comics/issues/{id}/pages/{n}", on(s.handleComicIssuePage))
	member.HandleFunc("GET /api/comics/issues/{id}/pages/{n}/panels", on(s.handleComicPagePanels)) // guided view (home fork)
	member.Need(auth.PermComics).HandleFunc("GET /api/comics/lookup", on(s.handleComicLookup))     // home fork: accounts allowed comics add them
	member.Need(auth.PermComics).HandleFunc("POST /api/comics", on(s.handleAddComicSeries))
	admin.HandleFunc("PUT /api/comics/{id}", on(s.handleUpdateComicSeries))
	admin.HandleFunc("DELETE /api/comics/{id}", on(s.handleDeleteComicSeries))
	admin.HandleFunc("POST /api/comics/{id}/refresh", on(s.handleRefreshComicSeries))
	admin.HandleFunc("POST /api/comics/{id}/search", on(s.handleSearchComicSeries))
	admin.HandleFunc("PUT /api/comics/issues/wanted", on(s.handleSetComicIssuesWanted))
	admin.HandleFunc("POST /api/comics/issues/{id}/search", on(s.handleSearchComicIssue))
	admin.HandleFunc("GET /api/comics/issues/{id}/releases", on(s.handleComicIssueReleases))
	s.registerComicReviewRoutes(admin, on)       // review list and search history (home fork)
	s.registerComicWeekRoutes(member, admin, on) // the week's new comics (home fork)
	s.registerComicArcRoutes(member, admin, on)  // story arcs (home fork)
	s.registerComicToolRoutes(admin, on)         // library tools (home fork)
	admin.HandleFunc("POST /api/comics/issues/{id}/grab", on(s.handleGrabComicIssue))
	admin.HandleFunc("POST /api/comics/import", on(s.handleImportComics))
	admin.HandleFunc("POST /api/comics/import/folder", on(s.handleImportComicFolder))
}

// ---- settings ----

type comicSettings struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
	KeySet  bool   `json:"keySet"`
}

func (s *Server) currentComicSettings() comicSettings {
	return comicSettings{Enabled: s.comicsEnabled(), Path: s.comicsRoot(), KeySet: s.comicKeySet()}
}

func (s *Server) handleGetComicSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.currentComicSettings())
}

// PUT /api/comics/settings {"enabled","path","key","removeKey"}; only what
// is sent changes. A key is checked with ComicVine before it is saved.
func (s *Server) handlePutComicSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled   *bool   `json:"enabled"`
		Path      *string `json:"path"`
		Key       *string `json:"key"`
		RemoveKey bool    `json:"removeKey"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.Path != nil {
		p := strings.TrimSpace(*req.Path)
		if p != "" && !filepath.IsAbs(p) {
			writeError(w, http.StatusBadRequest, "The comics folder must be a full path inside the container, like /data/comics.")
			return
		}
		if err := s.Settings.Set(keyComicsPath, p, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The folder couldn't be saved.")
			return
		}
	}
	if req.RemoveKey {
		if err := s.Settings.Set(keyComicVineKey, "", true); err != nil {
			writeError(w, http.StatusInternalServerError, "The key couldn't be removed.")
			return
		}
	} else if req.Key != nil {
		key := strings.TrimSpace(*req.Key)
		if key == "" || len(key) > 200 || strings.ContainsAny(key, " \r\n\t") {
			writeError(w, http.StatusBadRequest, "That doesn't look like a ComicVine API key. Copy it again from comicvine.gamespot.com/api.")
			return
		}
		test := comics.NewComicVine("Mediarium-home/"+s.version, func() string { return key })
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := test.Test(ctx); err != nil {
			if errors.Is(err, comics.ErrKeyRefused) {
				writeError(w, http.StatusBadRequest, "ComicVine refused that key. Copy it again from comicvine.gamespot.com/api.")
				return
			}
			writeUpstreamError(w, "check the key with ComicVine", err)
			return
		}
		if err := s.Settings.Set(keyComicVineKey, key, true); err != nil {
			writeError(w, http.StatusInternalServerError, "The key couldn't be saved.")
			return
		}
	}
	if req.Enabled != nil {
		v := ""
		if *req.Enabled {
			v = "1"
		}
		if err := s.Settings.Set(keyComicsEnabled, v, false); err != nil {
			writeError(w, http.StatusInternalServerError, "The switch couldn't be saved.")
			return
		}
	}
	writeJSON(w, http.StatusOK, s.currentComicSettings())
}

// ---- library ----

func (s *Server) handleListComics(w http.ResponseWriter, r *http.Request) {
	list, err := s.comicRepo().ListSeries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The comics couldn't be read.")
		return
	}
	for i := range list {
		list[i].ImageURL = s.comicImage(list[i].ImageURL)
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) comicSeriesFromPath(w http.ResponseWriter, r *http.Request) (comics.Series, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a series id.")
		return comics.Series{}, false
	}
	sr, err := s.comicRepo().GetSeries(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That series isn't in your library.")
		return comics.Series{}, false
	}
	return sr, true
}

func (s *Server) comicIssueFromPath(w http.ResponseWriter, r *http.Request) (comics.Issue, comics.Series, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't an issue id.")
		return comics.Issue{}, comics.Series{}, false
	}
	is, err := s.comicRepo().GetIssue(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That issue isn't in your library.")
		return comics.Issue{}, comics.Series{}, false
	}
	sr, err := s.comicRepo().GetSeries(is.SeriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That issue's series isn't in your library.")
		return comics.Issue{}, comics.Series{}, false
	}
	return is, sr, true
}

// GET /api/comics/{id}: a series and its issues.
func (s *Server) handleGetComicSeries(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.comicSeriesFromPath(w, r)
	if !ok {
		return
	}
	issues, err := s.comicRepo().Issues(sr.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The issues couldn't be read.")
		return
	}
	sr.ImageURL = s.comicImage(sr.ImageURL)
	admin := isAdminRequest(r)
	for i := range issues {
		issues[i].ImageURL = s.comicImage(issues[i].ImageURL)
		if !admin {
			issues[i].FilePath = ""
		}
		if issues[i].Status == comics.StatusDownloaded {
			sr.Have++
		} else if issues[i].Wanted {
			sr.Wanted++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": sr, "issues": issues})
}

// GET /api/comics/lookup?q=: series on ComicVine, with those already here marked.
func (s *Server) handleComicLookup(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	found, err := s.comicVine().SearchSeries(ctx, q, 20)
	if err != nil {
		writeUpstreamError(w, "search ComicVine", err)
		return
	}
	type item struct {
		comics.Series
		InLibrary int64 `json:"inLibrary,omitempty"`
	}
	out := make([]item, 0, len(found))
	for _, f := range found {
		it := item{Series: f}
		if have, err := s.comicRepo().SeriesByCV(f.CVID); err == nil {
			it.InLibrary = have.ID
		}
		it.ImageURL = s.comicImage(f.ImageURL)
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, out)
}

// addComicSeries adds a series and its issues. want is "all" (every issue
// out so far and to come), "future" (only new ones) or "none".
func (s *Server) addComicSeries(ctx context.Context, cvID int64, want, folder string) (comics.Series, int, error) {
	cv := s.comicVine()
	info, err := cv.GetSeries(ctx, cvID)
	if err != nil {
		return comics.Series{}, 0, err
	}
	info.Folder = folder
	info.MonitorNew = want != "none"
	sr, err := s.comicRepo().UpsertSeries(info)
	if err != nil {
		return comics.Series{}, 0, err
	}
	n, err := s.refreshComicIssues(ctx, sr, want == "all")
	return sr, n, err
}

// refreshComicIssues fetches a series' issues; new ones are wanted when
// wantAll, or (for issues not out yet) when the series monitors new issues.
func (s *Server) refreshComicIssues(ctx context.Context, sr comics.Series, wantAll bool) (int, error) {
	list, err := s.comicVine().SeriesIssues(ctx, sr.CVID)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	added := 0
	first := sr.CheckedAt == ""
	for _, is := range list {
		is.SeriesID = sr.ID
		is.Wanted = wantAll || (sr.MonitorNew && (!first || !is.Released(now)))
		isNew, err := s.comicRepo().UpsertIssue(is)
		if err != nil {
			return added, err
		}
		if isNew {
			added++
		}
	}
	_ = s.comicRepo().MarkChecked(sr.ID)
	return added, nil
}

// POST /api/comics {"cvId", "want": "all"|"future"|"none"}
func (s *Server) handleAddComicSeries(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CVID int64  `json:"cvId"`
		Want string `json:"want"`
	}
	if err := decodeJSON(r, &req); err != nil || req.CVID <= 0 {
		writeError(w, http.StatusBadRequest, "Choose a series to add.")
		return
	}
	if req.Want != "all" && req.Want != "none" {
		req.Want = "future"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	sr, n, err := s.addComicSeries(ctx, req.CVID, req.Want, "")
	if err != nil {
		writeUpstreamError(w, "add the series from ComicVine", err)
		return
	}
	if req.Want == "all" {
		s.background(func() { s.searchComicSeries(sr.ID, queue.PriorityManual) })
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": sr, "issues": n})
}

// PUT /api/comics/{id} {"monitorNew"}
func (s *Server) handleUpdateComicSeries(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.comicSeriesFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		MonitorNew *bool `json:"monitorNew"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.MonitorNew != nil {
		_ = s.comicRepo().SetMonitorNew(sr.ID, *req.MonitorNew)
	}
	sr, _ = s.comicRepo().GetSeries(sr.ID)
	writeJSON(w, http.StatusOK, sr)
}

// DELETE /api/comics/{id}: removes the series from the library; files stay.
func (s *Server) handleDeleteComicSeries(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.comicSeriesFromPath(w, r)
	if !ok {
		return
	}
	if err := s.comicRepo().DeleteSeries(sr.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "The series couldn't be removed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": sr.Name + " was removed from the library. Its files are untouched."})
}

// POST /api/comics/{id}/refresh: fetch the issue list again.
func (s *Server) handleRefreshComicSeries(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.comicSeriesFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	n, err := s.refreshComicIssues(ctx, sr, false)
	if err != nil {
		writeUpstreamError(w, "refresh the series from ComicVine", err)
		return
	}
	// home fork: and files already in its folder for issues marked missing
	found := s.claimComicFiles(sr)
	writeJSON(w, http.StatusOK, map[string]any{"added": n, "found": found})
}

// PUT /api/comics/issues/wanted {"ids":[...], "wanted":true}
func (s *Server) handleSetComicIssuesWanted(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []int64 `json:"ids"`
		Wanted bool    `json:"wanted"`
		Search bool    `json:"search"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 || len(req.IDs) > 5000 {
		writeError(w, http.StatusBadRequest, "Choose some issues.")
		return
	}
	n, err := s.comicRepo().SetWanted(req.IDs, req.Wanted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The issues couldn't be changed.")
		return
	}
	queued := 0
	if req.Wanted && req.Search {
		ids := req.IDs
		queued = len(ids)
		s.background(func() {
			for _, id := range ids {
				s.searchComicIssue(id, queue.PriorityManual)
				time.Sleep(1500 * time.Millisecond)
			}
		})
	}
	writeJSON(w, http.StatusOK, map[string]int{"changed": n, "searching": queued})
}

// ---- searching ----

type comicRelease struct {
	indexers.Result
	Matches bool // accepted by comics.Evaluate
	Blocked bool
	Verdict comics.Verdict
}

// confidenceRank orders accepted releases: high, medium, low.
func confidenceRank(c string) int {
	switch c {
	case comics.ConfidenceHigh:
		return 0
	case comics.ConfidenceMedium:
		return 1
	}
	return 2
}

func (s *Server) comicReleases(ctx context.Context, sr comics.Series, is comics.Issue, all bool) ([]comicRelease, error) {
	return s.comicReleasesFor(ctx, sr, is, all, "")
}

// comicReleasesFor is comicReleases with the person's own search words
// (home fork), used instead of the issue's own searches when given.
func (s *Server) comicReleasesFor(ctx context.Context, sr comics.Series, is comics.Issue, all bool, own string) ([]comicRelease, error) {
	list, err := s.searchableIndexers()
	if err != nil {
		return nil, err
	}
	if len(list) == 0 && !s.getComicsOn() {
		return nil, errors.New("there are no indexers to search. Add one in Settings > Indexers & Search")
	}
	blocked := s.blockedKeys()
	seen := map[string]bool{}
	var out []comicRelease
	queries := comics.SearchQueries(sr, is)
	if own != "" {
		queries = []string{own}
	}
	for _, q := range queries {
		if ctx.Err() != nil {
			break
		}
		for _, res := range indexers.MergeResults(indexers.SearchAll(ctx, list, q, comicCategories)) {
			if seen[res.DownloadURL] {
				continue
			}
			seen[res.DownloadURL] = true
			res.Title = html.UnescapeString(res.Title) // some feeds escape twice ("&amp;amp;")
			v := comics.Evaluate(comics.Release{Title: res.Title, SizeBytes: res.SizeBytes, Categories: res.Categories}, sr, is)
			cr := comicRelease{Result: res, Matches: v.Reject == "", Verdict: v}
			cr.Blocked = blocked[blocklist.Key(res.Title)]
			out = append(out, cr) // rejected ones too: they're the search's "why not"
		}
		if !all && slices.ContainsFunc(out, func(c comicRelease) bool { return c.Matches }) {
			break
		}
	}
	// GetComics (home fork): searched too when switched on.
	if all || !slices.ContainsFunc(out, func(c comicRelease) bool { return c.Matches && c.Verdict.Confidence != comics.ConfidenceLow }) {
		for _, res := range s.getComicsResultsFor(ctx, sr, is, own) {
			if seen[res.DownloadURL] {
				continue
			}
			seen[res.DownloadURL] = true
			v := comics.Evaluate(comics.Release{Title: res.Title, SizeBytes: res.SizeBytes}, sr, is)
			out = append(out, comicRelease{Result: res, Matches: v.Reject == "", Verdict: v, Blocked: blocked[blocklist.Key(res.Title)]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Matches != out[j].Matches {
			return out[i].Matches
		}
		if a, b := confidenceRank(out[i].Verdict.Confidence), confidenceRank(out[j].Verdict.Confidence); a != b {
			return a < b
		}
		if out[i].Verdict.Score != out[j].Verdict.Score {
			return out[i].Verdict.Score > out[j].Verdict.Score
		}
		if out[i].Priority != out[j].Priority {
			return indexerRank(out[i].Priority) < indexerRank(out[j].Priority)
		}
		return out[i].SizeBytes > out[j].SizeBytes // the fuller scan
	})
	return out, nil
}

// searchComicIssue looks for an issue and grabs the best release.
func (s *Server) searchComicIssue(issueID int64, prio queue.Priority) bool {
	repo := s.comicRepo()
	is, err := repo.GetIssue(issueID)
	if err != nil || is.Status != comics.StatusMissing {
		return false
	}
	if busy, _ := repo.HasActiveDownload(is.ID); busy {
		return false
	}
	sr, err := repo.GetSeries(is.SeriesID)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results, err := s.comicReleases(ctx, sr, is, false)
	if err != nil {
		slog.Info("comics: search", "issue", issueID, "err", err)
		return false
	}
	sources := s.sourcesFor("")
	// This search's findings replace the issue's earlier review entries.
	_, _ = s.db.Exec(`DELETE FROM comic_reviews WHERE issue_id = ?`, is.ID)
	var doubtful []comicRelease
	for _, res := range results {
		if !res.Matches || res.Blocked || len(filterSources([]indexers.Result{res.Result}, sources)) == 0 {
			continue
		}
		if res.Verdict.Confidence == comics.ConfidenceLow {
			doubtful = append(doubtful, res) // a person decides (the review list)
			continue
		}
		_, err := s.grabComicIssue(sr, is, res.Title, res.DownloadURL, res.SizeBytes, res.Protocol, prio)
		s.logComicSearch(is.ID, "grabbed", results)
		return err == nil
	}
	if len(doubtful) > 0 {
		s.addComicReviews(is.ID, doubtful)
		s.logComicSearch(is.ID, "review", results)
		return false
	}
	s.logComicSearch(is.ID, "nothing", results)
	return false
}

// searchComicSeries searches every wanted, missing, released issue of a series.
func (s *Server) searchComicSeries(seriesID int64, prio queue.Priority) int {
	issues, err := s.comicRepo().Issues(seriesID)
	if err != nil {
		return 0
	}
	now := time.Now()
	n := 0
	for _, is := range issues {
		if !is.Wanted || is.Status != comics.StatusMissing || !is.Released(now) {
			continue
		}
		if s.searchComicIssue(is.ID, prio) {
			n++
		}
		time.Sleep(1500 * time.Millisecond)
	}
	return n
}

func (s *Server) handleSearchComicSeries(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.comicSeriesFromPath(w, r)
	if !ok {
		return
	}
	s.background(func() { s.searchComicSeries(sr.ID, queue.PriorityManual) })
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Searching the wanted issues of " + sr.Name + " in the background."})
}

func (s *Server) handleSearchComicIssue(w http.ResponseWriter, r *http.Request) {
	is, _, ok := s.comicIssueFromPath(w, r)
	if !ok {
		return
	}
	if s.searchComicIssue(is.ID, queue.PriorityManual) {
		writeJSON(w, http.StatusOK, map[string]any{"grabbed": 1, "message": "Found a release. It is downloading now."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grabbed": 0, "message": "Nothing suitable was found right now."})
}

func (s *Server) handleComicIssueReleases(w http.ResponseWriter, r *http.Request) {
	is, sr, ok := s.comicIssueFromPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	list, err := s.comicReleasesFor(ctx, sr, is, r.URL.Query().Get("all") != "", strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		writeError(w, http.StatusBadGateway, plainerror.Message(err))
		return
	}
	type payload struct {
		searchResultPayload
		Matches bool           `json:"matches"`
		Verdict comics.Verdict `json:"verdict"`
	}
	all := r.URL.Query().Get("all") != ""
	out := make([]payload, 0, len(list))
	for _, rel := range list {
		if !rel.Matches && !all {
			continue
		}
		p := payload{searchResultPayload: s.toSearchResultPayload(rel.Result), Matches: rel.Matches, Verdict: rel.Verdict}
		p.Blocklisted = rel.Blocked
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGrabComicIssue(w http.ResponseWriter, r *http.Request) {
	is, sr, ok := s.comicIssueFromPath(w, r)
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
	id, err := s.grabComicIssue(sr, is, req.Title, req.DownloadURL, req.SizeBytes, protocol, queue.PriorityManual)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"queueId": id})
}

func (s *Server) grabComicIssue(sr comics.Series, is comics.Issue, title, url string, size int64, protocol indexers.Protocol, prio queue.Priority) (int64, error) {
	if protocol == indexers.ProtocolTorrent && !s.torrentsEnabled() {
		return 0, errTorrentsDisabled
	}
	if s.comicsRoot() == "" {
		return 0, errors.New("Choose the comics folder in Comics settings first.")
	}
	repo := s.comicRepo()
	s.grabMu.Lock()
	busy, err := repo.HasActiveDownload(is.ID)
	if err == nil && busy {
		err = errors.New("A download for this issue is already running or waiting.")
	}
	var id int64
	if err == nil {
		id, err = s.QueueRepo.Enqueue(queue.Item{ReleaseTitle: title, NZBURL: url, SizeBytes: size, Protocol: queue.Protocol(protocol), Priority: prio})
	}
	if err == nil {
		if err = repo.LinkDownload(id, is.ID); err != nil {
			_ = s.QueueRepo.SetStatus(id, queue.StatusFailed, "couldn't link the download to its issue")
		}
	}
	if err == nil {
		_ = repo.SetState(is.ID, comics.StatusDownloading, "", 0)
	}
	s.grabMu.Unlock()
	if err != nil {
		return 0, err
	}
	_ = s.QueueRepo.LogActivity(id, "grabbed", fmt.Sprintf("Downloading %s #%s: %s", sr.Name, is.Number, title))
	s.dispatch.Kick()
	return id, nil
}

// ---- automation ----

// huntComics is the module's part of the automatic search: once a day each
// series' issue list is refreshed (new issues become wanted when the series
// follows new issues), then a few wanted, released, missing issues are searched.
func (s *Server) huntComics(ctx context.Context) {
	if !s.comicsEnabled() || !s.comicKeySet() || s.comicsRoot() == "" {
		return
	}
	st := s.comicMod()
	if !st.hunt.TryLock() {
		return
	}
	defer st.hunt.Unlock()
	repo := s.comicRepo()
	if list, err := repo.ListSeries(); err == nil {
		for _, sr := range list {
			if ctx.Err() != nil {
				return
			}
			if t, err := time.Parse(time.RFC3339, sr.CheckedAt); err == nil && time.Since(t) < 20*time.Hour {
				continue
			}
			rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			_, _ = s.refreshComicIssues(rctx, sr, false)
			cancel()
		}
	}
	missing, err := repo.WantedMissing(30)
	if err != nil {
		return
	}
	now := time.Now()
	n := 0
	for _, is := range missing {
		if ctx.Err() != nil || n >= 10 {
			return
		}
		if !is.Released(now) {
			continue
		}
		n++
		s.searchComicIssue(is.ID, queue.PriorityAutomatic)
		time.Sleep(1500 * time.Millisecond)
	}
}

// ---- the download pipeline ----

// prepareComicPipeline is preparePipeline's case for a comic download.
func (s *Server) prepareComicPipeline(item queue.Item, protocol indexers.Protocol) (run func(), ok bool, err error) {
	repo := s.comicRepo()
	var is comics.Issue
	for i := 0; i < 20; i++ {
		is, err = repo.IssueForDownload(item.ID)
		if !errors.Is(err, comics.ErrNotFound) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if errors.Is(err, comics.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, errors.New("That issue isn't in your library any more.")
	}
	sr, err := repo.GetSeries(is.SeriesID)
	if err != nil {
		return nil, true, errors.New("That issue's series isn't in your library any more.")
	}
	_ = repo.SetState(is.ID, comics.StatusDownloading, "", 0)
	return func() {
		if err := s.runComicPipeline(item.ID, sr, is, item.ReleaseTitle, item.NZBURL, protocol); err != nil {
			slog.Info("queue: comic download ended", "queueId", item.ID, "err", err)
		}
	}, true, nil
}

// comicDest is where an issue's file goes.
func (s *Server) comicDest(sr comics.Series, is comics.Issue, file string) (string, error) {
	root := s.comicsRoot()
	if root == "" {
		return "", errors.New("the comics folder isn't set. Choose one in Comics settings")
	}
	mode, repl := s.illegalCharSettings()
	folder := sr.Folder
	if folder == "" {
		parts := strings.Split(filepath.ToSlash(comics.Folder(sr)), "/")
		for i, p := range parts {
			parts[i] = organizer.Sanitize(p, mode, repl)
		}
		folder = filepath.Join(root, filepath.Join(parts...))
	}
	ext := strings.ToLower(filepath.Ext(file))
	return filepath.Join(folder, organizer.Sanitize(comics.FileName(sr, is), mode, repl)+ext), nil
}

// largestComicFile finds the comic file in a finished download.
func largestComicFile(dir string) (string, int64, error) {
	var best string
	var size int64
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !comics.IsComicFile(d.Name()) {
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > size {
			best, size = p, info.Size()
		}
		return nil
	})
	if err == nil && best == "" {
		err = errors.New("no comic file")
	}
	return best, size, err
}

func (s *Server) runComicPipeline(queueID int64, sr comics.Series, is comics.Issue, releaseTitle, downloadURL string, protocol indexers.Protocol) error {
	ctx, run := s.pipelines.begin(queueID)
	defer s.pipelines.end(queueID, run)
	if s.pausedBeforeStart(queueID) {
		return errPaused
	}
	repo := s.comicRepo()
	label := fmt.Sprintf("%s #%s", sr.Name, is.Number)
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
		s.unclaimComicIssue(is.ID) // home fork: not one another download has filed meanwhile
		_ = s.QueueRepo.LogActivity(queueID, "failed", fmt.Sprintf("%s: %s", label, plain))
		s.notifyItem("failed", comicItem(sr, is, releaseTitle, plain))
		if isBadRelease(stepErr) {
			if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: releaseTitle, Protocol: string(protocol), Reason: stepErr.Error()}); err == nil {
				s.background(func() { s.searchComicIssue(is.ID, queue.PriorityAutomatic) })
			}
		}
		return stepErr
	}
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
	nameHiddenComics(dir, releaseTitle)             // home fork: "file_0.bin" that is really a CBR
	if files := comicFilesIn(dir); len(files) > 1 { // home fork: a pack fills every issue it holds
		n, got, already := s.importComicPack(is, files)
		if n == 0 && already > 0 {
			// The same pack again (two copies grabbed): everything in it is
			// already filed, so there's nothing to fail.
			_ = s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, "")
			s.cleanupWorkDir(dir, protocol)
			_ = s.QueueRepo.LogActivity(queueID, "imported", fmt.Sprintf("%s: everything in this pack is already in your library", label))
			return nil
		}
		if n == 0 {
			return fail(badRelease(errors.New("the pack holds no issue the library is missing")))
		}
		if !got {
			s.unclaimComicIssue(is.ID) // home fork: unless another copy filed it meanwhile
		}
		if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
			return fail(err)
		}
		s.cleanupWorkDir(dir, protocol)
		_ = s.QueueRepo.LogActivity(queueID, "imported", fmt.Sprintf("%s: %d issues filed from the pack", label, n))
		s.notifyItem("imported", comicItem(sr, is, releaseTitle, ""))
		return nil
	}
	file, size, err := largestComicFile(dir)
	if err != nil {
		return fail(badRelease(errors.New("the download has no comic file (cbz, cbr, pdf)")))
	}
	// home fork: file it as the issue its name says ("Volume 023" is #23),
	// never as another one.
	if named := comics.NamedIssue(releaseTitle, comics.IsManga(sr)); named != "" && named != comics.NormalNumber(is.Number) && named != comics.AltNumber(is) {
		other, ok := s.comicIssueByNumber(sr.ID, named)
		otherSr := sr
		if !ok { // another edition of the same series may have it (home_comic_editions.go)
			otherSr, other, ok = s.comicIssueInOtherEdition(sr, named)
		}
		if !ok {
			return fail(fmt.Errorf("the release is #%s of %s, not #%s, and the library has no #%s to file it as", named, sr.Name, is.Number, named))
		}
		_ = repo.SetState(is.ID, comics.StatusMissing, "", 0)
		if otherSr.ID != sr.ID {
			_ = s.QueueRepo.LogActivity(queueID, "renamed", fmt.Sprintf("%s: the release is #%s, filed in the other edition (%d issues)", label, named, otherSr.IssueCount))
		} else {
			_ = s.QueueRepo.LogActivity(queueID, "renamed", fmt.Sprintf("%s: the release is #%s, filed as that", label, named))
		}
		sr, is = otherSr, other
		label = fmt.Sprintf("%s #%s", sr.Name, is.Number)
	}
	dest, err := s.comicDest(sr, is, file)
	if err != nil {
		return fail(err)
	}
	dest = freeName(dest)
	res, err := organizer.Import(file, dest, organizer.ConflictSkip, nil)
	if err != nil {
		return fail(fmt.Errorf("couldn't move the issue into your library: %w", err))
	}
	if res.Skipped {
		return fail(fmt.Errorf("a file is already at %s", dest))
	}
	if sr.Folder == "" {
		_ = repo.SetSeriesFolder(sr.ID, filepath.Dir(res.DestPath))
	}
	if err := repo.SetState(is.ID, comics.StatusDownloaded, res.DestPath, size); err != nil {
		return fail(err)
	}
	if err := s.QueueRepo.SetStatus(queueID, queue.StatusCompleted, ""); err != nil {
		return fail(err)
	}
	s.cleanupWorkDir(dir, protocol)
	_ = s.QueueRepo.LogActivity(queueID, "imported", label+" imported")
	ci := comicItem(sr, is, releaseTitle, "")
	ci.SizeBytes, ci.Path, ci.Quality = size, res.DestPath, strings.ToUpper(strings.TrimPrefix(filepath.Ext(res.DestPath), "."))
	s.notifyItem("imported", ci)
	slog.Info("comics: imported", "issue", is.ID, "series", sr.Name)
	return nil
}

// ---- importing an existing library ----

// POST /api/comics/import {"series":[{"cvId","folder","followNew","files":[{"issueCvId","path"}]}]}
// adds series (details and issues from ComicVine) and registers files that
// are already on disk, where they are. Used to bring over a Kapowarr library.
func (s *Server) handleImportComics(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Series []struct {
			CVID      int64  `json:"cvId"`
			Folder    string `json:"folder"`
			FollowNew bool   `json:"followNew"`
			Files     []struct {
				IssueCVID int64  `json:"issueCvId"`
				Path      string `json:"path"`
			} `json:"files"`
		} `json:"series"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Series) == 0 || len(req.Series) > 500 {
		writeError(w, http.StatusBadRequest, "Send the series to import.")
		return
	}
	if !s.comicKeySet() {
		writeError(w, http.StatusPreconditionFailed, comics.ErrNoKey.Error())
		return
	}
	root := s.comicsRoot()
	type result struct {
		CVID    int64  `json:"cvId"`
		Name    string `json:"name"`
		Issues  int    `json:"issues"`
		Files   int    `json:"files"`
		Missing int    `json:"missingFiles"`
		Error   string `json:"error,omitempty"`
	}
	var out []result
	ctx := r.Context()
	for _, in := range req.Series {
		res := result{CVID: in.CVID}
		folder := filepath.Clean(in.Folder)
		if in.Folder == "" || (root != "" && !strings.HasPrefix(folder, filepath.Clean(root)+string(filepath.Separator))) {
			folder = ""
		}
		want := "future"
		if !in.FollowNew {
			want = "none"
		}
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		sr, n, err := s.addComicSeries(cctx, in.CVID, want, folder)
		cancel()
		if err != nil {
			res.Error = plainerror.Message(err)
			out = append(out, res)
			continue
		}
		res.Name, res.Issues = sr.Name, n
		if folder != "" {
			_ = s.comicRepo().SetSeriesFolder(sr.ID, folder)
		}
		issues, _ := s.comicRepo().Issues(sr.ID)
		byCV := map[int64]comics.Issue{}
		for _, is := range issues {
			byCV[is.CVID] = is
		}
		for _, f := range in.Files {
			is, ok := byCV[f.IssueCVID]
			p := filepath.Clean(f.Path)
			if !ok || (root != "" && !strings.HasPrefix(p, filepath.Clean(root)+string(filepath.Separator))) {
				res.Missing++
				continue
			}
			st, err := os.Stat(p)
			if err != nil || st.IsDir() {
				res.Missing++
				continue
			}
			if err := s.comicRepo().SetState(is.ID, comics.StatusDownloaded, p, st.Size()); err == nil {
				_, _ = s.comicRepo().SetWanted([]int64{is.ID}, true)
				res.Files++
			}
		}
		out = append(out, res)
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- reading, images ----

// GET /api/comics/issues/{id}/file: the issue's file, for reading or saving.
func (s *Server) handleComicIssueFile(w http.ResponseWriter, r *http.Request) {
	is, _, ok := s.comicIssueFromPath(w, r)
	if !ok {
		return
	}
	if is.FilePath == "" {
		writeError(w, http.StatusNotFound, "This issue has no file yet.")
		return
	}
	f, err := os.Open(is.FilePath)
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
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filepath.Base(is.FilePath)))
	http.ServeContent(w, r, filepath.Base(is.FilePath), st.ModTime(), f)
}

// comicImage turns a ComicVine image URL into the proxied one the page uses.
func (s *Server) comicImage(u string) string {
	if u == "" {
		return ""
	}
	s.comicMod().images.Store(u, struct{}{})
	return "/api/comics/image?u=" + url.QueryEscape(u)
}

func (s *Server) comicImageKnown(u string) bool {
	if _, ok := s.comicMod().images.Load(u); ok {
		return true
	}
	var n int
	_ = s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM comic_series WHERE image_url = ?) + (SELECT COUNT(*) FROM comic_issues WHERE image_url = ?)`, u, u).Scan(&n)
	return n > 0
}

var comicImageClient = &http.Client{Timeout: 20 * time.Second}

// GET /api/comics/image?u=: a ComicVine image fetched for the page.
func (s *Server) handleComicImage(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	if u == "" || len(u) > 2048 || !strings.HasPrefix(u, "https://comicvine.gamespot.com/") || !s.comicImageKnown(u) {
		writeError(w, http.StatusNotFound, "Unknown image.")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad image address.")
		return
	}
	req.Header.Set("User-Agent", "Mediarium-home/"+s.version)
	resp, err := comicImageClient.Do(req)
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

// comicIssueByNumber finds a series' issue by its number (home fork).
func (s *Server) comicIssueByNumber(seriesID int64, number string) (comics.Issue, bool) {
	list, err := s.comicRepo().Issues(seriesID)
	if err != nil {
		return comics.Issue{}, false
	}
	for _, i := range list {
		if comics.NormalNumber(i.Number) == number {
			return i, true
		}
	}
	return comics.Issue{}, false
}

// retryComicDownload is Retry in Activity for a comic download (home fork):
// the same release again for the same issue, reusing what was already
// downloaded when it's still there, like a movie retry. It reports false
// when the queue item isn't a comic's.
func (s *Server) retryComicDownload(w http.ResponseWriter, item queue.Item) bool {
	repo := s.comicRepo()
	is, err := repo.IssueForDownload(item.ID)
	if errors.Is(err, comics.ErrNotFound) {
		return false
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "That issue isn't in your library any more.")
		return true
	}
	sr, err := repo.GetSeries(is.SeriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That series isn't in your library any more.")
		return true
	}
	keptDir, reuse := s.keptDownload(item)
	// The new item must not start before the earlier files are in its folder.
	release := s.dispatch.Hold()
	newID, err := s.grabComicIssue(sr, is, item.ReleaseTitle, item.NZBURL, item.SizeBytes, indexers.Protocol(item.Protocol), queue.PriorityManual)
	if err != nil {
		release()
		writeError(w, http.StatusConflict, err.Error())
		return true
	}
	if reuse {
		s.moveKeptDownload(keptDir, s.workDirFor(newID))
	}
	release()
	message := "Retrying, downloading again: " + item.ReleaseTitle
	if reuse {
		message = "Retrying from the files already downloaded: " + item.ReleaseTitle
	}
	s.queueItemEvent(item, "retried", queue.LevelInfo, message)
	_ = s.QueueRepo.Delete(item.ID)
	writeJSON(w, http.StatusAccepted, map[string]int64{"queueId": newID})
	return true
}
