package api

// Usherr: each person can connect their own MangaDex account to Manga (optional,
// off until they do). Their follows, read marks, reading status and lists then
// stay the same in both places, and the Manga page shows their lists, new chapters
// of what they follow, and what they read lately on MangaDex. This file: the
// routes, the sign-in, and the series page's part. The syncing itself is in
// home_manga_account_sync.go. See docs/manga.md.
//
// The sign-in is a personal API client's: client id and secret, username and
// password, all encrypted with the server key like indexer passwords, never
// logged and never sent back to the page.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/manga"
)

const (
	mdSignInRefused = "MangaDex didn't accept the sign-in. Check the four details, and that the API client is approved on mangadex.org, then save them again."
	mdManualEvery   = 10 * time.Minute // the least time between "Sync now"s
	mdOpenEvery     = 2 * time.Minute  // the least time between looks when a series page opens
	mdCoverPrefix   = "https://uploads.mangadex.org/covers/"
)

// mdAccounts keeps each person's signed-in session and their sync lock.
type mdAccounts struct {
	mu       sync.Mutex
	sessions map[int64]*manga.DexSession
	locks    map[int64]*sync.Mutex
	opened   map[[2]int64]time.Time // when a series page last asked, per person
	manual   map[int64]time.Time
	prints   map[int64]map[int64]mdPrint // read marks seen, per person and series
}

var mdAccountStates sync.Map

func (s *Server) mdAcc() *mdAccounts {
	v, _ := mdAccountStates.LoadOrStore(s, &mdAccounts{sessions: map[int64]*manga.DexSession{}, locks: map[int64]*sync.Mutex{}, opened: map[[2]int64]time.Time{}, manual: map[int64]time.Time{}, prints: map[int64]map[int64]mdPrint{}})
	return v.(*mdAccounts)
}

func (a *mdAccounts) lock(user int64) *sync.Mutex {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.locks[user] == nil {
		a.locks[user] = &sync.Mutex{}
	}
	return a.locks[user]
}

func (a *mdAccounts) forget(user int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, user)
	delete(a.prints, user)
}

// mdSession is a person's signed-in MangaDex, or ErrNotFound when they haven't
// connected one, or ErrDexSignIn when MangaDex turned it down.
func (s *Server) mdSession(user int64) (*manga.DexSession, error) {
	acc := s.mdAcc()
	acc.mu.Lock()
	sess := acc.sessions[user]
	acc.mu.Unlock()
	if sess != nil {
		return sess, nil
	}
	a, err := s.mangaRepo().DexAccountOf(user)
	if err != nil {
		return nil, err
	}
	if a.State == manga.DexAccountRefused {
		return nil, manga.ErrDexSignIn
	}
	var creds manga.DexCredentials
	var refresh string
	for _, f := range []struct {
		enc string
		out *string
	}{{a.ClientID, &creds.ClientID}, {a.ClientSecret, &creds.ClientSecret}, {a.Username, &creds.Username}, {a.Password, &creds.Password}, {a.Refresh, &refresh}} {
		if f.enc == "" {
			continue
		}
		plain, err := s.box.Decrypt(f.enc)
		if err != nil {
			return nil, errors.New("the saved MangaDex sign-in can't be read: save it again")
		}
		*f.out = plain
	}
	sess = manga.NewDexSession(s.mangaMod().dex, creds, refresh)
	sess.OnRefresh = func(token string) {
		if enc, err := s.box.Encrypt(token); err == nil {
			_ = s.mangaRepo().SetDexRefresh(user, enc)
		}
	}
	acc.mu.Lock()
	if have := acc.sessions[user]; have != nil {
		sess = have
	} else {
		acc.sessions[user] = sess
	}
	acc.mu.Unlock()
	return sess, nil
}

