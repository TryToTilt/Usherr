package panelai

import (
	"fmt"
	"math"
	"runtime"
	"sync"
)

// Tensor is an n-dimensional array, row-major: floats, or integers (shapes
// and indices).
type Tensor struct {
	Shape []int
	F     []float32
	I     []int64
	isInt bool
}

func (t *Tensor) size() int {
	n := 1
	for _, d := range t.Shape {
		n *= d
	}
	return n
}

func newF(shape ...int) *Tensor {
	t := &Tensor{Shape: shape}
	t.F = make([]float32, t.size())
	return t
}

func newI(shape []int, v []int64) *Tensor { return &Tensor{Shape: shape, I: v, isInt: true} }

// ints reads a tensor of integers (or whole floats) as a list.
func (t *Tensor) ints() []int64 {
	if t.isInt {
		return t.I
	}
	out := make([]int64, len(t.F))
	for i, v := range t.F {
		out[i] = int64(v)
	}
	return out
}

// workers is how many goroutines the heavy operations use.
var workers = max(1, runtime.GOMAXPROCS(0))

// parallel runs f over [0,n) in chunks, on all workers.
func parallel(n int, f func(lo, hi int)) {
	w := min(workers, n)
	if w <= 1 {
		f(0, n)
		return
	}
	var wg sync.WaitGroup
	step := (n + w - 1) / w
	for lo := 0; lo < n; lo += step {
		hi := min(n, lo+step)
		wg.Add(1)
		go func() {
			defer wg.Done()
			f(lo, hi)
		}()
	}
	wg.Wait()
}

// conv2d is Conv for one image (N=1), group 1.
func conv2d(x, w, b *Tensor, a map[string]attr) (*Tensor, error) {
	if len(x.Shape) != 4 || x.Shape[0] != 1 || len(w.Shape) != 4 {
		return nil, fmt.Errorf("conv: unsupported shapes %v %v", x.Shape, w.Shape)
	}
	if g, ok := a["group"]; ok && g.i != 1 {
		return nil, fmt.Errorf("conv: groups aren't supported")
	}
	C, H, W := x.Shape[1], x.Shape[2], x.Shape[3]
	OC, kh, kw := w.Shape[0], w.Shape[2], w.Shape[3]
	sh, sw := 1, 1
	if s := a["strides"].ints; len(s) == 2 {
		sh, sw = int(s[0]), int(s[1])
	}
	pt, pl, pb, pr := 0, 0, 0, 0
	if p := a["pads"].ints; len(p) == 4 {
		pt, pl, pb, pr = int(p[0]), int(p[1]), int(p[2]), int(p[3])
	}
	OH := (H+pt+pb-kh)/sh + 1
	OW := (W+pl+pr-kw)/sw + 1
	P := OH * OW
	K := C * kh * kw
	// The input laid out as a K x P matrix (im2col); a 1x1, stride-1 conv
	// with no padding uses the input as it is.
	var col []float32
	if kh == 1 && kw == 1 && sh == 1 && sw == 1 && pt == 0 && pl == 0 {
		col = x.F
	} else {
		col = make([]float32, K*P)
		parallel(K, func(lo, hi int) {
			for k := lo; k < hi; k++ {
				c, rem := k/(kh*kw), k%(kh*kw)
				ky, kx := rem/kw, rem%kw
				row := col[k*P : (k+1)*P]
				src := x.F[c*H*W : (c+1)*H*W]
				for oy := 0; oy < OH; oy++ {
					iy := oy*sh - pt + ky
					out := row[oy*OW : (oy+1)*OW]
					if iy < 0 || iy >= H {
						clear(out)
						continue
					}
					line := src[iy*W : (iy+1)*W]
					for ox := range out {
						ix := ox*sw - pl + kx
						if ix < 0 || ix >= W {
							out[ox] = 0
						} else {
							out[ox] = line[ix]
						}
					}
				}
			}
		})
	}
	y := newF(1, OC, OH, OW)
	// y (OC x P) = w (OC x K) * col (K x P), in column blocks that stay in
	// cache, the blocks shared out over the workers.
	const blk = 256
	nb := (P + blk - 1) / blk
	parallel(OC*nb, func(lo, hi int) {
		for job := lo; job < hi; job++ {
			oc, bi := job/nb, job%nb
			p0, p1 := bi*blk, min(P, (bi+1)*blk)
			out := y.F[oc*P+p0 : oc*P+p1]
			var bias float32
			if b != nil {
				bias = b.F[oc]
			}
			for i := range out {
				out[i] = bias
			}
			wr := w.F[oc*K : (oc+1)*K]
			k := 0
			for ; k+4 <= K; k += 4 {
				w0, w1, w2, w3 := wr[k], wr[k+1], wr[k+2], wr[k+3]
				r0 := col[k*P+p0 : k*P+p1]
				r1 := col[(k+1)*P+p0 : (k+1)*P+p1]
				r2 := col[(k+2)*P+p0 : (k+2)*P+p1]
				r3 := col[(k+3)*P+p0 : (k+3)*P+p1]
				r1, r2, r3 = r1[:len(r0)], r2[:len(r0)], r3[:len(r0)]
				o := out[:len(r0)]
				for i := range o {
					o[i] += w0*r0[i] + w1*r1[i] + w2*r2[i] + w3*r3[i]
				}
			}
			for ; k < K; k++ {
				wk := wr[k]
				r := col[k*P+p0 : k*P+p1]
				o := out[:len(r)]
				for i := range o {
					o[i] += wk * r[i]
				}
			}
		}
	})
	return y, nil
}

