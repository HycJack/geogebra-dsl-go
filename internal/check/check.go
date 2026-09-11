// Package check is the orchestration entry point. It runs the full pipeline
// from raw input bytes to a diag.Receipt, choosing the input shape by content
// sniffing (JSON vs text). Build/parse failures are fail-closed (nothing
// downstream runs); once the graph exists, later stages collect every
// independent problem so the receipt reports the full list of defects. The CLI
// maps the receipt to an exit code.
package check

import (
	"bytes"

	"github.com/you/geogebra-dsl-go/internal/catalog"
	"github.com/you/geogebra-dsl-go/internal/deps"
	"github.com/you/geogebra-dsl-go/internal/diag"
	"github.com/you/geogebra-dsl-go/internal/geo"
	"github.com/you/geogebra-dsl-go/internal/ir"
	"github.com/you/geogebra-dsl-go/internal/reach"
	"github.com/you/geogebra-dsl-go/internal/sig"
	"github.com/you/geogebra-dsl-go/internal/text"
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
		g, fatal, buildProbs = buildText(input)
	default:
		rc.Fail(diag.Problem{Code: diag.CodeUsage, Msg: "无法识别输入形态"})
		return rc
	}

	// Structural parse/build failure: the input cannot be turned into a usable
	// graph, so nothing downstream can run (fail-closed).
	if len(fatal) > 0 {
		for _, p := range fatal {
			rc.Fail(p)
		}
		return rc
	}

	// Resolve object kinds from their commands (text input leaves them unknown).
	g.SetKindFromCmd()

	// Stages B..E collect every independent problem rather than stopping at the
	// first, so the receipt reports the full list of what's wrong with the AI
	// output. OK is true only when nothing was reported.
	rc.Errors = append(rc.Errors, buildProbs...)
	rc.Errors = append(rc.Errors, runSig(cat, g)...)

	order, cycleProbs := deps.Order(g)
	rc.Errors = append(rc.Errors, cycleProbs...)

	geoProbs := geo.Check(g, order)
	rc.Errors = append(rc.Errors, geoProbs...)

	targets := reach.Targets(g)
	rc.Errors = append(rc.Errors, reach.Verify(g, targets)...)

	if len(cycleProbs) == 0 {
		rc.Executable = order
	}
	rc.OK = len(rc.Errors) == 0
	return rc
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

func buildText(input []byte) (g *ir.Graph, fatal []diag.Problem, build []diag.Problem) {
	stmts, parseProbs := text.Parse(string(input))
	if len(parseProbs) > 0 {
		// Unparseable → cannot even construct statements: fail-closed, nothing
		// downstream can run.
		return nil, parseProbs, nil
	}
	g, buildProbs := text.Build(stmts)
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