// mdRun asks MangaDex with a person's account, and keeps what the answer says
// about the account and about MangaDex being away.
func (s *Server) mdRun(user int64, fn func(*manga.DexSession) error) error {
	sess, err := s.mdSession(user)
	if err != nil {
		return err
	}
	err = fn(sess)
	switch {
	case err == nil:
		if a, e := s.mangaRepo().DexAccountOf(user); e == nil && (a.State != manga.DexAccountOK || a.Problem != "") {
			_ = s.mangaRepo().SetDexAccountState(user, manga.DexAccountOK, "")
		}
	case errors.Is(err, manga.ErrDexSignIn):
		_ = s.mangaRepo().SetDexAccountState(user, manga.DexAccountRefused, mdSignInRefused)
		s.mdAcc().forget(user)
		slog.Info("manga: MangaDex turned a person's sign-in down; nothing more is asked until they save it again", "user", user)
	default:
		s.noteDexTrouble(err)
	}
	return err
}

// mdAllows says whether an account has a permission, for work done in the
// background, with nobody's request at hand.
func (s *Server) mdAllows(user int64, perm string) bool {
	if u, err := s.Auth.GetUser(user); err == nil && u != nil && u.IsAdmin {
		return true
	}
	p, err := s.Auth.PermissionsOf(user)
	return err == nil && p.Allows(perm)
}

// mdAdultFor says whether a person may see series MangaDex rates erotica or
// pornographic: only when the NSFW section is switched on and they may see it.
func (s *Server) mdAdultFor(user int64) bool {
	return s.nsfwEnabled() && s.mdAllows(user, auth.PermNSFW)
}

func (s *Server) mdAdultRequest(r *http.Request) bool {
	return s.nsfwEnabled() && s.canSeeAdultManga(r)
}

func (s *Server) registerMangaAccountRoutes(member routeGroup, on func(http.HandlerFunc) http.HandlerFunc) {
	member.HandleFunc("GET /api/manga/md/account", on(s.handleGetMangaDexAccount))
	member.HandleFunc("PUT /api/manga/md/account", on(s.handlePutMangaDexAccount))
	member.HandleFunc("DELETE /api/manga/md/account", on(s.handleDeleteMangaDexAccount))
	member.HandleFunc("POST /api/manga/md/sync", on(s.handleMangaDexSyncNow))
	member.HandleFunc("GET /api/manga/md/shelves", on(s.handleMangaDexShelves))
	member.HandleFunc("GET /api/manga/md/cover", on(s.handleMangaDexAccountCover))
	member.HandleFunc("GET /api/manga/{id}/md", on(s.handleMangaDexSeries))
	member.HandleFunc("PUT /api/manga/{id}/md/follow", on(s.handleMangaDexFollow))
	member.HandleFunc("PUT /api/manga/{id}/md/status", on(s.handleMangaDexStatus))
	member.HandleFunc("PUT /api/manga/{id}/md/lists/{list}", on(s.handleMangaDexInList))
}

// ---- the account ----

type mdAccountView struct {
	Connected   bool                `json:"connected"`
	State       string              `json:"state,omitempty"`
	Problem     string              `json:"problem,omitempty"`
	ConnectedAt string              `json:"connectedAt,omitempty"`
	LightAt     string              `json:"lightAt,omitempty"`
	FullAt      string              `json:"fullAt,omitempty"`
	Stats       *manga.DexSyncStats `json:"stats,omitempty"`
	ChaptersOn  bool                `json:"chaptersOn"` // MangaDex chapters are switched on for the house
	Away        *mangaDexAwayState  `json:"away,omitempty"`
}

func (s *Server) mdAccountView(user int64) mdAccountView {
	v := mdAccountView{ChaptersOn: s.currentMangaSources().MangaDex, Away: s.mangaDexAway()}
	a, err := s.mangaRepo().DexAccountOf(user)
	if err != nil {
		return v
	}
	v.Connected, v.State, v.Problem, v.ConnectedAt, v.LightAt, v.FullAt = true, a.State, a.Problem, a.ConnectedAt, a.LightAt, a.FullAt
	stats := a.Stats
	v.Stats = &stats
	return v
}

// GET /api/manga/md/account: whether this person connected MangaDex, and how it's
// going. The sign-in itself never comes back: only that it's saved.
func (s *Server) handleGetMangaDexAccount(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mdAccountView(requestUserID(r)))
}

