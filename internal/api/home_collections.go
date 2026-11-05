package api

// Usherr: collections pushed to Jellyfin and Plex (what Kometa did). A
// collection has a name and a source: an Usherr import list (Trakt, MDBList,
// TMDB lists, a CSV...), a TMDB collection (a film series), TMDB's trending
// titles, a rule over the library ("Horror from the 80s", "rated 8 and up",
// "added in the last 30 days", "the 20 best rated"), or titles picked by
// hand. Every 12 hours (and on "Sync now") Usherr finds the titles in each
// server's library by their TMDB, IMDb or TheTVDB id and makes the
// collection there, or puts in what's new and (if wanted) takes out what no
// longer belongs. A collection of the same name already on the server (one
// Kometa made, say) is taken over rather than made twice.
//
// Jellyfin gets one collection (films and shows together); Plex keeps
// collections per library, so each Plex library with matching titles gets
// its own.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/mediaservers"
)

const (
	keyCollHours = "home.collections.hours" // hours between syncs (12)

	collMaxPicks   = 500
	collMaxAdds    = 10 // missing titles added to Usherr in one sync at most
	collMaxMissing = 20 // missing titles named on the page
)

type collPick struct {
	Kind   string `json:"kind"`
	TMDBID int    `json:"tmdbId"`
	IMDBID string `json:"imdbId,omitempty"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
}

// collConfig is what the source needs; which fields count depends on it.
type collConfig struct {
	ListID         int64      `json:"listId,omitempty"`         // list
	TMDBCollection int        `json:"tmdbCollection,omitempty"` // tmdb
	TMDBName       string     `json:"tmdbName,omitempty"`       // tmdb: as typed, to find the number
	Count          int        `json:"count,omitempty"`          // trending: how many; rule: the best rated N (0 all)
	Genres         []string   `json:"genres,omitempty"`         // rule: any of these
	YearFrom       int        `json:"yearFrom,omitempty"`       // rule
	YearTo         int        `json:"yearTo,omitempty"`         // rule
	MinRating      float64    `json:"minRating,omitempty"`      // rule: the server's rating, out of 10
	AddedDays      int        `json:"addedDays,omitempty"`      // rule: added in the last N days
	Picked         []collPick `json:"picked,omitempty"`         // picked
}

// srvCollection is one collection as stored and as the page sees it.
type srvCollection struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Source        string     `json:"source"` // list, tmdb, trending, rule or picked
	Config        collConfig `json:"config"`
	Media         string     `json:"media"`   // movie, tv or both
	Servers       []string   `json:"servers"` // jellyfin, plex
	SortTitle     string     `json:"sortTitle"`
	Poster        string     `json:"poster"` // "", upload or made
	HasUpload     bool       `json:"hasUpload"`
	Visibility    []string   `json:"visibility"` // Plex: library, home, shared
	OnlyLibrary   bool       `json:"onlyLibrary"`
	RemoveMissing bool       `json:"removeMissing"`
	Enabled       bool       `json:"enabled"`
	CreatedMs     int64      `json:"createdMs"`
	LastSyncMs    int64      `json:"lastSyncMs,omitempty"`
	LastStatus    string     `json:"lastStatus,omitempty"`
	LastMessage   string     `json:"lastMessage,omitempty"`
	LastFound     int        `json:"lastFound"`
	LastMatched   int        `json:"lastMatched"`
	LastMissing   []string   `json:"lastMissing"`
	Targets       []string   `json:"targets"` // where it is now: "Jellyfin", "Plex: Movies"
	Running       bool       `json:"running"`
}

var (
	collSources  = map[string]bool{"list": true, "tmdb": true, "trending": true, "rule": true, "picked": true}
	collVisNames = map[string]bool{"library": true, "home": true, "shared": true}
)

// ---- stored ----

const collCols = `id, name, source, config, media, servers, sort_title, poster, visibility, only_library, remove_missing, enabled, created_ms, last_sync_ms, last_status, last_message, last_found, last_matched, last_missing`

func splitList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) collScan(sc interface{ Scan(...any) error }) (srvCollection, error) {
	var c srvCollection
	var cfg, servers, vis, missing string
	var only, remove, on int
	if err := sc.Scan(&c.ID, &c.Name, &c.Source, &cfg, &c.Media, &servers, &c.SortTitle, &c.Poster, &vis, &only, &remove, &on, &c.CreatedMs, &c.LastSyncMs, &c.LastStatus, &c.LastMessage, &c.LastFound, &c.LastMatched, &missing); err != nil {
		return c, err
	}
	_ = json.Unmarshal([]byte(cfg), &c.Config)
	c.Servers, c.Visibility = splitList(servers), splitList(vis)
	c.OnlyLibrary, c.RemoveMissing, c.Enabled = only == 1, remove == 1, on == 1
	c.LastMissing = []string{}
	_ = json.Unmarshal([]byte(missing), &c.LastMissing)
	_, err := os.Stat(s.collUploadFile(c.ID))
	c.HasUpload = err == nil
	c.Running = collRunning(c.ID)
	return c, nil
}

func (s *Server) collGet(ctx context.Context, id int64) (srvCollection, error) {
	c, err := s.collScan(s.db.QueryRowContext(ctx, `SELECT `+collCols+` FROM server_collections WHERE id = ?`, id))
	if err != nil {
		return c, err
	}
	c.Targets = s.collTargetNames(ctx, id)
	return c, nil
}

func (s *Server) collAll(ctx context.Context) ([]srvCollection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+collCols+` FROM server_collections ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	out := []srvCollection{}
	for rows.Next() {
		if c, err := s.collScan(rows); err == nil {
			out = append(out, c)
		}
	}
	rows.Close()
	for i := range out {
		out[i].Targets = s.collTargetNames(ctx, out[i].ID)
	}
	return out, rows.Err()
}

type collLink struct {
	Target, Remote, SortSent, PosterSent, VisSent string
}

