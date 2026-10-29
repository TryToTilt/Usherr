package api

// Usherr: the pre-show's cards as short videos, for the players that can
// only play files (the Jellyfin app, through the NeXroll Intros plugin). The
// player and the Marquee draw the cards live; here ffmpeg draws them in the
// Marquee's colours: the welcome with the film's poster, the rating, silence
// your phones, the countdown and the rest. Each is made once and kept in the
// pre-show folder's .cards, named by what's on it.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rdborg/mediarium/internal/qr"
	"github.com/rdborg/mediarium/web"
)

const preshowCardLook = "3" // bump when the drawing changes, so the kept cards are made again

// What a card shows.
type preshowCardSpec struct {
	Card     string            `json:"card"`
	Title    string            `json:"title,omitempty"`
	Theater  string            `json:"theater,omitempty"`
	At       string            `json:"at,omitempty"` // "8:00 PM", a Showtime's
	Rated    string            `json:"rated,omitempty"`
	Poster   string            `json:"poster,omitempty"`
	Backdrop string            `json:"backdrop,omitempty"`
	Date     string            `json:"date,omitempty"` // soon: "In theaters November 21"
	QR       string            `json:"qr,omitempty"`   // qr: the address
	Reel     []preshowReelItem `json:"reel,omitempty"`
	Theme    string            `json:"theme"`
	Font     string            `json:"font"`
	Seconds  float64           `json:"seconds"`
}

func (c preshowCardSpec) hash() string {
	b, _ := json.Marshal(c)
	h := sha256.Sum256(append([]byte(preshowCardLook+"|"), b...))
	return hex.EncodeToString(h[:])[:20]
}

// The Marquee's themes (marqueeLook.css): background, text, accent, highlight.
var preshowCardColours = map[string][4]string{
	"usherr":   {"#100509", "#fff0d0", "#c99a42", "#f6d783"},
	"deco":     {"#0b0a08", "#f3e9d2", "#c9a24a", "#ecc96a"},
	"noir":     {"#050505", "#f2f2f2", "#8d8d8d", "#ffffff"},
	"neon":     {"#07040f", "#f5ecff", "#ff2fb3", "#39f3ff"},
	"ivory":    {"#f3ead8", "#2a1d12", "#9c6b2f", "#7a1f1f"},
	"midnight": {"#060b1a", "#e6ecf8", "#8fa8d8", "#d8e4ff"},
	"drivein":  {"#0b1a1c", "#fff4e0", "#ff7a3d", "#ffd166"},
	"velvet":   {"#12030a", "#ffe9ec", "#d1263f", "#ffb4c0"},
}

// The Marquee's serif fonts; the rest are drawn in a sans.
var preshowSerifFonts = map[string]bool{"classic": true, "deco": true, "roman": true, "bold": true, "elegant": true}

// preshowCardFor is a card as the Marquee looks now.
func (s *Server) preshowCardFor(st preshowStep, film preshowFilm) preshowCardSpec {
	card := st.Card
	m := s.marqueeSettings()
	c := preshowCardSpec{Card: card, Theme: m.Theme, Font: m.Font, Seconds: preshowCards[card]}
	if _, ok := preshowCardColours[c.Theme]; !ok {
		c.Theme = "usherr"
	}
	switch card {
	case "welcome":
		c.Theater, c.Title, c.Poster, c.Backdrop = s.preshowSettingsNow().Theater, film.Title, film.PosterURL, film.BackdropURL
		if film.AtMs > 0 {
			c.At = time.UnixMilli(film.AtMs).Format("3:04 PM")
		}
	case "rating":
		c.Title, c.Rated, c.Backdrop = film.Title, film.Rated, film.BackdropURL
	case "feature":
		c.Title, c.Backdrop = film.Title, film.BackdropURL
	case "soon":
		c.Title, c.Poster, c.Date = st.Title, st.PosterURL, soonLine(st.ReleaseDate, st.Why, st.Season, time.Now())
	case "announce":
		c.Title = st.Title
	case "qr":
		c.QR = st.Title
	case "reel":
		c.Reel = st.Reel
	}
	return c
}

