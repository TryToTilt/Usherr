package api

// Home fork: the week's new comics (after Pullbox's "What's New"). Built from
// ComicVine's store dates rather than Pullbox's own data service. A week is
// fetched once and kept; the current and coming weeks are fetched again
// after twelve hours, as ComicVine fills them in. Pull follows the series
// (if it isn't in the library) and gets the issue.

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/queue"
)

func (s *Server) registerComicWeekRoutes(member, admin routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	member.HandleFunc("GET /api/comics/week", on(s.handleComicWeek))
	member.Need(auth.PermComics).HandleFunc("POST /api/comics/week/pull", on(s.handleComicWeekPull))
}

var comicWeekMu sync.Mutex // one ComicVine fetch at a time

// comicWeek returns a week's issues, from the cache when fresh enough.
func (s *Server) comicWeek(ctx context.Context, from, to string) ([]comics.WeekIssue, time.Time, error) {
	comicWeekMu.Lock()
	defer comicWeekMu.Unlock()
	var at, data string
	err := s.db.QueryRowContext(ctx, `SELECT fetched_at, data FROM comic_week_cache WHERE week_start = ?`, from).Scan(&at, &data)
	fetched, _ := time.Parse(time.RFC3339, at)
	thisWeek, _ := comics.WeekOf(time.Now())
	recheck := 12 * time.Hour
	if data == "[]" || data == "null" { // nothing dated yet: ask again sooner (home fork)
		recheck = 2 * time.Hour
	}
	stale := err != nil || (from >= thisWeek && time.Since(fetched) > recheck) ||
		(from < thisWeek && fetched.Format("2006-01-02") <= to) // a past week fetched before it was over
	if !stale {
		var list []comics.WeekIssue
		if json.Unmarshal([]byte(data), &list) == nil {
			return list, fetched, nil
		}
	}
	list, ferr := s.comicVine().StoreDateIssues(ctx, from, to)
	if ferr != nil {
		if err == nil { // ComicVine is unreachable: the older copy will do
			var old []comics.WeekIssue
			if json.Unmarshal([]byte(data), &old) == nil {
				return old, fetched, nil
			}
		}
		return nil, time.Time{}, ferr
	}
	b, _ := json.Marshal(list)
	now := time.Now().UTC()
	_, _ = s.db.ExecContext(ctx, `INSERT INTO comic_week_cache (week_start, fetched_at, data) VALUES (?, ?, ?)
		ON CONFLICT (week_start) DO UPDATE SET fetched_at = excluded.fetched_at, data = excluded.data`, from, now.Format(time.RFC3339), string(b))
	return list, now, nil
}

type comicWeekIssue struct {
	comics.WeekIssue
	SeriesID int64  `json:"seriesId,omitempty"` // in the library
	IssueID  int64  `json:"issueId,omitempty"`
	Status   string `json:"status,omitempty"` // missing, downloading, downloaded
	Wanted   bool   `json:"wanted,omitempty"`
}

// GET /api/comics/week?date=YYYY-MM-DD: the issues on sale that week.
func (s *Server) handleComicWeek(w http.ResponseWriter, r *http.Request) {
	day := time.Now()
	if d, err := time.Parse("2006-01-02", r.URL.Query().Get("date")); err == nil {
		day = d
	}
	from, to := comics.WeekOf(day)
	// The week before is fetched ahead, so going back is quick.
	prevFrom, prevTo := comics.WeekOf(day.AddDate(0, 0, -7))
	s.background(func() {
		bg, done := context.WithTimeout(context.Background(), 2*time.Minute)
		defer done()
		_, _, _ = s.comicWeek(bg, prevFrom, prevTo)
	})
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	list, at, err := s.comicWeek(ctx, from, to)
	if err != nil {
		writeUpstreamError(w, "read the week's comics from ComicVine", err)
		return
	}
	// What's in the library already, by ComicVine ids.
	series := map[int64]int64{}
	if rows, err := s.db.QueryContext(ctx, `SELECT cv_id, id FROM comic_series`); err == nil {
		for rows.Next() {
			var cv, id int64
			if rows.Scan(&cv, &id) == nil {
				series[cv] = id
			}
		}
		rows.Close()
	}
	type have struct {
		id     int64
		status string
		wanted bool
	}
	issues := map[int64]have{}
	if rows, err := s.db.QueryContext(ctx, `SELECT cv_id, id, status, wanted FROM comic_issues`); err == nil {
		for rows.Next() {
			var cv int64
			var h have
			if rows.Scan(&cv, &h.id, &h.status, &h.wanted) == nil {
				issues[cv] = h
			}
		}
		rows.Close()
	}
	out := make([]comicWeekIssue, 0, len(list))
	for _, wi := range list {
		wi.ImageURL = s.comicImage(wi.ImageURL) // through Mediarium: ComicVine refuses images linked from elsewhere
		ci := comicWeekIssue{WeekIssue: wi, SeriesID: series[wi.VolumeCVID]}
		if h, ok := issues[wi.CVID]; ok {
			ci.IssueID, ci.Status, ci.Wanted = h.id, h.status, h.wanted
		}
		out = append(out, ci)
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "fetchedAt": at, "issues": out})
}

// POST /api/comics/week/pull {issueCvId, volumeCvId}: follow the series (if
// it isn't in the library: new issues are wanted from now on) and get this
// issue.
func (s *Server) handleComicWeekPull(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IssueCVID  int64 `json:"issueCvId"`
		VolumeCVID int64 `json:"volumeCvId"`
	}
	if err := decodeJSON(r, &req); err != nil || req.IssueCVID <= 0 || req.VolumeCVID <= 0 {
		writeError(w, http.StatusBadRequest, "Choose an issue.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	repo := s.comicRepo()
	sr, err := repo.SeriesByCV(req.VolumeCVID)
	added := false
	if err != nil {
		if sr, _, err = s.addComicSeries(ctx, req.VolumeCVID, "future", ""); err != nil {
			writeUpstreamError(w, "add the series from ComicVine", err)
			return
		}
		added = true
	}
	var issueID int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM comic_issues WHERE cv_id = ?`, req.IssueCVID).Scan(&issueID); err != nil {
		// ComicVine listed the issue after the series was last read.
		if _, err := s.refreshComicIssues(ctx, sr, false); err == nil {
			_ = s.db.QueryRowContext(ctx, `SELECT id FROM comic_issues WHERE cv_id = ?`, req.IssueCVID).Scan(&issueID)
		}
	}
	if issueID == 0 {
		writeError(w, http.StatusNotFound, "ComicVine doesn't list that issue under the series yet. Try again later.")
		return
	}
	if _, err := repo.SetWanted([]int64{issueID}, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.background(func() { s.searchComicIssue(issueID, queue.PriorityManual) })
	msg := "Searching for it now."
	if added {
		msg = sr.Name + " is in your library now, and new issues will come in by themselves. Searching for this one."
	}
	writeJSON(w, http.StatusOK, map[string]any{"seriesId": sr.ID, "issueId": issueID, "message": msg})
}
