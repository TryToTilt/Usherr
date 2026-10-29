package api

// Home fork: the taste engine. Each person's taste, from their thumbs up and
// down and what they watch in Jellyfin (or Emby), as weights on what titles
// are made of: genres, TMDB keywords, cast, director or creators, decade and
// language. A title's match is how well it fits, as a percentage: 50 is an
// ordinary title for this person, the high 90s as good as their favourites.
//
// Votes count most (a thumbs down against), finished films next, shows
// started a little less. Traits a lot of titles share (Drama, English) count
// less than rarer ones (heist, a director). Titles' traits are fetched from
// TMDB once and kept (home_title_traits); a background job fills them in
// for the library, so the first look is quick.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	tasteWatchedMax   = 150 // most recent watched titles counted
	tasteMinSignals   = 3   // fewer liked or watched titles than this: no taste yet
	tasteParallel     = 6
	tasteJobBatch     = 800 // titles a background run fetches at most
	tasteJobEvery     = 3 * time.Hour
	tasteLikedSeeds   = 8  // "For you" asks TMDB about this many liked titles
	tasteForYouSeeds  = 12 // and about recent watching up to this many in all
	tasteForYouScored = 120
	tasteForYouMax    = 60
	tasteForYouFloor  = 60  // a lower match isn't "for you"
	tasteEase         = 0.4 // how fast matches climb with each spread above an ordinary title
)

// tasteWeight is how much each kind of trait says about a taste.
var tasteWeight = map[byte]float64{'g': 0.6, 'k': 1.0, 'c': 0.5, 'm': 1.3, 'y': 0.3, 'l': 0.4}

// bookkeepingKeywords are TMDB keywords about how a film was made or sold,
// not what it's like: no taste in them.
var bookkeepingKeywords = map[string]bool{
	"k:duringcreditsstinger": true, "k:aftercreditsstinger": true, "k:post-credits scene": true, "k:mid-credits scene": true,
	"k:woman director": true, "k:sequel": true, "k:based on novel or book": true, "k:remake": true, "k:prequel": true,
	"k:anime": true, "k:live action remake": true, "k:short film": true,
}

