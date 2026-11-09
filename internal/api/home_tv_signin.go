package api

// Usherr: Sign in on a TV, and Finish on your phone.
//
// Sign in on a TV works like the "enter this code on your phone" sign-in of TV
// apps. The TV's sign-in page asks for a code (POST /api/auth/tv/start) and
// gets two things back: a short code to show (and a QR code of a link with it)
// and a long pairing secret it keeps to itself. Someone signed in on a phone
// opens the link (or types the code), sees what kind of device asked, and
// approves or denies. The TV asks every couple of seconds with its secret
// (POST /api/auth/tv/poll); once approved, that answer signs it in with a
// normal session cookie, as the person who approved.
//
// The rules: a code lasts five minutes and signs in one TV once; codes and
// tries are limited per address; only a few can wait at once; the TV never
// learns who will approve until it is signed in; the code and the QR carry no
// secret (the secret never leaves the TV, and a code alone signs nothing in);
// approving needs a browser session (not an API key) that was not itself made
// by a TV code; and an administrator can switch it off.
//
// Finish on your phone hands a page over: a signed-in browser (the TV) asks
// for a short-lived link to one of the app's own pages (POST
// /api/auth/handoff), shows it as a QR code, and the phone that opens it, signed
// in as the same person, goes to that page. Only a path inside the app is
// accepted, never a full address, and nothing secret goes in it.
//
// Everything here lives in memory: a restart forgets waiting codes, which is
// the same as them running out.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/httpsec"
	"github.com/rdborg/mediarium/internal/qr"
	"github.com/rdborg/mediarium/internal/settings"
)

const (
	tvCodeFor        = 5 * time.Minute // how long a TV code can be approved and used
	tvCodeLen        = 8               // shown as two groups of four
	tvMaxPending     = 100             // codes waiting at once, from everywhere
	tvMaxPendingIP   = 3               // codes waiting at once from one address
	tvStartsPerIP    = 10              // new codes per address per tvLimitWindow
	tvWrongPerUser   = 10              // unknown codes one account may try per tvLimitWindow
	tvLimitWindow    = 15 * time.Minute
	tvPollEvery      = 2                // seconds between the TV's checks
	handoffFor       = 10 * time.Minute // how long a Finish on your phone link works
	handoffMaxPerUse = 20               // links one account may have waiting at once
	handoffPathMax   = 512
	handoffWhatMax   = 80
)

// tvCodeAlphabet leaves out letters and digits that look alike (0 O, 1 I L)
// and U, which makes fewer words.
const tvCodeAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

// tvPending is one TV waiting for a phone.
type tvPending struct {
	code       string
	secretHash string
	ip         string // the TV's address
	device     string // "Chrome on Android TV"
	home       bool   // the TV is on the home network (or Tailscale)
	expires    time.Time
	userID     int64 // set by an approval
	denied     bool
}

// handoff is one Finish on your phone link.
type handoff struct {
	code    string
	userID  int64
	path    string
	what    string
	expires time.Time
	opened  bool
}

type tvSignInState struct {
	once     sync.Once
	mu       sync.Mutex
	byCode   map[string]*tvPending
	bySecret map[string]*tvPending
	handoffs map[string]*handoff
	shift    time.Duration // tests move the clock forward
	starts   *auth.LoginLimiter
	wrong    *auth.LoginLimiter
}

func (s *Server) tvs() *tvSignInState {
	st := &s.tvSign
	st.once.Do(func() {
		st.byCode = map[string]*tvPending{}
		st.bySecret = map[string]*tvPending{}
		st.handoffs = map[string]*handoff{}
		st.starts = auth.NewLoginLimiter(tvStartsPerIP, tvLimitWindow)
		st.wrong = auth.NewLoginLimiter(tvWrongPerUser, tvLimitWindow)
	})
	return st
}

func (st *tvSignInState) now() time.Time { return time.Now().Add(st.shift) }

