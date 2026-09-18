// Package sig resolves whether an object (command + resolved ref kinds)
// matches any overload in the catalog, by parameter count and coarse kinds.
package sig

import (
	"fmt"
	"strings"

	"github.com/hycjack/geogebra-dsl-go/internal/catalog"
	"github.com/hycjack/geogebra-dsl-go/internal/ir"
	"github.com/hycjack/geogebra-dsl-go/internal/number"
)

// kindTokens maps catalog type tokens (as they appear in a TypeExpr, after
// splitting on "/" and "Or") to the ir.Kind they represent. Some catalog types
// are wildcards that accept any object.
//
// Groups:
//   - core geometric kinds,
//   - non-geometric GeoGebra objects (Text/Matrix/Polynomial/Curve/Locus/Set/Turtle),
//   - numeric subtypes that collapse to KNumber,
//   - wildcards for types outside our coarse model.
//
// <List<...>> and <List of ...> variants are handled by kindForToken's prefix
// rule rather than enumerated here — there are ~18 spellings and they are all
// KList.
//
// The "any" wildcards are deliberately split into two classes (see
// strictWildcardObjectTokens): <Object>/<GeoObject>/<Region>/... reference a
// real geometric object and so must reject a bare value literal, while
// <Expression>/<Symbol>/<Variable>/<Name>/... are expression or name slots
// where a number is a legal argument (Sequence(2, k, 1, 10), If(cond, 1, 2)).
var kindTokens = map[string]struct {
	kind ir.Kind
	any  bool
}{
	"Point":            {kind: ir.KPoint},
	"Line":             {kind: ir.KLine},
	"Segment":          {kind: ir.KSegment},
	"Ray":              {kind: ir.KRay},
	"Vector":           {kind: ir.KVector},
	"Circle":           {kind: ir.KCircle},
	"Conic":            {kind: ir.KConic},
	"Ellipse":          {kind: ir.KConic}, // an ellipse is a conic
	"Parabola":         {kind: ir.KConic},
	"Polygon":          {kind: ir.KPolygon},
	"Polyline":         {kind: ir.KPolygon}, // a polyline is a degenerate polygon
	"Sector":           {kind: ir.KPolygon}, // a sector is a filled polygon
	"Number":           {kind: ir.KNumber},
	"Slider":           {kind: ir.KNumber}, // a slider holds a number
	"Boolean":          {kind: ir.KBool},
	"Function":         {kind: ir.KFunction},
	"List":             {kind: ir.KList},
	"Plane":            {kind: ir.KPlane},
	"Quadric":          {kind: ir.KQuadric},
	"Surface":          {kind: ir.KQuadric}, // GeoGebra's Surface command yields a quadric
	"Solid":            {kind: ir.KSolid},
	"Polyhedron":       {kind: ir.KPolyhedron},
	"Text":             {kind: ir.KText},
	"String":           {kind: ir.KText}, // GeoGebra's <String> is a text literal/slot
	"Matrix":           {kind: ir.KMatrix},
	"Polynomial":       {kind: ir.KPolynomial},
	"Curve":            {kind: ir.KCurve},
	"Spline":           {kind: ir.KCurve}, // a spline is a curve
	"Locus":            {kind: ir.KLocus},
	"Set":              {kind: ir.KSet},
	"Turtle":           {kind: ir.KTurtle},
	"ScriptingCommand": {kind: ir.KScript}, // Repeat's second slot: a real call
	// Arc and implicit curves are partial curves; GeoGebra accepts them in
	// conic slots, and matching them here is what makes <Arc> checkable instead
	// of silently rejecting every known kind.
	"Arc":              {kind: ir.KConic},
	"ImplicitCurve":    {kind: ir.KConic},
	"Implicit Curve":   {kind: ir.KConic},
	"GeoObject":        {any: true}, // generic object wildcard
	"Any":              {any: true}, // GeoGebra's catch-all: accepts any object type
	"Object":           {any: true},
	"Geometric Object": {any: true},
	"Expression":       {any: true}, // treat as wildcard; deep expr parsing is out of v1 scope
	"Variable":         {any: true},
	"Interval":         {kind: ir.KNumber},
	"Region":           {any: true},
	// Numeric subtypes. GeoGebra writes <Integer>/<Real>/<Angle> for value slots
	// and a bare number literal fills them — an angle measure is just a number of
	// degrees. Our coarse model cannot tell 3 from 3.5, so these collapse to
	// KNumber; the leniency is one-directional, matching real GeoGebra.
	"Integer":       {kind: ir.KNumber},
	"Real":          {kind: ir.KNumber},
	"Complex":       {kind: ir.KNumber},
	"ComplexNumber": {kind: ir.KNumber},
	"Angle":         {kind: ir.KNumber},
	// Single-token value-ish slots, all plain numbers in practice: the value a
	// limit is taken at, a summation bound, an intersection index, a stretch ratio,
	// a parameter value, and a screen/view index.
	"Value":     {kind: ir.KNumber},
	"Infinity":  {kind: ir.KNumber},
	"Index":     {kind: ir.KNumber},
	"Ratio":     {kind: ir.KNumber},
	"Parameter": {kind: ir.KNumber},
	// A view index is an integer, and a screen point is a point in window
	// coordinates rather than in the construction — both collapse to the coarse
	// kinds we do track.
	"ViewIndex":   {kind: ir.KNumber},
	"ScreenPoint": {kind: ir.KPoint},
	"Sequence":    {kind: ir.KList},
	// Wildcards: these describe expressions, names, UI widgets or spreadsheet
	// cells, none of which our coarse model types. Being a wildcard (rather than
	// unmodeled) is the difference between "accept anything" and "reject every
	// known kind", and accepting is what GeoGebra does here.
	"Quadratic Function": {any: true},
	"Boolean expression": {any: true},
	"Symbol":             {any: true},
	"Equation":           {any: true},
	"Inequality":         {any: true},
	"FunctionName":       {any: true},
	"Name":               {any: true},
	"Keyword":            {any: true},
	"Button":             {any: true},
	"ActionObject":       {any: true},
	"Image":              {any: true},
	"GraphicsView":       {any: true},
	"Spreadsheet Cell":   {any: true},
	"Column":             {any: true},
	"Row":                {any: true},
	"Cell":               {any: true},
	"CellRange":          {any: true},
	"Start Cell":         {any: true},
	"End Cell":           {any: true},
	"Face":               {any: true},
	"Edge":               {any: true},
	"Axes":               {any: true},
	// Rotate(<Object>, <Angle>, <Axis of Rotation>) is the 3D form of a
	// rotation about a line. Wildcarding it let a bare number fill the axis
	// slot — Rotate(sq, ang, 5) validated even though the <Point> overload
	// correctly rejected the same script. "Axis Direction or Plane" is a
	// genuine union of three real kinds; see unionTokens.
	"Axis of Rotation":                     {kind: ir.KLine},
	"SurfaceOr3DObject":                    {any: true},
	"3DObject":                             {any: true},
	"PointOrObjectWithPosition":            {any: true},
	"Enum(-1|0|1)":                         {any: true},
	"Composite(ListOfText+FrequencyTable)": {any: true},
}

