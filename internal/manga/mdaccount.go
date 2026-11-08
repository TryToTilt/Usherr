package manga

// A person's own MangaDex account: signing in with a personal API client, and the
// calls that need it (follows, read marks, reading status, lists, the follow feed,
// reading history). Only personal clients exist today: client id and secret, plus
// the account's username and password. They skip two-factor sign-in.
//
// Rules kept here, from https://api.mangadex.org/docs/:
//
//   - The sign-in and the access token go only to auth.mangadex.org and
//     api.mangadex.org, never to an image server (*.mangadex.network or
//     uploads.mangadex.org). Calls that don't need the account are made without it,
//     so MangaDex can answer them from its cache.
//   - Everything goes through the client's one pace, and its waits after a 429 or a
//     ban, shared with the chapter downloads.
//   - When MangaDex turns the sign-in down, that's the end of it: no trying again
//     until the person saves their details again.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dexAuthURL     = "https://auth.mangadex.org/realms/mangadex/protocol/openid-connect/token"
	dexCoverBase   = "https://uploads.mangadex.org/covers/"
	dexTokenMargin = 45 * time.Second // a token this close to its end is renewed first
	dexIDsPerCall  = 100
)

// ErrDexSignIn means MangaDex didn't accept the account's sign-in: wrong details,
// a client that isn't approved yet, or one that was removed.
var ErrDexSignIn = errors.New("MangaDex didn't accept the sign-in")

// DexCredentials are what a personal client signs in with.
type DexCredentials struct {
	ClientID     string
	ClientSecret string
	Username     string
	Password     string
}

// Complete says whether every field is there.
func (c DexCredentials) Complete() bool {
	return strings.TrimSpace(c.ClientID) != "" && c.ClientSecret != "" && strings.TrimSpace(c.Username) != "" && c.Password != ""
}

// DexSession is one person's signed-in MangaDex. The refresh token can be kept
// between restarts (OnRefresh is told each new one); the access token only lives
// here.
type DexSession struct {
	Client    *MangaDex
	AuthURL   string
	Creds     DexCredentials
	OnRefresh func(refresh string) // a new refresh token, to keep

	mu      sync.Mutex
	access  string
	expires time.Time
	refresh string
	refused bool
}

// DexAuthURLFor is the token address for a client: MangaDex's own, or the test
// copy's /auth/token.
func DexAuthURLFor(c *MangaDex) string {
	if c.Base == dexBaseURL {
		return dexAuthURL
	}
	return c.Base + "/auth/token"
}

// NewDexSession makes a session; refresh is a kept refresh token, or "".
func NewDexSession(c *MangaDex, creds DexCredentials, refresh string) *DexSession {
	return &DexSession{Client: c, AuthURL: DexAuthURLFor(c), Creds: creds, refresh: refresh}
}

// Refused says whether MangaDex turned this sign-in down.
func (s *DexSession) Refused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refused
}

type dexTokenAnswer struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	ExpiresIn int    `json:"expires_in"`
	Error     string `json:"error"`
}

// token is a valid access token, signing in or renewing first when needed.
func (s *DexSession) token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refused {
		return "", ErrDexSignIn
	}
	if s.access != "" && time.Now().Add(dexTokenMargin).Before(s.expires) {
		return s.access, nil
	}
	if s.refresh != "" {
		err := s.grant(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {s.refresh}})
		if err == nil {
			return s.access, nil
		}
		if !errors.Is(err, ErrDexSignIn) {
			return "", err
		}
		s.refresh = "" // worn out: sign in once with the details instead
	}
	if !s.Creds.Complete() {
		s.refused = true
		return "", ErrDexSignIn
	}
	err := s.grant(ctx, url.Values{"grant_type": {"password"}, "username": {s.Creds.Username}, "password": {s.Creds.Password}})
	if errors.Is(err, ErrDexSignIn) {
		s.refused = true
	}
	if err != nil {
		return "", err
	}
	return s.access, nil
}

