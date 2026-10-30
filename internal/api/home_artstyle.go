package api

// Usherr: Re-Posterr's own posters (M5, after Posterizarr). Re-Posterr makes
// pictures in the household's style: art without words (TMDB's, fanart.tv's
// or TheTVDB's, or the household's own), then the title in a font of their
// choosing, over a fade or a band, inside a border, or the film's own logo in
// its place, and their own overlay over it all. The same for a show's season
// posters ("Season 2"), its episodes' title cards (a still with the episode's
// name and number) and backgrounds with the title on.
//
// A style is a recipe of layers drawn in order (fade, band, border, overlay,
// title). It carries a version, so later options are new layers or new
// fields, and a style saved before keeps drawing the same. Its fingerprint
// goes on each change made with it: when the style changes, the nightly look
// offers the new look again (or puts it on, as its mode says). A library can
// have its own poster style (Anime unlike Movies).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/metadata"
	"github.com/rdborg/mediarium/web"
)

const (
	keyArtStyles  = "home.art.styles"  // JSON: {poster, season, card, backdrop, libraries}
	keyArtStyleOn = "home.art.styleOn" // the nightly look makes these: poster,season,card,backdrop

	artCardW, artCardH = 1920, 1080 // an episode's title card, a background
	artPosterH         = 1500
	artEveryPlaying    = 40 // a pass checks for someone watching every so many pictures
)

// The fonts Re-Posterr draws with: free fonts (SIL Open Font License, in
// artfonts with their licences) and Usherr's own.
//
//go:embed artfonts/*.ttf
var artFontFS embed.FS

type artFontInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	file string
}

var artFonts = []artFontInfo{
	{"bebas", "Bebas Neue", "BebasNeue-Regular.ttf"},
	{"anton", "Anton", "Anton-Regular.ttf"},
	{"oswald", "Oswald", "Oswald.ttf"},
	{"playfair", "Playfair Display", "PlayfairDisplay.ttf"},
	{"wix", "Wix Madefor (Usherr's own)", ""},
}

var artFontCache sync.Map // id -> *opentype.Font

func artFont(id string) (*opentype.Font, error) {
	if f, ok := artFontCache.Load(id); ok {
		return f.(*opentype.Font), nil
	}
	var raw []byte
	var err error
	for _, f := range artFonts {
		if f.ID != id {
			continue
		}
		if f.file == "" {
			raw, err = fs.ReadFile(web.DistFS, web.DistDir+"/fonts/WixMadeforDisplay-Variable.ttf")
		} else {
			raw, err = artFontFS.ReadFile("artfonts/" + f.file)
		}
	}
	if raw == nil || err != nil {
		if id != "bebas" {
			return artFont("bebas")
		}
		return nil, errors.New("the font isn't there")
	}
	f, err := opentype.Parse(raw)
	if err != nil {
		return nil, err
	}
	artFontCache.Store(id, f)
	return f, nil
}

// The fonts a title falls back to when the chosen one lacks its letters
// (Cyrillic, Greek, Japanese, Korean, Chinese...): the image's DejaVu and
// WenQuanYi Zen Hei. A missing file is passed over.
var artFallbackFiles = []string{"/usr/share/fonts/dejavu/DejaVuSans-Bold.ttf", "/usr/share/fonts/wqy-zenhei/wqy-zenhei.ttc"}

var artFallbacks = sync.OnceValue(func() []*opentype.Font {
	var out []*opentype.Font
	for _, p := range artFallbackFiles {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if f, err := opentype.Parse(raw); err == nil {
			out = append(out, f)
		} else if c, err := opentype.ParseCollection(raw); err == nil {
			if f, err := c.Font(0); err == nil {
				out = append(out, f)
			}
		}
	}
	return out
})

// artCovers says whether the font has every letter of s.
func artCovers(f *opentype.Font, s string) bool {
	var buf sfnt.Buffer
	for _, r := range s {
		if r == ' ' || r == ' ' {
			continue
		}
		if gi, err := f.GlyphIndex(&buf, r); err != nil || gi == 0 {
			return false
		}
	}
	return true
}

// artFontFor is the chosen font, or a fallback that has the words' letters.
func artFontFor(id, s string) (*opentype.Font, error) {
	f, err := artFont(id)
	if err != nil || artCovers(f, s) {
		return f, err
	}
	for _, fb := range artFallbacks() {
		if artCovers(fb, s) {
			return fb, nil
		}
	}
	return f, nil
}

// ---- styles ----

// artLayer is one step of a style. Which fields count depends on its kind:
//   - fade: a darkening from an edge (pos top or bottom; middle darkens it all),
//     size = how far in (% of the height), opacity at the edge;
//   - band: a solid strip, size = its height (%), color, opacity;
//   - border: a frame, size = its width (pixels on a poster 1000 wide), color;
//   - overlay: a PNG of the household's (file), laid over the whole picture,
//     or another by the copy's resolution (byRes: 4k, 1080, 720, sd);
//   - title: the words: font, size (the largest, % of the height), color,
//     caps, pos; logo = the title's own logo instead, when it has one;
//     sub = the second line (the year; the show's name on a season poster;
//     season and episode on a title card); outline and shadow; maxLines;
//     words = the wording ("Season {n}"; "Season {s} · Episode {e}");
//     short = a collection's name without "Collection".
type artLayer struct {
	Kind    string `json:"kind"`
	Pos     string `json:"pos,omitempty"`
	Color   string `json:"color,omitempty"`
	Opacity int    `json:"opacity,omitempty"`
	Size    int    `json:"size,omitempty"`
	Font    string `json:"font,omitempty"`
	Caps    bool   `json:"caps,omitempty"`
	Logo    bool   `json:"logo,omitempty"`
	Sub     bool   `json:"sub,omitempty"`
	// Added later; empty in a style saved before, which draws the same.
	Outline  int               `json:"outline,omitempty"`
	Shadow   bool              `json:"shadow,omitempty"`
	MaxLines int               `json:"maxLines,omitempty"`
	Words    string            `json:"words,omitempty"`
	Short    bool              `json:"short,omitempty"`
	File     string            `json:"file,omitempty"`
	ByRes    map[string]string `json:"byRes,omitempty"`
}

