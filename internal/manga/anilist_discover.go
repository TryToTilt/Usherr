package manga

// Finding manga on AniList: what's trending, popular or new, a genre's best, what's
// like a series and related to it, and the lists of a public profile. As with
// Search, nothing of this is saved until someone adds a series, and an answer is
// kept for a while in memory, so a page opened twice asks once.

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Genres are AniList's own, leaving out the adult one.
var Genres = []string{"Action", "Adventure", "Comedy", "Drama", "Ecchi", "Fantasy", "Horror", "Mahou Shoujo", "Mecha", "Music", "Mystery", "Psychological", "Romance", "Sci-Fi", "Slice of Life", "Sports", "Supernatural", "Thriller"}

// ValidGenre reports whether g is one of Genres ("" is no genre).
func ValidGenre(g string) bool {
	if g == "" {
		return true
	}
	for _, x := range Genres {
		if x == g {
			return true
		}
	}
	return false
}

// Discover's shelves.
const (
	ShelfTrending = "trending"
	ShelfPopular  = "popular"
	ShelfNew      = "new"
)

const (
	newShelfDays  = 150              // how long a series counts as new
	listKeepFor   = 15 * time.Minute // someone who just changed their list shouldn't wait hours to see it
	aboutKeepFor  = 24 * time.Hour   // genres and what's related hardly change
	mangaListMost = 200              // a very long list shouldn't become a very long page
)

// What a tile needs: no staff or store links, so a page of them stays cheap for AniList.
const tileFields = `id type title { romaji english native } synonyms format status startDate { year } volumes countryOfOrigin isAdult
	description(asHtml: false) coverImage { large } siteUrl genres averageScore`

const discoverQuery = `query ($page: Int, $genre: String, $sort: [MediaSort], $since: FuzzyDateInt) { Page(page: $page, perPage: 24) { pageInfo { hasNextPage }
	media(type: MANGA, isAdult: false, format_not: NOVEL, genre: $genre, sort: $sort, startDate_greater: $since) { ` + tileFields + ` } } }`

const aboutQuery = `query ($id: Int) { Media(id: $id, type: MANGA) { genres averageScore
	relations { edges { relationType(version: 2) node { ` + tileFields + ` } } }
	recommendations(perPage: 12, sort: RATING_DESC) { nodes { mediaRecommendation { ` + tileFields + ` } } } } }`

const userListQuery = `query ($name: String) { MediaListCollection(userName: $name, type: MANGA, status_in: [CURRENT, PLANNING]) { lists { entries { status media { ` + tileFields + ` } } } } }`

func (a *AniList) kept(key string, keep time.Duration) (cachedAnswer, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.cache[key]
	return c, ok && time.Since(c.at) < keep
}

func (a *AniList) keep(key string, c cachedAnswer) {
	c.at = time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) > 500 { // small and short-lived: start again rather than keep a list
		a.cache = map[string]cachedAnswer{}
	}
	a.cache[key] = c
}

