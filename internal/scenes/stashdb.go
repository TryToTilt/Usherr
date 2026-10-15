package scenes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// StashDB (stashdb.org) is a community database of adult scenes, performers
// and studios, run on stash-box. ThePornDB offers the same stash-box API at
// its own address, so the base address is a setting. Every request needs the
// user's own API key, sent in the "ApiKey" header.

// DefaultStashBox is StashDB's GraphQL endpoint.
const DefaultStashBox = "https://stashdb.org/graphql"

// ErrNoKey is returned when no API key has been saved.
var ErrNoKey = errors.New("add your StashDB API key in Settings > Scenes first")

// ErrKeyRefused is returned when the server doesn't accept the key.
var ErrKeyRefused = errors.New("StashDB didn't accept the API key. Copy it again from your StashDB profile")

// StashBox talks to a stash-box server.
type StashBox struct {
	Endpoint  string        // GraphQL address; DefaultStashBox when empty
	Key       func() string // the API key, read on every request so a new one applies at once
	UserAgent string
	HTTP      *http.Client
}

// NewStashBox returns a client that reads its address and key on each call.
func NewStashBox(userAgent string, key func() string) *StashBox {
	return &StashBox{Key: key, UserAgent: userAgent, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type gqlError struct {
	Message string `json:"message"`
}

func (c *StashBox) query(ctx context.Context, q string, vars map[string]any, out any) error {
	key := ""
	if c.Key != nil {
		key = strings.TrimSpace(c.Key())
	}
	if key == "" {
		return ErrNoKey
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultStashBox
	}
	body, _ := json.Marshal(map[string]any{"query": q, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", key)
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach StashDB: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrKeyRefused
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("StashDB answered %d", resp.StatusCode)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("StashDB sent something that isn't GraphQL: %w", err)
	}
	if len(env.Errors) > 0 {
		msg := env.Errors[0].Message
		if strings.Contains(strings.ToLower(msg), "unauthorized") || strings.Contains(strings.ToLower(msg), "not authorized") {
			return ErrKeyRefused
		}
		return fmt.Errorf("StashDB: %s", msg)
	}
	return json.Unmarshal(env.Data, out)
}

// ---- wire types ----

type wireImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type wireStudio struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Parent  *struct {
		Name string `json:"name"`
	} `json:"parent"`
	Images []wireImage `json:"images"`
}

type wirePerformer struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Disambiguation string      `json:"disambiguation"`
	Aliases        []string    `json:"aliases"`
	Images         []wireImage `json:"images"`
	SceneCount     int         `json:"scene_count"`
	Gender         string      `json:"gender"`
}

type wireScene struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Details     string      `json:"details"`
	ReleaseDate string      `json:"release_date"`
	Duration    int         `json:"duration"`
	Code        string      `json:"code"`
	Studio      *wireStudio `json:"studio"`
	Images      []wireImage `json:"images"`
	Performers  []struct {
		As        string        `json:"as"`
		Performer wirePerformer `json:"performer"`
	} `json:"performers"`
	Tags []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

const studioFields = `id name aliases parent { name } images { url width height }`
const performerFields = `id name disambiguation aliases scene_count images { url width height }`
const sceneFields = `id title details release_date duration code
	studio { ` + studioFields + ` }
	images { url width height }
	performers { as performer { id name disambiguation aliases gender } }
	tags { name }`

// bestImage picks the widest landscape-ish image, or the first one.
func bestImage(imgs []wireImage) string {
	best := ""
	bestW := -1
	for _, im := range imgs {
		if im.Width > bestW {
			best, bestW = im.URL, im.Width
		}
	}
	return best
}

func (w wireStudio) toStudio() Studio {
	s := Studio{StashID: w.ID, Name: w.Name, Aliases: w.Aliases, ImageURL: bestImage(w.Images)}
	if w.Parent != nil {
		s.ParentName = w.Parent.Name
	}
	if s.Aliases == nil {
		s.Aliases = []string{}
	}
	return s
}

func (w wirePerformer) toPerformer() Performer {
	p := Performer{StashID: w.ID, Name: w.Name, Disambiguation: w.Disambiguation, Aliases: w.Aliases, ImageURL: bestImage(w.Images)}
	if p.Aliases == nil {
		p.Aliases = []string{}
	}
	return p
}

func (w wireScene) toScene() Scene {
	s := Scene{StashID: w.ID, Title: w.Title, Details: w.Details, ReleaseDate: w.ReleaseDate, Duration: w.Duration, Code: w.Code, ImageURL: bestImage(w.Images), Wanted: true, Status: StatusMissing}
	if len(s.ReleaseDate) > 10 {
		s.ReleaseDate = s.ReleaseDate[:10]
	}
	if w.Studio != nil {
		s.StudioStashID, s.StudioName = w.Studio.ID, w.Studio.Name
		s.StudioAliases = w.Studio.Aliases
	}
	if s.StudioAliases == nil {
		s.StudioAliases = []string{}
	}
	s.Performers = []Credit{}
	for _, p := range w.Performers {
		s.Performers = append(s.Performers, Credit{StashID: p.Performer.ID, Name: p.Performer.Name, As: p.As, Gender: p.Performer.Gender})
	}
	s.Tags = []string{}
	for _, t := range w.Tags {
		if t.Name != "" {
			s.Tags = append(s.Tags, t.Name)
		}
	}
	// These details came from StashDB itself.
	s.DetailsAt = now()
	return s
}

// FoundPerformer is a performer search result, with their scene count.
type FoundPerformer struct {
	Performer
	SceneCount int `json:"sceneCount"`
}

// SearchPerformers finds performers by name.
func (c *StashBox) SearchPerformers(ctx context.Context, term string) ([]FoundPerformer, error) {
	var out struct {
		R struct {
			Performers []wirePerformer `json:"performers"`
		} `json:"searchPerformers"`
	}
	err := c.query(ctx, `query($t: String!) { searchPerformers(term: $t, per_page: 25) { performers { `+performerFields+` } } }`, map[string]any{"t": term}, &out)
	if err != nil {
		return nil, err
	}
	found := []FoundPerformer{}
	for _, p := range out.R.Performers {
		found = append(found, FoundPerformer{Performer: p.toPerformer(), SceneCount: p.SceneCount})
	}
	return found, nil
}

// SearchStudios finds studios by name.
func (c *StashBox) SearchStudios(ctx context.Context, term string) ([]Studio, error) {
	var out struct {
		R []wireStudio `json:"searchStudio"`
	}
	if err := c.query(ctx, `query($t: String!) { searchStudio(term: $t, limit: 25) { `+studioFields+` } }`, map[string]any{"t": term}, &out); err != nil {
		return nil, err
	}
	found := []Studio{}
	for _, s := range out.R {
		found = append(found, s.toStudio())
	}
	return found, nil
}

// SearchScenes finds scenes by words from their title, studio or performers.
func (c *StashBox) SearchScenes(ctx context.Context, term string) ([]Scene, error) {
	var out struct {
		R struct {
			Scenes []wireScene `json:"scenes"`
		} `json:"searchScenes"`
	}
	if err := c.query(ctx, `query($t: String!) { searchScenes(term: $t, per_page: 25) { scenes { `+sceneFields+` } } }`, map[string]any{"t": term}, &out); err != nil {
		return nil, err
	}
	found := []Scene{}
	for _, s := range out.R.Scenes {
		found = append(found, s.toScene())
	}
	return found, nil
}

// FindScene returns one scene by its stash-box id.
func (c *StashBox) FindScene(ctx context.Context, id string) (Scene, error) {
	var out struct {
		R *wireScene `json:"findScene"`
	}
	if err := c.query(ctx, `query($id: ID!) { findScene(id: $id) { `+sceneFields+` } }`, map[string]any{"id": id}, &out); err != nil {
		return Scene{}, err
	}
	if out.R == nil {
		return Scene{}, ErrNotFound
	}
	return out.R.toScene(), nil
}

// FindPerformer returns one performer by its stash-box id.
func (c *StashBox) FindPerformer(ctx context.Context, id string) (Performer, error) {
	var out struct {
		R *wirePerformer `json:"findPerformer"`
	}
	if err := c.query(ctx, `query($id: ID!) { findPerformer(id: $id) { `+performerFields+` } }`, map[string]any{"id": id}, &out); err != nil {
		return Performer{}, err
	}
	if out.R == nil {
		return Performer{}, ErrNotFound
	}
	return out.R.toPerformer(), nil
}

// FindStudio returns one studio by its stash-box id.
func (c *StashBox) FindStudio(ctx context.Context, id string) (Studio, error) {
	var out struct {
		R *wireStudio `json:"findStudio"`
	}
	if err := c.query(ctx, `query($id: ID) { findStudio(id: $id) { `+studioFields+` } }`, map[string]any{"id": id}, &out); err != nil {
		return Studio{}, err
	}
	if out.R == nil {
		return Studio{}, ErrNotFound
	}
	return out.R.toStudio(), nil
}

// ScenePage is one page of a performer's or studio's scenes, newest first.
type ScenePage struct {
	Count  int     `json:"count"`
	Scenes []Scene `json:"scenes"`
}

// Kind of a followed source.
const (
	KindPerformer = "performer"
	KindStudio    = "studio"
)

// ScenesOf lists the scenes of a performer or studio (kind), newest first.
// since, when set (YYYY-MM-DD), keeps only scenes released on or after it.
func (c *StashBox) ScenesOf(ctx context.Context, kind, stashID string, page, perPage int, since string) (ScenePage, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 40
	}
	field := "performers"
	if kind == KindStudio {
		field = "studios"
	}
	input := map[string]any{
		field:       map[string]any{"value": []string{stashID}, "modifier": "INCLUDES"},
		"page":      page,
		"per_page":  perPage,
		"sort":      "DATE",
		"direction": "DESC",
	}
	if since != "" {
		input["date"] = map[string]any{"value": since, "modifier": "GREATER_THAN"}
	}
	var out struct {
		R struct {
			Count  int         `json:"count"`
			Scenes []wireScene `json:"scenes"`
		} `json:"queryScenes"`
	}
	if err := c.query(ctx, `query($in: SceneQueryInput!) { queryScenes(input: $in) { count scenes { `+sceneFields+` } } }`, map[string]any{"in": input}, &out); err != nil {
		return ScenePage{}, err
	}
	p := ScenePage{Count: out.R.Count, Scenes: []Scene{}}
	for _, s := range out.R.Scenes {
		p.Scenes = append(p.Scenes, s.toScene())
	}
	return p, nil
}

// Test checks that the server answers and accepts the key.
func (c *StashBox) Test(ctx context.Context) error {
	var out struct {
		Me *struct {
			Name string `json:"name"`
		} `json:"me"`
	}
	return c.query(ctx, `query { me { name } }`, nil, &out)
}
