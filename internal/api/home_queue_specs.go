package api

// Home fork: what a film or episode download is, from its release name, for
// the Activity page: resolution, source, video codec, HDR, audio, edition,
// language and the group, the way the search results show them.

import (
	"strings"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/queue"
)

type releaseSpecs struct {
	Resolution string `json:"resolution,omitempty"`
	Source     string `json:"source,omitempty"`
	Codec      string `json:"codec,omitempty"`
	HDR        string `json:"hdr,omitempty"`
	Audio      string `json:"audio,omitempty"`
	Edition    string `json:"edition,omitempty"`
	Language   string `json:"language,omitempty"`
	Subbed     bool   `json:"subbed,omitempty"`
	Group      string `json:"group,omitempty"`
	Repack     bool   `json:"repack,omitempty"` // a proper or repack
	ThreeD     bool   `json:"threeD,omitempty"`
}

// specsOf reads a film or TV release name; nil for music, books and comics, or when
// the name says nothing useful.
func (s *Server) specsOf(it queue.Item) *releaseSpecs {
	if (it.MovieID == 0 && it.SeriesID == 0) || strings.TrimSpace(it.ReleaseTitle) == "" {
		return nil
	}
	r := parser.Parse(it.ReleaseTitle)
	sp := &releaseSpecs{
		Resolution: r.Resolution, Source: r.Source, Codec: r.Codec, HDR: r.HDR, Audio: r.AudioCodec,
		Edition: r.Edition, Subbed: r.Subbed, Group: r.Group, Repack: r.Proper || r.Repack, ThreeD: r.Is3D,
	}
	pref := ""
	if s.Settings != nil {
		pref = s.preferredLanguage()
	}
	fit := quality.FitLanguage(r, pref)
	sp.Language = fit.Label
	if *sp == (releaseSpecs{}) {
		return nil
	}
	return sp
}
