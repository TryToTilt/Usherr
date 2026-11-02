package api

// Usherr: Community, step 6. Reviews. One per person per film or show:
// stars and a few words. They're posted three ways:
//
//   - from Usherr (the film's page, or a Diary entry), under the person's
//     linked Discord name;
//   - with /review in Discord (the film suggested as it's typed), so friends
//     without Usherr can review;
//   - by typing "#review 4.5/5 …" in a watch party's thread (the party's
//     film) or in a film's post in the reviews forum (a ⭐ says it was taken).
//
// Every review gets a card where reviews go (Settings, Community: a text
// channel, or a forum with one post per film and the reviews as replies; the
// watch parties' channel when none is chosen), so a #review from a party is
// copied there with a link back, and nobody posts twice. Posting again
// replaces the earlier review (its card is edited). The card's Remove button
// works only for whoever wrote it; an edited or deleted #review message
// changes or removes the review. Every film page shows its reviews under
// "From the community".

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/auth"
	"github.com/rdborg/mediarium/internal/brand"
	"github.com/rdborg/mediarium/internal/community"
)

type review struct {
	Kind       string `json:"kind"`
	TMDBID     int    `json:"tmdbId"`
	ExternalID string `json:"-"`
	UserID     int64  `json:"-"`
	Username   string `json:"username,omitempty"` // their Usherr account, when linked
	Name       string `json:"name"`
	Avatar     string `json:"avatar,omitempty"`
	Stars      int    `json:"stars"`
	Text       string `json:"text"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	Poster     string `json:"-"`
	Source     string `json:"source"`
	Server     string `json:"-"`
	Channel    string `json:"-"` // where its card (or the typed message) is
	MessageID  string `json:"-"`
	// A typed review: where it was typed, and in which party.
	OriginChannel string `json:"-"`
	OriginMessage string `json:"-"`
	OriginLabel   string `json:"from,omitempty"`
	UpdatedMs     int64  `json:"updatedMs"`
	Link          string `json:"link,omitempty"` // the review in Discord
	Mine          bool   `json:"mine,omitempty"`
}

const reviewCols = `r.kind, r.tmdb_id, r.external_id, COALESCE(r.user_id, 0), COALESCE(u.username, ''), r.name, r.avatar, r.stars, r.text,
	r.title, r.year, r.poster, r.source, r.server, r.channel, r.message_id, r.origin_channel, r.origin_message, r.origin_label, r.updated_ms`

func scanReview(sc interface{ Scan(...any) error }) (review, error) {
	var rv review
	err := sc.Scan(&rv.Kind, &rv.TMDBID, &rv.ExternalID, &rv.UserID, &rv.Username, &rv.Name, &rv.Avatar, &rv.Stars, &rv.Text,
		&rv.Title, &rv.Year, &rv.Poster, &rv.Source, &rv.Server, &rv.Channel, &rv.MessageID, &rv.OriginChannel, &rv.OriginMessage, &rv.OriginLabel, &rv.UpdatedMs)
	if err == nil && rv.Server != "" && rv.Channel != "" && rv.MessageID != "" {
		rv.Link = discordLink(rv.Server, rv.Channel, rv.MessageID)
	}
	return rv, err
}

func discordLink(server, channel, message string) string {
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", server, channel, message)
}

func (s *Server) reviewOf(ctx context.Context, kind string, tmdbID int, externalID string) (review, bool) {
	rv, err := scanReview(s.db.QueryRowContext(ctx, `SELECT `+reviewCols+` FROM community_reviews r LEFT JOIN users u ON u.id = r.user_id
		WHERE r.backend = 'discord' AND r.kind = ? AND r.tmdb_id = ? AND r.external_id = ?`, kind, tmdbID, externalID))
	return rv, err == nil
}

// starsText is a rating as stars: 9 is ★★★★½.
func starsText(n int) string {
	if n <= 0 {
		return ""
	}
	n = min(n, 10)
	s := strings.Repeat("★", n/2)
	if n%2 == 1 {
		s += "½"
	}
	return s
}

var (
	reviewOutOf   = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*/\s*(10|5)\b`)
	reviewStarRun = regexp.MustCompile(`★+½?`)
	reviewStarsW  = regexp.MustCompile(`(?i)\b(\d(?:[.,]5)?)\s*stars?\b`)
)

