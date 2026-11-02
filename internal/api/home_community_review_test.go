package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// reviewTMDB knows Heat (a film) and The Wire (a show).
func reviewTMDB(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/movie/949"):
			_, _ = w.Write([]byte(`{"id":949,"title":"Heat","release_date":"1995-12-15","runtime":170,"poster_path":"/heat.jpg"}`))
		case r.URL.Path == "/search/movie":
			_, _ = w.Write([]byte(`{"results":[{"id":949,"title":"Heat","release_date":"1995-12-15","poster_path":"/heat.jpg"}],"total_pages":1}`))
		case r.URL.Path == "/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":1438,"name":"The Wire","first_air_date":"2002-06-02"}],"total_pages":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// linkDiscord links the signed-in account to a Discord account the way
// people do: /link, then the code.
func linkDiscord(t *testing.T, fd *fakeDiscord, base string, client *http.Client, id, name string) {
	t.Helper()
	before := fd.lastReply()
	fd.interact(t, map[string]any{"id": "i-" + id, "token": "tok", "type": 2, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
		"data": map[string]any{"name": "link"}, "member": map[string]any{"user": map[string]any{"id": id, "username": name, "global_name": name, "avatar": "av"}}})
	deadline := time.Now().Add(5 * time.Second)
	for fd.lastReply() == before {
		if time.Now().After(deadline) {
			t.Fatal("no answer to /link")
		}
		time.Sleep(20 * time.Millisecond)
	}
	m := regexp.MustCompile(`\*\*(\d{3}) (\d{3})\*\*`).FindStringSubmatch(fd.lastReply())
	if st, _ := doJSONStatus(t, client, http.MethodPost, base+"/api/home/community/link", map[string]any{"code": m[1] + m[2]}); st != http.StatusOK {
		t.Fatalf("link: %d", st)
	}
}

