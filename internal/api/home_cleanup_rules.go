package api

// Usherr: clean-up rule sets, the job Maintainerr did.
//
// An administrator makes rule sets: which library (movies, TV, or one library
// folder), conditions joined by ALL or ANY, what to do (delete the files and
// stop looking for the title, only stop looking, or delete and look again
// later) and a grace period. A title that matches first goes on "Leaving
// soon" with a date; only when that date has come, and it still matches, is
// anything done to it. Everyone can see Leaving soon, and members can press
// "Keep it" when an administrator allows it.
//
// Safety, because losing a movie or a show is expensive here:
//   - every rule set is off until switched on, and Preview shows exactly what
//     would go on Leaving soon and what would be acted on now, with reasons;
//   - streamed titles are never touched: a .strm pointer file, anything in a
//     "-Stream" or "Streamed" folder, and any file that is a link (nzbdav's
//     virtual drive) are left out before any condition is looked at;
//   - titles with a keep tag, and kept titles, are never touched;
//   - nothing is done when what's been watched wasn't read from every media
//     server in the last two days (watchedFreshFor, as for the basic rules);
//   - one run acts on at most MaxPerRun titles (50 unless changed);
//   - every action goes to Activity.
//
// The basic rules in watched.go (CleanupRules) keep working as they did.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/settings"
)

// Condition types.
const (
	condWatchedDays   = "watchedDays"   // watched, last played N days ago or more
	condUnwatchedDays = "unwatchedDays" // never watched, arrived N days ago or more
	condIdleDays      = "idleDays"      // nobody watched it in the last N days
	condWatchedAll    = "watchedAll"    // every part watched (a whole movie, season or show)
	condSizeOver      = "sizeOverGB"    // files over N GB
	condYearBefore    = "yearBefore"    // released before year N
	condHasTag        = "hasTag"
	condLacksTag      = "lacksTag"
	condRequestedBy   = "requestedBy" // added or requested by account N
	condNotRequested  = "notRequested"
	condQuality       = "quality"   // every file's quality contains the text ("2160p", "Bluray-1080p")
	condShowEnded     = "showEnded" // TMDB says the show ended or was cancelled
)

// Actions.
const (
	actDeleteUnmonitor = "delete-unmonitor" // the default
	actUnmonitor       = "unmonitor"
	actDelete          = "delete" // delete, keep looking (download again later)
)

// Show scopes: what one match on a show stands for.
const (
	scopeEpisodes = "episodes" // each episode file on its own
	scopeSeason   = "season"   // a whole season at once
	scopeShow     = "show"     // the whole show at once
)

const (
	defaultGraceDays = 14
	maxRuleSets      = 50
)

type ruleCondition struct {
	Type   string  `json:"type"`
	Number float64 `json:"number,omitempty"`
	Text   string  `json:"text,omitempty"`
}

// cleanupRuleSet is one rule set. Library is "movies" or "tv"; Root, when
// set, limits it to that library folder.
type cleanupRuleSet struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	Enabled    bool            `json:"enabled"`
	Library    string          `json:"library"`
	Root       string          `json:"root"`
	Mode       string          `json:"mode"`  // "simple" or "advanced": which editor it opens in
	Match      string          `json:"match"` // "all" or "any"
	Conditions []ruleCondition `json:"conditions"`
	ShowScope  string          `json:"showScope"`
	Action     string          `json:"action"`
	GraceDays  int             `json:"graceDays"`
	KeepTags   []string        `json:"keepTags"`
}

// cleanupOptions are shared by every rule set.
type cleanupOptions struct {
	MaxPerRun   int  `json:"maxPerRun"`   // 50 unless changed
	MembersKeep bool `json:"membersKeep"` // members may press "Keep it"
	// CollectionServers are the media servers that get a "Leaving soon"
	// collection; none unless picked.
	CollectionServers []int64 `json:"collectionServers"`
	CollectionName    string  `json:"collectionName"`
	LastRun           string  `json:"lastRun,omitempty"` // RFC 3339
	LastError         string  `json:"lastError,omitempty"`
}

var cleanupRulesMu sync.Mutex // one rule run at a time

// ---- storage

func (o *cleanupOptions) fill() {
	if o.MaxPerRun <= 0 {
		o.MaxPerRun = maxCleanupPerRun
	}
	if o.CollectionServers == nil {
		o.CollectionServers = []int64{}
	}
	if strings.TrimSpace(o.CollectionName) == "" {
		o.CollectionName = "Leaving soon"
	}
}

func (s *Server) cleanupOptions() cleanupOptions {
	var o cleanupOptions
	var raw string
	if err := s.db.QueryRow(`SELECT config FROM cleanup_options WHERE id = 1`).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &o)
	}
	o.fill()
	return o
}

func (s *Server) saveCleanupOptions(o cleanupOptions) error {
	b, _ := json.Marshal(o)
	_, err := s.db.Exec(`INSERT INTO cleanup_options (id, config) VALUES (1, ?) ON CONFLICT (id) DO UPDATE SET config = excluded.config`, string(b))
	return err
}

func (rs *cleanupRuleSet) fill() {
	if rs.Library != "tv" {
		rs.Library = "movies"
	}
	if rs.Mode != "advanced" {
		rs.Mode = "simple"
	}
	if rs.Match != "all" {
		rs.Match = "any"
	}
	switch rs.ShowScope {
	case scopeSeason, scopeShow:
	default:
		rs.ShowScope = scopeEpisodes
	}
	switch rs.Action {
	case actUnmonitor, actDelete:
	default:
		rs.Action = actDeleteUnmonitor
	}
	if rs.GraceDays < 0 {
		rs.GraceDays = defaultGraceDays
	}
	if rs.Conditions == nil {
		rs.Conditions = []ruleCondition{}
	}
	if rs.KeepTags == nil {
		rs.KeepTags = []string{}
	}
}

func (s *Server) listRuleSets() ([]cleanupRuleSet, error) {
	rows, err := s.db.Query(`SELECT id, enabled, config FROM cleanup_rule_sets ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list rule sets: %w", err)
	}
	defer rows.Close()
	out := []cleanupRuleSet{}
	for rows.Next() {
		var id int64
		var on int
		var raw string
		if err := rows.Scan(&id, &on, &raw); err != nil {
			return nil, err
		}
		var rs cleanupRuleSet
		_ = json.Unmarshal([]byte(raw), &rs)
		rs.ID, rs.Enabled = id, on != 0
		rs.fill()
		out = append(out, rs)
	}
	return out, rows.Err()
}

func (s *Server) saveRuleSet(rs cleanupRuleSet) (cleanupRuleSet, error) {
	rs.fill()
	b, _ := json.Marshal(rs)
	if rs.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO cleanup_rule_sets (enabled, config, created_ms) VALUES (?, ?, ?)`, rs.Enabled, string(b), time.Now().UnixMilli())
		if err != nil {
			return rs, err
		}
		rs.ID, _ = res.LastInsertId()
		return rs, nil
	}
	res, err := s.db.Exec(`UPDATE cleanup_rule_sets SET enabled = ?, config = ? WHERE id = ?`, rs.Enabled, string(b), rs.ID)
	if err != nil {
		return rs, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return rs, sql.ErrNoRows
	}
	return rs, nil
}

