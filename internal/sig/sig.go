// Package sig resolves whether an object (command + resolved ref kinds)
// matches any overload in the catalog, by parameter count and coarse kinds.
package sig

import (
	"strings"

	"github.com/you/geogebra-dsl-go/internal/catalog"
	"github.com/you/geogebra-dsl-go/internal/ir"
)

// kindAliases maps catalog type tokens (as they appear in a TypeExpr, after
// splitting on "/") to the ir.Kind they represent. Some catalog types are
// wildcards that accept any object.
var kindTokens = map[string]struct {
	kind ir.Kind
	any  bool
}{
	"Point":      {kind: ir.KPoint},
	"Line":       {kind: ir.KLine},
	"Segment":    {kind: ir.KSegment},
	"Ray":        {kind: ir.KRay},
	"Vector":     {kind: ir.KVector},
	"Circle":     {kind: ir.KCircle},
	"Conic":      {kind: ir.KConic},
	"Polygon":    {kind: ir.KPolygon},
	"Number":     {kind: ir.KNumber},
	"Boolean":    {kind: ir.KBool},
	"Function":   {kind: ir.KFunction},
	"List":       {kind: ir.KList},
	"Plane":      {kind: ir.KPlane},
	"Quadric":    {kind: ir.KQuadric},
	"Solid":      {kind: ir.KSolid},
	"Polyhedron": {kind: ir.KPolyhedron},
	"GeoObject":  {any: true}, // generic object wildcard
	"Object":     {any: true},
	"Expression": {any: true}, // treat as wildcard; deep expr parsing is out of v1 scope
	"Variable":   {any: true},
	"Interval":   {kind: ir.KNumber},
	"Region":     {any: true},
}

// compatible reports whether an actual ir.Kind satisfies a catalog type token.
func tokenAccepts(token string, actual ir.Kind) bool {
	token = strings.TrimSpace(token)
	if t, ok := kindTokens[token]; ok {
		if t.any {
			return actual != ir.KUnknown && actual != ir.KScript
		}
		return t.kind == actual
	}
	// Unknown token: be lenient (don't reject on a type we can't model yet).
	return true
}

// Match holds the outcome of matching an object against the catalog.
type Match struct {
	Known      bool // command exists in catalog
	OK         bool // at least one overload accepted (known && args+types)
	Matched    *catalog.Overload
	ParamCount []int  // accepted param counts, for diagnostics
	Explain    string // human text when !OK
}

// Lookup matches an object against the catalog. g provides the kinds of the
// object's refs; args that are literal tokens (numbers etc.) are treated as
// literals, not refs.
func Lookup(c *catalog.Catalog, g *ir.Graph, o *ir.Object) Match {
	cmd, known := c.Lookup(o.Cmd)
	if !known {
		return Match{Known: false, Explain: "命令 " + o.Cmd + " 不在命令表里"}
	}
	counts := map[int]bool{}
	var matched *catalog.Overload
	for i := range cmd.Overloads {
		ov := &cmd.Overloads[i]
		if len(o.Args) != len(ov.Params) && !ov.IsVarArg {
			continue
		}
		if ov.IsVarArg {
			if len(o.Args) < len(ov.Params) {
				continue
			}
		} else {
			counts[len(ov.Params)] = true
		}
		if kindsMatch(ov, g, o) {
			matched = ov
			break
		}
	}
	if matched != nil {
		return Match{Known: true, OK: true, Matched: matched}
	}
	pc := make([]int, 0, len(counts))
	for c := range counts {
		pc = append(pc, c)
	}
	return Match{
		Known:      true,
		OK:         false,
		ParamCount: pc,
		Explain:    "命令 " + o.Cmd + " 的参数个数或类型不匹配任何签名",
	}
}

// kindsMatch checks the positional kinds of o.Args against an overload. Each
// positional arg is mapped to a kind: a bare identifier that resolves in g is
// that object's kind; a numeric/other literal contributes no kind (treated as
// satisfying a Number-ish/unknown slot only — handled by matching combinatorics
// below). For v1 we match on the kinds of refs by position where the arg is a
// ref;
func kindsMatch(ov *catalog.Overload, g *ir.Graph, o *ir.Object) bool {
	if len(o.Args) != len(ov.Params) {
		return false
	}
	for i, arg := range o.Args {
		param := ov.Params[i]
		actual := argKind(g, arg)
		if !paramAccepts(param.Type, actual) {
			return false
		}
	}
	return true
}

// argKind resolves an individual argument expression to an ir.Kind. Bare
// identifiers resolve to the referenced object's kind; anything else (numbers,
// coordinate lists, arithmetic) resolves to KUnknown which is lenient below.
func argKind(g *ir.Graph, arg string) ir.Kind {
	arg = strings.TrimSpace(arg)
	if isIdent(arg) {
		if obj, ok := g.Get(arg); ok {
			return obj.Kind
		}
		return ir.KUnknown // undefined name; build stage reports it
	}
	return ir.KUnknown
}

// paramAccepts reports whether a catalog param type accepts an actual kind.
func paramAccepts(t catalog.TypeExpr, actual ir.Kind) bool {
	for _, tok := range t.Alternatives() {
		if tokenAccepts(tok, actual) {
			return true
		}
	}
	// Lenient: actual KUnknown (a literal we don't classify) passes an
	// untyped/Number slot rather than failing a well-typed command.
	if actual == ir.KUnknown {
		return true
	}
	return false
}

// isIdent reports whether s is a plain identifier (object name).
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		ok := r == '_' || r == ':' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}