// traitKeys are a title's traits as keys: "g:Comedy", "k:heist",
// "c:<actor>", "m:<director or creator>", "y:1990s", "l:ja".
func traitKeys(t *metadata.TitleTraits) []string {
	if t == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(prefix string, list []string) {
		for _, v := range list {
			if k := prefix + strings.TrimSpace(v); len(k) > 2 && !seen[k] && !bookkeepingKeywords[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	add("g:", t.Genres)
	add("k:", t.Keywords)
	add("c:", t.Cast)
	add("m:", t.Makers)
	if t.Year > 0 {
		add("y:", []string{strconv.Itoa(t.Year/10*10) + "s"})
	}
	add("l:", []string{t.Lang})
	return out
}

// ---- titles' traits, kept ----

var traitStore = struct {
	sync.Mutex
	loaded   bool
	m        map[string]*metadata.TitleTraits // "movie:603"; nil: TMDB has no such title
	inflight map[string]bool
	df       map[string]map[string]int // kind -> trait -> how many titles have it
	n        map[string]int
	dfAt     time.Time
}{m: map[string]*metadata.TitleTraits{}, inflight: map[string]bool{}}

func traitID(kind string, id int) string { return kind + ":" + strconv.Itoa(id) }

func (s *Server) loadTraits(ctx context.Context) {
	traitStore.Lock()
	defer traitStore.Unlock()
	if traitStore.loaded {
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT kind, tmdb_id, data FROM home_title_traits`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var kind, data string
		var id int
		if rows.Scan(&kind, &id, &data) != nil {
			continue
		}
		var t metadata.TitleTraits
		if json.Unmarshal([]byte(data), &t) == nil && t.Name != "" {
			traitStore.m[traitID(kind, id)] = &t
		} else {
			traitStore.m[traitID(kind, id)] = nil
		}
	}
	traitStore.loaded = true
}

// traitsFor is the traits of these titles: those kept at once, the rest
// fetched from TMDB for up to wait. What isn't fetched by then goes on being
// fetched in the background, for next time.
func (s *Server) traitsFor(ctx context.Context, kind string, ids []int, wait time.Duration) map[int]*metadata.TitleTraits {
	s.loadTraits(ctx)
	out := map[int]*metadata.TitleTraits{}
	var missing []int
	traitStore.Lock()
	for _, id := range ids {
		k := traitID(kind, id)
		if t, ok := traitStore.m[k]; ok {
			if t != nil {
				out[id] = t
			}
		} else if id > 0 && !traitStore.inflight[k] {
			traitStore.inflight[k] = true
			missing = append(missing, id)
		}
	}
	traitStore.Unlock()
	if len(missing) == 0 {
		return out
	}
	if !s.TMDB().HasAPIKey() {
		s.releaseInflight(kind, missing)
		return out
	}
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		// Its own context: the fetching outlives a request that stops waiting.
		bg, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		defer close(done)
		sem := make(chan struct{}, tasteParallel)
		var wg sync.WaitGroup
		for _, id := range missing {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if t := s.fetchTraits(bg, kind, id); t != nil {
					mu.Lock()
					out[id] = t
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
	}()
	if wait > 0 {
		select {
		case <-done:
		case <-time.After(wait):
		case <-ctx.Done():
		}
	}
	mu.Lock()
	defer mu.Unlock()
	cp := make(map[int]*metadata.TitleTraits, len(out))
	for k, v := range out {
		cp[k] = v
	}
	return cp
}

func (s *Server) releaseInflight(kind string, ids []int) {
	traitStore.Lock()
	for _, id := range ids {
		delete(traitStore.inflight, traitID(kind, id))
	}
	traitStore.Unlock()
}

// fetchTraits asks TMDB (in English, so a trait has the same name whatever
// the page's language) and keeps the answer. A title TMDB doesn't know is
// kept as {} so it isn't asked about again; after any other failure it is
// asked about again next time.
func (s *Server) fetchTraits(ctx context.Context, kind string, id int) *metadata.TitleTraits {
	defer s.releaseInflight(kind, []int{id})
	t, err := s.TMDB().Traits(ctx, kind, id)
	var se *metadata.StatusError
	if err != nil && !(errors.As(err, &se) && se.Code == http.StatusNotFound) {
		return nil
	}
	data := []byte("{}")
	if err == nil {
		data, _ = json.Marshal(t)
	} else {
		t = nil
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO home_title_traits (kind, tmdb_id, data) VALUES (?, ?, ?)
		ON CONFLICT (kind, tmdb_id) DO UPDATE SET data = excluded.data, fetched_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`, kind, id, string(data)); err != nil {
		slog.Debug("taste: keep traits", "kind", kind, "id", id, "err", err)
	}
	traitStore.Lock()
	traitStore.m[traitID(kind, id)] = t
	traitStore.Unlock()
	return t
}

// idf is how telling a trait is: the rarer among the titles known, the more.
func idf(kind, key string) float64 {
	traitStore.Lock()
	defer traitStore.Unlock()
	if traitStore.df == nil || time.Since(traitStore.dfAt) > 10*time.Minute {
		traitStore.df, traitStore.n = map[string]map[string]int{"movie": {}, "tv": {}}, map[string]int{}
		for k, t := range traitStore.m {
			kd, _, _ := strings.Cut(k, ":")
			if t == nil || traitStore.df[kd] == nil {
				continue
			}
			traitStore.n[kd]++
			for _, f := range traitKeys(t) {
				traitStore.df[kd][f]++
			}
		}
		traitStore.dfAt = time.Now()
	}
	n := float64(traitStore.n[kind])
	return math.Log((n+1)/(float64(traitStore.df[kind][key])+1)) + 1
}

func traitWeight(kind, key string) float64 { return tasteWeight[key[0]] * idf(kind, key) }

// ---- a person's taste ----

type tasteSignal struct {
	id     int
	name   string
	weight float64
	via    string // liked, disliked, watched
}

type tasteProfile struct {
	kind    string
	w       map[string]float64
	signals []tasteSignal
	voted   map[int]int // TMDB id -> 1 or -1
	seen    map[int]bool
	neutral float64 // an ordinary title's score
	spread  float64 // how much titles' scores vary
	ready   bool    // enough to go on
	partial bool    // some titles' traits were still being fetched
	at      time.Time

	fyMu      sync.Mutex // the list and its reasons are asked for at once: worked out once
	forYou    []similarCandidate
	forYouWhy map[int]tasteWhy
}

// tasteWhy is a title's match for a person, and why.
type tasteWhy struct {
	Match   int      `json:"match"`
	Because string   `json:"because,omitempty"` // the liked or watched title it's most like
	Via     string   `json:"via,omitempty"`     // liked or watched
	Tags    []string `json:"tags,omitempty"`    // its traits that fit best
}

var tasteCache = struct {
	sync.Mutex
	m       map[string]*tasteProfile
	version map[int64]int // a vote changes it, so the taste is worked out again
}{m: map[string]*tasteProfile{}, version: map[int64]int{}}

// tasteChanged is called when a person votes.
func tasteChanged(uid int64) {
	tasteCache.Lock()
	tasteCache.version[uid]++
	tasteCache.Unlock()
}

// tasteOf is the taste of a Mediarium account (uid, 0 for none) and the
// media server user it watches as (person, "" for none), for films or shows.
func (s *Server) tasteOf(ctx context.Context, uid int64, person, kind string) *tasteProfile {
	tasteCache.Lock()
	key := strconv.FormatInt(uid, 10) + "|" + person + "|" + kind + "|" + strconv.Itoa(tasteCache.version[uid])
	if p, ok := tasteCache.m[key]; ok {
		age := time.Since(p.at)
		if age < 2*time.Minute || (!p.partial && age < time.Hour) {
			tasteCache.Unlock()
			return p
		}
	}
	tasteCache.Unlock()

	p := &tasteProfile{kind: kind, w: map[string]float64{}, voted: map[int]int{}, seen: map[int]bool{}, at: time.Now()}
	if uid > 0 {
		for _, v := range s.votesOf(ctx, uid, kind, 400) {
			id, err := strconv.Atoi(v.Ref)
			if err != nil || id <= 0 {
				continue
			}
			p.voted[id] = v.Vote
			if v.Vote > 0 {
				p.signals = append(p.signals, tasteSignal{id, v.Title, 2, "liked"})
			} else {
				p.signals = append(p.signals, tasteSignal{id, v.Title, -2.5, "disliked"})
			}
		}
	}
	// Stars in the Diary: four and up count as liked, two and under against.
	starred := map[int]bool{}
	if uid > 0 {
		for id, e := range s.diaryStars(ctx, uid, kind) {
			if _, voted := p.voted[id]; voted {
				continue
			}
			w := math.Max(-2.5, math.Min(2.5, float64(e.Stars-5)*0.55))
			via := "watched"
			if w >= 1 {
				via = "liked"
			} else if w < 0 {
				via = "disliked"
			}
			starred[id] = true
			if w != 0 {
				p.signals = append(p.signals, tasteSignal{id, e.Title, w, via})
			}
		}
	}
	if person != "" {
		if wa, err := s.watchingOf(ctx, person); err == nil {
			recent, sign, base := wa.recentMovies, 1, 1.0
			if kind == "tv" {
				recent, sign, base = wa.recentShows, -1, 0.7
			}
			for id := range wa.seen {
				if id*sign > 0 {
					p.seen[id*sign] = true
				}
			}
			for i, id := range recent {
				if i == tasteWatchedMax {
					break
				}
				if _, voted := p.voted[id]; !voted && !starred[id] {
					p.signals = append(p.signals, tasteSignal{id, wa.names[sign*id], base / (1 + float64(i)/60), "watched"})
				}
			}
		}
	}
	ids := make([]int, len(p.signals))
	for i, sg := range p.signals {
		ids[i] = sg.id
	}
	traits := s.traitsFor(ctx, kind, ids, 12*time.Second)
	p.partial = len(traits) < len(ids)
	p.build(traits, someTraits(kind, 300))
	tasteCache.Lock()
	tasteCache.m[key] = p
	tasteCache.Unlock()
	return p
}

// build works the taste out from its titles' traits, and what an ordinary
// title scores from a sample of others.
func (p *tasteProfile) build(traits map[int]*metadata.TitleTraits, sample []*metadata.TitleTraits) {
	positive := 0
	for _, sg := range p.signals {
		t := traits[sg.id]
		if t == nil {
			continue
		}
		if sg.weight > 0 {
			positive++
		}
		for _, f := range traitKeys(t) {
			p.w[f] += sg.weight * traitWeight(p.kind, f)
		}
	}
	if positive >= tasteMinSignals {
		p.calibrate(traits, sample)
	}
}

// someTraits is a sample of the titles known, for what an ordinary title
// scores.
func someTraits(kind string, n int) []*metadata.TitleTraits {
	traitStore.Lock()
	defer traitStore.Unlock()
	var out []*metadata.TitleTraits
	for k, t := range traitStore.m {
		if t != nil && strings.HasPrefix(k, kind+":") {
			out = append(out, t)
			if len(out) == n {
				break
			}
		}
	}
	return out
}

// raw is how well a title's traits line up with the taste, however many
// traits it has.
func (p *tasteProfile) raw(t *metadata.TitleTraits) float64 {
	keys := traitKeys(t)
	if len(keys) == 0 {
		return 0
	}
	var sum, norm float64
	for _, f := range keys {
		tw := traitWeight(p.kind, f)
		sum += p.w[f] * tw
		norm += tw * tw
	}
	return sum / math.Sqrt(norm)
}

// calibrate sets what an ordinary title scores for this taste, and how much
// titles vary, so matches read the same for everyone: 50 is an ordinary
// title, and it climbs with each spread above that (see match).
func (p *tasteProfile) calibrate(traits map[int]*metadata.TitleTraits, sample []*metadata.TitleTraits) {
	var ord []float64
	for _, t := range sample {
		ord = append(ord, p.raw(t))
	}
	if len(ord) < 20 { // too few titles known yet: what the taste is built from
		for _, sg := range p.signals {
			if t := traits[sg.id]; t != nil {
				ord = append(ord, p.raw(t))
			}
		}
	}
	if len(ord) < 2 {
		return
	}
	sort.Float64s(ord)
	p.neutral = ord[len(ord)/2]
	var mean, sq float64
	for _, v := range ord {
		mean += v
	}
	mean /= float64(len(ord))
	for _, v := range ord {
		sq += (v - mean) * (v - mean)
	}
	p.spread = math.Sqrt(sq / float64(len(ord)))
	p.ready = p.spread > 1e-9
}

// match is a title's fit, 1 to 99 (0: no taste yet, or nothing known).
func (p *tasteProfile) match(t *metadata.TitleTraits) int {
	if p == nil || !p.ready || t == nil {
		return 0
	}
	// Eases off towards 1 and 99: about 69 one spread above an ordinary
	// title, 82 two above, 91 three above.
	m := 50 + 49*math.Tanh(tasteEase*(p.raw(t)-p.neutral)/p.spread)
	return int(math.Round(math.Max(1, math.Min(99, m))))
}

// judge is a title's match and why: its traits that fit the taste best, and
// the liked or watched title it has most in common with.
func (p *tasteProfile) judge(t *metadata.TitleTraits, traits map[int]*metadata.TitleTraits, self int) tasteWhy {
	w := tasteWhy{Match: p.match(t)}
	if w.Match == 0 {
		return w
	}
	keys := traitKeys(t)
	type kv struct {
		k string
		v float64
	}
	var best []kv
	for _, f := range keys {
		if f[0] == 'l' || f[0] == 'y' {
			continue // "fits: 1990s" says little
		}
		if v := p.w[f] * traitWeight(p.kind, f); v > 0 {
			best = append(best, kv{f, v})
		}
	}
	sort.Slice(best, func(i, j int) bool { return best[i].v > best[j].v })
	for _, b := range best[:min(2, len(best))] {
		w.Tags = append(w.Tags, b.k[2:])
	}
	if w.Match < 50 {
		return w
	}
	mine := map[string]bool{}
	for _, f := range keys {
		mine[f] = true
	}
	var top float64
	for _, sg := range p.signals {
		st := traits[sg.id]
		if sg.weight <= 0 || st == nil || sg.id == self || sg.name == "" {
			continue
		}
		var shared float64
		telling := false // more than a genre in common
		for _, f := range traitKeys(st) {
			if mine[f] {
				shared += traitWeight(p.kind, f)
				telling = telling || f[0] == 'k' || f[0] == 'm' || f[0] == 'c'
			}
		}
		if sg.via == "liked" {
			shared *= 1.3
		}
		if telling && shared > top {
			top, w.Because, w.Via = shared, sg.name, sg.via
		}
	}
	return w
}

// signalTraits is the traits of the titles a taste is built from.
func (s *Server) signalTraits(ctx context.Context, p *tasteProfile) map[int]*metadata.TitleTraits {
	ids := make([]int, len(p.signals))
	for i, sg := range p.signals {
		ids[i] = sg.id
	}
	return s.traitsFor(ctx, p.kind, ids, 0)
}

// ---- who's who ----

// personMatch is the media server user named like a Mediarium account ("" for
// none): user name, first name, full name, or the e-mail address before the @.
func personMatch(people []jfPerson, u *auth.User) string {
	if u == nil {
		return ""
	}
	names := []string{u.Username, u.FirstName, strings.TrimSpace(u.FirstName + " " + u.LastName)}
	if at := strings.Index(u.Email, "@"); at > 0 {
		names = append(names, u.Email[:at])
	}
	for _, n := range names {
		for _, p := range people {
			if n != "" && strings.EqualFold(p.Name, n) {
				return p.ID
			}
		}
	}
	return ""
}

// accountFor is the Mediarium account of a media server user (0 for none).
func (s *Server) accountFor(people []jfPerson, person string) int64 {
	list, err := s.Auth.ListAccounts()
	if err != nil {
		return 0
	}
	for i := range list {
		if !list[i].Disabled && personMatch(people, &list[i].User) == person {
			return list[i].ID
		}
	}
	return 0
}

// myTaste is the signed-in person's taste.
func (s *Server) myTaste(ctx context.Context, kind string) *tasteProfile {
	return s.tasteOfUser(ctx, auth.UserFromContext(ctx), kind)
}

// tasteOfUser is an account's taste: its votes, and the watching of the
// media server user named like it.
func (s *Server) tasteOfUser(ctx context.Context, u *auth.User, kind string) *tasteProfile {
	var uid int64
	if u != nil {
		uid = u.ID
	}
	people, _ := s.jfPeople(ctx)
	// Someone else's watching never stands in for yours (personOfUser).
	return s.tasteOf(ctx, uid, personOfUser(people, u), kind)
}

// ---- in the picks: Tonight, Pick for the Room, Roulette ----

// roomTastes is the taste of each person in the room (media server users).
func (s *Server) roomTastes(ctx context.Context, people []string, kind string) []*tasteProfile {
	all, _ := s.jfPeople(ctx)
	var out []*tasteProfile
	for _, p := range people {
		out = append(out, s.tasteOf(ctx, s.accountFor(all, p), p, kind))
	}
	return out
}

// tasteFilter leaves out what anyone in the room gave a thumbs down, and
// gives each title the room's match: the average of everyone's who has a
// taste yet (none when nobody has).
func (s *Server) tasteFilter(ctx context.Context, kind string, list []roomTitle, tastes []*tasteProfile) ([]roomTitle, map[int]int) {
	var keep []roomTitle
	ids := make([]int, 0, len(list))
	for _, t := range list {
		down := false
		for _, p := range tastes {
			down = down || p.voted[t.TMDB] < 0
		}
		if !down {
			keep = append(keep, t)
			ids = append(ids, t.TMDB)
		}
	}
	matches := map[int]int{}
	someone := false
	for _, p := range tastes {
		someone = someone || p.ready
	}
	if !someone {
		return keep, matches
	}
	traits := s.traitsFor(ctx, kind, ids, 3*time.Second)
	for _, t := range keep {
		var sum, n int
		for _, p := range tastes {
			if m := p.match(traits[t.TMDB]); m > 0 {
				sum += m
				n++
			}
		}
		if n > 0 {
			matches[t.TMDB] = sum / n
		}
	}
	return keep, matches
}

// tasteSample is roomSample with the room's taste counted: a title that fits
// comes up more often, one that doesn't hardly ever.
func tasteSample(list []roomTitle, matches map[int]int, n int, r *rand.Rand) []roomTitle {
	return roomSampleWeighted(list, n, r, func(t roomTitle) float64 {
		m, ok := matches[t.TMDB]
		if !ok {
			return 1
		}
		f := float64(m) / 50
		return f * f
	})
}

// ---- For you ----

// forYou is a person's "For you" list: what TMDB relates to what they liked
// and watched, each given a match, the best fits first. Nothing they have
// seen or voted on, nor what the household set aside.
func (s *Server) forYou(ctx context.Context, p *tasteProfile) ([]similarCandidate, map[int]tasteWhy) {
	if p == nil || !p.ready {
		return nil, nil
	}
	p.fyMu.Lock()
	defer p.fyMu.Unlock()
	if p.forYou != nil {
		return p.forYou, p.forYouWhy
	}
	var seeds []int
	for _, sg := range p.signals {
		if sg.via == "liked" && len(seeds) < tasteLikedSeeds {
			seeds = append(seeds, sg.id)
		}
	}
	for _, sg := range p.signals {
		if sg.via == "watched" && len(seeds) < tasteForYouSeeds {
			seeds = append(seeds, sg.id)
		}
	}
	perSeed, err := s.suggestions(ctx, p.kind, seeds)
	if err != nil {
		return nil, nil
	}
	excluded := s.excludedIDs(ctx, p.kind)
	var cands []similarCandidate
	for _, c := range mergeSuggestions(perSeed) {
		_, voted := p.voted[c.ID]
		if c.Year > 0 && c.HasPoster && !p.seen[c.ID] && !excluded[c.ID] && !voted {
			cands = append(cands, c)
		}
	}
	now := time.Now()
	pre := func(c similarCandidate) float64 { return float64(c.Votes) + 0.3*similarScore(c, now) }
	sort.SliceStable(cands, func(i, j int) bool { return pre(cands[i]) > pre(cands[j]) })
	cands = cands[:min(len(cands), tasteForYouScored)]
	ids := make([]int, len(cands))
	for i, c := range cands {
		ids[i] = c.ID
	}
	traits := s.traitsFor(ctx, p.kind, ids, 15*time.Second)
	sig := s.signalTraits(ctx, p)
	why := map[int]tasteWhy{}
	out := []similarCandidate{}
	for _, c := range cands {
		if t := traits[c.ID]; t != nil {
			if w := p.judge(t, sig, c.ID); w.Match >= tasteForYouFloor {
				why[c.ID] = w
				out = append(out, c)
			}
		}
	}
	rank := func(c similarCandidate) float64 { return float64(why[c.ID].Match) + 2*float64(min(c.Votes, 4)) }
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) > rank(out[j]) })
	out = out[:min(len(out), tasteForYouMax)]
	p.forYou, p.forYouWhy = out, why
	if len(traits) < len(ids) {
		tasteCache.Lock()
		p.partial = true // worked out again shortly, with more of them known
		tasteCache.Unlock()
	}
	return out, why
}

