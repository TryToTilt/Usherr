package api

import "testing"

func TestMarkerTitles(t *testing.T) {
	for _, s := range []string{"Intro", "Opening", "OP", "op2", "Opening Credits", "Cold open"} {
		if !introTitle.MatchString(s) {
			t.Errorf("%q is an intro", s)
		}
	}
	for _, s := range []string{"Credits", "End Credits", "ED", "Ending", "Outro"} {
		if !creditsTitle.MatchString(s) {
			t.Errorf("%q is the credits", s)
		}
	}
	for _, s := range []string{"Chapter 1", "The Opening Night", "Prologue", "Episode"} {
		if introTitle.MatchString(s) || creditsTitle.MatchString(s) {
			t.Errorf("%q is neither", s)
		}
	}
}
