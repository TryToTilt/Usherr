package panelai

import (
	"errors"
	"math"
)

// Trees is a binary tree-ensemble classifier (ONNX TreeEnsembleClassifier,
// as scikit-learn's gradient boosting exports it): each tree adds the score
// of the leaf a feature row ends in, and the logistic function turns the
// total into the probability of the second class.
type Trees struct {
	roots []int // the first node of each tree
	feat  []int
	value []float32
	leaf  []bool
	yes   []int // node index when feature <= value
	no    []int
	score []float64 // a leaf's weight
	base  float64
}

// LoadTrees reads a tree-ensemble classifier model.
func LoadTrees(b []byte) (*Trees, error) {
	g, err := parseModel(b)
	if err != nil {
		return nil, err
	}
	var n *node
	for i := range g.nodes {
		if g.nodes[i].op == "TreeEnsembleClassifier" {
			n = &g.nodes[i]
		}
	}
	if n == nil {
		return nil, errors.New("no tree ensemble in the model")
	}
	a := n.attrs
	if a["post_transform"].s != "LOGISTIC" {
		return nil, errors.New("only logistic tree ensembles are supported")
	}
	tree, id := a["nodes_treeids"].ints, a["nodes_nodeids"].ints
	// Nodes are found by (tree, node id).
	key := func(t, n int64) int64 { return t<<32 | n }
	at := make(map[int64]int, len(id))
	for i := range id {
		at[key(tree[i], id[i])] = i
	}
	t := &Trees{
		feat: make([]int, len(id)), value: a["nodes_values"].floats, leaf: make([]bool, len(id)),
		yes: make([]int, len(id)), no: make([]int, len(id)), score: make([]float64, len(id)),
	}
	if len(a["base_values"].floats) > 0 {
		t.base = float64(a["base_values"].floats[0])
	}
	seen := map[int64]bool{}
	for i := range id {
		t.feat[i] = int(a["nodes_featureids"].ints[i])
		switch a["nodes_modes"].strs[i] {
		case "LEAF":
			t.leaf[i] = true
		case "BRANCH_LEQ":
		default:
			return nil, errors.New("only <= branches are supported")
		}
		t.yes[i] = at[key(tree[i], a["nodes_truenodeids"].ints[i])]
		t.no[i] = at[key(tree[i], a["nodes_falsenodeids"].ints[i])]
		if !seen[tree[i]] {
			seen[tree[i]] = true
			t.roots = append(t.roots, at[key(tree[i], 0)])
		}
	}
	ct, cn, cw := a["class_treeids"].ints, a["class_nodeids"].ints, a["class_weights"].floats
	for i := range ct {
		t.score[at[key(ct[i], cn[i])]] += float64(cw[i])
	}
	return t, nil
}

// Prob is the probability of the second class ("a is read before b").
func (t *Trees) Prob(x []float32) float64 {
	s := t.base
	for _, r := range t.roots {
		i := r
		for !t.leaf[i] {
			if x[t.feat[i]] <= t.value[i] {
				i = t.yes[i]
			} else {
				i = t.no[i]
			}
		}
		s += t.score[i]
	}
	return 1 / (1 + math.Exp(-s))
}
