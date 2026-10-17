package api

import (
	"fmt"
	"net/http"

	"github.com/rdborg/mediarium/internal/indexers"
)

// The interactive release lists (movie, season or episode, album) answer with
// a plain list of releases. With ?detail=1 they answer with the list and also
// say which indexers were asked and which did not answer, so the page
// can tell "nothing was found" from "an indexer is down".

type unansweredSource struct {
	Name    string `json:"name"`
	Message string `json:"message"` // plain words, for example "Could not reach Example."
}

type searchAnswer[T any] struct {
	Results    []T                `json:"results"`
	Sources    int                `json:"sources"` // indexers that were asked
	Unanswered []unansweredSource `json:"unanswered"`
	Warnings   []string           `json:"warnings"` // for example an indexer asked past its daily limit
}

// unansweredFrom lists the indexers that failed, in the order asked. An indexer
// asked for several titles of one movie or show is listed once, with the first
// thing it said.
func unansweredFrom(outcomes []indexers.Outcome) []unansweredSource {
	out := []unansweredSource{}
	listed := map[string]bool{}
	for _, o := range outcomes {
		if listed[o.IndexerName] {
			continue
		}
		if o.Err != nil {
			listed[o.IndexerName] = true
			out = append(out, unansweredSource{Name: o.IndexerName, Message: indexers.ProblemMessage(o)})
		}
		if o.Skipped {
			listed[o.IndexerName] = true
			msg := indexers.PausedMessage(o.IndexerName, o.Health, nil)
			if o.LimitNote != "" {
				msg = o.IndexerName + ": " + o.LimitNote
			}
			out = append(out, unansweredSource{Name: o.IndexerName, Message: msg})
		}
	}
	return out
}

// limitWarnings lists the indexers that were asked although their daily limit
// is used up (a search started by hand asks them anyway), once each.
func limitWarnings(outcomes []indexers.Outcome) []string {
	out := []string{}
	listed := map[string]bool{}
	for _, o := range outcomes {
		if o.LimitNote == "" || o.Skipped || listed[o.IndexerName] {
			continue
		}
		listed[o.IndexerName] = true
		out = append(out, fmt.Sprintf("Even though its daily limit is used up, %s was asked, so this search may be refused.", o.IndexerName))
	}
	return out
}

// writeSearch sends the release list: the bare list, or with ?detail=1 the
// list with what the indexers said. sources is how many were asked.
func writeSearch[T any](w http.ResponseWriter, r *http.Request, results []T, sources int, outcomes []indexers.Outcome) {
	if results == nil {
		results = []T{}
	}
	if r.URL.Query().Get("detail") != "1" {
		writeJSON(w, http.StatusOK, results)
		return
	}
	writeJSON(w, http.StatusOK, searchAnswer[T]{Results: results, Sources: sources, Unanswered: unansweredFrom(outcomes), Warnings: limitWarnings(outcomes)})
}
