package api

// Usherr: reading comics, per person: the Continue, Want to Read and Read
// views, marking issues read or unread, how someone likes to read, and fixing a
// series that was matched to the wrong ComicVine volume.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/comics"
)

func (s *Server) registerComicReadingRoutes(admin, member routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	member.HandleFunc("GET /api/comics/reading", on(s.handleComicReading))
	member.HandleFunc("PUT /api/comics/reading/mark", on(s.handleComicMarkRead))
	member.HandleFunc("PUT /api/comics/reading/want", on(s.handleComicWantRead))
	member.HandleFunc("PUT /api/comics/reader-prefs", on(s.handleComicReaderPrefs))
	admin.HandleFunc("POST /api/comics/{id}/match/preview", on(s.handleComicMatchPreview))
	admin.HandleFunc("POST /api/comics/{id}/match", on(s.handleComicMatch))
}

// comicPlaceSaved is told each place the reader sends: reaching the last page
// finishes the issue, read here or later from a device that was offline.
func (s *Server) comicPlaceSaved(user int64, item string, position, duration float64) {
	rest, ok := strings.CutPrefix(item, "comic:")
	if !ok || duration <= 0 || position < duration {
		return
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return
	}
	_ = s.comicRepo().MarkRead(user, []int64{id}, true, time.Now())
}

// GET /api/comics/reading: this person's Continue, Want to Read and Read.
func (s *Server) handleComicReading(w http.ResponseWriter, r *http.Request) {
	v, err := s.comicRepo().Reading(r.Context(), requestUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Your reading couldn't be read.")
		return
	}
	for _, l := range [][]comics.ReadingItem{v.Continue, v.Want, v.Read} {
		for i := range l {
			l[i].ImageURL = s.comicImage(l[i].ImageURL)
		}
	}
	writeJSON(w, http.StatusOK, v)
}