func (s *Server) collLinks(ctx context.Context, id int64) map[string]*collLink {
	out := map[string]*collLink{}
	rows, err := s.db.QueryContext(ctx, `SELECT target, remote_id, sort_sent, poster_sent, vis_sent FROM server_collection_links WHERE collection_id = ?`, id)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var l collLink
		if rows.Scan(&l.Target, &l.Remote, &l.SortSent, &l.PosterSent, &l.VisSent) == nil {
			out[l.Target] = &l
		}
	}
	return out
}

func (s *Server) collSaveLink(ctx context.Context, id int64, l *collLink) {
	if l.Remote == "" {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM server_collection_links WHERE collection_id = ? AND target = ?`, id, l.Target)
		return
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO server_collection_links (collection_id, target, remote_id, sort_sent, poster_sent, vis_sent) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (collection_id, target) DO UPDATE SET remote_id = excluded.remote_id, sort_sent = excluded.sort_sent, poster_sent = excluded.poster_sent, vis_sent = excluded.vis_sent`,
		id, l.Target, l.Remote, l.SortSent, l.PosterSent, l.VisSent)
}

// collTargetNames are where a collection is, as people read it.
func (s *Server) collTargetNames(ctx context.Context, id int64) []string {
	out := []string{}
	links := s.collLinks(ctx, id)
	keys := make([]string, 0, len(links))
	for k := range links {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, s.collTargetName(ctx, k))
	}
	return out
}

func (s *Server) collTargetName(ctx context.Context, target string) string {
	p := strings.Split(target, ":")
	if p[0] != "plex" || len(p) != 3 {
		return "Jellyfin"
	}
	if srvID, err := strconv.ParseInt(p[1], 10, 64); err == nil {
		if ps, ok := s.artPlexServer(srvID); ok {
			if libs, err := s.mediaClient.PlexLibraries(ctx, ps); err == nil {
				for _, l := range libs {
					if l.ID == p[2] {
						return "Plex: " + l.Title
					}
				}
			}
		}
	}
	return "Plex"
}

func (s *Server) collUploadFile(id int64) string {
	return filepath.Join(s.artDir(), "collections", strconv.FormatInt(id, 10)+".jpg")
}

// ---- checking what's sent ----

type collInput struct {
	Name          string     `json:"name"`
	Source        string     `json:"source"`
	Config        collConfig `json:"config"`
	Media         string     `json:"media"`
	Servers       []string   `json:"servers"`
	SortTitle     string     `json:"sortTitle"`
	Poster        string     `json:"poster"`
	Visibility    []string   `json:"visibility"`
	OnlyLibrary   *bool      `json:"onlyLibrary"`
	RemoveMissing *bool      `json:"removeMissing"`
	Enabled       *bool      `json:"enabled"`
}

// collCheck turns what was sent into a collection, or says what's wrong.
func (s *Server) collCheck(ctx context.Context, in collInput) (srvCollection, string) {
	c := srvCollection{Name: strings.TrimSpace(in.Name), Source: in.Source, Media: in.Media, SortTitle: trimRunes(strings.TrimSpace(in.SortTitle), 120),
		Poster: in.Poster, OnlyLibrary: true, RemoveMissing: true, Enabled: true}
	if c.Name == "" {
		return c, "Give the collection a name."
	}
	c.Name = trimRunes(c.Name, 120)
	if !collSources[c.Source] {
		return c, "Choose where the collection's titles come from."
	}
	if c.Media != "tv" && c.Media != "both" {
		c.Media = "movie"
	}
	for _, sv := range in.Servers {
		if (sv == "jellyfin" || sv == "plex") && !containsStr(c.Servers, sv) {
			c.Servers = append(c.Servers, sv)
		}
	}
	if len(c.Servers) == 0 {
		return c, "Choose Jellyfin, Plex or both."
	}
	if c.Poster != "upload" && c.Poster != "made" {
		c.Poster = ""
	}
	c.Visibility = []string{}
	for _, v := range in.Visibility {
		if collVisNames[v] && !containsStr(c.Visibility, v) {
			c.Visibility = append(c.Visibility, v)
		}
	}
	sort.Strings(c.Visibility)
	if in.OnlyLibrary != nil {
		c.OnlyLibrary = *in.OnlyLibrary
	}
	if in.RemoveMissing != nil {
		c.RemoveMissing = *in.RemoveMissing
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	}
	cfg := in.Config
	var keep collConfig
	switch c.Source {
	case "list":
		if cfg.ListID <= 0 {
			return c, "Choose one of your import lists."
		}
		if _, err := s.getImportList(cfg.ListID); err != nil {
			return c, "That import list isn't there any more."
		}
		keep.ListID = cfg.ListID
	case "tmdb":
		c.Media = "movie"
		keep.TMDBName = trimRunes(strings.TrimSpace(cfg.TMDBName), 120)
		keep.TMDBCollection = cfg.TMDBCollection
		if n, err := strconv.Atoi(keep.TMDBName); err == nil && n > 0 {
			keep.TMDBCollection = n
		}
		if keep.TMDBCollection <= 0 && keep.TMDBName != "" {
			if !s.TMDB().HasAPIKey() {
				return c, "A TMDB key is needed to find a TMDB collection by name (Settings > Movie and show info)."
			}
			id, err := s.TMDB().CollectionID(ctx, keep.TMDBName)
			if err != nil {
				return c, "TMDB couldn't be asked: " + err.Error()
			}
			if id == 0 {
				return c, "TMDB has no collection by that name. Try its number from themoviedb.org."
			}
			keep.TMDBCollection = id
		}
		if keep.TMDBCollection <= 0 {
			return c, "Type the TMDB collection's name or number."
		}
	case "trending":
		keep.Count = cfg.Count
		if keep.Count < 1 || keep.Count > 20 {
			keep.Count = 20
		}
	case "rule":
		for _, g := range cfg.Genres {
			if g = strings.TrimSpace(g); g != "" && len(keep.Genres) < 20 {
				keep.Genres = append(keep.Genres, trimRunes(g, 60))
			}
		}
		keep.YearFrom, keep.YearTo = clampYear(cfg.YearFrom), clampYear(cfg.YearTo)
		if keep.YearFrom > 0 && keep.YearTo > 0 && keep.YearFrom > keep.YearTo {
			keep.YearFrom, keep.YearTo = keep.YearTo, keep.YearFrom
		}
		if cfg.MinRating > 0 && cfg.MinRating <= 10 {
			keep.MinRating = cfg.MinRating
		}
		if cfg.AddedDays > 0 && cfg.AddedDays <= 3650 {
			keep.AddedDays = cfg.AddedDays
		}
		if cfg.Count > 0 && cfg.Count <= 500 {
			keep.Count = cfg.Count
		}
		if len(keep.Genres) == 0 && keep.YearFrom == 0 && keep.YearTo == 0 && keep.MinRating == 0 && keep.AddedDays == 0 && keep.Count == 0 {
			return c, "Set at least one rule: a genre, years, a rating, how recently added, or the best rated few."
		}
	case "picked":
		seen := map[string]bool{}
		for _, p := range cfg.Picked {
			if p.Kind != "tv" {
				p.Kind = "movie"
			}
			k := p.Kind + ":" + strconv.Itoa(p.TMDBID) + ":" + p.IMDBID
			if (p.TMDBID <= 0 && p.IMDBID == "") || seen[k] || len(keep.Picked) >= collMaxPicks {
				continue
			}
			seen[k] = true
			p.Title = trimRunes(p.Title, 200)
			keep.Picked = append(keep.Picked, p)
		}
		if len(keep.Picked) == 0 {
			return c, "Pick at least one title."
		}
		c.Media = "both"
	}
	c.Config = keep
	return c, ""
}

