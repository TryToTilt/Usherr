package notify_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/notify"
)

func TestSendFileAttachesTheBook(t *testing.T) {
	f := startFakeSMTP(t, &fakeSMTP{starttls: true, user: "u", pass: "p"})
	book := bytes.Repeat([]byte("epub bytes \x00\xff"), 500)
	err := f.sender("starttls").SendFile(context.Background(), notify.FileMail{
		To: "me@kindle.com", Subject: "Le Petit\nInjected: x", Text: "Here it is.", FileName: "Le Petit.epub", ContentType: "application/epub+zip", Data: book,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.rcpts, ",") != "me@kindle.com" {
		t.Fatalf("recipients: %v", f.rcpts)
	}
	msg, err := mail.ReadMessage(strings.NewReader(f.data))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Injected") != "" {
		t.Fatal("header injection through the subject")
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	text, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(text); !strings.Contains(string(b), "Here it is.") {
		t.Errorf("text: %q", b)
	}
	file, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if file.FileName() != "Le Petit.epub" {
		t.Errorf("file name %q", file.FileName())
	}
	raw, _ := io.ReadAll(file)
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("a line of %d characters", len(line))
		}
	}
	got, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, strings.NewReader(strings.ReplaceAll(string(raw), "\r\n", ""))))
	if err != nil || !bytes.Equal(got, book) {
		t.Errorf("the book came through changed (%v)", err)
	}
}

func TestSendFileBadAddress(t *testing.T) {
	f := startFakeSMTP(t, &fakeSMTP{})
	if err := f.sender("none").SendFile(context.Background(), notify.FileMail{To: "not an address", FileName: "a.epub", ContentType: "application/epub+zip"}); err == nil {
		t.Fatal("want an error")
	}
}