// strictWildcardObjectTokens holds the catalog type tokens that our coarse
// model types as a wildcard but which, in GeoGebra, are references to a real
// geometric object — a <Object>/<GeoObject> transform target, a <Region>, an
// <Image>, a spreadsheet <Cell>, a scripting <ActionObject>. They accept any
// object kind, but they must NOT accept a bare value literal: in GeoGebra a
// number is not an object, so `Point(0, 2)`, `Point(2)`, `Rotate(0, 90, O)`
// and `Delete(0)` do not construct anything.
//
// The remaining "any" wildcards are expression or name slots, where a number is
// a perfectly legal argument: Sequence(2, k, 1, 10), Sum(2, k, 1, 3),
// If(cond, 1, 2), `Text(0)` (any expression rendered as text), the numeric
// enum `StemPlot(l, -1)`. Those stay permissive and are listed here only so the
// two classes are visible next to each other:
//
//	Expression / Expression …  Symbol  Variable  Name  FunctionName
//	Keyword  Equation  Inequality  Boolean expression  Any  Quadratic Function
//	Enum(-1|0|1)
//
// Keeping them permissive is what stops this rule from becoming a blunt
// instrument: Sequence(2, k, 1, 10) must keep validating.
var strictWildcardObjectTokens = map[string]bool{
	"Object":                               true,
	"GeoObject":                            true,
	"Geometric Object":                     true,
	"Region":                               true,
	"Image":                                true,
	"GraphicsView":                         true,
	"PointOrObjectWithPosition":            true,
	"SurfaceOr3DObject":                    true,
	"3DObject":                             true,
	"Composite(ListOfText+FrequencyTable)": true,
	"Spreadsheet Cell":                     true,
	"Column":                               true,
	"Row":                                  true,
	"Cell":                                 true,
	"CellRange":                            true,
	"Start Cell":                           true,
	"End Cell":                             true,
	"Face":                                 true,
	"Edge":                                 true,
	"Axes":                                 true,
	"Button":                               true,
	"ActionObject":                         true,
}