// PUT /api/manga/md/account {"clientId","clientSecret","username","password"}:
// saves this person's sign-in, encrypted, and tries it once.
func (s *Server) handlePutMangaDexAccount(w http.ResponseWriter, r *http.Request) {
	user := requestUserID(r)
	if user <= 0 {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	var req struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		Username     string `json:"username"`
		Password     string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	creds := manga.DexCredentials{ClientID: strings.TrimSpace(req.ClientID), ClientSecret: strings.TrimSpace(req.ClientSecret), Username: strings.TrimSpace(req.Username), Password: req.Password}
	if !creds.Complete() {
		writeError(w, http.StatusBadRequest, "Fill in all four: client id, client secret, username and password.")
		return
	}
	if len(creds.ClientID)+len(creds.ClientSecret)+len(creds.Username)+len(creds.Password) > 2000 {
		writeError(w, http.StatusBadRequest, "Those are too long to be a MangaDex sign-in.")
		return
	}
	a := manga.DexAccount{UserID: user}
	for _, f := range []struct {
		plain string
		out   *string
	}{{creds.ClientID, &a.ClientID}, {creds.ClientSecret, &a.ClientSecret}, {creds.Username, &a.Username}, {creds.Password, &a.Password}} {
		enc, err := s.box.Encrypt(f.plain)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "The sign-in couldn't be saved.")
			return
		}
		*f.out = enc
	}
	if err := s.mangaRepo().SaveDexAccount(a); err != nil {
		writeError(w, http.StatusInternalServerError, "The sign-in couldn't be saved.")
		return
	}
	s.mdAcc().forget(user)
	if st := s.mangaMod().dex.Paused(); st != nil {
		writeJSON(w, http.StatusOK, s.mdAccountView(user)) // tried once MangaDex is back
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	err := s.mdRun(user, func(sess *manga.DexSession) error {
		_, _, err := sess.Follows(ctx, 1)
		return err
	})
	cancel()
	if errors.Is(err, manga.ErrDexSignIn) {
		writeError(w, http.StatusBadRequest, mdSignInRefused)
		return
	}
	if err == nil {
		s.background(func() { s.mdSyncSoon(user, mdSyncManual) })
	}
	writeJSON(w, http.StatusOK, s.mdAccountView(user))
}

// DELETE /api/manga/md/account: forgets the sign-in and everything synced from it.
func (s *Server) handleDeleteMangaDexAccount(w http.ResponseWriter, r *http.Request) {
	user := requestUserID(r)
	lock := s.mdAcc().lock(user)
	lock.Lock()
	defer lock.Unlock()
	if err := s.mangaRepo().DeleteDexAccount(user); err != nil {
		writeError(w, http.StatusInternalServerError, "It couldn't be disconnected. Try again.")
		return
	}
	s.mdAcc().forget(user)
	writeJSON(w, http.StatusOK, map[string]string{"message": "MangaDex is disconnected, and everything Usherr kept from it is deleted. Your follows and reading status in Usherr stay."})
}

// POST /api/manga/md/sync: sync now, in the background.
func (s *Server) handleMangaDexSyncNow(w http.ResponseWriter, r *http.Request) {
	user := requestUserID(r)
	a, err := s.mangaRepo().DexAccountOf(user)
	if err != nil {
		writeError(w, http.StatusNotFound, "Connect your MangaDex account first.")
		return
	}
	if a.State == manga.DexAccountRefused {
		writeError(w, http.StatusConflict, mdSignInRefused)
		return
	}
	acc := s.mdAcc()
	acc.mu.Lock()
	last := acc.manual[user]
	if time.Since(last) < mdManualEvery {
		acc.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "It synced a few minutes ago. Try again in a little while.")
		return
	}
	acc.manual[user] = time.Now()
	acc.mu.Unlock()
	s.background(func() { s.mdSyncSoon(user, mdSyncManual) })
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "Syncing with MangaDex. This page updates when it's done."})
}

// ---- the series page ----

