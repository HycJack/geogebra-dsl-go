// Package text converts a GeoGebra-style text script into an ir.Graph. The
// grammar is intentionally small (enough for AI-generated teaching scripts):
//
//	# comment
//	A = Point(0, 2)      # command object
//	l = Line(A, B)
//	c = Circle(C, T)
//	M = (1, 2)           # literal point
//	r = 3                # number variable
//	L = {1, 2, 3}        # list literal (Kind List)
//	y = x^2 + 1          # algebraic expression (Kind Function)
//	f(x) = 2x + 1        # function definition (Kind Function, param x local)
//	SetColor(c, "red")   # statement-style modifier (no "=")
//
// Each object is `ID = Command(arg, ...)`. Arguments that are bare identifiers
// resolve to references; numbers, coordinates, lists, and expressions are
// literals. Bare no-"=" statements are accepted only for the modifier/scripting
// command set (Set…/StartAnimation/Rename/…).
package text

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/hycjack/geogebra-dsl-go/internal/diag"
	"github.com/hycjack/geogebra-dsl-go/internal/ir"
	"github.com/hycjack/geogebra-dsl-go/internal/number"
)

// statement is one parsed line.
type statement struct {
	id            string
	cmd           string   // command name ("" for literal/number)
	args          []string // raw argument expressions
	lineNo        int
	literalPoint  bool
	numberLiteral string
	boolLiteral   string   // "true"/"false" literal (Kind KBool)
	literalList   []string // elements of a { ... } list literal (Kind KList)
	exprLiteral   string   // non-empty: RHS is a raw expression / function body
	fnParams      []string // parameter names for a function def like f(x,y) = ...
	modifier      bool     // statement-style command with no "=", e.g. SetColor(c, "red")
}

