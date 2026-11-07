package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/books"
	"github.com/rdborg/mediarium/internal/ebookconv"
	"github.com/rdborg/mediarium/internal/ereader"
	"github.com/rdborg/mediarium/internal/notify"
)

// Send to Kindle or e-reader: a book goes by email, through the mail server of
// the email notification (the same one as password resets), to an address the
// account saved on its profile page and nowhere else.
//
// For a Kindle address: Amazon takes EPUB and PDF, and no Kindle formats any
// more, so a MOBI or AZW3 goes as the converter's EPUB, and an EPUB goes
// through FixForKindle. Any other address gets the file as it is. The library
// is never changed: the copies live in memory for the length of the send.

const (
	// maxSendBytes is the most a book may be: Amazon's limit for an email.
	maxSendBytes = 50 << 20
	sendTimeout  = 5 * time.Minute
	// sendsPerHour limits an account, so an account can't flood a mailbox.
	sendsPerHour = 20
)

// sendOneAtATime keeps one book at a time in memory.
var sendOneAtATime sync.Mutex

// sendBookMail sends the email (tests replace it to read what's sent).
var sendBookMail = func(ctx context.Context, m *notify.EmailSender, mail notify.FileMail) error {
	return m.SendFile(ctx, mail)
}

// sendMailProblem says why books can't be emailed now ("" when they can) and
// the address they come from.
func (s *Server) sendMailProblem() (mailer *notify.EmailSender, sender, problem string) {
	const setUp = "An administrator has to set up an email notification first (Settings, Notifications): its mail server sends the books."
	if s.NotifyRepo == nil {
		return nil, "", setUp
	}
	m, _, ok := s.NotifyRepo.AccountMailer()
	if !ok {
		return nil, "", setUp
	}
	return m, fromAddress(m.From), ""
}

// fromAddress is the bare address of "Name <you@example.com>".
func fromAddress(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimRight(strings.TrimSpace(from[i+1:]), ">")
	}
	return strings.TrimSpace(from)
}

var bookFileTypes = map[string]string{
	"epub": "application/epub+zip",
	"pdf":  "application/pdf",
	"mobi": "application/x-mobipocket-ebook",
	"azw":  "application/vnd.amazon.ebook",
	"azw3": "application/vnd.amazon.ebook",
	"prc":  "application/x-mobipocket-ebook",
}

// asciiFileName keeps a file name that survives every mail system, since a
// Kindle takes the book's title from inside the file anyway.
func asciiFileName(title, ext string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)), r == ' ', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	name := strings.Join(strings.Fields(b.String()), " ")
	if name == "" {
		name = "book"
	}
	if len(name) > 80 {
		name = strings.TrimSpace(name[:80])
	}
	return name + "." + ext
}

func megabytes(n int64) int64 { return (n + (1<<20 - 1)) >> 20 }

var errSendTooBig = errors.New("too big to email")

// bookForDevice makes the file a device gets. An error is for the person.
func (s *Server) bookForDevice(b books.Book, d ereader.Device) (data []byte, ext string, err error) {
	ext = ebookExt(b)
	if d.Kindle && ext != "epub" && ext != "pdf" && !kindleFormat(ext) {
		return nil, "", fmt.Errorf("a Kindle can't take a %s file by email. It takes EPUB and PDF", strings.ToUpper(ext))
	}
	f, info, _, err := s.openEbook(b)
	if err != nil {
		slog.Info("books: open book to send", "book", b.ID, "err", err)
		return nil, "", errors.New("couldn't find the book's file. It may have been moved or deleted")
	}
	defer f.Close()
	path := ""
	if d.Kindle && kindleFormat(ext) {
		// A Kindle no longer takes its own older formats by email.
		if path, err = s.epubOf(b); err != nil {
			slog.Info("books: convert book to send", "book", b.ID, "err", err)
			return nil, "", errors.New(convertError(err))
		}
		ext = "epub"
		if info, err = os.Stat(path); err != nil {
			return nil, "", errors.New("couldn't read the converted book")
		}
	}
	if info.Size() > maxSendBytes {
		return nil, "", fmt.Errorf("%w: %d", errSendTooBig, megabytes(info.Size()))
	}
	if path != "" {
		data, err = os.ReadFile(path)
	} else {
		data, err = io.ReadAll(f)
	}
	if err != nil {
		slog.Info("books: read book to send", "book", b.ID, "err", err)
		return nil, "", errors.New("couldn't read the book's file")
	}
	if d.Kindle && ext == "epub" {
		fixed, what, err := ebookconv.FixForKindle(data)
		switch {
		case errors.Is(err, ebookconv.ErrDRM):
			return nil, "", errors.New("this book is locked with DRM, so a Kindle would refuse it")
		case err != nil:
			// Nothing to lose by trying: Amazon answers by email if it refuses.
			slog.Info("books: mend EPUB for Kindle", "book", b.ID, "err", err)
		case int64(len(fixed)) > maxSendBytes:
			return nil, "", fmt.Errorf("%w: %d", errSendTooBig, megabytes(int64(len(fixed))))
		default:
			data = fixed
			if len(what) > 0 {
				slog.Info("books: mended EPUB for Kindle", "book", b.ID, "what", what)
			}
		}
	}
	return data, ext, nil
}

