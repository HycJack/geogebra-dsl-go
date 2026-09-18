// Package check is the orchestration entry point. It runs the full pipeline
// from raw input bytes to a diag.Receipt, choosing the input shape by content
// sniffing (JSON vs text). Build/parse failures are fail-closed (nothing
// downstream runs); once the graph exists, later stages collect every
// independent problem so the receipt reports the full list of defects. The CLI
// maps the receipt to an exit code.
package check

import (
	"bytes"

	"github.com/hycjack/geogebra-dsl-go/internal/catalog"
	"github.com/hycjack/geogebra-dsl-go/internal/deps"
	"github.com/hycjack/geogebra-dsl-go/internal/diag"
	"github.com/hycjack/geogebra-dsl-go/internal/geo"
	"github.com/hycjack/geogebra-dsl-go/internal/ir"
	"github.com/hycjack/geogebra-dsl-go/internal/reach"
	"github.com/hycjack/geogebra-dsl-go/internal/sig"
	"github.com/hycjack/geogebra-dsl-go/internal/text"
)

// Options control a check run.
type Options struct {
	Catalog     *catalog.Catalog // defaulted to catalog.Default() when nil
	ForceSource string           // "", "text", or "ir" — override sniffing
}

// Check validates raw input bytes and returns a receipt. It never panics: fn
// errors (e.g. missing embedded catalog) are turned into a failing receipt.
func Check(input []byte, opt Options) *diag.Receipt {
	source := sniff(input, opt.ForceSource)
	rc := diag.NewReceipt(source)

	cat := opt.Catalog
	if cat == nil {
		d, err := catalog.Default()
		if err != nil {
			rc.Fail(diag.Problem{Code: diag.CodeUsage, Msg: "无法加载命令表：" + err.Error()})
			return rc
		}
		cat = d
	}

	var g *ir.Graph
	var fatal []diag.Problem
	var buildProbs []diag.Problem

	switch source {
	case "ir":
		var perr error
		g, fatal, perr = ir.ParseJSON(input)
		if perr != nil && len(fatal) == 0 {
			fatal = []diag.Problem{{Code: diag.CodeParseJSON, Msg: "IR 解析失败"}}
		}
	case "text":
		g, fatal, buildProbs = buildText(input, cat)
	default:
		rc.Fail(diag.Problem{Code: diag.CodeUsage, Msg: "无法识别输入形态"})
		return rc
	}

	// Structural parse/build failure: the input cannot be turned into a usable
	// graph, so nothing downstream can run (fail-closed). Only true structural
	// damage (parse/*, usage) stops the run; a semantic problem surfaced during
	// parsing (e.g. dep/redefine for a duplicate IR id) is collected below so
	// every independent defect is still reported.
	var structural []diag.Problem
	for _, p := range fatal {
		switch p.Code {
		case diag.CodeParseSyntax, diag.CodeParseJSON, diag.CodeUsage:
			structural = append(structural, p)
		default:
			buildProbs = append(buildProbs, p)
		}
	}
	if len(structural) > 0 {
		for _, p := range structural {
			rc.Fail(p)
		}
		return rc
	}

	// Resolve object kinds from their commands (text input leaves them unknown).
	// The command→kind mapping is data (cmdmeta.json) in the catalog, not code.
	// Expression objects are classified afterwards: once command kinds exist we
	// can tell `r = 2/3` and `r = d+1` (d a Number) are Numbers, not Functions.
	cat.ApplyKinds(g)
	text.ReclassifyNumericExprs(g)

	// Stages B..E collect every independent problem rather than stopping at the
	// first, so the receipt reports the full list of what's wrong with the AI
	// output. OK is true only when nothing was reported.
	rc.Errors = append(rc.Errors, buildProbs...)
	if source == "ir" {
		// IR input carries its `refs` verbatim; a ref to an id that isn't an
		// object is an undefined reference. (The text path reports these during
		// build, so we only re-check IR here to avoid double-reporting.)
		rc.Errors = append(rc.Errors, checkIRRefs(g)...)
	}
	rc.Errors = append(rc.Errors, runSig(cat, g)...)
	// Statement-style commands (SetColor, ShowAxes, ...) are not graph objects,
	// so runSig never saw them; validate them here against the same catalog.
	rc.Errors = append(rc.Errors, runSigStatements(cat, g)...)

	order, cycleProbs := deps.Order(g)
	rc.Errors = append(rc.Errors, cycleProbs...)

	geoProbs := geo.Check(g, order)
	rc.Errors = append(rc.Errors, geoProbs...)

	targets := reach.Targets(g)
	rc.Errors = append(rc.Errors, reach.Verify(g, targets)...)

	if len(cycleProbs) == 0 {
		rc.Executable = order
	}
	// Snapshot the resolved object kinds for hosts that want to reason about
	// the constructed geometry (e.g. pick the 2D vs 3D GeoGebra view) without
	// re-running the pipeline.
	rc.Kinds = kindsOf(g)
	rc.OK = len(rc.Errors) == 0
	return rc
}