type mdSeriesView struct {
	Connected bool         `json:"connected"`
	Refused   bool         `json:"refused,omitempty"`
	Linked    bool         `json:"linked"`
	DexID     string       `json:"dexId,omitempty"`
	Following bool         `json:"following"`
	Status    string       `json:"status"`
	Lists     []mdListMark `json:"lists"`
	Changed   bool         `json:"changed,omitempty"` // read marks changed: read the series again
}

type mdListMark struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	In   bool   `json:"in"`
}

func (s *Server) mdSeriesViewOf(user int64, sr manga.Series) mdSeriesView {
	repo := s.mangaRepo()
	v := mdSeriesView{Following: repo.Following(user, sr.ID), Lists: []mdListMark{}}
	v.Status, _ = repo.ReadingStatus(user, sr.ID)
	if a, err := repo.DexAccountOf(user); err == nil {
		v.Connected, v.Refused = true, a.State == manga.DexAccountRefused
	}
	if d := repo.DexStateOf(sr.ID); d.Link == manga.DexLinked && d.DexID != "" {
		v.Linked, v.DexID = true, strings.ToLower(d.DexID)
	}
	if v.Connected && v.Linked {
		lists, _ := repo.DexLists(user)
		for _, l := range lists {
			in := false
			for _, id := range l.Manga {
				if id == v.DexID {
					in = true
					break
				}
			}
			v.Lists = append(v.Lists, mdListMark{ID: l.ID, Name: l.Name, In: in})
		}
	}
	return v
}

// GET /api/manga/{id}/md: this person's follow, status and lists for a series.
// Opening the page also brings in its read marks from MangaDex, at most every two
// minutes.
func (s *Server) handleMangaDexSeries(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	user := requestUserID(r)
	v := s.mdSeriesViewOf(user, sr)
	if v.Connected && !v.Refused && v.Linked && s.mangaMod().dex.Paused() == nil {
		acc := s.mdAcc()
		key := [2]int64{user, sr.ID}
		acc.mu.Lock()
		due := time.Since(acc.opened[key]) >= mdOpenEvery
		if due {
			acc.opened[key] = time.Now()
		}
		acc.mu.Unlock()
		if lock := acc.lock(user); due && lock.TryLock() {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			changed, _ := s.mdSyncOpened(ctx, user, sr, v.DexID)
			cancel()
			lock.Unlock()
			v = s.mdSeriesViewOf(user, sr)
			v.Changed = changed
		}
	}
	writeJSON(w, http.StatusOK, v)
}

