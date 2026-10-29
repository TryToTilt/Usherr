package api

import (
	"strings"
	"testing"
)

func segsText(ss []quoteSeg) string {
	var out []string
	for _, s := range ss {
		if s.Quote {
			out = append(out, "Q:"+s.Text)
		} else {
			out = append(out, "N:"+s.Text)
		}
	}
	return strings.Join(out, " | ")
}

func TestQuoteSegments(t *testing.T) {
	for in, want := range map[string]string{
		`"Run," Tom said. "Now!"`:                          `Q:Run, | N:Tom said. | Q:Now!`,
		`“Run,” Tom said, “now.”`:                          `Q:Run, | N:Tom said, | Q:now.`,
		`‘I don’t know,’ said Jane. ‘Do you?’`:             `Q:I don’t know, | N:said Jane. | Q:Do you?`,
		`She opened the door. Nothing.`:                    `N:She opened the door. Nothing.`,
		`“It goes on`:                                      `Q:It goes on`,
		`The boys’ toys were ‘gone’ by then, she thought.`: `N:The boys’ toys were | Q:gone | N:by then, she thought.`,
	} {
		if got := segsText(quoteSegments(in)); got != want {
			t.Errorf("quoteSegments(%q)\n got  %s\n want %s", in, got, want)
		}
	}
}

func speakers(lines []castLine) string {
	var out []string
	for _, l := range lines {
		out = append(out, l.Speaker+"/"+l.How)
	}
	return strings.Join(out, " ")
}

func TestCastChapter(t *testing.T) {
	text := strings.Join([]string{
		`“We have to go,” said Tom.`,              // tag after: verb name
		`Mary shook her head. “Not yet.”`,         // beat
		`“Why not?”`,                              // nobody: Tom's turn
		`“Because,” she said, “it isn't time.”`,   // a woman, and her turn
		`The rain came down harder.`,              // narration breaks the exchange
		`Doctor Watson said quietly, “Sit down.”`, // tag before
		`“I won't,” I said.`,                      // a first-person narrator
		`“Fine,” Holmes replied. “Then stand.”`,   // one tag, both quotes
	}, "\n\n")
	paras, lines := castChapter(text)
	takeTurns(paras, lines)
	got := speakers(lines)
	want := "Tom/tag Mary/beat Tom/turn Mary/turn Mary/turn Doctor Watson/tag I/tag Holmes/tag Holmes/tag" // "she said", in an exchange: her turn
	if got != want {
		t.Fatalf("speakers\n got  %s\n want %s", got, want)
	}
	pieces := castPieces(paras, lines, 300)
	var who []string
	for _, p := range pieces {
		who = append(who, p.Speaker+":"+p.Text)
	}
	if !strings.Contains(strings.Join(who, "|"), "Tom:We have to go,|:said Tom.|:Mary shook her head.|Mary:Not yet.") {
		t.Fatalf("pieces: %v", who)
	}
}

func TestCastVoices(t *testing.T) {
	george, bella, emma := rtvVoiceRef{"kokoro", "bm_george"}, rtvVoiceRef{"kokoro", "af_bella"}, rtvVoiceRef{"kokoro", "bf_emma"}
	c := &rtvCast{Dialogue: &bella, Women: &emma, People: map[string]rtvVoiceRef{"Tom": george}}
	genders := map[string]string{"Mary": "f"}
	for speaker, want := range map[string]string{"": "", "Tom": "bm_george", "Mary": "bf_emma", "?f": "bf_emma", "?m": "af_bella", "?": "af_bella", "I": "", "Stranger": "af_bella"} {
		got := ""
		if r := c.voiceFor(speaker, genders); r != nil {
			got = r.Voice
		}
		if got != want {
			t.Errorf("voiceFor(%q) = %q, want %q", speaker, got, want)
		}
	}
	var none *rtvCast
	if none.voiceFor("Tom", nil) != nil {
		t.Error("no cast gave a voice")
	}
}