// leavingEntry is a title on Leaving soon.
type leavingEntry struct {
	Unit      string `json:"unit"`
	RuleSetID int64  `json:"ruleSetId"`
	Kind      string `json:"kind"` // movie, show, season or episode
	TitleID   int64  `json:"titleId"`
	Season    int    `json:"season,omitempty"`
	Title     string `json:"title"`
	Reason    string `json:"reason"`
	First     int64  `json:"firstMs"`
	Act       int64  `json:"actMs"` // when it is acted on, if it still matches
}

func (s *Server) listLeaving() ([]leavingEntry, error) {
	rows, err := s.db.Query(`SELECT unit, rule_set_id, kind, title_id, season, title, reason, first_ms, act_ms FROM cleanup_leaving ORDER BY act_ms, title`)
	if err != nil {
		return nil, fmt.Errorf("list leaving soon: %w", err)
	}
	defer rows.Close()
	out := []leavingEntry{}
	for rows.Next() {
		var e leavingEntry
		if err := rows.Scan(&e.Unit, &e.RuleSetID, &e.Kind, &e.TitleID, &e.Season, &e.Title, &e.Reason, &e.First, &e.Act); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// exemptEntry is a kept title.
type exemptEntry struct {
	Unit    string `json:"unit"`
	Kind    string `json:"kind"` // movie or show
	TitleID int64  `json:"titleId"`
	Title   string `json:"title"`
	ByUser  int64  `json:"byUser,omitempty"`
	ByName  string `json:"byName,omitempty"`
	At      int64  `json:"atMs"`
}

func (s *Server) listExempt() ([]exemptEntry, error) {
	rows, err := s.db.Query(`SELECT unit, kind, title_id, title, by_user, by_name, at_ms FROM cleanup_exempt ORDER BY title`)
	if err != nil {
		return nil, fmt.Errorf("list kept titles: %w", err)
	}
	defer rows.Close()
	out := []exemptEntry{}
	for rows.Next() {
		var e exemptEntry
		if err := rows.Scan(&e.Unit, &e.Kind, &e.TitleID, &e.Title, &e.ByUser, &e.ByName, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- what a title looks like to the rules

// cleanupUnit is what one match stands for: a movie, an episode file, a
// season or a whole show.
type cleanupUnit struct {
	Key        string // movie:ID, file:EPISODE, season:SERIES:N or show:SERIES
	Exempt     string // the key that keeps it: movie:ID or show:SERIES
	Kind       string // movie, episode, season or show
	ID         int64  // movie id, first episode id, or series id
	SeriesID   int64
	Season     int
	TMDBID     int
	Title      string
	Files      []string
	EpisodeIDs []int64 // the downloaded episodes it holds
	Tags       []string
	Year       int
	Qualities  []string
	Size       int64
	AddedBy    int64
	Requesters []int64
	Root       string // the library folder its files are in
	ShowStatus string

	Known      bool      // a media server has it (so "never watched" is believable)
	AnyPlay    bool      // some part was played
	Watched    bool      // every part was played
	LastPlayed time.Time // the latest play of any part
	Fresh      time.Time // when the newest file arrived
	Arrived    time.Time // when it came into the library (for "never watched")
	Streamed   bool
}

// streamedFile says whether a library file is a streamed copy: a .strm
// pointer, a file in a streamed folder ("Movies-Stream", "Streamed"), or a
// link into nzbdav's virtual drive. Such files are never touched.
func streamedFile(path string) bool {
	if path == "" {
		return false
	}
	if streamedPath(path) {
		return true
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return true
	}
	return false
}

func days(n float64) time.Duration { return time.Duration(n * 24 * float64(time.Hour)) }

// evalCondition says whether one condition holds for u, and why.
func evalCondition(c ruleCondition, u cleanupUnit, now time.Time) (bool, string) {
	switch c.Type {
	case condWatchedDays:
		if c.Number <= 0 || !u.Watched || u.LastPlayed.IsZero() || !u.Fresh.Before(u.LastPlayed) {
			return false, ""
		}
		if now.Sub(u.LastPlayed) >= days(c.Number) {
			return true, "watched " + ago(u.LastPlayed, now) + " ago"
		}
	case condUnwatchedDays:
		if c.Number <= 0 || u.AnyPlay || !u.Known || u.Arrived.IsZero() {
			return false, ""
		}
		if now.Sub(u.Arrived) >= days(c.Number) {
			return true, "not watched in the " + ago(u.Arrived, now) + " since it arrived"
		}
	case condIdleDays:
		if c.Number <= 0 || !u.Known {
			return false, ""
		}
		if u.AnyPlay {
			if u.LastPlayed.IsZero() || !u.Fresh.Before(u.LastPlayed) {
				return false, ""
			}
			if now.Sub(u.LastPlayed) >= days(c.Number) {
				return true, "nobody watched it in " + ago(u.LastPlayed, now)
			}
			return false, ""
		}
		if !u.Arrived.IsZero() && now.Sub(u.Arrived) >= days(c.Number) {
			return true, "nobody watched it in " + ago(u.Arrived, now)
		}
	case condWatchedAll:
		if u.Watched {
			if u.Kind == "movie" || u.Kind == "episode" {
				return true, "watched"
			}
			return true, "watched completely"
		}
	case condSizeOver:
		if c.Number > 0 && float64(u.Size) > c.Number*gib {
			return true, fmt.Sprintf("%.1f GB", float64(u.Size)/gib)
		}
	case condYearBefore:
		if u.Year > 0 && float64(u.Year) < c.Number {
			return true, fmt.Sprintf("from %d", u.Year)
		}
	case condHasTag:
		if t := strings.TrimSpace(c.Text); t != "" && hasTag(u.Tags, []string{t}) {
			return true, "tagged " + t
		}
	case condLacksTag:
		if t := strings.TrimSpace(c.Text); t != "" && !hasTag(u.Tags, []string{t}) {
			return true, "not tagged " + t
		}
	case condRequestedBy:
		id := int64(c.Number)
		if id <= 0 {
			return false, ""
		}
		if u.AddedBy == id {
			return true, "added by the account picked"
		}
		for _, r := range u.Requesters {
			if r == id {
				return true, "requested by the account picked"
			}
		}
	case condNotRequested:
		if len(u.Requesters) == 0 {
			return true, "nobody requested it"
		}
	case condQuality:
		want := strings.ToLower(strings.TrimSpace(c.Text))
		if want == "" || len(u.Qualities) == 0 {
			return false, ""
		}
		for _, q := range u.Qualities {
			if !strings.Contains(strings.ToLower(q), want) {
				return false, ""
			}
		}
		return true, "quality " + c.Text
	case condShowEnded:
		st := strings.ToLower(u.ShowStatus)
		if u.Kind != "movie" && st == "ended" {
			return true, "the show has ended"
		}
		if u.Kind != "movie" && (st == "canceled" || st == "cancelled") {
			return true, "the show was cancelled"
		}
	}
	return false, ""
}

// ruleSetMatches says whether u matches the rule set's conditions (ALL or
// ANY), and why. A rule set with no conditions matches nothing.
func ruleSetMatches(rs cleanupRuleSet, u cleanupUnit, now time.Time) (bool, string) {
	if len(rs.Conditions) == 0 {
		return false, ""
	}
	var why []string
	for _, c := range rs.Conditions {
		ok, reason := evalCondition(c, u, now)
		if rs.Match == "all" {
			if !ok {
				return false, ""
			}
			why = append(why, reason)
			continue
		}
		if ok {
			return true, reason
		}
	}
	if rs.Match == "all" {
		return true, strings.Join(nonEmpty(why), ", ")
	}
	return false, ""
}

func nonEmpty(list []string) []string {
	out := list[:0:0]
	for _, s := range list {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ruleSetCovers says whether the rule set is for u's library.
func ruleSetCovers(rs cleanupRuleSet, u cleanupUnit) bool {
	if (rs.Library == "movies") != (u.Kind == "movie") {
		return false
	}
	if rs.Root == "" {
		return true
	}
	return u.Root != "" && strings.EqualFold(filepath.Clean(rs.Root), filepath.Clean(u.Root))
}

// ---- reading the library

// cleanupSnapshot is everything the rules look at, read once per run.
type cleanupSnapshot struct {
	movies       []library.Movie
	shows        []library.Series
	episodes     map[int64][]library.Episode
	movieTags    map[int64][]string
	seriesTags   map[int64][]string
	movieWatch   map[int64]library.Watched
	movieHeld    map[int64]bool
	showHeld     map[int64]bool
	showPlayed   map[int64]bool
	epWatch      map[[3]int64]library.Watched
	added        map[int64]time.Time
	downloaded   map[int64]time.Time
	requesters   map[string][]int64 // "movie:TMDB" / "tv:TMDB"
	movieFolders []string
	tvFolders    []string
}

func (s *Server) readCleanupSnapshot(needMovies, needTV bool) (*cleanupSnapshot, error) {
	snap := &cleanupSnapshot{
		episodes: map[int64][]library.Episode{}, movieWatch: map[int64]library.Watched{}, movieHeld: map[int64]bool{},
		showHeld: map[int64]bool{}, showPlayed: map[int64]bool{}, epWatch: map[[3]int64]library.Watched{}, requesters: map[string][]int64{},
		movieFolders: s.movieFolders(), tvFolders: s.tvFolders(),
	}
	watched, err := s.MovieRepo.ListWatched()
	if err != nil {
		return nil, err
	}
	for _, w := range watched {
		switch w.Kind {
		case "movie":
			snap.movieWatch[w.TitleID] = w
		case watchedHeld:
			snap.movieHeld[w.TitleID] = true
		case watchedHeldShow:
			snap.showHeld[w.TitleID] = true
		case "episode":
			snap.epWatch[[3]int64{w.TitleID, int64(w.Season), int64(w.Episode)}] = w
			snap.showPlayed[w.TitleID] = true
		}
	}
	if snap.movieTags, err = s.MovieRepo.AllTitleTags(library.TagMovie); err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	if snap.seriesTags, err = s.MovieRepo.AllTitleTags(library.TagSeries); err != nil {
		return nil, fmt.Errorf("read tags: %w", err)
	}
	rows, err := s.db.Query(`SELECT kind, payload, requested_by FROM requests WHERE status = 'approved' AND requested_by IS NOT NULL AND kind IN ('movie', 'tv')`)
	if err != nil {
		return nil, fmt.Errorf("read requests: %w", err)
	}
	for rows.Next() {
		var kind, payload string
		var by int64
		if rows.Scan(&kind, &payload, &by) == nil {
			if id := payloadTMDBID(payload); id > 0 {
				k := fmt.Sprintf("%s:%d", kind, id)
				snap.requesters[k] = append(snap.requesters[k], by)
			}
		}
	}
	rows.Close()
	if needMovies && s.moviesEnabled() {
		if snap.movies, err = s.MovieRepo.List(); err != nil {
			return nil, err
		}
		if snap.added, err = s.MovieRepo.MovieAddedDates(); err != nil {
			return nil, err
		}
		if snap.downloaded, err = s.movieDownloadTimes(); err != nil {
			return nil, err
		}
	}
	if needTV && s.tvEnabled() {
		if snap.shows, err = s.MovieRepo.ListSeries(); err != nil {
			return nil, err
		}
		for _, sh := range snap.shows {
			eps, err := s.MovieRepo.ListEpisodes(sh.ID)
			if err != nil {
				return nil, err
			}
			snap.episodes[sh.ID] = eps
		}
	}
	return snap, nil
}

// movieUnits are the downloaded movies.
func (snap *cleanupSnapshot) movieUnits() []cleanupUnit {
	var out []cleanupUnit
	for _, m := range snap.movies {
		if m.Status != library.StatusDownloaded || m.FilePath == "" {
			continue
		}
		fresh := latest(snap.downloaded[m.ID], fileTime(m.FilePath))
		w, seen := snap.movieWatch[m.ID]
		u := cleanupUnit{
			Key: fmt.Sprintf("movie:%d", m.ID), Exempt: fmt.Sprintf("movie:%d", m.ID), Kind: "movie", ID: m.ID, TMDBID: m.TMDBID,
			Title: movieLabel(m), Files: []string{m.FilePath}, Tags: snap.movieTags[m.ID], Year: m.Year, Qualities: []string{m.Quality},
			Size: fileSize(m.FilePath), AddedBy: m.AddedBy, Requesters: snap.requesters[fmt.Sprintf("movie:%d", m.TMDBID)],
			Root: rootContaining(snap.movieFolders, m.FilePath), Known: seen || snap.movieHeld[m.ID],
			AnyPlay: seen, Watched: seen, LastPlayed: w.LastPlayed, Fresh: fresh, Arrived: latest(snap.added[m.ID], fresh),
			Streamed: streamedFile(m.FilePath),
		}
		out = append(out, u)
	}
	return out
}

// showUnits are the downloaded episodes of every show, grouped by scope.
func (snap *cleanupSnapshot) showUnits(scope string) []cleanupUnit {
	var out []cleanupUnit
	for _, sh := range snap.shows {
		var files []string
		byFile := map[string][]library.Episode{}
		for _, e := range snap.episodes[sh.ID] {
			if e.Status != library.StatusDownloaded || e.FilePath == "" {
				continue
			}
			if _, ok := byFile[e.FilePath]; !ok {
				files = append(files, e.FilePath)
			}
			byFile[e.FilePath] = append(byFile[e.FilePath], e)
		}
		if len(files) == 0 {
			continue
		}
		base := cleanupUnit{
			Exempt: fmt.Sprintf("show:%d", sh.ID), SeriesID: sh.ID, TMDBID: sh.TMDBID, Tags: snap.seriesTags[sh.ID], Year: sh.Year,
			AddedBy: sh.AddedBy, Requesters: snap.requesters[fmt.Sprintf("tv:%d", sh.TMDBID)], ShowStatus: sh.TMDBStatus,
			Known: snap.showHeld[sh.ID] || snap.showPlayed[sh.ID],
		}
		// groups of files, in order: one per file, per season, or the show.
		type group struct {
			key, kind, title string
			id               int64
			season           int
			files            []string
		}
		var groups []group
		switch scope {
		case scopeShow:
			groups = append(groups, group{key: fmt.Sprintf("show:%d", sh.ID), kind: "show", title: sh.Title, id: sh.ID, files: files})
		case scopeSeason:
			idx := map[int]int{}
			for _, f := range files {
				sn := byFile[f][0].Season
				i, ok := idx[sn]
				if !ok {
					i = len(groups)
					idx[sn] = i
					groups = append(groups, group{key: fmt.Sprintf("season:%d:%d", sh.ID, sn), kind: "season", title: fmt.Sprintf("%s Season %d", sh.Title, sn), id: sh.ID, season: sn})
				}
				groups[i].files = append(groups[i].files, f)
			}
		default:
			for _, f := range files {
				eps := byFile[f]
				first, last := eps[0], eps[len(eps)-1]
				title := fmt.Sprintf("%s S%02dE%02d", sh.Title, first.Season, first.Episode)
				if len(eps) > 1 {
					title += fmt.Sprintf("-E%02d", last.Episode)
				}
				groups = append(groups, group{key: fmt.Sprintf("file:%d", first.ID), kind: "episode", title: title, id: first.ID, season: first.Season, files: []string{f}})
			}
		}
		for _, g := range groups {
			u := base
			u.Key, u.Kind, u.ID, u.Season, u.Title, u.Files = g.key, g.kind, g.id, g.season, g.title, g.files
			u.Watched = true
			for _, f := range g.files {
				u.Fresh = latest(u.Fresh, fileTime(f))
				u.Size += fileSize(f)
				if streamedFile(f) {
					u.Streamed = true
				}
				if u.Root == "" {
					u.Root = rootContaining(snap.tvFolders, f)
				}
				for _, e := range byFile[f] {
					u.EpisodeIDs = append(u.EpisodeIDs, e.ID)
					u.Qualities = append(u.Qualities, e.Quality)
					w, seen := snap.epWatch[[3]int64{sh.ID, int64(e.Season), int64(e.Episode)}]
					if seen {
						u.AnyPlay = true
						u.LastPlayed = latest(u.LastPlayed, w.LastPlayed)
					} else {
						u.Watched = false
					}
				}
			}
			u.Arrived = u.Fresh
			out = append(out, u)
		}
	}
	return out
}

// ---- a run

// cleanupMatch is a unit a rule set matched.
type cleanupMatch struct {
	Unit      string `json:"unit"`
	RuleSetID int64  `json:"ruleSetId"`
	RuleSet   string `json:"ruleSet"`
	Kind      string `json:"kind"`
	TitleID   int64  `json:"titleId"`
	Season    int    `json:"season,omitempty"`
	Title     string `json:"title"`
	Reason    string `json:"reason"`
	Action    string `json:"action"`
	ActMs     int64  `json:"actMs,omitempty"` // when it is due (on Leaving soon)

	unit cleanupUnit
	rs   cleanupRuleSet
}

// cleanupPlan is what a run would do.
type cleanupPlan struct {
	NewLeaving []cleanupMatch `json:"newLeaving"` // go on Leaving soon now
	Due        []cleanupMatch `json:"due"`        // acted on now (grace over, still matching)
	Waiting    []cleanupMatch `json:"waiting"`    // on Leaving soon, not due yet
	Dropped    []leavingEntry `json:"dropped"`    // on Leaving soon, no longer matching: taken off
	More       int            `json:"more"`       // due, but over the per-run limit: left for the next run
	Streamed   int            `json:"skippedStreamed"`
	Kept       int            `json:"skippedKept"`
	Limit      int            `json:"limit"`
	Stale      string         `json:"stale,omitempty"` // why nothing would be acted on
}

// matchRuleSets finds every unit the rule sets match (the first rule set to
// match a unit wins), leaving out streamed, tagged-keep and kept titles.
func (s *Server) matchRuleSets(sets []cleanupRuleSet, now time.Time) ([]cleanupMatch, int, int, error) {
	needMovies, needTV := false, false
	for _, rs := range sets {
		if rs.Library == "movies" {
			needMovies = true
		} else {
			needTV = true
		}
	}
	snap, err := s.readCleanupSnapshot(needMovies, needTV)
	if err != nil {
		return nil, 0, 0, err
	}
	exempt := map[string]bool{}
	if list, err := s.listExempt(); err == nil {
		for _, e := range list {
			exempt[e.Unit] = true
		}
	}
	return matchUnits(sets, snap, exempt, now)
}

func matchUnits(sets []cleanupRuleSet, snap *cleanupSnapshot, exempt map[string]bool, now time.Time) ([]cleanupMatch, int, int, error) {
	var out []cleanupMatch
	seen := map[string]bool{}
	streamed, kept := map[string]bool{}, map[string]bool{}
	var movieUnits []cleanupUnit
	moviesRead := false
	showUnits := map[string][]cleanupUnit{}
	for _, rs := range sets {
		var units []cleanupUnit
		if rs.Library == "movies" {
			if !moviesRead {
				movieUnits, moviesRead = snap.movieUnits(), true
			}
			units = movieUnits
		} else {
			if _, ok := showUnits[rs.ShowScope]; !ok {
				showUnits[rs.ShowScope] = snap.showUnits(rs.ShowScope)
			}
			units = showUnits[rs.ShowScope]
		}
		for _, u := range units {
			if !ruleSetCovers(rs, u) || seen[u.Key] {
				continue
			}
			// The safety checks come before any condition.
			if u.Streamed {
				streamed[u.Key] = true
				continue
			}
			if exempt[u.Exempt] || hasTag(u.Tags, rs.KeepTags) {
				kept[u.Key] = true
				continue
			}
			ok, why := ruleSetMatches(rs, u, now)
			if !ok {
				continue
			}
			seen[u.Key] = true
			out = append(out, cleanupMatch{Unit: u.Key, RuleSetID: rs.ID, RuleSet: rs.Name, Kind: u.Kind, TitleID: titleIDOf(u), Season: u.Season,
				Title: u.Title, Reason: why, Action: rs.Action, unit: u, rs: rs})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, len(streamed), len(kept), nil
}

// titleIDOf is the movie or series a unit belongs to.
func titleIDOf(u cleanupUnit) int64 {
	if u.Kind == "movie" {
		return u.ID
	}
	return u.SeriesID
}

// planCleanup works out what a run at now would do with the given rule sets
// and what is on Leaving soon already. Nothing is changed.
func (s *Server) planCleanup(sets []cleanupRuleSet, leaving []leavingEntry, now time.Time) (cleanupPlan, error) {
	opts := s.cleanupOptions()
	plan := cleanupPlan{NewLeaving: []cleanupMatch{}, Due: []cleanupMatch{}, Waiting: []cleanupMatch{}, Dropped: []leavingEntry{}, Limit: opts.MaxPerRun}
	matches, streamed, kept, err := s.matchRuleSets(sets, now)
	if err != nil {
		return plan, err
	}
	plan.Streamed, plan.Kept = streamed, kept
	return arrangePlan(plan, matches, leaving, now), nil
}

// arrangePlan sorts the matches against Leaving soon: new ones go on it,
// ones whose date has come are due (oldest date first, up to the limit), and
// entries that no longer match come off.
func arrangePlan(plan cleanupPlan, matches []cleanupMatch, leaving []leavingEntry, now time.Time) cleanupPlan {
	on := map[string]leavingEntry{}
	for _, e := range leaving {
		on[e.Unit] = e
	}
	matched := map[string]bool{}
	for _, m := range matches {
		matched[m.Unit] = true
		e, listed := on[m.Unit]
		if !listed {
			m.ActMs = now.Add(days(float64(m.rs.GraceDays))).UnixMilli()
			plan.NewLeaving = append(plan.NewLeaving, m)
			continue
		}
		m.ActMs = e.Act
		if e.Act <= now.UnixMilli() {
			plan.Due = append(plan.Due, m)
		} else {
			plan.Waiting = append(plan.Waiting, m)
		}
	}
	for _, e := range leaving {
		if !matched[e.Unit] {
			plan.Dropped = append(plan.Dropped, e)
		}
	}
	sort.SliceStable(plan.Due, func(i, j int) bool {
		if plan.Due[i].ActMs != plan.Due[j].ActMs {
			return plan.Due[i].ActMs < plan.Due[j].ActMs
		}
		return plan.Due[i].Title < plan.Due[j].Title
	})
	if plan.Limit > 0 && len(plan.Due) > plan.Limit {
		plan.More = len(plan.Due) - plan.Limit
		plan.Due = plan.Due[:plan.Limit]
	}
	return plan
}

// watchedStale says why the watch data can't be trusted for acting ("" when
// it can).
func (s *Server) watchedStale(now time.Time) string {
	last, _ := time.Parse(time.RFC3339, s.watchedStatus().LastFullSync)
	if now.Sub(last) > watchedFreshFor {
		return "What's been watched hasn't been read from all your media servers in the last two days, so the clean-up rules did nothing. Press Read now first, and check every server answers."
	}
	return ""
}

type cleanupRunResult struct {
	Added   []cleanupMatch `json:"added"`   // put on Leaving soon
	Acted   []cleanupMatch `json:"acted"`   // done
	Failed  []cleanupMatch `json:"failed"`  // couldn't be done
	Dropped []leavingEntry `json:"dropped"` // came off Leaving soon
	More    int            `json:"more"`
}

// runCleanupRuleSets runs the switched-on rule sets once: new matches go on
// Leaving soon, due ones are acted on, ones that no longer match come off.
func (s *Server) runCleanupRuleSets(now time.Time, manual bool) (cleanupRunResult, error) {
	res := cleanupRunResult{Added: []cleanupMatch{}, Acted: []cleanupMatch{}, Failed: []cleanupMatch{}, Dropped: []leavingEntry{}}
	if why := s.watchedStale(now); why != "" {
		s.noteCleanupRun(now, why)
		return res, errors.New(why)
	}
	cleanupRulesMu.Lock()
	defer cleanupRulesMu.Unlock()
	all, err := s.listRuleSets()
	if err != nil {
		return res, err
	}
	var sets []cleanupRuleSet
	for _, rs := range all {
		if rs.Enabled {
			sets = append(sets, rs)
		}
	}
	leaving, err := s.listLeaving()
	if err != nil {
		return res, err
	}
	plan, err := s.planCleanup(sets, leaving, now)
	if err != nil {
		return res, err
	}
	for _, e := range plan.Dropped {
		if _, err := s.db.Exec(`DELETE FROM cleanup_leaving WHERE unit = ?`, e.Unit); err == nil {
			res.Dropped = append(res.Dropped, e)
			_ = s.QueueRepo.LogActivity(0, "cleanup", fmt.Sprintf("%s is no longer leaving soon: it doesn't match the clean-up rules any more", e.Title))
		}
	}
	for _, m := range plan.NewLeaving {
		if _, err := s.db.Exec(`INSERT OR REPLACE INTO cleanup_leaving (unit, rule_set_id, kind, title_id, season, title, reason, first_ms, act_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.Unit, m.RuleSetID, m.Kind, m.TitleID, m.Season, m.Title, m.Reason, now.UnixMilli(), m.ActMs); err != nil {
			return res, fmt.Errorf("add to leaving soon: %w", err)
		}
		res.Added = append(res.Added, m)
		_ = s.QueueRepo.LogActivity(0, "cleanup", fmt.Sprintf("%s is leaving soon, on %s (%s, rule set %q)", m.Title, time.UnixMilli(m.ActMs).Format("2 Jan 2006"), m.Reason, m.RuleSet))
	}
	for _, m := range plan.Due {
		if err := s.actOnMatch(m); err != nil {
			slog.Warn("cleanup rules: couldn't act", "title", m.Title, "err", err)
			res.Failed = append(res.Failed, m)
			_ = s.QueueRepo.LogActivity(0, "failed", fmt.Sprintf("Clean-up couldn't remove %s: %v", m.Title, err))
			continue
		}
		_, _ = s.db.Exec(`DELETE FROM cleanup_leaving WHERE unit = ?`, m.Unit)
		res.Acted = append(res.Acted, m)
		var movieID int64
		if m.Kind == "movie" {
			movieID = m.TitleID
		}
		_ = s.QueueRepo.LogActivity(movieID, "removed", fmt.Sprintf("Clean-up %s %s (%s, rule set %q)", actionWords(m.Action), m.Title, m.Reason, m.RuleSet))
	}
	res.More = plan.More
	s.noteCleanupRun(now, "")
	if len(res.Added)+len(res.Acted)+len(res.Dropped) > 0 {
		slog.Info("cleanup rules: ran", "leaving", len(res.Added), "acted", len(res.Acted), "dropped", len(res.Dropped), "manual", manual)
	}
	s.syncLeavingCollections()
	return res, nil
}

func actionWords(action string) string {
	switch action {
	case actUnmonitor:
		return "stopped looking for"
	case actDelete:
		return "deleted the files of (it will be downloaded again)"
	}
	return "deleted the files of, and stopped looking for,"
}

func (s *Server) noteCleanupRun(now time.Time, problem string) {
	o := s.cleanupOptions()
	o.LastRun = now.UTC().Format(time.RFC3339)
	o.LastError = problem
	_ = s.saveCleanupOptions(o)
}

// actOnMatch does what the rule set says to one unit. Every file is checked
// again first: a streamed file is never deleted, whatever matched.
func (s *Server) actOnMatch(m cleanupMatch) error {
	u := m.unit
	for _, f := range u.Files {
		if streamedFile(f) {
			return fmt.Errorf("%s is a streamed file, so it was left alone", f)
		}
	}
	del := m.Action != actUnmonitor
	unmonitor := m.Action != actDelete
	if u.Kind == "movie" {
		mv, err := s.MovieRepo.Get(u.ID)
		if err != nil {
			return err
		}
		if del {
			if mv.FilePath != "" && streamedFile(mv.FilePath) {
				return errors.New("its file is streamed, so it was left alone")
			}
			if _, err := s.removeMovieFiles(mv); err != nil {
				return err
			}
			if err := s.MovieRepo.ClearMovieFile(mv.ID); err != nil {
				return err
			}
		}
		if unmonitor {
			return s.MovieRepo.SetMonitored(mv.ID, false)
		}
		return nil
	}
	sh, err := s.MovieRepo.GetSeries(u.SeriesID)
	if err != nil {
		return err
	}
	if del {
		for _, f := range u.Files {
			// Only the episode files and the files named after them: never
			// the show's folder.
			if _, _, err := s.discardFileWithSidecars(firstNonEmpty(rootContaining(s.tvFolders(), f), s.tvRoot()), f, seriesLabel(sh)); err != nil {
				return err
			}
		}
		for _, id := range u.EpisodeIDs {
			if err := s.MovieRepo.ClearEpisodeFile(id); err != nil {
				return err
			}
		}
	}
	if !unmonitor {
		return nil
	}
	ids := u.EpisodeIDs
	switch u.Kind {
	case "season", "show":
		// The whole season (or show), so its missing episodes aren't looked
		// for either.
		eps, err := s.MovieRepo.ListEpisodes(sh.ID)
		if err != nil {
			return err
		}
		ids = ids[:0:0]
		for _, e := range eps {
			if u.Kind == "show" || e.Season == u.Season {
				ids = append(ids, e.ID)
			}
		}
	}
	for _, id := range ids {
		if err := s.MovieRepo.SetEpisodeMonitored(id, false); err != nil {
			return err
		}
	}
	if u.Kind == "show" {
		return s.MovieRepo.SetSeriesMonitored(sh.ID, false)
	}
	return nil
}

// cleanupRulesJob runs the rule sets once a day (from watchedJob, after a
// fresh read of what's been watched).
func (s *Server) cleanupRulesJob() {
	sets, err := s.listRuleSets()
	if err != nil {
		return
	}
	on := false
	for _, rs := range sets {
		on = on || rs.Enabled
	}
	if !on {
		return
	}
	last, _ := time.Parse(time.RFC3339, s.cleanupOptions().LastRun)
	if time.Since(last) < cleanupRunEvery {
		return
	}
	if _, err := s.runCleanupRuleSets(time.Now(), false); err != nil {
		slog.Info("cleanup rules: run failed", "err", err)
	}
}

// ---- the Leaving soon collection on Plex, Jellyfin and Emby

// syncLeavingCollections puts the titles on Leaving soon into a collection on
// the media servers picked (none unless picked), and takes out the ones that
// came off. In the background.
func (s *Server) syncLeavingCollections() {
	opts := s.cleanupOptions()
	if s.MediaServers == nil || s.mediaClient == nil {
		return
	}
	leaving, err := s.listLeaving()
	if err != nil {
		return
	}
	want := map[string]bool{} // "movie:TMDB" / "tv:TMDB"
	for _, e := range leaving {
		if k := s.leavingTMDBKey(e.Kind, e.TitleID); k != "" {
			want[k] = true
		}
	}
	pick := map[int64]bool{}
	for _, id := range opts.CollectionServers {
		pick[id] = true
	}
	servers, err := s.MediaServers.List()
	if err != nil {
		return
	}
	name := opts.CollectionName
	prevRaw := s.leavingPushed()
	for _, srv := range servers {
		if !srv.Enabled || (srv.Kind != mediaservers.KindPlex && srv.Kind != mediaservers.KindJellyfin && srv.Kind != mediaservers.KindEmby) {
			continue
		}
		srv := srv
		prev := prevRaw[strconv.FormatInt(srv.ID, 10)]
		if !pick[srv.ID] && len(prev) == 0 {
			continue
		}
		target := want
		if !pick[srv.ID] {
			target = map[string]bool{} // switched off: take everything out
		}
		s.background(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			done := map[string]bool{}
			for k := range target {
				if prev[k] {
					done[k] = true
					continue
				}
				kind, id := splitTMDBKey(k)
				if found, err := s.mediaClient.SyncTags(ctx, srv, kind, id, []string{name}, nil); err != nil {
					slog.Warn("cleanup rules: leaving soon collection", "server", srv.Name, "err", err)
				} else if found {
					done[k] = true
				}
			}
			for k := range prev {
				if target[k] {
					continue
				}
				kind, id := splitTMDBKey(k)
				if _, err := s.mediaClient.SyncTags(ctx, srv, kind, id, nil, []string{name}); err != nil {
					slog.Warn("cleanup rules: leaving soon collection", "server", srv.Name, "err", err)
					done[k] = true // try again next time
				}
			}
			s.setLeavingPushed(srv.ID, done)
		})
	}
}

func (s *Server) leavingTMDBKey(kind string, titleID int64) string {
	if kind == "movie" {
		if m, err := s.MovieRepo.Get(titleID); err == nil && m.TMDBID > 0 {
			return fmt.Sprintf("movie:%d", m.TMDBID)
		}
		return ""
	}
	if sh, err := s.MovieRepo.GetSeries(titleID); err == nil && sh.TMDBID > 0 {
		return fmt.Sprintf("tv:%d", sh.TMDBID)
	}
	return ""
}

func splitTMDBKey(k string) (mediaservers.MediaKind, int) {
	kind, rest, _ := strings.Cut(k, ":")
	id, _ := strconv.Atoi(rest)
	if kind == "tv" {
		return mediaservers.MediaTV, id
	}
	return mediaservers.MediaMovie, id
}

var leavingPushedMu sync.Mutex

// leavingPushed is what each server's collection holds, as far as Usherr
// put it there (kept with the options, by server id).
func (s *Server) leavingPushed() map[string]map[string]bool {
	leavingPushedMu.Lock()
	defer leavingPushedMu.Unlock()
	out := map[string]map[string]bool{}
	var raw string
	if err := s.db.QueryRow(`SELECT config FROM cleanup_options WHERE id = 2`).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func (s *Server) setLeavingPushed(server int64, keys map[string]bool) {
	leavingPushedMu.Lock()
	defer leavingPushedMu.Unlock()
	out := map[string]map[string]bool{}
	var raw string
	if err := s.db.QueryRow(`SELECT config FROM cleanup_options WHERE id = 2`).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	out[strconv.FormatInt(server, 10)] = keys
	b, _ := json.Marshal(out)
	_, _ = s.db.Exec(`INSERT INTO cleanup_options (id, config) VALUES (2, ?) ON CONFLICT (id) DO UPDATE SET config = excluded.config`, string(b))
}

// ---- API

func validRuleSet(rs cleanupRuleSet) string {
	if strings.TrimSpace(rs.Name) == "" || len(rs.Name) > 80 {
		return "Give the rule set a name (up to 80 characters)."
	}
	if rs.GraceDays < 0 || rs.GraceDays > 365 {
		return "The grace period must be between 0 and 365 days."
	}
	if len(rs.Conditions) > 20 {
		return "A rule set can have up to 20 conditions."
	}
	for _, c := range rs.Conditions {
		switch c.Type {
		case condWatchedDays, condUnwatchedDays, condIdleDays:
			if c.Number < 1 || c.Number > 3650 {
				return "Days must be between 1 and 3650."
			}
		case condSizeOver:
			if c.Number <= 0 || math.IsInf(c.Number, 0) || math.IsNaN(c.Number) {
				return "Give a size in GB."
			}
		case condYearBefore:
			if c.Number < 1900 || c.Number > 2200 {
				return "Give a year, such as 2000."
			}
		case condHasTag, condLacksTag, condQuality:
			if t := strings.TrimSpace(c.Text); t == "" || len(t) > 40 {
				return "Fill in the tag or quality (up to 40 characters)."
			}
		case condRequestedBy:
			if c.Number < 1 {
				return "Pick the account."
			}
		case condWatchedAll, condNotRequested, condShowEnded:
		default:
			return fmt.Sprintf("%q isn't a condition Usherr knows.", c.Type)
		}
	}
	if rs.Enabled && len(rs.Conditions) == 0 {
		return "Add at least one condition before switching the rule set on."
	}
	return ""
}

func cleanTags(in []string) []string {
	out := []string{}
	for _, t := range in {
		if t = strings.TrimSpace(t); t != "" && len(t) <= 40 {
			out = append(out, t)
		}
	}
	return out
}

func (s *Server) cleanupRulesPayload() (map[string]any, error) {
	sets, err := s.listRuleSets()
	if err != nil {
		return nil, err
	}
	exempt, err := s.listExempt()
	if err != nil {
		return nil, err
	}
	return map[string]any{"ruleSets": sets, "options": s.cleanupOptions(), "exempt": exempt, "movieFolders": s.movieFolders(), "tvFolders": s.tvFolders()}, nil
}

// GET /api/cleanup/rule-sets: the rule sets, the options and the kept titles.
func (s *Server) handleCleanupRuleSets(w http.ResponseWriter, r *http.Request) {
	out, err := s.cleanupRulesPayload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/cleanup/rule-sets and PUT /api/cleanup/rule-sets/{id}.
func (s *Server) handleSaveCleanupRuleSet(w http.ResponseWriter, r *http.Request) {
	var rs cleanupRuleSet
	if err := decodeJSON(r, &rs); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	rs.ID = 0
	if v := r.PathValue("id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "No such rule set.")
			return
		}
		rs.ID = id
	} else {
		if sets, err := s.listRuleSets(); err == nil && len(sets) >= maxRuleSets {
			writeError(w, http.StatusBadRequest, "That's as many rule sets as Usherr keeps.")
			return
		}
		if rs.GraceDays == 0 {
			rs.GraceDays = defaultGraceDays
		}
	}
	rs.Name = strings.TrimSpace(rs.Name)
	rs.Root = strings.TrimSpace(rs.Root)
	rs.KeepTags = cleanTags(rs.KeepTags)
	if msg := validRuleSet(rs); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if rs.Root != "" {
		folders := s.movieFolders()
		if rs.Library == "tv" {
			folders = s.tvFolders()
		}
		if chosenFolder(rs.Root, folders, "") == "" {
			writeError(w, http.StatusBadRequest, "That folder isn't one of your library folders.")
			return
		}
	}
	saved, err := s.saveRuleSet(rs)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "No such rule set.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The rule set couldn't be saved.")
		return
	}
	if !saved.Enabled {
		// Off: what it put on Leaving soon comes off.
		_, _ = s.db.Exec(`DELETE FROM cleanup_leaving WHERE rule_set_id = ?`, saved.ID)
		s.syncLeavingCollections()
	}
	if saved.Enabled && !s.watchedSyncOn() {
		_ = setBool(s.Settings, settings.KeyWatchedSync, true) // the rules need the watch data
	}
	writeJSON(w, http.StatusOK, saved)
}

// DELETE /api/cleanup/rule-sets/{id}: the rule set goes, and what it put on
// Leaving soon comes off.
func (s *Server) handleDeleteCleanupRuleSet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "No such rule set.")
		return
	}
	if _, err := s.db.Exec(`DELETE FROM cleanup_rule_sets WHERE id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.db.Exec(`DELETE FROM cleanup_leaving WHERE rule_set_id = ?`, id)
	s.syncLeavingCollections()
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/cleanup/options {maxPerRun, membersKeep, collectionServers, collectionName}
func (s *Server) handlePutCleanupOptions(w http.ResponseWriter, r *http.Request) {
	var req cleanupOptions
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if req.MaxPerRun < 1 || req.MaxPerRun > 500 {
		writeError(w, http.StatusBadRequest, "The most per run must be between 1 and 500.")
		return
	}
	if len(strings.TrimSpace(req.CollectionName)) > 60 {
		writeError(w, http.StatusBadRequest, "Keep the collection name under 60 characters.")
		return
	}
	o := s.cleanupOptions()
	o.MaxPerRun, o.MembersKeep, o.CollectionServers, o.CollectionName = req.MaxPerRun, req.MembersKeep, req.CollectionServers, strings.TrimSpace(req.CollectionName)
	o.fill()
	if err := s.saveCleanupOptions(o); err != nil {
		writeError(w, http.StatusInternalServerError, "The options couldn't be saved.")
		return
	}
	s.syncLeavingCollections()
	writeJSON(w, http.StatusOK, o)
}

// GET /api/cleanup/preview[?ruleSet=ID]: what the next run would do (the
// switched-on rule sets), or what one rule set would do on its own, on or
// off. Nothing is changed.
func (s *Server) handleCleanupRulesPreview(w http.ResponseWriter, r *http.Request) {
	all, err := s.listRuleSets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var sets []cleanupRuleSet
	one, _ := strconv.ParseInt(r.URL.Query().Get("ruleSet"), 10, 64)
	for _, rs := range all {
		if (one > 0 && rs.ID == one) || (one == 0 && rs.Enabled) {
			sets = append(sets, rs)
		}
	}
	if one > 0 && len(sets) == 0 {
		writeError(w, http.StatusNotFound, "No such rule set.")
		return
	}
	leaving, err := s.listLeaving()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if one > 0 {
		// One rule set on its own: only its own Leaving soon entries count.
		mine := leaving[:0:0]
		for _, e := range leaving {
			if e.RuleSetID == one {
				mine = append(mine, e)
			}
		}
		leaving = mine
	}
	now := time.Now()
	plan, err := s.planCleanup(sets, leaving, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan.Stale = s.watchedStale(now)
	writeJSON(w, http.StatusOK, plan)
}

// POST /api/cleanup/run: run the switched-on rule sets once, now. The grace
// period still applies: this only acts on what has been leaving long enough.
func (s *Server) handleCleanupRulesRun(w http.ResponseWriter, r *http.Request) {
	res, err := s.runCleanupRuleSets(time.Now(), true)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// exemptTarget names the movie or show a request is about.
func (s *Server) exemptTarget(kind string, id int64) (unit, title string, err error) {
	switch kind {
	case "movie":
		m, err := s.MovieRepo.Get(id)
		if err != nil {
			return "", "", errors.New("That movie isn't in your library.")
		}
		return fmt.Sprintf("movie:%d", m.ID), movieLabel(m), nil
	case "show", "season", "episode", "series", "tv":
		sh, err := s.MovieRepo.GetSeries(id)
		if err != nil {
			return "", "", errors.New("That show isn't in your library.")
		}
		return fmt.Sprintf("show:%d", sh.ID), seriesLabel(sh), nil
	}
	return "", "", errors.New("That title couldn't be found.")
}

func (s *Server) keepTitle(r *http.Request, kind string, id int64) (exemptEntry, error) {
	unit, title, err := s.exemptTarget(kind, id)
	if err != nil {
		return exemptEntry{}, err
	}
	e := exemptEntry{Unit: unit, Kind: strings.SplitN(unit, ":", 2)[0], TitleID: id, Title: title, At: time.Now().UnixMilli()}
	if u := auth.UserFromContext(r.Context()); u != nil {
		e.ByUser = u.ID
		e.ByName = strings.TrimSpace(u.FirstName + " " + u.LastName)
		if e.ByName == "" {
			e.ByName = u.Username
		}
	}
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO cleanup_exempt (unit, kind, title_id, title, by_user, by_name, at_ms) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Unit, e.Kind, e.TitleID, e.Title, e.ByUser, e.ByName, e.At); err != nil {
		return e, err
	}
	// It comes off Leaving soon: the movie, or every part of the show.
	if e.Kind == "movie" {
		_, _ = s.db.Exec(`DELETE FROM cleanup_leaving WHERE kind = 'movie' AND title_id = ?`, id)
	} else {
		_, _ = s.db.Exec(`DELETE FROM cleanup_leaving WHERE kind <> 'movie' AND title_id = ?`, id)
	}
	who := e.ByName
	if who == "" {
		who = "Someone"
	}
	_ = s.QueueRepo.LogActivity(0, "cleanup", fmt.Sprintf("%s kept %s: the clean-up rules won't touch it", who, e.Title))
	s.syncLeavingCollections()
	return e, nil
}

type keepRequest struct {
	Kind string `json:"kind"` // movie or show
	ID   int64  `json:"id"`   // movies.id or series.id
}

// POST /api/cleanup/exemptions {kind, id}: an administrator keeps a title.
func (s *Server) handleAddCleanupExemption(w http.ResponseWriter, r *http.Request) {
	var req keepRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	e, err := s.keepTitle(r, req.Kind, req.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// DELETE /api/cleanup/exemptions/{unit}: the title can be cleaned up again.
func (s *Server) handleDeleteCleanupExemption(w http.ResponseWriter, r *http.Request) {
	unit := r.PathValue("unit")
	var title string
	_ = s.db.QueryRow(`SELECT title FROM cleanup_exempt WHERE unit = ?`, unit).Scan(&title)
	if _, err := s.db.Exec(`DELETE FROM cleanup_exempt WHERE unit = ?`, unit); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if title != "" {
		_ = s.QueueRepo.LogActivity(0, "cleanup", fmt.Sprintf("%s is no longer kept: the clean-up rules apply to it again", title))
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/leaving-soon: what the clean-up rules will act on, and when. For
// everyone.
func (s *Server) handleLeavingSoon(w http.ResponseWriter, r *http.Request) {
	list, err := s.listLeaving()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type item struct {
		leavingEntry
		TMDBID int    `json:"tmdbId,omitempty"`
		Poster string `json:"poster,omitempty"`
	}
	out := make([]item, 0, len(list))
	for _, e := range list {
		it := item{leavingEntry: e}
		if e.Kind == "movie" {
			if m, err := s.MovieRepo.Get(e.TitleID); err == nil {
				it.TMDBID, it.Poster = m.TMDBID, m.PosterPath
			}
		} else if sh, err := s.MovieRepo.GetSeries(e.TitleID); err == nil {
			it.TMDBID, it.Poster = sh.TMDBID, sh.PosterPath
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "canKeep": isAdminRequest(r) || s.cleanupOptions().MembersKeep})
}

// POST /api/leaving-soon/keep {kind, id}: "Keep it". Members may when an
// administrator allows it; who pressed it is logged.
func (s *Server) handleLeavingSoonKeep(w http.ResponseWriter, r *http.Request) {
	if !isAdminRequest(r) && !s.cleanupOptions().MembersKeep {
		writeError(w, http.StatusConflict, "Only an administrator can keep titles here.")
		return
	}
	var req keepRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	// Only what is on Leaving soon can be kept this way.
	var n int
	if req.Kind == "movie" {
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM cleanup_leaving WHERE kind = 'movie' AND title_id = ?`, req.ID).Scan(&n)
	} else {
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM cleanup_leaving WHERE kind <> 'movie' AND title_id = ?`, req.ID).Scan(&n)
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "That title isn't leaving soon.")
		return
	}
	e, err := s.keepTitle(r, req.Kind, req.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}
