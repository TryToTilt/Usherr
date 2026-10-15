// Package panelai finds comic panels with a small AI model, inside
// Mediarium (home fork). It runs ONNX models in pure Go: just the
// operations a YOLOv8 detector and a tree-ensemble classifier use, so the
// build stays pure Go (no C++ runtime) on every platform. The models are the
// Inkwell panel models (huggingface.co/cedarrapidsboy/inkwell-panel-models):
// a YOLOv8-nano panel detector (AGPL-3.0, like Mediarium) and a reading-order
// classifier (MIT). They're downloaded on first use, not built in.
package panelai

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// This file reads the parts of an ONNX model file (a protocol buffer) that
// the runner needs: the graph's nodes, its stored tensors, and the node
// attributes. Field numbers are from onnx.proto.

type attr struct {
	f      float32
	i      int64
	s      string
	t      *Tensor
	floats []float32
	ints   []int64
	strs   []string
}

type node struct {
	op      string
	inputs  []string
	outputs []string
	attrs   map[string]attr
}

type graph struct {
	nodes   []node
	inits   map[string]*Tensor
	inputs  []string
	outputs []string
}

// pb walks one protocol buffer message.
type pb struct {
	b []byte
}

func (p *pb) varint() (uint64, error) {
	v, n := binary.Uvarint(p.b)
	if n <= 0 {
		return 0, errors.New("onnx: bad varint")
	}
	p.b = p.b[n:]
	return v, nil
}

// next returns the next field: its number, wire type, and the value (a
// varint, a fixed number or the bytes of a length-delimited field).
func (p *pb) next() (field int, wire int, v uint64, data []byte, err error) {
	key, err := p.varint()
	if err != nil {
		return
	}
	field, wire = int(key>>3), int(key&7)
	switch wire {
	case 0:
		v, err = p.varint()
	case 1:
		if len(p.b) < 8 {
			return 0, 0, 0, nil, errors.New("onnx: short fixed64")
		}
		v = binary.LittleEndian.Uint64(p.b)
		p.b = p.b[8:]
	case 2:
		var n uint64
		if n, err = p.varint(); err != nil {
			return
		}
		if uint64(len(p.b)) < n {
			return 0, 0, 0, nil, errors.New("onnx: short field")
		}
		data, p.b = p.b[:n], p.b[n:]
	case 5:
		if len(p.b) < 4 {
			return 0, 0, 0, nil, errors.New("onnx: short fixed32")
		}
		v = uint64(binary.LittleEndian.Uint32(p.b))
		p.b = p.b[4:]
	default:
		err = fmt.Errorf("onnx: wire type %d", wire)
	}
	return
}

func parseModel(b []byte) (*graph, error) {
	p := pb{b}
	for len(p.b) > 0 {
		f, _, _, data, err := p.next()
		if err != nil {
			return nil, err
		}
		if f == 7 { // ModelProto.graph
			return parseGraph(data)
		}
	}
	return nil, errors.New("onnx: no graph")
}

func parseGraph(b []byte) (*graph, error) {
	g := &graph{inits: map[string]*Tensor{}}
	p := pb{b}
	for len(p.b) > 0 {
		f, _, _, data, err := p.next()
		if err != nil {
			return nil, err
		}
		switch f {
		case 1:
			n, err := parseNode(data)
			if err != nil {
				return nil, err
			}
			g.nodes = append(g.nodes, n)
		case 5:
			name, t, err := parseTensor(data)
			if err != nil {
				return nil, err
			}
			g.inits[name] = t
		case 11, 12:
			name := valueInfoName(data)
			if f == 11 {
				g.inputs = append(g.inputs, name)
			} else {
				g.outputs = append(g.outputs, name)
			}
		}
	}
	// Graph inputs that are also stored tensors aren't real inputs.
	var in []string
	for _, n := range g.inputs {
		if _, ok := g.inits[n]; !ok {
			in = append(in, n)
		}
	}
	g.inputs = in
	return g, nil
}

func valueInfoName(b []byte) string {
	p := pb{b}
	for len(p.b) > 0 {
		f, _, _, data, err := p.next()
		if err != nil {
			return ""
		}
		if f == 1 {
			return string(data)
		}
	}
	return ""
}

