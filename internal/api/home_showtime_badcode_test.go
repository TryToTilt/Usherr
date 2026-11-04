package api

import (
	"strings"
	"testing"
)

func TestBadCodeMessage(t *testing.T) {
	if codeDataChars+1 != 13 {
		t.Fatalf("a code is %d characters, the message says 13", codeDataChars+1)
	}
	for in, want := range map[string]string{
		"HXK4-7QPM-2RD9":       "has 12 letters",
		"HXK4-7QPM-2RD9CC":     "has 14 letters",
		"HXK4-7QPM-2RD9X":      "typo",
		"see you at the film!": "isn't a showtime code",
	} {
		if got := badCodeMessage(in); !strings.Contains(got, want) {
			t.Errorf("%q: %q, want %q in it", in, got, want)
		}
	}
}
