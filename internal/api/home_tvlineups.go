package api

// Usherr: Real TV's channel choice. The listings that Real TV reads (the
// Gracenote guide) serve every lineup of a zip code: the stations on the air
// (the local network affiliates and their subchannels, MeTV, Cozi, ION...)
// and each cable and satellite provider's channels (ESPN, HGTV, TNT...). A
// person picks a lineup, then ticks the channels they want, instead of six
// fixed buttons.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// gnProvider is one lineup a zip code can watch: over the air, a cable
// company, a satellite service.
type gnProvider struct {
	ID        string `json:"id"` // the lineup id
	Type      string `json:"type"`
	Name      string `json:"name"`
	HeadendID string `json:"headendId"`
	Device    string `json:"device"`
}

const otaLineupID = "USA-lineupId-DEFAULT"

var otaProvider = gnProvider{ID: otaLineupID, Type: "OTA", Name: "Local Over the Air Broadcast", HeadendID: "lineupId", Device: "-"}

// gracenoteBase is where the listings are read from (a variable so a test can
// answer in its place).
var gracenoteBase = "https://tvlistings.gracenote.com"

const gnUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

type providersFound struct {
	list []gnProvider
	at   time.Time
}

var providerCache sync.Map // zip -> providersFound

// gnProviders lists the lineups at a zip code, the stations on the air first.
func gnProviders(ctx context.Context, zip string) ([]gnProvider, error) {
	if v, ok := providerCache.Load(zip); ok && time.Since(v.(providersFound).at) < 6*time.Hour {
		return v.(providersFound).list, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gracenoteBase+"/gapzap_webapi/api/Providers/getPostalCodeProviders/USA/"+url.PathEscape(zip)+"/gapzap/en-us", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", gnUserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the TV listings didn't answer: %w", err)
	}
	defer resp.Body.Close()
	var page struct {
		Providers []struct {
			Type      string `json:"type"`
			Device    string `json:"device"`
			LineupID  string `json:"lineupId"`
			Name      string `json:"name"`
			HeadendID string `json:"headendId"`
		} `json:"Providers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&page); err != nil || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the TV listings couldn't be read (%d)", resp.StatusCode)
	}
	list := []gnProvider{otaProvider}
	for _, p := range page.Providers {
		if p.LineupID == "" || p.LineupID == otaLineupID {
			continue
		}
		list = append(list, gnProvider{ID: p.LineupID, Type: p.Type, Name: p.Name, HeadendID: p.HeadendID, Device: p.Device})
	}
	providerCache.Store(zip, providersFound{list: list, at: time.Now()})
	return list, nil
}

// providerByID finds a lineup at a zip code; "" is over the air.
func providerByID(ctx context.Context, zip, id string) (gnProvider, error) {
	if id == "" || id == otaLineupID {
		return otaProvider, nil
	}
	list, err := gnProviders(ctx, zip)
	if err != nil {
		return gnProvider{}, err
	}
	for _, p := range list {
		if p.ID == id {
			return p, nil
		}
	}
	return gnProvider{}, fmt.Errorf("that lineup isn't available at %s", zip)
}

// ---- a lineup's channels ----

type guideFound struct {
	chans []gnChannel
	at    time.Time
}

var (
	guideMu    sync.Mutex
	guideCache = map[string]guideFound{}
)

// guideDayCached is guideDay, remembered for an hour: picking channels and
// then building them asks for the same day twice.
func guideDayCached(ctx context.Context, zip string, day time.Time, lp gnProvider) ([]gnChannel, error) {
	key := zip + "|" + day.Format("2006-01-02") + "|" + lp.ID
	guideMu.Lock()
	if f, ok := guideCache[key]; ok && time.Since(f.at) < time.Hour {
		guideMu.Unlock()
		return f.chans, nil
	}
	guideMu.Unlock()
	chans, err := guideDay(ctx, zip, day, lp)
	if err != nil {
		return nil, err
	}
	guideMu.Lock()
	guideCache[key] = guideFound{chans: chans, at: time.Now()}
	for k, f := range guideCache {
		if time.Since(f.at) > 3*time.Hour {
			delete(guideCache, k)
		}
	}
	guideMu.Unlock()
	return chans, nil
}

type lineupChannel struct {
	ID       string `json:"id"` // the channel id in the listings
	CallSign string `json:"callSign"`
	Name     string `json:"name"`
	Number   string `json:"number"`
	Category string `json:"category"`          // News, Sports, Movies, Kids & Family, Local, Entertainment
	Network  string `json:"network,omitempty"` // NBC, ABC...: the market's own affiliate, over the air
	Programs int    `json:"programs"`
}

// categoryOf says what kind of channel a listings channel is. The listings'
// own filters are too loose to trust (a local station counts as News for its
// newscast, MTV as Movies), so the channel's name decides first: CNN is News,
// ESPN is Sports, HBO is Movies. The filters only break a tie for channels the
// names don't cover, and then only for News and Sports.
func categoryOf(c gnChannel) string {
	name := strings.ToUpper(strings.TrimSpace(c.CallSign + " " + c.AffiliateName))
	for _, r := range categoryNames {
		for _, w := range r.words {
			if hasWord(name, w) {
				return r.category
			}
		}
	}
	// A station with a network or a market name: a local station.
	if aff := strings.TrimSpace(c.AffiliateName); aff != "" && !strings.EqualFold(aff, "null") {
		return "Local"
	}
	has := map[string]bool{}
	for _, f := range c.StationFilters {
		has[f] = true
	}
	switch {
	case has["filter-sports"] && !has["filter-movie"]:
		return "Sports"
	case has["filter-news"] && !has["filter-movie"] && !has["filter-talk"] && !has["filter-family"]:
		return "News"
	}
	return "Entertainment"
}

// categoryNames: words in a channel's call sign or name that say its kind, in
// the order they're tried.
var categoryNames = []struct {
	category string
	words    []string
}{
	{"News", []string{"CNN", "FNC", "FOXNEWS", "FOX NEWS", "MSNBC", "MSNOW", "CNBC", "HLN", "BLOOMBERG", "BLOOM", "NEWSMAX", "NEWSMXH", "NEWSNATION", "NWSNTN", "NEWSY", "WEATHER", "WEATH", "C-SPAN", "CSPAN", "CSPAN2", "CSPAN3", "BBCA", "BBC WORLD", "FBN", "FOXBUS", "OAN", "REUTERS", "SCRIPPS", "ABC NEWS", "NBC NEWS", "CBS NEWS"}},
	{"Sports", []string{"ESPN*", "FS1", "FS2", "FOXSPORTS", "GOLF", "NFL*", "NBA*", "MLB*", "NHL*", "TENNIS", "SEC", "SECN", "BTN", "ACCN", "CBSSN", "CBSSPORTS", "OUTDOOR", "OLY", "OLYMPIC", "SPEED", "BEINSPORTS", "BEIN", "STADIUM", "RACETRAX", "TRACK", "SPORTSMAN", "SPORTSMN", "WWE", "BIGTEN"}},
	{"Movies", []string{"HBO*", "SHO", "SHOWTIME*", "SHOWTM*", "STARZ*", "ENCORE*", "ENC*", "TCM", "MGM", "MGMHD", "CINEMAX*", "MAX", "MOREMAX", "THRILLERMAX", "ACTIONMAX", "EPIX*", "FMC", "FXM", "FLIX", "MOVIES", "MOVIE", "MPLEX", "MOVIEPLEX", "INDIEPLEX", "RETROPLEX", "SUNDANCE", "IFC", "PARAMOUNT NETWORK", "THIS TV", "GETTV", "CINE", "HALLMARK MOVIES", "HMM", "LMN"}},
	{"Kids & Family", []string{"DISN*", "DISNEY*", "DISJR", "DISXD", "DXD", "NICK*", "NIK*", "TEENNICK", "TOON", "CARTOON", "BOOM", "BOOMERANG", "BABYFIRST", "PBSKIDS", "PBS KIDS", "UNIVERSAL KIDS", "SPROUT", "FAMILY", "FREEFM", "FREEFORM", "FAM", "HALLMARK", "HALL", "UPTV", "UP TV", "DUCKTV", "NOGGIN", "TOONHD", "CBEEBIES", "CTYSTR"}},
}

// hasWord says whether name holds w as a whole word; a word ending in * is
// a start of a word (HBO* matches HBODRMA).
func hasWord(name, w string) bool {
	prefix := strings.HasSuffix(w, "*")
	w = strings.TrimSuffix(w, "*")
	for i := 0; ; {
		j := strings.Index(name[i:], w)
		if j < 0 {
			return false
		}
		a, b := i+j, i+j+len(w)
		before := a == 0 || !isAlnum(name[a-1])
		after := prefix || b == len(name) || !isAlnum(name[b]) || (strings.HasSuffix(name[b:], "HD") && len(name[b:]) == 2) || (strings.HasPrefix(name[b:], "HD") && len(name[b:]) == 2)
		if before && after {
			return true
		}
		i = b
		if i >= len(name) {
			return false
		}
	}
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

// channelLabel is how a channel is named for people: the station's own name
// when the listings give one (ANTENNA TV), else its call sign without the
// "HD" (ESPNHD is ESPN).
func channelLabel(c gnChannel) string {
	if n := strings.TrimSpace(c.AffiliateName); n != "" && !strings.EqualFold(n, "null") {
		return titleCase(n)
	}
	cs := strings.TrimSpace(c.CallSign)
	if len(cs) > 3 && strings.HasSuffix(cs, "HD") {
		cs = strings.TrimSuffix(cs, "HD")
	}
	return cs
}

func titleCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		switch w {
		case "tv", "pbs", "nbc", "abc", "cbs", "cw", "usa", "hd", "bbc", "cnn":
			words[i] = strings.ToUpper(w)
		default:
			if len(w) > 0 {
				words[i] = strings.ToUpper(w[:1]) + w[1:]
			}
		}
	}
	return strings.Join(words, " ")
}

// channelNum is the number a Live TV channel gets for a listings channel: 29
// is 29, a main station 4.1 is 4, and its subchannel 4.2 is 42.
func channelNum(no string) int {
	head, tail, _ := strings.Cut(no, ".")
	n, _ := strconv.Atoi(head)
	if sub, err := strconv.Atoi(tail); err == nil && sub > 1 {
		return n*10 + sub
	}
	return n
}

func numberLess(a, b string) bool {
	fa, _ := strconv.ParseFloat(a, 64)
	fb, _ := strconv.ParseFloat(b, 64)
	if fa != fb {
		return fa < fb
	}
	return a < b
}

// GET /api/tv/replay/providers?zip=: the lineups at a zip code.
func (s *Server) handleTVReplayProviders(w http.ResponseWriter, r *http.Request) {
	zip := strings.TrimSpace(r.URL.Query().Get("zip"))
	if len(zip) != 5 || strings.Trim(zip, "0123456789") != "" {
		writeError(w, http.StatusBadRequest, "Give a five-digit US zip code.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	list, err := gnProviders(ctx, zip)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": list})
}

// GET /api/tv/replay/lineup?zip=&date=&lineup=: the channels of a lineup that
// have programs on that day.
func (s *Server) handleTVReplayLineup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	zip := strings.TrimSpace(q.Get("zip"))
	if len(zip) != 5 || strings.Trim(zip, "0123456789") != "" {
		writeError(w, http.StatusBadRequest, "Give a five-digit US zip code.")
		return
	}
	day, err := time.ParseInLocation("2006-01-02", q.Get("date"), time.Local)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Give the day as YYYY-MM-DD.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	lp, err := providerByID(ctx, zip, q.Get("lineup"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	chans, err := guideDayCached(ctx, zip, day, lp)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// The market's own affiliate of each network, over the air.
	mainOf := map[string]string{}
	if lp.ID == otaLineupID {
		for n := range networkNames {
			if st, ok := stationFor(chans, n); ok {
				mainOf[st.ChannelID] = n
			}
		}
	}
	// A cable lineup lists many channels twice, as standard and as HD: the HD one is kept.
	calls := map[string]bool{}
	for _, c := range chans {
		calls[c.CallSign] = true
	}
	out := make([]lineupChannel, 0, len(chans))
	for _, c := range chans {
		if len(c.Events) == 0 || (lp.ID != otaLineupID && calls[c.CallSign+"HD"]) {
			continue
		}
		name := channelLabel(c)
		if n := mainOf[c.ChannelID]; n != "" {
			name = n
		}
		out = append(out, lineupChannel{ID: c.ChannelID, CallSign: c.CallSign, Name: name, Number: c.ChannelNo, Category: categoryOf(c), Network: mainOf[c.ChannelID], Programs: len(c.Events)})
	}
	sort.SliceStable(out, func(i, j int) bool { return numberLess(out[i].Number, out[j].Number) })
	writeJSON(w, http.StatusOK, map[string]any{"provider": lp, "channels": out})
}
