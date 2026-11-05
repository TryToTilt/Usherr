package api

import (
	"reflect"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/community"
)

// A guest's Usherr reads the host's card as the host's Usherr makes it.
func TestJoinedReadsTheHostsCard(t *testing.T) {
	st := showtime{ID: 7, TMDBID: 603, Title: "The Matrix", AtMs: time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC).UnixMilli(), Runtime: 136, State: "booked"}
	people := []partyPerson{{ID: "1", Name: "Alex", Answer: "in"}, {ID: "2", Name: "Sam", Answer: "in"}, {ID: "3", Name: "Jo", Answer: "out"}}
	card := partyCard(st, party{Host: "Robin"}, people, st.State).Card

	code, ok := cardCode(card)
	if !ok || code.TMDBID != st.TMDBID || code.AtMs != st.AtMs {
		t.Fatalf("code read as %+v, %v", code, ok)
	}
	host, going := cardPeople(card)
	if host != "Robin" || !reflect.DeepEqual(going, []string{"Alex", "Sam"}) {
		t.Errorf("read %q going %v", host, going)
	}
	if _, going := cardPeople(partyCard(st, party{Host: "Robin"}, nil, st.State).Card); len(going) != 0 {
		t.Errorf("nobody going read as %v", going)
	}
	if host, _ := cardPeople(partyCard(st, party{Host: "Robin"}, people, "done").Card); host != "Robin" {
		t.Errorf("the host of a finished party read as %q", host)
	}
	if _, ok := cardCode(&community.Card{Fields: []community.Field{{Name: "Runtime", Value: "2h 16m"}}}); ok {
		t.Error("a card with no code read as having one")
	}
}

func TestJoinedStampLine(t *testing.T) {
	m := stampLineRe.FindStringSubmatch(stampLine("123456789012345678"))
	if m == nil || m[1] != "123456789012345678" {
		t.Fatalf("stamp line not read back: %v", m)
	}
	if stampLineRe.MatchString("<@123> left early") {
		t.Error("another line read as a stamp")
	}
}
