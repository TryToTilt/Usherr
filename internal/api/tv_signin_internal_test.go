package api

import (
	"strings"
	"testing"
)

func TestTVCodeNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"abcd-efgh": "ABCDEFGH",
		"ABCD EFGH": "ABCDEFGH",
		"ABCDEFGH":  "ABCDEFGH",
		"ABCDEFG":   "",
		"ABCDEFGHJ": "",
		"ABCD0FGH":  "", // 0 is not in the alphabet
		"ABCDIFGH":  "",
		"../../x":   "",
		"":          "",
	} {
		if got := normalizeTVCode(in); got != want {
			t.Errorf("normalizeTVCode(%q) = %q, want %q", in, got, want)
		}
	}
	for range 50 {
		c, err := randomCode(tvCodeLen)
		if err != nil || normalizeTVCode(c) != c {
			t.Fatalf("a made code doesn't read back: %q %v", c, err)
		}
	}
}

func TestTVDeviceLabel(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 (Linux; Android 11; SHIELD Android TV) AppleWebKit/537.36 Chrome/120.0 Safari/537.36": "Chrome on NVIDIA Shield",
		"Mozilla/5.0 (Linux; Android 9; AFTMM) AppleWebKit/537.36 Silk/120.0 Chrome/120 Safari/537.36":     "Silk on Fire TV",
		"Mozilla/5.0 (SMART-TV; Linux; Tizen 6.0) AppleWebKit/537.36 SamsungBrowser/4.0 Safari/537.36":     "Samsung Internet on a Samsung TV",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605 Version/17.0 Safari/604":   "Safari on iPhone",
		"curl/8.0": "A browser",
	} {
		if got := deviceLabel(ua); got != want {
			t.Errorf("deviceLabel(%q) = %q, want %q", ua, got, want)
		}
	}
}

func TestTVHandoffPathRules(t *testing.T) {
	good := []string{"/", "/title/603", "/title/603?tab=reviews", "/you/diary#new", "/settings/profile", "/crews/4?chat=1"}
	bad := []string{
		"", "title/603", "https://evil.example/", "http://evil.example", "//evil.example/x", "/\\evil.example",
		"javascript:alert(1)", "/../api/system/backup", "/title/../../x", "/api/system/backup", "/p/ABCDEFGH",
		"/tv-code?code=ABCDEFGH", "/%2e%2e/settings", "/title%2f..%2fapi", "/title/1\n", "/title/1 x", "/títle",
		"/title/<script>", "/title/" + strings.Repeat("a", 600), "/./title",
	}
	for _, p := range good {
		if !validHandoffPath(p) {
			t.Errorf("%q refused", p)
		}
	}
	for _, p := range bad {
		if validHandoffPath(p) {
			t.Errorf("%q accepted", p)
		}
	}
}