// Discover is a page of a shelf, optionally of one genre, and whether more follow.
// Adult series and novels aren't on the shelves at all.
func (a *AniList) Discover(ctx context.Context, shelf, genre string, page int, now time.Time) ([]Found, bool, error) {
	if page < 1 {
		page = 1
	}
	key := "discover:" + shelf + ":" + genre + ":" + strconv.Itoa(page)
	if c, ok := a.kept(key, a.KeepFor); ok {
		return c.list, c.more, nil
	}
	vars := map[string]any{"page": page}
	if genre != "" {
		vars["genre"] = genre
	}
	switch shelf {
	case ShelfPopular:
		vars["sort"] = []string{"POPULARITY_DESC"}
	case ShelfNew:
		vars["sort"] = []string{"POPULARITY_DESC"}
		since := now.AddDate(0, 0, -newShelfDays)
		vars["since"] = since.Year()*10000 + int(since.Month())*100 + since.Day()
	default:
		vars["sort"] = []string{"TRENDING_DESC"}
	}
	var answer struct {
		Data struct {
			Page struct {
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
				Media []aniMedia `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := a.ask(ctx, discoverQuery, vars, &answer); err != nil {
		return nil, false, err
	}
	list := make([]Found, 0, len(answer.Data.Page.Media))
	for _, m := range answer.Data.Page.Media {
		list = append(list, m.found())
	}
	more := answer.Data.Page.PageInfo.HasNextPage
	a.keep(key, cachedAnswer{list: list, more: more})
	return list, more, nil
}

// RelatedSeries is a series connected to another, and how.
type RelatedSeries struct {
	Found
	Relation string `json:"relation"` // sequel, prequel, side, spinoff, alternative, parent
}

// About is what AniList adds to a series' page. None of it is stored.
type About struct {
	Genres  []string        `json:"genres"`
	Score   int             `json:"score,omitempty"` // 0 to 100
	Related []RelatedSeries `json:"related"`
	Similar []Found         `json:"similar"`
}

// The kinds of link worth showing. An adaptation or a character's other story
// isn't a volume you'd read next.
var relationKinds = map[string]string{
	"SEQUEL": "sequel", "PREQUEL": "prequel", "SIDE_STORY": "side", "SPIN_OFF": "spinoff", "ALTERNATIVE": "alternative", "PARENT": "parent",
}

// AboutSeries reads a series' genres, score, related series and what's like it.
func (a *AniList) AboutSeries(ctx context.Context, id int64) (About, error) {
	key := "about:" + strconv.FormatInt(id, 10)
	if c, ok := a.kept(key, aboutKeepFor); ok {
		return c.about, nil
	}
	var answer struct {
		Data struct {
			Media *struct {
				Genres       []string `json:"genres"`
				AverageScore int      `json:"averageScore"`
				Relations    struct {
					Edges []struct {
						RelationType string   `json:"relationType"`
						Node         aniMedia `json:"node"`
					} `json:"edges"`
				} `json:"relations"`
				Recommendations struct {
					Nodes []struct {
						MediaRecommendation *aniMedia `json:"mediaRecommendation"`
					} `json:"nodes"`
				} `json:"recommendations"`
			} `json:"Media"`
		} `json:"data"`
	}
	if err := a.ask(ctx, aboutQuery, map[string]any{"id": id}, &answer); err != nil {
		return About{}, err
	}
	m := answer.Data.Media
	if m == nil {
		return About{}, ErrNotFound
	}
	out := About{Genres: m.Genres, Score: m.AverageScore, Related: []RelatedSeries{}, Similar: []Found{}}
	for _, e := range m.Relations.Edges {
		kind, ok := relationKinds[e.RelationType]
		if !ok || e.Node.Type != "MANGA" || e.Node.Format == "NOVEL" {
			continue
		}
		out.Related = append(out.Related, RelatedSeries{Found: e.Node.found(), Relation: kind})
	}
	for _, n := range m.Recommendations.Nodes {
		if r := n.MediaRecommendation; r != nil && r.Type == "MANGA" && r.Format != "NOVEL" {
			out.Similar = append(out.Similar, r.found())
		}
	}
	a.keep(key, cachedAnswer{about: out})
	return out, nil
}

// ErrNoSuchList means AniList has no such profile, or its lists are private.
var ErrNoSuchList = errors.New("no such AniList list")

// UserList is what a public profile is reading and plans to read, reading first.
func (a *AniList) UserList(ctx context.Context, name string) ([]Found, error) {
	name = strings.TrimSpace(name)
	key := "list:" + strings.ToLower(name)
	if c, ok := a.kept(key, listKeepFor); ok {
		return c.list, nil
	}
	var answer struct {
		Data struct {
			Collection *struct {
				Lists []struct {
					Entries []struct {
						Status string   `json:"status"`
						Media  aniMedia `json:"media"`
					} `json:"entries"`
				} `json:"lists"`
			} `json:"MediaListCollection"`
		} `json:"data"`
	}
	if err := a.ask(ctx, userListQuery, map[string]any{"name": name}, &answer); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNoSuchList
		}
		return nil, err
	}
	if answer.Data.Collection == nil {
		return nil, ErrNoSuchList
	}
	var reading, planning []Found
	seen := map[int64]bool{}
	for _, l := range answer.Data.Collection.Lists {
		for _, e := range l.Entries {
			if seen[e.Media.ID] {
				continue
			}
			seen[e.Media.ID] = true
			f := e.Media.found()
			if e.Status == "CURRENT" {
				f.ListStatus = "reading"
				reading = append(reading, f)
			} else {
				f.ListStatus = "planning"
				planning = append(planning, f)
			}
		}
	}
	list := append(reading, planning...)
	if len(list) > mangaListMost {
		list = list[:mangaListMost]
	}
	a.keep(key, cachedAnswer{list: list})
	return list, nil
}