type artStyle struct {
	V      int        `json:"v"`
	Layers []artLayer `json:"layers"`
}

type artStyles struct {
	Poster   artStyle `json:"poster"`
	Season   artStyle `json:"season"`
	Card     artStyle `json:"card"`
	Backdrop artStyle `json:"backdrop"`
	// A library's own poster style, by its name in Jellyfin.
	Libraries map[string]artStyle `json:"libraries,omitempty"`
}

func artDefaultStyles() artStyles {
	fade := func(n int) artLayer {
		return artLayer{Kind: "fade", Pos: "bottom", Color: "#000000", Opacity: 85, Size: n}
	}
	return artStyles{
		Poster:   artStyle{V: 1, Layers: []artLayer{fade(40), {Kind: "title", Pos: "bottom", Font: "bebas", Color: "#ffffff", Size: 9, Caps: true, Logo: true}}},
		Season:   artStyle{V: 1, Layers: []artLayer{fade(45), {Kind: "title", Pos: "bottom", Font: "bebas", Color: "#ffffff", Size: 9, Caps: true, Logo: true, Sub: true}}},
		Card:     artStyle{V: 1, Layers: []artLayer{fade(50), {Kind: "title", Pos: "bottom", Font: "oswald", Color: "#ffffff", Size: 12, Sub: true}}},
		Backdrop: artStyle{V: 1, Layers: []artLayer{fade(45), {Kind: "title", Pos: "bottom", Font: "bebas", Color: "#ffffff", Size: 11, Caps: true, Logo: true}}},
	}
}

