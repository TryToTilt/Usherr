// Package lastfm talks to Last.fm's scrobbling API (ws.audioscrobbler.com):
// the web sign-in that turns a token into a session key, "now playing",
// scrobbles (up to 50 per call) and love/unlove.
//
// Adapted from DroppedNeedle (AGPL-3.0), backend/repositories/lastfm_repository.py
// and backend/services/lastfm_auth_service.py: the call signing and the
// session flow. Batching and the error kinds are Usherr's own.
package lastfm

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/netguard"
)

const (
	// DefaultBaseURL is Last.fm's API.
	DefaultBaseURL = "https://ws.audioscrobbler.com/2.0/"
	// AuthURL is the page where a person lets an app use their account.
	AuthURL = "https://www.last.fm/api/auth/"
	// MaxScrobbles is how many plays one scrobble call takes.
	MaxScrobbles = 50
	timeout      = 20 * time.Second
)

// Errors a caller treats differently.
var (
	// ErrBadSession: the session key was revoked; the person must connect again.
	ErrBadSession = errors.New("Last.fm no longer accepts this connection")
	// ErrBadKey: the API key or secret is wrong.
	ErrBadKey = errors.New("Last.fm doesn't accept the API key or secret")
	// ErrBadToken: the sign-in token was not approved or has expired.
	ErrBadToken = errors.New("Last.fm didn't approve the sign-in")
	// ErrRateLimited: Last.fm asked for fewer calls.
	ErrRateLimited = errors.New("Last.fm asked for fewer requests")
)

// APIError is any other error Last.fm answered with.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("Last.fm error %d: %s", e.Code, e.Message) }

// Track is a play as Last.fm wants it.
type Track struct {
	Artist      string
	Title       string
	Album       string
	AlbumArtist string
	DurationSec int
	TrackNumber int
	MBID        string // recording id, when known
	Timestamp   int64  // unix seconds the play started (scrobbles only)
}

// Client calls Last.fm with one application's key and secret.
type Client struct {
	key, secret string
	base        string
	http        *http.Client
	userAgent   string
}

// Option changes a Client.
type Option func(*Client)

// WithBaseURL points the client at another server (a fake in tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.base = u } }

// WithHTTPClient replaces the HTTP client (tests).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New returns a client for an application's key and secret.
func New(key, secret, version string, opts ...Option) *Client {
	c := &Client{key: key, secret: secret, base: DefaultBaseURL, http: netguard.Client(timeout), userAgent: "Usherr/" + version}
	for _, o := range opts {
		o(c)
	}
	return c
}

// AuthPageURL is where to send a person to let Usherr scrobble for them.
// Last.fm comes back to callback with ?token=… added.
func (c *Client) AuthPageURL(callback string) string {
	return AuthURL + "?" + url.Values{"api_key": {c.key}, "cb": {callback}}.Encode()
}

