package api

// Usherr: Real TV. Rebuild a real network's day as Live TV channels: the
// guide for a zip code and a day (from the Gracenote listings that TV-guide
// sites use), each program matched to its exact episode, from the library
// when it's there or streamed, and the AI choosing something in the spirit
// of the rest (news, sports): the same network's shows first, the library
// over streaming. Streams can be checked first; that searches the indexers
// once per episode, so it's a choice.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/internal/tv"
)

// replaySlot is one program of the real day and what Usherr plays for it.
type replaySlot struct {
	tv.FixedItem
	Title      string `json:"title"` // what aired
	EpTitle    string `json:"episodeTitle,omitempty"`
	Kind       string `json:"kind"` // news, sports, show, movie
	New        bool   `json:"new,omitempty"`
	Plays      string `json:"plays,omitempty"`      // what Usherr plays, in words
	From       string `json:"from,omitempty"`       // library, stream
	Checked    string `json:"checked,omitempty"`    // "found", "missing" when streams were checked
	Why        string `json:"why,omitempty"`        // the AI's reason for a stand-in
	BackupName string `json:"backupName,omitempty"` // what plays if the exact show can't be
	seasonNum  int
	epNum      int
	airDate    string
}

type replayNetwork struct {
	GuideID  string       `json:"guideId,omitempty"` // the station in the listings
	Network  string       `json:"network"`
	CallSign string       `json:"callSign"`
	Number   string       `json:"number"`
	Slots    []replaySlot `json:"slots"`
}

type replayJob struct {
	mu       sync.Mutex
	Step     string          `json:"step"`
	Done     bool            `json:"done"`
	Error    string          `json:"error,omitempty"`
	Date     string          `json:"date"`
	Zip      string          `json:"zip"`
	Lineup   string          `json:"lineup,omitempty"`
	Networks []replayNetwork `json:"networks,omitempty"`
	Searches int             `json:"searches"` // stream checks made
}

var replayJobs sync.Map // id -> *replayJob

func (j *replayJob) step(s string) {
	j.mu.Lock()
	j.Step = s
	j.mu.Unlock()
}

