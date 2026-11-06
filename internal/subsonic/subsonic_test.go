package subsonic_test

import (
	"encoding/json"
	"encoding/xml"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/subsonic"
)

var srv = subsonic.Server{Type: "usherr", Version: "9.9.9"}

func TestTokenMatchesTheSpecExample(t *testing.T) {
	// From subsonic.org/pages/api.jsp: password "sesame", salt "c19b2d".
	if !subsonic.TokenMatches("sesame", "c19b2d", "26719a1196d2a940705a59634eb18eab") {
		t.Fatal("the spec's example must match")
	}
	if !subsonic.TokenMatches("sesame", "c19b2d", "26719A1196D2A940705A59634EB18EAB") {
		t.Fatal("upper-case hex must match")
	}
	if subsonic.TokenMatches("sesame", "c19b2e", "26719a1196d2a940705a59634eb18eab") || subsonic.TokenMatches("other", "c19b2d", "26719a1196d2a940705a59634eb18eab") {
		t.Fatal("another salt or secret must not match")
	}
}

func TestReadCredentialsRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    string
		code int
	}{
		{"token and salt", "u=sam&t=abc&s=xyz", 0},
		{"plain password", "u=sam&p=secret", 0},
		{"hex password", "u=sam&p=enc:736563726574", 0},
		{"api key", "apiKey=k", 0},
		{"api key with a user", "apiKey=k&u=sam", subsonic.ErrConflictingAuth},
		{"api key with a token", "apiKey=k&t=a&s=b", subsonic.ErrConflictingAuth},
		{"password and token", "u=sam&p=x&t=a&s=b", subsonic.ErrMissingParam},
		{"token without salt", "u=sam&t=a", subsonic.ErrMissingParam},
		{"salt without token", "u=sam&s=a", subsonic.ErrMissingParam},
		{"no user", "p=x", subsonic.ErrMissingParam},
		{"nothing", "", subsonic.ErrMissingParam},
		{"twice", "u=sam&u=kim&p=x", subsonic.ErrMissingParam},
		{"bad hex", "u=sam&p=enc:zz", subsonic.ErrWrongAuth},
	} {
		q, _ := url.ParseQuery(tc.q)
		c, code, _ := subsonic.ReadCredentials(q)
		if code != tc.code {
			t.Errorf("%s: code %d, want %d", tc.name, code, tc.code)
		}
		if tc.name == "hex password" && c.Password != "secret" {
			t.Errorf("enc: password %q", c.Password)
		}
	}
}

func TestXMLAnswerShape(t *testing.T) {
	w := httptest.NewRecorder()
	srv.OK(w, subsonic.Format{}, subsonic.Map{"album": subsonic.Map{"id": "al-1", "name": `Rock & "Roll"`, "songCount": 2,
		"song": []subsonic.Map{{"id": "tr-1", "title": "One", "duration": 61}, {"id": "tr-2", "title": "Two\x01"}}}})
	body := w.Body.String()
	if !strings.HasPrefix(body, `<?xml version="1.0" encoding="UTF-8"?>`) || !strings.Contains(w.Header().Get("Content-Type"), "text/xml") {
		t.Fatalf("header: %s", body)
	}
	var doc struct {
		XMLName xml.Name `xml:"http://subsonic.org/restapi subsonic-response"`
		Status  string   `xml:"status,attr"`
		Version string   `xml:"version,attr"`
		Open    bool     `xml:"openSubsonic,attr"`
		Album   struct {
			ID    string `xml:"id,attr"`
			Name  string `xml:"name,attr"`
			Count int    `xml:"songCount,attr"`
			Songs []struct {
				ID       string `xml:"id,attr"`
				Title    string `xml:"title,attr"`
				Duration int    `xml:"duration,attr"`
			} `xml:"song"`
		} `xml:"album"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("not XML: %v\n%s", err, body)
	}
	if doc.Status != "ok" || doc.Version != subsonic.APIVersion || !doc.Open || doc.Album.Name != `Rock & "Roll"` || doc.Album.Count != 2 ||
		len(doc.Album.Songs) != 2 || doc.Album.Songs[0].Duration != 61 || doc.Album.Songs[1].Title != "Two" {
		t.Fatalf("parsed: %+v\n%s", doc, body)
	}

	w = httptest.NewRecorder()
	srv.Fail(w, subsonic.Format{}, subsonic.ErrNotFound, "Album not found.")
	if !strings.Contains(w.Body.String(), `status="failed"`) || !strings.Contains(w.Body.String(), `<error code="70" message="Album not found."/>`) || w.Code != 200 {
		t.Fatalf("error: %s", w.Body.String())
	}

	// Text content (lyrics lines).
	w = httptest.NewRecorder()
	srv.OK(w, subsonic.Format{}, subsonic.Map{"lyrics": subsonic.Map{"artist": "A", "value": "la <la>"}})
	if !strings.Contains(w.Body.String(), `<lyrics artist="A">la &lt;la&gt;</lyrics>`) {
		t.Fatalf("text: %s", w.Body.String())
	}
}

func TestJSONAndJSONPAnswerShape(t *testing.T) {
	q, _ := url.ParseQuery("f=json")
	w := httptest.NewRecorder()
	srv.OK(w, subsonic.FormatOf(q), subsonic.Map{"artists": subsonic.Map{"ignoredArticles": "The", "index": []subsonic.Map{}}, "nothing": nil})
	var doc map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("not JSON: %v %s", err, w.Body.String())
	}
	r := doc["subsonic-response"]
	if r["status"] != "ok" || r["version"] != subsonic.APIVersion || r["type"] != "usherr" || r["serverVersion"] != "9.9.9" || r["openSubsonic"] != true {
		t.Fatalf("envelope: %v", r)
	}
	if _, has := r["nothing"]; has {
		t.Fatal("nulls are left out")
	}
	if idx := r["artists"].(map[string]any)["index"]; idx == nil || len(idx.([]any)) != 0 {
		t.Fatalf("an empty list stays a list: %v", r["artists"])
	}

	q, _ = url.ParseQuery("f=jsonp&callback=cb.done")
	w = httptest.NewRecorder()
	srv.OK(w, subsonic.FormatOf(q), nil)
	if !strings.HasPrefix(w.Body.String(), `cb.done({"subsonic-response":`) || !strings.HasSuffix(w.Body.String(), "})") {
		t.Fatalf("jsonp: %s", w.Body.String())
	}
	q, _ = url.ParseQuery("f=jsonp&callback=alert(1)")
	if f := subsonic.FormatOf(q); !f.JSON || f.Callback != "" {
		t.Fatalf("a bad callback falls back to JSON: %+v", f)
	}
}

func TestEndpointName(t *testing.T) {
	for in, want := range map[string]string{"/rest/getAlbum.view": "getalbum", "/rest/ping": "ping", "/u/rest/getCoverArt": "getcoverart"} {
		if got := subsonic.Endpoint(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
