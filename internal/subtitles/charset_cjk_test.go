package subtitles

import (
	"strings"
	"testing"
)

func TestToUTF8EastAsianCodePages(t *testing.T) {
	tests := []struct {
		name string
		lang string
		in   string
		want string
	}{
		{"japanese shift-jis", "ja", "\x82\xb1\x82\xf1\x82\xc9\x82\xbf\x82\xcd", "こんにちは"},
		{"chinese simplified gbk", "zh", "\xc4\xe3\xba\xc3", "你好"},
		{"chinese simplified region", "zh-CN", "\xc4\xe3\xba\xc3", "你好"},
		{"chinese traditional big5", "zh-TW", "\xa7\x41\xa6\x6e", "你好"},
		{"korean euc-kr", "ko", "\xbe\xc8\xb3\xe7", "안녕"},
		{"thai windows-874", "th", "\xca\xc7\xd1\xca\xb4\xd5", "สวัสดี"},
		{"no known code page is left alone", "hi", "\xe0\xe1\xe2", "\xe0\xe1\xe2"},
		{"western language still windows-1252", "fr", "caf\xe9", "café"},
		{"utf-8 stays", "ja", "こんにちは", "こんにちは"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(ToUTF8([]byte(tc.in), tc.lang)); got != tc.want {
				t.Errorf("ToUTF8(%q, %q) = %q, want %q", tc.in, tc.lang, got, tc.want)
			}
		})
	}
}

// One damaged byte in a UTF-8 file does not turn the whole file into a code page.
func TestToUTF8KeepsMostlyUTF8Text(t *testing.T) {
	line := "Привет, мир! Это тест.\n"
	in := strings.Repeat(line, 12) + "\xff" + line
	got := string(ToUTF8([]byte(in), "ru"))
	if !strings.Contains(got, "Привет, мир! Это тест.") {
		t.Fatalf("the Russian text was damaged: %q", got[:60])
	}
	if strings.Count(got, "Привет") != 13 {
		t.Fatalf("lines lost: %d", strings.Count(got, "Привет"))
	}
}
