package api

// Usherr: the profile editor's release tester. It judges release names against
// the profile as it stands in the editor (saved or not), with the same checks
// an automatic search makes, and says why: the quality, the language, the
// release-name rules, bad releases, release groups, custom formats and sizes.

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

type tryRelease struct {
	Title  string  `json:"title"`
	SizeGB float64 `json:"sizeGB"`
	// Episodes is how many episodes it holds, 0 for a movie.
	Episodes int `json:"episodes"`
}

type tryRequest struct {
	Profile  profileRequest `json:"profile"`
	Releases []tryRelease   `json:"releases"`
}

type tryResult struct {
	Title   string              `json:"title"`
	Take    bool                `json:"take"`
	Quality string              `json:"quality"`
	Score   int                 `json:"score"`
	Skip    []string            `json:"skip"`    // why it would be skipped
	Counts  []string            `json:"counts"`  // what counts for it
	Formats []quality.FormatHit `json:"formats"` // custom formats it matched
}

const maxTryReleases = 12

func (s *Server) handleTryProfile(w http.ResponseWriter, r *http.Request) {
	var req tryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if len(req.Releases) == 0 || len(req.Releases) > maxTryReleases {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Give 1 to %d release names to try.", maxTryReleases))
		return
	}
	p := req.Profile.toProfile(0)
	if strings.TrimSpace(p.Name) == "" {
		p.Name = "this profile"
	}
	p, err := quality.Validate(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), quality.ErrInvalid.Error()+": "))
		return
	}
	formats, err := s.FormatRepo.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byID := map[int64]*quality.CustomFormat{}
	for i := range formats {
		byID[formats[i].ID] = &formats[i]
	}
	p = quality.AttachFormats(p, byID)
	p.Language, p.LanguageOnly = s.preferredLanguage(), s.languageOnly()
	if p.AudioLanguage != "" {
		p.Language = p.AudioLanguage
	}
	out := make([]tryResult, 0, len(req.Releases))
	for _, rel := range req.Releases {
		out = append(out, tryOne(p, rel))
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// tryOne is the verdict on one release.
func tryOne(p quality.Profile, in tryRelease) tryResult {
	title := strings.TrimSpace(in.Title)
	rel := parser.Parse(title)
	tier := quality.Classify(rel)
	res := tryResult{Title: title, Quality: string(tier), Skip: []string{}, Counts: []string{}, Formats: []quality.FormatHit{}}
	if title == "" {
		res.Skip = append(res.Skip, "no release name")
		return res
	}
	if !p.AllowsTier(tier) {
		if tier == quality.TierUnknown {
			res.Skip = append(res.Skip, "the name doesn't say its quality")
		} else {
			res.Skip = append(res.Skip, fmt.Sprintf("%s isn't a quality this profile takes", tier))
		}
	}
	if why := p.LanguageReason(rel); why != "" && !p.LanguageOK(rel) {
		res.Skip = append(res.Skip, why)
	}
	if ok, why := p.TitleAllowedRel(title, rel); !ok {
		res.Skip = append(res.Skip, why)
	}
	size := int64(in.SizeGB * float64(int64(1)<<30))
	eps := in.Episodes
	if size > 0 {
		if !p.SizeAllowed(size) {
			res.Skip = append(res.Skip, fmt.Sprintf("%g GB is over the %g GB limit", in.SizeGB, p.MaxSizeGB))
		}
		if why := p.TierSizeReason(tier, size, eps); why != "" {
			res.Skip = append(res.Skip, why)
		}
	}
	res.Score = p.Score(title)
	_, res.Formats = p.FormatBreakdown(title)
	if res.Formats == nil {
		res.Formats = []quality.FormatHit{}
	}
	for _, f := range res.Formats {
		res.Counts = append(res.Counts, fmt.Sprintf("%s %+d", f.Name, f.Score))
	}
	for _, pr := range p.PreferredHits(title) {
		res.Counts = append(res.Counts, fmt.Sprintf("%s %+d", pr.Term, pr.Score))
	}
	if p.PreferredGroup(title, rel) {
		res.Counts = append(res.Counts, "a preferred release group")
	}
	if p.PreferHEVC && quality.HEVC(title) {
		res.Counts = append(res.Counts, "x265, preferred at the same quality")
	}
	if p.WantSubs && quality.HasSubs(title) {
		res.Counts = append(res.Counts, "English subtitles")
	}
	res.Take = len(res.Skip) == 0
	return res
}