// ---- the endpoints ----

// GET /api/home/taste/why?kind=: the "For you" list's match and reason, by
// TMDB id.
func (s *Server) handleTasteWhy(w http.ResponseWriter, r *http.Request) {
	kind, ok := discoverKind(r)
	if !ok {
		writeError(w, http.StatusBadRequest, `kind must be "movie" or "tv"`)
		return
	}
	_, why := s.forYou(r.Context(), s.myTaste(r.Context(), kind))
	if why == nil {
		why = map[int]tasteWhy{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"why": why})
}

type tasteTag struct {
	Name string `json:"name"`
	Type string `json:"type"` // genre, keyword, cast, maker, decade
}

var tagTypes = map[byte]string{'g': "genre", 'k': "keyword", 'c': "cast", 'm': "maker", 'y': "decade", 'l': "language"}

// GET /api/home/taste?kind=: your taste in short: whether there's enough to
// go on, what it's built from, and what you lean towards and away from.
func (s *Server) handleTaste(w http.ResponseWriter, r *http.Request) {
	kind, ok := discoverKind(r)
	if !ok {
		writeError(w, http.StatusBadRequest, `kind must be "movie" or "tv"`)
		return
	}
	p := s.myTaste(r.Context(), kind)
	count := map[string]int{}
	for _, sg := range p.signals {
		count[sg.via]++
	}
	likes, dislikes := p.leanings(12, 8)
	writeJSON(w, http.StatusOK, map[string]any{"ready": p.ready, "liked": count["liked"], "disliked": count["disliked"], "watched": count["watched"], "likes": likes, "dislikes": dislikes})
}