// Step 6: reviews. From Usherr (a card under your Discord name; posting again
// edits it), with /review in Discord (suggestions while typing, an answer
// after "thinking…"), and as "#review" in a watch party's thread (the
// message is the review, a ⭐ says so). Remove works only for whoever wrote
// it, and every review shows on the film's page.
func TestCommunityReviews(t *testing.T) {
	fd := newFakeDiscord(t)
	server, base, admin := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", reviewTMDB(t).URL)
	server.TestSetDiscord(fd.srv.URL, "ws"+strings.TrimPrefix(fd.srv.URL, "http")+"/gateway", plainWebsocket)
	t.Cleanup(func() {
		doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": false})
	})
	if st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "token": communityToken, "server": "900000000000000001", "channel": "700000000000000004"}); st != http.StatusOK {
		t.Fatalf("connect: %d %v", st, body)
	}
	reviews := base + "/api/home/reviews?kind=movie&tmdbId=949"

	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/reviews", map[string]any{"kind": "movie", "tmdbId": 949, "stars": 9, "text": "x"}); st != http.StatusForbidden {
		t.Errorf("a review before linking: %d", st)
	}
	linkDiscord(t, fd, base, admin, "555000000000000001", "Movie Fan")

	// From Usherr: a card.
	posted := len(fd.posted)
	st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/reviews", map[string]any{"kind": "movie", "tmdbId": 949, "stars": 9, "text": "The diner scene! ||the airport||"})
	if st != http.StatusOK {
		t.Fatalf("post: %d %v", st, body)
	}
	fd.mu.Lock()
	card, _ := json.Marshal(fd.posted[posted])
	fd.mu.Unlock()
	if !strings.Contains(string(card), "Heat (1995)") || !strings.Contains(string(card), "★★★★½") || !strings.Contains(string(card), "review:rm:movie:949") || !strings.Contains(string(card), "Movie Fan") {
		t.Fatalf("the card: %s", card)
	}
	// Again: the same card is edited, no second one.
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/reviews", map[string]any{"kind": "movie", "tmdbId": 949, "stars": 10, "text": "Even better again"}); st != http.StatusOK {
		t.Fatal("post again")
	}
	if len(fd.posted) != posted+1 || len(fd.called("PATCH", "/channels/700000000000000004/messages/1")) == 0 {
		t.Errorf("posting again made a new card (%d) instead of editing", len(fd.posted)-posted)
	}

	// /review from a friend without Usherr: suggestions, then "thinking…", then the answer.
	fd.interact(t, map[string]any{"id": "ac1", "token": "actok", "type": 4, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
		"data":   map[string]any{"name": "review", "options": []any{map[string]any{"name": "film", "value": "hea", "focused": true}}},
		"member": map[string]any{"user": map[string]any{"id": "777000000000000001", "username": "pal", "global_name": "Pal"}}})
	deadline := time.Now().Add(5 * time.Second)
	for {
		fd.mu.Lock()
		var choices []any
		for _, r := range fd.replies {
			if r["type"] == float64(8) {
				choices, _ = r["data"].(map[string]any)["choices"].([]any)
			}
		}
		fd.mu.Unlock()
		if len(choices) == 2 {
			if choices[0].(map[string]any)["value"] != "movie:949" || choices[1].(map[string]any)["value"] != "tv:1438" {
				t.Errorf("suggestions: %v", choices)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no suggestions")
		}
		time.Sleep(20 * time.Millisecond)
	}
	fd.interact(t, map[string]any{"id": "c2", "token": "pal-tok", "type": 2, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
		"data": map[string]any{"name": "review", "options": []any{
			map[string]any{"name": "film", "value": "movie:949"}, map[string]any{"name": "stars", "value": 6}, map[string]any{"name": "review", "value": "Long, but good"}}},
		"member": map[string]any{"user": map[string]any{"id": "777000000000000001", "username": "pal", "global_name": "Pal"}}})
	answer := fd.waitCalls(t, "PATCH", "/webhooks/4242/pal-tok/messages/@original", 1)[0]
	if !strings.Contains(answer.Body["content"].(string), "Posted your review of **Heat**") {
		t.Errorf("the answer to /review: %v", answer.Body)
	}

	// Remove: only for whoever wrote it.
	press := func(who string) {
		fd.interact(t, map[string]any{"id": "b-" + who, "token": "tok", "type": 3, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
			"data": map[string]any{"custom_id": "review:rm:movie:949"}, "member": map[string]any{"user": map[string]any{"id": who, "username": who}}})
	}
	press("999000000000000001")
	waitReply(t, fd, "Only whoever wrote")

	// #review in a party's thread.
	st, body = doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes", map[string]any{
		"tmdbId": 949, "title": "Heat", "year": 1995, "runtime": 170, "atMs": time.Now().Add(time.Hour).UnixMilli(), "preshowMin": 0, "playOn": "none", "share": true})
	if st != http.StatusCreated {
		t.Fatalf("book: %d %v", st, body)
	}
	fd.dispatch(t, "MESSAGE_CREATE", map[string]any{"id": "900000000000000501", "channel_id": "800000000000000005", "content": "#review 4/5 tense!",
		"timestamp": time.Now().UTC().Format(time.RFC3339), "author": map[string]any{"id": "888000000000000001", "username": "cousin", "global_name": "Cousin"}})
	fd.waitCalls(t, "PUT", "/channels/800000000000000005/messages/900000000000000501/reactions/", 1)

	_, list := doJSONStatus(t, admin, http.MethodGet, reviews, nil)
	got := map[string]map[string]any{}
	for _, r := range list["reviews"].([]any) {
		got[r.(map[string]any)["name"].(string)] = r.(map[string]any)
	}
	if got["Movie Fan"]["stars"] != float64(10) || got["Movie Fan"]["mine"] != true || got["Movie Fan"]["username"] != "ryan" ||
		got["Pal"]["stars"] != float64(6) || got["Cousin"]["stars"] != float64(8) || got["Cousin"]["text"] != "tense!" || got["Cousin"]["source"] != "typed" {
		t.Fatalf("the film page's reviews: %v", list["reviews"])
	}

	// Pal removes theirs; Cousin deletes the message; you remove yours from Usherr.
	press("777000000000000001")
	waitReply(t, fd, "Your review is removed")
	fd.dispatch(t, "MESSAGE_DELETE", map[string]any{"id": "900000000000000501", "channel_id": "800000000000000005"})
	time.Sleep(200 * time.Millisecond)
	if st, _ := doJSONStatus(t, admin, http.MethodDelete, reviews, nil); st != http.StatusNoContent {
		t.Errorf("remove mine: %d", st)
	}
	_, list = doJSONStatus(t, admin, http.MethodGet, reviews, nil)
	if l := list["reviews"].([]any); len(l) != 0 {
		t.Errorf("left after removing: %v", l)
	}
	if n := len(fd.called("DELETE", "/channels/700000000000000004/messages/")); n < 2 {
		t.Errorf("cards removed in Discord: %d", n)
	}
}

