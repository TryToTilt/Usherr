package api

// Home fork: direct downloads for comics (after Pullbox's direct sources):
// GetComics is searched alongside the indexers; its posts are judged like any
// release (comics.Evaluate: packs and collections go to the review list), and
// a grabbed post is fetched from the best of its hosts (GetComics' own
// server, PixelDrain, MediaFire, MEGA). Cloudflare checks go through
// FlareSolverr. Off until switched on in Settings > Extras.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/comics"
	"github.com/rdborg/mediarium/internal/directdl"
	"github.com/rdborg/mediarium/internal/indexers"
)

const (
	keyGetComics = "home.getcomics_enabled" // "1" when on
	keyFlareURL  = "home.flaresolverr_url"  // e.g. http://host.docker.internal:8191
)

var (
	directOnce   sync.Once
	directClient *directdl.Client
)

func (s *Server) direct() *directdl.Client {
	directOnce.Do(func() {
		directClient = directdl.New(func() string { v, _ := s.Settings.Get(keyFlareURL); return strings.TrimSpace(v) })
	})
	return directClient
}

func (s *Server) getComicsOn() bool { v, _ := s.Settings.Get(keyGetComics); return v == "1" }

// getComicsQueries are GetComics' searches for an issue: it finds "Daredevil
// 16" but not "Daredevil 016".
func getComicsQueries(sr comics.Series, is comics.Issue) []string {
	name := strings.Join(strings.Fields(strings.NewReplacer(":", " ", "/", " ", "–", " ", "-", " ").Replace(sr.Name)), " ")
	if alt := comics.AltNumber(is); alt != "" {
		return []string{name + " " + strings.ToUpper(strings.TrimSuffix(alt, ".now")) + " NOW"}
	}
	return []string{name + " " + comics.NormalNumber(is.Number)}
}

// getComicsResults searches GetComics for an issue, as indexer results.
func (s *Server) getComicsResults(ctx context.Context, sr comics.Series, is comics.Issue) []indexers.Result {
	return s.getComicsResultsFor(ctx, sr, is, "")
}

// getComicsResultsFor searches GetComics with the person's own words when
// given ("Saga Vol. 6"), else the issue's own searches.
func (s *Server) getComicsResultsFor(ctx context.Context, sr comics.Series, is comics.Issue, own string) []indexers.Result {
	if !s.getComicsOn() {
		return nil
	}
	queries := getComicsQueries(sr, is)
	if own != "" {
		queries = []string{own}
	}
	var out []indexers.Result
	for _, q := range queries {
		sctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		posts, err := s.direct().Search(sctx, q)
		cancel()
		if err != nil {
			slog.Info("comics: GetComics search", "query", q, "err", err)
			continue
		}
		for _, p := range posts {
			out = append(out, indexers.Result{Title: p.Title, IndexerName: "GetComics", Protocol: indexers.ProtocolDirect,
				DownloadURL: p.URL, InfoURL: p.URL, SizeBytes: p.SizeBytes, Priority: 2})
		}
	}
	return out
}

// downloadDirect fetches a GetComics post's file into dir.
func (s *Server) downloadDirect(ctx context.Context, queueID int64, postURL, dir string) error {
	if !strings.HasPrefix(postURL, "https://getcomics.org/") {
		return fmt.Errorf("not a GetComics post: %s", postURL)
	}
	progress := newProgressSaver(func(pct float64) error { return s.QueueRepo.SetProgress(queueID, pct) })
	_, err := s.direct().Fetch(ctx, postURL, dir, progress.Report)
	progress.Flush()
	if gone := (*directdl.GoneError)(nil); errors.As(err, &gone) {
		return badRelease(err) // dead on every host: blocklisted, and the issue searched again
	}
	return err
}