// leanings are the traits counting most for and against.
func (p *tasteProfile) leanings(nFor, nAgainst int) ([]tasteTag, []tasteTag) {
	type kv struct {
		k string
		v float64
	}
	var all []kv
	for k, v := range p.w {
		if k[0] != 'l' {
			all = append(all, kv{k, v})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v || all[i].v == all[j].v && all[i].k < all[j].k })
	likes, dislikes := []tasteTag{}, []tasteTag{}
	for _, x := range all {
		if x.v <= 0 || len(likes) == nFor {
			break
		}
		likes = append(likes, tasteTag{x.k[2:], tagTypes[x.k[0]]})
	}
	for i := len(all) - 1; i >= 0 && all[i].v < 0 && len(dislikes) < nAgainst; i-- {
		dislikes = append(dislikes, tasteTag{all[i].k[2:], tagTypes[all[i].k[0]]})
	}
	return likes, dislikes
}

// tasteLine is a taste for the AI, in a sentence or two ("" when there's
// none yet).
func (p *tasteProfile) tasteLine() string {
	if p == nil || !p.ready {
		return ""
	}
	likes, dislikes := p.leanings(8, 5)
	names := func(l []tasteTag) string {
		out := make([]string, len(l))
		for i, t := range l {
			out[i] = t.Name
		}
		return strings.Join(out, ", ")
	}
	line := "They tend to like: " + names(likes) + "."
	if len(dislikes) > 0 {
		line += " Not keen on: " + names(dislikes) + "."
	}
	return line
}

