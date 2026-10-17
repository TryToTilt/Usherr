package api

// Usherr: the programs between programs (internal/tv/blocks.go). "filler" is
// a bumper or an ad from the channel's filler folder; "card" is an "Up next"
// card. In the browser the card is drawn by the page; in the tuner stream
// (Jellyfin, IPTV apps) ffmpeg draws it.

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/tv"
)

// isBetween says whether a program is filler or a card, not a show.
func isBetween(p tv.Program) bool { return p.Kind == "card" || p.Kind == "filler" }

// shows drops filler and cards: the guide and the feeds list shows only. A
// show split by commercial breaks is one entry again, from its first part's
// start to its last part's end.
func shows(list []tv.Program) []tv.Program {
	out := list[:0:0]
	for _, p := range list {
		if isBetween(p) {
			continue
		}
		if n := len(out); n > 0 && p.Offset > 0 && out[n-1].FilePath == p.FilePath && out[n-1].ItemID == p.ItemID {
			out[n-1].End = p.End
			continue
		}
		out = append(out, p)
	}
	return out
}

// isSplitPart says whether p is a part of a show that breaks split: it
// starts inside the file, or another part of the same file follows.
func (s *Server) isSplitPart(channelID int64, p tv.Program) bool {
	if p.Offset > 0 {
		return true
	}
	list, err := s.tvRepo().Programs(channelID, p.End, p.End.Add(15*time.Minute))
	if err != nil {
		return false
	}
	for _, q := range list {
		if q.FilePath == p.FilePath && q.Offset > 0 {
			return true
		}
	}
	return false
}

// upNext is the first show after t on a channel.
func (s *Server) upNext(channelID int64, t time.Time) (tv.Program, bool) {
	list, err := s.tvRepo().Programs(channelID, t, t.Add(12*time.Hour))
	if err != nil {
		return tv.Program{}, false
	}
	for _, p := range list {
		if !isBetween(p) && !p.Start.Before(t) {
			return p, true
		}
	}
	return tv.Program{}, false
}

// GET /api/tv/channels/{id}/filler/{start}: the filler video on at start (ms).
func (s *Server) handleTVFiller(w http.ResponseWriter, r *http.Request) {
	id, err1 := strconv.ParseInt(r.PathValue("id"), 10, 64)
	start, err2 := strconv.ParseInt(r.PathValue("start"), 10, 64)
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	p, ok, err := s.tvRepo().At(id, time.UnixMilli(start))
	if err != nil || !ok || p.Kind != "filler" || p.Start.UnixMilli() != start {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, p.FilePath)
}

// tvFont is a font ffmpeg can draw cards with, or "" when there is none.
func tvFont(bold bool) string {
	names := []string{"DejaVuSans.ttf"}
	if bold {
		names = []string{"DejaVuSans-Bold.ttf", "DejaVuSans.ttf"}
	}
	for _, dir := range []string{"/usr/share/fonts/dejavu", "/usr/share/fonts/truetype/dejavu", "/usr/share/fonts/TTF"} {
		for _, n := range names {
			if p := filepath.Join(dir, n); fileExists(p) {
				return p
			}
		}
	}
	return ""
}

// tvCardArgs draws an "Up next" card for d: the channel's name, then the
// next show, its episode and when it starts. Without a font it is the plain
// off-air picture.
func tvCardArgs(channel string, next *tv.Program, d, tsOffset time.Duration) []string {
	font, bold := tvFont(false), tvFont(true)
	if font == "" {
		return tvSlateArgs(d, tsOffset)
	}
	dir, err := os.MkdirTemp("", "tvcard")
	if err != nil {
		return tvSlateArgs(d, tsOffset)
	}
	// The texts go through files, so nothing in a title needs escaping.
	text := func(name, s string) string {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte(s), 0o600)
		return strings.ReplaceAll(p, ":", `\:`)
	}
	draw := []string{
		"drawbox=x=0:y=0:w=iw:h=110:color=0x00000066:t=fill",
		"drawtext=fontfile=" + bold + ":textfile=" + text("ch", channel) + ":fontsize=40:fontcolor=0xf4e3b0:x=60:y=36",
	}
	if next != nil {
		sub := next.Subtitle
		at := next.Start.Local().Format("3:04 PM")
		draw = append(draw,
			"drawtext=fontfile="+bold+":textfile="+text("up", "UP NEXT")+":fontsize=44:fontcolor=0xe8b04b:x=60:y=250",
			"drawtext=fontfile="+bold+":textfile="+text("title", next.Title)+":fontsize=76:fontcolor=white:x=60:y=315",
			"drawtext=fontfile="+font+":textfile="+text("sub", sub)+":fontsize=38:fontcolor=0xd8d8d8:x=60:y=420",
			"drawtext=fontfile="+font+":textfile="+text("at", at)+":fontsize=40:fontcolor=0xe8b04b:x=60:y=490",
		)
	} else {
		draw = append(draw, "drawtext=fontfile="+bold+":textfile="+text("up", "We'll be right back")+":fontsize=64:fontcolor=white:x=(w-text_w)/2:y=(h-text_h)/2")
	}
	sec := func(x time.Duration) string { return strconv.FormatFloat(x.Seconds(), 'f', 3, 64) }
	go func() { time.Sleep(d + time.Minute); _ = os.RemoveAll(dir) }()
	return []string{"-hide_banner", "-loglevel", "error", "-nostdin",
		"-re", "-f", "lavfi", "-i", "color=c=0x14213d:s=1280x720:r=25",
		"-re", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo",
		"-t", sec(d),
		"-vf", strings.Join(draw, ","),
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "stillimage", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "64k",
		"-output_ts_offset", sec(tsOffset),
		"-muxdelay", "0", "-muxpreload", "0",
		"-f", "mpegts", "pipe:1",
	}
}