// parseReview reads a typed review: "#review 4.5/5 Loved it", "#review ★★★★
// Great", "#review 8/10 …", "#review 4 stars …". It returns half stars (1 to
// 10, 0 when none was given) and the words without the rating.
func parseReview(text string) (int, string) {
	text = strings.TrimSpace(text)
	if len(text) >= 7 && strings.EqualFold(text[:7], "#review") {
		text = strings.TrimSpace(text[7:])
	}
	stars := 0
	take := func(loc []int) {
		text = strings.TrimSpace(text[:loc[0]] + " " + text[loc[1]:])
	}
	if m := reviewOutOf.FindStringSubmatchIndex(text); m != nil {
		v, _ := strconv.ParseFloat(strings.Replace(text[m[2]:m[3]], ",", ".", 1), 64)
		of, _ := strconv.ParseFloat(text[m[4]:m[5]], 64)
		stars = int(math.Round(v / of * 10))
		take(m[:2])
	} else if m := reviewStarRun.FindStringIndex(text); m != nil {
		run := text[m[0]:m[1]]
		stars = strings.Count(run, "★")*2 + strings.Count(run, "½")
		take(m)
	} else if m := reviewStarsW.FindStringSubmatchIndex(text); m != nil {
		v, _ := strconv.ParseFloat(strings.Replace(text[m[2]:m[3]], ",", ".", 1), 64)
		stars = int(math.Round(v * 2))
		take(m[:2])
	}
	text = strings.TrimSpace(strings.TrimLeft(text, " -–—:,."))
	return min(max(stars, 0), 10), text
}

func tmdbPage(kind string, id int) string {
	if kind == "tv" {
		return fmt.Sprintf("https://www.themoviedb.org/tv/%d", id)
	}
	return fmt.Sprintf("https://www.themoviedb.org/movie/%d", id)
}

// reviewCard is a review as the bot posts it. inPost: it's a reply in the
// film's own forum post, which already says which film it is.
func reviewCard(rv review, inPost bool) community.Post {
	desc := fmt.Sprintf("**%s** reviewed it", rv.Name)
	if st := starsText(rv.Stars); st != "" {
		desc += "  " + st
	}
	if rv.Text != "" {
		desc += "\n\n" + rv.Text
	}
	if rv.OriginLabel != "" && rv.OriginMessage != "" && rv.Server != "" {
		desc += fmt.Sprintf("\n\n-# [From %s](%s)", rv.OriginLabel, discordLink(rv.Server, rv.OriginChannel, rv.OriginMessage))
	}
	from := "Discord"
	if rv.Source == "usherr" {
		from = brand.Name
	}
	card := &community.Card{Title: titleYear(rv.Title, rv.Year), URL: tmdbPage(rv.Kind, rv.TMDBID), Description: desc, Color: 0xEF9F27,
		Thumbnail: rv.Poster, Footer: "Review · posted from " + from, Time: time.UnixMilli(rv.UpdatedMs)}
	buttons := []community.Button{{Label: "Remove", ID: fmt.Sprintf("review:rm:%s:%d", rv.Kind, rv.TMDBID), Style: "secondary"}}
	if inPost {
		card.Title, card.URL, card.Thumbnail = "", "", ""
	} else {
		buttons = append(buttons, community.Button{Label: "About it", URL: tmdbPage(rv.Kind, rv.TMDBID)})
	}
	return community.Post{Card: card, Buttons: buttons}
}

// filmPostCard is the first message of a film's post in the reviews forum.
func filmPostCard(kind string, id int, title string, year int, poster string) community.Post {
	what := "film"
	if kind == "tv" {
		what = "show"
	}
	return community.Post{Card: &community.Card{
		Title:       titleYear(title, year),
		URL:         tmdbPage(kind, id),
		Description: fmt.Sprintf("Talk about this %s here. A reply starting with `#review 4/5` (or **/review**) counts as your review: it shows on the %s's page in %s, one each. Other replies are just conversation.", what, what, brand.Name),
		Color:       0xEF9F27,
		Image:       poster,
		Footer:      brand.Name + " reviews",
	}}
}

