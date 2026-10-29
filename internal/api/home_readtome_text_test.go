package api

import (
	"strings"
	"testing"
)

func TestSpeakable(t *testing.T) {
	for in, want := range map[string]string{
		"Dr. Watson met Mr. Holmes on St. James Street.": "Doctor Watson met Mister Holmes on Saint James Street.",
		"In 1999 she was 21, and in 1905 the 3rd of 12.": "In nineteen ninety-nine she was twenty-one, and in nineteen oh five the third of twelve.",
		"It cost $5 and $1,250, about 40% more.":         "It cost five dollars and one thousand two hundred fifty dollars, about forty percent more.",
		"Back in the 1990s, at 10:05 and 7:00.":          "Back in the nineteen nineties, at ten oh five and seven o'clock.",
		"In 2005 and 2024, pages 10–12.":                 "In two thousand five and twenty twenty-four, pages ten to twelve.",
		"She waited—then ran.":                           "She waited, then ran.",
		`"Wait—" he said... and stopped…`:                `"Wait…" he said… and stopped…`,
		"A note[12] here* and B12 and 3D and 2.5 stay.":  "A note here and B12 and 3D and 2.5 stay.",
		"Apples, pears, etc. Then e.g. this, i.e. that.": "Apples, pears, et cetera. Then for example, this, that is, that.",
		"The 21st, the 100th, the 42nd.":                 "The twenty-first, the one hundredth, the forty-second.",
		"Born in 1,000,000 BC.":                          "Born in 1,000,000 BC.",
	} {
		if got := speakable(in, nil); got != want {
			t.Errorf("speakable(%q)\n got  %q\n want %q", in, got, want)
		}
	}
	// Paragraphs stay paragraphs; a scene break becomes the mark.
	if got := speakable("One.\n\n* * *\n\nTwo.", nil); got != "One.\n\n"+sceneMark+"\n\nTwo." {
		t.Errorf("scene: %q", got)
	}
}

func TestSayRules(t *testing.T) {
	rules := []sayRule{{Word: "Hermione", Say: "her-MY-oh-nee"}, {Word: "Smaug", Say: "Smowg"}}
	got := speakable("Hermione and HERMIONE saw Smaug; Smaugs and Hermiones stay.", rules)
	if got != "her-MY-oh-nee and her-MY-oh-nee saw Smowg; Smaugs and Hermiones stay." {
		t.Errorf("say: %q", got)
	}
	if got := applySay("Smaug Smaug", rules); got != "Smowg Smowg" {
		t.Errorf("side by side: %q", got)
	}
	if got := applySay("cost $5", []sayRule{{Word: "cost", Say: "$1 price"}}); got != "$1 price $5" {
		t.Errorf("a dollar in the say: %q", got)
	}
}

func TestSpeakableTitle(t *testing.T) {
	for in, want := range map[string]string{"Chapter IV": "Chapter four", "PART XII: The End": "PART twelve: The End", "IX": "nine", "Chapter Lion": "Chapter Lion", "Chapter 3": "Chapter three", "MIX": "MIX"} {
		if got := speakableTitle(in, nil); got != want {
			t.Errorf("speakableTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSceneBreakPauses(t *testing.T) {
	ps := splitSpeech("First scene ends.\n\n"+sceneMark+"\n\nSecond begins.\n\nMore.", 200)
	if len(ps) != 3 || !ps[0].SceneEnd || ps[1].SceneEnd || strings.Contains(ps[1].Text, sceneMark) {
		t.Fatalf("pieces: %+v", ps)
	}
	text, _, _ := epubText([]byte(`<html><body><p>One.</p><hr/><p>Two.</p><p>***</p><p>Three.</p></body></html>`))
	ps = splitSpeech(speakable(text, nil), 200)
	if len(ps) != 3 || !ps[0].SceneEnd || !ps[1].SceneEnd {
		t.Fatalf("from the page: %q %+v", text, ps)
	}
}

func TestChapterIndexes(t *testing.T) {
	if got := (rtvOptions{}).chapterIndexes(3); len(got) != 3 || got[2] != 2 {
		t.Errorf("all: %v", got)
	}
	if got := (rtvOptions{Chapters: []int{4, 2, 0, 2, -1}}).chapterIndexes(3); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Errorf("picked: %v", got)
	}
	if got := (rtvOptions{Chapters: []int{9}}).chapterIndexes(3); len(got) != 3 {
		t.Errorf("none real: %v", got)
	}
}

func TestVoiceMixes(t *testing.T) {
	parts, ok := parseMix("af_heart(7)+bm_george(3)")
	if !ok || len(parts) != 2 || parts[0].Weight != 7 || parts[1].ID != "bm_george" {
		t.Fatalf("mix: %v %v", parts, ok)
	}
	if p, ok := parseMix("af_heart+bm_george"); !ok || p[1].Weight != 1 {
		t.Fatalf("plain mix: %v", p)
	}
	for _, bad := range []string{"af_heart", "af_heart+af_heart", "a+b+c+d", "af_heart(0)+bm_george", "af_heart(10)+bm_george", "../x+bm_george", "af_heart+"} {
		if _, ok := parseMix(bad); ok {
			t.Errorf("%q passed as a mix", bad)
		}
	}
	vs := []rtvVoice{{ID: "af_heart", Label: "Heart · American · woman"}, {ID: "bm_george", Label: "George · British · man"}}
	if got := voiceName(vs, "af_heart(7)+bm_george(3)"); got != "Heart 70% + George 30%" {
		t.Errorf("mix name: %q", got)
	}
	if got := voiceName(vs, "bm_george"); got != "George" {
		t.Errorf("name: %q", got)
	}
	if o := (rtvOptions{ParaMs: 50, SceneMs: 99999, ChapterMs: 0}).clamped(); o.ParaMs != 200 || o.SceneMs != 4000 || o.ChapterMs != 0 {
		t.Errorf("clamped: %+v", o)
	}
}
