package api

// Usherr: what a profile's Advanced options do to an automatic search's
// releases (internal/quality/extras.go): the indexers and protocol it is held
// to, how new or old a release may be, and its own sizes per quality. The
// releases they rule out are taken out before the best one is picked, like
// the size table; a search by hand still lists them.

import (
	"fmt"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/quality"
)

// profileIndexerNames is the names of the indexers a profile is held to, nil
// when it may use all of them.
func (s *Server) profileIndexerNames(profile quality.Profile) map[string]bool {
	if len(profile.Indexers) == 0 || s.IndexerRepo == nil {
		return nil
	}
	list, err := s.IndexerRepo.List()
	if err != nil {
		return nil // can't tell: don't hold back every release
	}
	want := map[int64]bool{}
	for _, id := range profile.Indexers {
		if id < 0 {
			return map[string]bool{} // a tag rule and the profile have no indexer in common: none fits
		}
		want[id] = true
	}
	names := map[string]bool{}
	for _, inst := range list {
		if want[inst.ID] {
			names[strings.ToLower(inst.Name)] = true
		}
	}
	if len(names) == 0 {
		return nil // every chosen indexer is gone
	}
	return names
}

// extrasReason is why a profile's Advanced options rule a release out, ""
// when they don't. names is profileIndexerNames.
func extrasReason(profile quality.Profile, names map[string]bool, res indexers.Result, episodes int, now time.Time) string {
	if names != nil && !names[strings.ToLower(res.IndexerName)] {
		return fmt.Sprintf("found on %s, which the %q profile doesn't search", res.IndexerName, profile.Name)
	}
	torrent := res.Torrent || res.Protocol == indexers.ProtocolTorrent
	switch profile.Protocol {
	case quality.ProtocolUsenet:
		if torrent {
			return fmt.Sprintf("a torrent, and the %q profile takes Usenet only", profile.Name)
		}
	case quality.ProtocolTorrent:
		if !torrent {
			return fmt.Sprintf("a Usenet release, and the %q profile takes torrents only", profile.Name)
		}
	}
	if !res.PublishDate.IsZero() {
		age := now.Sub(res.PublishDate)
		if profile.MinAgeMinutes > 0 && age < time.Duration(profile.MinAgeMinutes)*time.Minute {
			return fmt.Sprintf("posted %d minutes ago; the %q profile waits %d", int(age.Minutes()), profile.Name, profile.MinAgeMinutes)
		}
		if profile.MaxAgeDays > 0 && age > time.Duration(profile.MaxAgeDays)*24*time.Hour {
			return fmt.Sprintf("posted %d days ago; the %q profile skips releases older than %d days", int(age.Hours()/24), profile.Name, profile.MaxAgeDays)
		}
	}
	return profile.TierSizeReason(quality.Classify(res.Parsed()), res.SizeBytes, episodes)
}

// applyProfileExtras takes out of results the releases the profile's Advanced
// options rule out, and returns the first reason given ("" when none).
func (s *Server) applyProfileExtras(profile quality.Profile, results []indexers.Result, episodes func(indexers.Result) int) ([]indexers.Result, string) {
	if !hasSearchExtras(profile) || len(results) == 0 {
		return results, ""
	}
	names := s.profileIndexerNames(profile)
	now := time.Now()
	out := make([]indexers.Result, 0, len(results))
	first := ""
	for _, res := range results {
		if why := extrasReason(profile, names, res, episodes(res), now); why != "" {
			if first == "" {
				first = why
			}
			continue
		}
		out = append(out, res)
	}
	return out, first
}

func hasSearchExtras(p quality.Profile) bool {
	return len(p.Indexers) > 0 || p.Protocol != quality.ProtocolAny || p.MinAgeMinutes > 0 || p.MaxAgeDays > 0 || len(p.TierSizes) > 0
}