// filmFacts are a film's or show's title, year and poster, from TMDB.
func (s *Server) filmFacts(ctx context.Context, kind string, id int) (string, int, string) {
	if !s.TMDB().HasAPIKey() {
		return "", 0, ""
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return s.tmdbBasics(ctx, kind, id)
}

// reviewsTo is where review cards go: the reviews channel (or forum), or the
// watch parties' channel when none is chosen.
func (s *Server) reviewsTo() (channel string, forum bool, server string, ok bool) {
	_, set, ok := s.communityCanShare()
	if !ok {
		return "", false, "", false
	}
	if set.ReviewsChannel != "" {
		return set.ReviewsChannel, set.ReviewsKind == "forum", set.Server, true
	}
	return set.Channel, false, set.Server, true
}

// filmPost is a film's post in the reviews forum, opened the first time it's
// reviewed (and again if someone deleted it).
func (s *Server) filmPost(ctx context.Context, b community.Backend, forum string, rv review, fresh bool) (string, error) {
	var thread string
	if !fresh && s.db.QueryRowContext(ctx, `SELECT thread_id FROM community_review_posts WHERE backend = 'discord' AND forum = ? AND kind = ? AND tmdb_id = ?`,
		forum, rv.Kind, rv.TMDBID).Scan(&thread) == nil {
		return thread, nil
	}
	thread, err := b.StartForumPost(ctx, forum, titleYear(rv.Title, rv.Year), filmPostCard(rv.Kind, rv.TMDBID, rv.Title, rv.Year, rv.Poster))
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO community_review_posts (backend, forum, kind, tmdb_id, thread_id) VALUES ('discord', ?, ?, ?, ?)
		ON CONFLICT (backend, forum, kind, tmdb_id) DO UPDATE SET thread_id = excluded.thread_id`, forum, rv.Kind, rv.TMDBID, thread)
	return thread, err
}

// filmOfPost is the film a post in the reviews forum is for.
func (s *Server) filmOfPost(ctx context.Context, thread string) (kind string, id int, ok bool) {
	ok = s.db.QueryRowContext(ctx, `SELECT kind, tmdb_id FROM community_review_posts WHERE backend = 'discord' AND thread_id = ?`, thread).Scan(&kind, &id) == nil
	return kind, id, ok
}

// saveReview keeps a review and puts its card where reviews go: in a forum,
// a reply in the film's post. Its earlier card is edited when it's still
// there, or replaced. A review typed right in that place is its own card.
func (s *Server) saveReview(ctx context.Context, rv review) (review, error) {
	b := s.community()
	prev, had := s.reviewOf(ctx, rv.Kind, rv.TMDBID, rv.ExternalID)
	rv.UpdatedMs = time.Now().UnixMilli()
	if rv.UserID == 0 {
		_ = s.db.QueryRowContext(ctx, `SELECT user_id FROM community_links WHERE backend = 'discord' AND external_id = ?`, rv.ExternalID).Scan(&rv.UserID)
	}
	to, forum, server, ok := s.reviewsTo()
	if !ok || b == nil {
		if rv.Source != "typed" {
			return rv, errCommunityOff
		}
	} else {
		rv.Server = server
		dest := to
		if forum {
			thread, err := s.filmPost(ctx, b, to, rv, false)
			if err != nil {
				return rv, err
			}
			dest = thread
		}
		oldCard := had && prev.MessageID != "" && !(prev.Source == "typed" && prev.MessageID == prev.OriginMessage)
		if rv.Source == "typed" && rv.OriginChannel == dest {
			// Typed right here: the message is the card.
			rv.Channel, rv.MessageID = rv.OriginChannel, rv.OriginMessage
		} else {
			post := reviewCard(rv, forum)
			if oldCard && prev.Channel == dest {
				if err := b.Edit(ctx, community.Ref{Channel: prev.Channel, ID: prev.MessageID}, post); err == nil {
					rv.Channel, rv.MessageID = prev.Channel, prev.MessageID
					oldCard = false
				}
			}
			if rv.MessageID == "" {
				ref, err := b.Post(ctx, dest, post)
				if err != nil && forum && errorsIsNotFound(err) {
					// The film's post was deleted in Discord: open it again.
					if thread, err2 := s.filmPost(ctx, b, to, rv, true); err2 == nil {
						dest = thread
						ref, err = b.Post(ctx, dest, post)
					}
				}
				if err != nil {
					return rv, err
				}
				rv.Channel, rv.MessageID = dest, ref.ID
			}
		}
		if oldCard && (prev.Channel != rv.Channel || prev.MessageID != rv.MessageID) {
			_ = b.Delete(ctx, community.Ref{Channel: prev.Channel, ID: prev.MessageID})
		}
	}
	var uid any
	if rv.UserID > 0 {
		uid = rv.UserID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO community_reviews (backend, kind, tmdb_id, external_id, user_id, name, avatar, stars, text, title, year, poster, source,
			server, channel, message_id, origin_channel, origin_message, origin_label, updated_ms)
		VALUES ('discord', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (backend, kind, tmdb_id, external_id) DO UPDATE SET user_id = excluded.user_id, name = excluded.name, avatar = excluded.avatar,
			stars = excluded.stars, text = excluded.text, title = excluded.title, year = excluded.year, poster = excluded.poster, source = excluded.source,
			server = excluded.server, channel = excluded.channel, message_id = excluded.message_id, origin_channel = excluded.origin_channel,
			origin_message = excluded.origin_message, origin_label = excluded.origin_label, updated_ms = excluded.updated_ms`,
		rv.Kind, rv.TMDBID, rv.ExternalID, uid, trimRunes(rv.Name, 100), rv.Avatar, rv.Stars, rv.Text, trimRunes(rv.Title, 200), rv.Year, rv.Poster,
		rv.Source, rv.Server, rv.Channel, rv.MessageID, rv.OriginChannel, rv.OriginMessage, rv.OriginLabel, rv.UpdatedMs)
	return rv, err
}

