package api

// Usherr: Community, step 2. People link their Discord account to their
// Usherr account themselves: /link in Discord answers with a code only they
// see, and they type it under Settings, Linked services. No sign-in page has
// to reach Usherr from outside (Discord's own sign-in would need an address
// Discord can send people back to), and the code proves the Discord account
// is theirs.

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/brand"
	"github.com/rdborg/mediarium/internal/community"
)

// linkCodeLife is how long a /link code works.
const linkCodeLife = 10 * time.Minute

// communityCommands are the slash commands the bot offers.
func communityCommands() []community.Command {
	return []community.Command{
		{Name: "link", Description: "Link your Discord account to your " + brand.Name + " account"},
		{Name: "stubs", Description: "Your ticket stubs: the watch parties you went to"}, // home_community_stubs.go
		{Name: "review", Description: "Review a film or show: stars and a few words", Options: []community.Option{ // home_community_review.go
			{Name: "film", Description: "Start typing its name, then pick it (not needed in a film's post or a watch party)", Kind: "string", Autocomplete: true},
			{Name: "stars", Description: "How many stars", Kind: "integer", Choices: reviewStarChoices()},
			{Name: "review", Description: "A few words (||like this|| hides a spoiler)", Kind: "string", MaxLength: 1500},
		}},
	}
}

// linkCodes are the /link codes waiting to be typed into Usherr.
type linkCodes struct {
	once  sync.Once
	mu    sync.Mutex
	codes map[string]pendingLink // code → who asked
	tries *auth.LoginLimiter     // wrong codes typed, per Usherr account
}

type pendingLink struct {
	user    community.User
	expires time.Time
}

func (l *linkCodes) init() {
	l.once.Do(func() {
		l.codes = map[string]pendingLink{}
		l.tries = auth.NewLoginLimiter(10, 15*time.Minute)
	})
}

// issue makes a code for a Discord account; an older code of theirs stops
// working.
func (l *linkCodes) issue(u community.User) (string, error) {
	l.init()
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for c, p := range l.codes {
		if p.user.ID == u.ID || now.After(p.expires) {
			delete(l.codes, c)
		}
	}
	for range 20 {
		n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			return "", err
		}
		code := fmt.Sprintf("%06d", n.Int64())
		if _, taken := l.codes[code]; !taken {
			l.codes[code] = pendingLink{user: u, expires: now.Add(linkCodeLife)}
			return code, nil
		}
	}
	return "", errors.New("no free code")
}

// take uses a code up: who it belongs to, if it's still good.
func (l *linkCodes) take(code string) (community.User, bool) {
	l.init()
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.codes[code]
	if !ok {
		return community.User{}, false
	}
	delete(l.codes, code)
	return p.user, time.Now().Before(p.expires)
}

// communityInteraction answers a command or button press from Discord.
func (s *Server) communityInteraction(in community.Interaction) {
	b := s.community()
	if b == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Usherr: step 6, /review. Its film is suggested as it's typed, and the
	// answer (looking the film up, posting the card) can take longer than
	// Discord's three seconds, so it says "thinking…" first.
	if in.Kind == "suggest" {
		if in.Name == "review" && in.Focused == "film" {
			_ = b.Suggest(ctx, in, s.reviewFilmPicks(ctx, in.Options["film"]))
		}
		return
	}
	if in.Kind == "command" && in.Name == "review" && in.Options["stars"] == "" && strings.TrimSpace(in.Options["review"]) == "" {
		// Nothing but the film (Discord hides the other boxes until they're
		// picked): ask in a form, which the film rides along with.
		if err := b.AskForm(ctx, in, reviewForm(in.Options["film"])); err != nil {
			slog.Warn("community: couldn't open the /review form", "err", err)
		}
		return
	}
	if in.Kind == "form" && strings.HasPrefix(in.Name, "review:form:") {
		in.Options = reviewFromForm(in)
	}
	if (in.Kind == "command" && in.Name == "review") || (in.Kind == "form" && strings.HasPrefix(in.Name, "review:form:")) {
		if err := b.Defer(ctx, in, true); err != nil {
			slog.Warn("community: couldn't answer /review", "err", err)
			return
		}
		later, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := b.Followup(later, in, s.reviewCommand(later, in)); err != nil {
			slog.Warn("community: couldn't finish answering /review", "err", err)
		}
		return
	}
	var text string
	switch {
	case in.Kind == "command" && in.Name == "link":
		text = s.linkReply(in.User)
	case in.Kind == "button" && strings.HasPrefix(in.Name, "stub:stamp:"):
		text = s.stampButton(ctx, in) // home_community_stubs.go
	case in.Kind == "command" && in.Name == "stubs":
		text = s.stubsReply(ctx, in.User) // home_community_stubs.go
	case in.Kind == "button" && strings.HasPrefix(in.Name, "party:"):
		text = s.partyButton(in) // home_community_party.go
	case in.Kind == "button" && strings.HasPrefix(in.Name, "review:"):
		text = s.reviewButton(ctx, in) // home_community_review.go
	default:
		text = "That isn't something " + brand.Name + " knows yet."
	}
	if err := b.Reply(ctx, in, text, true); err != nil {
		slog.Warn("community: couldn't answer in Discord", "what", in.Name, "err", err)
	}
}

