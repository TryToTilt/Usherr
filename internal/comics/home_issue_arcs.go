package comics

// Home fork: the story arcs one issue is part of. ComicVine only gives these
// on an issue's own page, so it's one call per issue (read once and kept).

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// ArcRef names a story arc.
type ArcRef struct {
	CVID int64  `json:"cvId"`
	Name string `json:"name"`
}

// IssueArcs lists the story arcs of the issue with ComicVine id cvID.
func (c *ComicVine) IssueArcs(ctx context.Context, cvID int64) ([]ArcRef, error) {
	var issue struct {
		Arcs []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"story_arc_credits"`
	}
	if err := c.get(ctx, fmt.Sprintf("/issue/4000-%d/", cvID), url.Values{"field_list": {"id,story_arc_credits"}}, &issue); err != nil {
		return nil, err
	}
	out := []ArcRef{}
	for _, a := range issue.Arcs {
		if a.ID > 0 && strings.TrimSpace(a.Name) != "" {
			out = append(out, ArcRef{CVID: a.ID, Name: strings.TrimSpace(a.Name)})
		}
	}
	return out, nil
}
