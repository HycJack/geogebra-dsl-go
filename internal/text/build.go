// Package text converts a GeoGebra-style text script into an ir.Graph. The
// grammar is intentionally small (enough for AI-generated teaching scripts):
//
//	# comment
//	A = Point(0, 2)
//	l = Line(A, B)
//	c = Circle(C, T)
//	M = (1, 2)          # literal point
//	r = 3               # number variable
//
// Each object is `ID = Command(arg, ...)`. Arguments that are bare identifiers
// resolve to references; numbers, coordinates, and expressions are literals.
package text

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/you/geogebra-dsl-go/internal/diag"
	"github.com/you/geogebra-dsl-go/internal/ir"
	"github.com/you/geogebra-dsl-go/internal/number"
)

// statement is one parsed line.
type statement struct {
	id            string
	cmd           string   // command name ("" for literal/number)
	args          []string // raw argument expressions
	lineNo        int
	literalPoint  bool
	numberLiteral string
}

// Parse splits a script into statements. Returns parse errors (fail-closed).
func Parse(src string) ([]statement, []diag.Problem) {
	src = strings.TrimPrefix(src, "\uFEFF") // strip UTF-8 BOM
	var stmts []statement
	var probs []diag.Problem
	lines := strings.Split(src, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(stripComment(raw))
		if line == "" {
			continue
		}
		s, ok, prob := parseLine(line, i+1)
		if !ok {
			probs = append(probs, prob)
			continue
		}
		stmts = append(stmts, s)
	}
	return stmts, probs
}

func parseLine(line string, lineNo int) (statement, bool, diag.Problem) {
	// split on '=' at top level.
	eq := topLevelIndex(line, '=')
	if eq < 0 {
		return statement{}, false, diag.Problem{
			Code: diag.CodeParseSyntax,
			Msg:  "不是一条指令（缺少 = 或命令调用）",
			Line: lineNo,
		}
	}
	id := strings.TrimSpace(line[:eq])
	rhs := strings.TrimSpace(line[eq+1:])
	if id == "" || rhs == "" {
		return statement{}, false, diag.Problem{Code: diag.CodeParseSyntax, Msg: "赋值两端不能为空", Line: lineNo}
	}
	if !isIdentName(id) {
		return statement{}, false, diag.Problem{Code: diag.CodeParseSyntax, Msg: "对象名不合法：" + id, Line: lineNo}
	}
	// Number literal: ID = 3.5
	if isNumber(rhs) {
		return statement{id: id, numberLiteral: rhs, lineNo: lineNo}, true, diag.Problem{}
	}
	// Literal point: ID = (0, 2)
	if strings.HasPrefix(rhs, "(") {
		inner, ok, prob := parenArgs(rhs, lineNo)
		if !ok {
			return statement{}, false, prob
		}
		return statement{id: id, args: inner, literalPoint: true, lineNo: lineNo}, true, diag.Problem{}
	}
	// Command: ID = Command(args)
	lp := strings.IndexByte(rhs, '(')
	if lp < 0 || !strings.HasSuffix(rhs, ")") {
		return statement{}, false, diag.Problem{Code: diag.CodeParseSyntax, Msg: "命令调用语法错误：" + rhs, Line: lineNo}
	}
	cmd := strings.TrimSpace(rhs[:lp])
	if cmd == "" {
		return statement{}, false, diag.Problem{Code: diag.CodeParseSyntax, Msg: "缺命令名：" + rhs, Line: lineNo}
	}
	inner := strings.TrimSpace(rhs[lp+1 : len(rhs)-1])
	args := splitArgs(inner)
	return statement{id: id, cmd: cmd, args: args, lineNo: lineNo}, true, diag.Problem{}
}

// parenArgs extracts the comma-separated args inside a parenthesized expression.
func parenArgs(rhs string, lineNo int) ([]string, bool, diag.Problem) {
	if !strings.HasPrefix(rhs, "(") || !strings.HasSuffix(rhs, ")") {
		return nil, false, diag.Problem{Code: diag.CodeParseSyntax, Msg: "括号不配对", Line: lineNo}
	}
	inner := strings.TrimSpace(rhs[1 : len(rhs)-1])
	return splitArgs(inner), true, diag.Problem{}
}