func clampYear(y int) int {
	if y < 1880 || y > 2200 {
		return 0
	}
	return y
}

// ---- the titles a collection wants ----

type collWant struct {
	Kind   string // movie, tv or "" (the list didn't say)
	TMDBID int
	IMDBID string
	TVDBID int
	Title  string
	Year   int
}

func (w collWant) name() string {
	if w.Year > 0 {
		return fmt.Sprintf("%s (%d)", w.Title, w.Year)
	}
	if w.Title == "" {
		if w.TMDBID > 0 {
			return "TMDB " + strconv.Itoa(w.TMDBID)
		}
		return w.IMDBID
	}
	return w.Title
}

// collWanted are the titles the source names (nil for a rule, which picks
// from the library).
func (s *Server) collWanted(ctx context.Context, c srvCollection) ([]collWant, error) {
	kinds := wantedKinds(c.Media)
	var out []collWant
	switch c.Source {
	case "list":
		l, err := s.getImportList(c.Config.ListID)
		if err != nil {
			return nil, errors.New("the import list isn't there any more")
		}
		items, err := s.fetchListItems(ctx, l)
		if err != nil {
			return nil, errors.New(listErrorText(err))
		}
		for _, it := range dedupeListItems(items) {
			if it.Kind != "" && !kinds[it.Kind] {
				continue
			}
			out = append(out, collWant{Kind: it.Kind, TMDBID: it.TMDBID, IMDBID: it.IMDBID, TVDBID: it.TVDBID, Title: it.Title, Year: it.Year})
		}
	case "tmdb":
		if !s.TMDB().HasAPIKey() {
			return nil, errors.New("a TMDB key is needed (Settings > Movie and show info)")
		}
		col, err := s.TMDB().GetCollection(ctx, c.Config.TMDBCollection)
		if err != nil {
			return nil, fmt.Errorf("TMDB couldn't be asked: %w", err)
		}
		for _, p := range col.Parts {
			out = append(out, collWant{Kind: "movie", TMDBID: p.TMDBID, Title: p.DisplayTitle(), Year: p.Year()})
		}
	case "trending":
		if !s.TMDB().HasAPIKey() {
			return nil, errors.New("a TMDB key is needed (Settings > Movie and show info)")
		}
		n := c.Config.Count
		if kinds["movie"] {
			list, err := s.TMDB().TrendingMovies(ctx)
			if err != nil {
				return nil, fmt.Errorf("TMDB couldn't be asked: %w", err)
			}
			for i, m := range list {
				if i >= n {
					break
				}
				out = append(out, collWant{Kind: "movie", TMDBID: m.TMDBID, Title: m.Title, Year: m.Year()})
			}
		}
		if kinds["tv"] {
			list, err := s.TMDB().TrendingTV(ctx)
			if err != nil {
				return nil, fmt.Errorf("TMDB couldn't be asked: %w", err)
			}
			for i, sh := range list {
				if i >= n {
					break
				}
				out = append(out, collWant{Kind: "tv", TMDBID: sh.TMDBID, Title: sh.Name, Year: sh.Year()})
			}
		}
	case "picked":
		for _, p := range c.Config.Picked {
			out = append(out, collWant{Kind: p.Kind, TMDBID: p.TMDBID, IMDBID: p.IMDBID, Title: p.Title, Year: p.Year})
		}
	}
	return out, nil
}

// collRule picks a server's titles by the rule.
func collRule(cfg collConfig, items []libItem, now time.Time) []libItem {
	var out []libItem
	for _, it := range items {
		if len(cfg.Genres) > 0 {
			ok := false
			for _, g := range it.Genres {
				for _, w := range cfg.Genres {
					ok = ok || strings.EqualFold(strings.TrimSpace(g), w)
				}
			}
			if !ok {
				continue
			}
		}
		if (cfg.YearFrom > 0 && it.Year < cfg.YearFrom) || (cfg.YearTo > 0 && (it.Year == 0 || it.Year > cfg.YearTo)) {
			continue
		}
		if cfg.MinRating > 0 && it.Rating < cfg.MinRating {
			continue
		}
		if cfg.AddedDays > 0 && (it.Added.IsZero() || now.Sub(it.Added) > time.Duration(cfg.AddedDays)*24*time.Hour) {
			continue
		}
		out = append(out, it)
	}
	if cfg.Count > 0 {
		sort.SliceStable(out, func(i, j int) bool { return out[i].Rating > out[j].Rating })
		if len(out) > cfg.Count {
			out = out[:cfg.Count]
		}
	}
	return out
}

