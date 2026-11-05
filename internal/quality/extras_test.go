package quality_test

import (
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/parser"
	"github.com/rdborg/mediarium/internal/quality"
)

// Usherr: the editor's Advanced options (extras.go).

func hd() quality.Profile {
	return quality.Profile{Name: "HD", Allowed: []quality.Tier{quality.TierWebDL1080p, quality.TierBluray1080p, quality.TierRemux2160p}, Cutoff: quality.TierBluray1080p, UpgradeAllowed: true}
}

const gb = int64(1) << 30

func TestExtrasStoredAndReadBack(t *testing.T) {
	r := newRepo(t)
	p := hd()
	p.Preferred = []quality.Preferred{{Term: `/\b(AMZN|NF)\b/`, Score: 20}, {Term: "Atmos", Score: 5}}
	p.MustNotContain = []string{`/\bKOR(SUB)?\b/`}
	p.Extras = quality.Extras{
		Kind:          "movies",
		TierSizes:     map[quality.Tier]quality.TierSize{quality.TierRemux2160p: {MinGB: 40, PreferredGB: 70, MaxGB: 150}},
		PreferSmaller: true, PreferHEVC: true, MinUpgradeGain: 100, UpgradeDays: 90,
		PreferGroups: []string{"FraMeSToR", " HONE "}, NeverGroups: []string{"YIFY", "yify"},
		AudioLanguage: "Japanese,English", WantSubs: true, Indexers: []int64{3, 3, 0, 7},
		Protocol: quality.ProtocolUsenet, MinAgeMinutes: 30, MaxAgeDays: 3000,
	}
	saved, err := r.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	e := got.Extras
	if e.Kind != "movies" || e.TierSizes[quality.TierRemux2160p].MaxGB != 150 || !e.PreferSmaller || !e.PreferHEVC || e.MinUpgradeGain != 100 || e.UpgradeDays != 90 ||
		strings.Join(e.PreferGroups, ",") != "FraMeSToR,HONE" || strings.Join(e.NeverGroups, ",") != "YIFY" || e.AudioLanguage != "Japanese,English" || !e.WantSubs ||
		len(e.Indexers) != 2 || e.Protocol != "usenet" || e.MinAgeMinutes != 30 || e.MaxAgeDays != 3000 {
		t.Errorf("read back %+v", e)
	}
	if len(got.Preferred) != 2 || got.Preferred[0].Term != `/\b(AMZN|NF)\b/` || got.Preferred[0].Score != 20 {
		t.Errorf("a pattern with | in it read back as %+v", got.Preferred)
	}
	// An old profile, saved before the extras, has none.
	plain, err := r.Create(quality.Profile{Name: "Plain", Allowed: []quality.Tier{quality.TierWebDL1080p}, Cutoff: quality.TierWebDL1080p})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Get(plain.ID); got.Extras.Kind != "" || got.Extras.TierSizes != nil || got.Extras.Protocol != "" {
		t.Errorf("a plain profile has extras %+v", got.Extras)
	}
}

