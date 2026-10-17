package api_test

import (
	"fmt"
	"testing"
)

// Members see which file a title has, not where it lies on the server.
func TestFilePathsAreNamesOnlyForMembers(t *testing.T) {
	server, base, admin, member, _ := familyServer(t)
	file := server.TestMoviesRoot() + "/Heat (1995)/Heat (1995).mkv"
	putFile(t, file, "12345")
	id := seedMovie(t, server.MovieRepo, 949, "Heat", 1995, file)
	url := fmt.Sprintf("%s/api/movies/%d", base, id)

	if got := getJSON[map[string]any](t, admin, url)["filePath"]; got != file {
		t.Fatalf("administrator sees %v, want the full path", got)
	}
	if got := getJSON[map[string]any](t, member, url)["filePath"]; got != "Heat (1995).mkv" {
		t.Fatalf("member sees %v, want the file name only", got)
	}
	list := getJSON[[]map[string]any](t, member, base+"/api/movies")
	if len(list) != 1 || list[0]["filePath"] != "Heat (1995).mkv" {
		t.Fatalf("member's list: %v", list)
	}
}
