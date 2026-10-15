package api

import "testing"

// Upstream's update tests check Update now itself; the fork's switch is
// tested on its own below.
func init() { homeForkBuild = false }

func TestHomeBuildNeverInstallsUpstream(t *testing.T) {
	homeForkBuild = true
	defer func() { homeForkBuild = false }()
	s := &Server{}
	s.cfg.ImageVersion = "3.0.0"
	if s.canInstallUpdates() {
		t.Fatal("the home build must not install upstream releases by itself")
	}
}
