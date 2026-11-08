package manga

// Browsing MangaDex, for the Manga > MangaDex page: titles by a query, the latest
// chapters, staff lists, tags, a random title, one chapter, and covers. All of it
// goes through the same client and pace as the chapter downloads (mangadex.go), so
// browsing can never push Usherr past MangaDex's limits. No account, no
// credentials. The API layer (internal/api/home_manga_browse.go) keeps answers for a
// few minutes, so many people browsing don't multiply the calls.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	dexUploadsURL = "https://uploads.mangadex.org"
	dexCoverGap   = 150 * time.Millisecond
	dexMaxCover   = 8 << 20

	// DexStaffPicks is the MangaDex staff's own public "Staff Picks" list, the one the
	// site's Recommended row shows. Its owner's other public lists hold the seasonal
	// and self-published picks, so this one id finds them all.
	DexStaffPicks = "805ba886-dd99-4aa4-b460-4bd7c7b71352"
)

// SafeRatings are the content ratings asked for unless someone chose otherwise.
var SafeRatings = []string{"safe", "suggestive"}

// AllRatings adds the adult ones, for an account allowed them that asked.
var AllRatings = []string{"safe", "suggestive", "erotica", "pornographic"}

// DexTitle is a series as the MangaDex page shows it.
type DexTitle struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	AltTitles        []string `json:"altTitles,omitempty"`
	Description      string   `json:"description,omitempty"`
	Year             int      `json:"year,omitempty"`
	Status           string   `json:"status,omitempty"`
	Rating           string   `json:"contentRating,omitempty"`
	Demographic      string   `json:"demographic,omitempty"`
	OriginalLanguage string   `json:"originalLanguage,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	CoverFile        string   `json:"coverFile,omitempty"`
	Authors          []string `json:"authors,omitempty"`
	AniListID        int64    `json:"anilistId,omitempty"`
	Languages        []string `json:"languages,omitempty"`
	LastChapter      string   `json:"lastChapter,omitempty"`
	LicensedLink     string   `json:"licensedLink,omitempty"`
	Follows          int      `json:"follows,omitempty"`
	Score            float64  `json:"score,omitempty"` // MangaDex's own rating, 1 to 10
}

// DexUpdate is a chapter in the latest updates, with the series it belongs to.
type DexUpdate struct {
	ChapterID  string     `json:"chapterId"`
	MangaID    string     `json:"mangaId"`
	Number     string     `json:"number,omitempty"`
	Volume     string     `json:"volume,omitempty"`
	Title      string     `json:"title,omitempty"`
	Language   string     `json:"language"`
	Groups     []DexGroup `json:"groups,omitempty"`
	ReadableAt string     `json:"readableAt,omitempty"`
	External   bool       `json:"external,omitempty"`
	Comments   int        `json:"comments,omitempty"`
	Manga      *DexTitle  `json:"manga,omitempty"`
}

// DexTag is one of MangaDex's tags.
type DexTag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"` // genre, theme, format, content
}

// DexList is a public custom list.
type DexList struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Owner  string   `json:"-"`
	Titles []string `json:"-"`
}

// DexQuery is a search of titles. Empty fields ask nothing of their own.
type DexQuery struct {
	Title        string
	IncludedTags []string
	ExcludedTags []string
	Status       []string // ongoing, completed, hiatus, cancelled
	Demographic  []string // shounen, shoujo, seinen, josei, none
	Original     []string // ja, ko, zh, en...
	Year         int
	Order        string // relevance, followedCount, latestUploadedChapter, createdAt, rating, year, title
	Ascending    bool
	Translated   string // has chapters in this language
	HasChapters  bool
	CreatedSince time.Time
	IDs          []string
	Ratings      []string
	Limit        int
	Offset       int
}

var (
	dexOrders  = map[string]bool{"relevance": true, "followedCount": true, "latestUploadedChapter": true, "createdAt": true, "updatedAt": true, "rating": true, "year": true, "title": true}
	dexStatus  = map[string]bool{"ongoing": true, "completed": true, "hiatus": true, "cancelled": true}
	dexDemo    = map[string]bool{"shounen": true, "shoujo": true, "seinen": true, "josei": true, "none": true}
	dexRating  = map[string]bool{"safe": true, "suggestive": true, "erotica": true, "pornographic": true}
	dexLangRe  = regexp.MustCompile(`^[a-z]{2}(-[a-z]{2})?$`)
	dexCoverRe = regexp.MustCompile(`^[0-9a-fA-F-]{36}\.(jpg|jpeg|png|gif|webp)$`)
)

