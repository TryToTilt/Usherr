package nsfw

// Bringing the StashDB favorites of the account behind the saved key over as
// follows. It only reads from StashDB and only ever adds follows.

import (
	"context"
	"fmt"
	"strings"
)

// favoritesPerPage and favoritesMaxPages bound one read of the favorites.
const (
	favoritesPerPage  = 100
	favoritesMaxPages = 50
)

// FavoritePerformers reads the performers the key's account marked as favorite.
func (c *StashBox) FavoritePerformers(ctx context.Context) ([]Performer, error) {
	var list []Performer
	for page := 1; page <= favoritesMaxPages; page++ {
		if page > 1 {
			if err := wait(ctx, RequestGap); err != nil {
				return nil, err
			}
		}
		input := map[string]any{"is_favorite": true, "page": page, "per_page": favoritesPerPage, "sort": "NAME", "direction": "ASC"}
		var out struct {
			R struct {
				Count      int             `json:"count"`
				Performers []wirePerformer `json:"performers"`
			} `json:"queryPerformers"`
		}
		if err := c.query(ctx, `query($input: PerformerQueryInput!) { queryPerformers(input: $input) { count performers { `+performerFields+` } } }`, map[string]any{"input": input}, &out); err != nil {
			return nil, err
		}
		for _, w := range out.R.Performers {
			list = append(list, w.toPerformer())
		}
		if len(out.R.Performers) == 0 || page*favoritesPerPage >= out.R.Count {
			break
		}
	}
	return list, nil
}

// FavoriteStudios reads the studios the key's account marked as favorite.
func (c *StashBox) FavoriteStudios(ctx context.Context) ([]Studio, error) {
	var list []Studio
	for page := 1; page <= favoritesMaxPages; page++ {
		if page > 1 {
			if err := wait(ctx, RequestGap); err != nil {
				return nil, err
			}
		}
		input := map[string]any{"is_favorite": true, "page": page, "per_page": favoritesPerPage, "sort": "NAME", "direction": "ASC"}
		var out struct {
			R struct {
				Count   int          `json:"count"`
				Studios []wireStudio `json:"studios"`
			} `json:"queryStudios"`
		}
		if err := c.query(ctx, `query($input: StudioQueryInput!) { queryStudios(input: $input) { count studios { `+studioFields+` } } }`, map[string]any{"input": input}, &out); err != nil {
			return nil, err
		}
		for _, w := range out.R.Studios {
			list = append(list, w.toStudio())
		}
		if len(out.R.Studios) == 0 || page*favoritesPerPage >= out.R.Count {
			break
		}
	}
	return list, nil
}

// Favorite is one StashDB favorite as the page shows it.
type Favorite struct {
	StashID string `json:"stashId"`
	Name    string `json:"name"`
}

// FavoriteSide is what bringing in one kind of favorite would do.
type FavoriteSide struct {
	ToFollow []Favorite `json:"toFollow"`
	Already  int        `json:"already"`
	// LeftAlone are favorites followed here once and since unfollowed: Usherr
	// doesn't follow them again by itself.
	LeftAlone []Favorite `json:"leftAlone"`
}

// sourceState is what the library knows about a performer or studio.
type sourceState struct {
	Followed  bool
	CheckedAt string
}

// diffFavorites works out which favorites to follow. A row with a last check
// but no follow was followed once and then unfollowed by hand (every follow
// sets the check date, and only a follow does), so it stays as it is. Follows
// that aren't favorites never come into it: nothing is ever unfollowed.
func diffFavorites(favorites []Favorite, known map[string]sourceState) FavoriteSide {
	side := FavoriteSide{ToFollow: []Favorite{}, LeftAlone: []Favorite{}}
	for _, f := range favorites {
		st, ok := known[f.StashID]
		switch {
		case ok && st.Followed:
			side.Already++
		case ok && st.CheckedAt != "":
			side.LeftAlone = append(side.LeftAlone, f)
		default:
			side.ToFollow = append(side.ToFollow, f)
		}
	}
	return side
}

// DiffPerformerFavorites compares favorite performers with the library.
func (r *Repo) DiffPerformerFavorites(favorites []Performer) (FavoriteSide, error) {
	list, err := r.Performers(false)
	if err != nil {
		return FavoriteSide{}, err
	}
	known := map[string]sourceState{}
	for _, p := range list {
		known[p.StashID] = sourceState{p.Followed, p.CheckedAt}
	}
	favs := make([]Favorite, 0, len(favorites))
	for _, p := range favorites {
		favs = append(favs, Favorite{p.StashID, p.Name})
	}
	return diffFavorites(favs, known), nil
}

// DiffStudioFavorites compares favorite studios with the library.
func (r *Repo) DiffStudioFavorites(favorites []Studio) (FavoriteSide, error) {
	list, err := r.Studios(false)
	if err != nil {
		return FavoriteSide{}, err
	}
	known := map[string]sourceState{}
	for _, s := range list {
		known[s.StashID] = sourceState{s.Followed, s.CheckedAt}
	}
	favs := make([]Favorite, 0, len(favorites))
	for _, s := range favorites {
		favs = append(favs, Favorite{s.StashID, s.Name})
	}
	return diffFavorites(favs, known), nil
}

// FollowPerformerFromNow follows a performer with the last check set to now,
// in one step: the follow check, which adds all of a source that has never been
// checked, can't catch it halfway and add its back catalogue as wanted. No
// video is added and nothing is searched.
func (r *Repo) FollowPerformerFromNow(p Performer) error {
	_, err := r.db.Exec(`INSERT INTO nsfw_performers (stash_id, name, disambiguation, aliases, image_url, followed, checked_at) VALUES (?, ?, ?, ?, ?, 1, ?)
		ON CONFLICT(stash_id) DO UPDATE SET name = excluded.name, disambiguation = excluded.disambiguation, aliases = excluded.aliases,
			image_url = excluded.image_url, followed = 1, checked_at = excluded.checked_at`,
		p.StashID, p.Name, p.Disambiguation, toJSON(p.Aliases), p.ImageURL, now())
	if err != nil {
		return fmt.Errorf("follow a performer: %w", err)
	}
	return nil
}

// FollowStudioFromNow does the same for a studio.
func (r *Repo) FollowStudioFromNow(s Studio) error {
	_, err := r.db.Exec(`INSERT INTO nsfw_studios (stash_id, name, aliases, parent_name, image_url, followed, checked_at) VALUES (?, ?, ?, ?, ?, 1, ?)
		ON CONFLICT(stash_id) DO UPDATE SET name = excluded.name, aliases = excluded.aliases, parent_name = excluded.parent_name,
			image_url = excluded.image_url, followed = 1, checked_at = excluded.checked_at`,
		s.StashID, s.Name, toJSON(s.Aliases), s.ParentName, s.ImageURL, now())
	if err != nil {
		return fmt.Errorf("follow a studio: %w", err)
	}
	return nil
}

// IsFavoriteSchemaError reports whether a stash-box server doesn't know the
// favorites filter (an older version, or one that isn't StashDB).
func IsFavoriteSchemaError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "is_favorite")
}
