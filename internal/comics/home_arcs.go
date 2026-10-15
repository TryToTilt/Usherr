package comics

// Home fork: story arcs from ComicVine (after Pullbox's story arcs). An arc's
// detail page lists its issues in reading order (all of them, unpaged); the
// issues are then read 100 at a time for their series, number and dates.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Arc is a ComicVine story arc.
type Arc struct {
	CVID        int64  `json:"cvId"`
	Name        string `json:"name"`
	Publisher   string `json:"publisher,omitempty"`
	Description string `json:"description,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
	Issues      int    `json:"issues,omitempty"` // how many ComicVine says it has (often 0: unreliable)
}

// ArcIssue is one issue of an arc, in reading order.
type ArcIssue struct {
	Position   int    `json:"position"`
	CVID       int64  `json:"cvId"`
	VolumeCVID int64  `json:"volumeCvId"`
	Series     string `json:"series"`
	Number     string `json:"number"`
	Title      string `json:"title,omitempty"`
	CoverDate  string `json:"coverDate,omitempty"`
	StoreDate  string `json:"storeDate,omitempty"`
	ImageURL   string `json:"imageUrl,omitempty"`
}

type cvArc struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Deck        string   `json:"deck"`
	Description string   `json:"description"`
	Image       *cvImage `json:"image"`
	Count       int      `json:"count_of_isssue_appearances"` // sic: ComicVine's spelling
	Publisher   *struct {
		Name string `json:"name"`
	} `json:"publisher"`
	Issues []struct {
		ID int64 `json:"id"`
	} `json:"issues"`
}

const arcFields = "id,name,deck,description,image,publisher,count_of_isssue_appearances"

func (a cvArc) arc() Arc {
	out := Arc{CVID: a.ID, Name: strings.TrimSpace(a.Name), ImageURL: a.Image.url(), Issues: a.Count}
	if a.Publisher != nil {
		out.Publisher = a.Publisher.Name
	}
	out.Description = plain(a.Deck)
	if out.Description == "" {
		out.Description = plain(a.Description)
	}
	if len(out.Description) > 1500 {
		out.Description = out.Description[:1500] + "…"
	}
	return out
}

// SearchArcs finds story arcs by name.
func (c *ComicVine) SearchArcs(ctx context.Context, query string) ([]Arc, error) {
	page := &cvPage{}
	if err := c.get(ctx, "/story_arcs/", url.Values{
		"filter":     {"name:" + strings.ReplaceAll(strings.TrimSpace(query), ",", " ")},
		"field_list": {arcFields},
		"limit":      {"30"},
	}, page); err != nil {
		return nil, err
	}
	var list []cvArc
	if err := json.Unmarshal(page.raw, &list); err != nil {
		return nil, err
	}
	out := make([]Arc, 0, len(list))
	for _, a := range list {
		out = append(out, a.arc())
	}
	return out, nil
}

// GetArc returns an arc and its issues in reading order.
func (c *ComicVine) GetArc(ctx context.Context, cvID int64) (Arc, []ArcIssue, error) {
	var a cvArc
	if err := c.get(ctx, fmt.Sprintf("/story_arc/4045-%d/", cvID), url.Values{"field_list": {arcFields + ",issues"}}, &a); err != nil {
		return Arc{}, nil, err
	}
	arc := a.arc()
	var ids []int64
	seen := map[int64]bool{}
	for _, i := range a.Issues {
		if i.ID > 0 && !seen[i.ID] {
			seen[i.ID] = true
			ids = append(ids, i.ID)
		}
	}
	if len(ids) > 2000 {
		ids = ids[:2000]
	}
	byID := map[int64]ArcIssue{}
	for start := 0; start < len(ids); start += 100 {
		batch := ids[start:min(start+100, len(ids))]
		parts := make([]string, len(batch))
		for k, id := range batch {
			parts[k] = strconv.FormatInt(id, 10)
		}
		page := &cvPage{}
		if err := c.get(ctx, "/issues/", url.Values{
			"filter":     {"id:" + strings.Join(parts, "|")},
			"field_list": {"id,issue_number,name,cover_date,store_date,image,volume"},
			"limit":      {"100"},
		}, page); err != nil {
			return Arc{}, nil, err
		}
		var list []struct {
			cvIssue
			Volume *struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"volume"`
		}
		if err := json.Unmarshal(page.raw, &list); err != nil {
			return Arc{}, nil, err
		}
		for _, i := range list {
			ai := ArcIssue{CVID: i.ID, Number: strings.TrimSpace(i.IssueNumber), Title: strings.TrimSpace(i.Name),
				CoverDate: i.CoverDate, StoreDate: i.StoreDate, ImageURL: i.Image.url()}
			if i.Volume != nil {
				ai.VolumeCVID, ai.Series = i.Volume.ID, strings.TrimSpace(i.Volume.Name)
			}
			byID[i.ID] = ai
		}
	}
	out := make([]ArcIssue, 0, len(ids))
	for _, id := range ids { // ComicVine's order is the reading order
		if ai, ok := byID[id]; ok && ai.VolumeCVID > 0 {
			ai.Position = len(out) + 1
			out = append(out, ai)
		}
	}
	return arc, out, nil
}
