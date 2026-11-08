package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/manga"
)

const (
	mdTower = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" // in the library, linked by AniList id
	mdNew   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" // followed there only
	mdAdult = "cccccccc-cccc-cccc-cccc-cccccccccccc" // rated pornographic
	mdList  = "dddddddd-dddd-dddd-dddd-dddddddddddd"
	mdCh1   = "11111111-1111-1111-1111-111111111111"
	mdCh2   = "22222222-2222-2222-2222-222222222222"
	mdCh3   = "33333333-3333-3333-3333-333333333333"
	mdNewC1 = "44444444-4444-4444-4444-444444444444"
)

// fakeAccountDex is a MangaDex with one account on it; no test asks the real one.
type fakeAccountDex struct {
	*httptest.Server
	mu       sync.Mutex
	hits     map[string]int
	follows  map[string]bool
	read     map[string]map[string]bool
	statuses map[string]string
	lists    map[string][]string
	authOn   map[string]string // the Authorization header each path last got
	refuse   bool
	posted   []string
}

func newFakeAccountDex(t *testing.T) *fakeAccountDex {
	t.Helper()
	f := &fakeAccountDex{hits: map[string]int{}, authOn: map[string]string{},
		follows:  map[string]bool{mdTower: true, mdNew: true, mdAdult: true},
		read:     map[string]map[string]bool{mdTower: {mdCh1: true}},
		statuses: map[string]string{mdTower: "reading"},
		lists:    map[string][]string{mdList: {mdTower, mdNew, mdAdult}},
	}
	title := func(id, name, rating string, al int) string {
		links := "{}"
		if al > 0 {
			links = fmt.Sprintf(`{"al":"%d"}`, al)
		}
		return fmt.Sprintf(`{"id":%q,"attributes":{"title":{"en":%q},"links":%s,"year":2020,"status":"ongoing","contentRating":%q},"relationships":[{"id":"cov","type":"cover_art","attributes":{"fileName":"c.jpg"}}]}`, id, name, links, rating)
	}
	titles := map[string]string{mdTower: title(mdTower, "Tower", "safe", 77), mdNew: title(mdNew, "Brand New", "suggestive", 0), mdAdult: title(mdAdult, "Grown Up", "pornographic", 0)}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p := r.URL.Path
		f.hits[r.Method+" "+p]++
		f.authOn[p] = r.Header.Get("Authorization")
		authed := r.Header.Get("Authorization") == "Bearer tok"
		needAuth := strings.HasPrefix(p, "/user/") || p == "/manga/read" || p == "/manga/status" || strings.HasSuffix(p, "/follow") || strings.HasSuffix(p, "/status") || (strings.HasSuffix(p, "/read") && r.Method == http.MethodPost) || strings.Contains(p, "/list/")
		switch {
		case p == "/auth/token":
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			if f.refuse || form.Get("password") != "pw" && form.Get("grant_type") == "password" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"tok","refresh_token":"ref","expires_in":900}`)
		case needAuth && !authed:
			w.WriteHeader(http.StatusUnauthorized)
		case p == "/user/follows/manga":
			var parts []string
			for _, id := range []string{mdTower, mdNew, mdAdult} {
				if f.follows[id] {
					parts = append(parts, titles[id])
				}
			}
			_, _ = fmt.Fprintf(w, `{"data":[%s],"total":%d}`, strings.Join(parts, ","), len(parts))
		case strings.HasSuffix(p, "/follow"):
			id := strings.Split(p, "/")[2]
			f.follows[id] = r.Method == http.MethodPost
			_, _ = io.WriteString(w, `{"result":"ok"}`)
		case p == "/manga/read":
			out := map[string][]string{}
			for _, id := range r.URL.Query()["ids[]"] {
				for c := range f.read[id] {
					out[id] = append(out[id], c)
				}
			}
			b, _ := json.Marshal(map[string]any{"result": "ok", "data": out})
			_, _ = w.Write(b)
		case strings.HasSuffix(p, "/read") && r.Method == http.MethodPost:
			id := strings.Split(p, "/")[2]
			var body struct {
				Read   []string `json:"chapterIdsRead"`
				Unread []string `json:"chapterIdsUnread"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if f.read[id] == nil {
				f.read[id] = map[string]bool{}
			}
			for _, c := range body.Read {
				f.read[id][c] = true
				f.posted = append(f.posted, "read "+c)
			}
			for _, c := range body.Unread {
				delete(f.read[id], c)
				f.posted = append(f.posted, "unread "+c)
			}
			_, _ = io.WriteString(w, `{"result":"ok"}`)
		case p == "/manga/status":
			b, _ := json.Marshal(map[string]any{"result": "ok", "statuses": f.statuses})
			_, _ = w.Write(b)
		case strings.HasSuffix(p, "/status"):
			id := strings.Split(p, "/")[2]
			var body struct {
				Status *string `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Status == nil {
				delete(f.statuses, id)
			} else {
				f.statuses[id] = *body.Status
			}
			_, _ = io.WriteString(w, `{"result":"ok"}`)
		case p == "/user/list":
			var parts []string
			for id, manga := range f.lists {
				var rels []string
				for _, m := range manga {
					rels = append(rels, fmt.Sprintf(`{"id":%q,"type":"manga"}`, m))
				}
				parts = append(parts, fmt.Sprintf(`{"id":%q,"attributes":{"name":"Favourites","visibility":"private"},"relationships":[%s]}`, id, strings.Join(rels, ",")))
			}
			_, _ = fmt.Fprintf(w, `{"data":[%s],"total":%d}`, strings.Join(parts, ","), len(parts))
		case strings.Contains(p, "/list/"):
			bits := strings.Split(p, "/")
			id, list := bits[2], bits[4]
			keep := []string{}
			for _, m := range f.lists[list] {
				if m != id {
					keep = append(keep, m)
				}
			}
			if r.Method == http.MethodPost {
				keep = append(keep, id)
			}
			f.lists[list] = keep
			_, _ = io.WriteString(w, `{"result":"ok"}`)
		case p == "/user/follows/manga/feed":
			if r.URL.Query().Get("order[readableAt]") != "desc" || r.URL.Query().Get("translatedLanguage[]") != "en" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, rating := range r.URL.Query()["contentRating[]"] {
				if rating == "pornographic" {
					w.WriteHeader(http.StatusTeapot) // the test's person may not see it, so it mustn't be asked for
					return
				}
			}
			_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"attributes":{"chapter":"5","volume":null,"title":"Five","readableAt":"2026-11-06T10:00:00+00:00"},"relationships":[{"id":%q,"type":"manga","attributes":{"title":{"en":"Brand New"},"contentRating":"suggestive"}},{"id":"g","type":"scanlation_group","attributes":{"name":"Good Scans"}}]}]}`, mdNewC1, mdNew)
		case p == "/user/history":
			_, _ = fmt.Fprintf(w, `{"result":"ok","ratings":[{"chapterId":%q,"readDate":"2026-11-05T10:00:00+00:00"}]}`, mdNewC1)
		case p == "/chapter":
			_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"attributes":{"chapter":"5","volume":null},"relationships":[{"id":%q,"type":"manga"}]}]}`, mdNewC1, mdNew)
		case p == "/manga":
			var parts []string
			for _, id := range r.URL.Query()["ids[]"] {
				if t, ok := titles[id]; ok {
					parts = append(parts, t)
				}
			}
			_, _ = fmt.Fprintf(w, `{"data":[%s]}`, strings.Join(parts, ","))
		case strings.HasSuffix(p, "/aggregate"):
			_, _ = fmt.Fprintf(w, `{"result":"ok","volumes":{"1":{"volume":"1","chapters":{"1":{"chapter":"1","id":%q,"others":[]},"2":{"chapter":"2","id":%q,"others":[]}}},"2":{"volume":"2","chapters":{"3":{"chapter":"3","id":%q,"others":[]}}}}}`, mdCh1, mdCh2, mdCh3)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAccountDex) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[key]
}

// accountServer is a manga server pointed at the fake, with one administrator.
func accountServer(t *testing.T, f *fakeAccountDex) (*Server, *manga.Repo, int64) {
	t.Helper()
	s, repo, _ := mangaTestServer(t)
	mangaDexNoGap, mdNoStill = true, true
	t.Cleanup(func() { mdNoStill = false })
	st := s.mangaMod()
	st.dex = manga.NewMangaDex("Usherr/test", f.URL)
	st.dex.APIGap, st.dex.AtHomeGap, st.dex.ImageGap = 0, 0, 0
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash, is_admin) VALUES ('reader', 'x', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	user, _ := res.LastInsertId()
	return s, repo, user
}

func mdCall(t *testing.T, s *Server, h http.HandlerFunc, method, path, body string, user int64, pathValues ...string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rd)
	for i := 0; i+1 < len(pathValues); i += 2 {
		r.SetPathValue(pathValues[i], pathValues[i+1])
	}
	rec := httptest.NewRecorder()
	h(rec, withUser(r, user))
	return rec
}

const mdGoodSignIn = `{"clientId":"personal-client-x","clientSecret":"s3cret","username":"reader","password":"pw"}`

func TestMangaDexAccountConnectSyncDisconnect(t *testing.T) {
	f := newFakeAccountDex(t)
	s, repo, user := accountServer(t, f)
	tower, err := repo.AddSeries(manga.Series{AniListID: 77, Title: "Tower", LicensedEN: -1, Status: "ongoing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.EnsureVolume(tower.ID, "1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveFeed(tower.ID, []manga.DexChapter{
		{ID: mdCh1, Number: "1", Volume: "1", Language: "en", Pages: 3},
		{ID: mdCh2, Number: "2", Volume: "1", Language: "en", Pages: 3},
		{ID: mdCh3, Number: "3", Volume: "2", Language: "en", Pages: 3},
	}, "", "en", false); err != nil {
		t.Fatal(err)
	}
	chapters, _ := repo.Chapters(tower.ID)
	byNumber := map[string]int64{}
	for _, c := range chapters {
		byNumber[c.Number] = c.ID
	}
	// Read here before connecting: chapter 3.
	if err := repo.MarkChaptersRead(user, []int64{byNumber["3"]}, true, time.Now()); err != nil {
		t.Fatal(err)
	}

	rec := mdCall(t, s, s.handlePutMangaDexAccount, http.MethodPut, "/api/manga/md/account", mdGoodSignIn, user)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3cret") || strings.Contains(rec.Body.String(), "personal-client-x") {
		t.Fatal("the sign-in came back to the page")
	}
	var stored string
	_ = s.db.QueryRow(`SELECT client_secret || password || client_id FROM manga_md_accounts WHERE user_id = ?`, user).Scan(&stored)
	if stored == "" || strings.Contains(stored, "s3cret") || strings.Contains(stored, "personal-client-x") {
		t.Fatalf("the sign-in isn't stored encrypted: %q", stored)
	}
	s.bg.Wait() // the first sync

	// Follows: Tower is linked through its AniList id, Brand New is added (it has
	// no AniList link, so MangaDex's own details do), the adult one stays out.
	if !repo.Following(user, tower.ID) {
		t.Fatal("Tower isn't followed here")
	}
	if d := repo.DexStateOf(tower.ID); d.Link != manga.DexLinked || d.DexID != mdTower {
		t.Fatalf("Tower's link: %+v", d)
	}
	added, err := repo.SeriesByDex(mdNew)
	if err != nil || added.Title != "Brand New" || added.MonitorNew || added.WantAll {
		t.Fatalf("Brand New: %+v %v", added, err)
	}
	if _, err := repo.SeriesByDex(mdAdult); err == nil {
		t.Fatal("an adult series was added for an account without the NSFW section")
	}
	if out, _ := repo.DexOutside(user); out[mdAdult] != mdWhyAdult {
		t.Fatalf("outside: %v", out)
	}
	// Read marks: chapter 1 came down, chapter 3 went up.
	list, _ := repo.Chapters(tower.ID)
	_ = repo.FillChapterReading(user, tower.ID, list)
	read := map[string]bool{}
	for _, c := range list {
		read[c.Number] = c.Read
	}
	if !read["1"] || read["2"] || !read["3"] {
		t.Fatalf("read here: %v", read)
	}
	if !f.read[mdTower][mdCh3] {
		t.Fatalf("chapter 3 didn't go up: %v", f.posted)
	}
	// Status came down.
	if st, _ := repo.ReadingStatus(user, tower.ID); st != "reading" {
		t.Fatalf("status %q", st)
	}
	// The lists, the feed and history are on the shelves, without the adult one.
	rec = mdCall(t, s, s.handleMangaDexShelves, http.MethodGet, "/api/manga/md/shelves", "", user)
	body := rec.Body.String()
	if !strings.Contains(body, "Favourites") || !strings.Contains(body, "Good Scans") || strings.Contains(body, "Grown Up") {
		t.Fatalf("shelves: %s", body)
	}
	var shelves struct {
		Recent []mdShelfItem `json:"recent"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &shelves)
	if len(shelves.Recent) != 1 || shelves.Recent[0].Title != "Brand New" || shelves.Recent[0].Number != "5" {
		t.Fatalf("recent: %+v", shelves.Recent)
	}
	if f.authOn["/manga"] != "" || f.authOn["/chapter"] != "" {
		t.Fatal("a public call carried the login")
	}

	// Read here: chapter 2, then the two-minute check sends it once it's still.
	s.mdPushReads(t.Context(), user, time.Now()) // the first look takes things as they are
	if err := repo.MarkChaptersRead(user, []int64{byNumber["2"]}, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.mdPushReads(t.Context(), user, time.Now()) // notices
	s.mdPushReads(t.Context(), user, time.Now()) // sends
	if !f.read[mdTower][mdCh2] {
		t.Fatalf("chapter 2 didn't go up: %v", f.posted)
	}
	// All of volume 1 is read now: the volume counts as read.
	vols, _ := repo.Volumes(tower.ID)
	_ = repo.FillReading(user, tower.ID, vols)
	s.mdSyncOpened(t.Context(), user, tower, mdTower)
	vols, _ = repo.Volumes(tower.ID)
	_ = repo.FillReading(user, tower.ID, vols)
	if len(vols) == 0 || !vols[0].Read {
		t.Fatalf("volume 1 isn't read: %+v", vols)
	}

	// Unread on MangaDex: unread here too, at the next look.
	f.mu.Lock()
	delete(f.read[mdTower], mdCh1)
	f.mu.Unlock()
	if _, err := s.mdSyncOpened(t.Context(), user, tower, mdTower); err != nil {
		t.Fatal(err)
	}
	list, _ = repo.Chapters(tower.ID)
	_ = repo.FillChapterReading(user, tower.ID, list)
	for _, c := range list {
		if c.Number == "1" && c.Read {
			t.Fatal("chapter 1 is still read here")
		}
		if c.Number == "2" && !c.Read {
			t.Fatal("chapter 2 lost its mark with its volume")
		}
	}

	// Unfollow and status here go straight up.
	rec = mdCall(t, s, s.handleMangaDexFollow, http.MethodPut, "/", `{"following":false}`, user, "id", fmt.Sprint(tower.ID))
	if rec.Code != http.StatusOK || f.follows[mdTower] {
		t.Fatalf("unfollow: %d %s, there %v", rec.Code, rec.Body.String(), f.follows[mdTower])
	}
	rec = mdCall(t, s, s.handleMangaDexStatus, http.MethodPut, "/", `{"status":"completed"}`, user, "id", fmt.Sprint(tower.ID))
	if rec.Code != http.StatusOK || f.statuses[mdTower] != "completed" {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	rec = mdCall(t, s, s.handleMangaDexStatus, http.MethodPut, "/", `{"status":"skimming"}`, user, "id", fmt.Sprint(tower.ID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a made-up status: %d", rec.Code)
	}
	// Out of a list.
	rec = mdCall(t, s, s.handleMangaDexInList, http.MethodPut, "/", `{"in":false}`, user, "id", fmt.Sprint(tower.ID), "list", mdList)
	if rec.Code != http.StatusOK || strings.Contains(strings.Join(f.lists[mdList], ","), mdTower) {
		t.Fatalf("list: %d %s %v", rec.Code, rec.Body.String(), f.lists[mdList])
	}

	// Disconnect deletes everything kept from MangaDex.
	rec = mdCall(t, s, s.handleDeleteMangaDexAccount, http.MethodDelete, "/", "", user)
	if rec.Code != http.StatusOK {
		t.Fatalf("disconnect: %d", rec.Code)
	}
	for _, table := range []string{"manga_md_accounts", "manga_md_follow_synced", "manga_md_read_synced", "manga_md_outside", "manga_md_lists", "manga_md_feed", "manga_md_history"} {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE user_id = ?`, user).Scan(&n)
		if n != 0 {
			t.Errorf("%s still has %d rows", table, n)
		}
	}
}

func TestMangaDexAccountRefusedSignInStops(t *testing.T) {
	f := newFakeAccountDex(t)
	f.refuse = true
	s, repo, user := accountServer(t, f)
	rec := mdCall(t, s, s.handlePutMangaDexAccount, http.MethodPut, "/", mdGoodSignIn, user)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "didn't accept the sign-in") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	a, err := repo.DexAccountOf(user)
	if err != nil || a.State != manga.DexAccountRefused {
		t.Fatalf("state %+v %v", a, err)
	}
	before := f.count("POST /auth/token")
	s.mangaDexAccountJob(t.Context())
	s.mangaDexAccountJob(t.Context())
	if got := f.count("POST /auth/token"); got != before {
		t.Fatalf("asked again after being turned down: %d", got-before)
	}
	rec = mdCall(t, s, s.handlePutMangaDexAccount, http.MethodPut, "/", `{"clientId":"x"}`, user)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("half a sign-in: %d", rec.Code)
	}
}

func TestMangaDexAccountNoAddWithoutChaptersSwitch(t *testing.T) {
	f := newFakeAccountDex(t)
	s, repo, user := accountServer(t, f)
	if rec := mdCall(t, s, s.handlePutMangaDexAccount, http.MethodPut, "/", mdGoodSignIn, user); rec.Code != http.StatusOK {
		t.Fatalf("connect: %d", rec.Code)
	}
	s.bg.Wait()
	sr, err := repo.SeriesByDex(mdNew)
	if err != nil {
		t.Fatal(err)
	}
	// MangaDex chapters are off for the house: added, with nothing downloading.
	if d := repo.DexStateOf(sr.ID); d.Follow != manga.FollowOff {
		t.Fatalf("chapters followed with MangaDex switched off: %+v", d)
	}
	ids := []string{}
	for k := range f.follows {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	if len(ids) != 3 {
		t.Fatalf("follows changed there: %v", ids)
	}
}