// A bare no-"=" statement is legal only for an official GeoGebra Scripting
// command (SetColor, ShowAxes, Slider, TurtleForward, ...). The set lives in
// ir.IsScriptingCommand so the parser and the command→kind table cannot drift.
//
// Kept as an explicit table rather than an "everything is a modifier" rule: a
// bare `Circle(A, B)` must remain a parse error, because Circle does return an
// object and writing it bare is how a typo'd assignment looks.

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
		// No '=' — allow statement-style modifier/scripting commands such as
		// `SetColor(c, "red")` or `StartAnimation(a)`. Anything else without
		// '=' remains a syntax error.
		if cmdArgs, ok, prob := parseCommandCall(line, lineNo); ok {
			return statement{cmd: cmdArgs.cmd, args: cmdArgs.args, lineNo: lineNo, modifier: true}, true, diag.Problem{}
		} else if prob != nil {
			return statement{}, false, *prob
		}
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
	// LHS may be a function definition `f(x, y) = ...` or a plain object name.
	name, fnParams, idOK := parseLHS(id)
	if !idOK {
		return statement{}, false, diag.Problem{Code: diag.CodeParseSyntax, Msg: "对象名不合法：" + id, Line: lineNo}
	}
	// A function definition `f(x) = <body>` always treats the whole RHS as the
	// function body expression — even if it looks like a command call such as
	// `f(x) = sin(x)`. GeoGebra parses these as the body, not an assignment of a
	// command result, so skip the command/list/point dispatch below.
	if len(fnParams) > 0 {
		return statement{id: name, fnParams: fnParams, exprLiteral: rhs, lineNo: lineNo}, true, diag.Problem{}
	}
	// Number literal: ID = 3.5
	if isNumber(rhs) {
		return statement{id: name, fnParams: fnParams, numberLiteral: rhs, lineNo: lineNo}, true, diag.Problem{}
	}
	// Boolean literal: ID = true / ID = false
	if isBoolLiteral(rhs) {
		return statement{id: name, fnParams: fnParams, boolLiteral: rhs, lineNo: lineNo}, true, diag.Problem{}
	}
	// Literal point: ID = (0, 2)
	if strings.HasPrefix(rhs, "(") {
		inner, ok, prob := parenArgs(rhs, lineNo)
		if !ok {
			return statement{}, false, prob
		}
		return statement{id: name, literalPoint: true, args: inner, lineNo: lineNo}, true, diag.Problem{}
	}
	// Literal list: ID = {a, b, c}
	if strings.HasPrefix(rhs, "{") {
		inner, ok, prob := braceArgs(rhs, lineNo)
		if !ok {
			return statement{}, false, prob
		}
		return statement{id: name, literalList: inner, lineNo: lineNo}, true, diag.Problem{}
	}
	// Command: ID = Command(args). Only a leading identifier followed by '('
	// is a command call. Anything else that merely ends in ')' (e.g.
	// `g = 2*k + Sqrt(4)`) is an arithmetic expression, routed below, so a
	// nested call embedded in an expression isn't misread as a command name.
	lp := strings.IndexByte(rhs, '(')
	if lp >= 0 && strings.HasSuffix(rhs, ")") {
		cmd := strings.TrimSpace(rhs[:lp])
		if cmd == "" {
			return statement{}, false, diag.Problem{Code: diag.CodeParseSyntax, Msg: "缺命令名：" + rhs, Line: lineNo}
		}
		if isIdentName(cmd) {
			inner := strings.TrimSpace(rhs[lp+1 : len(rhs)-1])
			args := splitArgs(inner)
			return statement{id: name, fnParams: fnParams, cmd: cmd, args: args, lineNo: lineNo}, true, diag.Problem{}
		}
		// prefix isn't a valid command name → fall through to expression RHS
	}
	// Expression right-hand side: y = x^2 + 1, f(x) = 2x + 1, etc. A well-formed
	// expression (not a bare command call missing its "=") is accepted as an
	// algebraic object so implicit curves / functions don't hard-fail parsing.
	if rhs != "" {
		return statement{id: name, fnParams: fnParams, exprLiteral: rhs, lineNo: lineNo}, true, diag.Problem{}
	}
	return statement{}, false, diag.Problem{Code: diag.CodeParseSyntax, Msg: "命令调用语法错误：" + rhs, Line: lineNo}
}

// parseLHS splits an assignment's left-hand side into a plain object name and,
// for a function definition like `f(x, y)`, the parameter names. It returns
// ok=false when the LHS is not a valid object name (with optional params).
func parseLHS(lhs string) (name string, params []string, ok bool) {
	lhs = strings.TrimSpace(lhs)
	lp := strings.IndexByte(lhs, '(')
	if lp < 0 {
		if !isIdentName(lhs) {
			return "", nil, false
		}
		return lhs, nil, true
	}
	if !strings.HasSuffix(lhs, ")") {
		return "", nil, false
	}
	name = strings.TrimSpace(lhs[:lp])
	if !isIdentName(name) {
		return "", nil, false
	}
	inner := strings.TrimSpace(lhs[lp+1 : len(lhs)-1])
	ps := splitArgs(inner)
	for _, p := range ps {
		if !isIdentName(p) {
			return "", nil, false
		}
	}
	return name, ps, true
}

// braceArgs extracts the comma-separated elements inside a { ... } list literal.
func braceArgs(rhs string, lineNo int) ([]string, bool, diag.Problem) {
	if !strings.HasPrefix(rhs, "{") || !strings.HasSuffix(rhs, "}") {
		return nil, false, diag.Problem{Code: diag.CodeParseSyntax, Msg: "花括号不配对", Line: lineNo}
	}
	inner := strings.TrimSpace(rhs[1 : len(rhs)-1])
	return splitArgs(inner), true, diag.Problem{}
}

