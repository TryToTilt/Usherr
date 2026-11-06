// Package subsonic writes Subsonic and OpenSubsonic API answers (XML, JSON
// and JSONP) and checks the sign-in parameters phone apps send. The server
// side (routes, data) is in internal/api/subsonic*.go.
//
// Adapted from DroppedNeedle (AGPL-3.0), backend/api/compat/subsonic/
// serialization.py, auth.py and errors.py: the answer envelope, the rule that
// scalars are XML attributes and lists repeated elements, and the strict
// checks on the sign-in parameters.
package subsonic

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// APIVersion is the Subsonic API version answered.
const APIVersion = "1.16.1"

// Namespace is the Subsonic XML namespace.
const Namespace = "http://subsonic.org/restapi"

// Error codes of the Subsonic API.
const (
	ErrGeneric          = 0
	ErrMissingParam     = 10
	ErrClientTooOld     = 20
	ErrServerTooOld     = 30
	ErrWrongAuth        = 40
	ErrTokenNotForLDAP  = 41
	ErrAuthNotSupported = 42
	ErrConflictingAuth  = 43
	ErrBadAPIKey        = 44
	ErrNotAuthorized    = 50
	ErrTrialOver        = 60
	ErrNotFound         = 70
)

// Map is one element of an answer: scalar values are XML attributes, a Map
// is a child element, a []Map is repeated child elements, and the key
// "value" is the element's text.
type Map = map[string]any

// Server names the server in every answer.
type Server struct {
	Type    string // "usherr"
	Version string // Usherr's version
}

// Format is how the client asked to be answered.
type Format struct {
	JSON     bool
	Callback string // JSONP, "" for plain JSON
}

var callbackPattern = regexp.MustCompile(`^[A-Za-z_$][\w$.]{0,127}$`)

// FormatOf reads f= and callback=. Anything but json or jsonp is XML; a
// JSONP callback that isn't a plain name falls back to JSON.
func FormatOf(q url.Values) Format {
	switch strings.ToLower(q.Get("f")) {
	case "json":
		return Format{JSON: true}
	case "jsonp":
		cb := q.Get("callback")
		if !callbackPattern.MatchString(cb) {
			cb = ""
		}
		return Format{JSON: true, Callback: cb}
	}
	return Format{}
}

func (s Server) envelope(status string) Map {
	return Map{"status": status, "version": APIVersion, "type": s.Type, "serverVersion": s.Version, "openSubsonic": true}
}

// OK writes a successful answer with payload's elements inside.
func (s Server) OK(w http.ResponseWriter, f Format, payload Map) {
	body := s.envelope("ok")
	for k, v := range payload {
		body[k] = v
	}
	s.write(w, f, body)
}

// Fail writes an error answer. Subsonic answers errors with HTTP 200.
func (s Server) Fail(w http.ResponseWriter, f Format, code int, message string) {
	body := s.envelope("failed")
	body["error"] = Map{"code": code, "message": message}
	s.write(w, f, body)
}

func (s Server) write(w http.ResponseWriter, f Format, body Map) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	if f.JSON {
		b, err := json.Marshal(Map{"subsonic-response": clean(body)})
		if err != nil {
			http.Error(w, "encode answer", http.StatusInternalServerError)
			return
		}
		if f.Callback != "" {
			h.Set("Content-Type", "application/javascript; charset=utf-8")
			fmt.Fprintf(w, "%s(%s)", f.Callback, b)
			return
		}
		h.Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(b)
		return
	}
	h.Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = io.WriteString(w, xml.Header)
	writeXML(w, "subsonic-response", body, true)
}

// clean drops nil values so JSON has no nulls (some apps crash on them).
func clean(m Map) Map {
	out := make(Map, len(m))
	for k, v := range m {
		switch x := v.(type) {
		case nil:
			continue
		case Map:
			out[k] = clean(x)
		case []Map:
			list := make([]Map, len(x))
			for i, e := range x {
				list[i] = clean(e)
			}
			out[k] = list
		default:
			out[k] = v
		}
	}
	return out
}

// xmlText strips characters XML 1.0 can't carry.
func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, s)))
	return b.String()
}

func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	}
	return "", false
}