// splitArgs splits a comma-separated argument list at the top level, respecting
// nested parentheses/brackets.
func splitArgs(s string) []string {
	var out []string
	depth := 0
	var cur strings.Builder
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
			cur.WriteRune(r)
		case ')', ']', '}':
			depth--
			cur.WriteRune(r)
		case ',':
			if depth == 0 {
				a := strings.TrimSpace(cur.String())
				if a != "" {
					out = append(out, a)
				}
				cur.Reset()
			} else {
				cur.WriteRune(r)
			}
		default:
			cur.WriteRune(r)
		}
	}
	a := strings.TrimSpace(cur.String())
	if a != "" {
		out = append(out, a)
	}
	return out
}

// Build turns parsed statements into a graph. It detects redefinition and
// undefined references; both are fail-closed errors. Ref resolution uses the
// whole set of defined objects (order-insensitive) so forward references that
// exist resolve fine — the cycle check handles dependency order afterwards.
func Build(stmts []statement) (*ir.Graph, []diag.Problem) {
	g := ir.New()
	// First pass: register ids so refs resolve regardless of order.
	for _, s := range stmts {
		if _, exists := g.Get(s.id); exists {
			// redefinition detected below; but keep first occurrence
			continue
		}
		g.Add(&ir.Object{ID: s.id})
	}
	var probs []diag.Problem
	seen := map[string]bool{}
	for _, s := range stmts {
		if seen[s.id] {
			probs = append(probs, diag.Problem{
				Code: diag.CodeDepRedefine, Msg: "对象重复定义：" + s.id, Obj: s.id, Line: s.lineNo,
			})
		}
		seen[s.id] = true
		o, _ := g.Get(s.id)
		o.Cmd = s.cmd
		o.Line = s.lineNo
		switch {
		case s.literalPoint:
			o.Kind = ir.KPoint
			o.Args = s.args
		case s.numberLiteral != "":
			o.Kind = ir.KNumber
			o.Args = []string{s.numberLiteral}
		default:
			// command object; first flatten any nested command calls in the
			// arguments (each becomes a synthetic object in the graph), then
			// resolve refs from the partly-flattened args.
			seq := &synthSeq{}
			var nested []diag.Problem
			o.Args, nested = flattenArgs(g, s.id, s.args, s.lineNo, seq)
			probs = append(probs, nested...)
			refs, undefs := resolveRefs(g, s.cmd, o.Args)
			o.Refs = refs
			for _, u := range undefs {
				probs = append(probs, diag.Problem{
					Code: diag.CodeDepUndefined, Msg: "引用了未定义对象：" + u, Obj: s.id, Line: s.lineNo,
				})
			}
		}
	}
	return g, probs
}

// synthSeq is a counter for generating unique synthetic object ids for nested
// command calls.
type synthSeq struct{ n int }

// flattenArgs rewrites an argument list so that any argument that is itself a
// command call (e.g. Midpoint(A,B) inside Circle(Midpoint(A,B),3)) becomes a
// synthetic object added to the graph, with the argument position replaced by
// that synthetic object's id. It recurses so arbitrarily deep nesting is
// captured, and each nested command gets its own dependency edges.
func flattenArgs(g *ir.Graph, owner string, args []string, lineNo int, seq *synthSeq) ([]string, []diag.Problem) {
	var probs []diag.Problem
	out := make([]string, len(args))
	for i, a := range args {
		a = strings.TrimSpace(a)
		sub, ok, prob := parseArgCommand(a, lineNo)
		if !ok {
			out[i] = a // not a command call: keep as literal/ref
			if prob != nil {
				probs = append(probs, *prob)
			}
			continue
		}
		// a is `SubCmd(inner...)`: materialize as synthetic object.
		seq.n++
		sid := owner + "." + sub.cmd + strconv.Itoa(seq.n)
		if _, exists := g.Get(sid); exists {
			probs = append(probs, diag.Problem{
				Code: diag.CodeDepRedefine, Msg: "嵌套命令 id 冲突：" + sid, Obj: owner, Line: lineNo,
			})
			out[i] = sid
			continue
		}
		g.Add(&ir.Object{ID: sid, Cmd: sub.cmd, Line: lineNo})
		subArgs, subProbs := flattenArgs(g, sid, sub.args, lineNo, seq)
		probs = append(probs, subProbs...)
		obj, _ := g.Get(sid)
		obj.Args = subArgs
		refs, undefs := resolveRefs(g, sub.cmd, subArgs)
		obj.Refs = refs
		for _, u := range undefs {
			probs = append(probs, diag.Problem{
				Code: diag.CodeDepUndefined, Msg: "引用了未定义对象（嵌套）：" + u, Obj: sid, Line: lineNo,
			})
		}
		out[i] = sid
	}
	return out, probs
}

