package api

// Home fork: story arcs (after Pullbox's story arcs). An arc is a reading
// list across series, in ComicVine's reading order, tied to the library by
// ComicVine ids. "Get the whole arc" adds each series it runs through (not
// followed: only the arc's issues are wanted) and searches for every issue
// still missing.

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/queue"
)

func (s *Server) registerComicArcRoutes(member, admin routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	member.HandleFunc("GET /api/comics/arcs", on(s.handleListComicArcs))
	member.HandleFunc("GET /api/comics/arcs/for-you", on(s.handleComicArcsForYou))
	member.HandleFunc("GET /api/comics/arcs/{id}", on(s.handleGetComicArc))
	admin.HandleFunc("GET /api/comics/arcs/lookup", on(s.handleLookupComicArcs))
	admin.HandleFunc("POST /api/comics/arcs", on(s.handleAddComicArc))
	admin.HandleFunc("POST /api/comics/arcs/{id}/want", on(s.handleWantComicArc))
	admin.HandleFunc("POST /api/comics/arcs/{id}/refresh", on(s.handleRefreshComicArc))
	admin.HandleFunc("DELETE /api/comics/arcs/{id}", on(s.handleDeleteComicArc))
}

type comicArcPayload struct {
	ID int64 `json:"id"`
	comics.Arc
	Total       int    `json:"total"`
	Have        int    `json:"have"`
	Wanted      int    `json:"wanted"`
	Downloading int    `json:"downloading"`
	Busy        string `json:"busy,omitempty"` // what "get the whole arc" is doing now
}

// arcProgress is what each "get the whole arc" is doing.
var arcProgress sync.Map // arc id -> string

const arcCounts = `SELECT COUNT(*),
	COALESCE(SUM(i.status = 'downloaded'), 0),
	COALESCE(SUM(i.wanted = 1 AND i.status = 'missing'), 0),
	COALESCE(SUM(i.status = 'downloading'), 0)
	FROM comic_arc_issues m LEFT JOIN comic_issues i ON i.cv_id = m.issue_cv_id WHERE m.arc_id = ?`

func (s *Server) arcPayload(ctx context.Context, id int64) (comicArcPayload, error) {
	var p comicArcPayload
	err := s.db.QueryRowContext(ctx, `SELECT id, cv_id, name, publisher, description, image_url FROM comic_arcs WHERE id = ?`, id).
		Scan(&p.ID, &p.CVID, &p.Name, &p.Publisher, &p.Description, &p.ImageURL)
	if err != nil {
		return p, err
	}
	_ = s.db.QueryRowContext(ctx, arcCounts, id).Scan(&p.Total, &p.Have, &p.Wanted, &p.Downloading)
	p.ImageURL = s.comicImage(p.ImageURL)
	if v, ok := arcProgress.Load(id); ok {
		p.Busy, _ = v.(string)
	}
	return p, nil
}

// GET /api/comics/arcs
func (s *Server) handleListComicArcs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id FROM comic_arcs ORDER BY name COLLATE NOCASE`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []comicArcPayload{}
	for _, id := range ids {
		if p, err := s.arcPayload(r.Context(), id); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type comicArcIssuePayload struct {
	comics.ArcIssue
	SeriesID int64  `json:"seriesId,omitempty"`
	IssueID  int64  `json:"issueId,omitempty"`
	Status   string `json:"status,omitempty"`
	Wanted   bool   `json:"wanted,omitempty"`
}

// GET /api/comics/arcs/{id}: the arc and its issues in reading order.
func (s *Server) handleGetComicArc(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	p, err := s.arcPayload(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That story arc isn't in your library.")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT m.position, m.issue_cv_id, m.volume_cv_id, m.series, m.number, m.title, m.cover_date, m.store_date,
			COALESCE(NULLIF(i.image_url, ''), m.image_url), COALESCE(i.series_id, 0), COALESCE(i.id, 0), COALESCE(i.status, ''), COALESCE(i.wanted, 0)
		FROM comic_arc_issues m LEFT JOIN comic_issues i ON i.cv_id = m.issue_cv_id
		WHERE m.arc_id = ? ORDER BY m.position`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	issues := []comicArcIssuePayload{}
	for rows.Next() {
		var m comicArcIssuePayload
		if rows.Scan(&m.Position, &m.CVID, &m.VolumeCVID, &m.Series, &m.Number, &m.Title, &m.CoverDate, &m.StoreDate,
			&m.ImageURL, &m.SeriesID, &m.IssueID, &m.Status, &m.Wanted) == nil {
			m.ImageURL = s.comicImage(m.ImageURL)
			issues = append(issues, m)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"arc": p, "issues": issues})
}

// GET /api/comics/arcs/lookup?q=
func (s *Server) handleLookupComicArcs(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeError(w, http.StatusBadRequest, "Type a story arc's name.")
		return
	}
	list, err := s.comicVine().SearchArcs(r.Context(), q)
	if err != nil {
		writeUpstreamError(w, "search ComicVine's story arcs", err)
		return
	}
	for k := range list {
		list[k].ImageURL = s.comicImage(list[k].ImageURL)
	}
	writeJSON(w, http.StatusOK, list)
}