func waitReply(t *testing.T, fd *fakeDiscord, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(fd.lastReply(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("no answer with %q (last: %q)", want, fd.lastReply())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Reviews to a forum: the first review of a film opens its post (the film's
// card first), every review of it is a reply there, a #review from a watch
// party is copied in with a link back, a #review typed in the film's post is
// its own reply, and a post opened by hand gets a hint.
func TestCommunityReviewsForum(t *testing.T) {
	fd := newFakeDiscord(t)
	server, base, admin := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", reviewTMDB(t).URL)
	server.TestSetDiscord(fd.srv.URL, "ws"+strings.TrimPrefix(fd.srv.URL, "http")+"/gateway", plainWebsocket)
	t.Cleanup(func() {
		doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": false})
	})
	if st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "token": communityToken}); st != http.StatusOK {
		t.Fatalf("connect: %d %v", st, body)
	}
	st, body := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "server": "900000000000000001", "channel": "700000000000000004", "reviewsChannel": "700000000000000009"})
	if set := body["settings"].(map[string]any); st != http.StatusOK || set["reviewsKind"] != "forum" {
		t.Fatalf("the reviews forum: %d %v", st, body)
	}
	linkDiscord(t, fd, base, admin, "555000000000000001", "Movie Fan")

	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/reviews", map[string]any{"kind": "movie", "tmdbId": 949, "stars": 9, "text": "Great"}); st != http.StatusOK {
		t.Fatal("post")
	}
	post := fd.called("POST", "/channels/700000000000000009/threads")
	if len(post) != 1 || post[0].Body["name"] != "Heat (1995)" {
		t.Fatalf("the film's post: %+v", post)
	}
	replies := fd.waitCalls(t, "POST", "/channels/820000000000000007/messages", 1)
	if raw, _ := json.Marshal(replies[0].Body); !strings.Contains(string(raw), "Movie Fan") || !strings.Contains(string(raw), "★★★★½") {
		t.Errorf("the reply: %s", raw)
	}

	// A watch party's #review: copied into the film's post, with a link back.
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes", map[string]any{
		"tmdbId": 949, "title": "Heat", "year": 1995, "runtime": 170, "atMs": time.Now().Add(time.Hour).UnixMilli(), "preshowMin": 0, "playOn": "none", "share": true}); st != http.StatusCreated {
		t.Fatal("book")
	}
	fd.dispatch(t, "MESSAGE_CREATE", map[string]any{"id": "900000000000000601", "channel_id": "800000000000000005", "content": "#review 4/5 tense!",
		"timestamp": time.Now().UTC().Format(time.RFC3339), "author": map[string]any{"id": "888000000000000001", "username": "cousin", "global_name": "Cousin"}})
	replies = fd.waitCalls(t, "POST", "/channels/820000000000000007/messages", 2)
	if raw, _ := json.Marshal(replies[1].Body); !strings.Contains(string(raw), "From the Heat watch party") || !strings.Contains(string(raw), "800000000000000005/900000000000000601") {
		t.Errorf("the copy: %s", raw)
	}
	if n := len(fd.called("POST", "/channels/700000000000000009/threads")); n != 1 {
		t.Errorf("film posts opened: %d, want one for Heat", n)
	}

	// A #review typed in the film's post: its own reply, no copy.
	fd.dispatch(t, "MESSAGE_CREATE", map[string]any{"id": "900000000000000602", "channel_id": "820000000000000007", "content": "#review ★★★ okay",
		"timestamp": time.Now().UTC().Format(time.RFC3339), "author": map[string]any{"id": "999000000000000001", "username": "friend", "global_name": "Friend"}})
	fd.waitCalls(t, "PUT", "/channels/820000000000000007/messages/900000000000000602/reactions/", 1)
	if n := len(fd.called("POST", "/channels/820000000000000007/messages")); n != 2 {
		t.Errorf("a review typed in the post was copied too (%d replies)", n)
	}
	_, list := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/reviews?kind=movie&tmdbId=949", nil)
	if l := list["reviews"].([]any); len(l) != 3 {
		t.Errorf("reviews: %v", l)
	}

	// A post opened by hand: a hint.
	fd.dispatch(t, "THREAD_CREATE", map[string]any{"id": "830000000000000001", "parent_id": "700000000000000009", "owner_id": "999000000000000001", "newly_created": true})
	hint := fd.waitCalls(t, "POST", "/channels/830000000000000001/messages", 1)
	if !strings.Contains(hint[0].Body["content"].(string), "/review") {
		t.Errorf("the hint: %v", hint[0].Body)
	}
}

