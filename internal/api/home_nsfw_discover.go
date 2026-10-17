package api

// Home fork: the NSFW page's Find tab browses StashDB with filters, like
// the Library: GET /api/nsfw/discover and tag autocomplete at
// GET /api/nsfw/tags. StashDB answers are kept for ten minutes, so going
// back and forth, paging and retyping are instant.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/nsfw"
)

type stashCacheEntry struct {
	at  time.Time
	ttl time.Duration
	val any
}

type stashCache struct {
	mu sync.Mutex
	m  map[string]stashCacheEntry
}

var discoverCache = &stashCache{m: map[string]stashCacheEntry{}}

// Searches are kept ten minutes; the plain views (no text typed), which
// change slowly and are slow for StashDB to sort, six hours.
const (
	stashCacheTTL  = 10 * time.Minute
	stashBrowseTTL = 6 * time.Hour
)

func (c *stashCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Since(e.at) > e.ttl {
		return nil, false
	}
	return e.val, true
}

func (c *stashCache) put(key string, v any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) > 400 {
		for k, e := range c.m {
			if time.Since(e.at) > e.ttl || len(c.m) > 300 {
				delete(c.m, k)
			}
		}
	}
	c.m[key] = stashCacheEntry{at: time.Now(), ttl: ttl, val: v}
}

// GET /api/nsfw/discover?kind=video|performer|studio&q=&tags=id,id&tagAll=1
// &since=2020&who=lesbian&gender=FEMALE&sort=trending&page=1
func (s *Server) handleVideoDiscover(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	kind := v.Get("kind")
	page, _ := strconv.Atoi(v.Get("page"))
	page = max(page, 1)
	q := strings.TrimSpace(v.Get("q"))
	if len(q) > 200 {
		q = q[:200]
	}
	key := kind + "|" + v.Encode()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	sb := s.stashBox()
	const per = 40
	ttl := stashCacheTTL
	if q == "" {
		ttl = stashBrowseTTL
	}

	switch kind {
	case nsfw.KindPerformer:
		type res struct {
			Items []nsfw.FoundPerformer `json:"performers"`
			Total int                   `json:"total"`
		}
		var out res
		if c, ok := discoverCache.get(key); ok {
			out = c.(res)
		} else {
			list, total, err := sb.QueryPerformers(ctx, nsfw.PerformersQuery{Name: q, Gender: v.Get("gender"), Sort: v.Get("sort"), Page: page, PerPage: per})
			if err != nil {
				s.writeStashError(w, err)
				return
			}
			out = res{Items: list, Total: total}
			discoverCache.put(key, out, ttl)
		}
		followed := map[string]bool{}
		if list, err := s.videoRepo().Performers(true); err == nil {
			for _, p := range list {
				followed[p.StashID] = true
			}
		}
		items := make([]nsfw.FoundPerformer, len(out.Items))
		for i, p := range out.Items {
			p.Followed = followed[p.StashID]
			s.allowVideoImage(p.ImageURL)
			items[i] = p
		}
		writeJSON(w, http.StatusOK, map[string]any{"performers": items, "total": out.Total, "page": page, "more": page*per < out.Total})

	case nsfw.KindStudio:
		type res struct {
			Items []nsfw.Studio `json:"studios"`
			Total int           `json:"total"`
		}
		var out res
		if c, ok := discoverCache.get(key); ok {
			out = c.(res)
		} else {
			list, total, err := sb.QueryStudios(ctx, q, v.Get("sort"), page, per)
			if err != nil {
				s.writeStashError(w, err)
				return
			}
			out = res{Items: list, Total: total}
			discoverCache.put(key, out, ttl)
		}
		followed := map[string]bool{}
		if list, err := s.videoRepo().Studios(true); err == nil {
			for _, st := range list {
				followed[st.StashID] = true
			}
		}
		items := make([]nsfw.Studio, len(out.Items))
		for i, st := range out.Items {
			st.Followed = followed[st.StashID]
			s.allowVideoImage(st.ImageURL)
			items[i] = st
		}
		writeJSON(w, http.StatusOK, map[string]any{"studios": items, "total": out.Total, "page": page, "more": page*per < out.Total})

	default:
		since, _ := strconv.Atoi(v.Get("since"))
		var tags []string
		for _, t := range strings.Split(v.Get("tags"), ",") {
			if t = strings.TrimSpace(t); t != "" && len(tags) < 10 {
				tags = append(tags, t)
			}
		}
		base := nsfw.NsfwQuery{Text: q, TagIDs: tags, TagAll: v.Get("tagAll") == "1", Since: since, Sort: v.Get("sort"), PerPage: per}
		who := v.Get("who")
		if who == "" || who == "all" {
			list, total, err := s.discoverVideoPage(ctx, base, page)
			if err != nil {
				s.writeStashError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"nsfw": s.markInLibrary(list), "total": total, "page": page, "more": page*per < total})
			return
		}
		// Who is a filter StashDB doesn't have, so it is applied here. One
		// page scans several StashDB pages at once, or a narrow filter would
		// leave pages nearly empty.
		const scan = 5
		pages := make([][]nsfw.Video, scan)
		totals := make([]int, scan)
		errs := make([]error, scan)
		var wg sync.WaitGroup
		for i := range scan {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pages[i], totals[i], errs[i] = s.discoverVideoPage(ctx, base, (page-1)*scan+i+1)
			}()
		}
		wg.Wait()
		if errs[0] != nil {
			s.writeStashError(w, errs[0])
			return
		}
		var kept []nsfw.Video
		for i := range scan {
			if errs[i] != nil {
				break
			}
			for _, sc := range pages[i] {
				for _, x := range nsfw.WhoOf(sc) {
					if x == who {
						kept = append(kept, sc)
						break
					}
				}
			}
		}
		total := totals[0]
		writeJSON(w, http.StatusOK, map[string]any{"nsfw": s.markInLibrary(kept), "total": total, "filtered": true, "page": page, "more": page*scan*per < total})
	}
}