// unionTokens maps a catalog type token that is genuinely a union of two or
// more concrete kinds to those kinds. kindTokens holds one kind per token,
// which cannot express "Line, Vector or Plane". Such a token is returned
// whole by tokenAlternatives (it contains a space) and would otherwise be a
// wildcard, which let any value — including a bare number — fill the slot.
//
// For Rotate(<Object>, <Angle>, <Point on Axis>, <Axis Direction or Plane>)
// the axis direction may be given as the axis line itself, as a direction
// vector, or as the plane of rotation.
var unionTokens = map[string][]ir.Kind{
	"Axis Direction or Plane": {ir.KLine, ir.KVector, ir.KPlane},
}

// subkind reports whether actual is accepted where want is required.
func subkind(want, actual ir.Kind) bool {
	if actual == want {
		return true
	}
	switch want {
	case ir.KConic:
		return actual == ir.KCircle
	case ir.KSolid:
		// A polyhedron is a solid, so a <Solid> slot also accepts one.
		return actual == ir.KPolyhedron
	}
	return false
}

// tokenAccepts reports whether an actual ir.Kind satisfies a catalog type token.
func tokenAccepts(token string, actual ir.Kind) bool {
	// An object wildcard does not accept a value literal: a bare number or
	// boolean is never an object in GeoGebra, so Point(0, 2) must not be read
	// as "the point on object 0 at parameter 2". Expression/name wildcards
	// (<Expression>, <Symbol>, <Variable>, <Name>, <Any>, the numeric enums)
	// are left permissive on purpose — see strictWildcardObjectTokens.
	if strictWildcardObjectTokens[token] && isValueLiteralKind(actual) {
		return false
	}
	for _, alt := range tokenAlternatives(token) {
		if ks, ok := unionTokens[alt]; ok {
			for _, k := range ks {
				if subkind(k, actual) {
					return true
				}
			}
			continue
		}
		t, ok := kindTokens[alt]
		if !ok {
			continue
		}
		if t.any {
			if actual != ir.KUnknown && actual != ir.KScript {
				return true
			}
		} else if subkind(t.kind, actual) {
			return true
		}
	}
	// No recognized alternative: the expected type is outside our coarse model.
	// Commands are still checked strictly against GeoGebra's syntax, so reject
	// whenever we CAN judge — the actual object has a known kind (a Point is not
	// a <Set>, a Number is not a <Polynomial>). Stay lenient only for KUnknown
	// actuals, which are results of commands we have not mapped plus unclassified
	// literals; that is the "字面量参数宽松通过" leniency DESIGN.md documents.
	if actual == ir.KUnknown {
		return true
	}
	return false
}

// isValueLiteralKind reports whether kind is a bare value literal (a number or
// a boolean). Those kinds can fill a value slot (<Number>, <Boolean>,
// <Angle>, ...) and an expression/name slot, but never an object slot.
func isValueLiteralKind(k ir.Kind) bool {
	return k == ir.KNumber || k == ir.KBool
}

// tokenAlternatives expands one catalog type token into the atomic tokens it
// means, so a union type is satisfied by any of its members:
//
//	"VectorOrList"              → Vector, List
//	"LineOrSegmentOrPolyline"   → Line, Segment, Polyline
//	"StringOrNumber"            → String, Number
//	"List<Number>" / "List of Numbers" / "ListOfText" → List
//
// Tokens containing spaces or brackets are returned whole: "Axis Direction or
// Plane" and "List<PointOrSlider>" must NOT be split on "Or" or "<". An exact
// table hit always wins and is checked first, because a token like
// "Axis Direction or Plane" is a wildcard in the table and splitting it on the
// lowercase "or" would destroy it.
func tokenAlternatives(token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if _, ok := kindTokens[token]; ok {
		return []string{token}
	}
	if strings.HasPrefix(strings.ToLower(token), "list") {
		// ~18 spellings of a list of something; all are KList in our model, and
		// the element type is not tracked.
		return []string{"List"}
	}
	if strings.HasPrefix(strings.ToLower(token), "expression") {
		// <Expression f(x,y)>, <Expression y'(t)>, <Expression in (x,y)>: a
		// labelled expression. Prefix-checked rather than exact because the label
		// varies and all of them are untyped expressions.
		return []string{"Expression"}
	}
	if strings.HasPrefix(strings.ToLower(token), "function") {
		// <Function f(x)>, <Function b(x)> (SolveODE): a labelled function.
		// "FunctionName" hits the exact table above first, so it stays a wildcard.
		return []string{"Function"}
	}
	if strings.HasPrefix(strings.ToLower(token), "equation") {
		// <Equation in A,B,C> (TriangleCurve): a labelled equation.
		return []string{"Equation"}
	}
	if !strings.ContainsAny(token, "<>() ") {
		return strings.Split(token, "Or")
	}
	return []string{token}
}