// argCommand is a decomposed nested command call found inside an argument.
type argCommand struct {
	cmd  string
	args []string
}

// parseArgCommand detects whether s parses as `Cmd(...)` and, if so, returns
// its command name and inner args. Non-command expression returns ok=false.
func parseArgCommand(s string, lineNo int) (argCommand, bool, *diag.Problem) {
	lp := strings.IndexByte(s, '(')
	if lp <= 0 || !strings.HasSuffix(s, ")") {
		return argCommand{}, false, nil
	}
	cmd := strings.TrimSpace(s[:lp])
	// the command name must be a plain identifier (no operators inside)
	if !isIdentName(cmd) {
		return argCommand{}, false, nil
	}
	inner := strings.TrimSpace(s[lp+1 : len(s)-1])
	return argCommand{cmd: cmd, args: splitArgs(inner)}, true, nil
}

// resolveRefs finds which args are bare identifiers referring to defined objects,
// and reports bare identifiers that are not defined (undefined refs). Numbers,
// coordinates, and bound variables (declared by commands like Sequence/Sum/
// Curve) are not refs; they are excluded from both dependency edges and
// undefined-ref reports.
func resolveRefs(g *ir.Graph, cmd string, args []string) (refs, undefs []string) {
	bound := boundVars(cmd, args)
	for i, a := range args {
		a = strings.TrimSpace(a)
		if bound[i] {
			continue // the arg is the command's own iteration/parameter variable, not a ref
		}
		if isNumber(a) || number.KnownConstant(a) {
			continue // literal or reserved constant, not a reference
		}
		if _, ok := g.Get(a); ok {
			// a defined object (a named object, or a synthetic nested-command
			// id like A.Midpoint1) is a dependency ref regardless of whether it
			// is a "clean" identifier.
			refs = append(refs, a)
		} else if isIdentName(a) {
			undefs = append(undefs, a)
		}
	}
	return refs, undefs
}

// boundVars returns, for each arg index, whether that arg is a variable *bound*
// by the command (e.g. Sequence(<expr>, <k>, <start>, <end>, [step]) binds k at
// index 1). Such names are in-scope local symbols and must not be treated as
// object references.
func boundVars(cmd string, args []string) []bool {
	out := make([]bool, len(args))
	switch strings.ToUpper(cmd) {
	case "SEQUENCE", "SUM", "PRODUCT", "ITERATIONLIST", "SERIES":
		// Sequence(expr, k, start, end[, step]), Sum/Product/..., expr, k, a, b
		if len(args) >= 2 {
			out[1] = true
		}
	case "CURVE", "SURFACE":
		// Curve(x_expr, k, a, b[, z_expr]) binds k at index 1 and, for 3D, t at 4.
		if len(args) >= 2 {
			out[1] = true
		}
		if len(args) >= 5 {
			out[4] = true
		}
	default:
		// no bound variables
	}
	return out
}

// isIdentName reports whether s is a valid identifier (letters/digits/_/colon,
// not starting with a digit, and not purely numeric).
func isIdentName(s string) bool {
	if s == "" {
		return false
	}
	first := s[0]
	if !(first == '_' || ('a' <= first && first <= 'z') || ('A' <= first && first <= 'Z')) {
		return false
	}
	for _, r := range s {
		ok := r == '_' || r == ':' || unicode.IsLetter(r) || unicode.IsDigit(r)
		if !ok {
			return false
		}
	}
	return true
}

// isNumber reports whether s is a plain number literal (optional sign, digits,
// optional decimal point).
func isNumber(s string) bool {
	if s == "" {
		return false
	}
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "+")
	dot := false
	for _, r := range s {
		switch {
		case '0' <= r && r <= '9':
		case r == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return true
}

// stripComment removes a trailing "# ..." comment from a line. Comments start
// at the first '#' and run to end of line; the grammar has no string literals
// that could contain '#', so this is safe.
func stripComment(line string) string {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		return line[:i]
	}
	return line
}

// topLevelIndex finds idx of the first '=' at nesting depth 0.
func topLevelIndex(s string, c byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case c:
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