// PUT /api/manga/{id}/md/follow {"following"}: follows here, and on MangaDex too.
func (s *Server) handleMangaDexFollow(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Following bool `json:"following"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	user := requestUserID(r)
	repo := s.mangaRepo()
	if err := repo.SetFollowing(user, sr.ID, req.Following); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	v := s.mdSeriesViewOf(user, sr)
	note := ""
	switch {
	case !v.Connected || v.Refused:
	case !v.Linked:
		note = "Saved in Usherr. This series isn't matched to MangaDex yet, so it'll follow there once it is."
		if req.Following {
			s.background(func() { s.mdLinkThenSync(user, sr) })
		}
	default:
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		err := s.mdRun(user, func(sess *manga.DexSession) error { return sess.SetFollow(ctx, v.DexID, req.Following) })
		cancel()
		if err == nil {
			_ = repo.SetFollowSyncedOne(user, v.DexID, req.Following)
		} else {
			note = "Saved in Usherr. MangaDex couldn't be told just now; Usherr tries again on its next sync."
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"md": v, "note": note})
}

// PUT /api/manga/{id}/md/status {"status"}: reading, on_hold, plan_to_read,
// dropped, re_reading, completed, or "" for none.
func (s *Server) handleMangaDexStatus(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &req); err != nil || !manga.ValidReadingStatus(req.Status) {
		writeError(w, http.StatusBadRequest, "That isn't a reading status.")
		return
	}
	user := requestUserID(r)
	repo := s.mangaRepo()
	if err := repo.SetReadingStatus(user, sr.ID, req.Status); err != nil {
		writeError(w, http.StatusInternalServerError, "That couldn't be saved.")
		return
	}
	v := s.mdSeriesViewOf(user, sr)
	note := ""
	if v.Connected && !v.Refused && v.Linked {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		err := s.mdRun(user, func(sess *manga.DexSession) error { return sess.SetReadingStatus(ctx, v.DexID, req.Status) })
		cancel()
		if err == nil {
			_ = repo.SetReadingStatusSynced(user, sr.ID, req.Status)
		} else {
			note = "Saved in Usherr. MangaDex couldn't be told just now; Usherr tries again on its next sync."
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"md": v, "note": note})
}

// PUT /api/manga/{id}/md/lists/{list} {"in"}: puts a series in one of this
// person's MangaDex lists, or takes it out.
func (s *Server) handleMangaDexInList(w http.ResponseWriter, r *http.Request) {
	sr, ok := s.mangaFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		In bool `json:"in"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	user := requestUserID(r)
	listID := strings.ToLower(r.PathValue("list"))
	v := s.mdSeriesViewOf(user, sr)
	known := false
	for _, l := range v.Lists {
		known = known || l.ID == listID
	}
	switch {
	case !v.Connected || v.Refused:
		writeError(w, http.StatusConflict, "Connect your MangaDex account first.")
		return
	case !v.Linked:
		writeError(w, http.StatusConflict, "This series isn't matched to MangaDex yet.")
		return
	case !known:
		writeError(w, http.StatusNotFound, "That list isn't one of yours.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	err := s.mdRun(user, func(sess *manga.DexSession) error { return sess.SetInList(ctx, v.DexID, listID, req.In) })
	cancel()
	if err != nil {
		writeError(w, http.StatusBadGateway, "MangaDex couldn't change the list just now. Try again in a minute.")
		return
	}
	_ = s.mangaRepo().SetInDexList(user, listID, v.DexID, req.In)
	writeJSON(w, http.StatusOK, map[string]any{"md": s.mdSeriesViewOf(user, sr)})
}

// ---- shelves on the Manga page ----

type mdShelfItem struct {
	DexID     string `json:"dexId"`
	Title     string `json:"title"`
	Cover     string `json:"cover,omitempty"`
	SeriesID  int64  `json:"seriesId,omitempty"` // in the library
	Number    string `json:"number,omitempty"`   // a chapter
	Group     string `json:"group,omitempty"`
	At        string `json:"at,omitempty"`
	Reason    string `json:"reason,omitempty"` // why it isn't in the library
	ChapterID string `json:"chapterId,omitempty"`
}

type mdShelf struct {
	ID    string        `json:"id"`
	Name  string        `json:"name"`
	Items []mdShelfItem `json:"items"`
	More  int           `json:"more,omitempty"`
}

const mdShelfMax = 30

// GET /api/manga/md/shelves: this person's MangaDex shelves. Series MangaDex rates
// erotica or pornographic are left out unless the NSFW section is on and the
// person may see it.
func (s *Server) handleMangaDexShelves(w http.ResponseWriter, r *http.Request) {
	user := requestUserID(r)
	repo := s.mangaRepo()
	a, err := repo.DexAccountOf(user)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false})
		return
	}
	adult := s.mdAdultRequest(r)
	dress := s.mdDresser(adult)
	feed, _ := repo.DexFeed(user)
	history, _ := repo.DexHistory(user)
	lists, _ := repo.DexLists(user)
	outside, _ := repo.DexOutside(user)
	var ids []string
	for _, f := range feed {
		ids = append(ids, f.DexID)
	}
	for _, h := range history {
		ids = append(ids, h.DexID)
	}
	for _, l := range lists {
		ids = append(ids, l.Manga...)
	}
	for id := range outside {
		ids = append(ids, id)
	}
	dress.load(ids)
	out := map[string]any{"connected": true, "state": a.State, "problem": a.Problem}
	newCh := []mdShelfItem{}
	for _, f := range feed {
		if it, ok := dress.item(f.DexID); ok && len(newCh) < mdShelfMax {
			it.Number, it.Group, it.At, it.ChapterID = f.Number, f.Group, f.ReadableAt, f.ID
			newCh = append(newCh, it)
		}
	}
	recent := []mdShelfItem{}
	seen := map[string]bool{}
	for _, h := range history {
		if h.DexID == "" || seen[h.DexID] {
			continue
		}
		seen[h.DexID] = true
		if it, ok := dress.item(h.DexID); ok && len(recent) < mdShelfMax {
			it.Number, it.At = h.Number, h.ReadAt
			recent = append(recent, it)
		}
	}
	shelves := []mdShelf{}
	for _, l := range lists {
		sh := mdShelf{ID: l.ID, Name: l.Name, Items: []mdShelfItem{}}
		for _, id := range l.Manga {
			it, ok := dress.item(id)
			if !ok {
				continue
			}
			if len(sh.Items) >= mdShelfMax {
				sh.More++
				continue
			}
			sh.Items = append(sh.Items, it)
		}
		shelves = append(shelves, sh)
	}
	away := []mdShelfItem{}
	for _, id := range manga.SortedKeys(func() map[string]bool {
		m := map[string]bool{}
		for id := range outside {
			m[id] = true
		}
		return m
	}()) {
		if it, ok := dress.item(id); ok && it.SeriesID == 0 {
			it.Reason = outside[id]
			away = append(away, it)
		}
	}
	out["newChapters"], out["recent"], out["lists"], out["outside"] = newCh, recent, shelves, away
	writeJSON(w, http.StatusOK, out)
}

