package api

// Usherr: streaming a game, like a film or an episode. The sports backend
// finds the game's releases (Sportarr with its sport-by-sport matching);
// Usherr's own streaming engine plays the best one that opens, with "Try
// another release" in the player as for anything else.

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// sportRelease is one release of a game.
type sportRelease struct {
	Title    string
	URL      string // the .nzb link
	Indexer  string
	Size     int64
	Quality  string
	Part     string // a fight card's part ("Prelims"), when it is one
	Language string // the backend's reading of the audio language, when it has one
}

var sportFound = struct {
	sync.Mutex
	m        map[string]sportFoundList
	inFlight map[string]chan struct{} // a search under way: others wait for it
}{m: map[string]sportFoundList{}, inFlight: map[string]chan struct{}{}}

type sportFoundList struct {
	list []sportRelease
	at   time.Time
}

// sportReleases is a game's releases, remembered for a while: a search for
// a game goes to every indexer, and their daily limits are low.
func (s *Server) sportReleases(ctx context.Context, b sportsBackend, eventID string) ([]sportRelease, error) {
	key := b.name() + "|" + eventID
	for {
		sportFound.Lock()
		if f, ok := sportFound.m[key]; ok && time.Since(f.at) < ownKeepFound && (len(f.list) > 0 || time.Since(f.at) < ownKeepNone) {
			sportFound.Unlock()
			return f.list, nil
		}
		wait, busy := sportFound.inFlight[key]
		if !busy {
			sportFound.inFlight[key] = make(chan struct{})
			sportFound.Unlock()
			break
		}
		sportFound.Unlock()
		select { // the same game is being searched: its answer is this one's
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	list, err := b.releases(ctx, eventID)
	sportFound.Lock()
	close(sportFound.inFlight[key])
	delete(sportFound.inFlight, key)
	if err != nil {
		sportFound.Unlock()
		return nil, err
	}
	sportFound.m[key] = sportFoundList{list: list, at: time.Now()}
	for k, f := range sportFound.m {
		if time.Since(f.at) > time.Hour {
			delete(sportFound.m, k)
		}
	}
	sportFound.Unlock()
	return list, nil
}

// prowlarrLink is a link through Prowlarr: /<indexer>/download?...
var prowlarrLink = regexp.MustCompile(`^/(\d+)/download$`)

// linkFixer makes release links ones Usherr can open (reachableLink),
// looking each host up once.
func (s *Server) linkFixer(ctx context.Context) func(string) string {
	known := map[string]bool{} // host -> Usherr reaches it
	return func(link string) string {
		u, err := url.Parse(link)
		if err != nil || u.Host == "" {
			return link
		}
		host := u.Hostname()
		ok, seen := known[host]
		if !seen {
			ok = net.ParseIP(host) != nil
			if !ok {
				lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				_, err := net.DefaultResolver.LookupHost(lctx, host)
				cancel()
				ok = err == nil
			}
			known[host] = ok
		}
		if ok {
			return link
		}
		return s.prowlarrLinkFor(u, link)
	}
}

// reachableLink makes a release link one Usherr can open. Apps on the same
// Docker network as Prowlarr get links by its container name
// (http://prowlarr:9696/3/download?...), which Usherr may not reach; Usherr's
// own indexers that go through the same Prowlarr say where it is (/3/api).
func (s *Server) reachableLink(ctx context.Context, link string) string {
	return s.linkFixer(ctx)(link)
}

// prowlarrLinkFor sends a Prowlarr link to the Prowlarr Usherr's own
// indexers use; any other link stays as it is.
func (s *Server) prowlarrLinkFor(u *url.URL, link string) string {
	m := prowlarrLink.FindStringSubmatch(u.Path)
	if m == nil || s.IndexerRepo == nil {
		return link
	}
	list, err := s.IndexerRepo.List()
	if err != nil {
		return link
	}
	for _, inst := range list {
		b, err := url.Parse(inst.BaseURL)
		if err != nil || b.Host == "" || strings.TrimRight(b.Path, "/") != "/"+m[1]+"/api" {
			continue
		}
		u.Scheme, u.Host = b.Scheme, b.Host
		return u.String()
	}
	return link
}

// POST /api/home/sports/events/{id}/stream {quality}: the game's releases
// as stream options, best first (the same options as a film's).
func (s *Server) handleSportsStream(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Quality string `json:"quality"`
		Part    string `json:"part"`
	}
	_ = decodeJSON(r, &req)
	if p := s.ownProblem(); p != "" {
		writeError(w, http.StatusPreconditionFailed, "Streaming games needs Usherr's streaming engine: "+p)
		return
	}
	b, err := s.sports()
	if err != nil {
		sportsFail(w, err)
		return
	}
	// Not tied to the page: a search dropped halfway is searched again.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 150*time.Second)
	defer cancel()
	found, err := s.sportReleases(ctx, b, r.PathValue("id"))
	if err != nil {
		sportsFail(w, err)
		return
	}
	fix := s.linkFixer(ctx)
	game := s.sportGameTitle(ctx, b, r.PathValue("id"))
	kept := make([]sportRelease, 0, len(found))
	for _, f := range found {
		if req.Part != "" && f.Part != "" && !strings.EqualFold(f.Part, req.Part) {
			continue
		}
		if sideProgram(f.Title, game) {
			continue
		}
		kept = append(kept, f)
	}
	// The household's audio language first (Settings > Quality); releases
	// in other languages only go last, there may be nothing else.
	// The household's audio language first (Settings > Quality), then the
	// quality asked for; the backend's order (closest to the game) within.
	// Releases in other languages only go last: there may be nothing else.
	wanted := quality.ParseLanguages(s.preferredLanguage())
	prof := s.streamProfile(req.Quality)
	type ranked struct {
		r          sportRelease
		lang, qual int
	}
	order := make([]ranked, 0, len(kept))
	for _, f := range kept {
		q := 0
		if req.Quality != "" && !prof.allows(f.Title, f.Size) {
			q = 1
		}
		order = append(order, ranked{f, sportLanguageRank(f, wanted), q})
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].lang != order[j].lang {
			return order[i].lang < order[j].lang
		}
		return order[i].qual < order[j].qual
	})
	list := make([]indexers.Result, 0, len(order))
	for _, o := range order {
		list = append(list, indexers.Result{Title: o.r.Title, DownloadURL: fix(o.r.URL), IndexerName: o.r.Indexer, SizeBytes: o.r.Size, Protocol: indexers.ProtocolUsenet})
	}
	list = s.reliableFirst(ctx, list)
	if len(list) == 0 {
		writeError(w, http.StatusNotFound, "No release of this game was found on your indexers right now.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": "Usherr", "options": s.offerOwnOptions(r, list)})
}

