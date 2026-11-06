package api_test

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// subsonicApp is a phone app talking to the server's /rest/.
type subsonicApp struct {
	t    *testing.T
	base string
	auth url.Values
}

func (a subsonicApp) get(endpoint string, params url.Values) (*http.Response, []byte) {
	a.t.Helper()
	q := url.Values{"v": {"1.16.1"}, "c": {"TestApp"}}
	for k, v := range a.auth {
		q[k] = v
	}
	for k, v := range params {
		q[k] = v
	}
	resp, err := http.Get(a.base + "/rest/" + endpoint + "?" + q.Encode())
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

// json calls an endpoint with f=json and returns the inner response.
func (a subsonicApp) json(endpoint string, params url.Values) map[string]any {
	a.t.Helper()
	if params == nil {
		params = url.Values{}
	}
	params.Set("f", "json")
	_, b := a.get(endpoint, params)
	var doc map[string]map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		a.t.Fatalf("%s: not JSON: %v %s", endpoint, err, b)
	}
	return doc["subsonic-response"]
}

// ok calls an endpoint and fails the test unless it answered "ok".
func (a subsonicApp) ok(endpoint string, params url.Values) map[string]any {
	a.t.Helper()
	r := a.json(endpoint, params)
	if r["status"] != "ok" {
		a.t.Fatalf("%s %v: %v", endpoint, params, r["error"])
	}
	return r
}

func errCode(r map[string]any) float64 {
	if e, ok := r["error"].(map[string]any); ok {
		return e["code"].(float64)
	}
	return -1
}

func tokenAuth(user, password, salt string) url.Values {
	sum := md5.Sum([]byte(password + salt))
	return url.Values{"u": {user}, "t": {hex.EncodeToString(sum[:])}, "s": {salt}}
}

func ssList(m map[string]any, keys ...string) []any {
	var cur any = m
	for _, k := range keys {
		cur = cur.(map[string]any)[k]
	}
	if cur == nil {
		return nil
	}
	return cur.([]any)
}

