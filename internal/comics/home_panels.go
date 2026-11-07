package comics

// Home fork: finding the panels on a comic page, for the reader's guided
// view. A deterministic method (no machine-learning model, nothing added to
// the image): the page is shrunk and turned grey, the gutter colour is read
// from the page's edge (white or black gutters both work), and the page is
// cut recursively along clean gutter lines, first into rows and then each
// row into panels, the way the eye reads it. That order is the reading
// order: rows top to bottom, panels left to right (right to left for manga).
// Each panel is trimmed to its drawn area; specks such as page numbers are
// dropped. A page where no panels can be told apart (a splash page, or art
// without gutters) has none, and the reader shows it whole.

import (
	"image"
	"image/color"
	"math"
	"sort"
)

// Panel is one panel's box, as fractions of the page's width and height.
type Panel struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// PanelOptions tunes the detector; the zero value is the default.
type PanelOptions struct {
	RightToLeft bool // manga: panels in a row are read right to left
}

const (
	panelWork      = 1000  // the longer side of the working copy, in pixels
	gutterTol      = 40    // how far from the gutter colour a pixel may be and still count as gutter
	gutterLine     = 0.92  // the share of a row (or column) that must be gutter for it to be a gutter line (lettering may cross)
	minGapFrac     = 0.002 // a gutter is at least this share of the page's side
	minPanelArea   = 0.012 // a panel covers at least this share of the page
	minPanelSide   = 0.06  // and is at least this share of the page's width and height
	minPanelInk    = 0.04  // and at least this share of it isn't gutter
	splashCoverage = 0.85  // one "panel" this big is the whole page: none to guide through
)

// gray is the working copy: one byte per pixel, and whether each pixel is
// gutter-coloured.
type grayPage struct {
	w, h   int
	gutter []bool
}

func (g *grayPage) isGutter(x, y int) bool { return g.gutter[y*g.w+x] }

// DetectPanels finds the panels of a page image, in reading order.
func DetectPanels(img image.Image, opt PanelOptions) []Panel {
	g := toGrayPage(img)
	if g == nil {
		return nil
	}
	var boxes []image.Rectangle
	cut(g, image.Rect(0, 0, g.w, g.h), opt, 0, &boxes)
	pageArea := float64(g.w * g.h)
	var out []Panel
	for _, b := range boxes {
		b = trim(g, b)
		if b.Empty() {
			continue
		}
		area := float64(b.Dx() * b.Dy())
		if area/pageArea < minPanelArea || float64(b.Dx())/float64(g.w) < minPanelSide || float64(b.Dy())/float64(g.h) < minPanelSide {
			continue
		}
		if ink(g, b) < minPanelInk {
			continue
		}
		out = append(out, Panel{
			X: float64(b.Min.X) / float64(g.w), Y: float64(b.Min.Y) / float64(g.h),
			W: float64(b.Dx()) / float64(g.w), H: float64(b.Dy()) / float64(g.h),
		})
	}
	if len(out) == 1 && out[0].W*out[0].H >= splashCoverage {
		return nil
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// toGrayPage shrinks the page to the working size, turns it grey and marks
// the gutter-coloured pixels. The gutter colour is the most common shade of
// the page's outer edge.
func toGrayPage(img image.Image) *grayPage {
	lum, w, h := shrunkLuminance(img)
	if lum == nil {
		return nil
	}
	// The gutter colour: the commonest shade (in steps of 8) along the edge.
	var hist [32]int
	edge := max(2, w/100)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < edge || x >= w-edge || y < edge || y >= h-edge {
				hist[lum[y*w+x]/8]++
			}
		}
	}
	best := 0
	for i := range hist {
		if hist[i] > hist[best] {
			best = i
		}
	}
	bg := best*8 + 4
	near := make([]bool, w*h)
	for i, v := range lum {
		d := int(v) - bg
		near[i] = d >= -gutterTol && d <= gutterTol
	}
	// Gutter is the gutter-coloured space joined to the page's edge: a white
	// sky or a dark night inside a framed panel isn't gutter, so lettering
	// may cross a gutter without the gutter being lost.
	g := &grayPage{w: w, h: h, gutter: make([]bool, w*h)}
	stack := make([]int, 0, 4*(w+h))
	push := func(i int) {
		if near[i] && !g.gutter[i] {
			g.gutter[i] = true
			stack = append(stack, i)
		}
	}
	for x := 0; x < w; x++ {
		push(x)
		push((h-1)*w + x)
	}
	for y := 0; y < h; y++ {
		push(y * w)
		push(y*w + w - 1)
	}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := i%w, i/w
		if x > 0 {
			push(i - 1)
		}
		if x < w-1 {
			push(i + 1)
		}
		if y > 0 {
			push(i - w)
		}
		if y < h-1 {
			push(i + w)
		}
	}
	return g
}