// grant asks the token address once. Call with mu held.
func (s *DexSession) grant(ctx context.Context, form url.Values) error {
	c := s.Client
	if err := c.turn(ctx, &c.nextAPI, c.APIGap); err != nil {
		return err
	}
	form.Set("client_id", s.Creds.ClientID)
	form.Set("client_secret", s.Creds.ClientSecret)
	if !dexAuthHost(s.AuthURL, c.Base) {
		return errors.New("that isn't MangaDex's sign-in address")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.AuthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return c.hold(DexDown, time.Now().Add(dexDownWait), "no answer")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var answer dexTokenAnswer
	_ = json.Unmarshal(data, &answer)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return c.hold(DexBusy, waitFrom(resp.Header, dexBusyWait), "")
	case resp.StatusCode >= 500:
		return c.hold(DexDown, time.Now().Add(dexDownWait), fmt.Sprintf("it answered %d", resp.StatusCode))
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || answer.Error != "":
		return ErrDexSignIn // invalid_grant, invalid_client, unauthorized_client: the person must look
	case resp.StatusCode != http.StatusOK || answer.Access == "":
		return fmt.Errorf("MangaDex's sign-in answered %d", resp.StatusCode)
	}
	life := time.Duration(answer.ExpiresIn) * time.Second
	if life <= 0 {
		life = 15 * time.Minute
	}
	s.access, s.expires = answer.Access, time.Now().Add(life)
	if answer.Refresh != "" && answer.Refresh != s.refresh {
		s.refresh = answer.Refresh
		if s.OnRefresh != nil {
			s.OnRefresh(answer.Refresh)
		}
	}
	return nil
}

// dexAuthHost says whether a sign-in or a call with the account may go to target:
// MangaDex's API and sign-in, or the test copy standing in for them.
func dexAuthHost(target, base string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme == "https" && (host == "api.mangadex.org" || host == "auth.mangadex.org") {
		return true
	}
	if b, err := url.Parse(base); err == nil && base != dexBaseURL {
		return strings.EqualFold(b.Host, u.Host) // a test copy
	}
	return false
}

// call asks the API with the account. One 401 gets one new token and one more go.
func (s *DexSession) call(ctx context.Context, method, path string, q url.Values, body, out any) error {
	for try := 0; try < 2; try++ {
		tok, err := s.token(ctx)
		if err != nil {
			return err
		}
		err = s.Client.authed(ctx, method, path, q, body, tok, out)
		if !errors.Is(err, errDexUnauthorized) {
			return err
		}
		s.mu.Lock()
		s.access = ""
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.refused = true
	s.mu.Unlock()
	return ErrDexSignIn
}

var errDexUnauthorized = errors.New("unauthorized")

// authed is get with a token and a method. MangaDex's own "you may not" (a 403 with
// its error list) is that call's answer; any other 403 is a ban, as for get.
func (c *MangaDex) authed(ctx context.Context, method, path string, q url.Values, body any, token string, out any) error {
	if err := c.turn(ctx, &c.nextAPI, c.APIGap); err != nil {
		return err
	}
	target := c.Base + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	if !dexAuthHost(target, c.Base) {
		return errors.New("that isn't MangaDex's API")
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return c.hold(DexDown, time.Now().Add(dexDownWait), "no answer")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errDexUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests:
		return c.hold(DexBusy, waitFrom(resp.Header, dexBusyWait), "")
	case resp.StatusCode == http.StatusForbidden:
		if dexErrorList(data) {
			return fmt.Errorf("MangaDex didn't allow that (%d)", resp.StatusCode)
		}
		return c.hold(DexRefused, time.Now().Add(dexRefusedWait), "")
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode >= 500:
		return c.hold(DexDown, time.Now().Add(dexDownWait), fmt.Sprintf("it answered %d", resp.StatusCode))
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fmt.Errorf("MangaDex turned the request down (%d)", resp.StatusCode)
	case err != nil:
		return c.hold(DexDown, time.Now().Add(dexDownWait), "the answer broke off")
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		c.hold(DexBusy, waitFrom(resp.Header, dexBusyWait), "")
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		// Usherr: an answer in a shape Usherr doesn't expect is Usherr's to fix,
		// not MangaDex being away, so nothing else is held up by it.
		slog.Warn("manga: a MangaDex answer couldn't be read", "path", path, "err", err)
		return fmt.Errorf("MangaDex's answer to %s couldn't be read", path)
	}
	return nil
}

// dexErrorList says whether a body is MangaDex's own error answer.
func dexErrorList(data []byte) bool {
	var e struct {
		Result string            `json:"result"`
		Errors []json.RawMessage `json:"errors"`
	}
	return json.Unmarshal(data, &e) == nil && e.Result == "error" && len(e.Errors) > 0
}

// ---- what the account can be asked ----

// DexFollowTitle is a series as a follow, a list or the feed shows it.
type DexFollowTitle struct {
	DexSeries
	Cover string `json:"cover,omitempty"` // on uploads.mangadex.org: fetched without the account
}

// Adult says whether a content rating is for 18+ accounts only.
func DexAdult(rating string) bool { return rating == "erotica" || rating == "pornographic" }

type dexMangaFull struct {
	dexMangaJSON
	Relationships []struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes *struct {
			FileName string `json:"fileName"`
		} `json:"attributes"`
	} `json:"relationships"`
}

