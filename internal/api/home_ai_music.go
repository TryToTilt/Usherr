package api

// Home fork: AI for music.
//   - Playlist from a mood: the AI picks albums from the library that fit
//     ("Sunday morning cooking, nothing too loud"), then about 25 of their
//     tracks in a good order, and Mediarium saves them as a playlist.
//   - More like this artist: artists in the library that go with one, and a
//     few to discover (found with the usual search, never added by itself).

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/ai"
	"github.com/rdborg/mediarium/internal/auth"
)

type aiAlbum struct {
	ID     int64
	Artist string
	Title  string
	Year   string
	Tracks int
}

// playableAlbums is every album with at least one track on disk.
func (s *Server) playableAlbums(ctx context.Context) ([]aiAlbum, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT al.id, ar.name, al.title, substr(al.release_date, 1, 4), COUNT(t.id)
		FROM albums al JOIN artists ar ON ar.id = al.artist_id JOIN tracks t ON t.album_id = al.id AND t.file_path <> ''
		GROUP BY al.id ORDER BY ar.sort_name, al.release_date`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []aiAlbum
	for rows.Next() {
		var a aiAlbum
		if rows.Scan(&a.ID, &a.Artist, &a.Title, &a.Year, &a.Tracks) == nil {
			out = append(out, a)
		}
	}
	return out, rows.Err()
}

const playlistAlbumsSystem = `You make playlists from a person's own music library. Given a mood or occasion and a numbered list of the albums they have, choose the albums whose music fits it best. Reply with a JSON object only:
{"albums":[3,17,42],"name":"...","note":""}
- albums: 6 to 12 album numbers from the list, the best fit first. Only from the list. Use what you know about these artists and records; if you don't know an album, judge by the artist.
- name: a short playlist name (at most 40 characters), in the language asked for.
- note: one short sentence about the mix, in the language asked for.`

const playlistTracksSystem = `You put together a playlist from a numbered list of tracks. Choose about 25 tracks that best fit the mood or occasion and put them in a good listening order (a gentle start, a natural flow, no two tracks by the same artist back to back where you can avoid it). Reply with a JSON object only:
{"tracks":[12,4,30]}
Only numbers from the list, each at most once.`

// POST /api/home/ai/playlist {mood, lang, shared}: a playlist from the library.
func (s *Server) handleAIPlaylist(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		Mood   string `json:"mood"`
		Lang   string `json:"lang"`
		Shared bool   `json:"shared"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	req.Mood = strings.TrimSpace(firstN(req.Mood, 200))
	if req.Mood == "" {
		writeError(w, http.StatusBadRequest, "Describe the mood or the occasion.")
		return
	}
	ctx := r.Context()
	u := auth.UserFromContext(ctx)
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	albums, err := s.playableAlbums(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(albums) == 0 {
		writeError(w, http.StatusBadRequest, "There's no music on disk to make a playlist from yet.")
		return
	}
	if !s.aiAllowed(ctx, u) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	// A very big library: a fair sample of it.
	if len(albums) > 1200 {
		rnd := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 3))
		rnd.Shuffle(len(albums), func(i, j int) { albums[i], albums[j] = albums[j], albums[i] })
		albums = albums[:1200]
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Mood or occasion: %q. Write the name and the note in %s.\n\nThe albums:\n", req.Mood, languageName(req.Lang))
	for i, a := range albums {
		fmt.Fprintf(&sb, "%d. %s - %s", i+1, a.Artist, a.Title)
		if a.Year != "" {
			fmt.Fprintf(&sb, " (%s)", a.Year)
		}
		sb.WriteString("\n")
	}
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: playlistAlbumsSystem, Prompt: sb.String(), JSON: true, MaxTokens: 500})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var pick struct {
		Albums []any  `json:"albums"` // numbers, or (from some models) names
		Name   string `json:"name"`
		Note   string `json:"note"`
	}
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &pick); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try again.")
		return
	}
	type trackLine struct {
		id                   int64
		artist, title, album string
	}
	var lines []trackLine
	seenAlbum := map[int64]bool{}
	for _, v := range pick.Albums {
		n := albumNumber(albums, v)
		if n < 1 || n > len(albums) || seenAlbum[albums[n-1].ID] {
			continue
		}
		a := albums[n-1]
		seenAlbum[a.ID] = true
		tracks, err := s.MusicRepo.ListTracks(a.ID)
		if err != nil {
			continue
		}
		for _, t := range tracks {
			if t.FilePath != "" && len(lines) < 320 {
				lines = append(lines, trackLine{t.ID, a.Artist, t.Title, a.Title})
			}
		}
	}
	if len(lines) == 0 {
		log.Printf("ai: playlist: no albums matched in %q", firstN(raw, 500))
		writeError(w, http.StatusBadGateway, "The AI didn't find albums for that. Try describing it another way.")
		return
	}
	sb.Reset()
	fmt.Fprintf(&sb, "Mood or occasion: %q.\n\nThe tracks:\n", req.Mood)
	for i, l := range lines {
		fmt.Fprintf(&sb, "%d. %s - %s (%s)\n", i+1, l.artist, l.title, l.album)
	}
	raw, err = ai.Ask(ctx, cfg, ai.Request{System: playlistTracksSystem, Prompt: sb.String(), JSON: true, MaxTokens: 400})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var order struct {
		Tracks []int `json:"tracks"`
	}
	_ = json.Unmarshal([]byte(ai.JSONObject(raw)), &order)
	var ids []int64
	used := map[int]bool{}
	for _, n := range order.Tracks {
		if n >= 1 && n <= len(lines) && !used[n] && len(ids) < 40 {
			used[n] = true
			ids = append(ids, lines[n-1].id)
		}
	}
	if len(ids) < 5 { // the model didn't order them: the albums' tracks, mixed
		rnd := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 5))
		perm := rnd.Perm(len(lines))
		ids = ids[:0]
		for _, i := range perm[:min(25, len(perm))] {
			ids = append(ids, lines[i].id)
		}
	}
	name := strings.TrimSpace(firstN(pick.Name, 60))
	if name == "" {
		name = firstN(req.Mood, 60)
	}
	pl, err := s.MusicRepo.CreatePlaylist(u.ID, "✨ "+name, req.Shared)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.MusicRepo.AddToPlaylist(pl.ID, u.ID, ids); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": pl.ID, "name": pl.Name, "note": strings.TrimSpace(pick.Note), "tracks": len(ids)})
}

