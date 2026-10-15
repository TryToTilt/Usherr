package tv

import (
	"strings"
	"testing"
	"time"
)

const sampleGuide = `<?xml version="1.0" encoding="UTF-8"?>
<tv>
 <channel id="abc"><display-name>ABC</display-name></channel>
 <programme start="20261009150000 +0000" stop="20261009160000 +0000" channel="abc"><title>News at Three</title><sub-title>Today</sub-title></programme>
 <programme start="20261009170000 +0000" stop="20261009173000 +0000" channel="abc"><title>Weather</title></programme>
 <programme start="20261009150000 +0000" stop="20261009160000 +0000" channel="other"><title>Not ours</title></programme>
</tv>`

func TestReadXMLTVAndLiveSchedule(t *testing.T) {
	from := time.Date(2026, 10, 9, 14, 30, 0, 0, time.UTC)
	to := time.Date(2026, 10, 9, 19, 0, 0, 0, time.UTC)
	g, err := ReadXMLTV(strings.NewReader(sampleGuide), map[string]bool{"abc": true}, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(g["abc"]) != 2 || len(g["other"]) != 0 {
		t.Fatalf("guide = %+v", g)
	}
	ch := Channel{ID: 3, Name: "ABC News"}
	list := LiveSchedule(ch, g["abc"], from, to)
	// 14:00-15:00 filler, news 15-16, filler 16-17, weather 17-17:30,
	// filler 17:30-18, 18-19.
	if len(list) != 6 {
		for _, p := range list {
			t.Log(p.Start.Format("15:04"), p.End.Format("15:04"), p.Title)
		}
		t.Fatalf("got %d programs", len(list))
	}
	if list[1].Title != "News at Three" || list[1].Subtitle != "Today" || list[0].Title != "ABC News" {
		t.Errorf("unexpected: %+v", list[:2])
	}
	for i := 1; i < len(list); i++ {
		if !list[i].Start.Equal(list[i-1].End) {
			t.Fatalf("gap at %d", i)
		}
	}
	if !list[len(list)-1].End.Equal(to) {
		t.Error("schedule should reach the end")
	}
}
