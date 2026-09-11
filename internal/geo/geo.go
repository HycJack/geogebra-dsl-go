// Package geo performs exact degeneracy checks on the object graph. Degenerate
// constructions (zero-radius circle, line through two identical points, etc.)
// are refused: the construction "cannot be built" in the sense the validator
// reports. Numeric comparisons use math/big.Rat for exactness.
package geo

import (
	"math/big"
	"strings"

	"github.com/you/geogebra-dsl-go/internal/diag"
	"github.com/you/geogebra-dsl-go/internal/ir"
)

// Check walks the topological order and flags degenerate objects. Fail-closed:
// any degeneracy yields a Problem and the graph is refused.
func Check(g *ir.Graph, order []string) []diag.Problem {
	var probs []diag.Problem
	for _, id := range order {
		o := g.Objects[id]
		switch o.Cmd {
		case "Line":
			if prob := lineThroughIdenticalPoints(g, o); prob != nil {
				probs = append(probs, *prob)
			}
		case "Circle", "CircleWithCenter", "CircleByRadiusM":
			if prob := zeroRadius(g, o); prob != nil {
				probs = append(probs, *prob)
			}
		}
	}
	return probs
}

// pointCoords returns the exact coordinates of a literal point object, and
// whether it is a literal point we can compare exactly.
func pointCoords(g *ir.Graph, id string) (*big.Rat, *big.Rat, bool) {
	o, ok := g.Get(id)
	if !ok || o.Kind != ir.KPoint || len(o.Args) < 2 {
		return nil, nil, false
	}
	x, okx := parseRat(o.Args[0])
	y, oky := parseRat(o.Args[1])
	if !okx || !oky {
		return nil, nil, false
	}
	return x, y, true
}

func lineThroughIdenticalPoints(g *ir.Graph, o *ir.Object) *diag.Problem {
	if len(o.Refs) != 2 {
		return nil
	}
	a, b := o.Refs[0], o.Refs[1]
	ax, ay, oka := pointCoords(g, a)
	bx, by, okb := pointCoords(g, b)
	if !oka || !okb {
		return nil // can't prove degeneracy from literals; treat as ok
	}
	if ax.Cmp(bx) == 0 && ay.Cmp(by) == 0 {
		return &diag.Problem{
			Code: diag.CodeGeoDegenerate,
			Msg:  "退化：直线经过两个重合点 " + a + " 与 " + b,
			Obj:  o.ID,
		}
	}
	return nil
}

func zeroRadius(g *ir.Graph, o *ir.Object) *diag.Problem {
	// Circle(center, radius) or Circle(center, point). Radius from refs or args.
	if len(o.Args) < 2 {
		return nil
	}
	radiusExpr := o.Args[len(o.Args)-1]
	if r, ok := parseRat(radiusExpr); ok {
		if r.Sign() <= 0 {
			return &diag.Problem{
				Code: diag.CodeGeoDegenerate,
				Msg:  "退化：圆的半径不为正（" + o.Args[len(o.Args)-1] + "）",
				Obj:  o.ID,
			}
		}
		return nil
	}
	// radius given as a point on the circle — degenerate if that point equals center.
	if len(o.Refs) >= 2 {
		cx, cy, oka := pointCoords(g, o.Refs[0])
		px, py, okb := pointCoords(g, o.Refs[1])
		if oka && okb && cx.Cmp(px) == 0 && cy.Cmp(py) == 0 {
			return &diag.Problem{
				Code: diag.CodeGeoDegenerate,
				Msg:  "退化：圆上点与圆心重合，半径为零",
				Obj:  o.ID,
			}
		}
	}
	return nil
}

// parseRat parses a plain decimal number (e.g. "0", "3.5", "-2.25") as *big.Rat,
// for exact comparison. Non-numeric input returns ok=false.
func parseRat(s string) (*big.Rat, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, false
	}
	return r, true
}