// collMatch finds the wanted titles among one server's (or one Plex
// library's) titles. A title in two libraries of the same Jellyfin (say a
// kept and a streamed copy) goes in once: the copy outside a "stream"
// library first. found says which wanted titles were found.
func collMatch(wants []collWant, items []libItem, found map[int]bool) []libItem {
	byKey := map[string][]libItem{}
	for _, it := range items {
		if it.TMDBID > 0 {
			byKey[it.Kind+":tmdb:"+strconv.Itoa(it.TMDBID)] = append(byKey[it.Kind+":tmdb:"+strconv.Itoa(it.TMDBID)], it)
		}
		if it.IMDBID != "" {
			byKey["imdb:"+strings.ToLower(it.IMDBID)] = append(byKey["imdb:"+strings.ToLower(it.IMDBID)], it)
		}
		if it.TVDBID > 0 && it.Kind == "tv" {
			byKey["tv:tvdb:"+strconv.Itoa(it.TVDBID)] = append(byKey["tv:tvdb:"+strconv.Itoa(it.TVDBID)], it)
		}
	}
	var out []libItem
	taken := map[string]bool{}
	for i, w := range wants {
		var hits []libItem
		if w.TMDBID > 0 {
			for _, k := range []string{"movie", "tv"} {
				if w.Kind == "" || w.Kind == k {
					hits = append(hits, byKey[k+":tmdb:"+strconv.Itoa(w.TMDBID)]...)
				}
			}
		}
		if len(hits) == 0 && w.IMDBID != "" {
			hits = byKey["imdb:"+strings.ToLower(w.IMDBID)]
		}
		if len(hits) == 0 && w.TVDBID > 0 && w.Kind != "movie" {
			hits = byKey["tv:tvdb:"+strconv.Itoa(w.TVDBID)]
		}
		if len(hits) == 0 {
			continue
		}
		found[i] = true
		// One copy per title and library holder (Jellyfin as a whole; each
		// Plex library).
		best := map[string]libItem{}
		var order []string
		for _, h := range hits {
			holder := h.Server + ":" + strconv.FormatInt(h.ServerID, 10) + ":" + h.Section
			cur, ok := best[holder]
			if !ok {
				order = append(order, holder)
				best[holder] = h
				continue
			}
			if strings.Contains(strings.ToLower(cur.Library), "stream") && !strings.Contains(strings.ToLower(h.Library), "stream") {
				best[holder] = h
			}
		}
		for _, holder := range order {
			h := best[holder]
			if !taken[h.Copy] {
				taken[h.Copy] = true
				out = append(out, h)
			}
		}
	}
	return out
}

// ---- the sync ----

var collState struct {
	mu      sync.Mutex
	running map[int64]bool
	one     sync.Mutex // one sync at a time
}

func collRunning(id int64) bool {
	collState.mu.Lock()
	defer collState.mu.Unlock()
	return collState.running[id]
}

func collSetRunning(id int64, on bool) {
	collState.mu.Lock()
	defer collState.mu.Unlock()
	if collState.running == nil {
		collState.running = map[int64]bool{}
	}
	if on {
		collState.running[id] = true
	} else {
		delete(collState.running, id)
	}
}

func (s *Server) collHours() int {
	v, _ := s.Settings.Get(keyCollHours)
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 || n > 24*7 {
		return 12
	}
	return n
}

// collectionsPushJob syncs each collection whose time has come.
func (s *Server) collectionsPushJob(ctx context.Context) {
	list, err := s.collAll(ctx)
	if err != nil || len(list) == 0 {
		return
	}
	every := time.Duration(s.collHours()) * time.Hour
	fresh := true
	for _, c := range list {
		if ctx.Err() != nil {
			return
		}
		if !c.Enabled || time.Since(time.UnixMilli(c.LastSyncMs)) < every {
			continue
		}
		s.collSync(ctx, c.ID, fresh)
		fresh = false
	}
}

// collSyncSoon syncs collections now, in the background.
func (s *Server) collSyncSoon(ids []int64) {
	for _, id := range ids {
		collSetRunning(id, true)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		for i, id := range ids {
			s.collSync(ctx, id, i == 0)
		}
	}()
}

// collResult is how one sync went.
type collResult struct {
	found, matched int
	missing        []string
	notes          []string
	failed         bool
}

// collSync brings one collection up to date on every server it's for.
func (s *Server) collSync(ctx context.Context, id int64, fresh bool) {
	collState.one.Lock()
	defer collState.one.Unlock()
	collSetRunning(id, true)
	defer collSetRunning(id, false)
	c, err := s.collGet(ctx, id)
	if err != nil {
		return
	}
	res := s.collPush(ctx, c, fresh)
	status, msg := "ok", strings.Join(res.notes, " ")
	if res.failed {
		status = "error"
	}
	missing, _ := json.Marshal(append([]string{}, res.missing...))
	_, _ = s.db.ExecContext(ctx, `UPDATE server_collections SET last_sync_ms = ?, last_status = ?, last_message = ?, last_found = ?, last_matched = ?, last_missing = ? WHERE id = ?`,
		time.Now().UnixMilli(), status, trimRunes(msg, 1000), res.found, res.matched, string(missing), id)
}

