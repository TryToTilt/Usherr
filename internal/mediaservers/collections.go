package mediaservers

// Usherr: collections pushed to Jellyfin and Plex (internal/api/home_collections.go),
// and what Usherr needs to put badges on posters (internal/api/home_badges.go):
// each library title with its ids, genres, rating, when it was added, its
// poster's tag (which changes when the poster does) and what its file holds.
//
// Jellyfin: a collection is a "BoxSet" made with POST /Collections; titles go
// in and out with POST and DELETE /Collections/{id}/Items. Plex: a collection
// belongs to one library (and holds films or shows), made with POST
// /library/collections and a server:// address naming the titles.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ServerCollection is a collection a media server has.
type ServerCollection struct {
	ID    string
	Title string
}

// IsNotFound says whether the server answered that the thing asked for isn't
// there (a collection deleted by hand).
func IsNotFound(err error) bool { return errors.Is(err, errNotFound) }

// collBatch is how many titles one request adds or takes out (the ids travel
// in the address).
const collBatch = 80

func batches(ids []string) [][]string {
	var out [][]string
	for i := 0; i < len(ids); i += collBatch {
		out = append(out, ids[i:min(i+collBatch, len(ids))])
	}
	return out
}

// ---- Jellyfin (and Emby) ----

// JellyfinCollections are the server's collections.
func (c *Client) JellyfinCollections(ctx context.Context, s Server) ([]ServerCollection, error) {
	var page struct {
		Items []struct {
			ID   string `json:"Id"`
			Name string `json:"Name"`
		} `json:"Items"`
	}
	q := url.Values{"IncludeItemTypes": {"BoxSet"}, "Recursive": {"true"}}
	if err := c.do(ctx, s, http.MethodGet, "/Items", q, nil, &page); err != nil {
		return nil, explain(s, err)
	}
	out := make([]ServerCollection, 0, len(page.Items))
	for _, it := range page.Items {
		out = append(out, ServerCollection{ID: it.ID, Title: it.Name})
	}
	return out, nil
}

// JellyfinCollectionItems are the ids of the titles in a collection. A
// collection that is gone answers an error IsNotFound knows.
func (c *Client) JellyfinCollectionItems(ctx context.Context, s Server, id string) ([]string, error) {
	var page struct {
		Items []struct {
			ID string `json:"Id"`
		} `json:"Items"`
	}
	q := url.Values{"ParentId": {id}}
	if err := c.do(ctx, s, http.MethodGet, "/Items", q, nil, &page); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		out = append(out, it.ID)
	}
	return out, nil
}

// JellyfinCreateCollection makes a collection holding the titles and says
// its id.
func (c *Client) JellyfinCreateCollection(ctx context.Context, s Server, name string, ids []string) (string, error) {
	first := ids
	if len(first) > collBatch {
		first = ids[:collBatch]
	}
	q := url.Values{"Name": {name}}
	if len(first) > 0 {
		q.Set("Ids", strings.Join(first, ","))
	}
	var made struct {
		ID string `json:"Id"`
	}
	if err := c.do(ctx, s, http.MethodPost, "/Collections", q, nil, &made); err != nil {
		return "", explain(s, err)
	}
	if made.ID == "" {
		return "", fmt.Errorf("%s made the collection but didn't say its id", s.Kind.Label())
	}
	if len(ids) > len(first) {
		if err := c.JellyfinCollectionAdd(ctx, s, made.ID, ids[len(first):]); err != nil {
			return made.ID, err
		}
	}
	return made.ID, nil
}

// JellyfinCollectionAdd puts titles in a collection.
func (c *Client) JellyfinCollectionAdd(ctx context.Context, s Server, id string, ids []string) error {
	for _, b := range batches(ids) {
		if err := c.do(ctx, s, http.MethodPost, "/Collections/"+url.PathEscape(id)+"/Items", url.Values{"Ids": {strings.Join(b, ",")}}, nil, nil); err != nil {
			return explain(s, err)
		}
	}
	return nil
}