type sendBookRequest struct {
	DeviceID int64 `json:"deviceId"`
}

// POST /api/books/{id}/send {"deviceId": 3}: emails the ebook to one of the
// account's own addresses and answers when the mail server has taken it.
func (s *Server) handleSendBook(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "You need to sign in first.")
		return
	}
	b, ok := s.bookFromPath(w, r)
	if !ok {
		return
	}
	if !s.ebooksEnabled() {
		writeError(w, http.StatusConflict, "Ebooks are switched off. Switch them on in Settings > Media types.")
		return
	}
	var req sendBookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Choose where to send it.")
		return
	}
	device, err := s.EReader.Device(u.ID, req.DeviceID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "Choose one of the addresses saved on your profile.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !hasEbook(b) {
		writeError(w, http.StatusConflict, "This book has no ebook file yet.")
		return
	}
	mailer, _, problem := s.sendMailProblem()
	if problem != "" {
		writeError(w, http.StatusConflict, problem)
		return
	}
	limiter := s.ereaderState().sends
	key := strconv.FormatInt(u.ID, 10)
	if !limiter.Reserve(key) {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("You can send up to %d books an hour. Try again later.", sendsPerHour))
		return
	}

	// The send goes on if the page is closed, and it reports to Activity.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), sendTimeout)
	defer cancel()
	sendOneAtATime.Lock()
	defer sendOneAtATime.Unlock()
	data, ext, err := s.bookForDevice(b, device)
	if err != nil {
		limiter.Refund(key)
		msg := upperFirst(err.Error()) + "."
		if errors.Is(err, errSendTooBig) {
			msg = fmt.Sprintf("This book is %s MB. An email can carry at most %d MB, so it can't be sent this way. Download it, or connect the e-reader with a cable.", strings.TrimPrefix(err.Error(), errSendTooBig.Error()+": "), maxSendBytes>>20)
		}
		s.logBookSend(b, device, u.Username, msg)
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	ct := bookFileTypes[ext]
	if ct == "" {
		ct = "application/octet-stream"
	}
	mail := notify.FileMail{
		To: device.Email, Subject: b.Title, FileName: asciiFileName(b.Title, ext), ContentType: ct, Data: data,
		Text: fmt.Sprintf("%s, by %s. Sent from Usherr by %s.", b.Title, b.Author, u.Username),
	}
	if err := sendBookMail(ctx, mailer, mail); err != nil {
		msg := "The mail server didn't take it: " + err.Error()
		slog.Info("books: send to device", "book", b.ID, "err", err)
		s.logBookSend(b, device, u.Username, msg)
		writeError(w, http.StatusBadGateway, msg)
		return
	}
	s.logBookSend(b, device, u.Username, "")
	writeJSON(w, http.StatusOK, map[string]any{"sent": true, "device": device.Name, "kindle": device.Kindle, "from": fromAddress(mailer.From)})
}

// logBookSend writes the result to Activity. The device's name goes there,
// never its address.
func (s *Server) logBookSend(b books.Book, d ereader.Device, who, failure string) {
	if failure == "" {
		_ = s.QueueRepo.LogActivity(0, "book_sent", fmt.Sprintf("%s was sent to %s by %s", b.Title, d.Name, who))
		return
	}
	_ = s.QueueRepo.LogActivity(0, "book_send_failed", fmt.Sprintf("%s could not be sent to %s by %s: %s", b.Title, d.Name, who, failure))
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type deviceRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// POST /api/auth/ereader/devices: saves an address to send books to.
func (s *Server) handleAddEReaderDevice(w http.ResponseWriter, r *http.Request) {
	// An address is where books can be sent, so a key can't add one.
	if !requireSession(w, r) {
		return
	}
	u, ok := s.ereaderUser(w, r)
	if !ok {
		return
	}
	var req deviceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Enter a name and an email address.")
		return
	}
	err := s.EReader.AddDevice(u.ID, req.Name, req.Email)
	switch {
	case errors.Is(err, ereader.ErrBadDevice), errors.Is(err, ereader.ErrTooManyDevices), errors.Is(err, ereader.ErrDuplicateDevice):
		writeError(w, http.StatusBadRequest, upperFirst(err.Error())+".")
		return
	case err != nil:
		slog.Error("ereader: add device", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save the address. Try again.")
		return
	}
	s.respondEReader(w, u, http.StatusOK, nil)
}

// DELETE /api/auth/ereader/devices/{id}
func (s *Server) handleRemoveEReaderDevice(w http.ResponseWriter, r *http.Request) {
	u, ok := s.ereaderUser(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a saved address.")
		return
	}
	if err := s.EReader.RemoveDevice(u.ID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "That isn't a saved address.")
			return
		}
		slog.Error("ereader: remove device", "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't remove the address. Try again.")
		return
	}
	s.respondEReader(w, u, http.StatusOK, nil)
}
