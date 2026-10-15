package comics

// Home fork: what happens in each issue of a series, from ComicVine (its
// short "deck", else the start of its description), for Catch me up.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// IssueSummary is one issue's number, name and what happens in it.
type IssueSummary struct {
	CVID    int64
	Number  string
	Name    string
	Summary string
}

// IssueSummaries lists a series' issues with their summaries, in ComicVine's
// order. An issue ComicVine says nothing about has an empty Summary.
func (c *ComicVine) IssueSummaries(ctx context.Context, cvID int64) ([]IssueSummary, error) {
	var out []IssueSummary
	for offset := 0; offset < 2000; offset += 100 {
		page := &cvPage{}
		if err := c.get(ctx, "/issues/", url.Values{
			"filter":     {fmt.Sprintf("volume:%d", cvID)},
			"field_list": {"id,issue_number,name,deck,description"},
			"sort":       {"id:asc"},
			"limit":      {"100"},
			"offset":     {strconv.Itoa(offset)},
		}, page); err != nil {
			return nil, err
		}
		var list []struct {
			ID          int64  `json:"id"`
			IssueNumber string `json:"issue_number"`
			Name        string `json:"name"`
			Deck        string `json:"deck"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(page.raw, &list); err != nil {
			return nil, err
		}
		for _, i := range list {
			sum := plain(i.Deck)
			if sum == "" {
				sum = plain(i.Description)
			}
			if r := []rune(sum); len(r) > 700 {
				sum = string(r[:700]) + "…"
			}
			out = append(out, IssueSummary{CVID: i.ID, Number: strings.TrimSpace(i.IssueNumber), Name: strings.TrimSpace(i.Name), Summary: strings.TrimSpace(sum)})
		}
		if len(list) < 100 || offset+100 >= page.total {
			break
		}
	}
	return out, nil
}