func (s *Server) collPush(ctx context.Context, c srvCollection, fresh bool) collResult {
	var res collResult
	fail := func(msg string) collResult {
		res.failed = true
		res.notes = append(res.notes, msg)
		return res
	}
	wants, err := s.collWanted(ctx, c)
	if err != nil {
		return fail("The titles couldn't be read: " + err.Error() + ".")
	}
	items, err := s.libItems(ctx, fresh)
	if err != nil {
		return fail("The media servers couldn't be read: " + err.Error() + ".")
	}
	kinds := wantedKinds(c.Media)
	var mine []libItem
	for _, it := range items {
		if kinds[it.Kind] && containsStr(c.Servers, it.Server) {
			mine = append(mine, it)
		}
	}
	// The titles for each place the collection goes: Jellyfin, or one Plex
	// library.
	groups := map[string][]libItem{}
	found := map[int]bool{}
	var matched []libItem
	if c.Source == "rule" {
		byHolder := map[string][]libItem{}
		for _, it := range mine {
			h := collTarget(it)
			byHolder[h] = append(byHolder[h], it)
		}
		// Jellyfin's libraries are one place: a rule's "best 20" is of the
		// server, a title once.
		for h, list := range byHolder {
			picked := collRule(c.Config, list, time.Now())
			groups[h] = collOnce(picked)
			matched = append(matched, groups[h]...)
		}
		res.found = len(collDistinct(matched))
	} else {
		matched = collMatch(wants, mine, found)
		for _, it := range matched {
			groups[collTarget(it)] = append(groups[collTarget(it)], it)
		}
		res.found = len(wants)
		for i, w := range wants {
			if !found[i] && len(res.missing) < collMaxMissing {
				res.missing = append(res.missing, w.name())
			}
		}
		if !c.OnlyLibrary {
			if n := s.collAddMissing(ctx, c, wants, found); n > 0 {
				res.notes = append(res.notes, fmt.Sprintf("%d missing added to Usherr.", n))
			}
		}
	}
	res.matched = len(collDistinct(matched))

	links := s.collLinks(ctx, c.ID)
	poster, posterKey, perr := s.collPosterFor(ctx, c, matched)
	if perr != nil {
		res.notes = append(res.notes, "The poster couldn't be made: "+perr.Error()+".")
	}
	// Places it was before and has no titles for now still get emptied (when
	// titles that no longer match are taken out).
	for t := range links {
		if _, ok := groups[t]; !ok {
			groups[t] = nil
		}
	}
	targets := make([]string, 0, len(groups))
	for t := range groups {
		targets = append(targets, t)
	}
	sort.Strings(targets)
	for _, t := range targets {
		if ctx.Err() != nil {
			return fail("Stopped.")
		}
		l := links[t]
		if l == nil {
			l = &collLink{Target: t}
		}
		name := s.collTargetName(ctx, t)
		ids := make([]string, 0, len(groups[t]))
		for _, it := range groups[t] {
			ids = append(ids, it.ID)
		}
		n, err := s.collPushTo(ctx, c, l, ids, poster, posterKey)
		s.collSaveLink(ctx, c.ID, l)
		switch {
		case err != nil:
			res.failed = true
			res.notes = append(res.notes, name+": "+err.Error()+".")
		case l.Remote != "":
			res.notes = append(res.notes, fmt.Sprintf("%s: %d.", name, n))
		}
	}
	if len(targets) == 0 {
		res.notes = append(res.notes, "No title is in a library yet.")
	}
	return res
}

// collTarget is where a copy's collection is: "jellyfin:<server>" or
// "plex:<server>:<library>".
func collTarget(it libItem) string {
	if it.Server == "plex" {
		return fmt.Sprintf("plex:%d:%s", it.ServerID, it.Section)
	}
	return fmt.Sprintf("jellyfin:%d", it.ServerID)
}

// collOnce keeps one copy of each title (a rule on Jellyfin's several
// libraries).
func collOnce(list []libItem) []libItem {
	var out []libItem
	idx := map[string]int{}
	for _, it := range list {
		k := it.Kind + ":" + strconv.Itoa(it.TMDBID)
		if it.TMDBID == 0 {
			k = "copy:" + it.Copy
		}
		if i, ok := idx[k]; ok {
			if strings.Contains(strings.ToLower(out[i].Library), "stream") && !strings.Contains(strings.ToLower(it.Library), "stream") {
				out[i] = it
			}
			continue
		}
		idx[k] = len(out)
		out = append(out, it)
	}
	return out
}

func collDistinct(list []libItem) map[string]bool {
	out := map[string]bool{}
	for _, it := range list {
		k := it.Kind + ":" + strconv.Itoa(it.TMDBID)
		if it.TMDBID == 0 {
			k = it.Copy
		}
		out[k] = true
	}
	return out
}

