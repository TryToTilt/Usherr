package api

// Home fork: "Story arcs for you" on the Comics library. A background job
// reads, once per issue, which story arcs the issues you have belong to
// (ComicVine gives that only one issue at a time, and allows about 200 calls
// an hour, so it does at most 150 an hour). The row then shows the arcs you
// already hold the most of; the browser moves the ones you've been reading up.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/comics"
)

const (
	arcsForYouEvery  = time.Hour
	arcsForYouPerRun = 150
)

var arcsForYouRunning sync.Mutex

// comicArcsJob reads the story arcs of issues not checked yet: the ones you
// have first, then the ones wanted.
func (s *Server) comicArcsJob(ctx context.Context) {
	if !s.comicsEnabled() || !arcsForYouRunning.TryLock() {
		return
	}
	defer arcsForYouRunning.Unlock()
	cv := s.comicVine()
	if cv == nil {
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT i.cv_id FROM comic_issues i
		WHERE i.cv_id > 0 AND (i.status = 'downloaded' OR i.wanted = 1)
		  AND NOT EXISTS (SELECT 1 FROM home_comic_issue_arcs a WHERE a.issue_cv_id = i.cv_id)
		ORDER BY i.status = 'downloaded' DESC, i.id DESC LIMIT ?`, arcsForYouPerRun)
	if err != nil {
		return
	}
	var todo []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			todo = append(todo, id)
		}
	}
	rows.Close()
	for _, id := range todo {
		arcs, err := cv.IssueArcs(ctx, id)
		if errors.Is(err, comics.ErrNoKey) || ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("comics: story arcs of issue %d: %v", id, err)
			return // most likely the hourly limit: try again next hour
		}
		b, _ := json.Marshal(arcs)
		_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO home_comic_issue_arcs (issue_cv_id, arcs) VALUES (?, ?)`, id, string(b))
	}
}

type arcForYouIssue struct {
	IssueID  int64  `json:"issueId"`
	SeriesID int64  `json:"seriesId"`
	Have     bool   `json:"have"`
	Image    string `json:"-"`
	Store    string `json:"-"`
}

type arcForYou struct {
	CVID      int64            `json:"cvId"`
	Name      string           `json:"name"`
	ImageURL  string           `json:"imageUrl,omitempty"`
	LibraryID int64            `json:"libraryId,omitempty"` // when it's in the Story arcs tab
	Have      int              `json:"have"`
	Series    int              `json:"series"` // how many of your series it runs through
	Issues    []arcForYouIssue `json:"issues"`
}

// GET /api/comics/arcs/for-you: the arcs your issues belong to, the ones you
// have most of first.
func (s *Server) handleComicArcsForYou(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT i.id, i.series_id, i.status, i.image_url, i.store_date, a.arcs
		FROM home_comic_issue_arcs a JOIN comic_issues i ON i.cv_id = a.issue_cv_id
		WHERE a.arcs != '[]'`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	by := map[int64]*arcForYou{}
	for rows.Next() {
		var is arcForYouIssue
		var status, raw string
		if rows.Scan(&is.IssueID, &is.SeriesID, &status, &is.Image, &is.Store, &raw) != nil {
			continue
		}
		is.Have = status == "downloaded"
		var arcs []comics.ArcRef
		if json.Unmarshal([]byte(raw), &arcs) != nil {
			continue
		}
		for _, a := range arcs {
			x := by[a.CVID]
			if x == nil {
				x = &arcForYou{CVID: a.CVID, Name: a.Name}
				by[a.CVID] = x
			}
			x.Issues = append(x.Issues, is)
		}
	}
	rows.Close()
	inLib := map[int64]int64{}
	if lr, err := s.db.QueryContext(r.Context(), `SELECT id, cv_id, image_url FROM comic_arcs`); err == nil {
		for lr.Next() {
			var id, cv int64
			var img string
			if lr.Scan(&id, &cv, &img) == nil {
				inLib[cv] = id
				if x := by[cv]; x != nil && img != "" {
					x.ImageURL = img
				}
			}
		}
		lr.Close()
	}
	out := []arcForYou{}
	for _, x := range by {
		sort.Slice(x.Issues, func(i, j int) bool { return x.Issues[i].Store < x.Issues[j].Store })
		seen := map[int64]bool{}
		for _, is := range x.Issues {
			if is.Have {
				x.Have++
			}
			seen[is.SeriesID] = true
			if x.ImageURL == "" && is.Image != "" {
				x.ImageURL = is.Image // the cover of its first issue you have or want
			}
		}
		x.Series = len(seen)
		x.LibraryID = inLib[x.CVID]
		if x.Have == 0 {
			continue
		}
		x.ImageURL = s.comicImage(x.ImageURL)
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Have != out[j].Have {
			return out[i].Have > out[j].Have
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > 40 {
		out = out[:40]
	}
	writeJSON(w, http.StatusOK, out)
}