// POST /api/tv/replay {zip, date, networks, check}: start building; GET
// /api/tv/replay/{id}: how far it got, and the plan when done.
func (s *Server) handleTVReplay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Zip      string   `json:"zip"`
		Date     string   `json:"date"`
		Networks []string `json:"networks"` // the main networks, by name
		Lineup   string   `json:"lineup"`   // the provider's lineup id; "" is over the air
		Channels []string `json:"channels"` // channels of the lineup, by id (/api/tv/replay/lineup)
		Check    bool     `json:"check"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	req.Zip = strings.TrimSpace(req.Zip)
	if len(req.Zip) != 5 || strings.Trim(req.Zip, "0123456789") != "" {
		writeError(w, http.StatusBadRequest, "Give a five-digit US zip code.")
		return
	}
	day, err := time.ParseInLocation("2006-01-02", req.Date, time.Local)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Give the day as YYYY-MM-DD.")
		return
	}
	if len(req.Channels) > 0 {
		if len(req.Channels) > maxReplayChannels {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Pick up to %d channels at a time.", maxReplayChannels))
			return
		}
	} else if len(req.Networks) == 0 || len(req.Networks) > 8 {
		writeError(w, http.StatusBadRequest, "Pick one to eight networks.")
		return
	}
	if _, ok := s.aiConfig(); !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	id := randomID("rtv_")
	job := &replayJob{Step: "Getting the guide", Date: req.Date, Zip: req.Zip, Lineup: req.Lineup}
	replayJobs.Store(id, job)
	user := auth.UserFromContext(r.Context())
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
		defer cancel()
		err := s.buildReplay(ctx, job, user, req.Zip, day, req.Lineup, req.Channels, req.Networks, req.Check)
		job.mu.Lock()
		job.Done = true
		if err != nil {
			job.Error = err.Error()
		}
		job.mu.Unlock()
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"id": id})
}

func (s *Server) handleTVReplayStatus(w http.ResponseWriter, r *http.Request) {
	v, ok := replayJobs.Load(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "That build isn't here any more. Start it again.")
		return
	}
	job := v.(*replayJob)
	job.mu.Lock()
	defer job.mu.Unlock()
	writeJSON(w, http.StatusOK, job)
}

// POST /api/tv/replay/create {networks: [{network, callSign, number, slots}], folder}:
// a channel for each network.
func (s *Server) handleTVReplayCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Date     string          `json:"date"`
		Zip      string          `json:"zip"`
		Lineup   string          `json:"lineup"`
		Folder   string          `json:"folder"`
		Networks []replayNetwork `json:"networks"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.Networks) == 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	repo := s.tvRepo()
	existing, _ := repo.Channels()
	taken := map[int]bool{}
	for _, c := range existing {
		taken[c.Number] = true
	}
	var made []tv.Channel
	for _, n := range req.Networks {
		rules := tv.Rules{Order: "rotate", FixedFrom: strings.TrimSpace(fmt.Sprintf("%s %s, %s (%s)", n.Network, n.CallSign, req.Date, req.Zip)), Look: "hd"}
		rules.Filler.Folder = strings.TrimSpace(req.Folder)
		if n.GuideID != "" {
			rules.RealTV = &tv.RealTVSource{Zip: req.Zip, Lineup: req.Lineup, GuideID: n.GuideID, Network: n.Network, CallSign: n.CallSign, Daily: true}
		}
		for _, sl := range n.Slots {
			rules.Fixed = append(rules.Fixed, sl.FixedItem)
		}
		if len(rules.Fixed) == 0 {
			continue
		}
		num := channelNum(n.Number)
		for num <= 0 || taken[num] {
			num += 200
		}
		taken[num] = true
		name := n.Network
		if n.CallSign != "" && !strings.EqualFold(n.CallSign, n.Network) {
			name = fmt.Sprintf("%s · %s", n.Network, n.CallSign)
		}
		saved, err := repo.Save(tv.Channel{Kind: "library", Enabled: true, Number: num, Name: name, Rules: rules})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		made = append(made, saved)
		go s.rebuildTVChannel(saved.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": made})
}

// ---- the guide ----

type gnEvent struct {
	StartTime string   `json:"startTime"`
	EndTime   string   `json:"endTime"`
	Duration  string   `json:"duration"`
	Filter    []string `json:"filter"`
	Flag      []string `json:"flag"`
	Program   struct {
		Title        string `json:"title"`
		TmsID        string `json:"tmsId"`
		Season       string `json:"season"`
		Episode      string `json:"episode"`
		EpisodeTitle string `json:"episodeTitle"`
		ReleaseYear  any    `json:"releaseYear"`
	} `json:"program"`
}

type gnChannel struct {
	CallSign      string    `json:"callSign"`
	AffiliateName string    `json:"affiliateName"`
	ChannelID     string    `json:"channelId"`
	ChannelNo     string    `json:"channelNo"`
	Events        []gnEvent `json:"events"`
	// What the channel mostly shows (filter-news, filter-sports...).
	StationFilters []string `json:"stationFilters"`
}

// guideDay fetches a day's listings of a lineup (over the air, or a cable or
// satellite provider) for a zip code, six hours at a time.
func guideDay(ctx context.Context, zip string, day time.Time, lp gnProvider) ([]gnChannel, error) {
	byID := map[string]*gnChannel{}
	var order []string
	for h := 0; h < 24; h += 6 {
		at := day.Add(time.Duration(h) * time.Hour)
		q := url.Values{"lineupId": {lp.ID}, "timespan": {"6"}, "headendId": {lp.HeadendID}, "country": {"USA"},
			"timezone": {""}, "device": {lp.Device}, "postalCode": {zip}, "isOverride": {"true"}, "time": {strconv.FormatInt(at.Unix(), 10)},
			"pref": {"16,128"}, "userId": {"-"}, "aid": {"orbebb"}, "languagecode": {"en-us"}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, gracenoteBase+"/api/grid?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", gnUserAgent)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("the TV listings didn't answer: %w", err)
		}
		var page struct {
			Channels []gnChannel `json:"channels"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&page)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("the TV listings couldn't be read (%d)", resp.StatusCode)
		}
		for _, c := range page.Channels {
			have, ok := byID[c.ChannelID]
			if !ok {
				cc := c
				cc.Events = nil
				byID[c.ChannelID] = &cc
				order = append(order, c.ChannelID)
				have = &cc
			}
			have.Events = append(have.Events, c.Events...)
		}
		if !sleepCtx(ctx, time.Second) {
			return nil, ctx.Err()
		}
	}
	out := make([]gnChannel, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// networkNames are how the listings name each network's affiliates.
var networkNames = map[string][]string{
	"NBC": {"NATIONAL BROADCASTING", "NBC"},
	"ABC": {"AMERICAN BROADCASTING", "ABC"},
	"CBS": {"CBS", "COLUMBIA BROADCASTING"},
	"FOX": {"FOX"},
	"CW":  {"THE CW", "CW TELEVISION"},
	"PBS": {"PUBLIC BROADCASTING", "PBS"},
}

// stationFor is a network's main local station: its affiliate on a ".1"
// with the lowest number (the market's own, not a neighbouring market's).
func stationFor(chans []gnChannel, network string) (gnChannel, bool) {
	names := networkNames[strings.ToUpper(network)]
	if len(names) == 0 {
		names = []string{strings.ToUpper(network)}
	}
	var best gnChannel
	bestNo := 0
	found := false
	for _, c := range chans {
		aff := strings.ToUpper(c.AffiliateName)
		hit := false
		for _, n := range names {
			if strings.Contains(aff, n) {
				hit = true
			}
		}
		if !hit || !strings.HasSuffix(c.ChannelNo, ".1") || len(c.Events) == 0 {
			continue
		}
		no, _ := strconv.Atoi(strings.TrimSuffix(c.ChannelNo, ".1"))
		if !found || no < bestNo {
			best, bestNo, found = c, no, true
		}
	}
	return best, found
}

// ---- building ----

// maxReplayChannels is how many channels one build makes.
const maxReplayChannels = 40

func (s *Server) buildReplay(ctx context.Context, job *replayJob, user *auth.User, zip string, day time.Time, lineup string, channelIDs, networks []string, check bool) error {
	lp, err := providerByID(ctx, zip, lineup)
	if err != nil {
		return err
	}
	chans, err := guideDayCached(ctx, zip, day, lp)
	if err != nil {
		return err
	}
	end := day.AddDate(0, 0, 1)
	// The stations to build: the ones ticked, or the main networks by name.
	type pick struct {
		st      gnChannel
		network string
		call    string
	}
	var picks []pick
	if len(channelIDs) > 0 {
		byID := map[string]gnChannel{}
		for _, c := range chans {
			byID[c.ChannelID] = c
		}
		mainOf := map[string]string{}
		if lp.ID == otaLineupID {
			for n := range networkNames {
				if st, ok := stationFor(chans, n); ok {
					mainOf[st.ChannelID] = n
				}
			}
		}
		for _, id := range channelIDs {
			c, ok := byID[id]
			if !ok || len(c.Events) == 0 {
				continue
			}
			if n := mainOf[id]; n != "" {
				picks = append(picks, pick{c, n, strings.TrimSuffix(strings.TrimSuffix(c.CallSign, "DT"), "TV")})
			} else if lp.ID == otaLineupID {
				picks = append(picks, pick{c, channelLabel(c), strings.TrimSuffix(strings.TrimSuffix(c.CallSign, "DT"), "TV")})
			} else {
				picks = append(picks, pick{c, channelLabel(c), ""})
			}
		}
	} else {
		for _, n := range networks {
			if st, ok := stationFor(chans, n); ok {
				picks = append(picks, pick{st, strings.ToUpper(n), strings.TrimSuffix(strings.TrimSuffix(st.CallSign, "DT"), "TV")})
			}
		}
	}
	var nets []replayNetwork
	for _, pk := range picks {
		st := pk.st
		rn := replayNetwork{GuideID: st.ChannelID, Network: pk.network, CallSign: pk.call, Number: st.ChannelNo}
		seen := map[string]bool{}
		for _, e := range st.Events {
			start, err1 := time.Parse(time.RFC3339, e.StartTime)
			stop, err2 := time.Parse(time.RFC3339, e.EndTime)
			if err1 != nil || err2 != nil || start.Before(day) || !start.Before(end) || seen[e.StartTime] {
				continue
			}
			seen[e.StartTime] = true
			start = start.Local()
			sl := replaySlot{Title: e.Program.Title, EpTitle: e.Program.EpisodeTitle, Kind: "show"}
			sl.At = start.Format("15:04")
			sl.Minutes = int(stop.Sub(start).Minutes())
			sl.Was = e.Program.Title
			sl.seasonNum, _ = strconv.Atoi(e.Program.Season)
			sl.epNum, _ = strconv.Atoi(e.Program.Episode)
			for _, f := range e.Flag {
				if f == "New" {
					sl.New, sl.airDate = true, day.Format("2006-01-02")
				}
			}
			for _, f := range e.Filter {
				switch f {
				case "filter-news":
					sl.Kind = "news"
				case "filter-sports":
					sl.Kind = "sports"
				}
			}
			if strings.HasPrefix(e.Program.TmsID, "MV") {
				sl.Kind = "movie"
			}
			rn.Slots = append(rn.Slots, sl)
		}
		sort.Slice(rn.Slots, func(a, b int) bool { return rn.Slots[a].At < rn.Slots[b].At })
		nets = append(nets, rn)
	}
	if len(nets) == 0 {
		return fmt.Errorf("none of those channels have programs on that day at %s", zip)
	}

	// The exact programs: news and sports only when they have episodes
	// (Dateline, 20/20); streamed episodes only once they've aired.
	job.step("Matching the shows")
	shows := map[string]int64{} // title -> TMDB id
	used := map[string]bool{}   // episodes already placed
	for i := range nets {
		for k := range nets[i].Slots {
			sl := &nets[i].Slots[k]
			if sl.Kind == "sports" || (sl.Kind == "news" && sl.seasonNum == 0) {
				continue
			}
			s.matchReplaySlot(ctx, sl, shows, used)
		}
	}

	// Streams, checked when asked: one indexer search per episode.
	if check {
		total := 0
		for _, n := range nets {
			for _, sl := range n.Slots {
				if sl.Exact && sl.From == "stream" {
					total++
				}
			}
		}
		// Four at a time, each given at most four minutes (StreamNZB, then
		// Usherr's own search), so a slow one can't eat the AI's time.
		var mu sync.Mutex
		done := 0
		sem := make(chan struct{}, 4)
		var wg sync.WaitGroup
		for i := range nets {
			for k := range nets[i].Slots {
				sl := &nets[i].Slots[k]
				if !sl.Exact || sl.From != "stream" {
					continue
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					cctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
					_, err := s.tvStreamSource(cctx, tv.StreamPath(sl.StreamTMDB, sl.Season, sl.Episode))
					cancel()
					mu.Lock()
					done++
					job.step(fmt.Sprintf("Checking streams (%d of %d)", done, total))
					if err != nil {
						sl.Checked = "missing"
						sl.Exact, sl.StreamTMDB, sl.Season, sl.Episode, sl.Plays, sl.From = false, 0, 0, 0, "", ""
					} else {
						sl.Checked = "found"
					}
					mu.Unlock()
					job.mu.Lock()
					job.Searches++
					job.mu.Unlock()
				}()
			}
		}
		wg.Wait()
	}

	// The rest: the AI's stand-ins, a few channels at a time. Channels in one
	// wave don't see each other's picks; later waves avoid every earlier one's.
	const wave = 4
	for from := 0; from < len(nets); from += wave {
		to := min(from+wave, len(nets))
		job.step(fmt.Sprintf("Choosing stand-ins (%d of %d channels)", from+1, len(nets)))
		var wg sync.WaitGroup
		for i := from; i < to; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				actx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
				defer cancel()
				if err := s.replayStandIns(actx, user, &nets[i], nets[:from]); err != nil {
					slog.Info("real tv: stand-ins", "network", nets[i].Network, "err", err)
				}
			}()
		}
		wg.Wait()
	}
	job.mu.Lock()
	job.Networks = nets
	job.Step = "Ready"
	job.mu.Unlock()
	return nil
}

// matchReplaySlot finds the exact episode (or movie) a slot aired: in the
// library when it's there, else on TMDB to stream.
func (s *Server) matchReplaySlot(ctx context.Context, sl *replaySlot, shows map[string]int64, used map[string]bool) {
	tm := s.displayTMDB()
	if tm == nil {
		return
	}
	if sl.Kind == "movie" {
		var id int64
		var title string
		_ = s.db.QueryRowContext(ctx, `SELECT id, title FROM movies WHERE lower(title) = lower(?) AND file_path <> '' LIMIT 1`, sl.Title).Scan(&id, &title)
		if id > 0 {
			sl.MovieID, sl.Exact, sl.From, sl.Plays, sl.Show = id, true, "library", title, title
		}
		return
	}
	key := strings.ToLower(sl.Title)
	tmdbID, ok := shows[key]
	if !ok {
		list, err := tm.SearchTV(ctx, sl.Title)
		if err == nil {
			best := -1.0
			for _, sh := range list {
				if !sameTitleLoose(sl.Title, sh.Name) {
					continue
				}
				if sh.Popularity > best {
					best, tmdbID = sh.Popularity, int64(sh.TMDBID)
				}
			}
		}
		shows[key] = tmdbID
	}
	if tmdbID == 0 {
		return
	}
	// The episode: the one first aired that day (for a new one), else by
	// its number, else by its title.
	seasons := []int{}
	if sl.seasonNum > 0 {
		seasons = append(seasons, sl.seasonNum)
	}
	if full, err := tm.GetShowFull(ctx, int(tmdbID)); err == nil && full.NumberOfSeasons > 0 && full.NumberOfSeasons != sl.seasonNum {
		seasons = append(seasons, full.NumberOfSeasons)
	}
	var all []metadata.EpisodeInfo
	for _, sn := range seasons {
		if eps, err := tm.GetSeason(ctx, int(tmdbID), sn); err == nil {
			all = append(all, eps...)
		}
	}
	// By its title, else the day it first aired (each episode once: a show
	// can air two new ones back to back), else its number.
	var found *metadata.EpisodeInfo
	pick := func(ok func(e metadata.EpisodeInfo) bool) {
		for i := range all {
			k := fmt.Sprintf("%d:%d:%d", tmdbID, all[i].Season, all[i].Episode)
			if found == nil && !used[k] && ok(all[i]) {
				found = &all[i]
				used[k] = true
			}
		}
	}
	if sl.EpTitle != "" {
		// Talk shows are named by their guests: the first one is enough.
		first := strings.ToLower(strings.TrimSpace(strings.Split(sl.EpTitle, ";")[0]))
		pick(func(e metadata.EpisodeInfo) bool {
			return sameTitleLoose(sl.EpTitle, e.Name) || (len(first) > 4 && strings.Contains(strings.ToLower(e.Name), first))
		})
	}
	if sl.airDate != "" {
		pick(func(e metadata.EpisodeInfo) bool { return e.AirDate == sl.airDate })
	}
	if sl.seasonNum > 0 && sl.EpTitle == "" && sl.airDate == "" {
		pick(func(e metadata.EpisodeInfo) bool { return e.Season == sl.seasonNum && e.Episode == sl.epNum })
	}
	if found == nil {
		return
	}
	sl.Show = sl.Title
	sl.Plays = fmt.Sprintf("%s S%02dE%02d", sl.Title, found.Season, found.Episode)
	// In the library?
	var epID, seriesID int64
	_ = s.db.QueryRowContext(ctx, `SELECT e.id, s.id FROM episodes e JOIN series s ON s.id = e.series_id
		WHERE s.tmdb_id = ? AND e.season = ? AND e.episode = ? AND e.file_path <> '' LIMIT 1`, tmdbID, found.Season, found.Episode).Scan(&epID, &seriesID)
	if epID > 0 {
		sl.SeriesID, sl.EpisodeID, sl.Exact, sl.From = seriesID, epID, true, "library"
		return
	}
	sl.StreamTMDB, sl.Season, sl.Episode, sl.Exact, sl.From = tmdbID, found.Season, found.Episode, true, "stream"
}

const replaySystem = `You program a TV channel that replays a real network's day. Some slots can't be replayed (local news, live sports, morning shows). For each of those, choose something to air in its place, in the spirit of the slot: what that network's viewers at that hour would want. Morning news: light, upbeat; daytime: talk, game shows, soaps; early evening: family-friendly; prime time: the network's kind of drama or comedy; late night: comedy. Prefer shows the network itself airs or aired. Keep anything for older viewers after 9 PM.
Reply with a JSON object only: {"fills":[{"slot":0,"seriesId":0,"stream":"","why":"a few words"}]}
- Every slot listed gets a show: never leave one empty. Answer for all of them.
- Never live news or live sports: those can't be played. The program the slot aired only for a news magazine with episodes (an older Dateline NBC or 20/20 in its own slot). In the spirit of a news slot, a news magazine or true-crime series fits (Dateline NBC, 20/20, 48 Hours, Forensic Files); of a morning show, something light and upbeat; of sports, a sports drama or comedy; of a talk show, a sitcom.
- Nothing for adults only (Breaking Bad and the like) before 9 PM.
- seriesId: a show from the library list (its id): use it when it fits the slot as well as anything (it costs nothing to play); the best fit for the slot comes first.
- stream: otherwise the title of a well-known TV series to stream (leave seriesId 0).
- Use variety: don't put the same show in every slot, but a block of the same show (two half hours in a row) is fine.
- A slot marked "backup" airs as it was when it can; give it a backup in the same spirit (a similar show), the library first.`

// replayStandIns asks the AI for something in the spirit of each slot that
// couldn't be matched, a second time for any it skipped; a slot still
// without one carries on the show before it, as a block.
func (s *Server) replayStandIns(ctx context.Context, user *auth.User, n *replayNetwork, others []replayNetwork) error {
	// What was chosen for these slots before is used again; only the rest
	// goes to the AI (and is remembered for next time).
	s.replayRecall(ctx, n)
	defer s.replayRemember(context.WithoutCancel(ctx), n)
	var err error
	for round := 0; round < 2; round++ {
		if err = s.replayStandInsOnce(ctx, user, n, others); err != nil {
			break
		}
	}
	var last *replaySlot
	for k := range n.Slots {
		sl := &n.Slots[k]
		if sl.Exact || sl.Plays != "" {
			if !sl.Exact {
				last = sl
			}
			continue
		}
		if last != nil {
			sl.SeriesID, sl.StreamTMDB, sl.From, sl.Plays, sl.Show, sl.Why = last.SeriesID, last.StreamTMDB, last.From, last.Plays, last.Show, "more of the show before"
		}
	}
	return err
}

func (s *Server) replayStandInsOnce(ctx context.Context, user *auth.User, n *replayNetwork, others []replayNetwork) error {
	cfg, ok := s.aiConfig()
	if !ok {
		return fmt.Errorf("AI isn't set up")
	}
	var open, backups []int
	for k, sl := range n.Slots {
		switch {
		case !sl.Exact && sl.Plays == "":
			open = append(open, k)
		case sl.Exact && sl.From == "stream" && sl.Backup == nil:
			backups = append(backups, k)
		}
	}
	open = append(open, backups...)
	if len(open) == 0 {
		return nil
	}
	if !s.aiAllowed(ctx, user) {
		return fmt.Errorf("no AI answers left today")
	}
	// The library list is the same for every channel of a build: it goes in
	// the cached part of the question, so it's paid for in full once.
	var lib, sb strings.Builder
	lib.WriteString("Shows in the library (id | title | year | genres):\n")
	libIDs := map[int64]string{}
	if rows, err := s.db.QueryContext(ctx, `SELECT id, title, year, COALESCE(genres, '') FROM series WHERE copy_of IS NULL AND EXISTS (SELECT 1 FROM episodes e WHERE e.series_id = series.id AND e.file_path <> '') ORDER BY title LIMIT 600`); err == nil {
		for rows.Next() {
			var id int64
			var title, genres string
			var year int
			if rows.Scan(&id, &title, &year, &genres) == nil {
				libIDs[id] = title
				fmt.Fprintf(&lib, "%d | %s | %d | %s\n", id, title, year, genreList(genres))
			}
		}
		rows.Close()
	}
	fmt.Fprintf(&sb, "\nThe network: %s (%s). Its day, with what airs as it was:\n", n.Network, n.CallSign)
	for _, sl := range n.Slots {
		switch {
		case sl.Exact:
			fmt.Fprintf(&sb, "%s (%d min) %s — airs as it was\n", sl.At, sl.Minutes, sl.Title)
		case sl.Plays != "":
			fmt.Fprintf(&sb, "%s (%d min) %s — %s in its place\n", sl.At, sl.Minutes, sl.Title, sl.Plays)
		}
	}
	// The other channels' stand-ins at the hours being filled here: not the
	// same show at the same time (only those hours, to keep the question short).
	hours := map[string]bool{}
	for _, k := range open {
		if at := n.Slots[k].At; len(at) >= 2 {
			hours[at[:2]] = true
		}
	}
	var taken []string
	for _, o := range others {
		for _, sl := range o.Slots {
			if !sl.Exact && sl.Show != "" && len(sl.At) >= 2 && hours[sl.At[:2]] {
				taken = append(taken, fmt.Sprintf("%s %s", sl.At, sl.Show))
			}
		}
	}
	if len(taken) > 0 {
		sb.WriteString("\nOther channels already show these at these times; don't pick the same show at the same hour, and spread the library around:\n")
		sb.WriteString(strings.Join(taken, "; "))
		sb.WriteString("\n")
	}
	sb.WriteString("\nSlots to fill (slot | time | minutes | what the network aired | kind):\n")
	for _, k := range open {
		sl := n.Slots[k]
		kind := sl.Kind
		if sl.Exact {
			kind = "backup: something like this show, in case it can't be played"
		}
		fmt.Fprintf(&sb, "%d | %s | %d | %s | %s\n", k, sl.At, sl.Minutes, sl.Title, kind)
	}
	// Room for every slot: each fill is about 85 tokens (the slot, the show, a
	// reason). A day of half hours with a backup for each ran past 4000.
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: replaySystem, Shared: lib.String(), Prompt: sb.String(), JSON: true, MaxTokens: replayTokens(len(open))})
	if err != nil {
		return err
	}
	var ans struct {
		Fills []struct {
			Slot     int    `json:"slot"`
			SeriesID int64  `json:"seriesId"`
			Stream   string `json:"stream"`
			Why      string `json:"why"`
		} `json:"fills"`
	}
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		return fmt.Errorf("the AI's answer couldn't be read")
	}
	tm := s.displayTMDB()
	streamIDs := map[string]int64{}
	for _, f := range ans.Fills {
		if f.Slot < 0 || f.Slot >= len(n.Slots) {
			continue
		}
		sl := &n.Slots[f.Slot]
		// A backup for an exact show: kept aside, played only if it must.
		if sl.Exact {
			if sl.Backup != nil || sl.From != "stream" {
				continue
			}
			if name, ok := libIDs[f.SeriesID]; ok {
				sl.Backup, sl.BackupName = &tv.FixedItem{SeriesID: f.SeriesID, Show: name}, name
			} else if title := strings.TrimSpace(f.Stream); title != "" && tm != nil && !sameTitleLoose(title, sl.Title) {
				if _, sh := matchTitle(ctx, tm, aiTitle{Title: title, Type: "tv"}); sh != nil {
					sl.Backup, sl.BackupName = &tv.FixedItem{StreamTMDB: int64(sh.TMDBID), Show: sh.Name}, sh.Name
				}
			}
			continue
		}
		sl.Why = strings.TrimSpace(f.Why)
		if name, ok := libIDs[f.SeriesID]; ok {
			sl.SeriesID, sl.From, sl.Plays, sl.Show = f.SeriesID, "library", name, name
			continue
		}
		title := strings.TrimSpace(f.Stream)
		// The program itself only when it has episodes (an older Dateline in
		// Dateline's slot); a news programme can't be played.
		if title == "" || tm == nil || (sameTitleLoose(title, sl.Title) && sl.seasonNum == 0) {
			continue
		}
		// A show in the library plays from there.
		inLib := int64(0)
		for id, name := range libIDs {
			if sameTitleLoose(title, name) {
				inLib = id
			}
		}
		if inLib > 0 {
			sl.SeriesID, sl.From, sl.Plays, sl.Show = inLib, "library", libIDs[inLib], libIDs[inLib]
			continue
		}
		id, ok := streamIDs[strings.ToLower(title)]
		if !ok {
			if _, sh := matchTitle(ctx, tm, aiTitle{Title: title, Type: "tv"}); sh != nil {
				id = int64(sh.TMDBID)
				title = sh.Name
			}
			streamIDs[strings.ToLower(title)] = id
		}
		if id > 0 {
			sl.StreamTMDB, sl.From, sl.Plays, sl.Show = id, "stream", title, title
		}
	}
	return nil
}

// replayTokens is how long an answer to n slots may be: about 85 tokens a
// slot and some room, at least 1500 and at most 12000.
func replayTokens(n int) int {
	return min(max(300+85*n, 1500), 12000)
}