func (m dexMangaFull) title() DexFollowTitle {
	out := DexFollowTitle{DexSeries: m.series()}
	for _, r := range m.Relationships {
		if r.Type == "cover_art" && r.Attributes != nil && r.Attributes.FileName != "" && !strings.ContainsAny(r.Attributes.FileName, "/?#") {
			out.Cover = dexCoverBase + m.ID + "/" + r.Attributes.FileName + ".256.jpg"
		}
	}
	return out
}

var dexAllRatings = []string{"safe", "suggestive", "erotica", "pornographic"}

// Follows is every series the account follows, up to max (0: no end but MangaDex's
// own 10,000), and how many there are.
func (s *DexSession) Follows(ctx context.Context, max int) ([]DexFollowTitle, int, error) {
	var out []DexFollowTitle
	total := 0
	for offset := 0; offset+dexIDsPerCall <= dexMaxOffset; offset += dexIDsPerCall {
		var answer struct {
			Data  []dexMangaFull `json:"data"`
			Total int            `json:"total"`
		}
		q := url.Values{"limit": {strconv.Itoa(dexIDsPerCall)}, "offset": {strconv.Itoa(offset)}, "includes[]": {"cover_art"}}
		if err := s.call(ctx, http.MethodGet, "/user/follows/manga", q, nil, &answer); err != nil {
			return nil, 0, err
		}
		total = answer.Total
		for _, m := range answer.Data {
			out = append(out, m.title())
		}
		if len(answer.Data) == 0 || offset+len(answer.Data) >= answer.Total || (max > 0 && len(out) >= max) {
			break
		}
	}
	return out, total, nil
}

// SetFollow follows or unfollows a series.
func (s *DexSession) SetFollow(ctx context.Context, dexID string, on bool) error {
	if !IsDexID(dexID) {
		return ErrNotFound
	}
	method := http.MethodPost
	if !on {
		method = http.MethodDelete
	}
	err := s.call(ctx, method, "/manga/"+dexID+"/follow", nil, nil, nil)
	if errors.Is(err, ErrNotFound) && !on {
		return nil // not followed there: that's the wish anyway
	}
	return err
}

// ReadMarks are the chapters read, by series, for up to 100 series at a time.
func (s *DexSession) ReadMarks(ctx context.Context, dexIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	for start := 0; start < len(dexIDs); start += dexIDsPerCall {
		part := dexIDs[start:min(start+dexIDsPerCall, len(dexIDs))]
		var answer struct {
			Data json.RawMessage `json:"data"`
		}
		q := url.Values{"ids[]": part, "grouped": {"true"}}
		if err := s.call(ctx, http.MethodGet, "/manga/read", q, nil, &answer); err != nil {
			return nil, err
		}
		var grouped map[string][]string
		if json.Unmarshal(answer.Data, &grouped) == nil {
			for k, v := range grouped {
				out[strings.ToLower(k)] = v
			}
			continue
		}
		var flat []string // one series can come back as a plain list
		if json.Unmarshal(answer.Data, &flat) == nil && len(part) == 1 {
			out[strings.ToLower(part[0])] = flat
		}
	}
	return out, nil
}