// collPushTo makes the collection hold exactly ids (or at least ids, when
// titles are never taken out) in one place, and says how many it holds.
func (s *Server) collPushTo(ctx context.Context, c srvCollection, l *collLink, ids []string, poster []byte, posterKey string) (int, error) {
	p := strings.Split(l.Target, ":")
	if len(p) < 2 {
		return 0, errors.New("unknown place")
	}
	srvID, _ := strconv.ParseInt(p[1], 10, 64)
	var srv mediaservers.Server
	var ok bool
	plex := p[0] == "plex" && len(p) == 3
	if plex {
		srv, ok = s.artPlexServer(srvID)
	} else {
		srv, ok = s.jellyfinServer()
		ok = ok && srv.ID == srvID
	}
	if !ok {
		return 0, errors.New("the server isn't connected now")
	}
	cl := s.mediaClient
	section := ""
	if plex {
		section = p[2]
	}
	// What the collection holds now (and whether it's still there).
	var members []string
	var err error
	count := 0
	if l.Remote != "" {
		if plex {
			members, err = cl.PlexCollectionItems(ctx, srv, l.Remote)
		} else {
			members, err = cl.JellyfinCollectionItems(ctx, srv, l.Remote)
		}
		if mediaservers.IsNotFound(err) {
			*l = collLink{Target: l.Target} // deleted on the server: made again
		} else if err != nil {
			return 0, err
		}
	}
	if l.Remote == "" {
		// One of that name already there (made by hand, or by Kometa) is taken over.
		var existing []mediaservers.ServerCollection
		if plex {
			existing, err = cl.PlexCollections(ctx, srv, section)
		} else {
			existing, err = cl.JellyfinCollections(ctx, srv)
		}
		if err != nil {
			return 0, err
		}
		for _, e := range existing {
			if strings.EqualFold(strings.TrimSpace(e.Title), c.Name) {
				l.Remote = e.ID
				break
			}
		}
		if l.Remote != "" {
			if plex {
				members, err = cl.PlexCollectionItems(ctx, srv, l.Remote)
			} else {
				members, err = cl.JellyfinCollectionItems(ctx, srv, l.Remote)
			}
			if err != nil {
				return 0, err
			}
		}
	}
	if l.Remote == "" {
		if len(ids) == 0 {
			return 0, nil
		}
		if plex {
			l.Remote, err = cl.PlexCreateCollection(ctx, srv, section, s.collPlexKind(ctx, srv, section), c.Name, ids)
		} else {
			l.Remote, err = cl.JellyfinCreateCollection(ctx, srv, c.Name, ids)
		}
		if err != nil {
			return 0, err
		}
		count = len(ids)
		l.SortSent = c.Name // made with its name; the sort title still goes below
	} else {
		have := map[string]bool{}
		for _, m := range members {
			have[m] = true
		}
		want := map[string]bool{}
		var add, remove []string
		for _, id := range ids {
			want[id] = true
			if !have[id] {
				add = append(add, id)
			}
		}
		if c.RemoveMissing {
			for _, m := range members {
				if !want[m] {
					remove = append(remove, m)
				}
			}
		}
		if len(add) > 0 {
			if plex {
				err = cl.PlexCollectionAdd(ctx, srv, l.Remote, add)
			} else {
				err = cl.JellyfinCollectionAdd(ctx, srv, l.Remote, add)
			}
			if err != nil {
				return 0, err
			}
		}
		if len(remove) > 0 {
			if plex {
				for _, r := range remove {
					if err = cl.PlexCollectionRemove(ctx, srv, l.Remote, r); err != nil {
						break
					}
				}
			} else {
				err = cl.JellyfinCollectionRemove(ctx, srv, l.Remote, remove)
			}
			if err != nil {
				return 0, err
			}
		}
		count = len(ids)
		if !c.RemoveMissing {
			for m := range have {
				if !want[m] {
					count++
				}
			}
		}
	}
	// Its name and sort title, its poster, and (Plex) where it's shown:
	// each sent when it changed.
	names := c.Name + "\x00" + c.SortTitle
	if l.SortSent != names && !(l.SortSent == c.Name && c.SortTitle == "") {
		if plex {
			err = cl.PlexCollectionNames(ctx, srv, section, l.Remote, c.Name, c.SortTitle)
		} else {
			err = cl.JellyfinCollectionNames(ctx, srv, l.Remote, c.Name, c.SortTitle)
		}
		if err != nil {
			slog.Info("collections: the name couldn't be set", "collection", c.Name, "err", err)
		} else {
			l.SortSent = names
		}
	}
	if posterKey != "" && posterKey != l.PosterSent {
		mime := http.DetectContentType(poster)
		if plex {
			err = cl.PlexSetPoster(ctx, srv, l.Remote, poster, mime)
		} else {
			err = jfSetImage(ctx, srv, l.Remote, "poster", poster, mime)
		}
		if err != nil {
			slog.Info("collections: the poster couldn't be sent", "collection", c.Name, "err", err)
		} else {
			l.PosterSent = posterKey
		}
	}
	if plex {
		vis := strings.Join(c.Visibility, ",")
		if vis != l.VisSent {
			if err := cl.PlexCollectionVisibility(ctx, srv, section, l.Remote, containsStr(c.Visibility, "library"), containsStr(c.Visibility, "home"), containsStr(c.Visibility, "shared")); err != nil {
				slog.Info("collections: Plex's home and library rows couldn't be set", "collection", c.Name, "err", err)
			} else {
				l.VisSent = vis
			}
		}
	}
	return count, nil
}

// collPlexKind is a Plex library's kind (movie or show).
func (s *Server) collPlexKind(ctx context.Context, srv mediaservers.Server, section string) string {
	libs, err := s.mediaClient.PlexLibraries(ctx, srv)
	if err != nil {
		return "movie"
	}
	for _, l := range libs {
		if l.ID == section {
			return l.Type
		}
	}
	return "movie"
}

// collAddMissing adds the source's titles the library lacks to Usherr, as
// an import list would (wanted, searched for by the usual hunt). A few per
// sync.
func (s *Server) collAddMissing(ctx context.Context, c srvCollection, wants []collWant, found map[int]bool) int {
	l := importList{Name: c.Name, Monitored: true}
	user := &auth.User{Username: "collection", FirstName: "Collection", LastName: c.Name, IsAdmin: true}
	added := 0
	for i, w := range wants {
		if added >= collMaxAdds || ctx.Err() != nil {
			break
		}
		if found[i] || w.TMDBID <= 0 || (w.Kind != "movie" && w.Kind != "tv") {
			continue
		}
		if s.titleInLibrary(w.Kind, w.TMDBID) || s.titleExcluded(w.Kind, w.TMDBID) {
			continue
		}
		if out, _, _ := s.addListTitle(ctx, l, user, w.Kind, w.TMDBID); out == "added" {
			added++
		}
	}
	return added
}

// ---- the poster ----

// collPosterFor is the poster the collection sends and its fingerprint
// (nil and "" for none: the server makes its own).
func (s *Server) collPosterFor(ctx context.Context, c srvCollection, matched []libItem) ([]byte, string, error) {
	var data []byte
	switch c.Poster {
	case "upload":
		b, err := os.ReadFile(s.collUploadFile(c.ID))
		if err != nil {
			return nil, "", errors.New("no picture was sent for it")
		}
		data = b
	case "made":
		b, err := s.collMadePoster(ctx, c, matched)
		if err != nil {
			return nil, "", err
		}
		data = b
	default:
		return nil, "", nil
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:8]), nil
}