// ValidDexOrder, ValidDexStatus... keep what reaches MangaDex to what it knows.
func ValidDexOrder(o string) bool       { return dexOrders[o] }
func ValidDexStatus(s string) bool      { return dexStatus[s] }
func ValidDexDemographic(d string) bool { return dexDemo[d] }
func ValidDexLanguage(l string) bool    { return dexLangRe.MatchString(l) }

// ValidCoverFile reports whether f is a cover's file name as MangaDex gives it.
func ValidCoverFile(f string) bool { return dexCoverRe.MatchString(f) }

// Values turns a query into MangaDex's parameters.
func (q DexQuery) Values() url.Values {
	v := url.Values{"includes[]": {"cover_art", "author"}}
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 24
	}
	offset := max(q.Offset, 0)
	if offset+limit > dexMaxOffset {
		offset = max(dexMaxOffset-limit, 0)
	}
	v.Set("limit", strconv.Itoa(limit))
	v.Set("offset", strconv.Itoa(offset))
	if t := strings.TrimSpace(q.Title); t != "" {
		v.Set("title", t)
	}
	add := func(key string, list []string, ok func(string) bool) {
		for _, s := range list {
			if ok(s) {
				v.Add(key, s)
			}
		}
	}
	add("includedTags[]", q.IncludedTags, IsDexID)
	add("excludedTags[]", q.ExcludedTags, IsDexID)
	add("status[]", q.Status, ValidDexStatus)
	add("publicationDemographic[]", q.Demographic, ValidDexDemographic)
	add("originalLanguage[]", q.Original, ValidDexLanguage)
	add("ids[]", q.IDs, IsDexID)
	ratings := q.Ratings
	if len(ratings) == 0 {
		ratings = SafeRatings
	}
	add("contentRating[]", ratings, func(s string) bool { return dexRating[s] })
	if q.Year > 0 {
		v.Set("year", strconv.Itoa(q.Year))
	}
	if ValidDexLanguage(q.Translated) {
		v.Set("availableTranslatedLanguage[]", q.Translated)
	}
	if q.HasChapters {
		v.Set("hasAvailableChapters", "true")
	}
	if !q.CreatedSince.IsZero() {
		v.Set("createdAtSince", q.CreatedSince.UTC().Format("2006-01-02T15:04:05"))
	}
	order := q.Order
	if !ValidDexOrder(order) {
		order = "followedCount"
	}
	if order == "relevance" && v.Get("title") == "" {
		order = "followedCount" // relevance needs words to be relevant to
	}
	dir := "desc"
	if q.Ascending {
		dir = "asc"
	}
	v.Set("order["+order+"]", dir)
	return v
}

type dexTitleJSON struct {
	ID         string `json:"id"`
	Attributes struct {
		Title            map[string]string   `json:"title"`
		AltTitles        []map[string]string `json:"altTitles"`
		Description      map[string]string   `json:"description"`
		Links            map[string]string   `json:"links"`
		Year             int                 `json:"year"`
		Status           string              `json:"status"`
		Rating           string              `json:"contentRating"`
		Demographic      string              `json:"publicationDemographic"`
		OriginalLanguage string              `json:"originalLanguage"`
		LastChapter      string              `json:"lastChapter"`
		Languages        []string            `json:"availableTranslatedLanguages"`
		Tags             []struct {
			Attributes struct {
				Name  map[string]string `json:"name"`
				Group string            `json:"group"`
			} `json:"attributes"`
		} `json:"tags"`
	} `json:"attributes"`
	Relationships []struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes *struct {
			Name     string `json:"name"`
			FileName string `json:"fileName"`
		} `json:"attributes"`
	} `json:"relationships"`
}

