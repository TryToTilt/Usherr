package api

// Usherr: saved NZBs played again by Usherr itself, without StreamNZB. A
// streamed movie or episode keeps the NZB of its release (the config
// folder's streamed/nzb: from the import, "Add as stream", or a release kept
// once it played, for the virtual drive). That NZB is read from Usenet as it
// is watched (internal/download/stream.go), so playing it again costs no
// search and no NZB fetched from an indexer.
//
//   - Before a saved release is offered, a sample of its articles is asked
//     about (NNTP STAT, every Usenet server in order, like StreamNZB's
//     availability check). The answer is kept on the file for a while, so
//     the servers aren't asked again on every look; a release whose
//     articles are gone is marked dead and the drive's search finds another.
//   - Each article is read from the first server that has it: when the
//     primary is missing one, the next server is asked.
//   - Connections come from the same per-server limits as downloads, so
//     nothing here opens more than a provider allows.
//
// The streamed library's pointer files (/api/strm) play the saved release
// first when Usherr's engine is chosen, and the Stremio and Nuvio add-on
// offers it as a "Saved" stream (/addon/<key>/saved/<file>).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/download"
)

// savedCheckKeep is how long an article check of a saved release holds:
// articles that were there this morning are there tonight.
const savedCheckKeep = 12 * time.Hour

// errSavedDead means a saved release's articles are gone from Usenet.
var errSavedDead = errors.New("the saved release's articles are gone from your Usenet servers")

// The engine's parts, as package values so a test can put stand-ins in (no
// news server, no network).
var (
	// savedServers are the Usenet servers, in their order (primary first).
	savedServers = func(s *Server) ([]download.ClientConfig, error) { return s.ownServers() }
	// savedCheck asks the servers about a sample of a release's articles.
	savedCheck = func(ctx context.Context, servers []download.ClientConfig, nzb *download.NZB) download.PrecheckResult {
		return download.Precheck(ctx, servers, download.ForStreamCheck(nzb), download.PrecheckOptions{Timeout: 15 * time.Second})
	}
	// savedOpen opens a streamed file's saved release for reading (opened
	// once and shared with the virtual drive).
	savedOpen = func(s *Server, ctx context.Context, id int64) (*download.StreamFile, error) {
		return s.driveStream(ctx, id)
	}
)

var (
	savedMu       sync.Mutex
	savedTokens   = map[int64]string{} // streamed file -> its open session (home_ustream.go)
	savedChecking sync.Map             // streamed file -> *sync.Mutex: one check at a time
)

// savedSuits says whether a file's saved release suits a quality: any for
// "" (the best there is), else one the quality's profile allows.
func savedSuits(f streamedFileRow, quality string, prof streamProfile) bool {
	if f.nzb == "" || f.health == "dead" {
		return false
	}
	return quality == "" || prof.allows(f.release, f.size)
}

// savedHealth says whether a saved release can be played: "ok" when its
// articles were found (now, or in the last hours), "" when nobody could
// tell (no server answered the check), errSavedDead when they're gone.
func (s *Server) savedHealth(ctx context.Context, f streamedFileRow) (string, error) {
	if f.nzb == "" {
		return "", errors.New("nothing saved for this")
	}
	if f.health == "dead" {
		return "", errSavedDead
	}
	if savedFresh(f) {
		return "ok", nil
	}
	mu, _ := savedChecking.LoadOrStore(f.id, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	// Checked while this one waited?
	if g, err := s.streamedFile(ctx, f.id); err == nil {
		if g.health == "dead" {
			return "", errSavedDead
		}
		if savedFresh(g) {
			return "ok", nil
		}
	}
	servers, err := savedServers(s)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(s.streamedNZBDir(), filepath.Base(f.nzb)))
	if err != nil {
		return "", fmt.Errorf("the saved NZB isn't there: %w", err)
	}
	nzb, err := download.ParseNZB(data)
	if err != nil {
		return "", fmt.Errorf("the saved NZB can't be read: %w", err)
	}
	res := savedCheck(ctx, servers, nzb)
	switch {
	case res.TooThinToStream():
		s.driveDead(f.id) // off the drive too, until its search finds another
		return "", errSavedDead
	case res.Skipped:
		return "", nil
	}
	s.markStreamedHealth(f.id, "ok")
	return "ok", nil
}

// savedFresh says whether a file's saved release was found whole lately.
func savedFresh(f streamedFileRow) bool {
	if f.health != "ok" {
		return false
	}
	at, err := time.Parse(time.RFC3339, f.healthAt)
	return err == nil && time.Since(at) < savedCheckKeep
}