// removeReview takes a review away, and its card with it (a typed one's own
// message stays: it's the person's to delete).
func (s *Server) removeReview(ctx context.Context, kind string, tmdbID int, externalID string) bool {
	rv, ok := s.reviewOf(ctx, kind, tmdbID, externalID)
	if !ok {
		return false
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM community_reviews WHERE backend = 'discord' AND kind = ? AND tmdb_id = ? AND external_id = ?`, kind, tmdbID, externalID)
	if b := s.community(); b != nil && rv.MessageID != "" && rv.MessageID != rv.OriginMessage {
		if err := b.Delete(ctx, community.Ref{Channel: rv.Channel, ID: rv.MessageID}); err != nil {
			slog.Warn("community: couldn't remove a review's card", "err", err)
		}
	}
	return true
}

// typedReviewOf is the review a typed message is, if it is one.
func (s *Server) typedReviewOf(ctx context.Context, message string) (kind string, id int, ext string, ok bool) {
	ok = s.db.QueryRowContext(ctx, `SELECT kind, tmdb_id, external_id FROM community_reviews WHERE source = 'typed' AND origin_message = ?`, message).Scan(&kind, &id, &ext) == nil
	return kind, id, ext, ok
}

// typedReview follows a "#review" message about a film: in a watch party's
// thread (label: "the Heat watch party") or in the film's post in the
// reviews forum. Edited, the review changes; without "#review" any more, or
// deleted, it goes.
func (s *Server) typedReview(ctx context.Context, kind string, tmdbID int, title string, year int, poster, label string, ev community.MessageEvent) {
	if ev.Kind == "delete" {
		if k, id, ext, ok := s.typedReviewOf(ctx, ev.ID); ok {
			s.removeReview(ctx, k, id, ext)
		}
		return
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ev.Text)), "#review") {
		if ev.Kind == "edit" {
			if k, id, ext, ok := s.typedReviewOf(ctx, ev.ID); ok {
				s.removeReview(ctx, k, id, ext)
			}
		}
		return
	}
	stars, text := parseReview(ev.Text)
	rv := review{Kind: kind, TMDBID: tmdbID, ExternalID: ev.User.ID, Name: ev.User.Name, Avatar: ev.User.Avatar, Stars: stars, Text: text,
		Title: title, Year: year, Poster: poster, Source: "typed", OriginChannel: ev.Channel, OriginMessage: ev.ID, OriginLabel: label}
	// An edit keeps who wrote it as it was (an edit carries no author).
	if ev.Kind == "edit" {
		k, id, ext, ok := s.typedReviewOf(ctx, ev.ID)
		if !ok || k != kind || id != tmdbID {
			return
		}
		was, _ := s.reviewOf(ctx, k, id, ext)
		rv.ExternalID, rv.Name, rv.Avatar = ext, was.Name, was.Avatar
	}
	if rv.ExternalID == "" {
		return
	}
	if _, err := s.saveReview(ctx, rv); err != nil {
		slog.Warn("community: couldn't keep a #review", "err", err)
		return
	}
	if b := s.community(); b != nil && ev.Kind == "new" {
		_ = b.React(ctx, community.Ref{Channel: ev.Channel, ID: ev.ID}, "⭐")
	}
}

// reviewsForumMessage handles a message in a post of the reviews forum: a
// #review in a film's post is a review of that film. It reports whether the
// message was in a film's post.
func (s *Server) reviewsForumMessage(ctx context.Context, ev community.MessageEvent) bool {
	kind, id, ok := s.filmOfPost(ctx, ev.Channel)
	if !ok {
		return false
	}
	var title, poster string
	var year int
	_ = s.db.QueryRowContext(ctx, `SELECT title, year, poster FROM community_reviews WHERE kind = ? AND tmdb_id = ? ORDER BY updated_ms DESC LIMIT 1`, kind, id).Scan(&title, &year, &poster)
	if title == "" {
		title, year, poster = s.filmFacts(ctx, kind, id)
	}
	s.typedReview(ctx, kind, id, title, year, poster, "", ev)
	return true
}

// reviewsForumThread answers a post someone opened by hand in the reviews
// forum (a talk about anything, which is welcome): Usherr can't tell which
// film a #review there would be about, so it says how reviews count.
func (s *Server) reviewsForumThread(data []byte) {
	var th struct {
		ID      string `json:"id"`
		Parent  string `json:"parent_id"`
		Owner   string `json:"owner_id"`
		Created bool   `json:"newly_created"`
	}
	if json.Unmarshal(data, &th) != nil || !th.Created {
		return
	}
	b := s.community()
	set := s.communitySettings()
	if b == nil || set.ReviewsKind != "forum" || th.Parent != set.ReviewsChannel || th.Owner == b.Status().ID {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := b.Post(ctx, th.ID, community.Post{Text: "Talk away! 🎬 For a review that shows on the film's page in " + brand.Name + ", use **/review** " +
		"(start typing the film's name and pick it), or reply in that film's own post starting with `#review 4/5`."}); err != nil {
		slog.Warn("community: couldn't answer a new post in the reviews forum", "err", err)
	}
}

