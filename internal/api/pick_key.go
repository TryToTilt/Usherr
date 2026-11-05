package api

import (
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// pickKey is what automation compares releases by when choosing the best one:
// the language first (a release with no language tag or only English before
// one that adds another language, whatever their quality), then the quality
// tier (its rank in the profile's own order, so the qualities of a group tie
// and are told apart by the built-in order), then the profile's preferred-term score. Equal keys are settled by
// preferOver (indexer priority, protocol, then a cached release).
//
// cached sits between the tier and the score, and is only ever non-zero when
// the person allowed "a lower score for a cached release": a cached release
// then beats an uncached one of the same language and tier whatever the score.
// It never lifts a release over a better language or tier.
//
// A torrent its indexer reports with no seeders at all is dead: dead goes
// below everything else, so it is only picked when nothing alive is left, and
// among two otherwise equal torrents the one with more seeders wins (see
// preferOver). A release whose seeders are not known is never counted as dead.
//
// Usherr: the profile's Advanced options add hevc (x265 over x264 at the same
// quality), group (a preferred release group), subs (English subtitles) and
// size (near the preferred size, or smaller), all 0 when they are off.
type pickKey struct {
	alive, language, tier, sub, hevc, cached, group, score, subs, size int
}

// above reports whether k beats other.
func (k pickKey) above(other pickKey) bool {
	if k.alive != other.alive {
		return k.alive > other.alive
	}
	if k.language != other.language {
		return k.language > other.language
	}
	if k.tier != other.tier {
		return k.tier > other.tier
	}
	if k.sub != other.sub {
		return k.sub > other.sub
	}
	if k.hevc != other.hevc {
		return k.hevc > other.hevc
	}
	if k.cached != other.cached {
		return k.cached > other.cached
	}
	if k.group != other.group {
		return k.group > other.group
	}
	if k.score != other.score {
		return k.score > other.score
	}
	if k.subs != other.subs {
		return k.subs > other.subs
	}
	return k.size > other.size
}

// keyFor is the pickKey of a release that profile accepts.
func keyFor(profile quality.Profile, rel parser.Release, res *indexers.Result) pickKey {
	k := pickKey{alive: 1, language: profile.LanguageRank(rel), tier: profile.TierRank(quality.Classify(rel)), sub: quality.Rank(quality.Classify(rel)), score: profile.Score(res.Title)}
	if res.SeedersKnown && !res.Cached && res.Seeders <= 0 {
		k.alive = 0
	}
	if res.Cached && cachedOverScore.Load() {
		k.cached = 1
	}
	// Usherr: Advanced options (internal/quality/extras.go).
	if profile.PreferHEVC && quality.HEVC(res.Title) {
		k.hevc = 1
	}
	if profile.PreferredGroup(res.Title, rel) {
		k.group = 1
	}
	if profile.WantSubs && quality.HasSubs(res.Title) {
		k.subs = 1
	}
	k.size = profile.SizePreference(quality.Classify(rel), res.SizeBytes)
	return k
}