// shrunkLuminance is the page at the working size, one grey byte a pixel, or nil
// for a picture too small to read.
func shrunkLuminance(img image.Image) ([]uint8, int, int) {
	b := img.Bounds()
	if b.Dx() < 50 || b.Dy() < 50 {
		return nil, 0, 0
	}
	scale := float64(panelWork) / math.Max(float64(b.Dx()), float64(b.Dy()))
	if scale > 1 {
		scale = 1
	}
	w, h := int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)
	lum := make([]uint8, w*h)
	// Average each working pixel's patch of the original (a box filter), so
	// fine hatching doesn't read as gutter or as ink by chance.
	for y := 0; y < h; y++ {
		sy0 := b.Min.Y + int(float64(y)/scale)
		sy1 := b.Min.Y + int(float64(y+1)/scale)
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < w; x++ {
			sx0 := b.Min.X + int(float64(x)/scale)
			sx1 := b.Min.X + int(float64(x+1)/scale)
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var sum, n int
			for sy := sy0; sy < sy1 && sy < b.Max.Y; sy += max(1, (sy1-sy0)/3) {
				for sx := sx0; sx < sx1 && sx < b.Max.X; sx += max(1, (sx1-sx0)/3) {
					sum += int(color.GrayModel.Convert(img.At(sx, sy)).(color.Gray).Y)
					n++
				}
			}
			if n > 0 {
				lum[y*w+x] = uint8(sum / n)
			}
		}
	}
	return lum, w, h
}

// lineGutter reports whether row y (horizontal) or column x (vertical) of r
// is a gutter line.
func lineGutter(g *grayPage, r image.Rectangle, horizontal bool, at int) bool {
	n, gut := 0, 0
	if horizontal {
		for x := r.Min.X; x < r.Max.X; x++ {
			n++
			if g.isGutter(x, at) {
				gut++
			}
		}
	} else {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			n++
			if g.isGutter(at, y) {
				gut++
			}
		}
	}
	return n > 0 && float64(gut)/float64(n) >= gutterLine
}

// pieces splits r along its gutter lines in one direction, dropping the
// gutters themselves and the margins at either end.
func pieces(g *grayPage, r image.Rectangle, horizontal bool) []image.Rectangle {
	lo, hi := r.Min.Y, r.Max.Y
	side := g.h
	if !horizontal {
		lo, hi, side = r.Min.X, r.Max.X, g.w
	}
	minGap := max(2, int(math.Round(minGapFrac*float64(side))))
	var out []image.Rectangle
	start := -1 // where the current piece began
	gapRun := 0
	for at := lo; at <= hi; at++ {
		gutter := at == hi || lineGutter(g, r, horizontal, at)
		if !gutter {
			if start < 0 {
				start = at
			}
			gapRun = 0
			continue
		}
		gapRun++
		if start >= 0 && (gapRun >= minGap || at == hi) {
			end := at - gapRun + 1
			if horizontal {
				out = append(out, image.Rect(r.Min.X, start, r.Max.X, end))
			} else {
				out = append(out, image.Rect(start, r.Min.Y, end, r.Max.Y))
			}
			start = -1
		}
	}
	return out
}

// cut splits r into panels, appending them to boxes in reading order.
func cut(g *grayPage, r image.Rectangle, opt PanelOptions, depth int, boxes *[]image.Rectangle) {
	if depth > 12 || r.Dx() < 4 || r.Dy() < 4 {
		*boxes = append(*boxes, r)
		return
	}
	if rows := pieces(g, r, true); len(rows) > 1 {
		for _, p := range rows {
			cut(g, p, opt, depth+1, boxes)
		}
		return
	}
	if cols := pieces(g, r, false); len(cols) > 1 {
		if opt.RightToLeft {
			sort.Slice(cols, func(i, j int) bool { return cols[i].Min.X > cols[j].Min.X })
		}
		for _, p := range cols {
			cut(g, p, opt, depth+1, boxes)
		}
		return
	}
	*boxes = append(*boxes, r)
}

// trim shrinks r to the part that isn't gutter.
func trim(g *grayPage, r image.Rectangle) image.Rectangle {
	for r.Min.Y < r.Max.Y && lineGutter(g, r, true, r.Min.Y) {
		r.Min.Y++
	}
	for r.Max.Y > r.Min.Y && lineGutter(g, r, true, r.Max.Y-1) {
		r.Max.Y--
	}
	for r.Min.X < r.Max.X && lineGutter(g, r, false, r.Min.X) {
		r.Min.X++
	}
	for r.Max.X > r.Min.X && lineGutter(g, r, false, r.Max.X-1) {
		r.Max.X--
	}
	return r
}