func errorsIsNotFound(err error) bool { return errors.Is(err, community.ErrNotFound) }

// communityEvent hears the gateway's other events.
func (s *Server) communityEvent(kind string, data json.RawMessage) {
	if kind == "THREAD_CREATE" {
		go s.reviewsForumThread(data)
	}
}

// reviewForm is /review's pop-up form: stars and words. The film (picked,
// typed, or "" where it's known) rides along in the form's ID.
func reviewForm(film string) community.Form {
	return community.Form{ID: "review:form:" + trimRunes(film, 80), Title: "Your review", Boxes: []community.Box{
		{ID: "stars", Label: "Stars, out of 5", Placeholder: "4.5", Max: 10},
		{ID: "review", Label: "Your review", Placeholder: "What did you think? ||Like this|| hides a spoiler.", Long: true, Max: 1500},
	}}
}

// reviewFromForm turns a sent form back into /review's options: half stars
// from "4.5", "4,5", "9/10" or "★★★★½".
func reviewFromForm(in community.Interaction) map[string]string {
	out := map[string]string{"film": strings.TrimPrefix(in.Name, "review:form:"), "review": in.Options["review"]}
	raw := strings.TrimSpace(in.Options["stars"])
	stars := 0
	if v, err := strconv.ParseFloat(strings.Replace(raw, ",", ".", 1), 64); err == nil {
		stars = int(math.Round(v * 2))
	} else if raw != "" {
		stars, _ = parseReview(raw)
	}
	if stars > 0 {
		out["stars"] = strconv.Itoa(min(stars, 10))
	}
	return out
}

// reviewStarChoices are /review's stars, best first.
func reviewStarChoices() []community.Choice {
	out := []community.Choice{}
	for n := 10; n >= 1; n-- {
		out = append(out, community.Choice{Name: fmt.Sprintf("%s  %s", starsText(n), strconv.FormatFloat(float64(n)/2, 'f', -1, 64)), Value: strconv.Itoa(n)})
	}
	return out
}

