package nsfw

// Home fork: browsing StashDB with filters, for the NSFW page's Find tab:
// videos by text, tags, year and sort; performers by name, gender and sort;
// studios by name; tags by name for autocomplete.

import (
	"context"
	"fmt"
	"strings"
)

// NsfwQuery is a filtered video search.
type NsfwQuery struct {
	Text    string
	TagIDs  []string
	TagAll  bool // every tag (true) or any of them (false)
	Since   int  // released in this year or later (0 = any)
	Sort    string
	Page    int
	PerPage int
}

var videoSorts = map[string][2]string{
	"trending": {"TRENDING", "DESC"},
	"newest":   {"DATE", "DESC"},
	"oldest":   {"DATE", "ASC"},
	"title":    {"TITLE", "ASC"},
	"added":    {"CREATED_AT", "DESC"},
}

// QueryNsfw runs a filtered video search and returns one page and the total.
func (c *StashBox) QueryNsfw(ctx context.Context, q NsfwQuery) ([]Video, int, error) {
	input := map[string]any{"page": max(q.Page, 1), "per_page": clampPer(q.PerPage)}
	if t := strings.TrimSpace(q.Text); t != "" {
		input["text"] = t
	}
	if len(q.TagIDs) > 0 {
		mod := "INCLUDES"
		if q.TagAll {
			mod = "INCLUDES_ALL"
		}
		input["tags"] = map[string]any{"value": q.TagIDs, "modifier": mod}
	}
	if q.Since > 0 {
		input["date"] = map[string]any{"value": fmt.Sprintf("%d-12-31", q.Since-1), "modifier": "GREATER_THAN"}
	}
	sort, ok := videoSorts[q.Sort]
	if !ok {
		sort = videoSorts["trending"]
		if strings.TrimSpace(q.Text) != "" {
			sort = videoSorts["newest"]
		}
	}
	input["sort"], input["direction"] = sort[0], sort[1]
	var out struct {
		QueryNsfw struct {
			Count int         `json:"count"`
			Nsfw  []wireVideo `json:"scenes"`
		} `json:"queryScenes"`
	}
	if err := c.query(ctx, `query($input: SceneQueryInput!) { queryScenes(input: $input) { count scenes { `+videoFields+` } } }`, map[string]any{"input": input}, &out); err != nil {
		return nil, 0, err
	}
	list := make([]Video, 0, len(out.QueryNsfw.Nsfw))
	for _, w := range out.QueryNsfw.Nsfw {
		list = append(list, w.toVideo())
	}
	return list, out.QueryNsfw.Count, nil
}

// PerformersQuery is a filtered performer search.
type PerformersQuery struct {
	Name    string
	Gender  string // MALE, FEMALE, TRANSGENDER_MALE, TRANSGENDER_FEMALE, INTERSEX, NON_BINARY
	Sort    string
	Page    int
	PerPage int
}

var performerSorts = map[string][2]string{
	"nsfw":   {"SCENE_COUNT", "DESC"},
	"name":   {"NAME", "ASC"},
	"newest": {"DEBUT", "DESC"},
	"recent": {"LAST_SCENE", "DESC"},
	"added":  {"CREATED_AT", "DESC"},
}

var genders = map[string]bool{"MALE": true, "FEMALE": true, "TRANSGENDER_MALE": true, "TRANSGENDER_FEMALE": true, "INTERSEX": true, "NON_BINARY": true}

// QueryPerformers runs a filtered performer search.
func (c *StashBox) QueryPerformers(ctx context.Context, q PerformersQuery) ([]FoundPerformer, int, error) {
	input := map[string]any{"page": max(q.Page, 1), "per_page": clampPer(q.PerPage)}
	if n := strings.TrimSpace(q.Name); n != "" {
		input["names"] = n
	}
	if genders[q.Gender] {
		input["gender"] = q.Gender
	}
	sort, ok := performerSorts[q.Sort]
	if !ok {
		sort = performerSorts["nsfw"]
	}
	input["sort"], input["direction"] = sort[0], sort[1]
	var out struct {
		QueryPerformers struct {
			Count      int             `json:"count"`
			Performers []wirePerformer `json:"performers"`
		} `json:"queryPerformers"`
	}
	if err := c.query(ctx, `query($input: PerformerQueryInput!) { queryPerformers(input: $input) { count performers { `+performerFields+` gender } } }`, map[string]any{"input": input}, &out); err != nil {
		return nil, 0, err
	}
	list := make([]FoundPerformer, 0, len(out.QueryPerformers.Performers))
	for _, w := range out.QueryPerformers.Performers {
		list = append(list, FoundPerformer{Performer: w.toPerformer(), VideoCount: w.VideoCount})
	}
	return list, out.QueryPerformers.Count, nil
}

// QueryStudios runs a studio search by name.
func (c *StashBox) QueryStudios(ctx context.Context, name, sort string, page, perPage int) ([]Studio, int, error) {
	input := map[string]any{"page": max(page, 1), "per_page": clampPer(perPage), "sort": "NAME", "direction": "ASC"}
	if n := strings.TrimSpace(name); n != "" {
		input["names"] = n
	}
	if sort == "added" {
		input["sort"], input["direction"] = "CREATED_AT", "DESC"
	}
	var out struct {
		QueryStudios struct {
			Count   int          `json:"count"`
			Studios []wireStudio `json:"studios"`
		} `json:"queryStudios"`
	}
	if err := c.query(ctx, `query($input: StudioQueryInput!) { queryStudios(input: $input) { count studios { `+studioFields+` } } }`, map[string]any{"input": input}, &out); err != nil {
		return nil, 0, err
	}
	list := make([]Studio, 0, len(out.QueryStudios.Studios))
	for _, w := range out.QueryStudios.Studios {
		list = append(list, w.toStudio())
	}
	return list, out.QueryStudios.Count, nil
}

// Tag is a StashDB tag.
type Tag struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

// QueryTags finds tags whose name contains name, for autocomplete.
func (c *StashBox) QueryTags(ctx context.Context, name string, limit int) ([]Tag, error) {
	var out struct {
		QueryTags struct {
			Tags []Tag `json:"tags"`
		} `json:"queryTags"`
	}
	input := map[string]any{"name": strings.TrimSpace(name), "page": 1, "per_page": clampPer(limit), "sort": "NAME", "direction": "ASC"}
	if err := c.query(ctx, `query($input: TagQueryInput!) { queryTags(input: $input) { tags { id name aliases } } }`, map[string]any{"input": input}, &out); err != nil {
		return nil, err
	}
	return out.QueryTags.Tags, nil
}

func clampPer(n int) int {
	if n <= 0 {
		return 40
	}
	return min(n, 100)
}

// WhoOf says who a video is with, from the performers' genders: "straight",
// "lesbian", "gay", "trans", "solo", or "" when not every gender is known.
func WhoOf(s Video) []string {
	if len(s.Performers) == 0 {
		return nil
	}
	female, male, trans := 0, 0, false
	for _, p := range s.Performers {
		switch {
		case p.Gender == "":
			return nil
		case strings.HasPrefix(p.Gender, "TRANSGENDER"):
			trans = true
		case p.Gender == "FEMALE":
			female++
		case p.Gender == "MALE":
			male++
		}
	}
	var out []string
	n := len(s.Performers)
	if n == 1 {
		out = append(out, "solo")
	}
	switch {
	case trans:
		out = append(out, "trans")
	case female > 0 && male > 0:
		out = append(out, "straight")
	case n > 1 && female == n:
		out = append(out, "lesbian")
	case n > 1 && male == n:
		out = append(out, "gay")
	}
	return out
}
