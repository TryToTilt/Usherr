package api

// Usherr: who watches, across media servers. A household may watch in
// Jellyfin (or Emby), in Plex, or both; everything personal (the Diary,
// Wrapped, For you, Because you watched, the picks) asks here instead of one
// server. Someone on both servers is one person, matched by name, and their
// id names each place they watch: "<jellyfin id>,plex:<server>:<account>".

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/mediaservers"
)

// plexServers are the enabled Plex servers Usherr can read.
func (s *Server) plexServers() []mediaservers.Server {
	list, err := s.MediaServers.List()
	if err != nil {
		return nil
	}
	var out []mediaservers.Server
	for _, m := range list {
		if m.Enabled && m.Kind == mediaservers.KindPlex && m.Token != "" {
			out = append(out, m)
		}
	}
	return out
}

// hasWatchServer is whether any media server Usherr reads watching from is
// connected: Jellyfin, Emby or Plex.
func (s *Server) hasWatchServer() bool {
	if _, ok := s.jellyfinServer(); ok {
		return true
	}
	return len(s.plexServers()) > 0
}

func plexPersonID(server int64, account int) string {
	return fmt.Sprintf("plex:%d:%d", server, account)
}

type plexPersonRef struct {
	server  int64
	account int
}

// personParts splits a person's id: their Jellyfin or Emby user, and their
// Plex accounts.
func personParts(person string) (jf string, plex []plexPersonRef) {
	for _, part := range strings.Split(person, ",") {
		if rest, ok := strings.CutPrefix(part, "plex:"); ok {
			srv, acc, ok := strings.Cut(rest, ":")
			sid, err1 := strconv.ParseInt(srv, 10, 64)
			aid, err2 := strconv.Atoi(acc)
			if ok && err1 == nil && err2 == nil {
				plex = append(plex, plexPersonRef{sid, aid})
			}
			continue
		}
		if part != "" && jf == "" {
			jf = part
		}
	}
	return jf, plex
}

// jfPart is a person's Jellyfin or Emby user ("" when they only watch in Plex).
func jfPart(person string) string {
	jf, _ := personParts(person)
	return jf
}