// reviewFilmPicks suggests films and shows for /review as the name is typed:
// "Heat (1995) · film", worth "movie:949".
func (s *Server) reviewFilmPicks(ctx context.Context, q string) []community.Choice {
	out := []community.Choice{}
	q = strings.TrimSpace(q)
	if len(q) < 2 || !s.TMDB().HasAPIKey() {
		return out
	}
	name := func(t string, y int, what string) string {
		if y > 0 {
			return fmt.Sprintf("%s (%d) · %s", t, y, what)
		}
		return t + " · " + what
	}
	// Films and shows at once: Discord gives the suggestions three seconds.
	var movies []community.Choice
	var shows []community.Choice
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if list, err := s.TMDB().SearchMovies(ctx, q); err == nil {
			for i, m := range list {
				if i == 7 {
					break
				}
				movies = append(movies, community.Choice{Name: name(m.Title, m.Year(), "film"), Value: fmt.Sprintf("movie:%d", m.TMDBID)})
			}
		}
	}()
	go func() {
		defer wg.Done()
		if list, err := s.TMDB().SearchTV(ctx, q); err == nil {
			for i, sh := range list {
				if i == 4 {
					break
				}
				shows = append(shows, community.Choice{Name: name(sh.Name, sh.Year(), "show"), Value: fmt.Sprintf("tv:%d", sh.TMDBID)})
			}
		}
	}()
	wg.Wait()
	out = append(append(out, movies...), shows...)
	return out
}

// reviewCommand answers /review: the film (picked from the suggestions, or
// the first found for what was typed), the stars and the words.
func (s *Server) reviewCommand(ctx context.Context, in community.Interaction) string {
	film := strings.TrimSpace(in.Options["film"])
	kind, id := "", 0
	if k, v, ok := strings.Cut(film, ":"); ok && (k == "movie" || k == "tv") {
		kind = k
		id, _ = strconv.Atoi(v)
	}
	// In a film's post in the reviews forum, or a watch party's thread, the
	// film is that one: nothing to pick.
	if id <= 0 && film == "" {
		if k, v, ok := s.filmOfPost(ctx, in.Channel); ok {
			kind, id = k, v
		} else if sid, ok := s.partyOfThread(ctx, in.Channel); ok {
			if st, ok := s.showtimeByID(ctx, sid); ok {
				kind, id = "movie", st.TMDBID
			}
		}
		if id <= 0 {
			return "Which film? Type /review again and, in the **film** box, start typing its name and pick it from the list. (In a film's own post in the reviews forum, or a watch party's thread, there's no need: it's that film.)"
		}
	}
	if id <= 0 {
		if picks := s.reviewFilmPicks(ctx, film); len(picks) > 0 {
			k, v, _ := strings.Cut(picks[0].Value, ":")
			kind = k
			id, _ = strconv.Atoi(v)
		}
	}
	if id <= 0 {
		return fmt.Sprintf("I couldn't find a film called “%s”. In the **film** box, start typing the film's name and pick it from the list; your words go in the **review** box.", trimRunes(film, 60))
	}
	title, year, poster := s.filmFacts(ctx, kind, id)
	if title == "" {
		return "I couldn't look that film up. Try again in a minute."
	}
	stars, _ := strconv.Atoi(in.Options["stars"])
	rv := review{Kind: kind, TMDBID: id, ExternalID: in.User.ID, Name: in.User.Name, Avatar: in.User.Avatar, Stars: min(max(stars, 0), 10),
		Text: strings.TrimSpace(in.Options["review"]), Title: title, Year: year, Poster: poster, Source: "command"}
	if rv.Stars == 0 && rv.Text == "" {
		return "Give it some stars, or a few words."
	}
	if _, err := s.saveReview(ctx, rv); err != nil {
		if err == errCommunityOff {
			return brand.Name + " has no channel to post reviews in yet."
		}
		return "Your review couldn't be posted: " + communityProblem(err, "in the Community channel")
	}
	return fmt.Sprintf("Posted your review of **%s**. Use /review again to change it, or Remove on the card.", title)
}

// reviewButton answers a review card's Remove button: only for whoever wrote it.
func (s *Server) reviewButton(ctx context.Context, in community.Interaction) string {
	parts := strings.Split(in.Name, ":") // review:rm:kind:id
	if len(parts) != 4 || parts[1] != "rm" {
		return "That button isn't known."
	}
	id, _ := strconv.Atoi(parts[3])
	if _, ok := s.reviewOf(ctx, parts[2], id, in.User.ID); !ok {
		return "Only whoever wrote a review can remove it."
	}
	s.removeReview(ctx, parts[2], id, in.User.ID)
	return "Your review is removed."
}

