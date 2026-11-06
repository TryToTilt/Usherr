package musicbrainz

// Adapted from DroppedNeedle (AGPL-3.0), backend/repositories/edition_policy.py
// (recall_key): among the editions of one release group, the one whose track
// count is nearest a download's is the one the download most likely is.

import (
	"context"
	"sort"
	"strings"
)

// TrackCount is the number of tracks on all of a release's media.
func (r Release) TrackCount() int {
	n := 0
	for _, m := range r.Media {
		if m.TrackCount > 0 {
			n += m.TrackCount
		} else {
			n += len(m.Tracks)
		}
	}
	return n
}

// EditionFor picks the edition of a release group that fits a download of
// files tracks: the nearest track count first, then Official, the earliest
// date (a precise date before a bare year), worldwide ("XW"), and the id so
// the choice is stable. Releases with no track count are skipped. ok is false
// when none is left.
func EditionFor(releases []Release, files int) (Release, bool) {
	var cand []Release
	for _, r := range releases {
		if r.ID != "" && r.TrackCount() > 0 {
			cand = append(cand, r)
		}
	}
	if len(cand) == 0 {
		return Release{}, false
	}
	gap := func(r Release) int {
		d := r.TrackCount() - files
		if d < 0 {
			d = -d
		}
		return d
	}
	rank := func(b bool) int {
		if b {
			return 0
		}
		return 1
	}
	sort.SliceStable(cand, func(i, j int) bool {
		a, b := cand[i], cand[j]
		if ga, gb := gap(a), gap(b); ga != gb {
			return ga < gb
		}
		if oa, ob := rank(strings.EqualFold(a.Status, "official")), rank(strings.EqualFold(b.Status, "official")); oa != ob {
			return oa < ob
		}
		if da, db := dateKey(a.Date), dateKey(b.Date); da != db {
			return da < db
		}
		if xa, xb := rank(a.Country == "XW"), rank(b.Country == "XW"); xa != xb {
			return xa < xb
		}
		return a.ID < b.ID
	})
	return cand[0], true
}

// EditionTracklist fetches the edition of a release group that fits a
// download of files tracks (EditionFor), with its tracklist.
func (c *Client) EditionTracklist(ctx context.Context, releaseGroupID string, files int) (Release, error) {
	releases, err := c.ReleaseGroupReleases(ctx, releaseGroupID)
	if err != nil {
		return Release{}, err
	}
	pick, ok := EditionFor(releases, files)
	if !ok {
		return Release{}, ErrNotFound
	}
	return c.GetRelease(ctx, pick.ID)
}