// A page at least this much wider than tall holds two pages side by side.
const spreadRatio = 1.1

// spineSlack is how far past the middle of a spread a panel may reach and still
// belong to one page.
const spineSlack = 0.05

// IsSpread reports whether a page this size is a two-page spread in one picture.
func IsSpread(w, h int) bool { return h > 0 && float64(w) > float64(h)*spreadRatio }

// SpreadOrder puts the panels of a two-page spread page by page: all of the first
// page before any of the second, keeping each page's own order. Seen as one wide
// picture, a row running across both pages would otherwise be read across the
// fold. A panel across the fold means the two pages can't be told apart, so the
// order stays as it was.
func SpreadOrder(panels []Panel, rightToLeft bool) []Panel {
	var first, second []Panel
	for _, p := range panels {
		onLeft := p.X+p.W <= 0.5+spineSlack
		onRight := p.X >= 0.5-spineSlack
		if !onLeft && !onRight {
			return panels
		}
		if onLeft == onRight { // a narrow one on the fold: with the page before it
			if len(second) > 0 {
				second = append(second, p)
			} else {
				first = append(first, p)
			}
			continue
		}
		if onRight == rightToLeft {
			first = append(first, p)
		} else {
			second = append(second, p)
		}
	}
	return append(first, second...)
}

const (
	stripRatio   = 2.2  // a page this many times taller than wide is a strip of a long scroll
	stripSpeck   = 0.12 // a piece less tall than this share of the width is lettering or a speck
	stripTallest = 1.8  // a piece taller than this many widths doesn't fit a screen at once
	stripStep    = 1.3  // and is shown this many widths at a time
	stripOverlap = 0.1  // with this much of the last view kept
	stripMinGap  = 2    // rows of the working copy that make a gutter
)

// IsStrip reports whether a page this size is a strip of a long scroll (manhwa).
func IsStrip(w, h int) bool { return w > 0 && float64(h) >= float64(w)*stripRatio }

// DetectStrip cuts a long strip along its gutters, top to bottom. A strip's panels
// fill its width, so only rows matter, and a stretch too long for the screen is
// shown a screen at a time, so a strip with no gutters at all still reads.
func DetectStrip(img image.Image) []Panel {
	b := img.Bounds()
	if !IsStrip(b.Dx(), b.Dy()) {
		return nil
	}
	lum, w, h := shrunkLuminance(img)
	if lum == nil {
		return nil
	}
	widths := float64(b.Dy()) / float64(b.Dx()) // the strip's height, in widths
	var out []Panel
	start := -1 // where the current piece began
	end := func(at int) {
		if start >= 0 {
			from, to := float64(start)/float64(h), float64(at)/float64(h)
			if (to-from)*widths >= stripSpeck {
				out = append(out, stripViews(from, to, widths)...)
			}
		}
		start = -1
	}
	gap := 0
	for y := 0; y < h; y++ {
		if stripGutterRow(lum[y*w : (y+1)*w]) {
			gap++
			if gap == stripMinGap {
				end(y - gap + 1)
			}
			continue
		}
		gap = 0
		if start < 0 {
			start = y
		}
	}
	end(h - gap)
	if len(out) < 2 {
		return nil
	}
	return out
}

// stripGutterRow reports whether a row of a strip is plain white or plain black,
// nearly edge to edge. A strip's panels reach its sides, so the page's edge says
// nothing about the gutter colour here, as it does on a normal page.
func stripGutterRow(row []uint8) bool {
	light, dark := 0, 0
	for _, v := range row {
		switch {
		case v >= 215:
			light++
		case v <= 35:
			dark++
		}
	}
	need := int(gutterLine * float64(len(row)))
	return light >= need || dark >= need
}

// stripViews shows from..to (shares of the strip's height) in views that fit a screen.
func stripViews(from, to, widths float64) []Panel {
	if (to-from)*widths <= stripTallest {
		return []Panel{{X: 0, Y: from, W: 1, H: to - from}}
	}
	step, kept := stripStep/widths, stripOverlap/widths
	var out []Panel
	for at := from; ; at += step - kept {
		if at+step >= to-kept {
			return append(out, Panel{X: 0, Y: at, W: 1, H: to - at})
		}
		out = append(out, Panel{X: 0, Y: at, W: 1, H: step})
	}
}

// ink is the share of r that isn't gutter-coloured.
func ink(g *grayPage, r image.Rectangle) float64 {
	n, inked := 0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			n++
			if !g.isGutter(x, y) {
				inked++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return float64(inked) / float64(n)
}
