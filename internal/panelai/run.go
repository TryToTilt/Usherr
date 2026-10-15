package panelai

import (
	"errors"
	"fmt"
)

// Model is a loaded ONNX graph, ready to run.
type Model struct {
	g    *graph
	uses map[string]int // how many nodes read each value (to free it after its last use)
}

// Load reads an ONNX model.
func Load(b []byte) (*Model, error) {
	g, err := parseModel(b)
	if err != nil {
		return nil, err
	}
	m := &Model{g: g, uses: map[string]int{}}
	for _, n := range g.nodes {
		for _, in := range n.inputs {
			m.uses[in]++
		}
	}
	for _, o := range g.outputs {
		m.uses[o]++
	}
	return m, nil
}

// Run evaluates the graph for the given inputs and returns its outputs.
func (m *Model) Run(inputs map[string]*Tensor) (map[string]*Tensor, error) {
	vals := map[string]*Tensor{}
	left := map[string]int{}
	for k, v := range inputs {
		vals[k] = v
	}
	for k, v := range m.uses {
		left[k] = v
	}
	get := func(name string) *Tensor {
		if name == "" {
			return nil
		}
		if t, ok := vals[name]; ok {
			return t
		}
		return m.g.inits[name]
	}
	for _, n := range m.g.nodes {
		in := make([]*Tensor, len(n.inputs))
		for i, name := range n.inputs {
			in[i] = get(name)
			if in[i] == nil && name != "" {
				return nil, fmt.Errorf("%s: input %q missing", n.op, name)
			}
		}
		out, err := apply(n, in)
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %w", n.op, n.outputs[0], err)
		}
		for i, name := range n.outputs {
			if i < len(out) {
				vals[name] = out[i]
			}
		}
		for _, name := range n.inputs {
			if left[name]--; left[name] == 0 {
				delete(vals, name)
			}
		}
	}
	res := map[string]*Tensor{}
	for _, o := range m.g.outputs {
		res[o] = vals[o]
	}
	return res, nil
}

func apply(n node, in []*Tensor) ([]*Tensor, error) {
	one := func(t *Tensor, err error) ([]*Tensor, error) { return []*Tensor{t}, err }
	a := n.attrs
	switch n.op {
	case "Conv":
		var b *Tensor
		if len(in) > 2 {
			b = in[2]
		}
		return one(conv2d(in[0], in[1], b, a))
	case "Sigmoid":
		return one(sigmoid(in[0]), nil)
	case "Add", "Sub", "Mul", "Div":
		return one(elementwise(in[0], in[1], n.op))
	case "Concat":
		return one(concat(in, normAxis(a["axis"].i, len(in[0].Shape))), nil)
	case "Split":
		axis := normAxis(a["axis"].i, len(in[0].Shape))
		var sizes []int
		if len(in) > 1 && in[1] != nil {
			for _, s := range in[1].ints() {
				sizes = append(sizes, int(s))
			}
		} else if s := a["split"].ints; len(s) > 0 {
			for _, v := range s {
				sizes = append(sizes, int(v))
			}
		} else {
			k := len(n.outputs)
			for i := 0; i < k; i++ {
				sizes = append(sizes, in[0].Shape[axis]/k)
			}
		}
		return split(in[0], axis, sizes), nil
	case "MaxPool":
		return one(maxPool(in[0], a), nil)
	case "Resize":
		if len(in) < 3 || in[2] == nil || len(in[2].F) != 4 {
			return nil, errors.New("only resizing by scales is supported")
		}
		if a["mode"].s != "nearest" {
			return nil, fmt.Errorf("resize mode %q isn't supported", a["mode"].s)
		}
		return one(resizeNearest(in[0], in[2].F), nil)
	case "Shape":
		v := make([]int64, len(in[0].Shape))
		for i, d := range in[0].Shape {
			v[i] = int64(d)
		}
		return one(newI([]int{len(v)}, v), nil)
	case "Gather":
		return one(gather(in[0], in[1], normAxis(a["axis"].i, len(in[0].Shape))), nil)
	case "Unsqueeze":
		axes := a["axes"].ints
		if len(in) > 1 {
			axes = in[1].ints()
		}
		shape := append([]int(nil), in[0].Shape...)
		r := len(shape) + len(axes)
		for _, ax := range axes {
			i := normAxis(ax, r)
			shape = append(shape[:i], append([]int{1}, shape[i:]...)...)
		}
		t := *in[0]
		t.Shape = shape
		return one(&t, nil)
	case "Cast":
		t := &Tensor{Shape: in[0].Shape}
		switch a["to"].i {
		case dtInt64, dtInt32, dtBool:
			t.I, t.isInt = in[0].ints(), true
		default:
			t.F = floatsOf(in[0])
		}
		return one(t, nil)
	case "Range":
		if in[0].isInt {
			s, e, d := in[0].I[0], in[1].ints()[0], in[2].ints()[0]
			var v []int64
			for x := s; (d > 0 && x < e) || (d < 0 && x > e); x += d {
				v = append(v, x)
			}
			return one(newI([]int{len(v)}, v), nil)
		}
		s, e, d := in[0].F[0], floatsOf(in[1])[0], floatsOf(in[2])[0]
		var v []float32
		for x := s; (d > 0 && x < e) || (d < 0 && x > e); x += d {
			v = append(v, x)
		}
		return one(&Tensor{Shape: []int{len(v)}, F: v}, nil)
	case "ConstantOfShape":
		shape := make([]int, 0)
		for _, d := range in[0].ints() {
			shape = append(shape, int(d))
		}
		val := a["value"].t
		if val != nil && val.isInt {
			t := newI(shape, make([]int64, (&Tensor{Shape: shape}).size()))
			for i := range t.I {
				t.I[i] = val.I[0]
			}
			return one(t, nil)
		}
		t := newF(shape...)
		if val != nil {
			for i := range t.F {
				t.F[i] = val.F[0]
			}
		}
		return one(t, nil)
	case "Reshape":
		want := in[1].ints()
		shape := make([]int, len(want))
		known, unknown := 1, -1
		for i, d := range want {
			switch {
			case d == 0:
				shape[i] = in[0].Shape[i]
			case d == -1:
				unknown = i
				continue
			default:
				shape[i] = int(d)
			}
			known *= shape[i]
		}
		if unknown >= 0 {
			shape[unknown] = in[0].size() / known
		}
		t := *in[0]
		t.Shape = shape
		return one(&t, nil)
	case "Expand":
		return one(expand(in[0], in[1].ints()))
	case "Transpose":
		return one(transpose(in[0], a["perm"].ints), nil)
	case "Softmax":
		ax := int64(-1)
		if v, ok := a["axis"]; ok {
			ax = v.i
		}
		return one(softmax(in[0], normAxis(ax, len(in[0].Shape))), nil)
	case "Slice":
		var axes, steps []int64
		if len(in) > 3 && in[3] != nil {
			axes = in[3].ints()
		}
		if len(in) > 4 && in[4] != nil {
			steps = in[4].ints()
		}
		return one(slice(in[0], in[1].ints(), in[2].ints(), axes, steps))
	case "Identity":
		return one(in[0], nil)
	}
	return nil, fmt.Errorf("operation %s isn't supported", n.op)
}
