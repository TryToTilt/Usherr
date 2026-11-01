package api

// Usherr: Forgot password? on the sign-in page. Someone types their user name
// or email address, and a one-time link to set a new password goes to the
// address on the account (auth/reset.go keeps only its hash). It's off until
// an administrator switches it on, and works only while an email
// notification is switched on (its mail server sends the link) and the
// address people open Usherr at is set: the link is made from that setting,
// never from the request, so nobody can have a reset email point elsewhere.
//
// The reply is the same, and takes about as long, whether the account exists
// or not; the email goes out in the background. Requests are limited per
// address and per account, and every request and reset goes on Activity.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/httpsec"
	"github.com/rdborg/mediarium/internal/notify"
	"github.com/rdborg/mediarium/internal/settings"
)

// resetState holds the reset limits, made on first use so a zero Server works.
type resetState struct {
	once sync.Once
	// asks limits Forgot password? requests per address; emails, the links
	// sent to one account; tries, checking and using links per address.
	asks, emails, tries *auth.LoginLimiter
}

func (s *Server) resets() *resetState {
	st := &s.resetS
	st.once.Do(func() {
		st.asks = auth.NewLoginLimiter(5, 15*time.Minute)
		st.emails = auth.NewLoginLimiter(3, time.Hour)
		st.tries = auth.NewLoginLimiter(30, 15*time.Minute)
	})
	return st
}

// resetReplyFloor is the least a Forgot password? reply takes, so an account
// that exists (a link is saved) can't be told from one that doesn't.
const resetReplyFloor = 600 * time.Millisecond

// forgotReply is the one answer every Forgot password? request gets.
const forgotReply = "If that account has an email address, a link to set a new password is on its way. It works once, for 30 minutes."

const resetLinkBadReply = "This link has expired or was already used. Ask for a new one under Forgot password? on the sign-in page."

// emailResetProblem says why reset emails can't be sent now ("" when they can):
// the mail server they go through and the address their links start with.
func (s *Server) emailResetProblem() (mailer *notify.EmailSender, base, problem string) {
	if !s.localSignInOn() {
		return nil, "", "Signing in with a password is turned off."
	}
	base = s.configuredPublicURL()
	if !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://") {
		return nil, "", "Set the address you open Usherr at first (Settings, General): the link in the email starts with it."
	}
	if s.NotifyRepo == nil {
		return nil, "", "Set up an email notification first (Settings, Notifications): its mail server sends the link."
	}
	m, _, ok := s.NotifyRepo.AccountMailer()
	if !ok {
		return nil, "", "Set up an email notification and switch it on first (Settings, Notifications): its mail server sends the link."
	}
	return m, base, ""
}

// emailResetOn says whether Forgot password? is offered: switched on, and
// able to send.
func (s *Server) emailResetOn() bool {
	if on, _ := s.Settings.GetBool(settings.KeyAuthEmailReset); !on {
		return false
	}
	_, _, problem := s.emailResetProblem()
	return problem == ""
}