// discoverVideoPage is one page of a StashDB video search, cached.
func (s *Server) discoverVideoPage(ctx context.Context, q nsfw.NsfwQuery, page int) ([]nsfw.Video, int, error) {
	type res struct {
		Items []nsfw.Video
		Total int
	}
	q.Page = page
	key := fmt.Sprintf("nsfw|%q|%v|%v|%d|%s|%d", q.Text, q.TagIDs, q.TagAll, q.Since, q.Sort, page)
	if c, ok := discoverCache.get(key); ok {
		r := c.(res)
		return r.Items, r.Total, nil
	}
	list, total, err := s.stashBox().QueryNsfw(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	ttl := stashCacheTTL
	if q.Text == "" {
		ttl = stashBrowseTTL
	}
	discoverCache.put(key, res{Items: list, Total: total}, ttl)
	return list, total, nil
}

// GET /api/nsfw/tags?q=: StashDB tags whose name contains q.
func (s *Server) handleVideoTags(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 || len(q) > 60 {
		writeJSON(w, http.StatusOK, []nsfw.Tag{})
		return
	}
	key := "tags|" + strings.ToLower(q)
	if c, ok := discoverCache.get(key); ok {
		writeJSON(w, http.StatusOK, c)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	tags, err := s.stashBox().QueryTags(ctx, q, 30)
	if err != nil {
		s.writeStashError(w, err)
		return
	}
	// The closest names first: exact, then starting with what was typed.
	lq := strings.ToLower(q)
	rank := func(t nsfw.Tag) int {
		n := strings.ToLower(t.Name)
		switch {
		case n == lq:
			return 0
		case strings.HasPrefix(n, lq):
			return 1
		}
		return 2
	}
	for i := 1; i < len(tags); i++ {
		for j := i; j > 0 && rank(tags[j]) < rank(tags[j-1]); j-- {
			tags[j], tags[j-1] = tags[j-1], tags[j]
		}
	}
	discoverCache.put(key, tags, stashBrowseTTL)
	writeJSON(w, http.StatusOK, tags)
}

// warmDiscoverJob fetches the Find tab's opening views ahead of time, so the
// first look at Videos, Performers or Studios is instant.
func (s *Server) warmDiscoverJob(ctx context.Context) {
	if !s.nsfwEnabled() || !s.stashKeySet() {
		return
	}
	for _, q := range []string{
		"kind=video&sort=trending&page=1",
		"kind=video&sort=trending&page=2",
		"kind=video&sort=newest&page=1",
		"kind=performer&sort=nsfw&page=1",
		"kind=performer&sort=nsfw&page=2",
		"kind=performer&sort=nsfw&gender=FEMALE&page=1",
		"kind=studio&sort=name&page=1",
	} {
		if ctx.Err() != nil {
			return
		}
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/nsfw/discover?"+q, nil)
		s.handleVideoDiscover(httptest.NewRecorder(), r)
	}
}