func writeXML(w io.Writer, name string, m Map, root bool) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<" + name)
	if root {
		b.WriteString(` xmlns="` + Namespace + `"`)
	}
	var kids []string
	text := ""
	for _, k := range keys {
		v := m[k]
		if v == nil {
			continue
		}
		if k == "value" {
			if s, ok := scalar(v); ok {
				text = s
				continue
			}
		}
		if s, ok := scalar(v); ok {
			b.WriteString(" " + k + `="` + xmlText(s) + `"`)
			continue
		}
		kids = append(kids, k)
	}
	if len(kids) == 0 && text == "" {
		b.WriteString("/>")
		_, _ = io.WriteString(w, b.String())
		return
	}
	b.WriteString(">")
	b.WriteString(xmlText(text))
	_, _ = io.WriteString(w, b.String())
	for _, k := range kids {
		switch x := m[k].(type) {
		case Map:
			writeXML(w, k, x, false)
		case []Map:
			for _, e := range x {
				writeXML(w, k, e, false)
			}
		case []int: // a list of numbers: <versions>1</versions>…
			for _, n := range x {
				_, _ = io.WriteString(w, "<"+k+">"+strconv.Itoa(n)+"</"+k+">")
			}
		case []string: // a list of words: <releaseTypes>Album</releaseTypes>…
			for _, v := range x {
				_, _ = io.WriteString(w, "<"+k+">"+xmlText(v)+"</"+k+">")
			}
		}
	}
	_, _ = io.WriteString(w, "</"+name+">")
}

// Credentials is how a request signs in.
type Credentials struct {
	User     string
	Password string // p=, already decoded from enc:
	Token    string // t=
	Salt     string // s=
	APIKey   string // apiKey=
}

// Length caps on the sign-in parameters.
const (
	maxUser   = 256
	maxSecret = 256
)

// ReadCredentials checks the sign-in parameters: each given once, a token
// with its salt, never a password with a token, never an API key with
// another way. code is 0 and ok true when they make sense.
func ReadCredentials(q url.Values) (c Credentials, code int, msg string) {
	for _, k := range []string{"u", "p", "t", "s", "apiKey"} {
		if len(q[k]) > 1 {
			return c, ErrMissingParam, "Parameter " + k + " is given more than once."
		}
	}
	c = Credentials{User: q.Get("u"), Token: strings.ToLower(q.Get("t")), Salt: q.Get("s"), APIKey: q.Get("apiKey")}
	p := q.Get("p")
	if strings.HasPrefix(p, "enc:") {
		raw, err := hex.DecodeString(p[4:])
		if err != nil {
			return c, ErrWrongAuth, "Wrong username or password."
		}
		p = string(raw)
	}
	c.Password = p
	if len(c.User) > maxUser || len(c.Password) > maxSecret || len(c.Token) > 128 || len(c.Salt) > 128 || len(c.APIKey) > maxSecret {
		return c, ErrWrongAuth, "Wrong username or password."
	}
	hasToken := c.Token != "" || c.Salt != ""
	switch {
	case c.APIKey != "" && c.User != "":
		return c, ErrConflictingAuth, "Send either an API key or a user name, not both."
	case c.APIKey != "" && (hasToken || c.Password != ""):
		return c, ErrConflictingAuth, "Send only one way of signing in."
	case c.APIKey != "":
		return c, 0, ""
	case c.Password != "" && hasToken:
		return c, ErrMissingParam, "Send either a password or a token, not both."
	case hasToken && (c.Token == "" || c.Salt == ""):
		return c, ErrMissingParam, "A token needs its salt (t and s)."
	case c.User == "":
		return c, ErrMissingParam, "Required parameter u is missing."
	case c.Password == "" && !hasToken:
		return c, ErrMissingParam, "Required parameter p, or t and s, is missing."
	}
	return c, 0, ""
}

// TokenMatches reports whether t is md5(secret + salt), in constant time.
func TokenMatches(secret, salt, token string) bool {
	sum := md5.Sum([]byte(secret + salt))
	want := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(want), []byte(strings.ToLower(token))) == 1
}

// Endpoint is the name a request path asks for: "/rest/getAlbum.view" and
// "/rest/getalbum" are both "getalbum".
func Endpoint(path string) string {
	name := path[strings.LastIndex(path, "/")+1:]
	return strings.ToLower(strings.TrimSuffix(name, ".view"))
}
