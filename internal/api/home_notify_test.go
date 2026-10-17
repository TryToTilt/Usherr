package api

import "testing"

func TestVideoSilent(t *testing.T) {
	for link, want := range map[string]bool{
		"/nsfw": true, "/nsfw/12": true, "/nsfw?tab=find": true,
		"/series/4": false, "/title/9": false, "/comics/3": false, "": false, "/nsfwque": false,
	} {
		if got := videoSilent(link); got != want {
			t.Errorf("videoSilent(%q) = %v, want %v", link, got, want)
		}
	}
}