func TestSubsonicSignInRulesAndSwitches(t *testing.T) {
	f := newFamilyMusic(t)
	anon := subsonicApp{t: t, base: f.base}

	// Open to anyone: the extensions list.
	if r := anon.ok("getOpenSubsonicExtensions", nil); len(ssList(r, "openSubsonicExtensions")) < 2 {
		t.Fatalf("extensions: %v", r)
	}
	if r := anon.json("ping", nil); errCode(r) != 10 {
		t.Fatalf("no credentials: %v", r)
	}

	pw := postJSON[map[string]any](t, f.member, f.url("/api/music/subsonic/password"), nil, http.StatusOK)
	secret := pw["password"].(string)
	if len(secret) < 20 {
		t.Fatalf("app password: %v", pw)
	}
	st := getJSON[map[string]any](t, f.member, f.url("/api/music/subsonic"))
	if st["enabled"] != false || st["serverOn"] != true || st["hasPassword"] != true || st["tokenReady"] != true || st["password"] != nil {
		t.Fatalf("status (the password is shown once only): %+v", st)
	}
	// It is the same password e-reader apps use.
	if !f.server.EReader.CheckOPDS(f.memberID(t), secret) {
		t.Fatal("the app password is the e-reader password")
	}
	app := subsonicApp{t: t, base: f.base, auth: tokenAuth("sam", secret, "a1b2c3")}
	if r := app.json("ping", nil); errCode(r) != 50 {
		t.Fatalf("switched off for the account: %v", r)
	}
	postJSONMethod[map[string]any](t, f.member, http.MethodPut, f.url("/api/music/subsonic"), map[string]any{"enabled": true}, http.StatusOK)

	app.ok("ping", nil)
	subsonicApp{t: t, base: f.base, auth: tokenAuth("SAM", secret, "zz9")}.ok("ping", nil) // user names ignore case
	subsonicApp{t: t, base: f.base, auth: url.Values{"u": {"sam"}, "p": {secret}}}.ok("ping", nil)
	subsonicApp{t: t, base: f.base, auth: url.Values{"u": {"sam"}, "p": {"enc:" + hex.EncodeToString([]byte(secret))}}}.ok("ping", nil)
	subsonicApp{t: t, base: f.base, auth: url.Values{"apiKey": {secret}}}.ok("ping", nil)
	// Typed without the dashes, or in capitals on a phone keyboard.
	bare := strings.ReplaceAll(secret, "-", "")
	subsonicApp{t: t, base: f.base, auth: tokenAuth("sam", bare, "q1")}.ok("ping", nil)
	subsonicApp{t: t, base: f.base, auth: tokenAuth("sam", strings.ToUpper(secret), "q2")}.ok("ping", nil)
	subsonicApp{t: t, base: f.base, auth: url.Values{"u": {"sam"}, "p": {bare}}}.ok("ping", nil)
	for name, tc := range map[string]struct {
		auth url.Values
		code float64
	}{
		"wrong token":                 {tokenAuth("sam", "nope", "a1b2c3"), 40},
		"the account's own password":  {tokenAuth("sam", "kids-password", "a1b2c3"), 40},
		"plain account password":      {url.Values{"u": {"sam"}, "p": {"kids-password"}}, 40},
		"someone else's app password": {tokenAuth("ryan", secret, "a1b2c3"), 40},
		"unknown user":                {tokenAuth("nobody", secret, "a1b2c3"), 40},
		"bad api key":                 {url.Values{"apiKey": {"nope"}}, 44},
		"api key with a user":         {url.Values{"apiKey": {secret}, "u": {"sam"}}, 43},
		"token without salt":          {url.Values{"u": {"sam"}, "t": {"abc"}}, 10},
	} {
		if r := (subsonicApp{t: t, base: f.base, auth: tc.auth}).json("ping", nil); r["status"] != "failed" || errCode(r) != tc.code {
			t.Errorf("%s: %v", name, r)
		}
	}

	// The administrator can switch phone apps off for everyone.
	postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.url("/api/music/player-settings"), map[string]any{"subsonic": false}, http.StatusOK)
	if r := app.json("ping", nil); errCode(r) != 50 {
		t.Fatalf("server switch: %v", r)
	}
	postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.url("/api/music/player-settings"), map[string]any{"subsonic": true}, http.StatusOK)

	// A password made before it was kept for token sign-in: p= and apiKey still work.
	if err := f.server.TestExec(`UPDATE ereader_access SET opds_secret_enc = ''`); err != nil {
		t.Fatal(err)
	}
	if r := app.json("ping", nil); errCode(r) != 40 || !strings.Contains(r["error"].(map[string]any)["message"].(string), "new one") {
		t.Fatalf("old password, token sign-in: %v", r)
	}
	subsonicApp{t: t, base: f.base, auth: url.Values{"u": {"sam"}, "p": {secret}}}.ok("ping", nil)
	if st := getJSON[map[string]any](t, f.member, f.url("/api/music/subsonic")); st["tokenReady"] != false {
		t.Fatalf("status of an old password: %v", st)
	}

	// Removing the password ends it.
	req, _ := http.NewRequest(http.MethodDelete, f.url("/api/music/subsonic/password"), nil)
	if resp, err := f.member.Do(req); err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove: %v %v", resp, err)
	}
	if r := (subsonicApp{t: t, base: f.base, auth: url.Values{"u": {"sam"}, "p": {secret}}}).json("ping", nil); errCode(r) != 40 {
		t.Fatalf("after removing: %v", r)
	}
}

