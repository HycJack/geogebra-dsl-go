// Package reach decides what must be constructible (the "goals") and verifies
// each is present in the graph. For IR input the goals come from the input
// goals[]; for text input (no explicit goals) the top-level objects — those not
// referenced by any other object — are the targets.
package reach

import (
	"github.com/hycjack/geogebra-dsl-go/internal/diag"
	"github.com/hycjack/geogebra-dsl-go/internal/ir"
)

// Targets returns the goal ids to verify.
func Targets(g *ir.Graph) []string {
	if len(g.Goals) > 0 {
		return append([]string(nil), g.Goals...)
	}
	// top-level: not referenced by anyone
	referenced := map[string]bool{}
	for _, id := range g.Order {
		o := g.Objects[id]
		for _, r := range o.Refs {
			referenced[r] = true
		}
	}
	var top []string
	for _, id := range g.Order {
		if !referenced[id] {
			top = append(top, id)
		}
	}
	return top
}

// Verify checks each target is present and (for IR, where refs are given)
// defined. Returns problems for missing targets.
func Verify(g *ir.Graph, targets []string) []diag.Problem {
	var probs []diag.Problem
	for _, t := range targets {
		if _, ok := g.Get(t); !ok {
			probs = append(probs, diag.Problem{
				Code: diag.CodeGoalUnreachable,
				Msg:  "目标对象不存在：" + t,
				Obj:  t,
			})
		}
	}
	return probs
}