// parseCommandCall parses a bare `Cmd(args)` line (no "="). It only accepts
// commands in the modifier set, so a non-modifier command with no "=" is a
// syntax error rather than being silently accepted.
func parseCommandCall(s string, lineNo int) (argCommand, bool, *diag.Problem) {
	lp := strings.IndexByte(s, '(')
	if lp <= 0 || !strings.HasSuffix(s, ")") {
		return argCommand{}, false, nil
	}
	cmd := strings.TrimSpace(s[:lp])
	if !isIdentName(cmd) {
		return argCommand{}, false, nil
	}
	// Modifier command names are matched case-insensitively (setcolor == SetColor),
	// consistent with the rest of the command handling.
	if !ir.IsScriptingCommand(cmd) {
		// A real command must be written as Object = Command(...). Reject with a
		// hint so the model learns the expected assignment syntax.
		return argCommand{}, false, &diag.Problem{
			Code: diag.CodeParseSyntax,
			Msg: "指令 " + cmd + " 会返回对象，必须写成 对象名 = " + cmd +
				"(…) 的形式；无赋值号的裸语句仅限官方 Scripting 类指令（Set*/Show*/Slider/Turtle* 等 67 条，见 manual 的 Scripting_Commands 页）",
			Line: lineNo,
		}
	}
	inner := strings.TrimSpace(s[lp+1 : len(s)-1])
	return argCommand{cmd: cmd, args: splitArgs(inner)}, true, nil
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
	var probs []diag.Problem
	// Pass 0: modifiers are statement-style commands (SetColor etc.) that modify
	// existing objects; their targets must refer to a defined object, so they
	// are validated after all named ids are known. Bare no-"=" modifiers are NOT
	// registered as objects.
	modifiers := make([]statement, 0)
	for _, s := range stmts {
		if s.modifier {
			modifiers = append(modifiers, s)
			continue
		}
		if _, exists := g.Get(s.id); exists {
			// redefinition detected below; but keep first occurrence
			continue
		}
		g.Add(&ir.Object{ID: s.id})
	}
	seen := map[string]bool{}
	for _, s := range stmts {
		if s.modifier {
			continue
		}
		// A redefinition is reported and the earlier definition is kept: the
		// redefining statement must NOT overwrite the retained first occurrence
		// (ids were registered once in the first pass above).
		if seen[s.id] {
			probs = append(probs, diag.Problem{
				Code: diag.CodeDepRedefine, Msg: "对象重复定义：" + s.id, Obj: s.id, Line: s.lineNo,
			})
			continue
		}
		seen[s.id] = true
		o, _ := g.Get(s.id)
		o.Cmd = s.cmd
		o.Line = s.lineNo
		switch {
		case s.literalPoint:
			o.Kind = ir.KPoint
			// Nested command calls in a point's coordinates (e.g. Sqrt(3) in
			// (Sqrt(3), 0)) are materialized as synthetic validated objects. The
			// raw coordinate text is kept for other stages; points are not
			// reference-resolved so symbolic coordinates (x, y) never error.
			seq := &synthSeq{}
			var nested []diag.Problem
			o.Args, nested = flattenArgs(g, s.id, s.args, s.lineNo, seq, nil)
			probs = append(probs, nested...)
		case s.numberLiteral != "":
			o.Kind = ir.KNumber
			o.Args = []string{s.numberLiteral}
		case s.boolLiteral != "":
			// A boolean literal defines a KBool object. KBool was previously
			// declared in ir.Kind but unreachable, so every <Boolean> parameter
			// slot had no way to be satisfied.
			o.Kind = ir.KBool
			o.Args = []string{s.boolLiteral}
		case s.literalList != nil:
			// { ... } list literal → a List object. Elements may contain nested
			// command calls (e.g. {Sqrt(2), 3}); flatten first, then resolve refs.
			o.Kind = ir.KList
			seq := &synthSeq{}
			var nested []diag.Problem
			o.Args, nested = flattenArgs(g, s.id, s.literalList, s.lineNo, seq, nil)
			probs = append(probs, nested...)
			refs, undefs := resolveRefs(g, s.cmd, o.Args)
			o.Refs = refs
			for _, u := range undefs {
				probs = append(probs, diag.Problem{
					Code: diag.CodeDepUndefined, Msg: "引用了未定义对象：" + u, Obj: s.id, Line: s.lineNo,
				})
			}
		case s.exprLiteral != "":
			// Raw algebraic expression / function body → a Function-ish object.
			// An expression's free variables (independent var x, etc.) are local,
			// not object refs; only identifiers that are actually defined objects
			// become dependency edges. fnParams are in-scope locals, never refs.
			o.Kind = ir.KFunction
			o.Args = []string{s.exprLiteral}
			bindings := map[string]bool{}
			for _, p := range s.fnParams {
				bindings[p] = true
			}
			refs, undefs := resolveRefsExpr(g, s.exprLiteral, bindings)
			o.Refs = refs
			_ = undefs // free variables are not reported as undefined for expressions
		default:
			// command object; first flatten any nested command calls in the
			// arguments (each becomes a synthetic object in the graph), then
			// resolve refs from the partly-flattened args. Bound variables of
			// the command (Curve/Sequence/Sum/...) stay in scope for nested
			// calls inside its arguments.
			seq := &synthSeq{}
			var nested []diag.Problem
			o.Args, nested = flattenArgs(g, s.id, s.args, s.lineNo, seq, boundVarSet(s.cmd, s.args))
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
	// Validate modifiers after all named objects are known. A modifier defines
	// no object, so it stays out of the graph's object map and out of the
	// executable order — but it is carried on the graph as an ir.Statement so
	// the sig stage can type-check it against the same catalog. Before modifiers
	// were carried this way, only their bare-identifier arguments were
	// ref-checked here, so SetLineStyle(A, 2) with a Point argument passed
	// silently. Nested command calls in the arguments are flattened like for any
	// other command, so SetColor(c, RGB(1,0,0)) reports RGB as unknown; the
	// modifier's own id is "stmtN", matching the synthetic-nested-command ids
	// (cur.cos1, ...) that already appear in the executable order.
	for i, s := range modifiers {
		owner := "stmt" + strconv.Itoa(i+1)
		seq := &synthSeq{}
		args, nested := flattenArgs(g, owner, s.args, s.lineNo, seq, nil)
		probs = append(probs, nested...)
		refs, undefs := resolveRefs(g, s.cmd, args)
		for _, u := range undefs {
			probs = append(probs, diag.Problem{
				Code: diag.CodeDepUndefined, Msg: "引用了未定义对象：" + u, Obj: owner, Line: s.lineNo,
			})
		}
		g.Statements = append(g.Statements, &ir.Statement{
			Cmd:  s.cmd,
			Args: args,
			Refs: refs,
			Line: s.lineNo,
		})
	}
	return g, probs
}

// synthSeq is a counter for generating unique synthetic object ids for nested
// command calls.
type synthSeq struct{ n int }

// flattenArgs rewrites an argument list so that any nested command call found
// in an argument (a whole-argument call such as Midpoint(A,B), or one embedded
// in arithmetic such as Sqrt(3)/2) becomes a synthetic object added to the
// graph, with that call replaced by its synthetic object's id. It recurses so
// arbitrarily deep nesting is captured, and each nested command gets its own
// dependency edges. Applied uniformly to command args, literal point
// coordinates, and list literal elements so nested commands validate wherever
// they appear. bindings are in-scope variable names (e.g. a Curve/Sequence
// parameter) that must not be treated as undefined when they appear inside a
// nested call.
func flattenArgs(g *ir.Graph, owner string, args []string, lineNo int, seq *synthSeq, bindings map[string]bool) ([]string, []diag.Problem) {
	var probs []diag.Problem
	out := make([]string, len(args))
	for i, a := range args {
		rewritten, p := scanFlatten(g, owner, strings.TrimSpace(a), lineNo, seq, bindings)
		probs = append(probs, p...)
		out[i] = rewritten
	}
	return out, probs
}

// scanFlatten rewrites src so that every command call `Func(args)` found
// anywhere in the string — including one embedded in a longer expression such
// as Sqrt(3)/2 or 2*Cos(t) — is materialized as a synthetic object in the
// graph and replaced by that object's id. It recurses bottom-up (innermost
// calls first) so each produced synthetic object carries its own dependency
// edges. Non-call text (numbers, operators, plain identifiers) passes through
// unchanged.
func scanFlatten(g *ir.Graph, owner, src string, lineNo int, seq *synthSeq, bindings map[string]bool) (string, []diag.Problem) {
	var probs []diag.Problem
	var sb strings.Builder
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		if isIdentStart(rune(c)) {
			j := i
			for j < n && isIdentChar(rune(src[j])) {
				j++
			}
			ident := src[i:j]
			// skip whitespace between the name and '(' so `point (1,2)` still
			// reads as a call; this only ever adds precision.
			k := j
			for k < n && (src[k] == ' ' || src[k] == '\t') {
				k++
			}
			if k < n && src[k] == '(' {
				if close := matchParen(src, k); close >= 0 {
					inner := src[k+1 : close]
					rewrittenInner, ip := scanFlatten(g, owner, inner, lineNo, seq, bindings)
					probs = append(probs, ip...)
					callArgs := splitArgs(rewrittenInner)
					seq.n++
					sid := owner + "." + ident + strconv.Itoa(seq.n)
					if _, exists := g.Get(sid); exists {
						probs = append(probs, diag.Problem{
							Code: diag.CodeDepRedefine, Msg: "嵌套命令 id 冲突：" + sid, Obj: owner, Line: lineNo,
						})
					} else {
						g.Add(&ir.Object{ID: sid, Cmd: ident, Line: lineNo})
						obj, _ := g.Get(sid)
						obj.Args = callArgs
						refs, undefs := resolveRefs(g, ident, callArgs)
						// In-scope bound variables of the enclosing command are
						// local symbols, so drop them from the undefined list
						// (e.g. t in Curve(cos(t), ...)).
						if len(bindings) > 0 && len(undefs) > 0 {
							kept := undefs[:0]
							for _, u := range undefs {
								if !bindings[u] {
									kept = append(kept, u)
								}
							}
							undefs = kept
						}
						obj.Refs = refs
						for _, u := range undefs {
							probs = append(probs, diag.Problem{
								Code: diag.CodeDepUndefined, Msg: "引用了未定义对象（嵌套）：" + u, Obj: sid, Line: lineNo,
							})
						}
					}
					sb.WriteString(sid)
					i = close + 1
					continue
				}
			}
			sb.WriteString(ident)
			i = j
			continue
		}
		sb.WriteByte(c)
		i++
	}
	return sb.String(), probs
}

// matchParen finds the index of the closing parenthesis that balances the '('
// at pos, or -1 if unbalanced. Assumes src[pos] == '('.
func matchParen(src string, pos int) int {
	depth := 0
	for i := pos; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// isIdentStart reports whether r can begin an identifier (letters, '_', ':').
// Digits are excluded so a number is never mistaken for a name.
func isIdentStart(r rune) bool {
	return r == '_' || r == ':' || unicode.IsLetter(r)
}

// argCommand is a decomposed command call found on a bare (no "=") line.
type argCommand struct {
	cmd  string
	args []string
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
		if isNumber(a) {
			continue // literal, not a reference
		}
		if isBoolLiteral(a) {
			continue // true/false literal, not a reference
		}
		if _, ok := g.Get(a); ok {
			// a defined object (a named object, or a synthetic nested-command
			// id like A.Midpoint1) is a dependency ref regardless of whether it
			// is a "clean" identifier. A token that is BOTH a reserved constant
			// name (pi/e/...) and a defined object is treated as the object
			// first, matching GeoGebra, where such names are reserved and would
			// not be usable as object ids anyway.
			refs = append(refs, a)
		} else if isIdentName(a) {
			if number.KnownConstant(a) {
				// A reserved numeric constant (pi/e/euler/gamma...) used as an
				// argument and not shadowed by a defined object: not a ref and
				// not an undefined reference.
				continue
			}
			undefs = append(undefs, a)
		}
	}
	return refs, undefs
}

// resolveRefsExpr scans a raw algebraic expression for object references. Unlike
// resolveRefs (whole-arg matching), it tokenizes the expression and only treats
// tokens that are *defined objects* as dependency refs. Free variables (the
// independent var x, trig args, etc.) and explicit fn params are local symbols,
// never refs and never undefined-ref errors.
func resolveRefsExpr(g *ir.Graph, expr string, bindings map[string]bool) (refs, _ []string) {
	seen := map[string]bool{}
	for _, tok := range tokenizeIdentifiers(expr) {
		if seen[tok] || bindings[tok] {
			continue
		}
		seen[tok] = true
		if isNumber(tok) {
			continue
		}
		if isBoolLiteral(tok) {
			continue
		}
		if _, ok := g.Get(tok); ok {
			// A defined object is a dependency ref, even if its name shadows a
			// reserved constant (pi/e/...).
			refs = append(refs, tok)
		}
	}
	return refs, nil
}

// tokenizeIdentifiers extracts maximal runs of identifier characters from s,
// skipping numbers and operators. Used to find object references inside an
// expression string.
func tokenizeIdentifiers(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if isIdentChar(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// isIdentChar reports whether r can appear in an identifier (letters, digits,
// '_', ':' — digits not at the start are handled by the run-level caller).
func isIdentChar(r rune) bool {
	return r == '_' || r == ':' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

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
	case "CURVE":
		// Curve(x_e, y_e, t, a, b)          (2D, 5 args)
		// Curve(x_e, y_e, z_e, t, a, b)     (3D, 6 args)
		// The parameter variable is the 3rd arg in 2D, the 4th in 3D.
		if len(args) == 6 {
			out[3] = true
		} else if len(args) >= 3 {
			out[2] = true
		}
	case "SURFACE":
		// Surface(x, y, z, u, u_min, u_max, t, t_min, t_max) — vars at 3 and 6.
		if len(args) >= 4 {
			out[3] = true
		}
		if len(args) >= 7 {
			out[6] = true
		}
	default:
		// no bound variables
	}
	return out
}

// boundVarSet returns the set of bound-variable names of a command's args (the
// iteration/parameter variables it binds), used so nested command calls inside
// those args treat the variable as a local symbol rather than an undefined ref.
func boundVarSet(cmd string, args []string) map[string]bool {
	mark := boundVars(cmd, args)
	var set map[string]bool
	for i, yes := range mark {
		if !yes || i >= len(args) {
			continue
		}
		// the arg at a bound position is the variable's name
		name := strings.TrimSpace(args[i])
		if isIdentName(name) {
			if set == nil {
				set = map[string]bool{}
			}
			set[name] = true
		}
	}
	return set
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
// isBoolLiteral reports whether s is the GeoGebra boolean literal true or
// false, case-insensitively. These are literals, not object references: treating
// them as identifiers made `ShowAxes(false)` report "undefined object: false".
//
// GeoGebra writes booleans bare — the Slider manual documents its <Is Angle>
// parameter as "can be true or false", default false.
func isBoolLiteral(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "false":
		return true
	}
	return false
}

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
// at the first '#' that is NOT inside a double-quoted string literal; a '#' in
// a string argument (e.g. SetCaption(c, "Answer #1")) is part of the string,
// not a comment. The grammar does not use backslash escapes inside strings, so
// a plain scan for "…" ranges is sufficient.
func stripComment(line string) string {
	inStr := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inStr = !inStr
		case '#':
			if !inStr {
				return line[:i]
			}
		}
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