// sweep forgets codes and links that ran out. Caller holds st.mu.
func (st *tvSignInState) sweep(now time.Time) {
	for k, p := range st.byCode {
		if now.After(p.expires) {
			delete(st.byCode, k)
			delete(st.bySecret, p.secretHash)
		}
	}
	for k, h := range st.handoffs {
		if now.After(h.expires) {
			delete(st.handoffs, k)
		}
	}
}

func (st *tvSignInState) drop(p *tvPending) {
	delete(st.byCode, p.code)
	delete(st.bySecret, p.secretHash)
}

func randomCode(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, v := range b {
		// 256 is not a multiple of 30, so a few letters come up a little more
		// often; with 30^8 codes that changes nothing that matters.
		out[i] = tvCodeAlphabet[int(v)%len(tvCodeAlphabet)]
	}
	return string(out), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// normalizeTVCode turns what someone typed ("abcd efgh", "ABCD-EFGH") into the
// stored form, or "" when it can't be a code.
func normalizeTVCode(typed string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(typed) {
		switch {
		case r == ' ' || r == '-':
			continue
		case strings.ContainsRune(tvCodeAlphabet, r):
			b.WriteRune(r)
		default:
			return ""
		}
		if b.Len() > tvCodeLen {
			return ""
		}
	}
	if b.Len() != tvCodeLen {
		return ""
	}
	return b.String()
}

// tvSignInOn says whether Sign in with your phone is offered (on unless an
// administrator switched it off).
func (s *Server) tvSignInOn() bool {
	v, _ := s.Settings.Get(settings.KeyAuthTVSignIn)
	return v != "0"
}

const tvOffMessage = "Signing in with your phone is turned off here. Ask an administrator."

// appAddress is where links for a phone start: the address of Usherr when it
// is set, otherwise the address this request came in on.
func (s *Server) appAddress(r *http.Request) string {
	if base := s.configuredPublicURL(); strings.HasPrefix(base, "https://") || strings.HasPrefix(base, "http://") {
		return base
	}
	scheme := "http"
	if s.isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + strings.TrimRight(s.cfg.URLBase, "/")
}

// deviceLabel names the kind of browser and device from its user agent, like
// "Chrome on Android TV". It is only a hint for the person approving.
func deviceLabel(ua string) string {
	has := func(s ...string) bool {
		for _, x := range s {
			if strings.Contains(ua, x) {
				return true
			}
		}
		return false
	}
	browser := "A browser"
	switch {
	case has("Silk/"):
		browser = "Silk"
	case has("SamsungBrowser"):
		browser = "Samsung Internet"
	case has("Edg/", "Edge/"):
		browser = "Edge"
	case has("Firefox/"):
		browser = "Firefox"
	case has("OPR/", "Opera"):
		browser = "Opera"
	case has("Chrome/", "CriOS/"):
		browser = "Chrome"
	case has("Safari/"):
		browser = "Safari"
	}
	device := ""
	switch {
	case has("AFT", "Fire TV"):
		device = "Fire TV"
	case has("SHIELD"):
		device = "NVIDIA Shield"
	case has("CrKey"):
		device = "Chromecast"
	case has("GoogleTV", "Google TV", "Android TV", "BRAVIA", "AndroidTV"):
		device = "Android TV"
	case has("Tizen"):
		device = "a Samsung TV"
	case has("Web0S", "webOS", "NetCast"):
		device = "an LG TV"
	case has("SMART-TV", "SmartTV", "HbbTV"):
		device = "a smart TV"
	case has("Xbox"):
		device = "Xbox"
	case has("PlayStation"):
		device = "PlayStation"
	case has("iPhone"):
		device = "iPhone"
	case has("iPad"):
		device = "iPad"
	case has("Android"):
		device = "Android"
	case has("CrOS"):
		device = "a Chromebook"
	case has("Macintosh", "Mac OS X"):
		device = "a Mac"
	case has("Windows"):
		device = "Windows"
	case has("Linux"):
		device = "Linux"
	}
	if device == "" {
		return browser
	}
	return browser + " on " + device
}

// ---- The TV side (no sign-in) ----

func (s *Server) registerTVSignInPublic(public *http.ServeMux) {
	public.HandleFunc("POST /api/auth/tv/start", s.handleTVStart)
	public.HandleFunc("POST /api/auth/tv/poll", s.handleTVPoll)
}

type tvStartReply struct {
	Code      string    `json:"code"`   // eight letters and digits, shown as ABCD-EFGH
	Secret    string    `json:"secret"` // the TV keeps this to itself; it never goes in the QR code
	URL       string    `json:"url"`    // what the QR code opens on the phone
	QR        []string  `json:"qr"`
	ExpiresAt time.Time `json:"expiresAt"`
	Interval  int       `json:"interval"` // seconds between checks
}

// POST /api/auth/tv/start {"replace"?: secret of the code it shows now}
func (s *Server) handleTVStart(w http.ResponseWriter, r *http.Request) {
	if !s.tvSignInOn() {
		writeError(w, http.StatusForbidden, tvOffMessage)
		return
	}
	var req struct {
		Replace string `json:"replace"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicBody)
	_ = decodeJSON(r, &req) // an empty body is fine
	ip := s.clientIP(r)
	ipKey := httpsec.LimitKey(ip)
	st := s.tvs()
	if !st.starts.Reserve(ipKey) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-in codes. Wait a few minutes and try again.")
		return
	}
	code, err := randomCode(tvCodeLen)
	secretBytes := make([]byte, 32)
	if _, e := rand.Read(secretBytes); err != nil || e != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't make a sign-in code. Try again.")
		return
	}
	secret := hex.EncodeToString(secretBytes)
	now := st.now()

	st.mu.Lock()
	st.sweep(now)
	if req.Replace != "" {
		if old, ok := st.bySecret[hashSecret(req.Replace)]; ok {
			st.drop(old)
		}
	}
	fromIP := 0
	for _, p := range st.byCode {
		if httpsec.LimitKey(p.ip) == ipKey {
			fromIP++
		}
	}
	if fromIP >= tvMaxPendingIP || len(st.byCode) >= tvMaxPending {
		st.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "Too many sign-in codes are waiting. Wait a few minutes and try again.")
		return
	}
	if _, taken := st.byCode[code]; taken {
		st.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "Couldn't make a sign-in code. Try again.")
		return
	}
	p := &tvPending{
		code: code, secretHash: hashSecret(secret), ip: ip, device: deviceLabel(r.UserAgent()),
		home: !publicAddress(ip), expires: now.Add(tvCodeFor),
	}
	st.byCode[code] = p
	st.bySecret[p.secretHash] = p
	st.mu.Unlock()

	link := s.appAddress(r) + "/tv-code?code=" + code
	out := tvStartReply{Code: code, Secret: secret, URL: link, ExpiresAt: p.expires, Interval: tvPollEvery}
	if c, err := qr.Encode([]byte(link)); err == nil {
		out.QR = c.Rows()
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/auth/tv/poll {"secret"}: {"status": "waiting" | "denied" |
// "expired"}, or, once approved, a session cookie and {"status": "approved"}
// with the account like a sign-in answers. It never says who will approve.
func (s *Server) handleTVPoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Secret string `json:"secret"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicBody)
	if err := decodeJSON(r, &req); err != nil || req.Secret == "" {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	st := s.tvs()
	now := st.now()
	st.mu.Lock()
	st.sweep(now)
	p, ok := st.bySecret[hashSecret(req.Secret)]
	if !ok {
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"status": "expired"})
		return
	}
	if p.denied {
		st.drop(p)
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"status": "denied"})
		return
	}
	if p.userID == 0 {
		st.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"status": "waiting"})
		return
	}
	// Approved: used up now, whatever happens next.
	st.drop(p)
	st.mu.Unlock()

	if !s.tvSignInOn() {
		writeError(w, http.StatusForbidden, tvOffMessage)
		return
	}
	user, err := s.Auth.GetUser(p.userID)
	if err != nil || user.Disabled {
		writeJSON(w, http.StatusOK, map[string]string{"status": "denied"})
		return
	}
	if err := s.Auth.RecordLogin(user.ID); err != nil {
		slog.Warn("auth: record sign-in", "err", err) // bookkeeping only
	}
	s.noteSignIn(r)
	token, expiresAt, err := s.Auth.CreateSessionOn(user.ID, auth.MethodTV, p.device)
	if err != nil {
		slog.Error("auth: TV sign-in", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't sign you in. Try again.")
		return
	}
	auth.SetSessionCookieAt(w, token, expiresAt, s.isHTTPS(r), s.cookiePath())
	slog.Info("auth: a TV signed in with a code", "user", user.Username, "device", p.device, "ip", s.clientIP(r))
	out := userPayload(user)
	out["status"] = "approved"
	writeJSON(w, http.StatusOK, out)
}