// albumNumber is the list number the model meant: a number, a number in
// text ("12" or "12. Artist - Album"), or the album's name ("Artist - Album"
// or the title alone). 0 when it isn't on the list.
func albumNumber(albums []aiAlbum, v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		x = strings.TrimSpace(x)
		digits := x
		if i := strings.IndexAny(x, ". "); i > 0 {
			digits = x[:i]
		}
		if n, err := strconv.Atoi(digits); err == nil {
			return n
		}
		want := normTitle(x)
		for i, a := range albums {
			if normTitle(a.Artist+" "+a.Title) == want || normTitle(a.Title) == want {
				return i + 1
			}
		}
	}
	return 0
}

// ---- More like this artist ----

const similarArtistsSystem = `You recommend music. Given one artist and a numbered list of the other artists in the person's library, reply with a JSON object only:
{"have":[4,19],"discover":[{"artist":"...","album":"...","why":"..."}],"note":""}
- have: up to 8 numbers of artists from the list that go well with this one, the closest first. Only from the list.
- discover: up to 6 artists NOT in the list they'd likely enjoy, each with one album to start with and one short sentence on why.
- note: one short sentence. Write the why and the note in the language asked for.`

// POST /api/home/ai/similar-artists {artistId, lang}
func (s *Server) handleAISimilarArtists(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.aiConfig()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "AI isn't set up. An administrator can set it up under Settings > Extras.")
		return
	}
	var req struct {
		ArtistID int64  `json:"artistId"`
		Lang     string `json:"lang"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ArtistID <= 0 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	ctx := r.Context()
	list, err := s.MusicRepo.ListArtists()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var me string
	var others []struct {
		ID   int64
		Name string
	}
	for _, a := range list {
		if a.ID == req.ArtistID {
			me = a.Name
		} else if a.Downloaded > 0 || a.Albums > 0 {
			others = append(others, struct {
				ID   int64
				Name string
			}{a.ID, a.Name})
		}
	}
	if me == "" {
		writeError(w, http.StatusNotFound, "That artist isn't in your library.")
		return
	}
	sort.Slice(others, func(i, j int) bool { return strings.ToLower(others[i].Name) < strings.ToLower(others[j].Name) })
	if len(others) > 1500 {
		others = others[:1500]
	}
	key := aiCacheKey("similar-artists", cfg.Provider, cfg.Model, strconv.FormatInt(req.ArtistID, 10), strconv.Itoa(len(others)), req.Lang)
	aiCacheMu.Lock()
	if c, ok := aiCache[key]; ok && time.Since(c.at) < 7*24*time.Hour {
		aiCacheMu.Unlock()
		writeJSON(w, http.StatusOK, c.data)
		return
	}
	aiCacheMu.Unlock()
	if !s.aiAllowed(ctx, auth.UserFromContext(ctx)) {
		writeError(w, http.StatusTooManyRequests, "That's all the AI answers for today. More tomorrow.")
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "The artist: %s. Answer in %s.\n\nThe other artists in the library:\n", me, languageName(req.Lang))
	for i, a := range others {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, a.Name)
	}
	raw, err := ai.Ask(ctx, cfg, ai.Request{System: similarArtistsSystem, Prompt: sb.String(), JSON: true, MaxTokens: 900})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var ans struct {
		Have     []int `json:"have"`
		Discover []struct {
			Artist string `json:"artist"`
			Album  string `json:"album"`
			Why    string `json:"why"`
		} `json:"discover"`
		Note string `json:"note"`
	}
	if err := json.Unmarshal([]byte(ai.JSONObject(raw)), &ans); err != nil {
		writeError(w, http.StatusBadGateway, "The AI's answer couldn't be read. Try again.")
		return
	}
	type haveOut struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	type discOut struct {
		Artist string `json:"artist"`
		Album  string `json:"album"`
		Why    string `json:"why"`
		To     string `json:"to"`
	}
	res := struct {
		Note     string    `json:"note"`
		Have     []haveOut `json:"have"`
		Discover []discOut `json:"discover"`
	}{Note: strings.TrimSpace(ans.Note), Have: []haveOut{}, Discover: []discOut{}}
	used := map[int]bool{}
	for _, n := range ans.Have {
		if n >= 1 && n <= len(others) && !used[n] && len(res.Have) < 8 {
			used[n] = true
			res.Have = append(res.Have, haveOut{others[n-1].ID, others[n-1].Name})
		}
	}
	inLib := map[string]bool{}
	for _, a := range list {
		inLib[normTitle(a.Name)] = true
	}
	for _, d := range ans.Discover {
		d.Artist = strings.TrimSpace(d.Artist)
		if d.Artist == "" || inLib[normTitle(d.Artist)] || len(res.Discover) == 6 {
			continue
		}
		res.Discover = append(res.Discover, discOut{Artist: d.Artist, Album: strings.TrimSpace(d.Album), Why: strings.TrimSpace(d.Why), To: "/search?q=" + url.QueryEscape(d.Artist)})
	}
	aiCacheMu.Lock()
	aiCache[key] = aiCached{at: time.Now(), data: res}
	aiCacheMu.Unlock()
	writeJSON(w, http.StatusOK, res)
}