// SetReadMarks marks chapters of one series read and unread, 100 at a time.
func (s *DexSession) SetReadMarks(ctx context.Context, dexID string, read, unread []string) error {
	if !IsDexID(dexID) {
		return ErrNotFound
	}
	for len(read) > 0 || len(unread) > 0 {
		body := map[string][]string{}
		if n := min(len(read), dexIDsPerCall); n > 0 {
			body["chapterIdsRead"], read = read[:n], read[n:]
		}
		if n := min(len(unread), dexIDsPerCall); n > 0 {
			body["chapterIdsUnread"], unread = unread[:n], unread[n:]
		}
		if err := s.call(ctx, http.MethodPost, "/manga/"+dexID+"/read", url.Values{"updateHistory": {"true"}}, body, nil); err != nil {
			return err
		}
	}
	return nil
}

// ReadingStatuses are the account's reading status of every series that has one.
func (s *DexSession) ReadingStatuses(ctx context.Context) (map[string]string, error) {
	var answer struct {
		Statuses json.RawMessage `json:"statuses"`
	}
	if err := s.call(ctx, http.MethodGet, "/manga/status", nil, nil, &answer); err != nil {
		return nil, err
	}
	// MangaDex sends an empty list as [] rather than {}.
	statuses := map[string]string{}
	if raw := bytes.TrimSpace(answer.Statuses); len(raw) > 0 && raw[0] == '{' {
		if err := json.Unmarshal(raw, &statuses); err != nil {
			return nil, fmt.Errorf("MangaDex's reading statuses couldn't be read: %w", err)
		}
	}
	out := map[string]string{}
	for k, v := range statuses {
		if ValidReadingStatus(v) {
			out[strings.ToLower(k)] = v
		}
	}
	return out, nil
}

// ReadingStatuses MangaDex knows. "" is none.
var readingStatuses = []string{"reading", "on_hold", "plan_to_read", "dropped", "re_reading", "completed"}

// ValidReadingStatus says whether s is one of them, or none.
func ValidReadingStatus(s string) bool {
	if s == "" {
		return true
	}
	for _, v := range readingStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// SetReadingStatus sets a series' status; "" takes it away.
func (s *DexSession) SetReadingStatus(ctx context.Context, dexID, status string) error {
	if !IsDexID(dexID) || !ValidReadingStatus(status) {
		return ErrNotFound
	}
	var v *string
	if status != "" {
		v = &status
	}
	return s.call(ctx, http.MethodPost, "/manga/"+dexID+"/status", nil, map[string]*string{"status": v}, nil)
}

// DexAccountList is one of the account's MDLists.
type DexAccountList struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Visibility string   `json:"visibility"`
	Manga      []string `json:"manga"`
}

// Lists are the account's lists, public and private, with their series.
func (s *DexSession) Lists(ctx context.Context) ([]DexAccountList, error) {
	var out []DexAccountList
	for offset := 0; offset+dexIDsPerCall <= dexMaxOffset; offset += dexIDsPerCall {
		var answer struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					Name       string `json:"name"`
					Visibility string `json:"visibility"`
				} `json:"attributes"`
				Relationships []dexRelation `json:"relationships"`
			} `json:"data"`
			Total int `json:"total"`
		}
		q := url.Values{"limit": {strconv.Itoa(dexIDsPerCall)}, "offset": {strconv.Itoa(offset)}}
		if err := s.call(ctx, http.MethodGet, "/user/list", q, nil, &answer); err != nil {
			return nil, err
		}
		for _, l := range answer.Data {
			if !IsDexID(l.ID) {
				continue
			}
			item := DexAccountList{ID: strings.ToLower(l.ID), Name: strings.TrimSpace(l.Attributes.Name), Visibility: l.Attributes.Visibility, Manga: []string{}}
			for _, r := range l.Relationships {
				if r.Type == "manga" && IsDexID(r.ID) {
					item.Manga = append(item.Manga, strings.ToLower(r.ID))
				}
			}
			out = append(out, item)
		}
		if len(answer.Data) == 0 || offset+len(answer.Data) >= answer.Total {
			break
		}
	}
	return out, nil
}