// Match holds the outcome of matching an object against the catalog.
type Match struct {
	Known   bool // command exists in catalog
	OK      bool // at least one overload accepted (known && args+types)
	Matched *catalog.Overload
	Explain string // human text when !OK
}

// unknownExplain builds the diagnostic for a command that is not in the table.
// Besides saying it's unknown, it points at the nearest catalog command and its
// official GeoGebra manual URL, so an AI or a human sees the correct syntax
// instead of just a rejection.
func unknownExplain(c *catalog.Catalog, name string) string {
	msg := "命令 " + name + " 不在命令表里"
	sugg := c.Suggest(name, 3)
	if len(sugg) == 0 {
		return msg
	}
	if cmd, ok := c.Lookup(sugg[0]); ok && cmd.URL != "" {
		msg += "；GeoGebra 中可能是 " + sugg[0] + "，官方用法见 " + cmd.URL
	} else {
		msg += "；相近命令：" + strings.Join(sugg, " / ")
	}
	return msg
}

// Lookup matches an object against the catalog. g provides the kinds of the
// object's refs; args that are literal tokens (numbers etc.) are treated as
// literals, not refs.
func Lookup(c *catalog.Catalog, g *ir.Graph, o *ir.Object) Match {
	cmd, known := c.Lookup(o.Cmd)
	if !known {
		return Match{Known: false, Explain: unknownExplain(c, o.Cmd)}
	}
	var matched *catalog.Overload
	for i := range cmd.Overloads {
		ov := &cmd.Overloads[i]
		if ov.IsVarArg {
			// The minimum is the number of REQUIRED params, not the total:
			// trailing optionals may all be omitted. Comparing against len
			// (Params) made valid GeoGebra usages fail — Join({1,2},{3,4}),
			// Zip(f, k, l) with only the first pair, If(c, then, c, then) with
			// no Else, and ExportImage() with every named option omitted.
			required, _ := paramCountRange(ov.Params)
			if len(o.Args) < required {
				continue
			}
		} else if required, acceptable := paramCountRange(ov.Params); len(o.Args) < required || len(o.Args) > acceptable {
			continue
		}
		if kindsMatch(ov, g, o) {
			matched = ov
			break
		}
	}
	if matched != nil {
		return Match{Known: true, OK: true, Matched: matched}
	}
	explain := "命令 " + o.Cmd + " 的参数个数或类型不匹配任何签名；正确签名：" + syntaxList(cmd.Overloads)
	if allValueLiteralArgs(g, o) {
		explain += coordinateHint(o.Cmd)
	}
	return Match{Known: true, OK: false, Explain: explain}
}

// allValueLiteralArgs reports whether every argument of o is a bare value
// literal — a number, a constant arithmetic expression, or true/false — with no
// object reference among them. That is the signature of a coordinate-style call
// (Point(0, 2), Vector(1, 1), Circle(0, 0, 2)) aimed at a command whose
// overloads all want objects, so the diagnostic can point at the coordinate
// syntax that actually works.
func allValueLiteralArgs(g *ir.Graph, o *ir.Object) bool {
	if len(o.Args) == 0 {
		return false
	}
	for _, a := range o.Args {
		if !isValueLiteralKind(argKind(g, a)) {
			return false
		}
	}
	return true
}

// coordinateHint appends the GeoGebra coordinate syntax for a call that passed
// value literals where the overloads want objects, so an AI repair loop can
// rewrite the line rather than guessing against the listed overloads.
func coordinateHint(cmd string) string {
	hint := "；这些参数都是数字/布尔字面量，但匹配到的重载要的是对象引用——先把它们建成对象再传引用"
	if strings.EqualFold(cmd, "Point") {
		hint += "；构造点请写 A = (x, y) 或 A = (x, y, z)，也可 Point({x, y}) / Point((x, y, z))"
	}
	return hint
}

