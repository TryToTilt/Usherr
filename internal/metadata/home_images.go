package metadata

// Home fork: pictures for Mediarium TV's guide: a title's logo (its name as
// artwork, as on a streaming service) and wide pictures.

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"sync/atomic"
)

// posterSwaps maps a title's TMDB poster path to the poster Re-Posterr put
// on it in Jellyfin (Usherr serves it), so Usherr shows the same.
var posterSwaps atomic.Pointer[map[string]string]

// SetPosterSwaps replaces the whole map.
func SetPosterSwaps(m map[string]string) { posterSwaps.Store(&m) }

// SwappedPoster is the poster to show instead of TMDB's, if there's one.
func SwappedPoster(path string) (string, bool) {
	m := posterSwaps.Load()
	if m == nil {
		return "", false
	}
	u, ok := (*m)[path]
	return u, ok
}

// ImageURL is a TMDB picture at the given size ("w500", "w1280", ...).
func ImageURL(size, path string) string {
	if path == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/" + size + path
}

// TitleLogo is the path of a movie's or show's logo ("movie" or "tv"),
// English first, or "" when TMDB has none.
func (c *Client) TitleLogo(ctx context.Context, kind string, tmdbID int) (string, error) {
	key := fmt.Sprintf("logo:%s:%d", kind, tmdbID)
	if v, ok := c.pages.get(key); ok {
		return v.(string), nil
	}
	var out struct {
		Logos []struct {
			FilePath string  `json:"file_path"`
			Lang     string  `json:"iso_639_1"`
			Vote     float64 `json:"vote_average"`
		} `json:"logos"`
	}
	if err := c.get(ctx, fmt.Sprintf("/%s/%d/images", kind, tmdbID), url.Values{"include_image_language": {"en,null"}}, &out); err != nil {
		return "", err
	}
	logos := out.Logos
	sort.SliceStable(logos, func(i, j int) bool {
		if (logos[i].Lang == "en") != (logos[j].Lang == "en") {
			return logos[i].Lang == "en"
		}
		return logos[i].Vote > logos[j].Vote
	})
	path := ""
	if len(logos) > 0 {
		path = logos[0].FilePath
	}
	c.pages.put(key, path)
	return path, nil
}

// CollectionImages is a TMDB collection's posters and backdrops (Usherr's
// artwork picking), in the same languages as GetImages. Kept for a few
// minutes; not to be modified.
func (c *Client) CollectionImages(ctx context.Context, id int) (*Images, error) {
	key := fmt.Sprintf("images:collection:%d:%s", id, c.lang())
	if v, ok := c.pages.get(key); ok {
		return v.(*Images), nil
	}
	lang := baseLanguage(c.lang())
	include := lang + ",en,null"
	if lang == "en" {
		include = "en,null"
	}
	var img Images
	if err := c.get(ctx, fmt.Sprintf("/collection/%d/images", id), url.Values{"include_image_language": {include}, "language": {lang}}, &img); err != nil {
		return nil, err
	}
	c.pages.put(key, &img)
	return &img, nil
}