// collMadePoster is the collection's poster in the household's Re-Posterr
// poster style: its name over a picture of the collection (TMDB's, for a
// TMDB collection) or of its first title. Kept, and made again only when
// the name, the style or the picture's title change.
func (s *Server) collMadePoster(ctx context.Context, c srvCollection, matched []libItem) ([]byte, error) {
	styles, _ := s.artStylesNow()
	st := styles.Poster
	kind, tmdb, tvdb := "", 0, 0
	if c.Source == "tmdb" {
		kind, tmdb = "collection", c.Config.TMDBCollection
	} else {
		for _, it := range matched {
			if it.TMDBID > 0 {
				kind, tmdb, tvdb = it.Kind, it.TMDBID, it.TVDBID
				break
			}
		}
	}
	if tmdb == 0 {
		return nil, errors.New("no title in it to take a picture from")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s:%d", c.Name, st.print(), kind, tmdb)))
	dir := filepath.Join(s.artDir(), "collections")
	file := filepath.Join(dir, fmt.Sprintf("%d-made-%s.jpg", c.ID, hex.EncodeToString(sum[:6])))
	if b, err := os.ReadFile(file); err == nil && len(b) > 0 {
		return b, nil
	}
	if !s.TMDB().HasAPIKey() {
		return nil, errors.New("a TMDB key is needed")
	}
	art, logo, err := s.artPosterArt(ctx, kind, tmdb, tvdb, "", st, "backdrop")
	if err != nil {
		return nil, err
	}
	if kind != "collection" {
		logo = nil // the first title's logo isn't the collection's
	}
	img := artDraw(art, artPosterW, artPosterH, st, artPosterWords(st, "collection", c.Name, 0, ""), logo, artExtras{dir: s.artDir()})
	out, err := artJPEG(img, 88)
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(dir, 0o755)
	if old, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("%d-made-*.jpg", c.ID))); len(old) > 0 {
		for _, o := range old {
			_ = os.Remove(o)
		}
	}
	_ = os.WriteFile(file, out, 0o644)
	return out, nil
}

// ---- the page ----

// GET /api/home/collections (admin)
func (s *Server) handlePushCollections(w http.ResponseWriter, r *http.Request) {
	list, err := s.collAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The collections couldn't be read.")
		return
	}
	type listRef struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	lists := []listRef{}
	if all, err := s.allImportLists(); err == nil {
		for _, l := range all {
			lists = append(lists, listRef{l.ID, l.Name})
		}
	}
	_, jf := s.jellyfinServer()
	writeJSON(w, http.StatusOK, map[string]any{"collections": list, "lists": lists, "hours": s.collHours(), "jellyfin": jf, "plex": len(s.plexServers()) > 0, "tmdb": s.TMDB().HasAPIKey()})
}

func (s *Server) collSaveRow(ctx context.Context, c srvCollection, id int64) (int64, error) {
	cfg, _ := json.Marshal(c.Config)
	args := []any{c.Name, c.Source, string(cfg), c.Media, strings.Join(c.Servers, ","), c.SortTitle, c.Poster, strings.Join(c.Visibility, ","), btoi(c.OnlyLibrary), btoi(c.RemoveMissing), btoi(c.Enabled)}
	if id == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO server_collections (name, source, config, media, servers, sort_title, poster, visibility, only_library, remove_missing, enabled, created_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			append(args, time.Now().UnixMilli())...)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE server_collections SET name = ?, source = ?, config = ?, media = ?, servers = ?, sort_title = ?, poster = ?, visibility = ?, only_library = ?, remove_missing = ?, enabled = ? WHERE id = ?`, append(args, id)...)
	return id, err
}

// POST /api/home/collections (admin): add one, and sync it now.
func (s *Server) handleAddPushCollection(w http.ResponseWriter, r *http.Request) {
	var in collInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	c, problem := s.collCheck(r.Context(), in)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	var n int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM server_collections`).Scan(&n)
	if n >= 300 {
		writeError(w, http.StatusBadRequest, "That's 300 collections already.")
		return
	}
	id, err := s.collSaveRow(r.Context(), c, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The collection couldn't be saved.")
		return
	}
	if c.Enabled {
		s.collSyncSoon([]int64{id})
	}
	out, _ := s.collGet(r.Context(), id)
	writeJSON(w, http.StatusCreated, out)
}

func collIDFrom(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// PUT /api/home/collections/{id} (admin): change one, and sync it now.
func (s *Server) handleUpdatePushCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := collIDFrom(w, r)
	if !ok {
		return
	}
	if _, err := s.collGet(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "That collection isn't there any more.")
		return
	}
	var in collInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	c, problem := s.collCheck(r.Context(), in)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	if _, err := s.collSaveRow(r.Context(), c, id); err != nil {
		writeError(w, http.StatusInternalServerError, "The collection couldn't be saved.")
		return
	}
	if c.Enabled {
		s.collSyncSoon([]int64{id})
	}
	out, _ := s.collGet(r.Context(), id)
	writeJSON(w, http.StatusOK, out)
}