// mdDresser turns MangaDex ids into shelf items, from the library or kept titles,
// leaving out what this person may not see.
type mdDresser struct {
	s      *Server
	adult  bool
	titles map[string]manga.DexFollowTitle
	series map[string]*manga.Series
}

func (s *Server) mdDresser(adult bool) *mdDresser {
	return &mdDresser{s: s, adult: adult, series: map[string]*manga.Series{}}
}

func (d *mdDresser) load(ids []string) {
	seen := map[string]bool{}
	var list []string
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			list = append(list, id)
		}
	}
	d.titles, _ = d.s.mangaRepo().DexTitlesOf(list)
	for _, id := range list {
		if sr, err := d.s.mangaRepo().SeriesByDex(id); err == nil {
			d.series[id] = &sr
		}
	}
}

func (d *mdDresser) item(id string) (mdShelfItem, bool) {
	it := mdShelfItem{DexID: id}
	t, known := d.titles[id]
	if sr := d.series[id]; sr != nil {
		if sr.IsAdult && !d.adult {
			return it, false
		}
		it.SeriesID, it.Title, it.Cover = sr.ID, sr.Title, d.s.mangaImage(sr.CoverURL)
	}
	if known {
		if manga.DexAdult(t.Rating) && !d.adult {
			return it, false
		}
		if it.Title == "" {
			it.Title = t.Title
		}
		if it.Cover == "" && t.Cover != "" {
			it.Cover = "/api/manga/md/cover?u=" + url.QueryEscape(t.Cover)
		}
	}
	if it.Title == "" {
		return it, false // nothing known yet: its rating included, so it isn't shown
	}
	return it, true
}

var mdCoverClient = &http.Client{Timeout: 20 * time.Second}

// GET /api/manga/md/cover?u=: a cover from uploads.mangadex.org, for a title Usherr
// has seen. Fetched without the account: login details never go to image servers.
func (s *Server) handleMangaDexAccountCover(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	if len(u) > 512 || !strings.HasPrefix(u, mdCoverPrefix) || strings.Contains(u, "..") || !s.mangaRepo().DexCoverKnown(u) {
		writeError(w, http.StatusNotFound, "Unknown image.")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad image address.")
		return
	}
	req.Header.Set("User-Agent", "Usherr/"+s.version)
	resp, err := mdCoverClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "The image couldn't be fetched.")
		return
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") || strings.Contains(ct, "svg") {
		writeError(w, http.StatusBadGateway, "The image couldn't be fetched.")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 8<<20))
}
