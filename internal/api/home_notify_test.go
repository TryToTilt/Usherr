package api

import "testing"

func TestSceneSilent(t *testing.T) {
	for link, want := range map[string]bool{
		"/scenes": true, "/scenes/12": true, "/scenes?tab=find": true,
		"/series/4": false, "/title/9": false, "/comics/3": false, "": false, "/scenesque": false,
	} {
		if got := sceneSilent(link); got != want {
			t.Errorf("sceneSilent(%q) = %v, want %v", link, got, want)
		}
	}
}