// soonLine says when a coming title comes: "In theaters November 21",
// "Season 3 · November 14", "Now in theaters".
func soonLine(date, why string, season int, now time.Time) string {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "Coming soon"
	}
	day := d.Format("January 2")
	if d.Year() != now.Year() {
		day = d.Format("January 2, 2006")
	}
	out := !d.After(now)
	switch {
	case season > 0 && out:
		return fmt.Sprintf("Season %d · now on", season)
	case season > 0:
		return fmt.Sprintf("Season %d · %s", season, day)
	case why == "cinema" && out:
		return "Now in theaters"
	case why == "cinema":
		return "In theaters " + day
	case out:
		return "Out now"
	}
	return "Coming " + day
}

// ---- making them ----

var preshowCardMaking = struct {
	sync.Mutex
	m    map[string]chan struct{} // being made
	spec map[string]preshowCardSpec
}{m: map[string]chan struct{}{}, spec: map[string]preshowCardSpec{}}

func (s *Server) preshowCardsDir() string { return filepath.Join(s.preshowDir(), ".cards") }

// preshowCardRemember keeps what a card shows, so its link can make it.
func preshowCardRemember(c preshowCardSpec) string {
	h := c.hash()
	preshowCardMaking.Lock()
	if len(preshowCardMaking.spec) > 500 {
		preshowCardMaking.spec = map[string]preshowCardSpec{}
	}
	preshowCardMaking.spec[h] = c
	preshowCardMaking.Unlock()
	return h
}

