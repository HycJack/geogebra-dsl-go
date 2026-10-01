// Package geo performs exact degeneracy checks on the object graph. Degenerate
// constructions (zero-radius circle, line through two identical points, etc.)
// are refused: the construction "cannot be built" in the sense the validator
// reports. Numeric comparisons use math/big.Rat for exactness.
package geo

import (
	"math/big"
	"strings"

	"github.com/hycjack/geogebra-dsl-go/internal/diag"
	"github.com/hycjack/geogebra-dsl-go/internal/ir"
	"github.com/hycjack/geogebra-dsl-go/internal/number"
)

// Check walks the topological order and flags degenerate objects. Fail-closed:
// any degeneracy yields a Problem and the graph is refused.
func Check(g *ir.Graph, order []string) []diag.Problem {
	var probs []diag.Problem
	for _, id := range order {
		o := g.Objects[id]
		switch strings.ToUpper(o.Cmd) {
		case "LINE":
			if prob := lineThroughIdenticalPoints(g, o); prob != nil {
				probs = append(probs, *prob)
			}
		case "CIRCLE":
			// Only CIRCLE reaches here. CIRCLEWITHCENTER / CIRCLEBYRADIUSM used
			// to be listed alongside it, but neither is a GeoGebra command
			// (CircleByRadiusM was not even defined in any catalog file — it
			// existed only as a cmdmeta entry and this case), so the branches
			// were unreachable. Both names now resolve through catalog aliases.
			if prob := zeroRadius(g, o); prob != nil {
				probs = append(probs, *prob)
			}
		// Objects built from two points that must be distinct. Before this the
		// degeneracy check only covered Line and Circle, so Semicircle(A, A)
		// validated even though both endpoints of the diameter coincide.
		case "SEMICIRCLE":
			if prob := coincidentEndpoints(g, o, "半圆直径两端点"); prob != nil {
				probs = append(probs, *prob)
			}
		case "SEGMENT", "RAY", "POLYLINE":
			if prob := coincidentEndpoints(g, o, "两端点"); prob != nil {
				probs = append(probs, *prob)
			}
		case "LINEBISECTOR", "PERPENDICULARBISECTOR":
			if prob := coincidentEndpoints(g, o, "被平分的两个端点"); prob != nil {
				probs = append(probs, *prob)
			}
		case "ELLIPSE", "HYPERBOLA":
			if prob := coincidentEndpoints(g, o, "两个焦点"); prob != nil {
				probs = append(probs, *prob)
			}
		}
	}
	return probs
}

// coincidentEndpoints flags an object whose first two referenced points are
// literal points that coincide. Like lineThroughIdenticalPoints it gives up when
// it cannot prove degeneracy from literals, which is the conservative choice:
// these commands take a <Segment> too, and a segment's endpoints are not
// resolvable here.
func coincidentEndpoints(g *ir.Graph, o *ir.Object, what string) *diag.Problem {
	if len(o.Refs) < 2 {
		return nil
	}
	ax, ay, oka := pointCoords(g, o.Refs[0])
	bx, by, okb := pointCoords(g, o.Refs[1])
	if !oka || !okb {
		return nil
	}
	if ax.Cmp(bx) == 0 && ay.Cmp(by) == 0 {
		return &diag.Problem{
			Code: diag.CodeGeoDegenerate,
			Msg:  "退化：" + what + " 重合：" + o.Refs[0] + " 与 " + o.Refs[1],
			Obj:  o.ID,
		}
	}
	return nil
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
	if r, ok := resolveNumeric(g, radiusExpr, map[string]bool{}); ok {
		if r.Sign() <= 0 {
			return &diag.Problem{
				Code: diag.CodeGeoDegenerate,
				Msg:  "退化：圆的半径不为正（" + radiusExpr + "）",
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

// resolveNumeric evaluates an expression to an exact rational, first
// substituting any reference to a number-valued object in the graph.
//
// This exists so the two spellings of the same radius agree: `Circle((0,0),
// 1 - 1)` is caught, and so must `r = 1 - 1` followed by `Circle((0,0), r)`.
// The value is right there in the graph — the object holds "1 - 1" — so
// treating a reference as unresolvable was a missed degeneracy, not a
// conservative choice. Substitution is per-identifier, so it also covers `r` and
// arithmetic over it (`r = d + 1`, `r = s + 1 - 1`). seen guards against a
// reference cycle, which deps reports on its own.
func resolveNumeric(g *ir.Graph, expr string, seen map[string]bool) (*big.Rat, bool) {
	if r, ok := parseRat(expr); ok {
		return r, true
	}
	sub := substituteNumbers(g, expr, seen)
	if sub == expr {
		return nil, false // nothing to substitute: not a resolvable number
	}
	return parseRat(sub)
}

// substituteNumbers rewrites every identifier in expr that names a number-valued
// object into that object's own value, leaving the result a literal-only
// expression number.Eval can handle. Identifiers that do not resolve to a
// number — a point, a function name, an undefined name — are left untouched,
// which leaves an expression the evaluator will reject: the conservative
// outcome, and the same one an unresolvable argument had before.
func substituteNumbers(g *ir.Graph, expr string, seen map[string]bool) string {
	var sb strings.Builder
	sb.Grow(len(expr))
	i, n := 0, len(expr)
	for i < n {
		c := expr[i]
		if !isIdentStartByte(c) {
			sb.WriteByte(c)
			i++
			continue
		}
		j := i
		for j < n && isIdentCharByte(expr[j]) {
			j++
		}
		name := expr[i:j]
		if lit, ok := numberLiteral(g, name, seen); ok {
			sb.WriteString(lit)
		} else {
			sb.WriteString(name)
		}
		i = j
	}
	return sb.String()
}

// numberLiteral returns the exact decimal form of the value of the number object
// named name, or ok=false when it is not a resolvable number. Recursion is
// bounded by seen, which both blocks reference cycles and stops a name from
// being substituted into its own definition.
func numberLiteral(g *ir.Graph, name string, seen map[string]bool) (string, bool) {
	if name == "" || seen[name] {
		return "", false
	}
	obj, ok := g.Get(name)
	if !ok || obj.Kind != ir.KNumber || len(obj.Args) == 0 {
		return "", false
	}
	seen[name] = true
	defer delete(seen, name)
	v, ok := resolveNumeric(g, obj.Args[0], seen)
	if !ok {
		return "", false
	}
	return v.RatString(), true
}

func isIdentStartByte(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func isIdentCharByte(c byte) bool {
	return isIdentStartByte(c) || ('0' <= c && c <= '9')
}

// parseRat resolves an argument expression (a decimal, a reserved constant such
// as pi/e/Euler, or nested arithmetic over them) to an exact *big.Rat value.
// Non-resolvable expressions (e.g. an undefined name or a bad construct) return
// ok=false, in which case the degeneracy check conservatively does not flag.
func parseRat(s string) (*big.Rat, bool) {
	return number.Eval(s)
}
