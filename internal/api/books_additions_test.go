package api_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/api"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/kosync"
	"github.com/rdborg/mediarium/internal/notify"
)

type editedBook struct {
	Title          string `json:"title"`
	Author         string `json:"author"`
	AuthorKey      string `json:"authorKey"`
	Year           int    `json:"year"`
	Description    string `json:"description"`
	SeriesName     string `json:"seriesName"`
	SeriesPosition string `json:"seriesPosition"`
	OLKey          string `json:"olKey"`
}

func TestEditBookDetails(t *testing.T) {
	f := newEReaderFixture(t)
	if err := f.server.BookRepo.SetSeries(f.dune.ID, "From a look-up", "9"); err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("%s/api/books/%d", f.base, f.dune.ID)
	got := postJSONMethod[editedBook](t, f.admin, http.MethodPut, url, map[string]any{
		"title": "  Dune   Messiah ", "author": "Someone Else", "seriesName": "Dune", "seriesPosition": "2", "year": 1969, "description": " Sand. ",
	}, http.StatusOK)
	if got.Title != "Dune Messiah" || got.Author != "Someone Else" || got.Year != 1969 || got.Description != "Sand." || got.SeriesName != "Dune" || got.SeriesPosition != "2" {
		t.Fatalf("got %+v", got)
	}

	// A look-up doesn't change the series back, and the file stays where it was.
	if err := f.server.BookRepo.SetSeries(f.dune.ID, "From a look-up", "9"); err != nil {
		t.Fatal(err)
	}
	stored, err := f.server.BookRepo.Get(f.dune.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SeriesName != "Dune" {
		t.Errorf("series %q", stored.SeriesName)
	}
	if stored.EbookPath != f.dune.EbookPath {
		t.Errorf("the file moved to %s", stored.EbookPath)
	}
	if _, err := os.Stat(f.dune.EbookPath); err != nil {
		t.Errorf("the file is gone: %v", err)
	}

	// Sending part of it keeps the rest.
	got = postJSONMethod[editedBook](t, f.admin, http.MethodPut, url, map[string]any{"year": 1970}, http.StatusOK)
	if got.Title != "Dune Messiah" || got.Year != 1970 {
		t.Errorf("got %+v", got)
	}

	for name, body := range map[string]map[string]any{
		"no title":          {"title": "  "},
		"year":              {"year": -4},
		"position":          {"seriesPosition": "two"},
		"number, no series": {"seriesName": "", "seriesPosition": "2"},
		"long title":        {"title": strings.Repeat("x", 400)},
	} {
		t.Run(name, func(t *testing.T) {
			postJSONMethod[map[string]any](t, f.admin, http.MethodPut, url, body, http.StatusBadRequest)
		})
	}

	// A new author's name no longer goes with the old author's page.
	if err := f.server.BookRepo.UpdateDetails(f.hobbit.ID, books.Details{Title: "The Hobbit", Author: "J.R.R. Tolkien", AuthorKey: "OL1A"}, false); err != nil {
		t.Fatal(err)
	}
	hobbit := fmt.Sprintf("%s/api/books/%d", f.base, f.hobbit.ID)
	if got := postJSONMethod[editedBook](t, f.admin, http.MethodPut, hobbit, map[string]any{"author": "j.r.r. tolkien"}, http.StatusOK); got.AuthorKey != "OL1A" {
		t.Errorf("a change of capitals: key %q", got.AuthorKey)
	}
	if got := postJSONMethod[editedBook](t, f.admin, http.MethodPut, hobbit, map[string]any{"author": "Somebody"}, http.StatusOK); got.AuthorKey != "" {
		t.Errorf("another author: key %q", got.AuthorKey)
	}

	// An account without the manage permission can't.
	postJSON[map[string]any](t, f.admin, f.base+"/api/users", map[string]any{"username": "kid", "password": "kids-password", "role": "member", "permissions": map[string]any{"play": true, "manage": false}}, http.StatusCreated)
	kid := erLoginAs(t, f.base, "kid", "kids-password")
	postJSONMethod[map[string]any](t, kid, http.MethodPut, url, map[string]any{"title": "Mine"}, http.StatusForbidden)
}

func TestBookFileDetails(t *testing.T) {
	f := newEReaderFixture(t)
	got := getJSON[struct {
		Found   bool              `json:"found"`
		Details books.FileDetails `json:"details"`
	}](t, f.admin, fmt.Sprintf("%s/api/books/%d/file-details", f.base, f.hobbit.ID))
	if !got.Found || got.Details.Title != "The Hobbit" || got.Details.Language != "en" {
		t.Errorf("got %+v", got)
	}
	if err := os.WriteFile(f.dune.EbookPath, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := getJSON[map[string]any](t, f.admin, fmt.Sprintf("%s/api/books/%d/file-details", f.base, f.dune.ID))
	if bad["found"] != false {
		t.Errorf("got %v", bad)
	}
}

func TestMatchBookAgain(t *testing.T) {
	f := newEReaderFixture(t)
	f.server.TestSetOpenLibrary(newFakeOpenLibrary(t).URL)
	wrong := f.addEbook(t, "OL5W", "Hobbit (wrong)", "Nobody", 2000, false, func(b *books.Book) { b.SeriesName = "Wrong" })
	if err := f.server.BookRepo.SetOwnCover(wrong.ID, bytes.Repeat([]byte("x"), 10), time.Now()); err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("%s/api/books/%d/rematch", f.base, wrong.ID)

	// Another book already has that key.
	postJSON[map[string]any](t, f.admin, url, map[string]any{"olKey": "OL2W", "title": "Dune"}, http.StatusConflict)

	if err := f.server.BookRepo.Delete(f.hobbit.ID); err != nil {
		t.Fatal(err)
	}
	got := postJSON[editedBook](t, f.admin, url, map[string]any{"olKey": "/works/OL1W", "title": "The Hobbit", "author": "J.R.R. Tolkien", "authorKey": "OL26320A", "year": 1937, "coverId": 123}, http.StatusOK)
	if got.OLKey != "OL1W" || got.Title != "The Hobbit" || got.Author != "J.R.R. Tolkien" || got.Year != 1937 || got.Description != "A hobbit goes on a trip." || got.SeriesName != "" {
		t.Fatalf("got %+v", got)
	}
	stored, err := f.server.BookRepo.Get(wrong.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OwnCover != 0 || stored.CoverID != 123 {
		t.Errorf("cover: own %d, id %d", stored.OwnCover, stored.CoverID)
	}
	if stored.EbookPath != wrong.EbookPath || stored.EbookStatus != books.StatusDownloaded {
		t.Errorf("the file changed: %+v", stored)
	}
	postJSON[map[string]any](t, f.admin, url, map[string]any{"title": "No key"}, http.StatusBadRequest)
}

// sendFixture has a member who may read, signed in, with a mail server set up.
func sendFixture(t *testing.T) (*ereaderFixture, *http.Client, chan notify.FileMail) {
	t.Helper()
	f := newEReaderFixture(t)
	postJSON[map[string]any](t, f.admin, f.base+"/api/users", map[string]any{"username": "sam", "password": "sams-password", "role": "member"}, http.StatusCreated)
	sam := erLoginAs(t, f.base, "sam", "sams-password")
	if _, err := f.server.NotifyRepo.Create(notify.Target{Name: "Mail", Type: notify.TargetEmail, Enabled: true, Config: map[string]string{
		"host": "smtp.example.com", "port": "587", "security": "starttls", "from": "Usherr <usherr@example.com>", "to": "household@example.com",
	}}); err != nil {
		t.Fatal(err)
	}
	mail := make(chan notify.FileMail, 4)
	api.CaptureBookMail(t, mail, nil)
	return f, sam, mail
}

type sendAccess struct {
	Send struct {
		Devices []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Email  string `json:"email"`
			Kindle bool   `json:"kindle"`
		} `json:"devices"`
		Sender  string `json:"sender"`
		Problem string `json:"problem"`
	} `json:"send"`
	KOSync struct {
		Path  string `json:"path"`
		Ready bool   `json:"ready"`
	} `json:"kosync"`
}

func addDevice(t *testing.T, f *ereaderFixture, c *http.Client, name, email string) int64 {
	t.Helper()
	a := postJSON[sendAccess](t, c, f.base+"/api/auth/ereader/devices", map[string]string{"name": name, "email": email}, http.StatusOK)
	for _, d := range a.Send.Devices {
		if d.Name == name {
			return d.ID
		}
	}
	t.Fatalf("%s not listed: %+v", name, a)
	return 0
}

func activityHas(t *testing.T, f *ereaderFixture, text string) bool {
	t.Helper()
	list := getJSON[[]struct {
		Message string `json:"message"`
	}](t, f.admin, f.base+"/api/activity?limit=50")
	for _, e := range list {
		if strings.Contains(e.Message, text) {
			return true
		}
	}
	return false
}

func TestSendBookToKindle(t *testing.T) {
	f, sam, mail := sendFixture(t)
	if a := getJSON[sendAccess](t, sam, f.base+"/api/auth/ereader"); a.Send.Sender != "usherr@example.com" || a.Send.Problem != "" {
		t.Errorf("access: %+v", a)
	}
	id := addDevice(t, f, sam, "My Kindle", "me@kindle.com")

	// An EPUB that needs mending goes as a mended copy; the library keeps its file.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range [][2]string{
		{"mimetype", "application/epub+zip"},
		{"content.opf", `<package><metadata xmlns:dc="x"><dc:title>T</dc:title></metadata></package>`},
		{"a.xhtml", `<html><body><p>Hi</p></body></html>`},
	} {
		w, _ := zw.Create(e[0])
		io.WriteString(w, e[1])
	}
	zw.Close()
	if err := os.WriteFile(f.dune.EbookPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	got := postJSON[map[string]any](t, sam, fmt.Sprintf("%s/api/books/%d/send", f.base, f.dune.ID), map[string]int64{"deviceId": id}, http.StatusOK)
	if got["kindle"] != true || got["from"] != "usherr@example.com" {
		t.Errorf("answer: %v", got)
	}
	m := <-mail
	if m.To != "me@kindle.com" || m.FileName != "Dune.epub" || m.ContentType != "application/epub+zip" || m.Subject != "Dune" {
		t.Errorf("mail: %+v", m)
	}
	zr, err := zip.NewReader(bytes.NewReader(m.Data), int64(len(m.Data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, zf := range zr.File {
		if zf.Name == "a.xhtml" {
			rc, _ := zf.Open()
			page, _ := io.ReadAll(rc)
			rc.Close()
			if !strings.HasPrefix(string(page), "<?xml") {
				t.Errorf("the page wasn't mended: %s", page)
			}
		}
	}
	if onDisk, _ := os.ReadFile(f.dune.EbookPath); !bytes.Equal(onDisk, buf.Bytes()) {
		t.Error("the library's file changed")
	}
	if !activityHas(t, f, "Dune was sent to My Kindle by sam") {
		t.Error("nothing in Activity")
	}
}

func TestSendBookGoesAsItIsToAnotherEReader(t *testing.T) {
	f, sam, mail := sendFixture(t)
	id := addDevice(t, f, sam, "Reader", "me@example.org")
	if err := os.WriteFile(f.dune.EbookPath, []byte("%PDF-1.4 not zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, sam, fmt.Sprintf("%s/api/books/%d/send", f.base, f.dune.ID), map[string]int64{"deviceId": id}, http.StatusOK)
	if m := <-mail; string(m.Data) != "%PDF-1.4 not zip" {
		t.Errorf("changed: %q", m.Data)
	}
}

func TestSendBookRefusals(t *testing.T) {
	f, sam, mail := sendFixture(t)
	id := addDevice(t, f, sam, "My Kindle", "me@kindle.com")
	url := fmt.Sprintf("%s/api/books/%d/send", f.base, f.hobbit.ID)

	// Someone else's address, or none saved.
	postJSON[map[string]any](t, f.admin, url, map[string]int64{"deviceId": id}, http.StatusBadRequest)
	postJSON[map[string]any](t, sam, url, map[string]int64{"deviceId": id + 100}, http.StatusBadRequest)
	// Only a saved address can be removed, by its owner.
	postJSONMethod[map[string]any](t, f.admin, http.MethodDelete, fmt.Sprintf("%s/api/auth/ereader/devices/%d", f.base, id), nil, http.StatusNotFound)

	// Over the limit: clear words, nothing sent.
	if err := os.Truncate(f.hobbit.EbookPath, 51<<20); err != nil {
		t.Fatal(err)
	}
	answer := postJSON[map[string]any](t, sam, url, map[string]int64{"deviceId": id}, http.StatusUnprocessableEntity)
	if msg, _ := answer["error"].(string); !strings.Contains(msg, "51 MB") || !strings.Contains(msg, "50 MB") {
		t.Errorf("message: %v", answer)
	}
	if !activityHas(t, f, "could not be sent") {
		t.Error("nothing in Activity")
	}

	// A book locked with DRM.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("META-INF/encryption.xml")
	io.WriteString(w, `<encryption><EncryptedData><EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/></EncryptedData></encryption>`)
	zw.Close()
	if err := os.WriteFile(f.dune.EbookPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, sam, fmt.Sprintf("%s/api/books/%d/send", f.base, f.dune.ID), map[string]int64{"deviceId": id}, http.StatusUnprocessableEntity)

	// A file a Kindle can't take.
	cbz := f.addEbook(t, "OL7W", "Comic", "Artist", 2001, false, nil)
	renamed := strings.TrimSuffix(cbz.EbookPath, ".epub") + ".cbz"
	if err := os.Rename(cbz.EbookPath, renamed); err != nil {
		t.Fatal(err)
	}
	if err := f.server.BookRepo.SetState(cbz.ID, books.Ebook, books.StatusDownloaded, "cbz", renamed); err != nil {
		t.Fatal(err)
	}
	postJSON[map[string]any](t, sam, fmt.Sprintf("%s/api/books/%d/send", f.base, cbz.ID), map[string]int64{"deviceId": id}, http.StatusUnprocessableEntity)

	select {
	case m := <-mail:
		t.Errorf("sent anyway: %s", m.Subject)
	default:
	}

	// Bad addresses, doubles.
	for _, addr := range []string{"nope", "a@b.com, c@d.com", "Me <me@example.org>"} {
		postJSON[map[string]any](t, sam, f.base+"/api/auth/ereader/devices", map[string]string{"name": "X", "email": addr}, http.StatusBadRequest)
	}
	postJSON[map[string]any](t, sam, f.base+"/api/auth/ereader/devices", map[string]string{"name": "Again", "email": "ME@kindle.com"}, http.StatusBadRequest)
	postJSON[map[string]any](t, sam, f.base+"/api/auth/ereader/devices", map[string]string{"name": "", "email": "new@kindle.com"}, http.StatusBadRequest)
	if a := postJSONMethod[sendAccess](t, sam, http.MethodDelete, fmt.Sprintf("%s/api/auth/ereader/devices/%d", f.base, id), nil, http.StatusOK); len(a.Send.Devices) != 0 {
		t.Errorf("not removed: %+v", a)
	}
}

func TestSendBookWithoutAMailServer(t *testing.T) {
	f := newEReaderFixture(t)
	id := addDevice(t, f, f.admin, "My Kindle", "me@kindle.com")
	if a := getJSON[sendAccess](t, f.admin, f.base+"/api/auth/ereader"); !strings.Contains(a.Send.Problem, "email notification") {
		t.Errorf("access: %+v", a)
	}
	postJSON[map[string]any](t, f.admin, fmt.Sprintf("%s/api/books/%d/send", f.base, f.dune.ID), map[string]int64{"deviceId": id}, http.StatusConflict)
}

func TestSendBookMailServerRefuses(t *testing.T) {
	f := newEReaderFixture(t)
	if _, err := f.server.NotifyRepo.Create(notify.Target{Name: "Mail", Type: notify.TargetEmail, Enabled: true, Config: map[string]string{
		"host": "smtp.example.com", "port": "587", "security": "starttls", "from": "usherr@example.com", "to": "x@example.com",
	}}); err != nil {
		t.Fatal(err)
	}
	api.CaptureBookMail(t, nil, errors.New("the server refused the message (552)"))
	id := addDevice(t, f, f.admin, "Kindle", "me@kindle.com")
	answer := postJSON[map[string]any](t, f.admin, fmt.Sprintf("%s/api/books/%d/send", f.base, f.hobbit.ID), map[string]int64{"deviceId": id}, http.StatusBadGateway)
	if msg, _ := answer["error"].(string); !strings.Contains(msg, "552") {
		t.Errorf("answer: %v", answer)
	}
	if !activityHas(t, f, "The Hobbit could not be sent to Kindle") {
		t.Error("nothing in Activity")
	}
}

// koClient talks KOSync the way KOReader does.
type koClient struct {
	t         *testing.T
	base      string
	user, key string
}

func (k koClient) do(method, path string, body any) (int, map[string]any) {
	k.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, k.base+path, rd)
	if err != nil {
		k.t.Fatal(err)
	}
	req.Header.Set("Accept", "application/vnd.koreader.v1+json")
	if k.user != "" {
		req.Header.Set("x-auth-user", k.user)
		req.Header.Set("x-auth-key", k.key)
	}
	resp, err := erClient().Do(req)
	if err != nil {
		k.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		k.t.Fatalf("%s %s answered %d with %q", method, path, resp.StatusCode, data)
	}
	return resp.StatusCode, out
}

func koFixture(t *testing.T) (*ereaderFixture, koClient) {
	t.Helper()
	f := newEReaderFixture(t)
	a := postJSON[ereaderAccess](t, f.admin, f.base+"/api/auth/ereader/opds", nil, http.StatusOK)
	return f, koClient{t: t, base: f.base + "/kosync", user: a.Username, key: kosync.MD5Hex(a.Password)}
}

func digestOf(t *testing.T, path string) string {
	t.Helper()
	d, err := kosync.PartialMD5File(path)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestKOSyncProtocol(t *testing.T) {
	f, ko := koFixture(t)
	anon := koClient{t: t, base: ko.base}
	if status, a := anon.do(http.MethodGet, "/healthcheck", nil); status != http.StatusOK || a["state"] != "OK" {
		t.Errorf("healthcheck: %d %v", status, a)
	}
	// Accounts aren't made from an app: 402 with code 2005, which KOReader shows.
	status, a := anon.do(http.MethodPost, "/users/create", map[string]string{"username": "new", "password": "x"})
	if status != http.StatusPaymentRequired || a["code"] != float64(2005) {
		t.Errorf("register: %d %v", status, a)
	}
	if status, a := ko.do(http.MethodGet, "/users/auth", nil); status != http.StatusOK || a["authorized"] != "OK" {
		t.Errorf("sign in: %d %v", status, a)
	}
	// The bare paths answer too.
	if status, _ := (koClient{t: t, base: f.base, user: ko.user, key: ko.key}).do(http.MethodGet, "/users/auth", nil); status != http.StatusOK {
		t.Errorf("bare path: %d", status)
	}
	for name, c := range map[string]koClient{
		"wrong key":  {t: t, base: ko.base, user: ko.user, key: kosync.MD5Hex("wrong")},
		"no headers": {t: t, base: ko.base},
		"no user":    {t: t, base: ko.base, user: "nobody", key: ko.key},
	} {
		status, a := c.do(http.MethodGet, "/users/auth", nil)
		if status != http.StatusUnauthorized || a["code"] != float64(2001) {
			t.Errorf("%s: %d %v", name, status, a)
		}
		if status, _ := c.do(http.MethodGet, "/syncs/progress/abc", nil); status != http.StatusUnauthorized {
			t.Errorf("%s reading: %d", name, status)
		}
	}

	// A document nobody knows: 200 and an empty object.
	if status, a := ko.do(http.MethodGet, "/syncs/progress/abc123", nil); status != http.StatusOK || len(a) != 0 {
		t.Errorf("unknown document: %d %v", status, a)
	}

	// A book that isn't in the library still syncs between apps, word for word.
	const place = "/body/DocFragment[11]/body/div/p[7]/text().123"
	put := map[string]any{"document": "abc123", "progress": place, "percentage": 0.31879884821061, "device": "Kobo", "device_id": "D1"}
	status, a = ko.do(http.MethodPut, "/syncs/progress", put)
	if status != http.StatusOK || a["document"] != "abc123" || a["timestamp"] == nil {
		t.Fatalf("report: %d %v", status, a)
	}
	status, a = ko.do(http.MethodGet, "/syncs/progress/abc123", nil)
	if status != http.StatusOK || a["progress"] != place || a["percentage"] != 0.31879884821061 || a["device"] != "Kobo" || a["device_id"] != "D1" || a["document"] != "abc123" {
		t.Errorf("read back: %d %v", status, a)
	}
	if ts, _ := a["timestamp"].(float64); ts < 1e9 || ts > 1e11 {
		t.Errorf("timestamp isn't in seconds: %v", a["timestamp"])
	}
	// device_id may be left out.
	status, a = ko.do(http.MethodPut, "/syncs/progress", map[string]any{"document": "nodevice", "progress": "12", "percentage": 0, "device": "X"})
	if status != http.StatusOK {
		t.Errorf("0%% without a device id: %d %v", status, a)
	}
	if _, a := ko.do(http.MethodGet, "/syncs/progress/nodevice", nil); a["progress"] != "12" || a["percentage"] != float64(0) || a["device_id"] != nil {
		t.Errorf("got %v", a)
	}

	for name, tc := range map[string]struct {
		body map[string]any
		code float64
	}{
		"no document":   {map[string]any{"progress": "1", "percentage": 0.1, "device": "X"}, 2004},
		"colon":         {map[string]any{"document": "a:b", "progress": "1", "percentage": 0.1, "device": "X"}, 2004},
		"unsafe id":     {map[string]any{"document": "a/b", "progress": "1", "percentage": 0.1, "device": "X"}, 2007},
		"no percentage": {map[string]any{"document": "a", "progress": "1", "device": "X"}, 2003},
		"text percent":  {map[string]any{"document": "a", "progress": "1", "percentage": "half", "device": "X"}, 2003},
		"no progress":   {map[string]any{"document": "a", "percentage": 0.1, "device": "X"}, 2003},
		"no device":     {map[string]any{"document": "a", "progress": "1", "percentage": 0.1}, 2003},
		"progress long": {map[string]any{"document": "a", "progress": strings.Repeat("x", 3000), "percentage": 0.1, "device": "X"}, 2003},
	} {
		status, a := ko.do(http.MethodPut, "/syncs/progress", tc.body)
		if status != http.StatusForbidden || a["code"] != tc.code {
			t.Errorf("%s: %d %v", name, status, a)
		}
	}

	// A place is one account's own.
	postJSON[map[string]any](t, f.admin, f.base+"/api/users", map[string]any{"username": "sam", "password": "sams-password", "role": "member"}, http.StatusCreated)
	sam := erLoginAs(t, f.base, "sam", "sams-password")
	pw := postJSON[ereaderAccess](t, sam, f.base+"/api/auth/ereader/opds", nil, http.StatusOK).Password
	other := koClient{t: t, base: ko.base, user: "sam", key: kosync.MD5Hex(pw)}
	if _, a := other.do(http.MethodGet, "/syncs/progress/abc123", nil); len(a) != 0 {
		t.Errorf("sam sees: %v", a)
	}
	// Typed without its dashes, the password still counts.
	bare := koClient{t: t, base: ko.base, user: "sam", key: kosync.MD5Hex(strings.ReplaceAll(pw, "-", ""))}
	if status, _ := bare.do(http.MethodGet, "/users/auth", nil); status != http.StatusOK {
		t.Errorf("without dashes: %d", status)
	}
	// As HTTP Basic (Readest falls back to that) the password itself is the key.
	req, _ := http.NewRequest(http.MethodGet, ko.base+"/users/auth", nil)
	req.SetBasicAuth("sam", pw)
	resp, err := erClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("basic: %d", resp.StatusCode)
	}

	// Ebooks off: there is no sync server.
	postJSONMethod[map[string]any](t, f.admin, http.MethodPut, f.base+"/api/modules", map[string]any{"ebooks": false}, http.StatusOK)
	req, _ = http.NewRequest(http.MethodGet, ko.base+"/healthcheck", nil)
	resp, err = erClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("ebooks off: %d", resp.StatusCode)
	}
}

func TestKOSyncFollowsToUsherrsPlace(t *testing.T) {
	f, ko := koFixture(t)
	doc := digestOf(t, f.hobbit.EbookPath)
	status, _ := ko.do(http.MethodPut, "/syncs/progress", map[string]any{"document": doc, "progress": "/body/DocFragment[2]/body/p[3]/text().5", "percentage": 0.42, "device": "Kobo", "device_id": "D1"})
	if status != http.StatusOK {
		t.Fatal(status)
	}
	var uid int64
	if err := f.server.TestDB().QueryRow(`SELECT id FROM users WHERE username = ?`, ko.user).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	p, had, err := f.server.BookRepo.GetProgress(uid, f.hobbit.ID, books.Ebook)
	if err != nil || !had {
		t.Fatalf("no place in Usherr: %v", err)
	}
	if p.Percent != 42 || p.Position != "" || p.Finished {
		t.Errorf("Usherr's place: %+v", p)
	}
	// The app's exact place comes back while Usherr hasn't moved.
	_, a := ko.do(http.MethodGet, "/syncs/progress/"+doc, nil)
	if a["device"] != "Kobo" || a["progress"] != "/body/DocFragment[2]/body/p[3]/text().5" {
		t.Errorf("own place: %v", a)
	}
	// A place at the very end finishes the book; 0% from a quick look doesn't lose it.
	ko.do(http.MethodPut, "/syncs/progress", map[string]any{"document": doc, "progress": "x", "percentage": 0.998, "device": "Kobo"})
	if p, _, _ := f.server.BookRepo.GetProgress(uid, f.hobbit.ID, books.Ebook); !p.Finished || p.Percent != 99.8 {
		t.Errorf("at the end: %+v", p)
	}
	ko.do(http.MethodPut, "/syncs/progress", map[string]any{"document": doc, "progress": "y", "percentage": 0, "device": "Kobo"})
	if p, _, _ := f.server.BookRepo.GetProgress(uid, f.hobbit.ID, books.Ebook); p.Percent != 99.8 {
		t.Errorf("after 0%%: %+v", p)
	}

	// The reader moved on: the app is told the chapter, by Usherr's name.
	if err := f.server.BookRepo.SetProgress(uid, books.Progress{BookID: f.hobbit.ID, Format: books.Ebook, Position: "epubcfi(/6/4!/4/2/1:0)", Percent: 70}); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute).UTC().Format("2006-01-02T15:04:05Z")
	if _, err := f.server.TestDB().Exec(`UPDATE book_progress SET updated_at = ? WHERE user_id = ?`, future, uid); err != nil {
		t.Fatal(err)
	}
	_, a = ko.do(http.MethodGet, "/syncs/progress/"+doc, nil)
	if a["device"] != "Usherr" || a["progress"] != "/body/DocFragment[2]/body" || a["percentage"] != 0.7 {
		t.Errorf("Usherr's place: %v", a)
	}
	// With only a percentage (from a Kobo), the chapter it falls in.
	if err := f.server.BookRepo.SetProgress(uid, books.Progress{BookID: f.hobbit.ID, Format: books.Ebook, Percent: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.server.TestDB().Exec(`UPDATE book_progress SET updated_at = ? WHERE user_id = ?`, future, uid); err != nil {
		t.Fatal(err)
	}
	_, a = ko.do(http.MethodGet, "/syncs/progress/"+doc, nil)
	if a["progress"] != "/body/DocFragment[1]/body" || a["percentage"] != 0.01 {
		t.Errorf("by percentage: %v", a)
	}
}

func TestKOSyncKnowsTheBookByItsFileName(t *testing.T) {
	f, ko := koFixture(t)
	doc := kosync.MD5Hex(filepath.Base(f.dune.EbookPath)) // KOReader's other setting
	ko.do(http.MethodPut, "/syncs/progress", map[string]any{"document": doc, "progress": "/x", "percentage": 0.5, "device": "Kobo"})
	var uid int64
	if err := f.server.TestDB().QueryRow(`SELECT id FROM users WHERE username = ?`, ko.user).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if p, had, _ := f.server.BookRepo.GetProgress(uid, f.dune.ID, books.Ebook); !had || p.Percent != 50 {
		t.Errorf("got %+v", p)
	}
}

func TestKOSyncPagedBooksSyncByPage(t *testing.T) {
	f, ko := koFixture(t)
	pdf := f.addEbook(t, "OL8W", "Manual", "Writer", 2010, false, nil)
	file := strings.TrimSuffix(pdf.EbookPath, ".epub") + ".pdf"
	if err := os.Rename(pdf.EbookPath, file); err != nil {
		t.Fatal(err)
	}
	if err := f.server.BookRepo.SetState(pdf.ID, books.Ebook, books.StatusDownloaded, "pdf", file); err != nil {
		t.Fatal(err)
	}
	doc := digestOf(t, file)
	ko.do(http.MethodPut, "/syncs/progress", map[string]any{"document": doc, "progress": "56", "percentage": 0.25, "device": "Kobo"})
	var uid int64
	if err := f.server.TestDB().QueryRow(`SELECT id FROM users WHERE username = ?`, ko.user).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if p, _, _ := f.server.BookRepo.GetProgress(uid, pdf.ID, books.Ebook); p.Position != "page:56" || p.Percent != 25 {
		t.Errorf("Usherr's place: %+v", p)
	}
	if err := f.server.BookRepo.SetProgress(uid, books.Progress{BookID: pdf.ID, Format: books.Ebook, Position: "page:90", Percent: 40}); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute).UTC().Format("2006-01-02T15:04:05Z")
	if _, err := f.server.TestDB().Exec(`UPDATE book_progress SET updated_at = ? WHERE user_id = ?`, future, uid); err != nil {
		t.Fatal(err)
	}
	if _, a := ko.do(http.MethodGet, "/syncs/progress/"+doc, nil); a["progress"] != "90" {
		t.Errorf("the app's place: %v", a)
	}
}

func TestKOSyncAccountRefused(t *testing.T) {
	f, _ := koFixture(t)
	postJSON[map[string]any](t, f.admin, f.base+"/api/users", map[string]any{
		"username": "kid", "password": "kids-password", "role": "member", "permissions": map[string]any{"play": false},
	}, http.StatusCreated)
	kid := erLoginAs(t, f.base, "kid", "kids-password")
	pw := postJSON[ereaderAccess](t, kid, f.base+"/api/auth/ereader/opds", nil, http.StatusOK).Password
	if status, _ := (koClient{t: t, base: f.base + "/kosync", user: "kid", key: kosync.MD5Hex(pw)}).do(http.MethodGet, "/users/auth", nil); status != http.StatusUnauthorized {
		t.Errorf("no play permission: %d", status)
	}
}

func TestKoboGetsACorrectedTitle(t *testing.T) {
	f, device, _ := newKoboFixture(t)
	device.sync()
	entries, _ := device.sync()
	if len(entries.kinds()) != 0 {
		t.Fatalf("nothing changed, yet: %v", entries.kinds())
	}
	postJSONMethod[map[string]any](t, f.admin, http.MethodPut, fmt.Sprintf("%s/api/books/%d", f.base, f.dune.ID), map[string]any{"title": "Dune Messiah"}, http.StatusOK)
	entries, _ = device.sync()
	if entries.titled("ChangedEntitlement", "Dune Messiah") == nil {
		t.Errorf("the Kobo wasn't told: %v", entries.kinds())
	}
}
