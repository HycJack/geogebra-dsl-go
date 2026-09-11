// Package deps checks the dependency graph for cycles and produces a
// topological execution order. Fail-closed with the dependency quasi-order.
package deps

import (
	"github.com/you/geogebra-dsl-go/internal/diag"
	"github.com/you/geogebra-dsl-go/internal/ir"
)

// Order topologically sorts the graph by refs. Returns a valid build order and
// any cycle problems (fail-closed: a cycle ⇒ non-empty problems and no order is
// usable, though a best-effort partial order is still returned).
func Order(g *ir.Graph) ([]string, []diag.Problem) {
	// count in-degrees: for each object, each distinct ref that exists is a
	// "must come before" dependency (edge ref -> object). Refs are deduplicated
	// so a repeated reference (e.g. Line(A,A)) is not double-counted.
	in := map[string]int{}
	for id := range g.Objects {
		in[id] = 0
	}
	for id := range g.Objects {
		o := g.Objects[id]
		seen := map[string]bool{}
		for _, r := range o.Refs {
			if _, ok := g.Objects[r]; ok && !seen[r] {
				seen[r] = true
				in[id]++
			}
		}
	}
	queue := []string{}
	for id, n := range in {
		if n == 0 {
			queue = append(queue, id)
		}
	}
	var order []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		// does any object reference id? decrement their in-degree.
		for otherID := range g.Objects {
			other := g.Objects[otherID]
			if contains(other.Refs, id) {
				in[otherID]--
				if in[otherID] == 0 {
					queue = append(queue, otherID)
				}
			}
		}
	}
	var probs []diag.Problem
	if len(order) != len(g.Objects) {
		// cycle detected: remaining nodes have in>0
		var cyc []string
		for id, n := range in {
			if n > 0 {
				cyc = append(cyc, id)
			}
		}
		probs = append(probs, diag.Problem{
			Code: diag.CodeDepCycle,
			Msg:  "依赖存在环，无法确定构建顺序：" + join(cyc),
			Obj:  first(cyc),
		})
	}
	return order, probs
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}
