package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Crew Wrapped and the reaction, vote and star badges, from what one
// Usherr keeps: last year's parties (one NSFW, left out), their screening's
// reactions, the stars the crew shared and who picked the winners. Ryan
// sent the most EEKs at a screening and the most HAs in a month that's
// over, voted for three winners and gave ten films five stars.
func TestCrewWrappedAndBadges(t *testing.T) {
	server, base, client := loginNewServer(t)
	u := linkUsherr{server, base, client}
	db := server.TestDB()
	exec := func(q string, args ...any) int64 {
		t.Helper()
		res, err := db.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	var uid int64
	if err := db.QueryRow(`SELECT id FROM users WHERE username = 'ryan'`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	last := now.Year() - 1
	at := func(month time.Month, day int) int64 {
		return time.Date(last, month, day, 20, 0, 0, 0, time.Local).UnixMilli()
	}
	crewID := exec(`INSERT INTO crews (cid, name, my_hid, state, created_ms) VALUES ('c-wrapped', 'The Friday Crew', 1, 'joined', 1)`)
	exec(`INSERT INTO crew_people (crew_id, user_id, joined_ms) VALUES (?, ?, 1)`, crewID, uid)
	exec(`INSERT INTO crew_sharing (user_id, share, set_ms) VALUES (?, 1, 1)`, uid)
	other := exec(`INSERT INTO crews (cid, name, my_hid, state, created_ms) VALUES ('c-other', 'Not mine', 1, 'joined', 1)`)

	choices := func(v any) string {
		raw, _ := json.Marshal(v)
		return string(raw)
	}
	type seat = map[string]any
	stID := exec(`INSERT INTO showtimes (user_id, tmdb_id, title, runtime, at_ms) VALUES (?, 1091, 'The Thing', 109, ?)`, uid, at(time.March, 6))
	exec(`INSERT INTO link_screenings_out (showtime_id, sid, skey) VALUES (?, 's-thing', 'k')`, stID)
	vote := func(vid string, hostH int, host, club string, winner int, ch any, when int64, showtime int64, state string) {
		exec(`INSERT INTO crew_votes (crew_id, vid, host_hid, host_name, closes_ms, at_ms, club_kid, state, winner_tmdb, choices, showtime_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			crewID, vid, hostH, host, when-3600000, when, club, state, winner, choices(ch), showtime)
	}
	vote("v1", 1, "Ryan", "", 1091, []map[string]any{
		{"tmdb": 1091, "title": "The Thing", "voters": []seat{{"h": 1, "who": "Ryan"}, {"h": 2, "who": "Sam"}}},
		{"tmdb": 9, "title": "Tremors", "voters": []seat{{"h": 2, "who": "Alex"}}},
	}, at(time.March, 6), stID, "booked")
	vote("v2", 2, "Sam", "k1", 9, []map[string]any{{"tmdb": 9, "title": "Tremors"}}, at(time.April, 3), 0, "closed")
	vote("v3", 1, "Ryan", "", 329865, []map[string]any{
		{"tmdb": 329865, "title": "Arrival", "voters": []seat{{"h": 1, "who": "Ryan"}}},
		{"tmdb": 9, "title": "Tremors"},
	}, at(time.May, 1), 0, "closed")
	vote("v4", 1, "Ryan", "", 77, []map[string]any{{"tmdb": 77, "title": "Late Night XXX", "voters": []seat{{"h": 1, "who": "Ryan"}}}, {"tmdb": 9, "title": "Tremors"}}, at(time.June, 5), 0, "closed")
	vote("v5", 1, "Ryan", "", 0, []map[string]any{{"tmdb": 5, "title": "Nobody"}}, at(time.July, 5), 0, "nobody")
	vote("v6", 1, "Ryan", "", 603, []map[string]any{{"tmdb": 603, "title": "The Matrix", "voters": []seat{{"h": 1, "who": "Ryan"}}}, {"tmdb": 9, "title": "Tremors"}}, now.Add(-time.Hour).UnixMilli(), 0, "closed")

	// Reactions at The Thing: Ryan's three EEKs and Sam's two make the
	// moment; Ryan's ten HAs (spread out) are the most that month.
	react := func(id string, user int64, name, house, word string, pos float64, when int64) {
		exec(`INSERT INTO link_reactions (sid, id, tmdb_id, user_id, name, house, word, pos, at_ms) VALUES ('s-thing', ?, 1091, ?, ?, ?, ?, ?, ?)`, id, user, name, house, word, pos, when)
	}
	t0 := at(time.March, 6)
	for i := range 3 {
		react(fmt.Sprintf("e%d", i), uid, "Ryan", "Ryan's Usherr", "EEK", 4360+float64(i), t0+int64(i))
	}
	react("s1", 0, "Sam", "Sam's Usherr", "EEK", 4362, t0+10)
	react("s2", 0, "Sam", "Sam's Usherr", "EEK", 4363, t0+11)
	react("s3", 0, "Sam", "Sam's Usherr", "WOW", 200, t0+12)
	for i := range 10 {
		react(fmt.Sprintf("h%d", i), uid, "Ryan", "Ryan's Usherr", "HA", 600+float64(i)*60, t0+100+int64(i))
	}

	// Stars: Sam shared his with the crew; Ryan's Diary (he shares too).
	exec(`INSERT INTO crew_activity (crew_id, hid, who, kind, title, stars, at_ms) VALUES (?, 2, 'Sam', 'rated', 'The Thing', 4, ?), (?, 2, 'Sam', 'rated', 'Tremors', 6, ?)`, crewID, t0, crewID, t0)
	exec(`INSERT INTO home_diary (user_id, kind, tmdb_id, title, watched_on, stars) VALUES (?, 'movie', 1091, 'The Thing', ?, 10)`, uid, fmt.Sprintf("%d-03-06", last))
	for i := range 9 {
		exec(`INSERT INTO home_diary (user_id, kind, tmdb_id, title, watched_on, stars) VALUES (?, 'movie', ?, 'A film', ?, 10)`, uid, 5000+i, fmt.Sprintf("%d-04-%02d", last, i+1))
	}

	st, w := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/crews/%d/wrapped?year=%d", crewID, last), nil)
	if st != http.StatusOK {
		t.Fatalf("wrapped: %d %v", st, w)
	}
	if w["empty"] != false || num(w["parties"]) != 3 || num(w["minutes"]) != 109+105+105 || w["about"] != true {
		t.Fatalf("you showed up: %v", w)
	}
	if ys := w["years"].([]any); len(ys) != 2 || num(ys[0]) != float64(now.Year()) || num(ys[1]) != float64(last) {
		t.Fatalf("years: %v", w["years"])
	}
	fav, _ := w["favourite"].(map[string]any)
	div, _ := w["divisive"].(map[string]any)
	low, _ := w["lowest"].(map[string]any)
	if fav["title"] != "The Thing" || num(fav["stars"]) != 7 || num(fav["count"]) != 2 || div["title"] != "The Thing" || low["title"] != "Tremors" || w["starsFrom"] != "crew" {
		t.Fatalf("favourite: %v / %v / %v (%v)", fav, div, low, w["starsFrom"])
	}
	m, _ := w["moment"].(map[string]any)
	if m["title"] != "The Thing" || m["word"] != "EEK" || num(m["count"]) != 5 || num(m["total"]) != 5 {
		t.Fatalf("moment: %v", m)
	}
	if ha, _ := w["mostHa"].(map[string]any); ha["title"] != "The Thing" || num(ha["count"]) != 10 {
		t.Fatalf("most HAs: %v", w["mostHa"])
	}
	pickers := crewItems(w["pickers"])
	if len(pickers) != 2 || pickers[0]["who"] != "Ryan" || num(pickers[0]["wins"]) != 2 || pickers[0]["you"] != true || pickers[1]["who"] != "Sam" || num(pickers[1]["wins"]) != 2 {
		t.Fatalf("pickers: %v", pickers)
	}
	if raw, _ := json.Marshal(w); strings.Contains(string(raw), "XXX") {
		t.Fatalf("an NSFW title: %s", raw)
	}

	// An empty year, a year to come, and a crew Ryan isn't in.
	if _, w := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/crews/%d/wrapped?year=%d", crewID, last-3), nil); w["empty"] != true {
		t.Fatalf("an empty year: %v", w)
	}
	if st, _ := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/crews/%d/wrapped?year=%d", crewID, now.Year()+1), nil); st != http.StatusBadRequest {
		t.Fatalf("next year: %d", st)
	}
	if st, _ := u.call(t, http.MethodGet, fmt.Sprintf("/api/home/crews/%d/wrapped", other), nil); st != http.StatusForbidden {
		t.Fatalf("not in the crew: %d", st)
	}

	for _, id := range []string{"scream", "clown", "taste", "generous"} {
		if b := u.badge(t, id); num(b["earned"]) == 0 || num(b["have"]) != num(b["need"]) {
			t.Fatalf("badge %s: %v", id, b)
		}
	}
}
