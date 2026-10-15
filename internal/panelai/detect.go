package panelai

import (
	"image"
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

// Detector finds panels with the detector model and orders them with the
// reading-order model.
type Detector struct {
	net   *Model
	order *Trees
}

const (
	netSize  = 640  // the detector's input: 640 x 640
	minConf  = 0.25 // a box the detector is less sure of is dropped
	maxIoU   = 0.45 // a box overlapping a surer one this much is the same panel
	padShade = 114.0 / 255
)

// NewDetector loads the two models.
func NewDetector(detector, order []byte) (*Detector, error) {
	net, err := Load(detector)
	if err != nil {
		return nil, err
	}
	tr, err := LoadTrees(order)
	if err != nil {
		return nil, err
	}
	return &Detector{net: net, order: tr}, nil
}

// Panels finds a page's panels in reading order. rightToLeft (manga) reads
// the page mirrored, so panels run right to left.
func (d *Detector) Panels(img image.Image, rightToLeft bool) ([]Panel, error) {
	in, scale, padX, padY := letterbox(img, rightToLeft)
	out, err := d.net.Run(map[string]*Tensor{d.net.g.inputs[0]: in})
	if err != nil {
		return nil, err
	}
	res := out[d.net.g.outputs[0]] // 1 x 5 x N: cx, cy, w, h, confidence
	n := res.Shape[2]
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	type box struct {
		x1, y1, x2, y2, conf float64
	}
	var boxes []box
	for i := 0; i < n; i++ {
		conf := float64(res.F[4*n+i])
		if conf < minConf {
			continue
		}
		cx, cy, bw, bh := float64(res.F[i]), float64(res.F[n+i]), float64(res.F[2*n+i]), float64(res.F[3*n+i])
		bx := box{
			x1: (cx - bw/2 - padX) / scale, y1: (cy - bh/2 - padY) / scale,
			x2: (cx + bw/2 - padX) / scale, y2: (cy + bh/2 - padY) / scale, conf: conf,
		}
		bx.x1, bx.x2 = clamp(bx.x1, 0, w), clamp(bx.x2, 0, w)
		bx.y1, bx.y2 = clamp(bx.y1, 0, h), clamp(bx.y2, 0, h)
		boxes = append(boxes, bx)
	}
	// Non-maximum suppression: the surest box of each overlapping group.
	sort.SliceStable(boxes, func(i, j int) bool { return boxes[i].conf > boxes[j].conf })
	var kept []box
	for _, c := range boxes {
		ok := true
		for _, k := range kept {
			ix := math.Max(0, math.Min(c.x2, k.x2)-math.Max(c.x1, k.x1))
			iy := math.Max(0, math.Min(c.y2, k.y2)-math.Max(c.y1, k.y1))
			inter := ix * iy
			union := (c.x2-c.x1)*(c.y2-c.y1) + (k.x2-k.x1)*(k.y2-k.y1) - inter
			if union > 0 && inter/union >= maxIoU {
				ok = false
				break
			}
		}
		if ok {
			kept = append(kept, c)
		}
	}
	panels := make([]Panel, len(kept))
	for i, k := range kept {
		panels[i] = Panel{X: k.x1 / w, Y: k.y1 / h, W: (k.x2 - k.x1) / w, H: (k.y2 - k.y1) / h}
	}
	panels = d.inOrder(panels)
	if rightToLeft { // back from the mirrored page
		for i := range panels {
			panels[i].X = 1 - panels[i].X - panels[i].W
		}
	}
	return panels, nil
}

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

// inOrder sorts panels into reading order: each panel scores how likely it
// is read before each other one, and the highest total goes first.
func (d *Detector) inOrder(ps []Panel) []Panel {
	n := len(ps)
	if n <= 1 {
		return ps
	}
	type geo struct{ x, y, w, h, cx, cy, x2, y2 float64 }
	g := make([]geo, n)
	hs := make([]float64, n)
	for i, p := range ps {
		g[i] = geo{p.X, p.Y, p.W, p.H, p.X + p.W/2, p.Y + p.H/2, p.X + p.W, p.Y + p.H}
		hs[i] = p.H
	}
	medH := median(hs)
	b2f := func(b bool) float32 {
		if b {
			return 1
		}
		return 0
	}
	ratio := func(a, b float64) float64 {
		if b > 0 {
			return a / b
		}
		return 1
	}
	score := make([]float64, n)
	x := make([]float32, 14)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			a, b := g[i], g[j]
			dx, dy := b.cx-a.cx, b.cy-a.cy
			v := 0.0
			if sh := math.Min(a.h, b.h); sh > 0 {
				v = math.Max(0, math.Min(a.y2, b.y2)-math.Max(a.y, b.y)) / sh
			}
			hz := 0.0
			if sw := math.Min(a.w, b.w); sw > 0 {
				hz = math.Max(0, math.Min(a.x2, b.x2)-math.Max(a.x, b.x)) / sw
			}
			x[0], x[1], x[2], x[3] = float32(dx), float32(dy), float32(v), float32(hz)
			x[4], x[5] = float32(ratio(a.h, b.h)), float32(ratio(a.w, b.w))
			x[6], x[7], x[8], x[9] = b2f(a.cx < .5), b2f(b.cx < .5), b2f(a.cy < .5), b2f(b.cy < .5)
			x[10], x[11] = b2f(a.h > medH*1.4), b2f(b.h > medH*1.4)
			x[12], x[13] = float32(float64(n)/20), b2f(v >= .3 && dx > 0)
			score[i] += d.order.Prob(x)
		}
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return score[idx[a]] > score[idx[b]] })
	out := make([]Panel, n)
	for k, i := range idx {
		out[k] = ps[i]
	}
	return out
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 0 {
		return (s[m-1] + s[m]) / 2
	}
	return s[m]
}

