package api

import "testing"

func TestMainArtist(t *testing.T) {
	cases := map[string]string{
		"Elvis Presley with The Jordanaires":                     "Elvis Presley",
		"Prince & The New Power Generation":                      "Prince",
		"The London Jazz Four _ George Martin and His Orchestra": "The London Jazz Four",
		"Elvis Presley":     "",
		"Simon & Garfunkel": "Simon",
	}
	for in, want := range cases {
		if got := mainArtist(in); got != want {
			t.Errorf("mainArtist(%q) = %q, want %q", in, got, want)
		}
	}
	if releaseYear("1961-10-01") != 1961 || releaseYear("") != 0 {
		t.Error("releaseYear")
	}
}