// DELETE /api/home/collections/{id}?servers=1 (admin): forget it; with
// servers=1 it's deleted from Jellyfin and Plex too (their titles stay).
func (s *Server) handleDeletePushCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := collIDFrom(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var problems []string
	if r.URL.Query().Get("servers") == "1" {
		for _, l := range s.collLinks(ctx, id) {
			if err := s.collDeleteRemote(ctx, l); err != nil {
				problems = append(problems, s.collTargetName(ctx, l.Target)+": "+err.Error())
			}
		}
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM server_collection_links WHERE collection_id = ?`, id)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM server_collections WHERE id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "The collection couldn't be removed.")
		return
	}
	_ = os.Remove(s.collUploadFile(id))
	if old, _ := filepath.Glob(filepath.Join(s.artDir(), "collections", fmt.Sprintf("%d-made-*.jpg", id))); len(old) > 0 {
		for _, o := range old {
			_ = os.Remove(o)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": true, "problems": problems})
}

func (s *Server) collDeleteRemote(ctx context.Context, l *collLink) error {
	p := strings.Split(l.Target, ":")
	srvID, _ := strconv.ParseInt(p[min(1, len(p)-1)], 10, 64)
	if p[0] == "plex" {
		ps, ok := s.artPlexServer(srvID)
		if !ok {
			return errors.New("the server isn't connected now")
		}
		return s.mediaClient.PlexDeleteCollection(ctx, ps, l.Remote)
	}
	srv, ok := s.jellyfinServer()
	if !ok {
		return errors.New("the server isn't connected now")
	}
	return s.mediaClient.JellyfinDeleteItem(ctx, srv, l.Remote)
}

// POST /api/home/collections/{id}/sync (admin)
func (s *Server) handleSyncPushCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := collIDFrom(w, r)
	if !ok {
		return
	}
	if _, err := s.collGet(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "That collection isn't there any more.")
		return
	}
	s.collSyncSoon([]int64{id})
	writeJSON(w, http.StatusOK, map[string]any{"started": true})
}

// POST /api/home/collections/sync (admin): every switched-on collection now.
func (s *Server) handleSyncPushCollections(w http.ResponseWriter, r *http.Request) {
	list, err := s.collAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The collections couldn't be read.")
		return
	}
	var ids []int64
	for _, c := range list {
		if c.Enabled {
			ids = append(ids, c.ID)
		}
	}
	s.collSyncSoon(ids)
	writeJSON(w, http.StatusOK, map[string]any{"started": len(ids)})
}

// PUT /api/home/collections/settings (admin): hours between syncs.
func (s *Server) handlePushCollectionSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hours int `json:"hours"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Hours < 1 || body.Hours > 24*7 {
		writeError(w, http.StatusBadRequest, "Choose between 1 hour and a week.")
		return
	}
	if err := s.Settings.Set(keyCollHours, strconv.Itoa(body.Hours), false); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hours": body.Hours})
}

// POST /api/home/collections/preview (admin): what a collection as typed
// would hold, without sending anything.
func (s *Server) handlePreviewPushCollection(w http.ResponseWriter, r *http.Request) {
	var in collInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that. Reload the page and try again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	c, problem := s.collCheck(ctx, in)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	wants, err := s.collWanted(ctx, c)
	if err != nil {
		writeError(w, http.StatusBadGateway, "The titles couldn't be read: "+err.Error()+".")
		return
	}
	items, err := s.libItems(ctx, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, "The media servers couldn't be read: "+err.Error()+".")
		return
	}
	kinds := wantedKinds(c.Media)
	var mine []libItem
	for _, it := range items {
		if kinds[it.Kind] && containsStr(c.Servers, it.Server) {
			mine = append(mine, it)
		}
	}
	var matched []libItem
	found := map[int]bool{}
	total := len(wants)
	if c.Source == "rule" {
		byHolder := map[string][]libItem{}
		for _, it := range mine {
			byHolder[collTarget(it)] = append(byHolder[collTarget(it)], it)
		}
		for _, list := range byHolder {
			matched = append(matched, collOnce(collRule(c.Config, list, time.Now()))...)
		}
	} else {
		matched = collMatch(wants, mine, found)
	}
	names := []string{}
	seen := map[string]bool{}
	for _, it := range matched {
		n := it.Title
		if it.Year > 0 {
			n += " (" + strconv.Itoa(it.Year) + ")"
		}
		if !seen[n] && len(names) < 12 {
			seen[n] = true
			names = append(names, n)
		}
	}
	missing := []string{}
	for i, wnt := range wants {
		if !found[i] && len(missing) < collMaxMissing {
			missing = append(missing, wnt.name())
		}
	}
	inLib := len(collDistinct(matched))
	if c.Source == "rule" {
		total = inLib
	} else {
		inLib = len(found)
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": total, "inLibrary": inLib, "sample": names, "missing": missing})
}

// GET /api/home/collections/titles?q= (admin): library titles to pick by hand.
func (s *Server) handlePushCollectionTitles(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	out := []collPick{}
	if q == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	items, err := s.libItems(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusBadGateway, "The media servers couldn't be read: "+err.Error()+".")
		return
	}
	seen := map[string]bool{}
	for _, it := range items {
		if (it.TMDBID == 0 && it.IMDBID == "") || !strings.Contains(strings.ToLower(it.Title), q) {
			continue
		}
		k := it.Kind + ":" + strconv.Itoa(it.TMDBID) + ":" + it.IMDBID
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, collPick{Kind: it.Kind, TMDBID: it.TMDBID, IMDBID: it.IMDBID, Title: it.Title, Year: it.Year})
		if len(out) >= 30 {
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/home/collections/{id}/poster (admin, a form with "file"): the
// collection's own poster.
func (s *Server) handlePushCollectionPosterUpload(w http.ResponseWriter, r *http.Request) {
	id, ok := collIDFrom(w, r)
	if !ok {
		return
	}
	if _, err := s.collGet(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "That collection isn't there any more.")
		return
	}
	raw, _, ok := artFormFile(w, r)
	if !ok {
		return
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a picture Usherr can read (JPEG, PNG or WebP).")
		return
	}
	if b := img.Bounds(); b.Dx() > artPosterW {
		dst := image.NewRGBA(image.Rect(0, 0, artPosterW, max(b.Dy()*artPosterW/b.Dx(), 1)))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	out, err := artJPEG(img, 90)
	if err == nil {
		_ = os.MkdirAll(filepath.Dir(s.collUploadFile(id)), 0o755)
		err = os.WriteFile(s.collUploadFile(id), out, 0o644)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The picture couldn't be kept.")
		return
	}
	_, _ = s.db.ExecContext(r.Context(), `UPDATE server_collections SET poster = 'upload' WHERE id = ?`, id)
	c, _ := s.collGet(r.Context(), id)
	writeJSON(w, http.StatusOK, c)
}

// GET /api/home/collections/{id}/poster (admin): the poster it sends (made
// now in the style, when that's the choice).
func (s *Server) handlePushCollectionPoster(w http.ResponseWriter, r *http.Request) {
	id, ok := collIDFrom(w, r)
	if !ok {
		return
	}
	c, err := s.collGet(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("made") == "1" {
		c.Poster = "made"
	}
	var matched []libItem
	if c.Poster == "made" && c.Source != "tmdb" {
		if wants, err := s.collWanted(r.Context(), c); err == nil {
			if items, err := s.libItems(r.Context(), false); err == nil {
				if c.Source == "rule" {
					matched = collRule(c.Config, items, time.Now())
				} else {
					matched = collMatch(wants, items, map[int]bool{})
				}
			}
		}
	}
	data, _, err := s.collPosterFor(r.Context(), c, matched)
	if err != nil || len(data) == 0 {
		msg := "This collection has no poster of its own: the server makes one."
		if err != nil {
			msg = "The poster couldn't be made: " + err.Error() + "."
		}
		writeError(w, http.StatusNotFound, msg)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJPEG(w, data)
}
