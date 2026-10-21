package api

import (
	"context"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/metadata"
)

const (
	// countryBackfillInterval is how often shows whose origin country is not
	// known yet (added before it was stored) are looked up on TMDB. The
	// batch and pause keep a big library from hitting TMDB in one burst.
	countryBackfillInterval = time.Hour
	countryBackfillBatch    = 40
	countryBackfillPause    = 250 * time.Millisecond
)

// saveSeriesCountry stores the origin country TMDB gives for a show when it
// differs from what is saved. It is part of every add and refresh.
func (s *Server) saveSeriesCountry(sr *library.Series, show metadata.Show) {
	countries := show.OriginCountry
	if countries == nil {
		countries = []string{}
	}
	if sr.OriginCountry != nil && slices.Equal(sr.OriginCountry, upperAll(countries)) {
		return
	}
	if err := s.MovieRepo.SetSeriesOriginCountry(sr.ID, countries); err != nil {
		log.Printf("api: %v", err)
		return
	}
	sr.OriginCountry = upperAll(countries)
}

func upperAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		if c = strings.ToUpper(strings.TrimSpace(c)); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// backfillCountries reads the origin country of shows that were added before
// it was stored. A lookup that fails for a passing reason ends the run (the
// next one tries again); a show TMDB no longer knows is saved with no country
// so it is not asked for again.
func (s *Server) backfillCountries(ctx context.Context) {
	if !s.TMDB().HasAPIKey() {
		return
	}
	gaps, err := s.MovieRepo.MissingOriginCountry(countryBackfillBatch)
	if err != nil {
		log.Printf("automation: country-backfill: %v", err)
		return
	}
	for i, g := range gaps {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(countryBackfillPause):
			}
		}
		sh, err := s.TMDB().GetShow(ctx, g.TMDBID)
		var countries []string
		if err != nil {
			if !strings.Contains(err.Error(), "status 404") {
				log.Printf("automation: country-backfill: %q: %v", g.Title, err)
				return
			}
		} else {
			countries = sh.OriginCountry
		}
		if err := s.MovieRepo.SetSeriesOriginCountry(g.ID, upperAll(countries)); err != nil {
			log.Printf("automation: country-backfill: %v", err)
			return
		}
	}
}
