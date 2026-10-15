package api

// Home fork: notifications. Scenes never notify (their pages live under
// /scenes and their downloads are scene downloads); comics notify like
// books ("ready to read").

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/notify"
)

// sceneSilent reports whether a notification is about the Scenes module.
func sceneSilent(link string) bool {
	return link == "/scenes" || strings.HasPrefix(link, "/scenes/") || strings.HasPrefix(link, "/scenes?")
}

// isSceneDownload reports whether a queue item is a scene download.
func (s *Server) isSceneDownload(queueID int64) bool {
	if queueID <= 0 {
		return false
	}
	ids, err := s.sceneRepo().SceneDownloadIDs()
	return err == nil && ids[queueID]
}

// comicItem is the notification item for one comic issue.
func comicItem(sr comics.Series, is comics.Issue, release, reason string) notify.Item {
	return notify.Item{
		Media: "comic", Title: fmt.Sprintf("%s #%s", sr.Name, is.Number), Year: sr.StartYear,
		Release: release, Reason: reason, PosterURL: is.ImageURL, LinkPath: fmt.Sprintf("/comics/%d", sr.ID),
	}
}

// ---- weekly "what's new" ----

const keyDigestLast = "home.digest_last" // the date the last digest went out

// whatsNewJob sends the weekly "what's new" on Sunday from 18:00 (local
// time), once a week, to the targets that listen for downloads.
func (s *Server) whatsNewJob(context.Context) {
	now := time.Now()
	if now.Weekday() != time.Sunday || now.Hour() < 18 {
		return
	}
	today := now.Format("2006-01-02")
	if last, _ := s.Settings.Get(keyDigestLast); last == today {
		return
	}
	title, message, ok := s.weeklyDigest(now.Add(-7 * 24 * time.Hour))
	_ = s.Settings.Set(keyDigestLast, today, false)
	if !ok {
		return
	}
	s.notifyEvent("imported", title, message)
}

// weeklyDigest sums up what joined the library since since: movies, shows
// (with how many episodes), albums, books and comics. Scenes are left out.
func (s *Server) weeklyDigest(since time.Time) (title, message string, ok bool) {
	rows, err := s.db.Query(`SELECT id, COALESCE(movie_id, 0), COALESCE(series_id, 0), COALESCE(album_id, 0), COALESCE(book_id, 0), COALESCE(book_format, ''), episode
		FROM download_queue WHERE status = 'completed' AND completed_at >= ?`, since.UTC().Format(time.RFC3339))
	if err != nil {
		return "", "", false
	}
	type item struct {
		id, movie, series, album, book int64
		format                         string
		episode                        sql.NullInt64
	}
	var items []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.movie, &it.series, &it.album, &it.book, &it.format, &it.episode) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	scenes, _ := s.sceneRepo().SceneDownloadIDs()
	comicIDs := map[int64]bool{}
	if cr, err := s.db.Query(`SELECT queue_id FROM comic_downloads`); err == nil {
		for cr.Next() {
			var id int64
			if cr.Scan(&id) == nil {
				comicIDs[id] = true
			}
		}
		cr.Close()
	}

	var movies, books, audiobooks, albums []string
	shows := map[string]int{}
	var showOrder []string
	comicCount := 0
	seen := map[string]bool{}
	add := func(list *[]string, name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			*list = append(*list, name)
		}
	}
	for _, it := range items {
		switch {
		case scenes[it.id]:
			continue
		case comicIDs[it.id]:
			comicCount++
		case it.series > 0:
			if sr, err := s.MovieRepo.GetSeries(it.series); err == nil {
				if _, ok := shows[sr.Title]; !ok {
					showOrder = append(showOrder, sr.Title)
				}
				n := 1
				if !it.episode.Valid || it.episode.Int64 == 0 { // a season pack
					n = 0
				}
				shows[sr.Title] += n
			}
		case it.album > 0:
			if al, err := s.MusicRepo.GetAlbum(it.album); err == nil {
				add(&albums, al.Title)
			}
		case it.book > 0:
			if b, err := s.BookRepo.Get(it.book); err == nil {
				if it.format == "audiobook" {
					add(&audiobooks, b.Title)
				} else {
					add(&books, b.Title)
				}
			}
		case it.movie > 0:
			if m, err := s.MovieRepo.Get(it.movie); err == nil {
				add(&movies, m.Title)
			}
		}
	}
	var lines []string
	list := func(label string, names []string) {
		if len(names) == 0 {
			return
		}
		shown := names
		if len(shown) > 6 {
			shown = append(append([]string{}, names[:6]...), fmt.Sprintf("and %d more", len(names)-6))
		}
		lines = append(lines, fmt.Sprintf("%s (%d): %s", label, len(names), strings.Join(shown, ", ")))
	}
	list("Movies", movies)
	if len(showOrder) > 0 {
		var parts []string
		for _, t := range showOrder {
			if n := shows[t]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s (%d)", t, n))
			} else {
				parts = append(parts, t)
			}
		}
		list("Shows", parts)
	}
	list("Books", books)
	list("Audiobooks", audiobooks)
	list("Albums", albums)
	if comicCount > 0 {
		lines = append(lines, fmt.Sprintf("Comics: %d new issue%s", comicCount, map[bool]string{true: "", false: "s"}[comicCount == 1]))
	}
	if len(lines) == 0 {
		return "", "", false
	}
	return "This week in Mediarium", strings.Join(lines, "\n"), true
}

// GET /api/home/digest-preview: the weekly "what's new" as it would read now.
func (s *Server) handleDigestPreview(w http.ResponseWriter, r *http.Request) {
	title, message, ok := s.weeklyDigest(time.Now().Add(-7 * 24 * time.Hour))
	writeJSON(w, http.StatusOK, map[string]any{"title": title, "message": message, "hasNews": ok})
}
