package api

import "testing"

func TestSNZBIndexerURLAndKeys(t *testing.T) {
	if got := snzbIndexerURL("http://host:9696/", "1/api"); got != "http://host:9696/1/api" {
		t.Errorf("aggregator url = %q", got)
	}
	if got := snzbIndexerURL("https://api.nzbgeek.info", "/api"); got != "https://api.nzbgeek.info" {
		t.Errorf("direct url = %q", got)
	}
	if indexerKey("Prowlarr-NZBgeek") != indexerKey("NZBgeek (via Prowlarr)") || indexerKey("NZBgeek") != "nzbgeek" {
		t.Errorf("keys: %q %q", indexerKey("Prowlarr-NZBgeek"), indexerKey("NZBgeek (via Prowlarr)"))
	}
	if pathKey("http://a:9697/2/api") != pathKey("http://b:9697/2/api") || pathKey("http://a:9697/2/api") == pathKey("http://a:9697/1/api") {
		t.Error("path keys don't tell indexers on one aggregator apart")
	}
	if pathKey("https://api.example.com") != "" {
		t.Error("a bare address has no path key")
	}
}

func TestProfilesFromSNZB(t *testing.T) {
	c, err := loadSNZBConfig(`{"filter_profiles":[
	 {"name":"Phone 720p","preset":"720p","rules":[
	   {"when":"avail.status == \"unavailable\"","action":"reject"},
	   {"when":"sizeGB > 30 and resolution != \"2160p\"","action":"reject"},
	   {"when":"sizeGB > 12","action":"reject"},
	   {"when":"sizeGB > 5","action":"reject","enabled":false}]},
	 {"name":"Living Room 4K","preset":"4k","rules":[
	   {"when":"sizeGB > 30 and resolution != \"2160p\"","action":"reject"},
	   {"when":"sizeGB > 40","action":"reject"}]},
	 {"name":"Second 720p","preset":"720p","rules":[{"when":"sizeGB > 2","action":"reject"}]},
	 {"name":"No size","preset":"1080p","rules":[{"when":"avail.onMyBackbone","points":500}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	got := profilesFromSNZB(c)
	want := map[string]float64{"720p": 12, "4K": 40}
	if len(got) != len(want) {
		t.Fatalf("profiles: %+v", got)
	}
	for _, p := range got {
		if want[p.Quality] != p.MaxSizeGB {
			t.Errorf("%s: %g GB, want %g", p.Quality, p.MaxSizeGB, want[p.Quality])
		}
	}
}

func TestLoadSNZBConfigRefusesWhatIsNotAConfig(t *testing.T) {
	for _, in := range []string{`not json`, `{}`, `{"indexers":[],"providers":[]}`} {
		if _, err := loadSNZBConfig(in); err == nil {
			t.Errorf("%q was accepted", in)
		}
	}
}