// reviewsFor are a film's or show's reviews, newest first.
func (s *Server) reviewsFor(ctx context.Context, kind string, tmdbID int, me string) []review {
	out := []review{}
	rows, err := s.db.QueryContext(ctx, `SELECT `+reviewCols+` FROM community_reviews r LEFT JOIN users u ON u.id = r.user_id
		WHERE r.backend = 'discord' AND r.kind = ? AND r.tmdb_id = ? ORDER BY r.updated_ms DESC`, kind, tmdbID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		if rv, err := scanReview(rows); err == nil {
			rv.Mine = me != "" && rv.ExternalID == me
			out = append(out, rv)
		}
	}
	return out
}

// myDiscordID is the asker's linked Discord account, or "".
func (s *Server) myDiscordID(ctx context.Context, userID int64) string {
	var id string
	_ = s.db.QueryRowContext(ctx, `SELECT external_id FROM community_links WHERE backend = 'discord' AND user_id = ?`, userID).Scan(&id)
	return id
}

func reviewTarget(r *http.Request) (string, int, bool) {
	kind := r.URL.Query().Get("kind")
	id, err := strconv.Atoi(r.URL.Query().Get("tmdbId"))
	return kind, id, err == nil && id > 0 && (kind == "movie" || kind == "tv")
}

// GET /api/home/reviews?kind=&tmdbId= (member): a film's reviews from the
// community, and whether you can post one.
func (s *Server) handleReviews(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := reviewTarget(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Say which film or show.")
		return
	}
	me := ""
	if u := auth.UserFromContext(r.Context()); u != nil {
		me = s.myDiscordID(r.Context(), u.ID)
	}
	_, _, connected := s.communityCanShare()
	list := s.reviewsFor(r.Context(), kind, id, me)
	var mine *review
	for i := range list {
		if list[i].Mine {
			mine = &list[i]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": list, "mine": mine, "linked": me != "", "connected": connected})
}

// POST /api/home/reviews {kind, tmdbId, stars, text} (member): post your
// review (or replace it) under your linked Discord name.
func (s *Server) handlePostReview(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		writeError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	var body struct {
		Kind   string `json:"kind"`
		TMDBID int    `json:"tmdbId"`
		Stars  int    `json:"stars"`
		Text   string `json:"text"`
	}
	if err := decodeJSON(r, &body); err != nil || (body.Kind != "movie" && body.Kind != "tv") || body.TMDBID <= 0 || body.Stars < 0 || body.Stars > 10 {
		writeError(w, http.StatusBadRequest, "That review couldn't be read.")
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Stars == 0 && body.Text == "" {
		writeError(w, http.StatusBadRequest, "Give it some stars, or a few words.")
		return
	}
	if len([]rune(body.Text)) > 1500 {
		writeError(w, http.StatusBadRequest, "That's a long review. Keep it under 1,500 characters.")
		return
	}
	link := s.communityLinkOf(u.ID)
	ext := s.myDiscordID(r.Context(), u.ID)
	if link == nil || ext == "" {
		writeError(w, http.StatusForbidden, "Link your Discord account first (Settings, Linked services): your review carries your Discord name.")
		return
	}
	title, year, poster := s.filmFacts(r.Context(), body.Kind, body.TMDBID)
	if title == "" {
		writeError(w, http.StatusBadRequest, "That film couldn't be looked up on TMDB.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rv, err := s.saveReview(ctx, review{Kind: body.Kind, TMDBID: body.TMDBID, ExternalID: ext, UserID: u.ID, Name: link.Name, Avatar: link.Avatar,
		Stars: body.Stars, Text: body.Text, Title: title, Year: year, Poster: poster, Source: "usherr"})
	if err != nil {
		if err == errCommunityOff {
			writeError(w, http.StatusConflict, "Community isn't connected, or has no channel to post in.")
			return
		}
		writeError(w, http.StatusBadGateway, communityProblem(err, "in the Community channel"))
		return
	}
	rv.Mine = true
	writeJSON(w, http.StatusOK, map[string]any{"review": rv})
}

// DELETE /api/home/reviews?kind=&tmdbId= (member): remove your review, and
// its card in Discord.
func (s *Server) handleDeleteReview(w http.ResponseWriter, r *http.Request) {
	kind, id, ok := reviewTarget(r)
	u := auth.UserFromContext(r.Context())
	if !ok || u == nil {
		writeError(w, http.StatusBadRequest, "Say which film or show.")
		return
	}
	ext := s.myDiscordID(r.Context(), u.ID)
	if ext == "" || !s.removeReview(r.Context(), kind, id, ext) {
		writeError(w, http.StatusNotFound, "You haven't reviewed it.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