func (j dexTitleJSON) title() DexTitle {
	a := j.Attributes
	base := dexMangaJSON{ID: j.ID}
	base.Attributes.Title, base.Attributes.AltTitles, base.Attributes.Links = a.Title, a.AltTitles, a.Links
	s := base.series()
	out := DexTitle{
		ID: j.ID, Title: s.Title, AltTitles: s.AltTitles, Year: a.Year, Status: a.Status, Rating: a.Rating,
		Demographic: a.Demographic, OriginalLanguage: a.OriginalLanguage, LastChapter: a.LastChapter, Languages: a.Languages,
		AniListID: s.AniListID, LicensedLink: safeLink(a.Links["engtl"]),
	}
	if len(out.AltTitles) > 6 {
		out.AltTitles = out.AltTitles[:6]
	}
	out.Description = strings.TrimSpace(a.Description["en"])
	if out.Description == "" {
		for _, k := range sortedKeys(a.Description) {
			if d := strings.TrimSpace(a.Description[k]); d != "" {
				out.Description = d
				break
			}
		}
	}
	if r := []rune(out.Description); len(r) > 1500 {
		out.Description = string(r[:1500]) + "…"
	}
	for _, t := range a.Tags {
		if n := strings.TrimSpace(t.Attributes.Name["en"]); n != "" {
			out.Tags = append(out.Tags, n)
		}
	}
	for _, r := range j.Relationships {
		if r.Attributes == nil {
			continue
		}
		switch r.Type {
		case "cover_art":
			if ValidCoverFile(r.Attributes.FileName) {
				out.CoverFile = r.Attributes.FileName
			}
		case "author", "artist":
			if n := strings.TrimSpace(r.Attributes.Name); n != "" && !contains(out.Authors, n) {
				out.Authors = append(out.Authors, n)
			}
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Titles runs a search, and says how many there are in all.
func (c *MangaDex) Titles(ctx context.Context, q DexQuery) ([]DexTitle, int, error) {
	var answer struct {
		Data  []dexTitleJSON `json:"data"`
		Total int            `json:"total"`
	}
	if err := c.get(ctx, "/manga", q.Values(), &c.nextAPI, c.APIGap, &answer); err != nil {
		return nil, 0, err
	}
	out := make([]DexTitle, 0, len(answer.Data))
	for _, j := range answer.Data {
		out = append(out, j.title())
	}
	return out, answer.Total, nil
}

// TitlesByID reads up to 100 titles, in the order asked, leaving out any not
// answered (taken down, or of a rating not asked for).
func (c *MangaDex) TitlesByID(ctx context.Context, ids []string, ratings []string) ([]DexTitle, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 100 {
		ids = ids[:100]
	}
	found, _, err := c.Titles(ctx, DexQuery{IDs: ids, Ratings: ratings, Limit: 100, Order: "followedCount"})
	if err != nil {
		return nil, err
	}
	by := map[string]DexTitle{}
	for _, t := range found {
		by[t.ID] = t
	}
	out := make([]DexTitle, 0, len(found))
	for _, id := range ids {
		if t, ok := by[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// Title reads one series, with its cover and authors.
func (c *MangaDex) Title(ctx context.Context, id string) (DexTitle, error) {
	if !IsDexID(id) {
		return DexTitle{}, ErrNotFound
	}
	var answer struct {
		Data dexTitleJSON `json:"data"`
	}
	q := url.Values{"includes[]": {"cover_art", "author", "artist"}}
	if err := c.get(ctx, "/manga/"+id, q, &c.nextAPI, c.APIGap, &answer); err != nil {
		return DexTitle{}, err
	}
	return answer.Data.title(), nil
}

// Random is one title picked by MangaDex, of the ratings given.
func (c *MangaDex) Random(ctx context.Context, ratings []string) (DexTitle, error) {
	q := url.Values{"includes[]": {"cover_art", "author"}}
	for _, r := range ratings {
		if dexRating[r] {
			q.Add("contentRating[]", r)
		}
	}
	var answer struct {
		Data dexTitleJSON `json:"data"`
	}
	if err := c.get(ctx, "/manga/random", q, &c.nextAPI, c.APIGap, &answer); err != nil {
		return DexTitle{}, err
	}
	return answer.Data.title(), nil
}

// Latest are the newest readable chapters in a language, newest first, the way
// the site's Latest Updates are made: /chapter by readableAt.
func (c *MangaDex) Latest(ctx context.Context, language string, ratings []string, limit int) ([]DexUpdate, error) {
	if !ValidDexLanguage(language) {
		language = "en"
	}
	if limit <= 0 || limit > 100 {
		limit = 32
	}
	q := url.Values{
		"limit": {strconv.Itoa(limit)}, "translatedLanguage[]": {language}, "order[readableAt]": {"desc"},
		"includes[]": {"scanlation_group"}, "includeFutureUpdates": {"0"}, "includeEmptyPages": {"0"}, "includeExternalUrl": {"0"},
	}
	for _, r := range ratings {
		if dexRating[r] {
			q.Add("contentRating[]", r)
		}
	}
	var answer struct {
		Data []dexChapterLatestJSON `json:"data"`
	}
	if err := c.get(ctx, "/chapter", q, &c.nextAPI, c.APIGap, &answer); err != nil {
		return nil, err
	}
	out := make([]DexUpdate, 0, len(answer.Data))
	for _, j := range answer.Data {
		ch := j.dexChapterJSON.chapter()
		u := DexUpdate{ChapterID: ch.ID, Number: ch.Number, Volume: ch.Volume, Title: ch.Title, Language: ch.Language, Groups: ch.Groups, ReadableAt: j.Attributes.ReadableAt, External: ch.ExternalURL != ""}
		for _, r := range j.Relationships {
			if r.Type == "manga" {
				u.MangaID = r.ID
			}
		}
		if u.MangaID != "" {
			out = append(out, u)
		}
	}
	return out, nil
}

type dexChapterLatestJSON struct {
	dexChapterJSON
	Attributes struct {
		ReadableAt string `json:"readableAt"`
	} `json:"attributes"`
}

// UnmarshalJSON fills both views of the attributes.
func (j *dexChapterLatestJSON) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &j.dexChapterJSON); err != nil {
		return err
	}
	var extra struct {
		Attributes struct {
			ReadableAt string `json:"readableAt"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(b, &extra); err != nil {
		return err
	}
	j.Attributes.ReadableAt = extra.Attributes.ReadableAt
	return nil
}

// DexChapterInfo is one chapter, with what the reader says about it.
type DexChapterInfo struct {
	DexChapter
	MangaID string
}

// Chapter reads one chapter, its groups and its series.
func (c *MangaDex) Chapter(ctx context.Context, id string) (DexChapterInfo, error) {
	if !IsDexID(id) {
		return DexChapterInfo{}, ErrNotFound
	}
	var answer struct {
		Data dexChapterJSON `json:"data"`
	}
	q := url.Values{"includes[]": {"scanlation_group"}}
	if err := c.get(ctx, "/chapter/"+id, q, &c.nextAPI, c.APIGap, &answer); err != nil {
		return DexChapterInfo{}, err
	}
	out := DexChapterInfo{DexChapter: answer.Data.chapter()}
	for _, r := range answer.Data.Relationships {
		if r.Type == "manga" {
			out.MangaID = r.ID
		}
	}
	return out, nil
}

// List reads a public custom list: its name, owner and titles.
func (c *MangaDex) List(ctx context.Context, id string) (DexList, error) {
	if !IsDexID(id) {
		return DexList{}, ErrNotFound
	}
	var answer struct {
		Data dexListJSON `json:"data"`
	}
	if err := c.get(ctx, "/list/"+id, nil, &c.nextAPI, c.APIGap, &answer); err != nil {
		return DexList{}, err
	}
	return answer.Data.list(), nil
}

// UserLists are someone's public lists, newest last as MangaDex keeps them.
func (c *MangaDex) UserLists(ctx context.Context, userID string) ([]DexList, error) {
	if !IsDexID(userID) {
		return nil, ErrNotFound
	}
	var answer struct {
		Data []dexListJSON `json:"data"`
	}
	if err := c.get(ctx, "/user/"+userID+"/list", url.Values{"limit": {"100"}}, &c.nextAPI, c.APIGap, &answer); err != nil {
		return nil, err
	}
	out := make([]DexList, 0, len(answer.Data))
	for _, j := range answer.Data {
		out = append(out, j.list())
	}
	return out, nil
}

type dexListJSON struct {
	ID         string `json:"id"`
	Attributes struct {
		Name       string `json:"name"`
		Visibility string `json:"visibility"`
	} `json:"attributes"`
	Relationships []dexRelation `json:"relationships"`
}

func (j dexListJSON) list() DexList {
	out := DexList{ID: j.ID, Name: strings.TrimSpace(j.Attributes.Name)}
	for _, r := range j.Relationships {
		switch r.Type {
		case "manga":
			out.Titles = append(out.Titles, r.ID)
		case "user":
			out.Owner = r.ID
		}
	}
	return out
}

// PickLists finds the seasonal, recommended and self-published lists among the
// staff's lists by their names. The newest seasonal list wins.
func PickLists(lists []DexList) (seasonal, recommended, self DexList) {
	for _, l := range lists {
		n := strings.ToLower(l.Name)
		switch {
		case strings.HasPrefix(n, "seasonal"):
			seasonal = l // the later one is the newer season
		case strings.Contains(n, "self-published") || strings.Contains(n, "self published"):
			self = l
		case strings.Contains(n, "staff pick") || strings.Contains(n, "recommended"):
			recommended = l
		}
	}
	return seasonal, recommended, self
}

// Tags are MangaDex's tags.
func (c *MangaDex) Tags(ctx context.Context) ([]DexTag, error) {
	var answer struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Name  map[string]string `json:"name"`
				Group string            `json:"group"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/manga/tag", nil, &c.nextAPI, c.APIGap, &answer); err != nil {
		return nil, err
	}
	out := make([]DexTag, 0, len(answer.Data))
	for _, t := range answer.Data {
		if n := strings.TrimSpace(t.Attributes.Name["en"]); n != "" && IsDexID(t.ID) {
			out = append(out, DexTag{ID: t.ID, Name: n, Group: t.Attributes.Group})
		}
	}
	return out, nil
}

// TitleStats are follows and the rating, for up to 100 titles.
func (c *MangaDex) TitleStats(ctx context.Context, ids []string) (map[string]DexTitle, error) {
	out := map[string]DexTitle{}
	q := url.Values{}
	for i, id := range ids {
		if i == 100 {
			break
		}
		if IsDexID(id) {
			q.Add("manga[]", id)
		}
	}
	if len(q) == 0 {
		return out, nil
	}
	var answer struct {
		Statistics map[string]struct {
			Follows int `json:"follows"`
			Rating  struct {
				Bayesian *float64 `json:"bayesian"`
			} `json:"rating"`
		} `json:"statistics"`
	}
	if err := c.get(ctx, "/statistics/manga", q, &c.nextAPI, c.APIGap, &answer); err != nil {
		return nil, err
	}
	for id, st := range answer.Statistics {
		t := DexTitle{Follows: st.Follows}
		if st.Rating.Bayesian != nil {
			t.Score = float64(int(*st.Rating.Bayesian*100)) / 100
		}
		out[id] = t
	}
	return out, nil
}

// ChapterComments are how many comments each chapter has.
func (c *MangaDex) ChapterComments(ctx context.Context, ids []string) (map[string]int, error) {
	out := map[string]int{}
	q := url.Values{}
	for i, id := range ids {
		if i == 100 {
			break
		}
		if IsDexID(id) {
			q.Add("chapter[]", id)
		}
	}
	if len(q) == 0 {
		return out, nil
	}
	var answer struct {
		Statistics map[string]struct {
			Comments *struct {
				Replies int `json:"repliesCount"`
			} `json:"comments"`
		} `json:"statistics"`
	}
	if err := c.get(ctx, "/statistics/chapter", q, &c.nextAPI, c.APIGap, &answer); err != nil {
		return nil, err
	}
	for id, st := range answer.Statistics {
		if st.Comments != nil {
			out[id] = st.Comments.Replies
		}
	}
	return out, nil
}

// Cover fetches a series' cover at a size (256 or 512 wide, 0 the original) into
// w, and says its type. No login, no cookies: covers are plain files.
func (c *MangaDex) Cover(ctx context.Context, mangaID, file string, size int, w io.Writer) (string, error) {
	if !IsDexID(mangaID) || !ValidCoverFile(file) {
		return "", ErrNotFound
	}
	if err := c.turn(ctx, &c.nextCover, dexCoverGap); err != nil {
		return "", err
	}
	target := strings.TrimRight(c.Uploads, "/") + "/covers/" + mangaID + "/" + file
	if size == 256 || size == 512 {
		target += fmt.Sprintf(".%d.jpg", size)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.Images.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") || strings.Contains(ct, "svg") {
		return "", fmt.Errorf("the cover couldn't be fetched (%d)", resp.StatusCode)
	}
	_, err = io.Copy(w, io.LimitReader(resp.Body, dexMaxCover))
	return ct, err
}