// POST /api/home/taste/scores {kind, ids}: your match for each title, for
// the Library's "For you", and which of them you've seen (a title you've
// watched is part of your taste, so it always fits). Titles whose traits
// aren't known yet are left out (and fetched, for next time).
func (s *Server) handleTasteScores(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind string `json:"kind"`
		IDs  []int  `json:"ids"`
	}
	if err := decodeJSON(r, &in); err != nil || (in.Kind != "movie" && in.Kind != "tv") || len(in.IDs) > 10000 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	p := s.myTaste(ctx, in.Kind)
	scores, seen := map[int]int{}, []int{}
	if p.ready {
		traits := s.traitsFor(ctx, in.Kind, in.IDs, 4*time.Second)
		for _, id := range in.IDs {
			if m := p.match(traits[id]); m > 0 {
				scores[id] = m
			}
			if p.seen[id] {
				seen = append(seen, id)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": p.ready, "scores": scores, "seen": seen})
}

// GET /api/home/taste/title?kind=&id=: your match for one title, and why.
func (s *Server) handleTasteTitle(w http.ResponseWriter, r *http.Request) {
	kind, ok := discoverKind(r)
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if !ok || err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "That title couldn't be read.")
		return
	}
	ctx := r.Context()
	p := s.myTaste(ctx, kind)
	if !p.ready || p.voted[id] != 0 {
		writeJSON(w, http.StatusOK, tasteWhy{}) // your own vote says more than a match
		return
	}
	t := s.traitsFor(ctx, kind, []int{id}, 8*time.Second)[id]
	writeJSON(w, http.StatusOK, p.judge(t, s.signalTraits(ctx, p), id))
}