// /review in a film's post or a watch party's thread needs no film: it's
// that one. Elsewhere, without a film, it asks which.
func TestReviewCommandKnowsTheFilmWhereItIs(t *testing.T) {
	fd := newFakeDiscord(t)
	server, base, admin := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", reviewTMDB(t).URL)
	server.TestSetDiscord(fd.srv.URL, "ws"+strings.TrimPrefix(fd.srv.URL, "http")+"/gateway", plainWebsocket)
	t.Cleanup(func() {
		doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": false})
	})
	if st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "token": communityToken}); st != http.StatusOK {
		t.Fatal("connect")
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "server": "900000000000000001", "channel": "700000000000000004", "reviewsChannel": "700000000000000009"}); st != http.StatusOK {
		t.Fatal("settings")
	}
	if st, _ := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes", map[string]any{
		"tmdbId": 949, "title": "Heat", "year": 1995, "runtime": 170, "atMs": time.Now().Add(time.Hour).UnixMilli(), "preshowMin": 0, "playOn": "none", "share": true}); st != http.StatusCreated {
		t.Fatal("book")
	}
	review := func(token, channel string) string {
		fd.interact(t, map[string]any{"id": "c-" + token, "token": token, "type": 2, "guild_id": "900000000000000001", "channel_id": channel,
			"data":   map[string]any{"name": "review", "options": []any{map[string]any{"name": "stars", "value": 8}, map[string]any{"name": "review", "value": "So good"}}},
			"member": map[string]any{"user": map[string]any{"id": "777000000000000001", "username": "pal", "global_name": "Pal"}}})
		return fd.waitCalls(t, "PATCH", "/webhooks/4242/"+token+"/messages/@original", 1)[0].Body["content"].(string)
	}
	if got := review("in-party", "800000000000000005"); !strings.Contains(got, "Posted your review of **Heat**") {
		t.Errorf("in the party's thread: %q", got)
	}
	if got := review("nowhere", "700000000000000004"); !strings.Contains(got, "Which film?") {
		t.Errorf("without a film, outside a film's post: %q", got)
	}
	// Only the film (Discord hides the other boxes): a form asks for the
	// stars and words, and sending it posts the review.
	fd.interact(t, map[string]any{"id": "f1", "token": "form-ask", "type": 2, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
		"data":   map[string]any{"name": "review", "options": []any{map[string]any{"name": "film", "value": "movie:949"}}},
		"member": map[string]any{"user": map[string]any{"id": "777000000000000001", "username": "pal", "global_name": "Pal"}}})
	deadline := time.Now().Add(5 * time.Second)
	var form map[string]any
	for form == nil {
		fd.mu.Lock()
		for _, r := range fd.replies {
			if r["type"] == float64(9) {
				form, _ = r["data"].(map[string]any)
			}
		}
		fd.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("no form")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if form["custom_id"] != "review:form:movie:949" {
		t.Fatalf("the form: %v", form)
	}
	fd.interact(t, map[string]any{"id": "f2", "token": "form-sent", "type": 5, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
		"data": map[string]any{"custom_id": "review:form:movie:949", "components": []any{
			map[string]any{"type": 1, "components": []any{map[string]any{"type": 4, "custom_id": "stars", "value": "4,5"}}},
			map[string]any{"type": 1, "components": []any{map[string]any{"type": 4, "custom_id": "review", "value": "From the form"}}}}},
		"member": map[string]any{"user": map[string]any{"id": "777000000000000001", "username": "pal", "global_name": "Pal"}}})
	got := fd.waitCalls(t, "PATCH", "/webhooks/4242/form-sent/messages/@original", 1)[0].Body["content"].(string)
	if !strings.Contains(got, "Posted your review of **Heat**") {
		t.Errorf("after the form: %q", got)
	}
	_, list := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/reviews?kind=movie&tmdbId=949", nil)
	if l := list["reviews"].([]any); len(l) != 1 || l[0].(map[string]any)["stars"] != float64(9) || l[0].(map[string]any)["text"] != "From the form" {
		t.Errorf("the review from the form: %v", l)
	}
}

