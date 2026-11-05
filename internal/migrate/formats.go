package migrate

// Usherr: custom formats from Radarr and Sonarr, copied as they are (Usherr
// reads the same conditions) into Usherr's format list, ready to score in
// Usherr's own profiles. Their profiles never come over: Usherr keeps its own.
// A format brought over by an earlier import is updated, never doubled.

import (
	"encoding/json"
	"strings"

	"github.com/rdborg/mediarium/internal/quality"
)

// formatsPlan is one app's custom formats.
type formatsPlan struct {
	app     quality.FormatApp
	appName string // "Radarr" or "Sonarr"
	formats []quality.ImportedFormat
	problem string // why the formats couldn't be read ("" when they could)
}

// FormatsSummary is what an import would bring over from one app.
type FormatsSummary struct {
	Formats int    `json:"formats"`
	Problem string `json:"problem,omitempty"`
}

func planFormats(app quality.FormatApp, appName string, raw json.RawMessage) *formatsPlan {
	fp := &formatsPlan{app: app, appName: appName}
	if len(raw) == 0 {
		fp.problem = appName + " didn't list its custom formats"
		return fp
	}
	parsed, err := quality.ParseFormats(raw, app)
	if err != nil {
		fp.problem = "its custom formats couldn't be read: " + err.Error()
		return fp
	}
	fp.formats = parsed
	return fp
}

func (fp *formatsPlan) summary() *FormatsSummary {
	return &FormatsSummary{Formats: len(fp.formats), Problem: fp.problem}
}

// importFormats saves an app's formats.
// A format whose name a format of the other app already has here is saved
// as "<name> (TV)" or "<name> (Movies)", since the two number their
// conditions differently.
func (im *Importer) importFormats(fp *formatsPlan) {
	if im.deps.Formats == nil || fp.problem != "" {
		if fp.problem != "" {
			im.record("customFormat", fp.appName+" custom formats", OutcomeSkipped, fp.problem, func(r *Results) *Counts { return &r.CustomFormats })
		}
		return
	}
	stored, err := im.deps.Formats.List()
	if err != nil {
		im.record("customFormat", fp.appName+" custom formats", OutcomeFailed, err.Error(), func(r *Results) *Counts { return &r.CustomFormats })
		return
	}
	otherApp := map[string]bool{}
	for _, f := range stored {
		if f.App != "" && f.App != fp.app {
			otherApp[strings.ToLower(f.Name)] = true
		}
	}
	suffix := " (Movies)"
	if fp.app == quality.AppSonarr {
		suffix = " (TV)"
	}
	for _, f := range fp.formats {
		cf := f.Format
		if otherApp[strings.ToLower(cf.Name)] {
			cf.Name += suffix
		}
		_, isNew, err := im.deps.Formats.Save(cf)
		outcome, reason := OutcomeAdded, ""
		switch {
		case err != nil:
			outcome, reason = OutcomeFailed, err.Error()
		case !isNew:
			outcome, reason = OutcomeExists, "updated to "+fp.appName+"'s conditions"
		}
		im.record("customFormat", cf.Name, outcome, reason, func(r *Results) *Counts { return &r.CustomFormats })
	}
}