// preshowCardFile is the card's video, made now if it isn't yet (one at a
// time; a card being made is waited for).
func (s *Server) preshowCardFile(ctx context.Context, h string) (string, error) {
	file := filepath.Join(s.preshowCardsDir(), h+".mp4")
	for {
		if _, err := os.Stat(file); err == nil {
			return file, nil
		}
		preshowCardMaking.Lock()
		spec, known := preshowCardMaking.spec[h]
		wait, busy := preshowCardMaking.m[h]
		if !busy && known {
			wait = make(chan struct{})
			preshowCardMaking.m[h] = wait
		}
		preshowCardMaking.Unlock()
		if !known {
			return "", errors.New("that card isn't known")
		}
		if busy {
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		err := s.preshowMakeCard(ctx, spec, file)
		preshowCardMaking.Lock()
		delete(preshowCardMaking.m, h)
		close(wait)
		preshowCardMaking.Unlock()
		if err != nil {
			return "", err
		}
		return file, nil
	}
}

var preshowCardOne sync.Mutex // one ffmpeg at a time

func (s *Server) preshowMakeCard(ctx context.Context, c preshowCardSpec, file string) error {
	preshowCardOne.Lock()
	defer preshowCardOne.Unlock()
	if _, err := os.Stat(file); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "usherr-card-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	args, err := s.preshowCardArgs(ctx, c, work)
	if err != nil {
		return err
	}
	tmp := file + ".part.mp4"
	args = append(args, tmp)
	start := time.Now()
	out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		slog.Warn("preshow: card not made", "card", c.Card, "err", err, "ffmpeg", lastLines(string(out), 3))
		return fmt.Errorf("the %s card couldn't be made", c.Card)
	}
	slog.Info("preshow: card made", "card", c.Card, "took", time.Since(start).Round(100*time.Millisecond))
	return os.Rename(tmp, file)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// The fonts: Usherr's own display font (from the web app), else DejaVu.
func (s *Server) preshowCardFonts(serif bool) (big, small string) {
	const dejavu = "/usr/share/fonts/dejavu/"
	small = dejavu + "DejaVuSans.ttf"
	if serif {
		return dejavu + "DejaVuSerif-Bold.ttf", small
	}
	big = dejavu + "DejaVuSans-Bold.ttf"
	own := filepath.Join(s.preshowCardsDir(), "fonts", "WixMadeforDisplay-Variable.ttf")
	if _, err := os.Stat(own); err == nil {
		return own, small
	}
	if b, err := fs.ReadFile(web.DistFS, web.DistDir+"/fonts/WixMadeforDisplay-Variable.ttf"); err == nil {
		if os.MkdirAll(filepath.Dir(own), 0o755) == nil && os.WriteFile(own, b, 0o644) == nil {
			return own, small
		}
	}
	return big, small
}

// preshowFetchImage saves a TMDB image for the card; others aren't fetched.
func preshowFetchImage(ctx context.Context, u, to string) bool {
	if !strings.HasPrefix(u, "https://image.tmdb.org/") {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	f, err := os.Create(to)
	if err != nil {
		return false
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 20<<20))
	return f.Close() == nil && err == nil
}

// fitSize is a font size that fits n characters in width pixels.
func fitSize(text string, width, most, least int) int {
	n := len([]rune(text))
	if n == 0 {
		return most
	}
	size := int(float64(width) / (0.62 * float64(n)))
	return max(least, min(most, size))
}

// preshowCardArgs is ffmpeg's command line for the card (without the output).
func (s *Server) preshowCardArgs(ctx context.Context, c preshowCardSpec, work string) ([]string, error) {
	col := preshowCardColours[c.Theme]
	bg, fg, accent, hi := strings.TrimPrefix(col[0], "#"), strings.TrimPrefix(col[1], "#"), strings.TrimPrefix(col[2], "#"), strings.TrimPrefix(col[3], "#")
	big, small := s.preshowCardFonts(preshowSerifFonts[c.Font] || c.Font == "")
	d := c.Seconds
	if d <= 0 {
		d = 7
	}
	dur := strconv.FormatFloat(d, 'f', 2, 64)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-f", "lavfi", "-i", "color=c=0x" + bg + ":s=1920x1080:r=30:d=" + dur,
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo"}
	inputs := 2
	var chain []string
	cur := "[0:v]"
	label := 0
	next := func() string {
		label++
		return "[v" + strconv.Itoa(label) + "]"
	}
	if c.Backdrop != "" && preshowFetchImage(ctx, c.Backdrop, filepath.Join(work, "backdrop.jpg")) {
		args = append(args, "-loop", "1", "-t", dur, "-i", filepath.Join(work, "backdrop.jpg"))
		in := "[" + strconv.Itoa(inputs) + ":v]"
		inputs++
		chain = append(chain, in+"scale=1920:1080:force_original_aspect_ratio=increase,crop=1920:1080,gblur=sigma=14,eq=brightness=-0.18:saturation=0.85,format=rgba,colorchannelmixer=aa=0.38[bd]")
		n := next()
		chain = append(chain, cur+"[bd]overlay=shortest=1"+n)
		cur = n
	}
	n := next()
	chain = append(chain, cur+"vignette=angle=PI/4.5"+n)
	cur = n

	textNo := 0
	// text draws a line: x "c" centres it; at is when it fades in.
	text := func(s, font, colour string, size int, x, y string, at float64) {
		if strings.TrimSpace(s) == "" {
			return
		}
		textNo++
		tf := filepath.Join(work, "t"+strconv.Itoa(textNo)+".txt")
		_ = os.WriteFile(tf, []byte(s), 0o644)
		if x == "c" {
			x = "(w-text_w)/2"
		}
		alpha := fmt.Sprintf("if(lt(t,%.2f),0,min(1,(t-%.2f)/0.7))", at, at)
		n := next()
		chain = append(chain, fmt.Sprintf("%sdrawtext=fontfile=%s:textfile=%s:fontsize=%d:fontcolor=0x%s:x=%s:y=%s:alpha='%s'", cur, font, tf, size, colour, x, y, alpha)+n)
		cur = n
	}
	rule := func(x, y, w, h int, at float64) {
		n := next()
		chain = append(chain, fmt.Sprintf("%sdrawbox=x=%d:y=%d:w=%d:h=%d:color=0x%s@0.9:t=fill:enable='gte(t,%.2f)'", cur, x, y, w, h, accent, at)+n)
		cur = n
	}
	upper := strings.ToUpper

	switch c.Card {
	case "welcome":
		left, width := "c", 1700
		if c.Poster != "" && preshowFetchImage(ctx, c.Poster, filepath.Join(work, "poster.jpg")) {
			args = append(args, "-loop", "1", "-t", dur, "-i", filepath.Join(work, "poster.jpg"))
			in := "[" + strconv.Itoa(inputs) + ":v]"
			inputs++
			chain = append(chain, in+"scale=-2:760,format=rgba,fade=t=in:st=0.2:d=0.9:alpha=1[ps]")
			n := next()
			chain = append(chain, cur+"[ps]overlay=x=230:y=160:shortest=1"+n)
			cur = n
			left, width = "820", 960
		}
		theater := c.Theater
		if theater == "" {
			theater = "the theater"
		}
		text(upper("Welcome to"), small, hi, 46, left, "330", 0.3)
		text(theater, big, fg, fitSize(theater, width, 130, 54), left, "400", 0.6)
		ruleX := 820
		if left == "c" {
			ruleX = 960 - 120
		}
		rule(ruleX, 590, 240, 6, 1.0)
		if c.Title != "" {
			line := "Tonight: " + c.Title
			if c.At != "" {
				line = "Tonight at " + c.At + ": " + c.Title
			}
			text(line, small, fg, fitSize(line, width, 56, 30), left, "640", 1.3)
		}
	case "rating":
		text(upper("The following feature is rated"), small, hi, 46, "c", "230", 0.2)
		n := next()
		chain = append(chain, fmt.Sprintf("%sdrawbox=x=560:y=330:w=800:h=330:color=0x%s:t=12:enable='gte(t,0.5)'", cur, hi)+n)
		cur = n
		text(c.Rated, big, hi, fitSize(c.Rated, 640, 170, 70), "c", "(330+(330-text_h)/2)", 0.5)
		text(c.Title, big, fg, fitSize(c.Title, 1600, 84, 40), "c", "740", 1.0)
	case "popcorn":
		text("Grab your popcorn", big, fg, 120, "c", "400", 0.2)
		rule(840, 560, 240, 6, 0.6)
		text("The show starts in a moment", small, hi, 52, "c", "610", 0.9)
	case "silence":
		text("Please silence your phones", big, fg, fitSize("Please silence your phones", 1700, 112, 60), "c", "400", 0.2)
		rule(840, 560, 240, 6, 0.6)
		text("and enjoy the show", small, hi, 52, "c", "610", 0.9)
	case "coming":
		rule(560, 380, 800, 4, 0.2)
		text("Coming attractions", big, hi, 128, "c", "440", 0.4)
		rule(560, 640, 800, 4, 0.2)
	case "countdown":
		n := next()
		chain = append(chain, fmt.Sprintf("%sdrawbox=x=0:y=538:w=1920:h=4:color=0x%s@0.5:t=fill,drawbox=x=958:y=0:w=4:h=1080:color=0x%s@0.5:t=fill,drawbox=x=560:y=140:w=800:h=800:color=0x%s@0.6:t=6", cur, accent, accent, fg)+n)
		cur = n
		n = next()
		chain = append(chain, fmt.Sprintf("%sdrawtext=fontfile=%s:text='%%{eif\\:max(1\\,5-floor(t))\\:d}':fontsize=520:fontcolor=0x%s:x=(w-text_w)/2:y=(h-text_h)/2:enable='lt(t,5)'", cur, big, hi)+n)
		cur = n
	case "soon":
		left, width := "c", 1700
		if c.Poster != "" && preshowFetchImage(ctx, c.Poster, filepath.Join(work, "poster.jpg")) {
			args = append(args, "-loop", "1", "-t", dur, "-i", filepath.Join(work, "poster.jpg"))
			in := "[" + strconv.Itoa(inputs) + ":v]"
			inputs++
			chain = append(chain, in+"scale=-2:760,format=rgba,fade=t=in:st=0.1:d=0.7:alpha=1[ps]")
			n := next()
			chain = append(chain, cur+"[ps]overlay=x=230:y=160:shortest=1"+n)
			cur = n
			left, width = "820", 960
		}
		text(upper("Coming soon"), small, hi, 46, left, "350", 0.2)
		text(c.Title, big, fg, fitSize(c.Title, width, 120, 50), left, "420", 0.5)
		ruleX := 820
		if left == "c" {
			ruleX = 960 - 120
		}
		rule(ruleX, 600, 240, 6, 0.8)
		text(c.Date, small, hi, fitSize(c.Date, width, 56, 30), left, "650", 1.0)
	case "announce":
		lines := wrapWords(c.Title, 26)
		size := 110
		if len(lines) > 2 {
			size = 84
		}
		top := 540 - len(lines)*size*6/10
		for i, l := range lines {
			text(l, big, fg, fitSize(l, 1700, size, 48), "c", strconv.Itoa(top+i*size*12/10), 0.2+0.25*float64(i))
		}
		rule(840, top+len(lines)*size*12/10+20, 240, 6, 0.8)
	case "qr":
		png := filepath.Join(work, "qr.png")
		if err := writeQRPNG(c.QR, png); err != nil {
			return nil, err
		}
		args = append(args, "-loop", "1", "-t", dur, "-i", png)
		in := "[" + strconv.Itoa(inputs) + ":v]"
		inputs++
		chain = append(chain, in+"scale=600:600:flags=neighbor,format=rgba,fade=t=in:st=0.3:d=0.6:alpha=1[qr]")
		n := next()
		chain = append(chain, cur+"[qr]overlay=x=230:y=240:shortest=1"+n)
		cur = n
		text(upper("On your phone"), small, hi, 46, "940", "380", 0.3)
		text("Scan for Usherr", big, fg, 104, "940", "450", 0.6)
		rule(940, 610, 240, 6, 0.9)
		host := strings.TrimPrefix(strings.TrimPrefix(c.QR, "https://"), "http://")
		text(host, small, fg, fitSize(host, 900, 44, 26), "940", "660", 1.1)
	case "reel":
		text("Coming soon", big, hi, 96, "c", "70", 0.1)
		k := len(c.Reel)
		const pw, ph, gap = 360, 540, 56
		x0 := (1920 - (k*pw + (k-1)*gap)) / 2
		for i, it := range c.Reel {
			f := filepath.Join(work, "reel"+strconv.Itoa(i)+".jpg")
			x := x0 + i*(pw+gap)
			at := 0.4 + 0.35*float64(i)
			if preshowFetchImage(ctx, it.PosterURL, f) {
				args = append(args, "-loop", "1", "-t", dur, "-i", f)
				in := "[" + strconv.Itoa(inputs) + ":v]"
				inputs++
				p := "[p" + strconv.Itoa(i) + "]"
				chain = append(chain, fmt.Sprintf("%sscale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,format=rgba,fade=t=in:st=%.2f:d=0.6:alpha=1%s", in, pw, ph, pw, ph, at, p))
				n := next()
				chain = append(chain, fmt.Sprintf("%s%soverlay=x=%d:y=220:shortest=1%s", cur, p, x, n))
				cur = n
			}
			cx := strconv.Itoa(x) + "+(" + strconv.Itoa(pw) + "-text_w)/2"
			text(it.Title, small, fg, fitSize(it.Title, pw, 34, 20), cx, "790", at+0.3)
			when := soonLine(it.ReleaseDate, it.Why, it.Season, time.Now())
			text(when, small, hi, fitSize(when, pw, 30, 18), cx, "840", at+0.4)
		}
	default: // feature
		text(upper("And now"), small, hi, 50, "c", "330", 0.3)
		text("Our feature presentation", big, fg, fitSize("Our feature presentation", 1700, 120, 60), "c", "410", 0.6)
		rule(840, 580, 240, 6, 1.0)
		text(c.Title, big, hi, fitSize(c.Title, 1600, 80, 40), "c", "630", 1.4)
	}
	chain = append(chain, fmt.Sprintf("%sfade=t=in:st=0:d=0.5,fade=t=out:st=%.2f:d=0.6,format=yuv420p[out]", cur, d-0.6))
	args = append(args, "-filter_complex", strings.Join(chain, ";"), "-map", "[out]", "-map", "1:a",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-r", "30",
		"-c:a", "aac", "-b:a", "96k", "-t", dur, "-movflags", "+faststart")
	return args, nil
}

// wrapWords breaks text into lines of about n letters (four at most).
func wrapWords(text string, n int) []string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(text) {
		if line != "" && len([]rune(line))+1+len([]rune(w)) > n {
			lines = append(lines, line)
			line = w
			continue
		}
		line = strings.TrimSpace(line + " " + w)
	}
	if line != "" {
		lines = append(lines, line)
	}
	if len(lines) > 4 {
		lines = append(lines[:3], strings.Join(lines[3:], " "))
	}
	return lines
}

// writeQRPNG draws the address as a QR code (black on white, with its quiet
// border), a module a pixel; ffmpeg scales it up.
func writeQRPNG(text, file string) error {
	code, err := qr.Encode([]byte(text))
	if err != nil {
		return err
	}
	const border = 4
	size := code.Size + 2*border
	img := image.NewGray(image.Rect(0, 0, size, size))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	for y := range code.Size {
		for x := range code.Size {
			if code.Modules[y][x] {
				img.SetGray(x+border, y+border, color.Gray{})
			}
		}
	}
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