func parseNode(b []byte) (node, error) {
	n := node{attrs: map[string]attr{}}
	p := pb{b}
	for len(p.b) > 0 {
		f, _, _, data, err := p.next()
		if err != nil {
			return n, err
		}
		switch f {
		case 1:
			n.inputs = append(n.inputs, string(data))
		case 2:
			n.outputs = append(n.outputs, string(data))
		case 4:
			n.op = string(data)
		case 5:
			name, a, err := parseAttr(data)
			if err != nil {
				return n, err
			}
			n.attrs[name] = a
		}
	}
	return n, nil
}

func parseAttr(b []byte) (string, attr, error) {
	var name string
	var a attr
	p := pb{b}
	for len(p.b) > 0 {
		f, wire, v, data, err := p.next()
		if err != nil {
			return "", a, err
		}
		switch f {
		case 1:
			name = string(data)
		case 2:
			a.f = math.Float32frombits(uint32(v))
		case 3:
			a.i = int64(v)
		case 4:
			a.s = string(data)
		case 5:
			_, t, err := parseTensor(data)
			if err != nil {
				return "", a, err
			}
			a.t = t
		case 7:
			if wire == 2 {
				for i := 0; i+4 <= len(data); i += 4 {
					a.floats = append(a.floats, math.Float32frombits(binary.LittleEndian.Uint32(data[i:])))
				}
			} else {
				a.floats = append(a.floats, math.Float32frombits(uint32(v)))
			}
		case 8:
			if wire == 2 {
				q := pb{data}
				for len(q.b) > 0 {
					x, err := q.varint()
					if err != nil {
						return "", a, err
					}
					a.ints = append(a.ints, int64(x))
				}
			} else {
				a.ints = append(a.ints, int64(v))
			}
		case 9:
			a.strs = append(a.strs, string(data))
		}
	}
	return name, a, nil
}

// ONNX element types this runner reads.
const (
	dtFloat  = 1
	dtInt32  = 6
	dtInt64  = 7
	dtBool   = 9
	dtDouble = 11
)

func parseTensor(b []byte) (string, *Tensor, error) {
	var name string
	var dims []int
	var dtype int
	var raw []byte
	var floats []float32
	var ints []int64
	p := pb{b}
	for len(p.b) > 0 {
		f, wire, v, data, err := p.next()
		if err != nil {
			return "", nil, err
		}
		switch f {
		case 1:
			if wire == 2 {
				q := pb{data}
				for len(q.b) > 0 {
					x, err := q.varint()
					if err != nil {
						return "", nil, err
					}
					dims = append(dims, int(int64(x)))
				}
			} else {
				dims = append(dims, int(int64(v)))
			}
		case 2:
			dtype = int(v)
		case 4:
			if wire == 2 {
				for i := 0; i+4 <= len(data); i += 4 {
					floats = append(floats, math.Float32frombits(binary.LittleEndian.Uint32(data[i:])))
				}
			} else {
				floats = append(floats, math.Float32frombits(uint32(v)))
			}
		case 5, 7:
			if wire == 2 {
				q := pb{data}
				for len(q.b) > 0 {
					x, err := q.varint()
					if err != nil {
						return "", nil, err
					}
					ints = append(ints, int64(x))
				}
			} else {
				ints = append(ints, int64(v))
			}
		case 8:
			name = string(data)
		case 9:
			raw = data
		}
	}
	t := &Tensor{Shape: dims}
	n := t.size()
	switch dtype {
	case dtFloat:
		if raw != nil {
			floats = make([]float32, len(raw)/4)
			for i := range floats {
				floats[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
			}
		}
		t.F = floats
	case dtDouble:
		t.F = make([]float32, len(raw)/8)
		for i := range t.F {
			t.F[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(raw[i*8:])))
		}
	case dtInt64:
		if raw != nil {
			ints = make([]int64, len(raw)/8)
			for i := range ints {
				ints[i] = int64(binary.LittleEndian.Uint64(raw[i*8:]))
			}
		}
		t.I, t.isInt = ints, true
	case dtInt32:
		if raw != nil {
			ints = make([]int64, len(raw)/4)
			for i := range ints {
				ints[i] = int64(int32(binary.LittleEndian.Uint32(raw[i*4:])))
			}
		}
		t.I, t.isInt = ints, true
	case dtBool:
		t.I, t.isInt = make([]int64, len(raw)), true
		for i, c := range raw {
			t.I[i] = int64(c)
		}
	default:
		return "", nil, fmt.Errorf("onnx: tensor %q has element type %d", name, dtype)
	}
	if (t.isInt && len(t.I) != n) || (!t.isInt && len(t.F) != n) {
		return "", nil, fmt.Errorf("onnx: tensor %q has the wrong amount of data", name)
	}
	return name, t, nil
}