// mergePeople makes one person of the same name on several servers.
func mergePeople(all []jfPerson) []jfPerson {
	byName := map[string]int{}
	var out []jfPerson
	for _, p := range all {
		key := strings.ToLower(strings.TrimSpace(p.Name))
		if i, ok := byName[key]; ok {
			out[i].ID += "," + p.ID
			out[i].IsAdmin = out[i].IsAdmin || p.IsAdmin
			continue
		}
		byName[key] = len(out)
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Plex's accounts change rarely; they're kept for a few minutes.
var plexPeopleCache = struct {
	sync.Mutex
	m map[int64]plexPeopleSeen
}{m: map[int64]plexPeopleSeen{}}

type plexPeopleSeen struct {
	list []mediaservers.PlexWatcher
	at   time.Time
}

func (s *Server) plexPeople(ctx context.Context, srv mediaservers.Server) ([]mediaservers.PlexWatcher, error) {
	plexPeopleCache.Lock()
	seen, ok := plexPeopleCache.m[srv.ID]
	plexPeopleCache.Unlock()
	if ok && time.Since(seen.at) < 5*time.Minute {
		return seen.list, nil
	}
	list, err := s.mediaClient.PlexAccounts(ctx, srv)
	if err != nil {
		return nil, err
	}
	plexPeopleCache.Lock()
	plexPeopleCache.m[srv.ID] = plexPeopleSeen{list, time.Now()}
	plexPeopleCache.Unlock()
	return list, nil
}

// played is one film or episode a person finished, from either server.
type played struct {
	mediaservers.Played
	source string // "jellyfin" or "plex"
	server int64  // the Plex server's id, for a Plex play
}

func (p played) day() string { return p.WatchedAt.In(time.Local).Format("2006-01-02") }

// playedLimits is how much of a Jellyfin history is read.
type playedLimits struct{ movies, episodes int }

// playedBy is what a person finished, on every server they watch on, newest
// first; since (when set) leaves out what was watched before it.
func (s *Server) playedBy(ctx context.Context, person string, since time.Time, lim playedLimits) ([]played, error) {
	jf, plex := personParts(person)
	var out []played
	if jf != "" {
		if srv, ok := s.jellyfinServer(); ok {
			list, err := jfPlayedBy(ctx, srv, jf, since, lim)
			if err != nil {
				return nil, err
			}
			out = append(out, list...)
		}
	}
	servers := map[int64]mediaservers.Server{}
	for _, m := range s.plexServers() {
		servers[m.ID] = m
	}
	for _, ref := range plex {
		srv, ok := servers[ref.server]
		if !ok {
			continue
		}
		list, err := s.mediaClient.PlexHistory(ctx, srv, ref.account, since)
		if err != nil {
			if jf == "" && len(plex) == 1 {
				return nil, err
			}
			slog.Info("plex: history not read", "server", srv.Name, "err", err)
			continue
		}
		for _, p := range list {
			out = append(out, played{p, "plex", srv.ID})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].WatchedAt.After(out[j].WatchedAt) })
	return out, nil
}

// jfPlayedBy is what a Jellyfin or Emby user finished, newest first.
func jfPlayedBy(ctx context.Context, srv mediaservers.Server, person string, since time.Time, lim playedLimits) ([]played, error) {
	base := "/Users/" + url.PathEscape(person) + "/Items?Recursive=true&Filters=IsPlayed&SortBy=DatePlayed&SortOrder=Descending&EnableImages=false&Fields=ProviderIds"
	if !since.IsZero() {
		base += "&MinDateLastSavedForUser=" + url.QueryEscape(since.UTC().Format(time.RFC3339))
	}
	var movies, eps struct {
		Items []jfPlayed `json:"Items"`
	}
	if err := jfJSON(ctx, srv, http.MethodGet, base+"&IncludeItemTypes=Movie&Limit="+strconv.Itoa(lim.movies), nil, &movies); err != nil {
		return nil, err
	}
	if err := jfJSON(ctx, srv, http.MethodGet, base+"&IncludeItemTypes=Episode&Limit="+strconv.Itoa(lim.episodes), nil, &eps); err != nil {
		return nil, err
	}
	var out []played
	for _, it := range movies.Items {
		at, err := time.Parse(time.RFC3339Nano, it.UserData.LastPlayedDate)
		id := jfItem{ProviderIDs: it.ProviderIDs}.tmdbID()
		if err != nil || id <= 0 {
			continue
		}
		out = append(out, played{mediaservers.Played{Kind: "movie", TMDBID: id, Title: it.Name, Year: it.ProductionYear, Minutes: int(it.RunTimeTicks / 600_000_000), WatchedAt: at}, "jellyfin", 0})
	}
	// The episodes' shows: their TMDB ids and years, a batch at a time.
	var series []string
	seen := map[string]bool{}
	for _, it := range eps.Items {
		if it.SeriesID != "" && !seen[it.SeriesID] {
			seen[it.SeriesID] = true
			series = append(series, it.SeriesID)
		}
	}
	tmdbOf, yearOf := map[string]int{}, map[string]int{}
	for i := 0; i < len(series); i += 100 {
		var shows struct {
			Items []jfItem `json:"Items"`
		}
		batch := series[i:min(i+100, len(series))]
		if err := jfJSON(ctx, srv, http.MethodGet, "/Users/"+url.PathEscape(person)+"/Items?EnableImages=false&Fields=ProviderIds&Ids="+strings.Join(batch, ","), nil, &shows); err != nil {
			return nil, err
		}
		for _, it := range shows.Items {
			tmdbOf[it.ID], yearOf[it.ID] = it.tmdbID(), it.ProductionYear
		}
	}
	for _, it := range eps.Items {
		at, err := time.Parse(time.RFC3339Nano, it.UserData.LastPlayedDate)
		id := tmdbOf[it.SeriesID]
		if err != nil || id <= 0 {
			continue
		}
		out = append(out, played{mediaservers.Played{Kind: "episode", TMDBID: id, Title: it.SeriesName, Year: yearOf[it.SeriesID], Season: it.Season, Episode: it.Episode, Minutes: int(it.RunTimeTicks / 600_000_000), WatchedAt: at}, "jellyfin", 0})
	}
	return out, nil
}

// plexTonight adds what a person is carrying on with in Plex: the server
// owner's On Deck (Plex keeps it for the owner), and for everyone the next
// episode of the shows they've watched lately, worked out from their history.
func (s *Server) plexTonight(ctx context.Context, person string, out []tonightChoice, seenShow map[int]bool) []tonightChoice {
	_, plex := personParts(person)
	if len(plex) == 0 {
		return out
	}
	servers := map[int64]mediaservers.Server{}
	for _, m := range s.plexServers() {
		servers[m.ID] = m
	}
	code := func(season, episode int, name string) string {
		c := fmt.Sprintf("S%02dE%02d", season, episode)
		if name != "" {
			c += " · " + name
		}
		return c
	}
	for _, ref := range plex {
		srv, ok := servers[ref.server]
		if !ok || ref.account != 1 {
			continue
		}
		deck, err := s.mediaClient.PlexOnDeck(ctx, srv)
		if err != nil {
			continue
		}
		for _, u := range deck {
			switch {
			case u.Kind == "movie" && u.Started:
				out = append(out, tonightChoice{roomTitle: roomTitle{TMDB: u.TMDBID, Name: u.Title, Year: u.Year}, Kind: "movie", Label: "Continue", Why: "film they started"})
			case u.Kind == "episode" && !seenShow[u.TMDBID]:
				seenShow[u.TMDBID] = true
				label, why := "Next", "next episode of a show they watch, "
				if u.Started {
					label, why = "Continue", "episode they started, "
				}
				out = append(out, tonightChoice{roomTitle: roomTitle{TMDB: u.TMDBID, Name: u.Title}, Kind: "tv", Label: label, Why: why + fmt.Sprintf("S%dE%d", u.Season, u.Episode), Episode: code(u.Season, u.Episode, u.Name)})
			}
		}
	}
	wa, err := s.watchingOf(ctx, person)
	if err != nil || wa == nil {
		return out
	}
	added := 0
	for _, show := range wa.recentShows {
		if added >= 12 {
			break
		}
		ref, ok := wa.plexShow[show]
		srv, known := servers[ref.server]
		if !ok || !known || seenShow[show] {
			continue
		}
		f := wa.furthest[show]
		next, ok := s.mediaClient.PlexNextEpisode(ctx, srv, ref.key, f[0], f[1])
		if !ok {
			continue
		}
		seenShow[show] = true
		added++
		out = append(out, tonightChoice{roomTitle: roomTitle{TMDB: show, Name: wa.names[-show]}, Kind: "tv", Label: "Next", Why: "next episode of a show they watch, " + fmt.Sprintf("S%dE%d", next.Season, next.Episode), Episode: code(next.Season, next.Episode, next.Name)})
	}
	return out
}

// jfViewer is the Jellyfin or Emby user to read the whole library through:
// an administrator, who sees every library ("" when nobody is on Jellyfin).
func jfViewer(people []jfPerson) string {
	viewer := ""
	for _, p := range people {
		if jf := jfPart(p.ID); jf != "" && (viewer == "" || p.IsAdmin) {
			viewer = jf
			if p.IsAdmin {
				break
			}
		}
	}
	return viewer
}

// plexShelves is everything in the Plex servers' film and TV libraries.
func (s *Server) plexShelves(ctx context.Context) []mediaservers.ShelfTitle {
	var out []mediaservers.ShelfTitle
	for _, srv := range s.plexServers() {
		list, err := s.mediaClient.PlexShelf(ctx, srv)
		if err != nil {
			slog.Info("plex: libraries not read", "server", srv.Name, "err", err)
			continue
		}
		out = append(out, list...)
	}
	return out
}