// ---- The phone side (signed in) ----

// A view-only account may sign its own TV in and hand its own pages to its
// phone: both concern only the person themselves (see readOnlyAllows).
func init() {
	viewerOwn["POST /api/auth/tv/code/{code}"] = true
	viewerOwn["POST /api/auth/handoff"] = true
}

func (s *Server) registerTVSignIn(member, admin routeGroup) {
	member.HandleFunc("GET /api/auth/tv/code/{code}", s.handleTVCodeInfo)
	member.HandleFunc("POST /api/auth/tv/code/{code}", s.handleTVCodeAnswer)
	member.HandleFunc("POST /api/auth/handoff", s.handleHandoffCreate)
	member.HandleFunc("GET /api/auth/handoff/{code}", s.handleHandoffOpen)
	member.HandleFunc("GET /api/auth/handoff/{code}/status", s.handleHandoffStatus)
	admin.HandleFunc("GET /api/auth/tv/settings", s.handleGetTVSignIn)
	admin.HandleFunc("PUT /api/auth/tv/settings", s.handlePutTVSignIn)
}

// approverOK checks the phone may approve a TV: signed in in this browser (an
// API key can't, and neither can a session a TV code made), and the feature on.
// It writes the refusal itself.
func (s *Server) approverOK(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return nil, false
	}
	if !requireSession(w, r) {
		return nil, false
	}
	c, err := r.Cookie(auth.CookieName)
	if err != nil {
		writeError(w, http.StatusForbidden, "Approve the TV from a browser where you signed in to Usherr itself.")
		return nil, false
	}
	if s.Auth.SessionMethod(c.Value) == auth.MethodTV {
		writeError(w, http.StatusForbidden, "A TV can't sign in another TV. Use your phone or computer.")
		return nil, false
	}
	if !s.tvSignInOn() {
		writeError(w, http.StatusForbidden, tvOffMessage)
		return nil, false
	}
	return user, true
}

