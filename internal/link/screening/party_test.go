package screening

import (
	"encoding/json"
	"strings"
	"testing"
)

const testSID = "0b8f3c2e-5d1a-4f6b-9a7c-2e4d6f8a0b1c"

// Each party message is read strictly: unknown fields, a wrong version, bad
// tokens, stars out of range and sums that don't add up are dropped; names
// are cleaned; a vote that doesn't share carries no name.
func TestPartyChecks(t *testing.T) {
	read := func(body string, v interface{ Check() bool }) bool {
		t.Helper()
		if decode([]byte(body), v) != nil {
			return false
		}
		return v.Check()
	}
	tok := "a1b2c3d4e5f60718"
	ok := []struct {
		body string
		v    interface{ Check() bool }
	}{
		{`{"v":1,"sid":"` + testSID + `","people":[{"id":"` + tok + `","name":"Sam","seated":true}]}`, &LobbySeat{}},
		{`{"v":1,"sid":"` + testSID + `","seq":5,"seats":[{"seat":0,"id":"` + tok + `","name":"Ryan","house":"Robin's Usherr","seated":true},{"seat":1,"house":"Sam's Usherr"}]}`, &LobbyRoster{}},
		{`{"v":1,"sid":"` + testSID + `","id":"` + tok + `","name":"Sam","status":"snacks"}`, &RoomStatus{}},
		{`{"v":1,"sid":"` + testSID + `","id":"` + tok + `","name":"Sam","status":""}`, &RoomStatus{}},
		{`{"v":1,"sid":"` + testSID + `","seq":3,"on":true,"until":1700000000000,"statuses":[{"name":"Sam","house":"Sam's","status":"back"}]}`, &Intermission{}},
		{`{"v":1,"sid":"` + testSID + `","seq":4,"on":false}`, &Intermission{}},
		{`{"v":1,"sid":"` + testSID + `","id":"` + tok + `","stars":7}`, &VerdictVote{}},
		{`{"v":1,"sid":"` + testSID + `","id":"` + tok + `","stars":10,"share":true,"name":"Sam"}`, &VerdictVote{}},
		{`{"v":1,"sid":"` + testSID + `","seq":2,"count":2,"total":15,"shared":[{"name":"Sam","stars":9}]}`, &VerdictTally{}},
		{`{"v":1,"sid":"` + testSID + `","seq":2,"count":0,"total":0}`, &VerdictTally{}},
	}
	for _, c := range ok {
		if !read(c.body, c.v) {
			t.Errorf("dropped: %s", c.body)
		}
	}
	bad := []struct {
		body string
		v    interface{ Check() bool }
	}{
		{`{"v":2,"sid":"` + testSID + `","people":[]}`, &LobbySeat{}},
		{`{"v":1,"sid":"` + testSID + `","people":[{"id":"NOTHEX","name":"Sam"}]}`, &LobbySeat{}},
		{`{"v":1,"sid":"` + testSID + `","people":[{"id":"` + tok + `","name":"A"},{"id":"` + tok + `","name":"B"}]}`, &LobbySeat{}},
		{`{"v":1,"sid":"` + testSID + `","people":[],"extra":1}`, &LobbySeat{}},
		{`{"v":1,"sid":"not-a-sid","people":[]}`, &LobbySeat{}},
		{`{"v":1,"sid":"` + testSID + `","seq":5,"seats":[{"seat":1},{"seat":1}]}`, &LobbyRoster{}},
		{`{"v":1,"sid":"` + testSID + `","seq":5,"seats":[{"seat":-1}]}`, &LobbyRoster{}},
		{`{"v":1,"sid":"` + testSID + `","seq":0,"seats":[]}`, &LobbyRoster{}},
		{`{"v":1,"sid":"` + testSID + `","id":"` + tok + `","name":"Sam","status":"dancing"}`, &RoomStatus{}},
		{`{"v":1,"sid":"` + testSID + `","seq":3,"on":true}`, &Intermission{}},
		{`{"v":1,"sid":"` + testSID + `","seq":3,"on":false,"until":5}`, &Intermission{}},
		{`{"v":1,"sid":"` + testSID + `","seq":3,"on":true,"until":5,"statuses":[{"name":"Sam","status":""}]}`, &Intermission{}},
		{`{"v":1,"sid":"` + testSID + `","id":"` + tok + `","stars":0}`, &VerdictVote{}},
		{`{"v":1,"sid":"` + testSID + `","id":"` + tok + `","stars":11}`, &VerdictVote{}},
		{`{"v":1,"sid":"` + testSID + `","id":"` + tok + `","stars":5,"share":true}`, &VerdictVote{}},
		{`{"v":1,"sid":"` + testSID + `","seq":2,"count":2,"total":21}`, &VerdictTally{}},
		{`{"v":1,"sid":"` + testSID + `","seq":2,"count":1,"total":0}`, &VerdictTally{}},
		{`{"v":1,"sid":"` + testSID + `","seq":2,"count":1,"total":5,"shared":[{"name":"A","stars":5},{"name":"B","stars":5}]}`, &VerdictTally{}},
	}
	for _, c := range bad {
		if read(c.body, c.v) {
			t.Errorf("taken: %s", c.body)
		}
	}

	// A vote that doesn't share loses its name; names are cleaned.
	var v VerdictVote
	if !read(`{"v":1,"sid":"`+testSID+`","id":"`+tok+`","stars":6,"name":"Sam"}`, &v) || v.Name != "" {
		t.Fatalf("an unshared vote kept its name: %+v", v)
	}
	var r LobbyRoster
	if !read(`{"v":1,"sid":"`+testSID+`","seq":1,"seats":[{"seat":0,"name":"  Ry\u0000an  "},{"seat":1}]}`, &r) || r.Seats[0].Name != "Ryan" || r.Seats[1].Name != "" {
		t.Fatalf("names: %+v", r.Seats)
	}
	// Too many seats.
	seats := make([]Seat, MaxSeats+1)
	for i := range seats {
		seats[i].Seat = i
	}
	raw, _ := json.Marshal(LobbyRoster{V: 1, SID: testSID, Seq: 1, Seats: seats})
	if read(string(raw), &LobbyRoster{}) {
		t.Fatal("a seat map too big")
	}
	if strings.Contains(string(raw), "\"me\"") {
		t.Fatal("a seat map says who's reading it")
	}
	for _, s := range []string{"", "snacks", "drinks", "break", "back"} {
		if !ValidStatus(s) {
			t.Errorf("status %q", s)
		}
	}
}