func sigmoid(x *Tensor) *Tensor {
	y := newF(x.Shape...)
	parallel(len(x.F)/4096+1, func(lo, hi int) {
		for i := lo * 4096; i < min(hi*4096, len(x.F)); i++ {
			y.F[i] = float32(1 / (1 + math.Exp(-float64(x.F[i]))))
		}
	})
	return y
}

// broadcast shapes per the ONNX (numpy) rules.
func broadcastShape(a, b []int) ([]int, error) {
	n := max(len(a), len(b))
	out := make([]int, n)
	for i := 0; i < n; i++ {
		da, db := 1, 1
		if j := len(a) - n + i; j >= 0 {
			da = a[j]
		}
		if j := len(b) - n + i; j >= 0 {
			db = b[j]
		}
		switch {
		case da == db, db == 1:
			out[i] = da
		case da == 1:
			out[i] = db
		default:
			return nil, fmt.Errorf("can't broadcast %v and %v", a, b)
		}
	}
	return out, nil
}

// strideFor gives the strides of shape src as seen inside out (0 where src
// is broadcast).
func strideFor(src, out []int) []int {
	st := make([]int, len(out))
	acc := 1
	for i := len(out) - 1; i >= 0; i-- {
		j := len(src) - len(out) + i
		if j >= 0 && src[j] != 1 {
			st[i] = acc
		}
		if j >= 0 {
			acc *= src[j]
		}
	}
	return st
}

// elementwise applies an element-wise operation with broadcasting.
func elementwise(a, b *Tensor, op string) (*Tensor, error) {
	shape, err := broadcastShape(a.Shape, b.Shape)
	if err != nil {
		return nil, err
	}
	if a.isInt && b.isInt {
		fa, fb := a.I, b.I
		out := newI(shape, make([]int64, (&Tensor{Shape: shape}).size()))
		sa, sb := strideFor(a.Shape, shape), strideFor(b.Shape, shape)
		eachIndex(shape, func(i int, idx []int) {
			x, y := fa[offset(idx, sa)], fb[offset(idx, sb)]
			switch op {
			case "Add":
				out.I[i] = x + y
			case "Sub":
				out.I[i] = x - y
			case "Mul":
				out.I[i] = x * y
			case "Div":
				out.I[i] = x / y
			}
		})
		return out, nil
	}
	af, bf := floatsOf(a), floatsOf(b)
	out := newF(shape...)
	f := func(x, y float32) float32 {
		switch op {
		case "Add":
			return x + y
		case "Sub":
			return x - y
		case "Mul":
			return x * y
		default:
			return x / y
		}
	}
	if sameShape(a.Shape, shape) && sameShape(b.Shape, shape) {
		parallel(len(out.F)/4096+1, func(lo, hi int) {
			for i := lo * 4096; i < min(hi*4096, len(out.F)); i++ {
				out.F[i] = f(af[i], bf[i])
			}
		})
		return out, nil
	}
	sa, sb := strideFor(a.Shape, shape), strideFor(b.Shape, shape)
	eachIndex(shape, func(i int, idx []int) {
		out.F[i] = f(af[offset(idx, sa)], bf[offset(idx, sb)])
	})
	return out, nil
}

func floatsOf(t *Tensor) []float32 {
	if !t.isInt {
		return t.F
	}
	f := make([]float32, len(t.I))
	for i, v := range t.I {
		f[i] = float32(v)
	}
	return f
}

