package api

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/tv"
)

func TestTVM3UAndXMLTV(t *testing.T) {
	chans := []tv.Channel{{ID: 1, Number: 1, Name: "Saturday Cartoons", Enabled: true}, {ID: 2, Number: 2, Name: "Off", Enabled: false}, {ID: 3, Number: 100, Name: "PBS", Kind: "live", Enabled: true}}
	m3u := tvM3U(chans, "http://localhost:8264", "k")
	if !strings.Contains(m3u, `tvg-id="mediarium.1"`) || !strings.Contains(m3u, "http://localhost:8264/api/tvfeed/k/stream/1.ts") || strings.Contains(m3u, "Off") ||
		!strings.Contains(m3u, "http://localhost:8264/api/tvlive/k/100/index.m3u8") {
		t.Errorf("m3u:\n%s", m3u)
	}
	start := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	progs := map[int64][]tv.Program{1: {{ChannelID: 1, Start: start, End: start.Add(26 * time.Minute), Kind: "episode", Title: "The Jetsons", Subtitle: "S01E01 · Rosey the Robot"}}}
	b, err := xml.Marshal(tvXMLTV(chans, progs))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`<channel id="mediarium.1">`, `start="20261009120000 +0000"`, `<title>The Jetsons</title>`, `<sub-title>Rosey the Robot</sub-title>`, `<episode-num system="onscreen">S01E01</episode-num>`} {
		if !strings.Contains(s, want) {
			t.Errorf("xmltv missing %s:\n%s", want, s)
		}
	}
}

func TestTVFFmpegArgs(t *testing.T) {
	args := strings.Join(tvFFmpegArgs("/data/tv/a.mkv", 90*time.Second, 10*time.Minute, 3600*time.Second, 10*time.Second, 1), " ")
	for _, want := range []string{"-noaccurate_seek -ss 90.000", "-map 0:a:1?", "-fflags +genpts -readrate 1 -readrate_initial_burst 10.000 -i /data/tv/a.mkv", "-t 600.000", "-c:v copy", "-c:a aac", "-output_ts_offset 3600.000", "-f mpegts pipe:1"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
	if strings.Contains(strings.Join(tvFFmpegArgs("/x", 0, time.Minute, 0, 0, 0), " "), "-ss") {
		t.Error("no seek at the very start")
	}
}

func TestPickAudioTrack(t *testing.T) {
	if got := pickAudioTrack([]string{"fre", "eng"}); got != 1 {
		t.Errorf("fre,eng -> %d", got)
	}
	if got := pickAudioTrack([]string{"jpn", "ger"}); got != 0 {
		t.Errorf("no English -> %d", got)
	}
}

func TestEpisodeLineAndQuality(t *testing.T) {
	if got := episodeLine("S01E34 · The Dump"); got != "S1E34 - The Dump" {
		t.Errorf("episodeLine = %q", got)
	}
	for in, want := range map[string]string{"WEBDL-2160p": "4K", "Bluray-1080p": "HD", "DVD": "SD", "": ""} {
		if got := qualityBadge(in); got != want {
			t.Errorf("qualityBadge(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoredLogo(t *testing.T) {
	if got := storedLogo("/api/tv/channels/4/logo?v=12", ""); got != tvLogoUploaded {
		t.Errorf("app address -> %q", got)
	}
	if got := storedLogo("https://image.tmdb.org/t/p/w300/x.png", tvLogoUploaded); got != "https://image.tmdb.org/t/p/w300/x.png" {
		t.Errorf("link -> %q", got)
	}
	if got := storedLogo("javascript:alert(1)", "https://a/b.png"); got != "https://a/b.png" {
		t.Errorf("odd address kept as %q", got)
	}
}

func TestStreamedPointerPath(t *testing.T) {
	if got := streamedPointerPath("movie", false, "Amadeus", 1984, 279, 0, 0); got != "Movies/Amadeus (1984) [tmdbid-279]/Amadeus (1984).strm" {
		t.Errorf("movie: %s", got)
	}
	if got := streamedPointerPath("series", true, "Solo Leveling: Arise?", 2024, 127532, 1, 3); got != "Anime/Solo Leveling Arise (2024) [tmdbid-127532]/Season 01/Solo Leveling Arise S01E03.strm" {
		t.Errorf("anime: %s", got)
	}
	if got := streamedPointerPath("series", false, "Born to be Wild (2025)", 2025, 230936, 1, 2); got != "TV/Born to be Wild (2025) [tmdbid-230936]/Season 01/Born to be Wild (2025) S01E02.strm" {
		t.Errorf("year in title: %s", got)
	}
}