// openSavedSession opens a streamed file's saved release as a stream
// session (or hands back the one already open), and answers its token and
// video.
func (s *Server) openSavedSession(ctx context.Context, f streamedFileRow) (string, *download.StreamFile, error) {
	savedMu.Lock()
	tok := savedTokens[f.id]
	savedMu.Unlock()
	if tok != "" {
		ustreamMu.Lock()
		sess, ok := ustreamSessions[tok]
		if ok {
			sess.last = time.Now()
		}
		ustreamMu.Unlock()
		if ok {
			return tok, sess.file, nil
		}
	}
	if _, err := s.savedHealth(ctx, f); err != nil {
		return "", nil, err
	}
	file, err := savedOpen(s, ctx, f.id)
	if err != nil {
		if errors.Is(err, download.ErrArticleMissing) {
			return "", nil, errSavedDead
		}
		return "", nil, err
	}
	tok = randomID("us_")
	ustreamMu.Lock()
	// No NZB on the session: this release is already the one kept.
	ustreamSessions[tok] = &ustreamSession{file: file, release: f.release, last: time.Now()}
	ustreamMu.Unlock()
	savedMu.Lock()
	savedTokens[f.id] = tok
	savedMu.Unlock()
	return tok, file, nil
}

// ownSavedSource is a streamed file playing from its saved release with
// Usherr's own engine.
func (s *Server) ownSavedSource(ctx context.Context, f streamedFileRow) (*streamedPlay, error) {
	if f.nzb == "" {
		return nil, errors.New("nothing saved for this")
	}
	if _, err := savedServers(s); err != nil {
		return nil, err
	}
	tok, file, err := s.openSavedSession(ctx, f)
	if err != nil {
		return nil, err
	}
	u := s.ustreamURL(tok, file.Name)
	return &streamedPlay{target: u, homeURL: s.ownLANURL(u), release: f.release}, nil
}

// ---- the add-on ----

// savedFilesFor are the streamed files with a saved release for a title (a
// film, or one episode).
func (s *Server) savedFilesFor(ctx context.Context, kind string, tmdb, season, episode int) []streamedFileRow {
	q := `SELECT f.id FROM streamed_files f JOIN streamed_titles t ON t.id = f.title_id
		WHERE t.kind = ? AND t.tmdb_id = ? AND f.nzb != ''`
	args := []any{kind, tmdb}
	if kind == "series" {
		q += ` AND f.season = ? AND f.episode = ?`
		args = append(args, season, episode)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var out []streamedFileRow
	for _, id := range ids {
		if f, err := s.streamedFile(ctx, id); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// addonSavedStreams are a title's saved releases as add-on streams, each
// checked first: one whose articles are gone isn't offered.
func (s *Server) addonSavedStreams(ctx context.Context, ref addonRef, tmdb int, base, key string) []addonStream {
	if ref.Kind != "movie" && ref.Kind != "series" {
		return nil
	}
	var out []addonStream
	for _, f := range s.savedFilesFor(ctx, ref.Kind, tmdb, ref.Season, ref.Episode) {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		health, err := s.savedHealth(cctx, f)
		cancel()
		if err != nil {
			continue
		}
		pic := "Saved"
		if h := heightOf(f.release); h > 0 {
			pic += " · " + map[int]string{2160: "4K", 1080: "1080p", 720: "720p", 480: "480p"}[h]
		}
		desc := f.release
		if desc == "" {
			desc = "Saved release"
		}
		var bits []string
		if f.size > 0 {
			bits = append(bits, fmt.Sprintf("%.1f GB", float64(f.size)/(1<<30)))
		}
		if health == "ok" {
			bits = append(bits, "articles checked")
		} else {
			bits = append(bits, "not checked yet")
		}
		desc += "\n" + strings.Join(bits, " · ")
		hints := map[string]any{"bingeGroup": "usherr-saved"}
		if f.release != "" {
			hints["filename"] = f.release
		}
		if f.size > 0 {
			hints["videoSize"] = f.size
		}
		out = append(out, addonStream{Name: "Usherr\n" + pic, Title: desc, Description: desc,
			URL: fmt.Sprintf("%s/addon/%s/saved/%d", base, key, f.id), BehaviorHints: hints})
	}
	return out
}

// handleAddonSaved: GET /addon/<key>/saved/<file>: a saved release, read
// from Usenet as it's watched, with seeking.
func (s *Server) handleAddonSaved(w http.ResponseWriter, r *http.Request) {
	u, ok := s.addonUser(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := s.streamedFile(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && f.nzb == "") {
		addonCORS(w)
		http.Error(w, "Nothing is saved for this any more. Open the title again.", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	tok, file, err := s.openSavedSession(ctx, f)
	cancel()
	if err != nil {
		addonCORS(w)
		http.Error(w, "Usherr couldn't start that: "+err.Error(), http.StatusBadGateway)
		return
	}
	// On the Marquee while the app reads it.
	what := usherrPlaying{userID: u.ID, kind: "movie", tmdbID: f.tmdb, title: f.title, release: f.release, source: s.ustreamURL(tok, file.Name)}
	if f.kind == "series" {
		what = usherrPlaying{userID: u.ID, kind: "episode", showID: f.tmdb, series: f.title, season: f.season, episode: f.episode, release: f.release, source: what.source}
	}
	addonPlaying("saved-"+strconv.FormatInt(id, 10)+"-"+strconv.FormatInt(u.ID, 10), what, rangeStart(r), file.Size)
	addonCORS(w)
	s.serveUStream(w, r, tok)
}