func sameShape(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func offset(idx, st []int) int {
	o := 0
	for i, v := range idx {
		o += v * st[i]
	}
	return o
}

// eachIndex calls f for every position of shape, in row-major order.
func eachIndex(shape []int, f func(i int, idx []int)) {
	n := (&Tensor{Shape: shape}).size()
	idx := make([]int, len(shape))
	for i := 0; i < n; i++ {
		f(i, idx)
		for d := len(shape) - 1; d >= 0; d-- {
			idx[d]++
			if idx[d] < shape[d] {
				break
			}
			idx[d] = 0
		}
	}
}

func normAxis(axis int64, rank int) int {
	if axis < 0 {
		axis += int64(rank)
	}
	return int(axis)
}

// concat joins tensors along axis.
func concat(ts []*Tensor, axis int) *Tensor {
	shape := append([]int(nil), ts[0].Shape...)
	shape[axis] = 0
	for _, t := range ts {
		shape[axis] += t.Shape[axis]
	}
	outer := 1
	for _, d := range shape[:axis] {
		outer *= d
	}
	inner := 1
	for _, d := range shape[axis+1:] {
		inner *= d
	}
	isInt := ts[0].isInt
	out := &Tensor{Shape: shape, isInt: isInt}
	if isInt {
		out.I = make([]int64, out.size())
	} else {
		out.F = make([]float32, out.size())
	}
	pos := 0
	rowLen := shape[axis] * inner
	for _, t := range ts {
		n := t.Shape[axis] * inner
		for o := 0; o < outer; o++ {
			if isInt {
				copy(out.I[o*rowLen+pos:], t.I[o*n:(o+1)*n])
			} else {
				copy(out.F[o*rowLen+pos:], t.F[o*n:(o+1)*n])
			}
		}
		pos += n
	}
	return out
}

// split cuts x along axis into pieces of the given sizes.
func split(x *Tensor, axis int, sizes []int) []*Tensor {
	outer := 1
	for _, d := range x.Shape[:axis] {
		outer *= d
	}
	inner := 1
	for _, d := range x.Shape[axis+1:] {
		inner *= d
	}
	rowLen := x.Shape[axis] * inner
	var out []*Tensor
	pos := 0
	for _, s := range sizes {
		shape := append([]int(nil), x.Shape...)
		shape[axis] = s
		t := newF(shape...)
		n := s * inner
		for o := 0; o < outer; o++ {
			copy(t.F[o*n:(o+1)*n], x.F[o*rowLen+pos:o*rowLen+pos+n])
		}
		pos += n
		out = append(out, t)
	}
	return out
}

// maxPool is MaxPool for one image, square windows.
func maxPool(x *Tensor, a map[string]attr) *Tensor {
	C, H, W := x.Shape[1], x.Shape[2], x.Shape[3]
	k := int(a["kernel_shape"].ints[0])
	s := 1
	if st := a["strides"].ints; len(st) > 0 {
		s = int(st[0])
	}
	p := 0
	if pd := a["pads"].ints; len(pd) > 0 {
		p = int(pd[0])
	}
	OH, OW := (H+2*p-k)/s+1, (W+2*p-k)/s+1
	y := newF(1, C, OH, OW)
	parallel(C, func(lo, hi int) {
		for c := lo; c < hi; c++ {
			src := x.F[c*H*W:]
			dst := y.F[c*OH*OW:]
			for oy := 0; oy < OH; oy++ {
				for ox := 0; ox < OW; ox++ {
					m := float32(math.Inf(-1))
					for ky := 0; ky < k; ky++ {
						iy := oy*s - p + ky
						if iy < 0 || iy >= H {
							continue
						}
						for kx := 0; kx < k; kx++ {
							ix := ox*s - p + kx
							if ix >= 0 && ix < W && src[iy*W+ix] > m {
								m = src[iy*W+ix]
							}
						}
					}
					dst[oy*OW+ox] = m
				}
			}
		}
	})
	return y
}

// resizeNearest is Resize in nearest mode (asymmetric, floor), by whole
// scales on the last two axes.
func resizeNearest(x *Tensor, scales []float32) *Tensor {
	C, H, W := x.Shape[1], x.Shape[2], x.Shape[3]
	OH, OW := int(float32(H)*scales[2]), int(float32(W)*scales[3])
	y := newF(1, C, OH, OW)
	for c := 0; c < C; c++ {
		for oy := 0; oy < OH; oy++ {
			iy := min(H-1, int(float32(oy)/scales[2]))
			for ox := 0; ox < OW; ox++ {
				ix := min(W-1, int(float32(ox)/scales[3]))
				y.F[(c*OH+oy)*OW+ox] = x.F[(c*H+iy)*W+ix]
			}
		}
	}
	return y
}

func transpose(x *Tensor, perm []int64) *Tensor {
	r := len(x.Shape)
	if len(perm) == 0 {
		perm = make([]int64, r)
		for i := range perm {
			perm[i] = int64(r - 1 - i)
		}
	}
	shape := make([]int, r)
	for i, p := range perm {
		shape[i] = x.Shape[p]
	}
	src := strideFor(x.Shape, x.Shape)
	st := make([]int, r)
	for i, p := range perm {
		st[i] = src[p]
	}
	out := newF(shape...)
	eachIndex(shape, func(i int, idx []int) { out.F[i] = x.F[offset(idx, st)] })
	return out
}

func softmax(x *Tensor, axis int) *Tensor {
	outer := 1
	for _, d := range x.Shape[:axis] {
		outer *= d
	}
	inner := 1
	for _, d := range x.Shape[axis+1:] {
		inner *= d
	}
	n := x.Shape[axis]
	y := newF(x.Shape...)
	for o := 0; o < outer; o++ {
		for in := 0; in < inner; in++ {
			base := o*n*inner + in
			m := float32(math.Inf(-1))
			for k := 0; k < n; k++ {
				m = max(m, x.F[base+k*inner])
			}
			var sum float64
			for k := 0; k < n; k++ {
				e := math.Exp(float64(x.F[base+k*inner] - m))
				y.F[base+k*inner] = float32(e)
				sum += e
			}
			for k := 0; k < n; k++ {
				y.F[base+k*inner] = float32(float64(y.F[base+k*inner]) / sum)
			}
		}
	}
	return y
}

// slice is Slice with starts, ends, axes and steps (step 1 only).
func slice(x *Tensor, starts, ends, axes, steps []int64) (*Tensor, error) {
	r := len(x.Shape)
	lo := make([]int, r)
	hi := append([]int(nil), x.Shape...)
	for i := range starts {
		ax := i
		if len(axes) > 0 {
			ax = normAxis(axes[i], r)
		}
		if len(steps) > 0 && steps[i] != 1 {
			return nil, fmt.Errorf("slice: steps other than 1 aren't supported")
		}
		d := int64(x.Shape[ax])
		s, e := starts[i], ends[i]
		if s < 0 {
			s += d
		}
		if e < 0 {
			e += d
		}
		lo[ax], hi[ax] = int(min(max(s, 0), d)), int(min(max(e, 0), d))
	}
	shape := make([]int, r)
	for i := range shape {
		shape[i] = max(0, hi[i]-lo[i])
	}
	st := strideFor(x.Shape, x.Shape)
	out := &Tensor{Shape: shape, isInt: x.isInt}
	if x.isInt {
		out.I = make([]int64, out.size())
	} else {
		out.F = make([]float32, out.size())
	}
	src := make([]int, r)
	eachIndex(shape, func(i int, idx []int) {
		for d := range idx {
			src[d] = idx[d] + lo[d]
		}
		if x.isInt {
			out.I[i] = x.I[offset(src, st)]
		} else {
			out.F[i] = x.F[offset(src, st)]
		}
	})
	return out, nil
}

// expand is Expand: broadcast x to shape.
func expand(x *Tensor, shape []int64) (*Tensor, error) {
	want := make([]int, len(shape))
	for i, v := range shape {
		want[i] = int(v)
	}
	out, err := broadcastShape(x.Shape, want)
	if err != nil {
		return nil, err
	}
	st := strideFor(x.Shape, out)
	t := &Tensor{Shape: out, isInt: x.isInt}
	if x.isInt {
		t.I = make([]int64, t.size())
	} else {
		t.F = make([]float32, t.size())
	}
	eachIndex(out, func(i int, idx []int) {
		if x.isInt {
			t.I[i] = x.I[offset(idx, st)]
		} else {
			t.F[i] = x.F[offset(idx, st)]
		}
	})
	return t, nil
}

// gather is Gather with integer indices along axis.
func gather(x *Tensor, ind *Tensor, axis int) *Tensor {
	outer := 1
	for _, d := range x.Shape[:axis] {
		outer *= d
	}
	inner := 1
	for _, d := range x.Shape[axis+1:] {
		inner *= d
	}
	idx := ind.ints()
	shape := append(append(append([]int(nil), x.Shape[:axis]...), ind.Shape...), x.Shape[axis+1:]...)
	out := &Tensor{Shape: shape, isInt: x.isInt}
	if x.isInt {
		out.I = make([]int64, out.size())
	} else {
		out.F = make([]float32, out.size())
	}
	n := x.Shape[axis]
	pos := 0
	for o := 0; o < outer; o++ {
		for _, j := range idx {
			if j < 0 {
				j += int64(n)
			}
			src := (o*n + int(j)) * inner
			if x.isInt {
				copy(out.I[pos:pos+inner], x.I[src:src+inner])
			} else {
				copy(out.F[pos:pos+inner], x.F[src:src+inner])
			}
			pos += inner
		}
	}
	return out
}