func (s *Server) linkReply(u community.User) string {
	code, err := s.linkC.issue(u)
	if err != nil {
		return "Something went wrong making your code. Try /link again."
	}
	shown := code[:3] + " " + code[3:]
	where := "In " + brand.Name + ", open Settings, then Linked services, and type it under Discord."
	if name := s.linkedUsername(u.ID); name != "" {
		return fmt.Sprintf("Your Discord account is linked to **%s** in %s. To link it to another account instead, sign in as that account and type this code: **%s**. %s It works for 10 minutes, and only you can see this.", name, brand.Name, shown, where)
	}
	return fmt.Sprintf("Your code is **%s**. %s It works for 10 minutes, and only you can see this.", shown, where)
}

// linkedUsername is the Usherr account a Discord account is linked to.
func (s *Server) linkedUsername(discordID string) string {
	var name string
	_ = s.db.QueryRow(`SELECT u.username FROM community_links l JOIN users u ON u.id = l.user_id
		WHERE l.backend = 'discord' AND l.external_id = ?`, discordID).Scan(&name)
	return name
}

type communityLinkInfo struct {
	Name     string `json:"name"`
	Avatar   string `json:"avatar,omitempty"`
	LinkedAt string `json:"linkedAt"`
}

func (s *Server) communityLinkOf(userID int64) *communityLinkInfo {
	var l communityLinkInfo
	err := s.db.QueryRow(`SELECT name, avatar, linked_at FROM community_links WHERE user_id = ? AND backend = 'discord'`, userID).
		Scan(&l.Name, &l.Avatar, &l.LinkedAt)
	if err != nil {
		return nil
	}
	return &l
}

// communityServerName is the name of the server Usherr posts in, when known.
func (s *Server) communityServerName() string {
	b := s.community()
	if b == nil {
		return ""
	}
	set := s.communitySettings()
	for _, sv := range b.Status().Servers {
		if sv.ID == set.Server {
			return sv.Name
		}
	}
	return ""
}

// GET /api/home/community/me (member): whether Community is on, and your
// linked Discord account.
func (s *Server) handleCommunityMe(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	on := false
	if b := s.community(); b != nil && b.Status().State == "connected" {
		on = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"on": on, "server": s.communityServerName(), "link": s.communityLinkOf(u.ID)})
}

// POST /api/home/community/link {"code"} (member): link the Discord account
// that asked for this code to your account.
func (s *Server) handleCommunityLink(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	code := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, body.Code)
	if len(code) != 6 {
		writeError(w, http.StatusBadRequest, "Type the six numbers from /link in Discord.")
		return
	}
	s.linkC.init()
	if !s.linkC.tries.Reserve(fmt.Sprint(u.ID)) {
		writeError(w, http.StatusTooManyRequests, "Too many wrong codes. Wait a few minutes, then type /link in Discord for a new one.")
		return
	}
	du, ok := s.linkC.take(code)
	if !ok {
		writeError(w, http.StatusBadRequest, "That code doesn't work: it's wrong, used or older than 10 minutes. Type /link in Discord for a new one.")
		return
	}
	if err := s.saveCommunityLink(r.Context(), u.ID, du); err != nil {
		writeError(w, http.StatusInternalServerError, "The link couldn't be saved. Try again.")
		return
	}
	s.auditAccount(r, "Discord account "+du.Name+" linked to "+u.Username)
	writeJSON(w, http.StatusOK, map[string]any{"link": s.communityLinkOf(u.ID)})
}

// DELETE /api/home/community/link (member): unlink your Discord account.
func (s *Server) handleCommunityUnlink(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	if _, err := s.db.Exec(`DELETE FROM community_links WHERE user_id = ?`, u.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "The link couldn't be removed. Try again.")
		return
	}
	s.auditAccount(r, "Discord account unlinked from "+u.Username)
	w.WriteHeader(http.StatusNoContent)
}

// communityLinked lists who has linked a Discord account (for the
// administrator's Community page).
func (s *Server) communityLinked() []map[string]string {
	out := []map[string]string{}
	rows, err := s.db.Query(`SELECT u.username, l.name, l.avatar FROM community_links l JOIN users u ON u.id = l.user_id
		WHERE l.backend = 'discord' ORDER BY u.username`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var user, name, avatar string
		if rows.Scan(&user, &name, &avatar) == nil {
			out = append(out, map[string]string{"username": user, "name": name, "avatar": avatar})
		}
	}
	return out
}

// saveCommunityLink links a Discord account to an Usherr account, unlinking
// either from whatever it was linked to before.
func (s *Server) saveCommunityLink(ctx context.Context, userID int64, du community.User) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM community_links WHERE user_id = ? OR (backend = 'discord' AND external_id = ?)`, userID, du.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO community_links (user_id, backend, external_id, name, avatar, linked_at) VALUES (?, 'discord', ?, ?, ?, ?)`,
		userID, du.ID, trimRunes(du.Name, 100), du.Avatar, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}