// PUT /api/comics/reading/mark {"issueId", "scope": "issue"|"upto"|"series", "read"},
// or {"issueIds": [...], "read"} for a list of issues.
func (s *Server) handleComicMarkRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IssueID  int64   `json:"issueId"`
		IssueIDs []int64 `json:"issueIds"`
		Scope    string  `json:"scope"`
		Read     bool    `json:"read"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IssueIDs) > 5000 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	repo := s.comicRepo()
	now := time.Now()
	ids := req.IssueIDs
	if len(ids) == 0 {
		if req.Scope != comics.ScopeIssue && req.Scope != comics.ScopeUpTo && req.Scope != comics.ScopeSeries {
			writeError(w, http.StatusBadRequest, "That request couldn't be read.")
			return
		}
		is, err := repo.GetIssue(req.IssueID)
		if err != nil {
			writeError(w, http.StatusNotFound, "That issue isn't in your library.")
			return
		}
		if ids, err = repo.ScopeIssues(is, req.Scope, req.Read, now); err != nil {
			writeError(w, http.StatusInternalServerError, "The issues couldn't be read.")
			return
		}
	}
	if err := repo.MarkRead(requestUserID(r), ids, req.Read, now); err != nil {
		writeError(w, http.StatusInternalServerError, "The issues couldn't be changed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issueIds": ids})
}

// PUT /api/comics/reading/want {"seriesId", "issueId" (none: the whole series), "want"}
func (s *Server) handleComicWantRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SeriesID int64 `json:"seriesId"`
		IssueID  int64 `json:"issueId"`
		Want     bool  `json:"want"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	repo := s.comicRepo()
	if req.IssueID != 0 {
		is, err := repo.GetIssue(req.IssueID)
		if err != nil {
			writeError(w, http.StatusNotFound, "That issue isn't in your library.")
			return
		}
		req.SeriesID = is.SeriesID
	} else if _, err := repo.GetSeries(req.SeriesID); err != nil {
		writeError(w, http.StatusNotFound, "That series isn't in your library.")
		return
	}
	if err := repo.SetWantRead(requestUserID(r), req.SeriesID, req.IssueID, req.Want, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, "Your list couldn't be changed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/comics/reader-prefs {"seriesId" (none: your default), "direction", "layout"};
// only what is sent changes.
func (s *Server) handleComicReaderPrefs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SeriesID int64 `json:"seriesId"`
		comics.ReaderPrefs
	}
	if err := decodeJSON(r, &req); err != nil || !comics.ValidDirection(req.Direction) || !comics.ValidLayout(req.Layout) {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	repo := s.comicRepo()
	if req.SeriesID != 0 {
		if _, err := repo.GetSeries(req.SeriesID); err != nil {
			writeError(w, http.StatusNotFound, "That series isn't in your library.")
			return
		}
	}
	if err := repo.SetReaderPrefs(requestUserID(r), req.SeriesID, req.ReaderPrefs); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// comicMatch fetches the volume a series would be pointed at and pairs up its
// issues. ComicVine's own pacing applies: this is one request for the volume and
// one for each hundred issues.
func (s *Server) comicMatch(w http.ResponseWriter, r *http.Request) (sr, info comics.Series, plan comics.RepointPlan, ok bool) {
	sr, ok = s.comicSeriesFromPath(w, r)
	if !ok {
		return sr, info, plan, false
	}
	var req struct {
		CVID int64 `json:"cvId"`
	}
	if err := decodeJSON(r, &req); err != nil || req.CVID <= 0 {
		writeError(w, http.StatusBadRequest, "Choose a volume.")
		return sr, info, plan, false
	}
	if req.CVID == sr.CVID {
		writeError(w, http.StatusBadRequest, "This series already points at that volume.")
		return sr, info, plan, false
	}
	repo := s.comicRepo()
	if other, err := repo.SeriesByCV(req.CVID); err == nil {
		writeError(w, http.StatusConflict, "That volume is already in your library as "+other.Name+". Remove one of them first.")
		return sr, info, plan, false
	}
	here, err := repo.Issues(sr.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The issues couldn't be read.")
		return sr, info, plan, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	cv := s.comicVine()
	if info, err = cv.GetSeries(ctx, req.CVID); err != nil {
		writeUpstreamError(w, "read the volume from ComicVine", err)
		return sr, info, plan, false
	}
	incoming, err := cv.SeriesIssues(ctx, req.CVID)
	if err != nil {
		writeUpstreamError(w, "read the issues from ComicVine", err)
		return sr, info, plan, false
	}
	return sr, info, comics.PlanRepoint(here, incoming), true
}

// POST /api/comics/{id}/match/preview {"cvId"}: what choosing that volume would
// do. Nothing changes.
func (s *Server) handleComicMatchPreview(w http.ResponseWriter, r *http.Request) {
	_, info, plan, ok := s.comicMatch(w, r)
	if !ok {
		return
	}
	info.ImageURL = s.comicImage(info.ImageURL)
	files, noMatch := 0, []string{}
	for _, m := range plan.Matched {
		if m.Here.Status != comics.StatusMissing {
			files++
		}
	}
	for _, i := range plan.Kept {
		noMatch = append(noMatch, i.Number)
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": info, "files": files, "noMatch": noMatch, "added": len(plan.New), "removed": len(plan.Dropped)})
}

// POST /api/comics/{id}/match {"cvId"}: point the series at that volume. Issues
// keep their rows by number, so read state and places stay; no file moves (the
// Rename tool in Comics settings does that, when someone runs it).
func (s *Server) handleComicMatch(w http.ResponseWriter, r *http.Request) {
	sr, info, plan, ok := s.comicMatch(w, r)
	if !ok {
		return
	}
	repo := s.comicRepo()
	if err := repo.Repoint(sr, info, plan, time.Now()); err != nil {
		if errors.Is(err, comics.ErrIssueElsewhere) {
			writeError(w, http.StatusConflict, "An issue of that volume already belongs to another series in your library. Remove that series first.")
			return
		}
		writeError(w, http.StatusInternalServerError, "The series couldn't be changed.")
		return
	}
	updated, err := repo.GetSeries(sr.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The series couldn't be read.")
		return
	}
	found := s.claimComicFiles(updated) // files in its folder that the new list names
	updated.ImageURL = s.comicImage(updated.ImageURL)
	writeJSON(w, http.StatusOK, map[string]any{"series": updated, "found": found})
}