// SetInList adds a series to a list, or takes it out.
func (s *DexSession) SetInList(ctx context.Context, dexID, listID string, in bool) error {
	if !IsDexID(dexID) || !IsDexID(listID) {
		return ErrNotFound
	}
	method := http.MethodPost
	if !in {
		method = http.MethodDelete
	}
	return s.call(ctx, method, "/manga/"+dexID+"/list/"+listID, nil, nil, nil)
}

// DexFeedChapter is a new chapter of a followed series.
type DexFeedChapter struct {
	ID         string `json:"id"`
	DexID      string `json:"dexId"`
	Number     string `json:"number"`
	Volume     string `json:"volume,omitempty"`
	Title      string `json:"title,omitempty"`
	Group      string `json:"group,omitempty"`
	ReadableAt string `json:"readableAt"`
	Series     string `json:"-"`
	Rating     string `json:"-"`
}

// FollowFeed is the newest chapters of followed series, newest first, in a
// language and the given content ratings.
func (s *DexSession) FollowFeed(ctx context.Context, language string, ratings []string, limit int) ([]DexFeedChapter, error) {
	q := url.Values{
		"limit": {strconv.Itoa(limit)}, "translatedLanguage[]": {language}, "order[readableAt]": {"desc"},
		"includes[]": {"manga", "scanlation_group"}, "contentRating[]": ratings, "includeFutureUpdates": {"0"},
	}
	var answer struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Volume     *string `json:"volume"`
				Chapter    *string `json:"chapter"`
				Title      *string `json:"title"`
				ReadableAt string  `json:"readableAt"`
			} `json:"attributes"`
			Relationships []struct {
				ID         string          `json:"id"`
				Type       string          `json:"type"`
				Attributes json.RawMessage `json:"attributes"`
			} `json:"relationships"`
		} `json:"data"`
	}
	if err := s.call(ctx, http.MethodGet, "/user/follows/manga/feed", q, nil, &answer); err != nil {
		return nil, err
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	out := make([]DexFeedChapter, 0, len(answer.Data))
	for _, d := range answer.Data {
		c := DexFeedChapter{ID: strings.ToLower(d.ID), Number: str(d.Attributes.Chapter), Volume: str(d.Attributes.Volume), Title: str(d.Attributes.Title), ReadableAt: d.Attributes.ReadableAt}
		for _, r := range d.Relationships {
			switch r.Type {
			case "manga":
				c.DexID = strings.ToLower(r.ID)
				var m dexMangaJSON
				m.ID = r.ID
				if len(r.Attributes) > 0 && json.Unmarshal(r.Attributes, &m.Attributes) == nil {
					ser := m.series()
					c.Series, c.Rating = ser.Title, ser.Rating
				}
			case "scanlation_group":
				var g struct {
					Name string `json:"name"`
				}
				if len(r.Attributes) > 0 && json.Unmarshal(r.Attributes, &g) == nil && c.Group == "" {
					c.Group = strings.TrimSpace(g.Name)
				}
			}
		}
		if IsDexID(c.ID) && IsDexID(c.DexID) {
			out = append(out, c)
		}
	}
	return out, nil
}

// DexRead is one chapter in the reading history.
type DexRead struct {
	ChapterID string
	ReadAt    string
}

// History is the account's reading history, newest first as MangaDex gives it.
func (s *DexSession) History(ctx context.Context) ([]DexRead, error) {
	var answer struct {
		Ratings []struct {
			ChapterID string `json:"chapterId"`
			ReadDate  string `json:"readDate"`
		} `json:"ratings"`
	}
	if err := s.call(ctx, http.MethodGet, "/user/history", nil, nil, &answer); err != nil {
		return nil, err
	}
	out := make([]DexRead, 0, len(answer.Ratings))
	for _, r := range answer.Ratings {
		if IsDexID(r.ChapterID) {
			out = append(out, DexRead{ChapterID: strings.ToLower(r.ChapterID), ReadAt: r.ReadDate})
		}
	}
	return out, nil
}

// ---- public calls, made without the account ----

