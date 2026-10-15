package api

// Home fork: the comics review list and search history (after Pullbox's
// intervention queue and search log). A search whose only matches are
// doubtful (a year that doesn't fit, an odd size) puts them here instead of
// grabbing one; accepting downloads it, rejecting blocklists it. Each
// issue's last search is kept with why every release was or wasn't taken.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/rdborg/mediarium/internal/blocklist"
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/queue"
)

func (s *Server) registerComicReviewRoutes(admin routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	admin.HandleFunc("GET /api/comics/reviews", on(s.handleComicReviews))
	admin.HandleFunc("POST /api/comics/reviews/{id}/accept", on(s.handleAcceptComicReview))
	admin.HandleFunc("POST /api/comics/reviews/{id}/reject", on(s.handleRejectComicReview))
	admin.HandleFunc("GET /api/comics/issues/{id}/search-log", on(s.handleComicSearchLog))
}

// addComicReviews puts doubtful matches for an issue on the review list (at
// most five, best first).
func (s *Server) addComicReviews(issueID int64, list []comicRelease) {
	for i, c := range list {
		if i == 5 {
			break
		}
		notes, _ := json.Marshal(c.Verdict.Notes)
		if _, err := s.db.Exec(`INSERT INTO comic_reviews (issue_id, title, download_url, size_bytes, protocol, indexer, score, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (issue_id, download_url) DO UPDATE SET score = excluded.score, notes = excluded.notes`,
			issueID, c.Title, c.DownloadURL, c.SizeBytes, string(c.Protocol), c.IndexerName, c.Verdict.Score, string(notes)); err != nil {
			slog.Info("comics: review list", "issue", issueID, "err", err)
		}
	}
}

type comicSearchEntry struct {
	Title      string   `json:"title"`
	Indexer    string   `json:"indexer,omitempty"`
	SizeBytes  int64    `json:"sizeBytes,omitempty"`
	Score      int      `json:"score,omitempty"`
	Confidence string   `json:"confidence,omitempty"`
	Notes      []string `json:"notes,omitempty"`
	Reject     string   `json:"reject,omitempty"`
	Blocked    bool     `json:"blocked,omitempty"`
}

// logComicSearch keeps an issue's last search: what was found, what was
// taken, and why the rest weren't.
func (s *Server) logComicSearch(issueID int64, outcome string, list []comicRelease) {
	var accepted, rejected []comicSearchEntry
	for _, c := range list {
		e := comicSearchEntry{Title: c.Title, Indexer: c.IndexerName, SizeBytes: c.SizeBytes, Blocked: c.Blocked}
		if c.Matches {
			e.Score, e.Confidence, e.Notes = c.Verdict.Score, c.Verdict.Confidence, c.Verdict.Notes
			accepted = append(accepted, e)
		} else if len(rejected) < 25 {
			e.Reject = c.Verdict.Reject
			rejected = append(rejected, e)
		}
	}
	d, _ := json.Marshal(map[string]any{"accepted": accepted, "rejected": rejected, "found": len(list)})
	_, _ = s.db.Exec(`INSERT INTO comic_search_log (issue_id, searched_at, outcome, details) VALUES (?, ?, ?, ?)
		ON CONFLICT (issue_id) DO UPDATE SET searched_at = excluded.searched_at, outcome = excluded.outcome, details = excluded.details`,
		issueID, time.Now().UTC().Format(time.RFC3339), outcome, string(d))
}

// GET /api/comics/issues/{id}/search-log
func (s *Server) handleComicSearchLog(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var at, outcome, details string
	if err := s.db.QueryRowContext(r.Context(), `SELECT searched_at, outcome, details FROM comic_search_log WHERE issue_id = ?`, id).Scan(&at, &outcome, &details); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"searched": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"searched": true, "at": at, "outcome": outcome, "details": json.RawMessage(details)})
}

type comicReviewPayload struct {
	ID        int64    `json:"id"`
	IssueID   int64    `json:"issueId"`
	SeriesID  int64    `json:"seriesId"`
	Series    string   `json:"series"`
	Number    string   `json:"number"`
	IssueName string   `json:"issueTitle"`
	CoverDate string   `json:"coverDate"`
	Title     string   `json:"title"`
	SizeBytes int64    `json:"sizeBytes"`
	Indexer   string   `json:"indexer"`
	Score     int      `json:"score"`
	Notes     []string `json:"notes"`
}

// GET /api/comics/reviews
func (s *Server) handleComicReviews(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT v.id, v.issue_id, i.series_id, s.name, i.number, i.title, i.cover_date,
			v.title, v.size_bytes, v.indexer, v.score, v.notes
		FROM comic_reviews v JOIN comic_issues i ON i.id = v.issue_id JOIN comic_series s ON s.id = i.series_id
		WHERE i.status = 'missing'
		ORDER BY s.name, i.sort_key, v.score DESC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []comicReviewPayload{}
	for rows.Next() {
		var p comicReviewPayload
		var notes string
		if rows.Scan(&p.ID, &p.IssueID, &p.SeriesID, &p.Series, &p.Number, &p.IssueName, &p.CoverDate, &p.Title, &p.SizeBytes, &p.Indexer, &p.Score, &notes) == nil {
			_ = json.Unmarshal([]byte(notes), &p.Notes)
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/comics/reviews/{id}/accept: download it; the issue's other
// doubtful matches leave the list.
func (s *Server) handleAcceptComicReview(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var issueID, size int64
	var title, url, protocol string
	if err := s.db.QueryRowContext(r.Context(), `SELECT issue_id, title, download_url, size_bytes, protocol FROM comic_reviews WHERE id = ?`, id).
		Scan(&issueID, &title, &url, &size, &protocol); err != nil {
		writeError(w, http.StatusNotFound, "That's no longer on the review list.")
		return
	}
	repo := s.comicRepo()
	is, err := repo.GetIssue(issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That issue is gone.")
		return
	}
	sr, err := repo.GetSeries(is.SeriesID)
	if err != nil {
		writeError(w, http.StatusNotFound, "That series is gone.")
		return
	}
	qid, err := s.grabComicIssue(sr, is, title, url, size, indexers.Protocol(protocol), queue.PriorityManual)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM comic_reviews WHERE issue_id = ?`, issueID)
	writeJSON(w, http.StatusOK, map[string]any{"queueId": qid, "message": "Downloading " + sr.Name + " #" + is.Number + "."})
}

// POST /api/comics/reviews/{id}/reject: not this issue. It's blocklisted, so
// no search offers it again.
func (s *Server) handleRejectComicReview(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var title, protocol string
	if err := s.db.QueryRowContext(r.Context(), `SELECT title, protocol FROM comic_reviews WHERE id = ?`, id).Scan(&title, &protocol); err != nil {
		writeError(w, http.StatusNotFound, "That's no longer on the review list.")
		return
	}
	if err := s.Blocklist.Add(blocklist.Entry{ReleaseTitle: title, Protocol: protocol, Reason: "Rejected on the comics review list: not this issue"}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.db.ExecContext(r.Context(), `DELETE FROM comic_reviews WHERE id = ?`, id)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Rejected. It won't be offered again."})
}