const tvCodeGoneMessage = "That code has run out or was already used. Ask the TV for a new one."

// findTVCode looks up a waiting code for user, counting unknown codes against
// the account so codes can't be tried one after another. Caller must not hold
// st.mu. It writes the refusal itself.
func (s *Server) findTVCode(w http.ResponseWriter, r *http.Request, user *auth.User) (*tvPending, bool) {
	st := s.tvs()
	key := "tv:" + strconv.FormatInt(user.ID, 10)
	if !st.wrong.Allow(key) {
		writeError(w, http.StatusTooManyRequests, "Too many wrong codes. Wait a few minutes and try again.")
		return nil, false
	}
	code := normalizeTVCode(r.PathValue("code"))
	now := st.now()
	st.mu.Lock()
	st.sweep(now)
	p, ok := st.byCode[code]
	st.mu.Unlock()
	if code == "" || !ok || p.userID != 0 || p.denied {
		st.wrong.RecordFailure(key)
		writeError(w, http.StatusNotFound, tvCodeGoneMessage)
		return nil, false
	}
	return p, true
}

type tvCodeInfo struct {
	Code      string    `json:"code"`
	Device    string    `json:"device"`
	Home      bool      `json:"home"` // asked from the home network (or Tailscale)
	ExpiresAt time.Time `json:"expiresAt"`
}

// GET /api/auth/tv/code/{code}: what is asking, before the person approves.
func (s *Server) handleTVCodeInfo(w http.ResponseWriter, r *http.Request) {
	user, ok := s.approverOK(w, r)
	if !ok {
		return
	}
	p, ok := s.findTVCode(w, r, user)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, tvCodeInfo{Code: p.code, Device: p.device, Home: p.home, ExpiresAt: p.expires})
}

