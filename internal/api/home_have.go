package api

// Home fork: what the household already has, for Discover's "Hide what I
// have", sorted three ways so each person can choose what to hide:
//   - disk: a file on disk (Mediarium's downloads, and the media server's
//     libraries outside the streamed folders)
//   - streamed: played from Usenet without a file kept (Mediarium's
//     streamed titles, and the media server's streamed folders: nzbdav's
//     *-Stream libraries, E:\Streamed, .strm files)
//   - wanted: in Mediarium's library, not downloaded yet
// A title can be in more than one. Kept for ten minutes.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

const haveTTL = 10 * time.Minute

type haveSets struct {
	Disk     []int `json:"disk"`
	Streamed []int `json:"streamed"`
	Wanted   []int `json:"wanted"`
}

type havePayload struct {
	Movies haveSets `json:"movies"`
	Shows  haveSets `json:"shows"`
}

var (
	haveMu  sync.Mutex
	haveAt  time.Time
	haveNow *havePayload
)

// streamedPath says whether a media server path is a streamed copy rather
// than a file on disk.
func streamedPath(p string) bool {
	p = strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
	if strings.HasSuffix(p, ".strm") {
		return true
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasSuffix(part, "-stream") || part == "streamed" {
			return true
		}
	}
	return false
}

type idSet map[int]bool

func (s idSet) list() []int {
	out := make([]int, 0, len(s))
	for id := range s {
		out = append(out, id)
	}
	return out
}

func (s *Server) have(ctx context.Context) *havePayload {
	haveMu.Lock()
	defer haveMu.Unlock()
	if haveNow != nil && time.Since(haveAt) < haveTTL {
		return haveNow
	}
	md, ms, mw := idSet{}, idSet{}, idSet{}
	sd, ss, sw := idSet{}, idSet{}, idSet{}

	// The media server: each title's path says disk or streamed.
	if srv, ok := s.jellyfinServer(); ok {
		people, _ := s.jfPeople(ctx)
		viewer := ""
		for _, p := range people {
			if p.IsAdmin && viewer == "" {
				viewer = p.ID
			}
		}
		if viewer == "" && len(people) > 0 {
			viewer = people[0].ID
		}
		if viewer != "" {
			var res struct {
				Items []struct {
					jfItem
					Path string `json:"Path"`
				} `json:"Items"`
			}
			p := "/Users/" + url.PathEscape(viewer) + "/Items?Recursive=true&IncludeItemTypes=Movie,Series&EnableImages=false&Fields=ProviderIds,Path"
			if err := jfJSON(ctx, srv, http.MethodGet, p, nil, &res); err == nil {
				for _, it := range res.Items {
					id := it.tmdbID()
					if id <= 0 {
						continue
					}
					streamed := streamedPath(it.Path)
					switch {
					case it.Type == "Movie" && streamed:
						ms[id] = true
					case it.Type == "Movie":
						md[id] = true
					case it.Type == "Series" && streamed:
						ss[id] = true
					case it.Type == "Series":
						sd[id] = true
					}
				}
			}
		}
	}

	// Mediarium's library: downloaded is on disk, the rest is wanted.
	if list, err := s.MovieRepo.List(); err == nil {
		for _, m := range list {
			if m.Status == library.StatusDownloaded {
				md[m.TMDBID] = true
			} else {
				mw[m.TMDBID] = true
			}
		}
	}
	if rows, err := s.db.QueryContext(ctx, `SELECT s.tmdb_id, COUNT(e.id) FROM series s
		LEFT JOIN episodes e ON e.series_id = s.id AND e.status = 'downloaded' GROUP BY s.id`); err == nil {
		for rows.Next() {
			var id, files int
			if rows.Scan(&id, &files) == nil {
				if files > 0 {
					sd[id] = true
				} else {
					sw[id] = true
				}
			}
		}
		rows.Close()
	}
	// Mediarium's streamed titles.
	if rows, err := s.db.QueryContext(ctx, `SELECT kind, tmdb_id FROM streamed_titles`); err == nil {
		for rows.Next() {
			var kind string
			var id int
			if rows.Scan(&kind, &id) == nil {
				if kind == "movie" {
					ms[id] = true
				} else {
					ss[id] = true
				}
			}
		}
		rows.Close()
	}
	// Wanted is only what isn't had some other way.
	for id := range mw {
		if md[id] || ms[id] {
			delete(mw, id)
		}
	}
	for id := range sw {
		if sd[id] || ss[id] {
			delete(sw, id)
		}
	}
	haveNow = &havePayload{
		Movies: haveSets{Disk: md.list(), Streamed: ms.list(), Wanted: mw.list()},
		Shows:  haveSets{Disk: sd.list(), Streamed: ss.list(), Wanted: sw.list()},
	}
	haveAt = time.Now()
	return haveNow
}

// GET /api/home/have: the TMDB ids the household has, on disk, streamed or wanted.
func (s *Server) handleHave(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.have(r.Context()))
}
