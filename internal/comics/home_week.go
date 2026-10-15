package comics

// Home fork: the week's new comics (after Pullbox's "What's New"), from
// ComicVine's store dates: every issue on sale in a week, with its series'
// publisher, so the page can filter to the publishers worth following.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// WeekIssue is an issue on sale in a given week.
type WeekIssue struct {
	CVID       int64  `json:"cvId"`
	VolumeCVID int64  `json:"volumeCvId"`
	Series     string `json:"series"`
	StartYear  int    `json:"startYear,omitempty"`
	Publisher  string `json:"publisher,omitempty"`
	Number     string `json:"number"`
	Title      string `json:"title,omitempty"`
	StoreDate  string `json:"storeDate"`
	CoverDate  string `json:"coverDate,omitempty"`
	ImageURL   string `json:"imageUrl,omitempty"`
}

// WeekOf is the Monday-to-Sunday week holding day, as YYYY-MM-DD dates.
func WeekOf(day time.Time) (from, to string) {
	off := (int(day.Weekday()) + 6) % 7 // days since Monday
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -off)
	return start.Format("2006-01-02"), start.AddDate(0, 0, 6).Format("2006-01-02")
}

// StoreDateIssues lists every issue on sale between from and to (inclusive),
// with its series' publisher and first year.
func (c *ComicVine) StoreDateIssues(ctx context.Context, from, to string) ([]WeekIssue, error) {
	type cvWeekIssue struct {
		cvIssue
		Volume *struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"volume"`
	}
	var out []WeekIssue
	for offset := 0; offset < 3000; offset += 100 {
		page := &cvPage{}
		if err := c.get(ctx, "/issues/", url.Values{
			"filter":     {fmt.Sprintf("store_date:%s|%s", from, to)},
			"field_list": {"id,issue_number,name,cover_date,store_date,image,volume"},
			"sort":       {"store_date:asc"},
			"limit":      {"100"},
			"offset":     {strconv.Itoa(offset)},
		}, page); err != nil {
			return nil, err
		}
		var list []cvWeekIssue
		if err := json.Unmarshal(page.raw, &list); err != nil {
			return nil, err
		}
		for _, i := range list {
			if i.Volume == nil {
				continue
			}
			out = append(out, WeekIssue{CVID: i.ID, VolumeCVID: i.Volume.ID, Series: strings.TrimSpace(i.Volume.Name),
				Number: strings.TrimSpace(i.IssueNumber), Title: strings.TrimSpace(i.Name),
				StoreDate: i.StoreDate, CoverDate: i.CoverDate, ImageURL: i.Image.url()})
		}
		if len(list) < 100 || offset+100 >= page.total {
			break
		}
	}
	// The issue list has no publishers: ask for the series, 100 at a time.
	vols := map[int64]bool{}
	var ids []int64
	for _, w := range out {
		if !vols[w.VolumeCVID] {
			vols[w.VolumeCVID] = true
			ids = append(ids, w.VolumeCVID)
		}
	}
	info := map[int64]cvVolume{}
	for len(ids) > 0 {
		n := min(100, len(ids))
		parts := make([]string, n)
		for k, id := range ids[:n] {
			parts[k] = strconv.FormatInt(id, 10)
		}
		ids = ids[n:]
		page := &cvPage{}
		if err := c.get(ctx, "/volumes/", url.Values{
			"filter":     {"id:" + strings.Join(parts, "|")},
			"field_list": {"id,start_year,publisher"},
			"limit":      {"100"},
		}, page); err != nil {
			return out, nil // the issues are still useful without publishers
		}
		var vs []cvVolume
		if json.Unmarshal(page.raw, &vs) == nil {
			for _, v := range vs {
				info[v.ID] = v
			}
		}
	}
	for k := range out {
		if v, ok := info[out[k].VolumeCVID]; ok {
			out[k].StartYear, _ = strconv.Atoi(strings.TrimSpace(v.StartYear))
			if v.Publisher != nil {
				out[k].Publisher = v.Publisher.Name
			}
		}
	}
	return out, nil
}