// POST /api/auth/tv/code/{code} {"approve": bool}
func (s *Server) handleTVCodeAnswer(w http.ResponseWriter, r *http.Request) {
	user, ok := s.approverOK(w, r)
	if !ok {
		return
	}
	var req struct {
		Approve bool `json:"approve"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	p, ok := s.findTVCode(w, r, user)
	if !ok {
		return
	}
	st := s.tvs()
	st.mu.Lock()
	// Answered by someone else in the meantime, or run out: the first answer counts.
	if cur, still := st.byCode[p.code]; !still || cur != p || p.userID != 0 || p.denied {
		st.mu.Unlock()
		writeError(w, http.StatusNotFound, tvCodeGoneMessage)
		return
	}
	if req.Approve {
		p.userID = user.ID
	} else {
		p.denied = true
	}
	device := p.device
	st.mu.Unlock()
	if req.Approve {
		s.auditAccount(r, fmt.Sprintf("Account %q signed in a TV (%s) with a code", user.Username, device))
		writeJSON(w, http.StatusOK, map[string]string{"status": "approved", "device": device})
		return
	}
	slog.Info("auth: a TV sign-in code was denied", "user", user.Username, "device", device)
	writeJSON(w, http.StatusOK, map[string]string{"status": "denied"})
}

// GET /api/auth/tv/settings
func (s *Server) handleGetTVSignIn(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": s.tvSignInOn()})
}

// PUT /api/auth/tv/settings {"enabled": bool}
func (s *Server) handlePutTVSignIn(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	v := "1"
	if !req.Enabled {
		v = "0"
	}
	if err := s.Settings.Set(settings.KeyAuthTVSignIn, v, false); err != nil {
		writeError(w, http.StatusInternalServerError, "Couldn't save that. Try again.")
		return
	}
	if !req.Enabled {
		// Codes waiting now can't be used any more either.
		st := s.tvs()
		st.mu.Lock()
		for _, p := range st.byCode {
			st.drop(p)
		}
		st.mu.Unlock()
		s.auditAccount(r, "Sign in with your phone (TV codes) was switched off")
	} else {
		s.auditAccount(r, "Sign in with your phone (TV codes) was switched on")
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}

// ---- Finish on your phone ----

// handoffFirst are the app's own pages a phone may be sent to (the first part
// of the path). Anything else is refused, so a link can only ever open a page of
// this app.
var handoffFirst = map[string]bool{
	"": true, "about": true, "apps": true, "book": true, "books": true, "bookshelf": true, "calendar": true,
	"comics": true, "crews": true, "discover": true, "following": true, "help": true, "import": true,
	"leaving-soon": true, "library": true, "manga": true, "music": true, "nsfw": true, "person": true,
	"preshow": true, "profile": true, "queue": true, "read-to-me": true, "search": true, "series": true,
	"server": true, "settings": true, "show": true, "showtime": true, "sports": true, "streamed": true,
	"title": true, "tv": true, "wanted": true, "you": true,
}

// validHandoffPath accepts a path inside the app ("/title/603?tab=reviews") and
// refuses anything that could leave it: a full address, "//host", backslashes,
// "..", control characters, or a first part not in handoffFirst.
func validHandoffPath(p string) bool {
	if p == "" || len(p) > handoffPathMax || p[0] != '/' || strings.HasPrefix(p, "//") {
		return false
	}
	for _, r := range p {
		if r > unicode.MaxASCII || r <= ' ' || r == 0x7f || r == '\\' || r == '"' || r == '<' || r == '>' || r == '`' {
			return false
		}
	}
	u, err := url.Parse(p)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return false
	}
	path := u.Path
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return false
	}
	parts := strings.Split(path[1:], "/")
	for _, seg := range parts {
		if seg == "." || seg == ".." {
			return false
		}
	}
	// The escaped form too: "%2e%2e" and "%2f" have no business here.
	low := strings.ToLower(u.EscapedPath())
	if strings.Contains(low, "%2e") || strings.Contains(low, "%2f") || strings.Contains(low, "%5c") {
		return false
	}
	return handoffFirst[parts[0]]
}