func TestExtrasRefused(t *testing.T) {
	cases := map[string]func(p *quality.Profile){
		"bad pattern": func(p *quality.Profile) { p.MustNotContain = []string{"/(unclosed/"} },
		"min above max": func(p *quality.Profile) {
			p.TierSizes = map[quality.Tier]quality.TierSize{quality.TierBluray1080p: {MinGB: 20, MaxGB: 10}}
		},
		"preferred outside": func(p *quality.Profile) {
			p.TierSizes = map[quality.Tier]quality.TierSize{quality.TierBluray1080p: {MinGB: 5, PreferredGB: 30, MaxGB: 10}}
		},
		"made-up tier size":     func(p *quality.Profile) { p.TierSizes = map[quality.Tier]quality.TierSize{"Bluray-8K": {MaxGB: 10}} },
		"made-up protocol":      func(p *quality.Profile) { p.Protocol = "ftp" },
		"made-up kind":          func(p *quality.Profile) { p.Kind = "books" },
		"negative upgrade gain": func(p *quality.Profile) { p.MinUpgradeGain = -1 },
	}
	for name, mutate := range cases {
		p := hd()
		mutate(&p)
		if _, err := quality.Validate(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A big Remux size is fine.
	p := hd()
	p.TierSizes = map[quality.Tier]quality.TierSize{quality.TierRemux2160p: {MaxGB: 120}}
	p.MaxSizeGB = 150
	if _, err := quality.Validate(p); err != nil {
		t.Errorf("a 120 GB Remux limit: %v", err)
	}
}

func TestExtrasJudgeReleases(t *testing.T) {
	p := hd()
	p.MustNotContain = []string{`/\bKOR(SUB)?\b/`}
	p.Preferred = []quality.Preferred{{Term: `/\b(AMZN|NF)\b/`, Score: 20}}
	p.NeverGroups = []string{"yify"}
	p.PreferGroups = []string{"framestor"}

	if ok, why := p.TitleAllowed("Movie.2024.1080p.WEBRip.x264-YIFY"); ok || !strings.Contains(why, "never takes") {
		t.Errorf("a never group: %v %q", ok, why)
	}
	if ok, _ := p.TitleAllowed("Movie.2024.1080p.WEB-DL.KORSUB.x264-GRP"); ok {
		t.Error("a pattern excluded term let through")
	}
	if ok, _ := p.TitleAllowed("Movie.2024.1080p.WEB-DL.KORN.x264-GRP"); !ok {
		t.Error("the pattern matched a longer word")
	}
	if s := p.Score("Movie.2024.1080p.NF.WEB-DL.x264-GRP"); s != 20 {
		t.Errorf("pattern preferred term scored %d", s)
	}
	title := "Movie.2024.1080p.BluRay.x264-FraMeSToR"
	if !p.PreferredGroup(title, parser.Parse(title)) {
		t.Error("the preferred group wasn't seen")
	}

	p.TierSizes = map[quality.Tier]quality.TierSize{quality.TierRemux2160p: {MinGB: 40, PreferredGB: 70, MaxGB: 120}}
	if why := p.TierSizeReason(quality.TierRemux2160p, 100*gb, 0); why != "" {
		t.Errorf("a 100 GB Remux refused: %s", why)
	}
	if why := p.TierSizeReason(quality.TierRemux2160p, 130*gb, 0); why == "" {
		t.Error("a 130 GB Remux let through")
	}
	if why := p.TierSizeReason(quality.TierRemux2160p, 20*gb, 0); why == "" {
		t.Error("a 20 GB 'Remux' let through")
	}
	if why := p.TierSizeReason(quality.TierBluray1080p, 300*gb, 0); why != "" {
		t.Errorf("a quality with no range of its own was limited: %s", why)
	}
	// Per episode for a show: 10 episodes of 5 GB fit a 40-120 GB range? No: 5 GB each is too small.
	if why := p.TierSizeReason(quality.TierRemux2160p, 50*gb, 10); why == "" {
		t.Error("the size wasn't counted per episode")
	}
	if why := p.TierSizeReason(quality.TierRemux2160p, 50*gb, quality.SeasonPack); why != "" {
		t.Errorf("a pack of unknown length was judged: %s", why)
	}
	if near, far := p.SizePreference(quality.TierRemux2160p, 72*gb), p.SizePreference(quality.TierRemux2160p, 110*gb); near <= far {
		t.Errorf("the size nearer 70 GB didn't rank higher: %d vs %d", near, far)
	}
	p.PreferSmaller = true
	if small, big := p.SizePreference(quality.TierBluray1080p, 8*gb), p.SizePreference(quality.TierBluray1080p, 20*gb); small <= big {
		t.Errorf("prefer smaller: %d vs %d", small, big)
	}
	if !quality.HEVC("Movie.2024.1080p.WEB-DL.H.265-GRP") || quality.HEVC("Movie.2024.1080p.WEB-DL.x264-GRP") {
		t.Error("HEVC detection")
	}
	if !quality.HasSubs("Movie.2024.1080p.WEB-DL.ENG.SUBS.x264-GRP") || quality.HasSubs("Movie.2024.1080p.WEB-DL.x264-GRP") {
		t.Error("subtitle detection")
	}
}

func TestExtrasUpgradeLimits(t *testing.T) {
	p := hd()
	p.UpgradeDays = 30
	if !p.WithinUpgradeWindow("2026-10-20", "2026-11-04") || p.WithinUpgradeWindow("2026-09-01", "2026-11-04") {
		t.Error("upgrade window")
	}
	if !p.WithinUpgradeWindow("", "2026-11-04") {
		t.Error("an unknown release date stopped upgrades")
	}
	p.UpgradeDays = 0
	if !p.WithinUpgradeWindow("1990-01-01", "2026-11-04") {
		t.Error("no window stopped upgrades")
	}
}
