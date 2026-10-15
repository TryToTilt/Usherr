package tv

// Live channels: free streams (Pluto, PBS, …) whose guide comes from an
// XMLTV feed such as i.mjh.nz. The guide is read for the channels we carry
// and the holes are filled with plain "on air" slots, so every channel has
// something in the guide around the clock.

import (
	"encoding/xml"
	"io"
	"sort"
	"time"
)

// GuideProgram is one programme read from an XMLTV feed.
type GuideProgram struct {
	Start, End time.Time
	Title      string
	Subtitle   string
}

func parseXMLTVTime(s string) (time.Time, bool) {
	for _, layout := range []string{"20060102150405 -0700", "20060102150405"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ReadXMLTV reads the programmes for the wanted channel ids that overlap
// [from, to), streaming, so big feeds don't sit in memory.
func ReadXMLTV(r io.Reader, want map[string]bool, from, to time.Time) (map[string][]GuideProgram, error) {
	out := map[string][]GuideProgram{}
	dec := xml.NewDecoder(r)
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "programme" {
			continue
		}
		var ch, start, stop string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "channel":
				ch = a.Value
			case "start":
				start = a.Value
			case "stop":
				stop = a.Value
			}
		}
		if !want[ch] {
			if err := dec.Skip(); err != nil {
				return out, err
			}
			continue
		}
		var p struct {
			Title    string `xml:"title"`
			SubTitle string `xml:"sub-title"`
		}
		if err := dec.DecodeElement(&p, &se); err != nil {
			return out, err
		}
		s, ok1 := parseXMLTVTime(start)
		e, ok2 := parseXMLTVTime(stop)
		if !ok1 || !ok2 || !e.After(s) || !e.After(from) || !s.Before(to) {
			continue
		}
		out[ch] = append(out[ch], GuideProgram{Start: s, End: e, Title: p.Title, Subtitle: p.SubTitle})
	}
	return out, nil
}

// LiveSchedule turns a live channel's guide into its programs for
// [from, to): the guide's programmes, in order and without overlaps, and
// hour-long "on air" slots wherever the guide has nothing.
func LiveSchedule(ch Channel, guide []GuideProgram, from, to time.Time) []Program {
	sort.Slice(guide, func(i, j int) bool { return guide[i].Start.Before(guide[j].Start) })
	var out []Program
	cursor := from.Truncate(time.Hour)
	fill := func(until time.Time) {
		for cursor.Before(until) {
			next := cursor.Truncate(time.Hour).Add(time.Hour)
			if next.After(until) {
				next = until
			}
			out = append(out, Program{ChannelID: ch.ID, Start: cursor, End: next, Kind: "live", Title: ch.Name, Subtitle: "Live"})
			cursor = next
		}
	}
	for _, g := range guide {
		if !g.End.After(cursor) {
			continue
		}
		start := g.Start
		if start.Before(cursor) {
			start = cursor
		}
		if start.After(cursor) {
			fill(start)
		}
		title := g.Title
		if title == "" {
			title = ch.Name
		}
		out = append(out, Program{ChannelID: ch.ID, Start: start, End: g.End, Kind: "live", Title: title, Subtitle: g.Subtitle})
		cursor = g.End
		if !cursor.Before(to) {
			break
		}
	}
	fill(to)
	return out
}