// POST /api/auth/forgot {"who": user name or email address}: always 202.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if !s.emailResetOn() {
		http.NotFound(w, r)
		return
	}
	started := time.Now()
	ip := s.clientIP(r)
	st := s.resets()
	if !st.asks.Reserve(httpsec.LimitKey(ip)) {
		writeError(w, http.StatusTooManyRequests, "Too many requests. Wait a few minutes and try again.")
		return
	}
	var req struct {
		Who string `json:"who"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicBody)
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if strings.TrimSpace(req.Who) == "" {
		writeError(w, http.StatusBadRequest, "Enter your user name or email address.")
		return
	}
	u, err := s.Auth.UserForReset(req.Who)
	switch {
	case err != nil:
		// No such account, or one a link can't help (no email, turned off,
		// signs in elsewhere): the same reply, nothing sent.
		slog.Info("auth: password reset asked for, nothing sent", "who", strings.ToLower(oneLineLimit(req.Who, 64)), "ip", ip)
	case !st.emails.Reserve(fmt.Sprint(u.ID)):
		slog.Warn("auth: password reset asked for too often, nothing sent", "user", u.Username, "ip", ip)
	default:
		token, err := s.Auth.CreateResetLink(u.ID)
		if err != nil {
			slog.Error("auth: could not make a password reset link", "user", u.Username, "err", err)
			break
		}
		s.auditAccount(r, "Password reset by email requested for "+u.Username)
		mailer, base, _ := s.emailResetProblem()
		go s.sendResetEmail(mailer, base, u, token, ip)
	}
	waitFloor(r.Context(), started, resetReplyFloor)
	writeJSON(w, http.StatusAccepted, map[string]string{"message": forgotReply})
}

// sendResetEmail sends the link. The secret is only ever in the email.
func (s *Server) sendResetEmail(mailer *notify.EmailSender, base string, u *auth.User, token, ip string) {
	if mailer == nil {
		return
	}
	link := base + "/reset/" + token
	ev := notify.Event{
		Type:  "account",
		Title: "Set a new password",
		Message: fmt.Sprintf("Someone asked to set a new password for your Usherr account %s, %s.\n\n"+
			"To set a new password, open this link. It works once, for 30 minutes:\n%s\n\n"+
			"If this wasn't you, ignore this email. Your password stays as it is.", u.Username, askedFrom(ip), link),
		Timestamp: time.Now(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := sendAccountMail(ctx, mailer, u.Email, ev); err != nil {
		slog.Error("auth: the password reset email could not be sent", "user", u.Username, "err", err)
		return
	}
	slog.Info("auth: password reset email sent", "user", u.Username)
}

// askedFrom says where a reset request came from, for the email. An address
// inside the home network says little (Docker Desktop shows every device at
// home as its own address, 172.x.x.x), so it says that instead.
func askedFrom(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return "from an unknown address"
	}
	if a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return "from a device on your home network"
	}
	return "from the address " + ip
}

// sendAccountMail sends an account email (tests replace it to read what's sent).
var sendAccountMail = func(ctx context.Context, m *notify.EmailSender, to string, ev notify.Event) error {
	return m.SendTo(ctx, to, ev)
}

// GET /api/auth/reset/{token}: whether a link still works, before anything is typed.
func (s *Server) handleResetCheck(w http.ResponseWriter, r *http.Request) {
	if !s.emailResetOn() {
		http.NotFound(w, r)
		return
	}
	if !s.resets().tries.Reserve(httpsec.LimitKey(s.clientIP(r))) {
		writeError(w, http.StatusTooManyRequests, "Too many tries. Wait a few minutes and try again.")
		return
	}
	u, err := s.Auth.ResetLinkUser(r.PathValue("token"))
	if err != nil {
		writeError(w, http.StatusGone, resetLinkBadReply)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": u.Username})
}

// POST /api/auth/reset {"token","password"}: sets the new password. Every
// session of the account ends; two-step sign-in stays on.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if !s.emailResetOn() {
		http.NotFound(w, r)
		return
	}
	if !s.resets().tries.Reserve(httpsec.LimitKey(s.clientIP(r))) {
		writeError(w, http.StatusTooManyRequests, "Too many tries. Wait a few minutes and try again.")
		return
	}
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicBody)
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	if msg := checkPassword(req.Password, "New password"); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	u, err := s.Auth.ResetWithLink(req.Token, req.Password)
	switch {
	case errors.Is(err, auth.ErrResetLinkBad):
		writeError(w, http.StatusGone, resetLinkBadReply)
		return
	case err != nil:
		slog.Error("auth: could not set a password through a reset link", "err", err)
		writeError(w, http.StatusInternalServerError, "The password couldn't be set. Try again.")
		return
	}
	s.auditAccount(r, "Password reset by email for "+u.Username)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Your password is set. Sign in with it."})
}

// waitFloor waits until at least floor has passed since started.
func waitFloor(ctx context.Context, started time.Time, floor time.Duration) {
	wait := floor - time.Since(started)
	if wait <= 0 {
		return
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// oneLineLimit is typed text made safe for a log line.
func oneLineLimit(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n]
	}
	return s
}
