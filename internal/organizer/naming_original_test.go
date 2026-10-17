package organizer_test

import (
	"path/filepath"
	"testing"

	"github.com/rdborg/mediarium/internal/organizer"
)

// "Keep the original file names": the file keeps the name it came with,
// unless that name is random letters, in which case the normal naming is used.
func TestOriginalNameKeepsTheNameItCameWith(t *testing.T) {
	cases := []struct {
		name, file  string
		mode        organizer.SanitizeMode
		replacement string
		want        string
		ok          bool
	}{
		{"a normal release file", filepath.Join("work", "queue-1", "Example.Movie.2024.1080p.BluRay.x264-GROUP.mkv"), organizer.SanitizeStrip, "", "Example.Movie.2024.1080p.BluRay.x264-GROUP.mkv", true},
		{"an episode of a season pack", filepath.Join("work", "Show.S01.1080p", "Show.S01E03.Third.One.1080p.WEB-DL-GRP.mkv"), organizer.SanitizeStrip, "", "Show.S01E03.Third.One.1080p.WEB-DL-GRP.mkv", true},
		{"spaces and brackets", "My Movie (2001) [1080p].mp4", organizer.SanitizeStrip, "", "My Movie (2001) [1080p].mp4", true},
		{"a plain number is a film", "1917.mkv", organizer.SanitizeStrip, "", "1917.mkv", true},
		{"one word", "Inception.mkv", organizer.SanitizeStrip, "", "Inception.mkv", true},
		{"illegal characters removed", "What? Now: Part*2.mkv", organizer.SanitizeStrip, "", "What Now Part2.mkv", true},
		{"illegal characters replaced", "What? Now: Part*2.mkv", organizer.SanitizeReplace, "-", "What- Now- Part-2.mkv", true},
		{"a leading dot would hide it", ".hidden.name.mkv", organizer.SanitizeStrip, "", "hidden.name.mkv", true},
		{"letters and digits mixed", "a8f7d6e5c4b3.mkv", organizer.SanitizeStrip, "", "", false},
		{"too short to say anything", "abc.mkv", organizer.SanitizeStrip, "", "", false},
		{"random case", "wFvDJHhDrbESDBLq.mkv", organizer.SanitizeStrip, "", "", false},
		{"random with digits", "Bz4tuzCT0GRU49jgjrQx.mkv", organizer.SanitizeStrip, "", "", false},
		{"a UUID", "3f2a9c1e-7b4d-4e8a-9c2f-1a2b3c4d5e6f.mkv", organizer.SanitizeStrip, "", "", false},
		{"a long number", "20240311123456.mkv", organizer.SanitizeStrip, "", "", false},
		{"only illegal characters", "???.mkv", organizer.SanitizeStrip, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := organizer.OriginalName(tc.file, tc.mode, tc.replacement)
			if got != tc.want || ok != tc.ok {
				t.Errorf("OriginalName(%q) = %q, %v; want %q, %v", tc.file, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestLooksObfuscatedLeavesRealNamesAlone(t *testing.T) {
	real := []string{"Example.Movie.2024.1080p", "TheMatrix", "Movie Title", "1917", "Alien", "Se7en", "S01E02", "WALL-E", "Up (2009)"}
	for _, n := range real {
		if organizer.LooksObfuscated(n) {
			t.Errorf("LooksObfuscated(%q) = true, want false", n)
		}
	}
	random := []string{"a8f7d6e5c4b3", "abc", "x1", "wFvDJHhDrbESDBLq", "d41d8cd98f00b204e9800998ecf8427e", "qwertzuiopasdfghjklyxcvbnm"}
	for _, n := range random {
		if !organizer.LooksObfuscated(n) {
			t.Errorf("LooksObfuscated(%q) = false, want true", n)
		}
	}
}

func TestOriginalTokens(t *testing.T) {
	ctx := organizer.NamingContext{
		MovieTitle: "Example Movie", Year: 2024,
		OriginalTitle:    "Example.Movie.2024.1080p.BluRay.x265-GROUP",
		OriginalFilename: "example-movie-1080p",
	}
	cases := []struct{ format, want string }{
		{"{Original Filename}", "example-movie-1080p"},
		{"{Original Title}", "Example.Movie.2024.1080p.BluRay.x265-GROUP"},
		{"{Movie Title} ({Year}) - {Original Filename}", "Example Movie (2024) - example-movie-1080p"},
		{"{OriginalTitle}", "Example.Movie.2024.1080p.BluRay.x265-GROUP"},
		{"{Movie Title}{ - Original Filename}", "Example Movie - example-movie-1080p"},
	}
	for _, tc := range cases {
		if got := organizer.Render(tc.format, ctx); got != tc.want {
			t.Errorf("Render(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
	if got := organizer.Render("{Movie Title}{ - Original Filename}", organizer.NamingContext{MovieTitle: "Example Movie"}); got != "Example Movie" {
		t.Errorf("an empty original file name leaves nothing behind, got %q", got)
	}
	for _, kind := range []organizer.TokenKind{organizer.TokenMovie, organizer.TokenTV} {
		format := "{Movie Title} {Original Filename} {Original Title}"
		if kind == organizer.TokenTV {
			format = "S{Season:00}E{Episode:00} {Original Filename} {Original Title}"
		}
		if msg := organizer.CheckFormat(format, kind); msg != "" {
			t.Errorf("CheckFormat(%q, %s) = %q, want it accepted", format, kind, msg)
		}
	}
	found := map[string]bool{}
	for _, tk := range organizer.Tokens() {
		found[tk.Token] = true
	}
	if !found["{Original Filename}"] || !found["{Original Title}"] {
		t.Error("the token list should offer {Original Filename} and {Original Title}")
	}
}