// kindsOf records each object's resolved coarse kind, skipping objects whose
// kind was never determined. Order follows the graph's definition order so a
// host sees them in the order the script introduced them.
func kindsOf(g *ir.Graph) map[string]string {
	out := map[string]string{}
	for _, id := range g.Order {
		if name := irKindName(g.Objects[id].Kind); name != "" {
			out[id] = name
		}
	}
	return out
}

// irKindName maps the ir.Kind enum to its name. ir.Kind.String only names the
// kinds a signature can bind to, which leaves Plane, Quadric and Solid all
// reporting "Unknown" — exactly the 3D indicators a host needs.
func irKindName(k ir.Kind) string {
	switch k {
	case ir.KPoint:
		return "Point"
	case ir.KLine:
		return "Line"
	case ir.KSegment:
		return "Segment"
	case ir.KRay:
		return "Ray"
	case ir.KVector:
		return "Vector"
	case ir.KCircle:
		return "Circle"
	case ir.KConic:
		return "Conic"
	case ir.KPolygon:
		return "Polygon"
	case ir.KNumber:
		return "Number"
	case ir.KBool:
		return "Bool"
	case ir.KFunction:
		return "Function"
	case ir.KObject:
		return "Object"
	case ir.KList:
		return "List"
	case ir.KPlane:
		return "Plane"
	case ir.KQuadric:
		return "Quadric"
	case ir.KSolid:
		return "Solid"
	case ir.KPolyhedron:
		return "Polyhedron"
	case ir.KScript:
		return "Script"
	case ir.KText:
		return "Text"
	case ir.KMatrix:
		return "Matrix"
	case ir.KPolynomial:
		return "Polynomial"
	case ir.KCurve:
		return "Curve"
	case ir.KLocus:
		return "Locus"
	case ir.KSet:
		return "Set"
	case ir.KTurtle:
		return "Turtle"
	}
	return ""
}

// sniff decides input shape. Explicit forceSource wins; otherwise leading '{'
// (after trimming whitespace) means IR JSON, else text.
func sniff(input []byte, force string) string {
	switch force {
	case "text":
		return "text"
	case "ir":
		return "ir"
	}
	t := bytes.TrimLeft(input, " \t\r\n")
	if len(t) > 0 && t[0] == '{' {
		return "ir"
	}
	return "text"
}

func buildText(input []byte, cat *catalog.Catalog) (g *ir.Graph, fatal []diag.Problem, build []diag.Problem) {
	stmts, parseProbs := text.Parse(string(input), cat)
	if len(parseProbs) > 0 {
		// Unparseable → cannot even construct statements: fail-closed, nothing
		// downstream can run.
		return nil, parseProbs, nil
	}
	g, buildProbs := text.Build(stmts, cat)
	// Build problems (redefine / undefined ref) are collected but the graph is
	// still usable for reporting further independent problems.
	return g, nil, buildProbs
}

func runSig(c *catalog.Catalog, g *ir.Graph) []diag.Problem {
	var probs []diag.Problem
	for _, id := range g.Order {
		o := g.Objects[id]
		if o.Cmd == "" {
			continue // literal point / number: no signature to check
		}
		m := sig.Lookup(c, g, o)
		if !m.Known {
			probs = append(probs, diag.Problem{
				Code: diag.CodeCmdUnknown, Msg: m.Explain, Obj: id, Line: o.Line,
			})
		} else if !m.OK {
			probs = append(probs, diag.Problem{
				Code: diag.CodeCmdArg, Msg: m.Explain, Obj: id, Line: o.Line,
			})
		}
	}
	return probs
}

// runSigStatements type-checks statement-style commands (modifiers) against the
// catalog. They are not graph objects, so runSig skips them; they are carried on
// the graph as ir.Statement by the text builder. Lookup is called with a
// temporary Object because it only reads Cmd and Args — arg kinds are resolved
// against g, where the modifier's real targets live.
func runSigStatements(c *catalog.Catalog, g *ir.Graph) []diag.Problem {
	var probs []diag.Problem
	for _, st := range g.Statements {
		o := &ir.Object{Cmd: st.Cmd, Args: st.Args, Line: st.Line}
		m := sig.Lookup(c, g, o)
		if !m.Known {
			probs = append(probs, diag.Problem{
				Code: diag.CodeCmdUnknown, Msg: m.Explain, Obj: st.Cmd, Line: st.Line,
			})
		} else if !m.OK {
			probs = append(probs, diag.Problem{
				Code: diag.CodeCmdArg, Msg: m.Explain, Obj: st.Cmd, Line: st.Line,
			})
		}
	}
	return probs
}

// checkIRRefs verifies that every `refs` entry on an object resolves to a real
// object in the graph. IR JSON supplies refs directly, so a dangling ref is an
// undefined reference (dep/undefined) rather than something the text builder
// would have caught.
func checkIRRefs(g *ir.Graph) []diag.Problem {
	var probs []diag.Problem
	for _, id := range g.Order {
		o := g.Objects[id]
		for _, r := range o.Refs {
			if _, ok := g.Get(r); !ok {
				probs = append(probs, diag.Problem{
					Code: diag.CodeDepUndefined, Msg: "引用了未定义对象：" + r, Obj: id, Line: o.Line,
				})
			}
		}
	}
	return probs
}