// JellyfinCollectionRemove takes titles out of a collection.
func (c *Client) JellyfinCollectionRemove(ctx context.Context, s Server, id string, ids []string) error {
	for _, b := range batches(ids) {
		if err := c.do(ctx, s, http.MethodDelete, "/Collections/"+url.PathEscape(id)+"/Items", url.Values{"Ids": {strings.Join(b, ",")}}, nil, nil); err != nil {
			return explain(s, err)
		}
	}
	return nil
}

// JellyfinCollectionNames sets a collection's name and the name it's sorted
// by ("" Jellyfin's own). Jellyfin takes the whole item back, so it's read
// first and sent back with the change.
func (c *Client) JellyfinCollectionNames(ctx context.Context, s Server, id, name, sortName string) error {
	var page struct {
		Items []map[string]any `json:"Items"`
	}
	q := url.Values{"Ids": {id}, "Fields": {"Overview,Genres,Tags,ProviderIds,Studios,People,SortName,ForcedSortName,LockedFields,LockData,DateCreated,PremiereDate,ProductionYear,OfficialRating,CustomRating,Taglines"}}
	if err := c.do(ctx, s, http.MethodGet, "/Items", q, nil, &page); err != nil {
		return explain(s, err)
	}
	if len(page.Items) == 0 {
		return errNotFound
	}
	item := page.Items[0]
	item["Name"] = name
	item["ForcedSortName"] = sortName
	if err := c.do(ctx, s, http.MethodPost, "/Items/"+url.PathEscape(id), nil, item, nil); err != nil {
		return explain(s, err)
	}
	return nil
}

// JellyfinDeleteItem deletes an item (a collection: its titles stay).
func (c *Client) JellyfinDeleteItem(ctx context.Context, s Server, id string) error {
	if err := c.do(ctx, s, http.MethodDelete, "/Items/"+url.PathEscape(id), nil, nil, nil); err != nil && !IsNotFound(err) {
		return explain(s, err)
	}
	return nil
}

// ---- Plex ----

// PlexMachineID is the server's machineIdentifier (the one saved, else
// asked).
func (c *Client) PlexMachineID(ctx context.Context, s Server) (string, error) {
	if s.ServerID != "" {
		return s.ServerID, nil
	}
	var doc plexDoc
	if err := c.do(ctx, s, http.MethodGet, "/identity", nil, nil, &doc); err != nil {
		return "", explain(s, err)
	}
	id := doc.container().MachineIdentifier
	if id == "" {
		return "", errors.New("Plex didn't say its machine id")
	}
	return id, nil
}

// plexItemsURI names titles of the server for a collection.
func plexItemsURI(machine string, keys []string) string {
	return "server://" + machine + "/com.plexapp.plugins.library/library/metadata/" + strings.Join(keys, ",")
}

