package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// FileMail is an email with one file attached (a book for an e-reader).
type FileMail struct {
	To          string
	Subject     string
	Text        string
	FileName    string
	ContentType string
	Data        []byte
}

// SendFile sends the file through this mail server. A file takes longer than a
// notice, so the caller's context says how long it may take, not s.Timeout.
func (s *EmailSender) SendFile(ctx context.Context, m FileMail) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("from address: %w", err)
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return errors.New("that address is not valid")
	}
	one := *s
	one.To = []string{to.Address}
	one.Timeout = 10 * time.Minute
	return one.deliver(ctx, from.Address, []string{to.Address}, func() []byte {
		return buildFileMessage(fromHeader(s.From, s.FromName), to.Address, m)
	})
}

// buildFileMessage renders the message: the text, then the file in base64.
func buildFileMessage(from, to string, m FileMail) []byte {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", oneLine(from))
	h("To", oneLine(to))
	h("Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Subject)))
	h("Date", time.Now().Format(time.RFC1123Z))
	h("MIME-Version", "1.0")
	h("Content-Type", `multipart/mixed; boundary="`+mw.Boundary()+`"`)
	b.WriteString("\r\n")

	text, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {`text/plain; charset="utf-8"`},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	qp := quotedprintable.NewWriter(text)
	_, _ = qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(m.Text, "\r\n", "\n"), "\n", "\r\n") + "\r\n"))
	_ = qp.Close()

	file, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {mime.FormatMediaType(m.ContentType, map[string]string{"name": m.FileName})},
		"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": m.FileName})},
		"Content-Transfer-Encoding": {"base64"},
	})
	// Mail lines stop at 76 characters.
	encoded := base64.StdEncoding.EncodeToString(m.Data)
	for len(encoded) > 76 {
		_, _ = file.Write([]byte(encoded[:76] + "\r\n"))
		encoded = encoded[76:]
	}
	_, _ = file.Write([]byte(encoded + "\r\n"))
	_ = mw.Close()
	return b.Bytes()
}
