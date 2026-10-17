package api

import "testing"

func TestHideLibraryPaths(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Night Harbour imported to /movies/Night Harbour (2021)/Night Harbour (2021).mkv", "Night Harbour imported to Night Harbour (2021).mkv"},
		{"Book (Ann Author, epub) imported to /books/Ann Author/Book.epub", "Book (Ann Author, epub) imported to Book.epub"},
		{"Album imported to /music/Artist/Album: 9 tracks, 1 skipped", "Album imported to Album: 9 tracks, 1 skipped"},
		{"Show S01E02 imported by hand to D:\\TV\\Show\\Season 1\\Show S01E02.mkv", "Show S01E02 imported by hand to Show S01E02.mkv"},
		{"Moved the file to /a/b/c.mkv.", "Moved the file to c.mkv."},
		{"Grabbed Saltwind and/or Tidewalker 1/2 from https://example.org/x", "Grabbed Saltwind and/or Tidewalker 1/2 from https://example.org/x"},
		{"Nothing to hide here", "Nothing to hide here"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := hideLibraryPaths(tt.in); got != tt.want {
			t.Errorf("hideLibraryPaths(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