// tasteJob fills in the traits of the library's titles and of what people
// voted on, a batch at a time, so tastes and matches are quick to work out.
func (s *Server) tasteJob(ctx context.Context) {
	if !s.TMDB().HasAPIKey() {
		return
	}
	s.loadTraits(ctx)
	budget := tasteJobBatch
	// What the household can watch in the media server: what Tonight, Pick
	// for the Room and Roulette choose from.
	shelf := map[string][]int{}
	people, _ := s.jfPeople(ctx)
	for _, p := range people {
		if sh, err := s.shelfOf(ctx, p.ID); err == nil {
			for _, t := range sh.movies {
				shelf["movie"] = append(shelf["movie"], t.TMDB)
			}
			for _, t := range sh.shows {
				shelf["tv"] = append(shelf["tv"], t.TMDB)
			}
		}
	}
	for _, kind := range []string{"movie", "tv"} {
		ids, _ := s.libraryIDs(ctx, kind)
		ids = append(ids, shelf[kind]...)
		if rows, err := s.db.QueryContext(ctx, `SELECT ref FROM home_votes WHERE kind = ?`, kind); err == nil {
			for rows.Next() {
				var ref string
				if rows.Scan(&ref) == nil {
					if id, err := strconv.Atoi(ref); err == nil {
						ids = append(ids, id)
					}
				}
			}
			rows.Close()
		}
		var missing []int
		traitStore.Lock()
		for _, id := range ids {
			if _, ok := traitStore.m[traitID(kind, id)]; !ok && id > 0 && len(missing) < budget {
				missing = append(missing, id)
			}
		}
		traitStore.Unlock()
		budget -= len(missing)
		if len(missing) > 0 {
			got := s.traitsFor(ctx, kind, missing, 15*time.Minute)
			slog.Info("taste: titles' traits fetched", "kind", kind, "asked", len(missing), "got", len(got))
		}
	}
}