var (
	artColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	artFileRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,80}\.png$`)
	artResKeys = map[string]bool{"4k": true, "1080": true, "720": true, "sd": true}
)

// check says what's wrong with a style ("" when nothing).
func (st artStyle) check() string {
	if st.V != 1 {
		return "That style is from a newer Usherr."
	}
	if len(st.Layers) == 0 || len(st.Layers) > 8 {
		return "A style has one to eight layers."
	}
	titles := 0
	for _, l := range st.Layers {
		if l.Pos != "" && l.Pos != "top" && l.Pos != "middle" && l.Pos != "bottom" {
			return "A layer's place is top, middle or bottom."
		}
		if l.Color != "" && !artColorRe.MatchString(l.Color) {
			return "Colours are like #ff8800."
		}
		if l.Opacity < 0 || l.Opacity > 100 {
			return "Strength is 0 to 100."
		}
		lo, hi := 0, 0
		switch l.Kind {
		case "fade":
			lo, hi = 5, 100
		case "band":
			lo, hi = 5, 60
		case "border":
			lo, hi = 0, 60
		case "overlay":
			if l.File != "" && !artFileRe.MatchString(l.File) {
				return "An overlay is a PNG file."
			}
			for k, f := range l.ByRes {
				if !artResKeys[k] || f != "" && !artFileRe.MatchString(f) {
					return "An overlay by resolution is 4k, 1080, 720 or sd, and a PNG file."
				}
			}
		case "title":
			lo, hi = 3, 20
			titles++
			ok := false
			for _, f := range artFonts {
				ok = ok || f.ID == l.Font
			}
			if !ok {
				return "That font isn't one Re-Posterr has."
			}
			if l.Outline < 0 || l.Outline > 20 || l.MaxLines < 0 || l.MaxLines > 4 || len([]rune(l.Words)) > 60 {
				return "The title's outline is 0 to 20, its lines 1 to 4, its wording 60 letters at most."
			}
		default:
			return "Re-Posterr doesn't know a " + l.Kind + " layer."
		}
		if l.Size < lo || l.Size > hi {
			return fmt.Sprintf("A %s's size is %d to %d.", l.Kind, lo, hi)
		}
	}
	if titles != 1 {
		return "A style has one title layer."
	}
	return ""
}

// print is the style's fingerprint: a change made with it carries it.
func (st artStyle) print() string {
	raw, _ := json.Marshal(st)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:6])
}

func (st artStyle) title() artLayer {
	for _, l := range st.Layers {
		if l.Kind == "title" {
			return l
		}
	}
	return artLayer{}
}

// byRes: the style's overlay changes with the copy's resolution.
func (st artStyle) byRes() bool {
	for _, l := range st.Layers {
		if l.Kind == "overlay" && len(l.ByRes) > 0 {
			return true
		}
	}
	return false
}

func (s *Server) artStylesNow() (artStyles, map[string]bool) {
	st := artDefaultStyles()
	if raw, _ := s.Settings.Get(keyArtStyles); raw != "" {
		var saved artStyles
		if json.Unmarshal([]byte(raw), &saved) == nil {
			for _, p := range []struct{ got, into *artStyle }{{&saved.Poster, &st.Poster}, {&saved.Season, &st.Season}, {&saved.Card, &st.Card}, {&saved.Backdrop, &st.Backdrop}} {
				if p.got.check() == "" {
					*p.into = *p.got
				}
			}
			for lib, ls := range saved.Libraries {
				if ls.check() == "" {
					if st.Libraries == nil {
						st.Libraries = map[string]artStyle{}
					}
					st.Libraries[lib] = ls
				}
			}
		}
	}
	on := map[string]bool{}
	if raw, _ := s.Settings.Get(keyArtStyleOn); raw != "" {
		for _, w := range strings.Split(raw, ",") {
			on[strings.TrimSpace(w)] = true
		}
	}
	return st, on
}

// artStyleFor is the style each change type is made in.
var artStyleFor = map[string]string{"poster": "poster", "seasons": "season", "cards": "card", "backdrop": "backdrop"}

func (st artStyles) of(what string) artStyle {
	switch what {
	case "season", "seasons":
		return st.Season
	case "card", "cards":
		return st.Card
	case "backdrop":
		return st.Backdrop
	}
	return st.Poster
}

// forLibrary is the poster style for a copy in the library: its own, else
// the house style.
func (st artStyles) forLibrary(lib string) artStyle {
	if ls, ok := st.Libraries[lib]; ok && lib != "" {
		return ls
	}
	return st.Poster
}

// print is the fingerprint of every poster style (the house's and the
// libraries'): a change of any offers the posters again.
func (st artStyles) posterPrint() string {
	if len(st.Libraries) == 0 {
		return st.Poster.print()
	}
	raw, _ := json.Marshal(struct {
		P artStyle
		L map[string]artStyle
	}{st.Poster, st.Libraries})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:6])
}

func (st artStyles) printOf(what string) string {
	if what == "poster" {
		return st.posterPrint()
	}
	return st.of(what).print()
}

// ---- drawing ----

type artWords struct {
	Main string // the title, the show's name or the episode's name
	Sub  string // the year, "Season 2" or "Season 1 · Episode 3"
}

// artExtras is what a picture's overlay depends on: where the household's
// PNGs are, and the copy's resolution.
type artExtras struct {
	dir string
	res string
}

func artParseColor(s string, alpha int) color.NRGBA {
	c := color.NRGBA{A: 255}
	if v, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32); err == nil && len(s) == 7 {
		c.R, c.G, c.B = uint8(v>>16), uint8(v>>8), uint8(v)
	}
	if alpha >= 0 {
		c.A = uint8(alpha * 255 / 100)
	}
	return c
}

// artCover scales the art to fill w x h, cut evenly from its middle.
func artCover(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	if src == nil {
		return dst
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == 0 || sh == 0 {
		return dst
	}
	r := b
	if sw*h > sh*w { // wider than wanted: trim the sides
		cw := sh * w / h
		r = image.Rect(b.Min.X+(sw-cw)/2, b.Min.Y, b.Min.X+(sw-cw)/2+cw, b.Max.Y)
	} else {
		ch := sw * h / w
		// Posters cut from a wide picture keep its upper middle, where faces are.
		top := (sh - ch) / 3
		r = image.Rect(b.Min.X, b.Min.Y+top, b.Max.X, b.Min.Y+top+ch)
	}
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, r, draw.Src, nil)
	return dst
}

// artShade lays the colour over rows y0..y1, its strength per row given by
// alpha (0..1).
func artShade(img *image.RGBA, x0, y0, x1, y1 int, c color.NRGBA, alpha func(y int) float64) {
	for y := max(y0, 0); y < min(y1, img.Rect.Dy()); y++ {
		a := alpha(y)
		if a <= 0 {
			continue
		}
		a = math.Min(a, 1)
		row := img.Pix[y*img.Stride:]
		for x := max(x0, 0); x < min(x1, img.Rect.Dx()); x++ {
			p := row[x*4 : x*4+3]
			p[0] = uint8(float64(p[0])*(1-a) + float64(c.R)*a)
			p[1] = uint8(float64(p[1])*(1-a) + float64(c.G)*a)
			p[2] = uint8(float64(p[2])*(1-a) + float64(c.B)*a)
		}
	}
}

// artBand is the strip a band or the words sit in, by place.
func artBand(pos string, size, w, h int) image.Rectangle {
	bh := h * size / 100
	switch pos {
	case "top":
		return image.Rect(0, 0, w, bh)
	case "middle":
		return image.Rect(0, (h-bh)/2, w, (h+bh)/2)
	}
	return image.Rect(0, h-bh, w, h)
}

// artOverlay reads one of the household's overlay PNGs.
func artOverlay(dir, name string) image.Image {
	if name == "" || !artFileRe.MatchString(name) {
		return nil
	}
	f, err := os.Open(filepath.Join(dir, "overlays", name))
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(io.LimitReader(f, maxMetaImage))
	if err != nil {
		return nil
	}
	return img
}

// artDraw makes the picture: the art, then each layer in order.
func artDraw(art image.Image, w, h int, st artStyle, words artWords, logo image.Image, ex artExtras) *image.RGBA {
	img := artCover(art, w, h)
	// Sizes in pixels are a poster's (1000 x 1500); a title card's are as thick.
	scale := float64(min(w, h*1000/1500)) / 1000
	inset := 0
	var band *image.Rectangle
	for _, l := range st.Layers {
		c := artParseColor(l.Color, l.Opacity)
		switch l.Kind {
		case "fade":
			alpha := float64(c.A) / 255
			c.A = 255
			if l.Pos == "middle" {
				artShade(img, 0, 0, w, h, c, func(int) float64 { return alpha })
				continue
			}
			r := artBand(l.Pos, l.Size, w, h)
			span := float64(max(r.Dy(), 1))
			artShade(img, 0, r.Min.Y, w, r.Max.Y, c, func(y int) float64 {
				t := float64(y-r.Min.Y) / span
				if l.Pos == "top" {
					t = 1 - t
				}
				return alpha * t * t * (3 - 2*t) // smooth
			})
		case "band":
			r := artBand(l.Pos, l.Size, w, h)
			band = &r
			alpha := float64(c.A) / 255
			c.A = 255
			artShade(img, 0, r.Min.Y, w, r.Max.Y, c, func(int) float64 { return alpha })
		case "border":
			t := int(float64(l.Size) * scale)
			if t <= 0 {
				continue
			}
			c.A = 255
			u := image.NewUniform(c)
			for _, r := range []image.Rectangle{image.Rect(0, 0, w, t), image.Rect(0, h-t, w, h), image.Rect(0, 0, t, h), image.Rect(w-t, 0, w, h)} {
				draw.Draw(img, r, u, image.Point{}, draw.Src)
			}
			inset = max(inset, t)
		case "overlay":
			name := l.File
			if f, ok := l.ByRes[ex.res]; ok && f != "" {
				name = f
			}
			if ov := artOverlay(ex.dir, name); ov != nil {
				draw.CatmullRom.Scale(img, img.Bounds(), ov, ov.Bounds(), draw.Over, nil)
			}
		case "title":
			artTitle(img, l, words, logo, band, inset, scale)
		}
	}
	return img
}

// artTitle puts the words (or the logo) in their place: in the band when
// there's one there, else in a strip near the edge.
func artTitle(img *image.RGBA, l artLayer, words artWords, logo image.Image, band *image.Rectangle, inset int, scale float64) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	var r image.Rectangle
	switch {
	case band != nil && (l.Pos == "" || artBandPos(*band, h) == l.Pos):
		r = *band
	case l.Pos == "top":
		r = image.Rect(0, h*5/100, w, h*25/100)
	case l.Pos == "middle":
		r = image.Rect(0, h*36/100, w, h*64/100)
	default:
		r = image.Rect(0, h*74/100, w, h*95/100)
	}
	if l.Pos == "top" {
		r.Min.Y = max(r.Min.Y, inset)
	} else {
		r.Max.Y = min(r.Max.Y, h-inset-h/100)
	}
	marg := w*7/100 + inset
	maxW := w - 2*marg
	pen := artPen{col: artParseColor(l.Color, -1), outline: int(math.Round(float64(l.Outline) * scale)), shadow: l.Shadow, scale: scale}
	main, sub := words.Main, ""
	if l.Sub {
		sub = words.Sub
	}
	if main == "" { // a season poster without the show's name: "Season 2" is the title
		main, sub = words.Sub, ""
	}
	if l.Caps {
		main, sub = strings.ToUpper(main), strings.ToUpper(sub)
	}
	maxLines := l.MaxLines
	if maxLines <= 0 {
		maxLines = 2
	}
	// The second line, two fifths of the size.
	size := float64(h*l.Size) / 100
	var subFace font.Face
	subH := 0
	if sub != "" {
		if f, err := artFontFor(l.Font, sub); err == nil {
			subFace = artFace(f, math.Max(size*0.4, float64(h)/45))
			m := subFace.Metrics()
			subH = (m.Ascent + m.Descent).Ceil() + h/120
		}
	}
	if logo != nil && l.Logo && words.Main != "" {
		lb := logo.Bounds()
		boxH := r.Dy() - subH - h/50
		lw, lh := maxW*85/100, maxW*85/100*lb.Dy()/max(lb.Dx(), 1)
		if lh > boxH {
			lw, lh = boxH*lb.Dx()/max(lb.Dy(), 1), boxH
		}
		top := r.Min.Y + (r.Dy()-lh-subH)/2
		dst := image.Rect((w-lw)/2, top, (w+lw)/2, top+lh)
		draw.CatmullRom.Scale(img, dst, logo, lb, draw.Over, nil)
		if sub != "" {
			pen.text(img, subFace, sub, w, dst.Max.Y+h/120)
		}
		return
	}
	f, err := artFontFor(l.Font, main)
	if err != nil {
		return
	}
	// The largest size the words fit at, on as many lines as the style allows.
	var face font.Face
	var lines []string
	for ; size >= float64(h)/40; size *= 0.92 {
		face = artFace(f, size)
		lines = artWrap(face, main, maxW, maxLines)
		if len(lines) <= maxLines && artWidest(face, lines) <= maxW && artBlock(face, len(lines))+subH <= r.Dy() {
			break
		}
	}
	lineH := artBlock(face, 1)
	top := r.Min.Y + (r.Dy()-artBlock(face, len(lines))-subH)/2
	for i, line := range lines {
		pen.text(img, face, line, w, top+i*lineH)
	}
	if sub != "" {
		pen.text(img, subFace, sub, w, top+len(lines)*lineH+h/120)
	}
}

func artBandPos(r image.Rectangle, h int) string {
	switch {
	case r.Min.Y == 0:
		return "top"
	case r.Max.Y == h:
		return "bottom"
	}
	return "middle"
}

var artFaces sync.Map // font+size -> font.Face

func artFace(f *opentype.Font, size float64) font.Face {
	key := fmt.Sprintf("%p:%.1f", f, size)
	if v, ok := artFaces.Load(key); ok {
		return v.(font.Face)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil
	}
	artFaces.Store(key, face)
	return face
}

// artBlock is how tall n lines are.
func artBlock(face font.Face, n int) int {
	m := face.Metrics()
	return n * (m.Ascent + m.Descent).Ceil()
}

func artWidest(face font.Face, lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, font.MeasureString(face, l).Ceil())
	}
	return w
}

// artWrap breaks the words into lines no wider than maxW, at spaces: two
// lines as even as can be when one won't do (and two are allowed).
func artWrap(face font.Face, s string, maxW, maxLines int) []string {
	words := strings.Fields(s)
	if len(words) <= 1 || font.MeasureString(face, s).Ceil() <= maxW {
		return []string{s}
	}
	if maxLines >= 2 {
		best, bestW := []string{s}, math.MaxInt
		for i := 1; i < len(words); i++ {
			a, b := strings.Join(words[:i], " "), strings.Join(words[i:], " ")
			if w := artWidest(face, []string{a, b}); w < bestW {
				best, bestW = []string{a, b}, w
			}
		}
		if bestW <= maxW {
			return best
		}
	}
	// More lines: as many words a line as fit.
	var lines []string
	cur := ""
	for _, wd := range words {
		try := strings.TrimSpace(cur + " " + wd)
		if cur != "" && font.MeasureString(face, try).Ceil() > maxW {
			lines, cur = append(lines, cur), wd
			continue
		}
		cur = try
	}
	return append(lines, cur)
}

// artPen writes a line of the title: its colour, and a dark outline or a
// shadow under it.
type artPen struct {
	col     color.NRGBA
	outline int
	shadow  bool
	scale   float64
}

// text writes the line centred, its top at y.
func (p artPen) text(img *image.RGBA, face font.Face, s string, w, y int) {
	if face == nil || s == "" {
		return
	}
	x := (w - font.MeasureString(face, s).Ceil()) / 2
	base := y + face.Metrics().Ascent.Ceil()
	at := func(src image.Image, dx, dy int) {
		d := &font.Drawer{Dst: img, Src: src, Face: face, Dot: fixed.P(x+dx, base+dy)}
		d.DrawString(s)
	}
	if p.shadow {
		off := max(int(6*p.scale), 2)
		sh := image.NewUniform(color.NRGBA{A: 110})
		for _, d := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} { // a little soft
			at(sh, off+d[0], off+d[1])
		}
	}
	if p.outline > 0 {
		dark := image.NewUniform(color.NRGBA{A: 255})
		for a := 0.0; a < 2*math.Pi; a += math.Pi / 8 {
			at(dark, int(math.Round(math.Cos(a)*float64(p.outline))), int(math.Round(math.Sin(a)*float64(p.outline))))
		}
	}
	at(image.NewUniform(p.col), 0, 0)
}

// ---- the art the pictures are made from ----

// artBases keeps the art downloaded for making pictures a while (the designer
// draws the same title many times).
var artBases = newTTLBytes(80, 30*time.Minute)

// artHostOK: a picture from TMDB, fanart.tv or TheTVDB.
func artHostOK(u string) bool {
	return strings.HasPrefix(u, metadata.ImageHost+"/") || strings.HasPrefix(u, "https://assets.fanart.tv/") || strings.HasPrefix(u, "https://artworks.thetvdb.com/") || artTestHost(u)
}

// artImage downloads a TMDB, fanart.tv or TheTVDB picture and reads it.
func artImage(ctx context.Context, u string) (image.Image, error) {
	if u == "" {
		return nil, errors.New("no picture")
	}
	if b, ok := artBases.get(u); ok {
		img, _, err := image.Decode(bytes.NewReader(b))
		return img, err
	}
	if !artHostOK(u) {
		return nil, errors.New("that picture isn't from TMDB, fanart.tv or TheTVDB")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := artHTTP().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download the picture: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download the picture: status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxMetaImage+1))
	if err != nil || len(raw) > maxMetaImage {
		return nil, errors.New("the picture couldn't be downloaded whole")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("the picture couldn't be read: %w", err)
	}
	artBases.put(u, raw)
	return img, nil
}

// artPosterArt is what a picture is made from: the chosen picture (one of
// the title's, or one the household sent, "upload:..."), else the best of
// the preferred type without words, else of the other. And the logo, for
// styles that use it.
func (s *Server) artPosterArt(ctx context.Context, kind string, tmdbID, tvdbID int, pick string, st artStyle, prefer string) (art, logo image.Image, err error) {
	cands, err := s.artCandidates(ctx, kind, tmdbID, tvdbID)
	if err != nil {
		return nil, nil, err
	}
	order := []string{"poster", "backdrop"}
	if prefer == "backdrop" {
		order = []string{"backdrop", "poster"}
	}
	switch {
	case strings.HasPrefix(pick, "upload:"):
		if art, err = s.artUpload(strings.TrimPrefix(pick, "upload:")); err != nil {
			return nil, nil, err
		}
	case pick == "":
		for _, typ := range order {
			for _, c := range cands[typ] {
				if c.Lang == "" {
					pick = c.URL
					break
				}
			}
			if pick != "" {
				break
			}
		}
	default:
		ok := false
		for _, typ := range order {
			for _, c := range cands[typ] {
				ok = ok || c.URL == pick
			}
		}
		if !ok {
			return nil, nil, errors.New("that picture isn't one of this title's")
		}
	}
	if art == nil {
		if pick == "" {
			return nil, nil, errors.New("TMDB and fanart.tv have no picture without words for this title")
		}
		if art, err = artImage(ctx, pick); err != nil {
			return nil, nil, err
		}
	}
	if st.title().Logo && len(cands["logo"]) > 0 {
		logo, _ = artImage(ctx, cands["logo"][0].URL)
	}
	return art, logo, nil
}

func artJPEG(img image.Image, q int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// artFill puts the numbers in a wording: {n} the season, {s} and {e} the
// season and episode.
func artFill(tpl string, n, s, e int) string {
	return strings.NewReplacer("{n}", strconv.Itoa(n), "{s}", strconv.Itoa(s), "{e}", strconv.Itoa(e)).Replace(tpl)
}

func artSeasonName(st artStyle, n int) string {
	if n == 0 {
		return "Specials"
	}
	tpl := st.title().Words
	if tpl == "" {
		tpl = "Season {n}"
	}
	return artFill(tpl, n, n, 0)
}

// artCardWords: the episode's name, and "Season 1 · Episode 3" (or the
// style's own wording).
func artCardWords(st artStyle, name string, season, ep int) artWords {
	tpl := st.title().Words
	if tpl == "" {
		tpl = "Season {s} · Episode {e}"
	}
	return artWords{Main: name, Sub: artFill(tpl, season, season, ep)}
}

// artSeasonWords: the show's name (or its logo) over "Season 2"; or just
// "Season 2" when the style leaves the show's name off.
func artSeasonWords(st artStyle, show string, season int) artWords {
	if !st.title().Sub {
		return artWords{Sub: artSeasonName(st, season)}
	}
	return artWords{Main: show, Sub: artSeasonName(st, season)}
}

// artPosterWords: the title and its year (a collection has none, and loses
// "Collection" when the style says so). own: the household's own words.
func artPosterWords(st artStyle, kind, title string, year int, own string) artWords {
	if own != "" {
		return artWords{Main: own}
	}
	w := artWords{Main: title}
	if kind == "collection" {
		if st.title().Short {
			w.Main = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(title, " Collection"), " collection"))
		}
		return w
	}
	if year > 0 {
		w.Sub = strconv.Itoa(year)
	}
	return w
}

// ---- Jellyfin: a show's seasons and episodes, a copy's library ----

type jfChild struct {
	ID     string `json:"Id"`
	Name   string
	Index  int `json:"IndexNumber"`
	Parent int `json:"ParentIndexNumber"`
}

func jfChildren(ctx context.Context, srv mediaservers.Server, seriesID, typ string) ([]jfChild, error) {
	var page struct{ Items []jfChild }
	q := "/Items?ParentId=" + url.QueryEscape(seriesID) + "&IncludeItemTypes=" + typ + "&Recursive=true&Limit=5000"
	if err := jfJSON(ctx, srv, http.MethodGet, q, nil, &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

// jfLibraryOf is the name of the Jellyfin library the item is in ("" when
// Jellyfin doesn't say).
func jfLibraryOf(ctx context.Context, srv mediaservers.Server, id string) string {
	var anc []struct{ Name, Type string }
	if jfJSON(ctx, srv, http.MethodGet, "/Items/"+url.PathEscape(id)+"/Ancestors", nil, &anc) != nil {
		return ""
	}
	for _, a := range anc {
		if a.Type == "CollectionFolder" {
			return a.Name
		}
	}
	return ""
}

// jfResolution is the copy's picture size: 4k, 1080, 720 or sd ("" unknown).
func jfResolution(ctx context.Context, srv mediaservers.Server, id string) string {
	var page struct {
		Items []struct {
			MediaStreams []struct {
				Type  string
				Width int
			}
		}
	}
	if jfJSON(ctx, srv, http.MethodGet, "/Items?Ids="+url.QueryEscape(id)+"&Fields=MediaStreams", nil, &page) != nil || len(page.Items) == 0 {
		return ""
	}
	for _, st := range page.Items[0].MediaStreams {
		if st.Type != "Video" || st.Width == 0 {
			continue
		}
		switch {
		case st.Width >= 3200:
			return "4k"
		case st.Width >= 1800:
			return "1080"
		case st.Width >= 1200:
			return "720"
		}
		return "sd"
	}
	return ""
}

// jfLibraries are the names of Jellyfin's libraries.
func jfLibraries(ctx context.Context, srv mediaservers.Server) []string {
	var folders []struct{ Name string }
	if jfJSON(ctx, srv, http.MethodGet, "/Library/VirtualFolders", nil, &folders) != nil {
		return nil
	}
	var out []string
	for _, f := range folders {
		out = append(out, f.Name)
	}
	return out
}

var errArtPaused = errors.New("paused while someone watches on Jellyfin; it carries on later")

// artApplyStyled makes a change in the household's style: a poster or a
// background on every copy of the title (each in its library's style and,
// with an overlay by resolution, its own), or a show's season posters or
// title cards. What it replaces is kept first, as for any change; pictures
// already made (a change that was paused) aren't made again.
func (s *Server) artApplyStyled(ctx context.Context, srv mediaservers.Server, c artChange) error {
	if c.JFIDs == "" {
		return errors.New("Jellyfin doesn't have this title (yet)")
	}
	styles, _ := s.artStylesNow()
	ex := artExtras{dir: s.artDir()}
	var tvdb, year int
	_ = s.db.QueryRowContext(ctx, `SELECT tvdb_id, year FROM art_titles WHERE kind = ? AND tmdb_id = ?`, c.Kind, c.TMDBID).Scan(&tvdb, &year)
	type job struct {
		id   string
		make func() (image.Image, error)
	}
	var jobs []job
	imgType := "poster"
	var poster image.Image // the poster made, kept for Usherr's own pages
	switch c.Type {
	case "poster", "backdrop":
		imgType = c.Type
		w, h := artPosterW, artPosterH
		if c.Type == "backdrop" {
			w, h = artCardW, artCardH
		}
		made := map[string]image.Image{}
		for _, id := range strings.Split(c.JFIDs, ",") {
			st := styles.of(c.Type)
			if c.Type == "poster" && len(styles.Libraries) > 0 {
				st = styles.forLibrary(jfLibraryOf(ctx, srv, id))
			}
			cex := ex
			if st.byRes() {
				cex.res = jfResolution(ctx, srv, id)
			}
			jobs = append(jobs, job{id, func() (image.Image, error) {
				key := st.print() + "|" + cex.res
				if made[key] == nil {
					art, logo, err := s.artPosterArt(ctx, c.Kind, c.TMDBID, tvdb, c.URL, st, c.Type)
					if err != nil {
						return nil, err
					}
					made[key] = artDraw(art, w, h, st, artPosterWords(st, c.Kind, c.Title, year, c.Words), logo, cex)
				}
				if c.Type == "poster" && poster == nil {
					poster = made[key]
				}
				return made[key], nil
			}})
		}
	case "seasons":
		st := styles.Season
		art, logo, err := s.artPosterArt(ctx, c.Kind, c.TMDBID, tvdb, c.URL, st, "poster")
		if err != nil {
			return err
		}
		made := map[int]image.Image{}
		for _, sid := range strings.Split(c.JFIDs, ",") {
			seasons, err := jfChildren(ctx, srv, sid, "Season")
			if err != nil {
				return err
			}
			for _, se := range seasons {
				n := se.Index
				jobs = append(jobs, job{se.ID, func() (image.Image, error) {
					if made[n] == nil {
						made[n] = artDraw(art, artPosterW, artPosterH, st, artSeasonWords(st, c.Title, n), logo, ex)
					}
					return made[n], nil
				}})
			}
		}
	case "cards":
		st := styles.Card
		stills := map[int]map[int]metadata.EpisodeInfo{}
		for _, sid := range strings.Split(c.JFIDs, ",") {
			eps, err := jfChildren(ctx, srv, sid, "Episode")
			if err != nil {
				return err
			}
			for _, ep := range eps {
				jobs = append(jobs, job{ep.ID, func() (image.Image, error) {
					if stills[ep.Parent] == nil {
						stills[ep.Parent] = map[int]metadata.EpisodeInfo{}
						if list, err := s.TMDB().GetSeason(ctx, c.TMDBID, ep.Parent); err == nil {
							for _, e := range list {
								stills[ep.Parent][e.Episode] = e
							}
						}
					}
					info := stills[ep.Parent][ep.Index]
					if info.Still == "" {
						return nil, nil // TMDB has no still: the episode keeps its picture
					}
					art, err := artImage(ctx, metadata.ImageURL("original", info.Still))
					if err != nil {
						return nil, err
					}
					name := ep.Name
					if name == "" {
						name = info.Name
					}
					return artDraw(art, artCardW, artCardH, st, artCardWords(st, name, ep.Parent, ep.Index), nil, ex), nil
				}})
			}
		}
	default:
		return errors.New("Re-Posterr doesn't make that picture")
	}
	backups := map[string]string{}
	_ = json.Unmarshal([]byte(c.Backups), &backups)
	defer s.artSaveBackups(context.WithoutCancel(ctx), c.ID, backups)
	made := 0
	for _, j := range jobs {
		if backups["+"+j.id] != "" {
			continue // made before the pause
		}
		if !c.ByHand && made > 0 && made%artEveryPlaying == 0 {
			if playing, err := s.mediaClient.NowPlaying(ctx, srv); err == nil && len(playing) > 0 {
				return errArtPaused
			}
		}
		img, err := j.make()
		if err != nil {
			return err
		}
		if img == nil {
			continue
		}
		data, err := artJPEG(img, 88)
		if err != nil {
			return err
		}
		if err := s.artKeep(ctx, srv, j.id, imgType, backups); err != nil {
			return err
		}
		if err := jfSetImage(ctx, srv, j.id, imgType, data, "image/jpeg"); err != nil {
			return err
		}
		backups["+"+j.id] = "1"
		made++
		if made%25 == 0 {
			s.artSaveBackups(ctx, c.ID, backups)
		}
	}
	if poster != nil {
		if b, err := artJPEG(poster, 88); err == nil {
			s.artSavePoster(c.ID, b)
		}
	}
	return nil
}

// ---- the API ----

// GET /api/home/art/styles (admin): the styles, which the nightly look
// makes, the fonts, Jellyfin's libraries and the overlays.
func (s *Server) handleArtStyles(w http.ResponseWriter, r *http.Request) {
	st, on := s.artStylesNow()
	var libs []string
	if srv, ok := s.jellyfinServer(); ok {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		libs = jfLibraries(ctx, srv)
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]any{"styles": st, "on": on, "fonts": artFonts, "defaults": artDefaultStyles(), "libraries": libs, "overlays": s.artOverlayNames()})
}

// PUT /api/home/art/styles {styles, on} (admin)
func (s *Server) handlePutArtStyles(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Styles artStyles       `json:"styles"`
		On     map[string]bool `json:"on"`
	}
	if err := decodeJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	all := []artStyle{b.Styles.Poster, b.Styles.Season, b.Styles.Card, b.Styles.Backdrop}
	for _, st := range b.Styles.Libraries {
		all = append(all, st)
	}
	for _, st := range all {
		if msg := st.check(); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
	}
	raw, _ := json.Marshal(b.Styles)
	var on []string
	for _, k := range []string{"poster", "season", "card", "backdrop"} {
		if b.On[k] {
			on = append(on, k)
		}
	}
	if s.Settings.Set(keyArtStyles, string(raw), false) != nil || s.Settings.Set(keyArtStyleOn, strings.Join(on, ","), false) != nil {
		writeError(w, http.StatusInternalServerError, "The style couldn't be saved.")
		return
	}
	s.handleArtStyles(w, r)
}

// GET /api/home/art/samples?q=&kind= (admin): titles to try a style on.
func (s *Server) handleArtSamples(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	kind := r.URL.Query().Get("kind")
	where, args := `WHERE jf_ids != ''`, []any{}
	if kind == "tv" || kind == "movie" || kind == "collection" {
		where += ` AND kind = ?`
		args = append(args, kind)
	}
	order := `ORDER BY RANDOM()`
	if q != "" {
		where += ` AND title LIKE ?`
		args = append(args, "%"+q+"%")
		order = `ORDER BY length(title), title`
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT kind, tmdb_id, title, year FROM art_titles `+where+` `+order+` LIMIT 8`, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The titles couldn't be read.")
		return
	}
	defer rows.Close()
	type sample struct {
		Kind   string `json:"kind"`
		TMDBID int    `json:"tmdbId"`
		Title  string `json:"title"`
		Year   int    `json:"year"`
	}
	out := []sample{}
	for rows.Next() {
		var x sample
		if rows.Scan(&x.Kind, &x.TMDBID, &x.Title, &x.Year) == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// artPreviewMade keeps the pictures drawn for the designer and the waiting
// list a while.
var artPreviewMade = newTTLBytes(200, 20*time.Minute)

// artLookOpts: what else a picture drawn to look at depends on.
type artLookOpts struct {
	pick  string // the art it's made from ("" the best without words)
	words string // the household's own words
	res   string // as on a copy of this resolution
}

// artLook draws a small picture of what a change in the style would make for
// the title: its poster or background, its first season's poster, or its
// first episode's title card.
func (s *Server) artLook(ctx context.Context, what string, st artStyle, kind string, tmdbID int, o artLookOpts) ([]byte, error) {
	raw, _ := json.Marshal(st)
	key := fmt.Sprintf("%s|%s|%d|%+v|%s", what, kind, tmdbID, o, raw)
	if b, ok := artPreviewMade.get(key); ok {
		return b, nil
	}
	var title string
	var tvdb, year int
	if s.db.QueryRowContext(ctx, `SELECT title, tvdb_id, year FROM art_titles WHERE kind = ? AND tmdb_id = ?`, kind, tmdbID).Scan(&title, &tvdb, &year) != nil {
		return nil, errors.New("Re-Posterr doesn't know that title yet")
	}
	ex := artExtras{dir: s.artDir(), res: o.res}
	var img image.Image
	switch what {
	case "card", "cards":
		if kind != "tv" {
			return nil, errors.New("title cards are for shows")
		}
		eps, err := s.TMDB().GetSeason(ctx, tmdbID, 1)
		if err != nil || len(eps) == 0 {
			return nil, errors.New("TMDB has no first season for this show")
		}
		ep := eps[0]
		for _, e := range eps {
			if e.Still != "" {
				ep = e
				break
			}
		}
		if ep.Still == "" {
			return nil, errors.New("TMDB has no stills for this show's first season")
		}
		art, err := artImage(ctx, metadata.ImageURL("original", ep.Still))
		if err != nil {
			return nil, err
		}
		img = artDraw(art, artCardW/2, artCardH/2, st, artCardWords(st, ep.Name, ep.Season, ep.Episode), nil, ex)
	case "backdrop":
		art, logo, err := s.artPosterArt(ctx, kind, tmdbID, tvdb, o.pick, st, "backdrop")
		if err != nil {
			return nil, err
		}
		img = artDraw(art, artCardW/2, artCardH/2, st, artPosterWords(st, kind, title, year, o.words), logo, ex)
	default:
		art, logo, err := s.artPosterArt(ctx, kind, tmdbID, tvdb, o.pick, st, "poster")
		if err != nil {
			return nil, err
		}
		words := artPosterWords(st, kind, title, year, o.words)
		if what == "season" || what == "seasons" {
			words = artSeasonWords(st, title, 1)
		}
		img = artDraw(art, artPosterW/2, artPosterH/2, st, words, logo, ex)
	}
	b, err := artJPEG(img, 84)
	if err != nil {
		return nil, err
	}
	artPreviewMade.put(key, b)
	return b, nil
}

func writeJPEG(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=600")
	_, _ = w.Write(b)
}

// POST /api/home/art/styles/preview {what, style?, library?, kind, tmdbId,
// url?, words?, res?} (admin): the picture a style makes for a title (the
// saved style when none is sent; a library's when it's named).
func (s *Server) handleArtStylePreview(w http.ResponseWriter, r *http.Request) {
	var b struct {
		What    string    `json:"what"`
		Style   *artStyle `json:"style"`
		Library string    `json:"library"`
		Kind    string    `json:"kind"`
		TMDBID  int       `json:"tmdbId"`
		URL     string    `json:"url"`
		Words   string    `json:"words"`
		Res     string    `json:"res"`
	}
	if err := decodeJSON(r, &b); err != nil || artStyleFor[b.What] == "" && b.What != "season" && b.What != "card" || len([]rune(b.Words)) > 80 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	styles, _ := s.artStylesNow()
	st := styles.of(b.What)
	if b.What == "poster" && b.Library != "" {
		st = styles.forLibrary(b.Library)
	}
	if b.Style != nil {
		if msg := b.Style.check(); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		st = *b.Style
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	img, err := s.artLook(ctx, b.What, st, b.Kind, b.TMDBID, artLookOpts{pick: b.URL, words: strings.TrimSpace(b.Words), res: b.Res})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJPEG(w, img)
}

// GET /api/home/art/changes/{id}/look (admin): what a change in the style
// would make (for the waiting list).
func (s *Server) handleArtChangeLook(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	list, err := s.artChanges(r.Context(), `WHERE c.id = ?`, id)
	if err != nil || len(list) == 0 || list[0].Style == "" {
		http.NotFound(w, r)
		return
	}
	c := list[0]
	styles, _ := s.artStylesNow()
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	img, err := s.artLook(ctx, c.Type, styles.of(c.Type), c.Kind, c.TMDBID, artLookOpts{pick: c.URL, words: c.Words})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeJPEG(w, img)
}

// POST /api/home/art/title/style {kind, tmdbId, what, url?, words?} (admin):
// makes the title's poster or background (or a show's season posters or
// title cards) in the style, by hand: a poster or background at once, the
// rest in the background. url may be a picture the household sent
// ("upload:..."), words their own words for it.
func (s *Server) handleArtTitleStyle(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Kind   string `json:"kind"`
		TMDBID int    `json:"tmdbId"`
		What   string `json:"what"`
		URL    string `json:"url"`
		Words  string `json:"words"`
	}
	if err := decodeJSON(r, &b); err != nil || artStyleFor[b.What] == "" || b.TMDBID <= 0 || (b.Kind != "movie" && b.Kind != "tv" && b.Kind != "collection") ||
		(b.What == "seasons" || b.What == "cards") && b.Kind != "tv" || len([]rune(b.Words)) > 80 {
		writeError(w, http.StatusBadRequest, "That request couldn't be read.")
		return
	}
	srv, ok := s.jellyfinServer()
	if !ok {
		writeError(w, http.StatusConflict, connectServerFirst)
		return
	}
	var title, jf string
	if s.db.QueryRowContext(r.Context(), `SELECT title, jf_ids FROM art_titles WHERE kind = ? AND tmdb_id = ?`, b.Kind, b.TMDBID).Scan(&title, &jf) != nil || jf == "" {
		writeError(w, http.StatusConflict, "Jellyfin doesn't have this title yet.")
		return
	}
	styles, _ := s.artStylesNow()
	now := time.Now().UnixMilli()
	_, _ = s.db.ExecContext(r.Context(), `UPDATE art_changes SET state = 'skipped', done_ms = ? WHERE kind = ? AND tmdb_id = ? AND type = ? AND state = 'waiting'`, now, b.Kind, b.TMDBID, b.What)
	res, err := s.db.ExecContext(r.Context(), `INSERT INTO art_changes (kind, tmdb_id, title, type, source, url, why, by_hand, style, words, created_ms) VALUES (?, ?, ?, ?, 'usherr', ?, 'chosen', 1, ?, ?, ?)`,
		b.Kind, b.TMDBID, title, b.What, b.URL, styles.printOf(b.What), strings.TrimSpace(b.Words), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The choice couldn't be saved.")
		return
	}
	id, _ := res.LastInsertId()
	if b.What == "seasons" || b.What == "cards" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
			defer cancel()
			_ = s.artMake(ctx, srv, id)
		}()
		writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "background": true})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if err := s.artMake(ctx, srv, id); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}