// sportLanguageRank: 0 in a wanted language, 1 not said (usually English),
// 2 several languages none of them wanted, 3 only other languages.
func sportLanguageRank(r sportRelease, wanted []string) int {
	langs := parser.Parse(r.Title).Languages
	// Sportarr says English for anything it can't tell; only another
	// language from it means something.
	if l := strings.TrimSpace(r.Language); l != "" && !strings.EqualFold(l, "unknown") && !strings.EqualFold(l, "english") {
		langs = append(langs, l)
	}
	multi := parser.Parse(r.Title).Multi || strings.Contains(strings.ToLower(r.Title), ".multi.")
	if len(langs) == 0 {
		if multi {
			return 2
		}
		return 1
	}
	for _, l := range langs {
		for _, w := range wanted {
			if strings.EqualFold(l, w) {
				return 0
			}
		}
	}
	if multi {
		return 2
	}
	return 3
}

// sideWords name the programmes around a game: the build-up, the shows
// after it, analysis, highlights.
var sideWords = []string{"show", "warm up", "warmup", "analysis", "notebook", "build up", "buildup", "highlights", "preview", "review",
	"pre qualifying", "post qualifying", "pre race", "post race", "pre sprint", "post sprint", "press conference", "interview", "onboard",
	"magazine", "countdown", "paddock", "debrief", "pre game", "post game", "pregame", "postgame", "weigh in", "weigh ins", "embedded"}

// sideProgram says whether a release is a programme around the game rather
// than the game: it names one the game's own title doesn't.
func sideProgram(release, game string) bool {
	t, g := " "+normWords(release)+" ", " "+normWords(game)+" "
	for _, w := range sideWords {
		if strings.Contains(t, " "+w+" ") && !strings.Contains(g, " "+w+" ") {
			return true
		}
	}
	return false
}

// sportGameTitle is the game's title, as the backend has it ("" if unknown).
func (s *Server) sportGameTitle(ctx context.Context, b sportsBackend, id string) string {
	type titled interface {
		gameTitle(ctx context.Context, id string) string
	}
	if t, ok := b.(titled); ok {
		return t.gameTitle(ctx, id)
	}
	return ""
}

// POST /api/home/sports/events/{id}/streamed {title, league}: a game was
// streamed (the Dashboard counts them).
func (s *Server) handleSportsStreamed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title  string `json:"title"`
		League string `json:"league"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Title) == "" || len(req.Title) > 300 || len(req.League) > 200 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	id := r.PathValue("id")
	if len(id) > 64 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	if err := s.noteSportStreamed(r.Context(), s.sportsMode(), id, req.Title, req.League, requestUserID(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) noteSportStreamed(ctx context.Context, backend, eventID, title, league string, user int64) error {
	if backend == "" {
		return errors.New("sports is off")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sports_streamed (backend, event_id, title, league, user_id, at_ms) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(backend, event_id) DO UPDATE SET at_ms = excluded.at_ms, user_id = excluded.user_id`,
		backend, eventID, strings.TrimSpace(title), strings.TrimSpace(league), user, time.Now().UnixMilli())
	return err
}
