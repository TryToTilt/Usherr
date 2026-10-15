package panelai

import (
	"math"
	"math/rand"
	"os"
	"testing"
)

// conv2d against the plain definition, with stride and padding.
func TestConvMatchesTheDefinition(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	x, w, b := newF(1, 3, 9, 7), newF(4, 3, 3, 3), newF(4)
	for _, tt := range []*Tensor{x, w, b} {
		for i := range tt.F {
			tt.F[i] = r.Float32()*2 - 1
		}
	}
	for _, stride := range []int64{1, 2} {
		a := map[string]attr{"strides": {ints: []int64{stride, stride}}, "pads": {ints: []int64{1, 1, 1, 1}}}
		y, err := conv2d(x, w, b, a)
		if err != nil {
			t.Fatal(err)
		}
		OH, OW := y.Shape[2], y.Shape[3]
		for oc := 0; oc < 4; oc++ {
			for oy := 0; oy < OH; oy++ {
				for ox := 0; ox < OW; ox++ {
					want := float64(b.F[oc])
					for c := 0; c < 3; c++ {
						for ky := 0; ky < 3; ky++ {
							for kx := 0; kx < 3; kx++ {
								iy, ix := oy*int(stride)-1+ky, ox*int(stride)-1+kx
								if iy >= 0 && iy < 9 && ix >= 0 && ix < 7 {
									want += float64(w.F[((oc*3+c)*3+ky)*3+kx]) * float64(x.F[(c*9+iy)*7+ix])
								}
							}
						}
					}
					if got := y.F[(oc*OH+oy)*OW+ox]; math.Abs(float64(got)-want) > 1e-4 {
						t.Fatalf("stride %d at %d,%d,%d: %v, want %v", stride, oc, oy, ox, got, want)
					}
				}
			}
		}
	}
}

func TestBroadcast(t *testing.T) {
	a := &Tensor{Shape: []int{2, 3}, F: []float32{1, 2, 3, 4, 5, 6}}
	b := &Tensor{Shape: []int{3}, F: []float32{10, 20, 30}}
	y, err := elementwise(a, b, "Mul")
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{10, 40, 90, 40, 100, 180}
	for i := range want {
		if y.F[i] != want[i] {
			t.Fatalf("%v", y.F)
		}
	}
}

func TestResampleWeightsSumToOne(t *testing.T) {
	for _, k := range resampleWeights(1987, 640) {
		var s float64
		for _, w := range k.w {
			s += w
		}
		if math.Abs(s-1) > 1e-9 {
			t.Fatalf("weights sum to %v", s)
		}
	}
}

// With the real models (MEDIARIUM_PANEL_MODELS=<folder>): a blank page has
// no panels.
func TestModelsLoad(t *testing.T) {
	dir := os.Getenv("MEDIARIUM_PANEL_MODELS")
	if dir == "" {
		t.Skip("set MEDIARIUM_PANEL_MODELS to the folder with the two model files")
	}
	det, err := os.ReadFile(dir + "/best.onnx")
	if err != nil {
		t.Fatal(err)
	}
	ord, err := os.ReadFile(dir + "/panel-order-model.onnx")
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDetector(det, ord)
	if err != nil {
		t.Fatal(err)
	}
	if p := d.order.Prob(make([]float32, 14)); p <= 0 || p >= 1 {
		t.Fatalf("order probability %v", p)
	}
}