// Sign is Last.fm's api_sig: every parameter but format and callback, sorted
// by name, written name then value, the secret added, then MD5.
func Sign(params url.Values, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "format" || k == "callback" || k == "api_sig" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(params.Get(k))
	}
	b.WriteString(secret)
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// call makes a signed call. GET for auth.getSession, POST for the rest.
func (c *Client) call(ctx context.Context, method string, params url.Values, out any) error {
	params.Set("api_key", c.key)
	params.Set("api_sig", Sign(params, c.secret))
	params.Set("format", "json")
	var req *http.Request
	var err error
	if method == http.MethodGet {
		req, err = http.NewRequestWithContext(ctx, method, c.base+"?"+params.Encode(), nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.base, strings.NewReader(params.Encode()))
		if req != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return fmt.Errorf("lastfm: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("lastfm: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("lastfm: read answer: %w", err)
	}
	var e struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != 0 {
		switch e.Error {
		case 9:
			return ErrBadSession
		case 10, 26, 13:
			return ErrBadKey
		case 4, 14, 15:
			return ErrBadToken
		case 29:
			return ErrRateLimited
		}
		return &APIError{Code: e.Error, Message: e.Message}
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("lastfm: HTTP %d", resp.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("lastfm: read answer: %w", err)
		}
	}
	return nil
}

// Session turns an approved token into the account's name and session key.
func (c *Client) Session(ctx context.Context, token string) (user, key string, err error) {
	var out struct {
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
	}
	if err := c.call(ctx, http.MethodGet, url.Values{"method": {"auth.getSession"}, "token": {token}}, &out); err != nil {
		return "", "", err
	}
	if out.Session.Key == "" {
		return "", "", ErrBadToken
	}
	return out.Session.Name, out.Session.Key, nil
}

func (t Track) put(v url.Values, suffix string) {
	v.Set("artist"+suffix, t.Artist)
	v.Set("track"+suffix, t.Title)
	if t.Album != "" {
		v.Set("album"+suffix, t.Album)
	}
	if t.AlbumArtist != "" && t.AlbumArtist != t.Artist {
		v.Set("albumArtist"+suffix, t.AlbumArtist)
	}
	if t.DurationSec > 0 {
		v.Set("duration"+suffix, strconv.Itoa(t.DurationSec))
	}
	if t.TrackNumber > 0 {
		v.Set("trackNumber"+suffix, strconv.Itoa(t.TrackNumber))
	}
	if t.MBID != "" {
		v.Set("mbid"+suffix, t.MBID)
	}
}

// NowPlaying tells Last.fm what the account is playing.
func (c *Client) NowPlaying(ctx context.Context, sessionKey string, t Track) error {
	v := url.Values{"method": {"track.updateNowPlaying"}, "sk": {sessionKey}}
	t.put(v, "")
	return c.call(ctx, http.MethodPost, v, nil)
}

// Scrobble sends up to MaxScrobbles plays in one call. It returns how many
// Last.fm accepted (it may ignore some, for example ones too old).
func (c *Client) Scrobble(ctx context.Context, sessionKey string, plays []Track) (int, error) {
	if len(plays) == 0 {
		return 0, nil
	}
	if len(plays) > MaxScrobbles {
		plays = plays[:MaxScrobbles]
	}
	v := url.Values{"method": {"track.scrobble"}, "sk": {sessionKey}}
	for i, t := range plays {
		s := "[" + strconv.Itoa(i) + "]"
		t.put(v, s)
		v.Set("timestamp"+s, strconv.FormatInt(t.Timestamp, 10))
	}
	var out struct {
		Scrobbles struct {
			Attr struct {
				Accepted int `json:"accepted"`
			} `json:"@attr"`
		} `json:"scrobbles"`
	}
	if err := c.call(ctx, http.MethodPost, v, &out); err != nil {
		return 0, err
	}
	return out.Scrobbles.Attr.Accepted, nil
}

// Love marks a track loved (or not) on the account.
func (c *Client) Love(ctx context.Context, sessionKey, artist, title string, love bool) error {
	m := "track.love"
	if !love {
		m = "track.unlove"
	}
	return c.call(ctx, http.MethodPost, url.Values{"method": {m}, "sk": {sessionKey}, "artist": {artist}, "track": {title}}, nil)
}

// ShouldScrobble is Last.fm's rule for when a play counts: the track is
// longer than 30 seconds and has been played for half its length or four
// minutes, whichever comes first. lengthSec 0 (unknown) never counts, as
// Last.fm needs the length to judge.
func ShouldScrobble(lengthSec, playedSec float64) bool {
	if lengthSec <= 30 {
		return false
	}
	need := lengthSec / 2
	if need > 240 {
		need = 240
	}
	return playedSec >= need
}

// Temporary reports whether err is worth trying again later (the play is
// kept): anything but a refused session or key.
func Temporary(err error) bool {
	return err != nil && !errors.Is(err, ErrBadSession) && !errors.Is(err, ErrBadKey)
}
