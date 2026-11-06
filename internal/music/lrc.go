package music

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// bom is the byte order mark some editors put first.
const bom = "\xef\xbb\xbf"

// LRCLine is one line of synced lyrics: when it starts (milliseconds) and
// its words ("" for a pause).
type LRCLine struct {
	StartMs int
	Text    string
}

var (
	lrcTime   = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	lrcOffset = regexp.MustCompile(`(?i)^\[offset:\s*([+-]?\d+)\s*\]`)
	lrcWord   = regexp.MustCompile(`<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>`) // enhanced LRC word stamps
)

// ParseLRC reads LRC text: one or more [mm:ss.xx] stamps before each line's
// words, an optional [offset:+/-ms] (positive makes the lines come sooner),
// and tags such as [ar:…] that are skipped. Lines come back in time order.
// The same parsing rules as the web player (web/src/lrc.ts).
func ParseLRC(text string) []LRCLine {
	offset := 0
	var out []LRCLine
	for _, raw := range strings.Split(strings.ReplaceAll(strings.TrimPrefix(text, bom), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if m := lrcOffset.FindStringSubmatch(line); m != nil {
			offset, _ = strconv.Atoi(m[1])
			continue
		}
		var starts []int
		for {
			m := lrcTime.FindStringSubmatch(line)
			if m == nil {
				break
			}
			min, _ := strconv.Atoi(m[1])
			sec, _ := strconv.Atoi(m[2])
			ms := 0
			if frac := m[3]; frac != "" {
				n, _ := strconv.Atoi(frac)
				switch len(frac) {
				case 1:
					ms = n * 100
				case 2:
					ms = n * 10
				default:
					ms = n
				}
			}
			starts = append(starts, (min*60+sec)*1000+ms)
			line = strings.TrimSpace(line[len(m[0]):])
		}
		if len(starts) == 0 {
			continue
		}
		words := strings.TrimSpace(lrcWord.ReplaceAllString(line, ""))
		for _, st := range starts {
			out = append(out, LRCLine{StartMs: st, Text: words})
		}
	}
	for i := range out {
		if out[i].StartMs -= offset; out[i].StartMs < 0 {
			out[i].StartMs = 0
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartMs < out[j].StartMs })
	return out
}