// FollowTitlesByID reads series by id, with their covers, whatever their rating: the caller
// decides who may see what.
func (c *MangaDex) FollowTitlesByID(ctx context.Context, ids []string) ([]DexFollowTitle, error) {
	var out []DexFollowTitle
	for start := 0; start < len(ids); start += dexIDsPerCall {
		part := ids[start:min(start+dexIDsPerCall, len(ids))]
		var answer struct {
			Data []dexMangaFull `json:"data"`
		}
		q := url.Values{"ids[]": part, "limit": {strconv.Itoa(dexIDsPerCall)}, "includes[]": {"cover_art"}, "contentRating[]": dexAllRatings}
		if err := c.get(ctx, "/manga", q, &c.nextAPI, c.APIGap, &answer); err != nil {
			return nil, err
		}
		for _, m := range answer.Data {
			out = append(out, m.title())
		}
	}
	return out, nil
}

// DexChapterRef is which series, volume and chapter number a chapter id is.
type DexChapterRef struct {
	ChapterID string
	DexID     string
	Volume    string // normalised, "" for none
	Number    string // normalised, OneshotNumber for none
}

func dexNumber(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "none") {
		return OneshotNumber
	}
	return NormalNumber(s)
}

func dexVolume(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "none") {
		return ""
	}
	return NormalNumber(s)
}

// Aggregate is every chapter of a series in a language, by volume, with all its
// uploads' ids.
func (c *MangaDex) Aggregate(ctx context.Context, dexID, language string) ([]DexChapterRef, error) {
	if !IsDexID(dexID) {
		return nil, ErrNotFound
	}
	var answer struct {
		Volumes json.RawMessage `json:"volumes"`
	}
	q := url.Values{"translatedLanguage[]": {language}}
	if err := c.get(ctx, "/manga/"+dexID+"/aggregate", q, &c.nextAPI, c.APIGap, &answer); err != nil {
		return nil, err
	}
	type chap struct {
		Chapter string   `json:"chapter"`
		ID      string   `json:"id"`
		Others  []string `json:"others"`
	}
	type vol struct {
		Volume   string          `json:"volume"`
		Chapters json.RawMessage `json:"chapters"`
	}
	// Both come as an object, or as a list when the keys are 0, 1, 2...
	var vols []vol
	var vm map[string]vol
	if json.Unmarshal(answer.Volumes, &vm) == nil {
		for _, v := range vm {
			vols = append(vols, v)
		}
	} else {
		_ = json.Unmarshal(answer.Volumes, &vols)
	}
	var out []DexChapterRef
	for _, v := range vols {
		var chs []chap
		var cm map[string]chap
		if json.Unmarshal(v.Chapters, &cm) == nil {
			for _, ch := range cm {
				chs = append(chs, ch)
			}
		} else {
			_ = json.Unmarshal(v.Chapters, &chs)
		}
		for _, ch := range chs {
			for _, id := range append([]string{ch.ID}, ch.Others...) {
				if IsDexID(id) {
					out = append(out, DexChapterRef{ChapterID: strings.ToLower(id), DexID: strings.ToLower(dexID), Volume: dexVolume(v.Volume), Number: dexNumber(ch.Chapter)})
				}
			}
		}
	}
	return out, nil
}

// ChapterRefs looks chapters up by id, 100 at a time.
func (c *MangaDex) ChapterRefs(ctx context.Context, ids []string) ([]DexChapterRef, error) {
	var out []DexChapterRef
	for start := 0; start < len(ids); start += dexIDsPerCall {
		part := ids[start:min(start+dexIDsPerCall, len(ids))]
		var answer struct {
			Data []dexChapterJSON `json:"data"`
		}
		q := url.Values{"ids[]": part, "limit": {strconv.Itoa(dexIDsPerCall)}, "contentRating[]": dexAllRatings, "includeUnavailable": {"1"}, "includeExternalUrl": {"1"}, "includeEmptyPages": {"1"}}
		if err := c.get(ctx, "/chapter", q, &c.nextAPI, c.APIGap, &answer); err != nil {
			return nil, err
		}
		for _, j := range answer.Data {
			ref := DexChapterRef{ChapterID: strings.ToLower(j.ID)}
			for _, r := range j.Relationships {
				if r.Type == "manga" {
					ref.DexID = strings.ToLower(r.ID)
				}
			}
			ch := j.chapter()
			ref.Volume, ref.Number = dexVolume(ch.Volume), dexNumber(ch.Number)
			if ref.DexID != "" {
				out = append(out, ref)
			}
		}
	}
	return out, nil
}