// PlexCollections are a library's collections.
func (c *Client) PlexCollections(ctx context.Context, s Server, section string) ([]ServerCollection, error) {
	var doc struct {
		MediaContainer struct {
			Metadata []struct {
				RatingKey string `json:"ratingKey"`
				Title     string `json:"title"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.do(ctx, s, http.MethodGet, "/library/sections/"+url.PathEscape(section)+"/collections", nil, nil, &doc); err != nil {
		return nil, explain(s, err)
	}
	var out []ServerCollection
	for _, m := range doc.MediaContainer.Metadata {
		out = append(out, ServerCollection{ID: m.RatingKey, Title: m.Title})
	}
	return out, nil
}

// PlexCollectionItems are the rating keys of the titles in a collection. A
// collection that is gone answers an error IsNotFound knows.
func (c *Client) PlexCollectionItems(ctx context.Context, s Server, key string) ([]string, error) {
	var doc struct {
		MediaContainer struct {
			Metadata []struct {
				RatingKey string `json:"ratingKey"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.do(ctx, s, http.MethodGet, "/library/collections/"+url.PathEscape(key)+"/children", nil, nil, &doc); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range doc.MediaContainer.Metadata {
		out = append(out, m.RatingKey)
	}
	return out, nil
}

// plexTypeNum is Plex's number for a library's kind of title.
func plexTypeNum(kind string) string {
	if kind == "show" {
		return "2"
	}
	return "1"
}

// PlexCreateCollection makes a collection in the library (kind: movie or
// show) holding the titles, and says its rating key.
func (c *Client) PlexCreateCollection(ctx context.Context, s Server, section, kind, title string, keys []string) (string, error) {
	if len(keys) == 0 {
		return "", errors.New("a Plex collection needs a title in it")
	}
	machine, err := c.PlexMachineID(ctx, s)
	if err != nil {
		return "", err
	}
	first := keys
	if len(first) > collBatch {
		first = keys[:collBatch]
	}
	q := url.Values{"type": {plexTypeNum(kind)}, "title": {title}, "smart": {"0"}, "sectionId": {section}, "uri": {plexItemsURI(machine, first)}}
	var doc struct {
		MediaContainer struct {
			Metadata []struct {
				RatingKey string `json:"ratingKey"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.do(ctx, s, http.MethodPost, "/library/collections", q, nil, &doc); err != nil {
		return "", explain(s, err)
	}
	if len(doc.MediaContainer.Metadata) == 0 || doc.MediaContainer.Metadata[0].RatingKey == "" {
		return "", errors.New("Plex made the collection but didn't say its key")
	}
	key := doc.MediaContainer.Metadata[0].RatingKey
	if len(keys) > len(first) {
		if err := c.PlexCollectionAdd(ctx, s, key, keys[len(first):]); err != nil {
			return key, err
		}
	}
	return key, nil
}

// PlexCollectionAdd puts titles in a collection.
func (c *Client) PlexCollectionAdd(ctx context.Context, s Server, key string, keys []string) error {
	machine, err := c.PlexMachineID(ctx, s)
	if err != nil {
		return err
	}
	for _, b := range batches(keys) {
		if err := c.do(ctx, s, http.MethodPut, "/library/collections/"+url.PathEscape(key)+"/items", url.Values{"uri": {plexItemsURI(machine, b)}}, nil, nil); err != nil {
			return explain(s, err)
		}
	}
	return nil
}

// PlexCollectionRemove takes one title out of a collection.
func (c *Client) PlexCollectionRemove(ctx context.Context, s Server, key, item string) error {
	if err := c.do(ctx, s, http.MethodDelete, "/library/collections/"+url.PathEscape(key)+"/items/"+url.PathEscape(item), nil, nil, nil); err != nil && !IsNotFound(err) {
		return explain(s, err)
	}
	return nil
}

// PlexCollectionNames sets a collection's title and the title it's sorted
// by ("" the title), locked so Plex keeps them.
func (c *Client) PlexCollectionNames(ctx context.Context, s Server, section, key, title, sortTitle string) error {
	if sortTitle == "" {
		sortTitle = title
	}
	q := url.Values{"type": {"18"}, "id": {key}, "title.value": {title}, "title.locked": {"1"}, "titleSort.value": {sortTitle}, "titleSort.locked": {"1"}}
	if err := c.do(ctx, s, http.MethodPut, "/library/sections/"+url.PathEscape(section)+"/all", q, nil, nil); err != nil {
		return explain(s, err)
	}
	return nil
}

// PlexCollectionVisibility says where Plex shows the collection: on the
// library's Recommended tab, on the owner's Home, on friends' Home.
func (c *Client) PlexCollectionVisibility(ctx context.Context, s Server, section, key string, library, home, shared bool) error {
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	q := url.Values{"promotedToRecommended": {b(library)}, "promotedToOwnHome": {b(home)}, "promotedToSharedHome": {b(shared)}}
	var doc struct {
		MediaContainer struct {
			Hub []struct {
				Identifier string `json:"identifier"`
			} `json:"Hub"`
		} `json:"MediaContainer"`
	}
	base := "/hubs/sections/" + url.PathEscape(section) + "/manage"
	_ = c.do(ctx, s, http.MethodGet, base, url.Values{"metadataItemId": {key}}, nil, &doc)
	if len(doc.MediaContainer.Hub) > 0 && doc.MediaContainer.Hub[0].Identifier != "" {
		if err := c.do(ctx, s, http.MethodPut, base+"/"+url.PathEscape(doc.MediaContainer.Hub[0].Identifier), q, nil, nil); err != nil {
			return explain(s, err)
		}
		return nil
	}
	if !library && !home && !shared {
		return nil // never shown anywhere: nothing to make
	}
	q.Set("metadataItemId", key)
	if err := c.do(ctx, s, http.MethodPost, base, q, nil, nil); err != nil {
		return explain(s, err)
	}
	return nil
}

// PlexSetPoster uploads a poster for any item (a collection too), without
// the lock PlexSetImage adds for films and shows.
func (c *Client) PlexSetPoster(ctx context.Context, s Server, key string, data []byte, mime string) error {
	if _, _, err := c.plexRaw(ctx, s, http.MethodPost, "/library/metadata/"+url.PathEscape(key)+"/posters", nil, data, mime); err != nil {
		return explain(s, err)
	}
	return nil
}

// PlexDeleteCollection deletes a collection (its titles stay).
func (c *Client) PlexDeleteCollection(ctx context.Context, s Server, key string) error {
	if err := c.do(ctx, s, http.MethodDelete, "/library/collections/"+url.PathEscape(key), nil, nil, nil); err != nil && !IsNotFound(err) {
		return explain(s, err)
	}
	return nil
}

// ---- Plex: library titles with what Usherr needs ----

// PlexAudio and PlexVideo are what a file's tracks hold, as Plex says.
type PlexAudio struct {
	Codec, Profile, Title string
	Channels              int
}

type PlexVideo struct {
	Width, Height int
	DolbyVision   bool
	ColorTrc      string // smpte2084 (HDR10), arib-std-b67 (HLG)
	Title         string // "4K DoVi/HDR10 (HEVC Main 10)"
}

// PlexLibItem is a film or show in one of Plex's libraries.
type PlexLibItem struct {
	Key, Section, Library string
	Kind                  string // movie or show
	Title                 string
	Year                  int
	TMDBID, TVDBID        int
	IMDBID                string
	Genres                []string
	Rating                float64 // the audience rating (TMDB's, by default), else the critics'
	AddedAt               int64   // unix seconds
	Thumb                 string  // changes when the poster does
	// From the listing: the first file's size and picture and sound, and a
	// fingerprint that changes with the file.
	Width, Height int
	AudioCodec    string
	AudioProfile  string
	AudioChannels int
	FilePrint     string
}

// PlexLibItems lists the films and shows in Plex's film and TV libraries.
func (c *Client) PlexLibItems(ctx context.Context, s Server) ([]PlexLibItem, error) {
	secs, err := c.plexSections(ctx, s)
	if err != nil {
		return nil, err
	}
	var out []PlexLibItem
	for _, sec := range secs {
		if sec.Type != "movie" && sec.Type != "show" {
			continue
		}
		var doc struct {
			MediaContainer struct {
				Metadata []struct {
					plexMeta
					Thumb          string  `json:"thumb"`
					AddedAt        int64   `json:"addedAt"`
					AudienceRating float64 `json:"audienceRating"`
					Rating         float64 `json:"rating"`
					Genre          []struct {
						Tag string `json:"tag"`
					} `json:"Genre"`
					Media []struct {
						Width         int    `json:"width"`
						Height        int    `json:"height"`
						AudioCodec    string `json:"audioCodec"`
						AudioProfile  string `json:"audioProfile"`
						AudioChannels int    `json:"audioChannels"`
						Part          []struct {
							ID   json.Number `json:"id"`
							Size int64       `json:"size"`
						} `json:"Part"`
					} `json:"Media"`
				} `json:"Metadata"`
			} `json:"MediaContainer"`
		}
		q := url.Values{}
		q.Set("includeGuids", "1")
		if err := c.do(ctx, s, http.MethodGet, "/library/sections/"+url.PathEscape(sec.ID)+"/all", q, nil, &doc); err != nil {
			return nil, explain(s, err)
		}
		for _, m := range doc.MediaContainer.Metadata {
			it := PlexLibItem{Key: m.RatingKey, Section: sec.ID, Library: sec.Title, Kind: sec.Type, Title: m.Title, Year: m.Year, TMDBID: m.tmdb(), AddedAt: m.AddedAt, Thumb: m.Thumb, Rating: m.AudienceRating}
			if it.Rating == 0 {
				it.Rating = m.Rating
			}
			for _, g := range m.Guids {
				if rest, ok := cutPrefix(g.ID, "tvdb://"); ok {
					it.TVDBID, _ = strconv.Atoi(rest)
				}
				if rest, ok := cutPrefix(g.ID, "imdb://"); ok {
					it.IMDBID = rest
				}
			}
			for _, g := range m.Genre {
				it.Genres = append(it.Genres, g.Tag)
			}
			if len(m.Media) > 0 {
				md := m.Media[0]
				it.Width, it.Height, it.AudioCodec, it.AudioProfile, it.AudioChannels = md.Width, md.Height, md.AudioCodec, md.AudioProfile, md.AudioChannels
				if len(md.Part) > 0 {
					it.FilePrint = md.Part[0].ID.String() + ":" + strconv.FormatInt(md.Part[0].Size, 10)
				}
			}
			out = append(out, it)
		}
	}
	return out, nil
}

// plexFlag reads Plex's yes/no, which comes as true, 1 or "1".
type plexFlag bool

func (f *plexFlag) UnmarshalJSON(b []byte) error {
	v := strings.Trim(string(bytes.TrimSpace(b)), `"`)
	*f = plexFlag(v == "true" || v == "1")
	return nil
}

// PlexTracks are the video and audio tracks of a title's first file.
func (c *Client) PlexTracks(ctx context.Context, s Server, key string) (PlexVideo, []PlexAudio, error) {
	var doc struct {
		MediaContainer struct {
			Metadata []struct {
				Media []struct {
					Part []struct {
						Stream []struct {
							StreamType           int      `json:"streamType"`
							Codec                string   `json:"codec"`
							Profile              string   `json:"profile"`
							Channels             int      `json:"channels"`
							Width                int      `json:"width"`
							Height               int      `json:"height"`
							DOVIPresent          plexFlag `json:"DOVIPresent"`
							ColorTrc             string   `json:"colorTrc"`
							DisplayTitle         string   `json:"displayTitle"`
							ExtendedDisplayTitle string   `json:"extendedDisplayTitle"`
						} `json:"Stream"`
					} `json:"Part"`
				} `json:"Media"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := c.do(ctx, s, http.MethodGet, "/library/metadata/"+url.PathEscape(key), nil, nil, &doc); err != nil {
		return PlexVideo{}, nil, explain(s, err)
	}
	var v PlexVideo
	var audio []PlexAudio
	md := doc.MediaContainer.Metadata
	if len(md) == 0 || len(md[0].Media) == 0 || len(md[0].Media[0].Part) == 0 {
		return v, nil, nil
	}
	seenVideo := false
	for _, st := range md[0].Media[0].Part[0].Stream {
		switch st.StreamType {
		case 1:
			if seenVideo {
				continue
			}
			seenVideo = true
			title := st.ExtendedDisplayTitle
			if title == "" {
				title = st.DisplayTitle
			}
			v = PlexVideo{Width: st.Width, Height: st.Height, DolbyVision: bool(st.DOVIPresent), ColorTrc: st.ColorTrc, Title: title}
		case 2:
			title := st.ExtendedDisplayTitle
			if title == "" {
				title = st.DisplayTitle
			}
			audio = append(audio, PlexAudio{Codec: st.Codec, Profile: st.Profile, Title: title, Channels: st.Channels})
		}
	}
	return v, audio, nil
}