type handoffReply struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	QR        []string  `json:"qr"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// POST /api/auth/handoff {"path", "what"?}: a link the same person can open on
// their phone. Needs a browser session.
func (s *Server) handleHandoffCreate(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	user := auth.UserFromContext(r.Context())
	var req struct {
		Path string `json:"path"`
		What string `json:"what"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if !validHandoffPath(req.Path) {
		writeError(w, http.StatusBadRequest, "Only a page of this app can be opened on your phone.")
		return
	}
	what := strings.TrimSpace(req.What)
	if len(what) > handoffWhatMax || strings.IndexFunc(what, unicode.IsControl) >= 0 {
		what = ""
	}
	st := s.tvs()
	now := st.now()
	st.mu.Lock()
	st.sweep(now)
	mine := 0
	for _, h := range st.handoffs {
		if h.userID == user.ID {
			mine++
		}
	}
	if mine >= handoffMaxPerUse {
		st.mu.Unlock()
		writeError(w, http.StatusTooManyRequests, "Too many phone links are waiting. Wait a few minutes and try again.")
		return
	}
	var code string
	for range 5 {
		c, err := randomCode(tvCodeLen)
		if err != nil {
			break
		}
		if _, taken := st.handoffs[c]; !taken {
			code = c
			break
		}
	}
	if code == "" {
		st.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "Couldn't make a link. Try again.")
		return
	}
	h := &handoff{code: code, userID: user.ID, path: req.Path, what: what, expires: now.Add(handoffFor)}
	st.handoffs[code] = h
	st.mu.Unlock()

	link := s.appAddress(r) + "/p/" + code
	out := handoffReply{Code: code, URL: link, ExpiresAt: h.expires}
	if c, err := qr.Encode([]byte(link)); err == nil {
		out.QR = c.Rows()
	}
	writeJSON(w, http.StatusOK, out)
}

const handoffGoneMessage = "This link has run out or was already used. Make a new one on the TV."

// findHandoff answers the link behind {code} when it belongs to the caller. A
// link made by someone else is refused and left alone, so its owner can still
// use it. Caller holds st.mu.
func (s *Server) findHandoff(w http.ResponseWriter, r *http.Request, user *auth.User) (*handoff, bool) {
	st := s.tvs()
	key := "handoff:" + strconv.FormatInt(user.ID, 10)
	if !st.wrong.Allow(key) {
		writeError(w, http.StatusTooManyRequests, "Too many wrong codes. Wait a few minutes and try again.")
		return nil, false
	}
	st.sweep(st.now())
	h, ok := st.handoffs[normalizeTVCode(r.PathValue("code"))]
	if !ok {
		st.wrong.RecordFailure(key)
		writeError(w, http.StatusNotFound, handoffGoneMessage)
		return nil, false
	}
	if h.userID != user.ID {
		st.wrong.RecordFailure(key)
		writeError(w, http.StatusForbidden, "This link was made for another account. Sign in as the person who made it on the TV.")
		return nil, false
	}
	return h, true
}

// GET /api/auth/handoff/{code}: {"path", "what"}, once. The phone then opens path.
func (s *Server) handleHandoffOpen(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	st := s.tvs()
	st.mu.Lock()
	h, ok := s.findHandoff(w, r, user)
	if !ok {
		st.mu.Unlock()
		return
	}
	if h.opened {
		st.mu.Unlock()
		writeError(w, http.StatusNotFound, handoffGoneMessage)
		return
	}
	h.opened = true
	path, what := h.path, h.what
	st.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"path": path, "what": what})
}

// GET /api/auth/handoff/{code}/status: {"opened"}, for the TV showing the code.
func (s *Server) handleHandoffStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	st := s.tvs()
	st.mu.Lock()
	h, ok := s.findHandoff(w, r, user)
	if !ok {
		st.mu.Unlock()
		return
	}
	opened := h.opened
	st.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"opened": opened})
}