// Step 7: ticket stubs. At the door (the film starts), everyone who pressed
// I'm in gets one, seats in that order, and the host too; the feature message
// says who. At the credits a stamp button goes up: it stamps your own stub,
// once, and not for someone without one. Someone in late gets a stub then.
func TestTicketStubs(t *testing.T) {
	fd := newFakeDiscord(t)
	server, base, admin := loginNewServer(t)
	server.TestSetDiscord(fd.srv.URL, "ws"+strings.TrimPrefix(fd.srv.URL, "http")+"/gateway", plainWebsocket)
	t.Cleanup(func() {
		doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": false})
	})
	if st, _ := doJSONStatus(t, admin, http.MethodPut, base+"/api/home/community", map[string]any{"on": true, "token": communityToken, "server": "900000000000000001", "channel": "700000000000000004"}); st != http.StatusOK {
		t.Fatal("connect")
	}
	linkDiscord(t, fd, base, admin, "555000000000000001", "Host")
	st, body := doJSONStatus(t, admin, http.MethodPost, base+"/api/home/showtimes", map[string]any{
		"tmdbId": 949, "title": "Heat", "year": 1995, "runtime": 2, "atMs": time.Now().Add(time.Hour).UnixMilli(), "preshowMin": 0, "playOn": "none", "share": true})
	if st != http.StatusCreated {
		t.Fatalf("book: %d %v", st, body)
	}
	id := int64(body["id"].(float64))
	press := func(who, name, button string) string {
		before := fd.lastReply()
		fd.interact(t, map[string]any{"id": "b-" + who + button, "token": "tok", "type": 3, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
			"data": map[string]any{"custom_id": button}, "member": map[string]any{"user": map[string]any{"id": who, "username": name, "global_name": name}}})
		deadline := time.Now().Add(5 * time.Second)
		for fd.lastReply() == before {
			if time.Now().After(deadline) {
				t.Fatalf("no answer to %s", button)
			}
			time.Sleep(20 * time.Millisecond)
		}
		return fd.lastReply()
	}
	press("777000000000000001", "Pal", fmt.Sprintf("party:%d:in", id))

	// The film starts: stubs at the door.
	server.TestShowtimeAt(id, time.Now().Add(-time.Minute).UnixMilli())
	server.TestShowtimeJob(context.Background())
	said := fd.waitCalls(t, "POST", "/channels/800000000000000005/messages", 1)
	if txt := said[0].Body["content"].(string); !strings.Contains(txt, "Feature presentation") || !strings.Contains(txt, "Ticket stubs for Host, Pal") {
		t.Errorf("the feature message: %q", txt)
	}
	_, mine := doJSONStatus(t, admin, http.MethodGet, base+"/api/home/stubs", nil)
	stubs := mine["stubs"].([]any)
	if len(stubs) != 1 || stubs[0].(map[string]any)["seat"] != float64(0) || stubs[0].(map[string]any)["stamped"] != false ||
		stubs[0].(map[string]any)["guests"].([]any)[0] != "Pal" {
		t.Fatalf("the host's stub: %v", mine)
	}
	if got := press("888000000000000001", "Late", fmt.Sprintf("party:%d:in", id)); !strings.Contains(got, "already started") || !strings.Contains(got, "ticket stub") {
		t.Errorf("in, late: %q", got)
	}

	// Too early to stamp: it opens at the credits.
	if got := press("777000000000000001", "Pal", fmt.Sprintf("stub:stamp:%d", id)); !strings.Contains(got, "opens at the credits") {
		t.Errorf("stamping before the credits: %q", got)
	}
	// The credits: the stamp button goes up.
	server.TestShowtimeAt(id, time.Now().Add(-3*time.Minute).UnixMilli())
	server.TestShowtimeJob(context.Background())
	said = fd.waitCalls(t, "POST", "/channels/800000000000000005/messages", 2)
	if raw, _ := json.Marshal(said[1].Body); !strings.Contains(string(raw), fmt.Sprintf("stub:stamp:%d", id)) || !strings.Contains(string(raw), "That's a wrap") {
		t.Fatalf("the stamp button: %s", raw)
	}
	if got := press("777000000000000001", "Pal", fmt.Sprintf("stub:stamp:%d", id)); !strings.Contains(got, "Stamped!") {
		t.Errorf("stamp: %q", got)
	}
	if got := press("777000000000000001", "Pal", fmt.Sprintf("stub:stamp:%d", id)); !strings.Contains(got, "already stamped") {
		t.Errorf("stamp again: %q", got)
	}
	if got := press("999000000000000001", "Stranger", fmt.Sprintf("stub:stamp:%d", id)); !strings.Contains(got, "don't have a stub") {
		t.Errorf("no stub: %q", got)
	}

	// /stubs.
	before := fd.lastReply()
	fd.interact(t, map[string]any{"id": "s1", "token": "tok", "type": 2, "guild_id": "900000000000000001", "channel_id": "700000000000000004",
		"data": map[string]any{"name": "stubs"}, "member": map[string]any{"user": map[string]any{"id": "777000000000000001", "username": "Pal", "global_name": "Pal"}}})
	deadline := time.Now().Add(5 * time.Second)
	for fd.lastReply() == before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := fd.lastReply(); !strings.Contains(got, "1 ticket stub**, 1 stamped") || !strings.Contains(got, "✅ **Heat (1995)**") || !strings.Contains(got, "seat 1") {
		t.Errorf("/stubs: %q", got)
	}
}