// syntaxList renders the accepted overload syntaxes of a command for a
// diagnostic. It collects every unique syntax first (so the total is honest),
// then shows the first three plus a count of the rest — the total is the part
// that tells the LLM repair loop "you can keep guessing against more
// signatures", not just the three shown.
func syntaxList(overloads []catalog.Overload) string {
	seen := map[string]bool{}
	var all []string
	for _, ov := range overloads {
		s := strings.TrimSpace(ov.Syntax)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		all = append(all, s)
	}
	if len(all) == 0 {
		return "（命令表未提供签名）"
	}
	const shown = 3
	if len(all) <= shown {
		return strings.Join(all, "；")
	}
	return strings.Join(all[:shown], "；") + fmt.Sprintf("；等共 %d 种", len(all))
}

// paramCountRange reports the minimum required argument count and the maximum
// acceptable count for an overload, respecting trailing optional params. An
// overload with no required params and trailing optionals accepts 0..total.
func paramCountRange(params []catalog.Param) (required, max int) {
	max = len(params)
	for _, p := range params {
		if p.Optional {
			break // trailing optionals from here on
		}
		required++
	}
	return
}

// kindsMatch checks the positional kinds of o.Args against an overload. Each
// positional arg is mapped to a kind: a bare identifier that resolves in g is
// that object's kind; a numeric/other literal contributes no kind (treated as
// satisfying a Number-ish/unknown slot only — handled by matching combinatorics
// below). For v1 we match on the kinds of refs by position where the arg is a
// ref. For a vararg overload the fixed params are still checked positionally,
// but additional args beyond them are accepted (barring a concrete constraint
// we can't model at the coarse-kind level).
func kindsMatch(ov *catalog.Overload, g *ir.Graph, o *ir.Object) bool {
	for i, arg := range o.Args {
		if i >= len(ov.Params) {
			// Past the fixed params. For a vararg the extra args are accepted;
			// a non-vararg overload should never reach here (Lookup filtered
			// the count), but guard anyway.
			break
		}
		param := ov.Params[i]
		actual := argKind(g, arg)
		if !paramAccepts(param.Type, actual) {
			return false
		}
	}
	return true
}

// argKind resolves an individual argument expression to an ir.Kind. Boolean
// literals resolve to KBool; bare identifiers resolve to the referenced
// object's kind; a pure numeric value (a number literal or a constant arithmetic
// expression) resolves to KNumber so it can fill a Number slot but never
// impersonate a named-type slot (Point, Line, ...). Anything else resolves to
// KUnknown, which is lenient below.
func argKind(g *ir.Graph, arg string) ir.Kind {
	arg = strings.TrimSpace(arg)
	if isBoolLiteral(arg) {
		return ir.KBool
	}
	if isIdent(arg) {
		if obj, ok := g.Get(arg); ok {
			return obj.Kind
		}
		return ir.KUnknown // undefined name; build/reach stage reports it
	}
	if _, ok := number.Eval(arg); ok {
		return ir.KNumber
	}
	return ir.KUnknown
}

// isBoolLiteral reports whether s is GeoGebra's true/false literal,
// case-insensitively. Duplicated from the text package rather than imported:
// sig type-checks the graph and must not depend on the parser that built it.
func isBoolLiteral(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "false":
		return true
	}
	return false
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

// isIdent reports whether s is a plain identifier (object name): letters,
// digits, '_', ':' or '.' — but not starting with a digit, so a bare number
// like "0" or a malformed token like "2ab" is never mistaken for a reference.
// The '.' is accepted because the builder mints synthetic ids for nested
// command calls using it as a separator (A.Midpoint1, stmt1.TurtleForward2),
// and those ids are reachable from argument positions. Without it argKind
// resolved every nested call to KUnknown, so a nested command's result kind was
// never consulted and any slot accepted it — Midpoint(Line(A,B), C) passed.
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	first := s[0]
	if !(first == '_' || ('a' <= first && first <= 'z') || ('A' <= first && first <= 'Z')) {
		return false
	}
	for _, r := range s {
		ok := r == '_' || r == ':' || r == '.' ||
			(('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9'))
		if !ok {
			return false
		}
	}
	return true
}