// saveComicArc fetches an arc from ComicVine and stores it with its issues.
func (s *Server) saveComicArc(ctx context.Context, cvID int64) (int64, error) {
	arc, issues, err := s.comicVine().GetArc(ctx, cvID)
	if err != nil {
		return 0, err
	}
	if len(issues) == 0 {
		return 0, errors.New("ComicVine lists no issues for this story arc")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO comic_arcs (cv_id, name, publisher, description, image_url, checked_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (cv_id) DO UPDATE SET name = excluded.name, publisher = excluded.publisher, description = excluded.description,
			image_url = excluded.image_url, checked_at = excluded.checked_at
		RETURNING id`, arc.CVID, arc.Name, arc.Publisher, arc.Description, arc.ImageURL, time.Now().UTC().Format(time.RFC3339)).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM comic_arc_issues WHERE arc_id = ?`, id); err != nil {
		return 0, err
	}
	for _, m := range issues {
		if _, err := tx.ExecContext(ctx, `INSERT INTO comic_arc_issues (arc_id, position, issue_cv_id, volume_cv_id, series, number, title, cover_date, store_date, image_url)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, m.Position, m.CVID, m.VolumeCVID, m.Series, m.Number, m.Title, m.CoverDate, m.StoreDate, m.ImageURL); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// POST /api/comics/arcs {cvId, want}
func (s *Server) handleAddComicArc(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CVID int64 `json:"cvId"`
		Want bool  `json:"want"`
	}
	if err := decodeJSON(r, &req); err != nil || req.CVID <= 0 {
		writeError(w, http.StatusBadRequest, "Choose a story arc.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	id, err := s.saveComicArc(ctx, req.CVID)
	if err != nil {
		writeUpstreamError(w, "read the story arc from ComicVine", err)
		return
	}
	if req.Want {
		s.startWantComicArc(id)
	}
	p, _ := s.arcPayload(r.Context(), id)
	writeJSON(w, http.StatusOK, p)
}

// POST /api/comics/arcs/{id}/want
func (s *Server) handleWantComicArc(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if _, err := s.arcPayload(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "That story arc isn't in your library.")
		return
	}
	if !s.startWantComicArc(id) {
		writeError(w, http.StatusConflict, "Already getting this arc.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Getting the whole arc: adding its series and searching for every missing issue."})
}

// startWantComicArc runs "get the whole arc" in the background, once at a
// time per arc.
func (s *Server) startWantComicArc(id int64) bool {
	if _, busy := arcProgress.LoadOrStore(id, "Starting"); busy {
		return false
	}
	s.background(func() {
		defer arcProgress.Delete(id)
		if err := s.wantComicArc(context.Background(), id); err != nil {
			slog.Info("comics: get story arc", "arc", id, "err", err)
		}
	})
	return true
}

func (s *Server) wantComicArc(ctx context.Context, arcID int64) error {
	rows, err := s.db.QueryContext(ctx, `SELECT issue_cv_id, volume_cv_id, series FROM comic_arc_issues WHERE arc_id = ? ORDER BY position`, arcID)
	if err != nil {
		return err
	}
	type member struct {
		issue, volume int64
		series        string
	}
	var members []member
	for rows.Next() {
		var m member
		if rows.Scan(&m.issue, &m.volume, &m.series) == nil {
			members = append(members, m)
		}
	}
	rows.Close()
	repo := s.comicRepo()
	// Each series the arc runs through, in the library (added ones aren't
	// followed: only the arc's issues are wanted).
	seriesOf := map[int64]comics.Series{}
	n := 0
	for _, m := range members {
		if _, done := seriesOf[m.volume]; done {
			continue
		}
		n++
		arcProgress.Store(arcID, "Adding series "+strconv.Itoa(n)+": "+m.series)
		sr, err := repo.SeriesByCV(m.volume)
		if err != nil {
			actx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			sr, _, err = s.addComicSeries(actx, m.volume, "none", "")
			cancel()
			if err != nil {
				slog.Info("comics: arc series", "series", m.series, "err", err)
				continue
			}
		}
		seriesOf[m.volume] = sr
	}
	var want []int64
	refreshed := map[int64]bool{}
	for _, m := range members {
		var issueID int64
		err := s.db.QueryRowContext(ctx, `SELECT id FROM comic_issues WHERE cv_id = ?`, m.issue).Scan(&issueID)
		if errors.Is(err, sql.ErrNoRows) && !refreshed[m.volume] {
			if sr, ok := seriesOf[m.volume]; ok {
				refreshed[m.volume] = true
				rctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				_, _ = s.refreshComicIssues(rctx, sr, false)
				cancel()
				_ = s.db.QueryRowContext(ctx, `SELECT id FROM comic_issues WHERE cv_id = ?`, m.issue).Scan(&issueID)
			}
		}
		if issueID > 0 {
			want = append(want, issueID)
		}
	}
	if _, err := repo.SetWanted(want, true); err != nil {
		return err
	}
	for k, id := range want {
		arcProgress.Store(arcID, "Searching "+strconv.Itoa(k+1)+" of "+strconv.Itoa(len(want)))
		s.searchComicIssue(id, queue.PriorityManual) // only if still missing
		time.Sleep(1500 * time.Millisecond)
	}
	return nil
}

// POST /api/comics/arcs/{id}/refresh: read the arc from ComicVine again.
func (s *Server) handleRefreshComicArc(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	p, err := s.arcPayload(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That story arc isn't in your library.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	if _, err := s.saveComicArc(ctx, p.CVID); err != nil {
		writeUpstreamError(w, "read the story arc from ComicVine", err)
		return
	}
	p, _ = s.arcPayload(r.Context(), id)
	writeJSON(w, http.StatusOK, p)
}

// DELETE /api/comics/arcs/{id}: the reading list goes; series, issues and
// files stay.
func (s *Server) handleDeleteComicArc(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM comic_arc_issues WHERE arc_id = ?`, id)
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM comic_arcs WHERE id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "The story arc was removed. Its series, issues and files stay."})
}