func (f *familyMusic) memberID(t *testing.T) int64 {
	t.Helper()
	u, err := f.server.Auth.UserByName("sam")
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// subsonicMember is a member with phone apps on and an app password.
func subsonicMember(t *testing.T, f *familyMusic, client *http.Client, user string) subsonicApp {
	t.Helper()
	pw := postJSON[map[string]any](t, client, f.url("/api/music/subsonic/password"), nil, http.StatusOK)
	postJSONMethod[map[string]any](t, client, http.MethodPut, f.url("/api/music/subsonic"), map[string]any{"enabled": true}, http.StatusOK)
	return subsonicApp{t: t, base: f.base, auth: tokenAuth(user, pw["password"].(string), "s4lt")}
}

func TestSubsonicBrowsingShapes(t *testing.T) {
	f := newFamilyMusic(t)
	if err := f.server.TestExec(`UPDATE tracks SET length_ms = 200000`); err != nil {
		t.Fatal(err)
	}
	app := subsonicMember(t, f, f.member, "sam")
	al := fmt.Sprintf("al-%d", f.albumID)

	if r := app.ok("getMusicFolders", nil); ssList(r, "musicFolders", "musicFolder")[0].(map[string]any)["id"] != float64(1) {
		t.Fatalf("folders: %v", r)
	}
	r := app.ok("getArtists", nil)
	idx := ssList(r, "artists", "index")
	if len(idx) != 1 || idx[0].(map[string]any)["name"] != "F" {
		t.Fatalf("index: %v", r)
	}
	artist := idx[0].(map[string]any)["artist"].([]any)[0].(map[string]any)
	if artist["name"] != "Fixture Band" || artist["albumCount"] != float64(1) || !strings.HasPrefix(artist["id"].(string), "ar-") {
		t.Fatalf("artist: %v", artist)
	}
	r = app.ok("getArtist", url.Values{"id": {artist["id"].(string)}})
	if albums := ssList(r, "artist", "album"); len(albums) != 1 || albums[0].(map[string]any)["id"] != al {
		t.Fatalf("artist albums: %v", r)
	}

	r = app.ok("getAlbum", url.Values{"id": {al}})
	album := r["album"].(map[string]any)
	songs := album["song"].([]any)
	if album["name"] != "First Album" || album["songCount"] != float64(3) || album["duration"] != float64(600) || len(songs) != 3 {
		t.Fatalf("album: %v", album)
	}
	s0 := songs[0].(map[string]any)
	for k, want := range map[string]any{"title": "Opening", "track": float64(1), "discNumber": float64(1), "duration": float64(200), "suffix": "flac",
		"contentType": "audio/flac", "albumId": al, "isDir": false, "type": "music", "path": "Fixture Band/First Album (1999)/01 - Opening.flac"} {
		if s0[k] != want {
			t.Errorf("song %s = %v, want %v", k, s0[k], want)
		}
	}
	if s0["size"] == nil || s0["coverArt"] != al || s0["parent"] != al {
		t.Fatalf("song: %v", s0)
	}

	// The same answer in XML.
	_, body := app.get("getAlbum", url.Values{"id": {al}})
	var doc struct {
		XMLName xml.Name `xml:"http://subsonic.org/restapi subsonic-response"`
		Status  string   `xml:"status,attr"`
		Album   struct {
			Name  string `xml:"name,attr"`
			Songs []struct {
				ID    string `xml:"id,attr"`
				Title string `xml:"title,attr"`
			} `xml:"song"`
		} `xml:"album"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil || doc.Status != "ok" || doc.Album.Name != "First Album" || len(doc.Album.Songs) != 3 || doc.Album.Songs[2].Title != "Closing Time" {
		t.Fatalf("xml: %v %+v\n%s", err, doc, body)
	}

	if r := app.json("getAlbum", url.Values{"id": {"al-999999"}}); errCode(r) != 70 {
		t.Fatalf("missing album: %v", r)
	}
	if r := app.json("getAlbum", nil); errCode(r) != 10 {
		t.Fatalf("no id: %v", r)
	}
	if r := app.json("getNothing", nil); errCode(r) != 70 {
		t.Fatalf("unknown endpoint: %v", r)
	}
	if r := app.ok("getSong", url.Values{"id": {s0["id"].(string)}}); r["song"].(map[string]any)["title"] != "Opening" {
		t.Fatalf("song: %v", r)
	}

	// search3: everything for "" (Symfonium's sync), words otherwise.
	for _, query := range []string{"", `""`} {
		r = app.ok("search3", url.Values{"query": {query}, "songCount": {"10"}})
		if len(ssList(r, "searchResult3", "song")) != 3 || len(ssList(r, "searchResult3", "album")) != 1 || len(ssList(r, "searchResult3", "artist")) != 1 {
			t.Fatalf("search %q: %v", query, r)
		}
	}
	r = app.ok("search3", url.Values{"query": {"middle"}})
	if s := ssList(r, "searchResult3", "song"); len(s) != 1 || s[0].(map[string]any)["title"] != "Middle Song" || len(ssList(r, "searchResult3", "album")) != 0 {
		t.Fatalf("search middle: %v", r)
	}
	r = app.ok("search3", url.Values{"query": {"100%_"}})
	if len(ssList(r, "searchResult3", "song")) != 0 {
		t.Fatalf("wildcards are literal: %v", r)
	}

	for _, typ := range []string{"newest", "random", "alphabeticalByName", "alphabeticalByArtist", "byYear"} {
		r = app.ok("getAlbumList2", url.Values{"type": {typ}, "fromYear": {"1990"}, "toYear": {"2000"}})
		if len(ssList(r, "albumList2", "album")) != 1 {
			t.Errorf("album list %s: %v", typ, r)
		}
	}
	if r := app.ok("getAlbumList2", url.Values{"type": {"frequent"}}); len(ssList(r, "albumList2", "album")) != 0 {
		t.Fatalf("nothing played yet: %v", r)
	}
	if r := app.json("getAlbumList2", nil); errCode(r) != 10 {
		t.Fatalf("no type: %v", r)
	}
	if r := app.ok("getRandomSongs", url.Values{"size": {"2"}}); len(ssList(r, "randomSongs", "song")) != 2 {
		t.Fatalf("random: %v", r)
	}
	r = app.ok("getIndexes", nil)
	lastMod := r["indexes"].(map[string]any)["lastModified"].(float64)
	if r := app.ok("getIndexes", url.Values{"ifModifiedSince": {fmt.Sprint(int64(lastMod))}}); r["indexes"].(map[string]any)["index"] != nil {
		t.Fatalf("unchanged since: %v", r)
	}
	if r := app.ok("getMusicDirectory", url.Values{"id": {al}}); len(ssList(r, "directory", "child")) != 3 {
		t.Fatalf("directory: %v", r)
	}
	if r := app.ok("getUser", url.Values{"username": {"sam"}}); r["user"].(map[string]any)["streamRole"] != true || r["user"].(map[string]any)["adminRole"] != false {
		t.Fatalf("user: %v", r)
	}
	if r := app.json("getUser", url.Values{"username": {"ryan"}}); errCode(r) != 50 {
		t.Fatalf("another user: %v", r)
	}
	if r := app.json("startScan", nil); errCode(r) != 50 {
		t.Fatalf("scan is for administrators: %v", r)
	}

	// Covers: the album's cover.jpg.
	resp, b := app.get("getCoverArt", url.Values{"id": {al}})
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" || len(b) == 0 {
		t.Fatalf("cover: %d %v", resp.StatusCode, resp.Header)
	}
	if resp, _ := app.get("getCoverArt", url.Values{"id": {"al-999999"}}); resp.StatusCode != 404 {
		t.Fatalf("no cover: %d", resp.StatusCode)
	}

	// Lyrics from a .lrc beside the track.
	tracks, _ := f.server.MusicRepo.ListTracks(f.albumID)
	if err := os.WriteFile(strings.TrimSuffix(tracks[0].FilePath, ".flac")+".lrc", []byte("[00:01.50]first\n[00:03.00]second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = app.ok("getLyricsBySongId", url.Values{"id": {s0["id"].(string)}})
	sl := ssList(r, "lyricsList", "structuredLyrics")
	if len(sl) != 1 || sl[0].(map[string]any)["synced"] != true {
		t.Fatalf("lyrics: %v", r)
	}
	lines := sl[0].(map[string]any)["line"].([]any)
	if len(lines) != 2 || lines[0].(map[string]any)["start"] != float64(1500) || lines[1].(map[string]any)["value"] != "second" {
		t.Fatalf("lines: %v", lines)
	}
	_, body = app.get("getLyricsBySongId", url.Values{"id": {s0["id"].(string)}})
	if !strings.Contains(string(body), `<line start="1500">first</line>`) {
		t.Fatalf("lyrics xml: %s", body)
	}
}

func TestSubsonicPlaylistsStarsPlaysAndPermissions(t *testing.T) {
	f := newFamilyMusic(t)
	if err := f.server.TestExec(`UPDATE tracks SET length_ms = 200000`); err != nil {
		t.Fatal(err)
	}
	app := subsonicMember(t, f, f.member, "sam")
	tr := func(i int) string { return fmt.Sprintf("tr-%d", f.trackIDs[i]) }

	// The administrator's private list isn't seen; a shared one is, but can't be changed.
	postJSON[map[string]any](t, f.admin, f.url("/api/music/playlists"), map[string]any{"name": "Private", "albumId": f.albumID}, http.StatusCreated)
	shared := postJSON[map[string]any](t, f.admin, f.url("/api/music/playlists"), map[string]any{"name": "Family", "shared": true, "albumId": f.albumID}, http.StatusCreated)
	r := app.ok("getPlaylists", nil)
	if pl := ssList(r, "playlists", "playlist"); len(pl) != 1 || pl[0].(map[string]any)["name"] != "Family" || pl[0].(map[string]any)["owner"] != "ryan" {
		t.Fatalf("playlists: %v", r)
	}
	familyID := fmt.Sprintf("pl-%d", int64(shared["id"].(float64)))
	if r := app.json("deletePlaylist", url.Values{"id": {familyID}}); errCode(r) != 50 {
		t.Fatalf("delete another's list: %v", r)
	}
	if r := app.json("updatePlaylist", url.Values{"playlistId": {familyID}, "name": {"Mine"}}); errCode(r) != 50 {
		t.Fatalf("rename another's list: %v", r)
	}

	r = app.ok("createPlaylist", url.Values{"name": {"Road trip"}, "songId": {tr(2), tr(0), tr(1)}})
	pl := r["playlist"].(map[string]any)
	if pl["songCount"] != float64(3) || pl["entry"].([]any)[0].(map[string]any)["title"] != "Closing Time" || pl["public"] != false {
		t.Fatalf("created: %v", pl)
	}
	pid := pl["id"].(string)
	app.ok("updatePlaylist", url.Values{"playlistId": {pid}, "songIndexToRemove": {"0"}, "songIdToAdd": {tr(2)}, "public": {"true"}})
	r = app.ok("getPlaylist", url.Values{"id": {pid}})
	var titles []string
	for _, e := range ssList(r, "playlist", "entry") {
		titles = append(titles, e.(map[string]any)["title"].(string))
	}
	if strings.Join(titles, ",") != "Opening,Middle Song,Closing Time" || r["playlist"].(map[string]any)["public"] != true {
		t.Fatalf("updated: %v %v", titles, r)
	}
	app.ok("createPlaylist", url.Values{"playlistId": {pid}, "songId": {tr(1)}})
	if r := app.ok("getPlaylist", url.Values{"id": {pid}}); len(ssList(r, "playlist", "entry")) != 1 {
		t.Fatalf("replaced: %v", r)
	}
	app.ok("deletePlaylist", url.Values{"id": {pid}})

	// Stars are the web app's likes.
	app.ok("star", url.Values{"id": {tr(0)}, "albumId": {fmt.Sprintf("al-%d", f.albumID)}})
	r = app.ok("getStarred2", nil)
	if len(ssList(r, "starred2", "song")) != 1 || len(ssList(r, "starred2", "album")) != 1 || ssList(r, "starred2", "song")[0].(map[string]any)["starred"] == nil {
		t.Fatalf("starred: %v", r)
	}
	if favs := getJSON[map[string]any](t, f.member, f.url("/api/music/favorites")); len(favs["tracks"].([]any)) != 1 {
		t.Fatalf("web likes: %v", favs)
	}
	app.ok("unstar", url.Values{"id": {tr(0)}})
	if r := app.ok("getStarred2", nil); len(ssList(r, "starred2", "song")) != 0 {
		t.Fatalf("unstarred: %v", r)
	}
	if r := app.json("star", url.Values{"id": {"tr-999999"}}); errCode(r) != 70 {
		t.Fatalf("star a missing song: %v", r)
	}

	// Plays: now playing is accepted, a submission goes in the history.
	app.ok("scrobble", url.Values{"id": {tr(1)}, "submission": {"false"}})
	start := time.Now().Add(-time.Hour).UnixMilli()
	app.ok("scrobble", url.Values{"id": {tr(1), tr(2)}, "time": {fmt.Sprint(start), fmt.Sprint(start + 200000)}})
	var plays int
	if err := f.server.TestDB().QueryRow(`SELECT COUNT(*) FROM music_play_history WHERE source = 'subsonic'`).Scan(&plays); err != nil || plays != 2 {
		t.Fatalf("plays: %d %v", plays, err)
	}
	if r := app.json("scrobble", url.Values{"id": {tr(1)}, "time": {"1", "2"}}); errCode(r) != 0 || r["status"] != "failed" {
		t.Fatalf("times don't fit ids: %v", r)
	}
	if r := app.ok("getAlbumList2", url.Values{"type": {"frequent"}}); len(ssList(r, "albumList2", "album")) != 1 {
		t.Fatalf("played album: %v", r)
	}

	// Play queue.
	app.ok("savePlayQueue", url.Values{"id": {tr(2), tr(0)}, "current": {tr(0)}, "position": {"42000"}})
	r = app.ok("getPlayQueue", nil)
	if q := r["playQueue"].(map[string]any); q["current"] != tr(0) || q["position"] != float64(42000) || len(q["entry"].([]any)) != 2 || q["changedBy"] != "TestApp" {
		t.Fatalf("queue: %v", r)
	}

	// Streaming needs the Play permission.
	resp, b := app.get("stream", url.Values{"id": {tr(0)}})
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "audio/flac" || len(b) == 0 {
		t.Fatalf("stream: %d %v", resp.StatusCode, resp.Header)
	}
	resp, _ = app.get("download", url.Values{"id": {tr(0)}})
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %v", resp.Header)
	}
	postJSON[map[string]any](t, f.admin, f.base+"/api/users", map[string]any{
		"username": "kid", "password": "kids-password", "role": "member", "permissions": map[string]any{"play": false},
	}, http.StatusCreated)
	kid := subsonicMember(t, f, erLoginAs(t, f.base, "kid", "kids-password"), "kid")
	kid.ok("getAlbum", url.Values{"id": {fmt.Sprintf("al-%d", f.albumID)}})
	if r := kid.json("stream", url.Values{"id": {tr(0)}}); errCode(r) != 50 {
		t.Fatalf("stream without Play: %v", r)
	}

	// A view-only account browses and plays but changes nothing.
	postJSON[map[string]any](t, f.admin, f.base+"/api/users", map[string]any{"username": "vee", "password": "viewer-password", "role": "viewer"}, http.StatusCreated)
	// (A viewer can't make an app password in the web app; set one up behind it.)
	vu, err := f.server.Auth.UserByName("vee")
	if err != nil {
		t.Fatal(err)
	}
	vpw, err := f.server.EReader.NewOPDSPassword(vu.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.server.TestExec(`INSERT INTO subsonic_users (user_id, enabled) VALUES (?, 1)`, vu.ID); err != nil {
		t.Fatal(err)
	}
	viewer := subsonicApp{t: t, base: f.base, auth: tokenAuth("vee", vpw, "v1")}
	viewer.ok("getAlbum", url.Values{"id": {fmt.Sprintf("al-%d", f.albumID)}})
	for _, ep := range []string{"createPlaylist", "star", "scrobble", "savePlayQueue"} {
		if r := viewer.json(ep, url.Values{"name": {"x"}, "id": {tr(0)}}); errCode(r) != 50 {
			t.Errorf("viewer %s: %v", ep, r)
		}
	}
}