// letterbox scales the page to fit 640 x 640, centred on grey, as the
// detector was trained, and returns the input tensor, the scale and the
// padding. mirror flips the page left to right. The scaling is Pillow's
// bilinear resize (a triangle filter widened when shrinking, rounded to
// 8 bits after each pass), so the network sees what it saw in training.
func letterbox(img image.Image, mirror bool) (*Tensor, float64, float64, float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := float64(netSize) / math.Max(float64(w), float64(h))
	nw, nh := int(math.Round(float64(w)*scale)), int(math.Round(float64(h)*scale))
	padX, padY := (netSize-nw)/2, (netSize-nh)/2
	px := pixels(img)
	if mirror {
		for y := 0; y < h; y++ {
			row := px[y*w*3 : (y+1)*w*3]
			for i, j := 0, w-1; i < j; i, j = i+1, j-1 {
				for c := 0; c < 3; c++ {
					row[i*3+c], row[j*3+c] = row[j*3+c], row[i*3+c]
				}
			}
		}
	}
	// Across, then down.
	kx := resampleWeights(w, nw)
	tmp := make([]uint8, nw*h*3)
	parallel(h, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			for x, k := range kx {
				for c := 0; c < 3; c++ {
					var v float64
					for i, wt := range k.w {
						v += wt * float64(px[(y*w+k.start+i)*3+c])
					}
					tmp[(y*nw+x)*3+c] = toByte(v)
				}
			}
		}
	})
	ky := resampleWeights(h, nh)
	t := newF(1, 3, netSize, netSize)
	for i := range t.F {
		t.F[i] = padShade
	}
	plane := netSize * netSize
	parallel(nh, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			k := ky[y]
			for x := 0; x < nw; x++ {
				o := (y+padY)*netSize + x + padX
				for c := 0; c < 3; c++ {
					var v float64
					for i, wt := range k.w {
						v += wt * float64(tmp[((k.start+i)*nw+x)*3+c])
					}
					t.F[c*plane+o] = float32(toByte(v)) / 255
				}
			}
		}
	})
	return t, scale, float64(padX), float64(padY)
}

type taps struct {
	start int
	w     []float64
}

// resampleWeights is Pillow's precompute_coeffs for the bilinear filter.
func resampleWeights(in, out int) []taps {
	scale := float64(in) / float64(out)
	fs := math.Max(scale, 1)
	support := fs // the bilinear filter's support is 1
	res := make([]taps, out)
	for o := 0; o < out; o++ {
		center := (float64(o) + 0.5) * scale
		xmin := max(0, int(center-support+0.5))
		xmax := min(in, int(center+support+0.5))
		ws := make([]float64, xmax-xmin)
		var sum float64
		for i := range ws {
			d := math.Abs((float64(i+xmin) - center + 0.5) / fs)
			if d < 1 {
				ws[i] = 1 - d
			}
			sum += ws[i]
		}
		if sum != 0 {
			for i := range ws {
				ws[i] /= sum
			}
		}
		res[o] = taps{start: xmin, w: ws}
	}
	return res
}

func toByte(v float64) uint8 {
	v = math.Round(v)
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func clampInt(v, lo, hi int) int { return min(max(v, lo), hi) }

// pixels returns the image as RGB bytes, row by row.
func pixels(img image.Image) []uint8 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]uint8, w*h*3)
	switch m := img.(type) {
	case *image.YCbCr:
		parallel(h, func(lo, hi int) {
			for y := lo; y < hi; y++ {
				for x := 0; x < w; x++ {
					r, g, bl, _ := m.At(b.Min.X+x, b.Min.Y+y).RGBA()
					i := (y*w + x) * 3
					out[i], out[i+1], out[i+2] = uint8(r>>8), uint8(g>>8), uint8(bl>>8)
				}
			}
		})
	default:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				i := (y*w + x) * 3
				out[i], out[i+1], out[i+2] = uint8(r>>8), uint8(g>>8), uint8(bl>>8)
			}
		}
	}
	return out
}
