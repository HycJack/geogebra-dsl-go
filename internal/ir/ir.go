// Package ir defines the single shared object graph produced by both input
// shapes (text script and IR JSON). Every later stage (deps, geo, reach) only
// reads an *ir.Graph; the seam the design isolates is this package.
package ir

import "strings"

// Kind is the coarse geometric type of an object. The catalog expresses every
// overloaded param type as one of these (or an "or" of several, resolved via
// CompatibleType below).
type Kind int

const (
	KUnknown Kind = iota
	KPoint
	KLine
	KSegment
	KRay
	KVector
	KCircle
	KConic
	KPolygon
	KNumber
	KBool
	KFunction
	KObject // generic GeoObject wildcard
	KList
	KPlane
	KQuadric
	KSolid
	KPolyhedron
	KScript // free point, never directly usable
	// Non-geometric GeoGebra object kinds. Appended (not inserted) so the
	// existing numeric values never shift. Before these existed, every <Text>,
	// <Matrix>, <Polynomial>, <Curve>, <Locus>, <Set> or <Turtle> parameter slot
	// was unmodeled: the checker could reject a Point there but had no positive
	// way to accept the object that really belongs, because no command was
	// mapped to such a kind.
	KText       // Text("<label>", <Point>), ReadText
	KMatrix     // Matrix(...)
	KPolynomial // Polynomial(...)
	KCurve      // Curve(...)
	KLocus      // Locus(...)
	KSet        // Union, Difference
	KTurtle     // Turtle(...)
)

// String returns the canonical lowercase name for a Kind.
func (k Kind) String() string {
	switch k {
	case KPoint:
		return "Point"
	case KLine:
		return "Line"
	case KSegment:
		return "Segment"
	case KRay:
		return "Ray"
	case KVector:
		return "Vector"
	case KCircle:
		return "Circle"
	case KConic:
		return "Conic"
	case KPolygon:
		return "Polygon"
	case KNumber:
		return "Number"
	case KBool:
		return "Boolean"
	case KFunction:
		return "Function"
	case KObject:
		return "Object"
	case KList:
		return "List"
	case KPlane:
		return "Plane"
	case KQuadric:
		return "Quadric"
	case KSolid:
		return "Solid"
	case KPolyhedron:
		return "Polyhedron"
	case KScript:
		return "Script"
	case KText:
		return "Text"
	case KMatrix:
		return "Matrix"
	case KPolynomial:
		return "Polynomial"
	case KCurve:
		return "Curve"
	case KLocus:
		return "Locus"
	case KSet:
		return "Set"
	case KTurtle:
		return "Turtle"
	default:
		return "Unknown"
	}
}

// Object is one node in the graph.
type Object struct {
	ID   string   `json:"id"`
	Cmd  string   `json:"cmd,omitempty"`  // command name as written ("" for literals)
	Kind Kind     `json:"kind,omitempty"` // resolved coarse kind
	Args []string `json:"args,omitempty"` // raw arg expressions
	Refs []string `json:"refs,omitempty"` // resolved object ids this depends on
	Line int      `json:"-"`              // 1-based source line (text input)
}

// Statement is a statement-style command (SetColor, ShowAxes, ...) that is
// validated against the catalog but deliberately is not a graph object: it has
// no id, adds no geometry and no goal, and is not part of the executable order.
// It is carried on the Graph so the sig stage can type-check it against the
// same catalog. Before this existed modifiers were ref-checked only, so
// `SetLineStyle(A, 2)` with a Point argument passed silently.
type Statement struct {
	Cmd  string   // command name as written
	Args []string // raw arg expressions
	Refs []string // resolved object ids this depends on
	Line int      // 1-based source line (text input)
}

// Graph is the shared object graph. Insertion order is preserved.
type Graph struct {
	Objects    map[string]*Object
	Order      []string
	Goals      []string
	Statements []*Statement
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{Objects: map[string]*Object{}}
}

// Add inserts (or replaces) an object, preserving insertion order.
func (g *Graph) Add(o *Object) {
	if _, ok := g.Objects[o.ID]; !ok {
		g.Order = append(g.Order, o.ID)
	}
	g.Objects[o.ID] = o
}

// Get returns the object by id.
func (g *Graph) Get(id string) (*Object, bool) {
	o, ok := g.Objects[id]
	return o, ok
}

// SetGoals records the target ids (empty for text input; caller computes top-level).
func (g *Graph) SetGoals(ids []string) { g.Goals = ids }

// copyArgs returns a fresh copy of the arg slice (avoid aliasing callers).
func copyArgs(a []string) []string {
	if a == nil {
		return nil
	}
	out := make([]string, len(a))
	copy(out, a)
	return out
}

// RefKinds returns the Kind of each resolved ref in order.
func (g *Graph) RefKinds(o *Object) []Kind {
	out := make([]Kind, 0, len(o.Refs))
	for _, r := range o.Refs {
		if obj, ok := g.Objects[r]; ok {
			out = append(out, obj.Kind)
		}
	}
	return out
}

// scriptingCommands is the official GeoGebra "Scripting Commands" category
// (67 commands), verified against
// https://geogebra.github.io/docs/manual/en/commands/Scripting_Commands/ .
// That page documents the rule both uses of this set encode:
//
//	"These commands don't return any object, therefore cannot be nested in
//	 other commands."
//
// They live here rather than in the text package because two independent
// consumers need the same set: the text builder (a bare no-"=" line is only
// legal for one of these) and kindForCmd (a nested call to one of these is a
// script statement, not a value). Kept as an explicit table rather than an
// "everything is a modifier" rule: a bare `Circle(A, B)` must remain a parse
// error, because Circle does return an object and writing it bare is how a
// typo'd assignment looks.
var scriptingCommands = map[string]bool{
	"ATTACHCOPYTOVIEW":         true,
	"BUTTON":                   true,
	"CENTERVIEW":               true,
	"CHECKBOX":                 true,
	"COPYFREEOBJECT":           true,
	"DELETE":                   true,
	"EXECUTE":                  true,
	"EXPORTIMAGE":              true,
	"GETTIME":                  true,
	"HIDELAYER":                true,
	"INPUTBOX":                 true,
	"PAN":                      true,
	"PARSETOFUNCTION":          true,
	"PARSETONUMBER":            true,
	"PLAYSOUND":                true,
	"READTEXT":                 true,
	"RENAME":                   true,
	"REPEAT":                   true,
	"RUNCLICKSCRIPT":           true,
	"RUNUPDATESCRIPT":          true,
	"SELECTOBJECTS":            true,
	"SETACTIVEVIEW":            true,
	"SETAXESRATIO":             true,
	"SETBACKGROUNDCOLOR":       true,
	"SETCAPTION":               true,
	"SETCOLOR":                 true,
	"SETCONDITIONTOSHOWOBJECT": true,
	"SETCONSTRUCTIONSTEP":      true,
	"SETCOORDS":                true,
	"SETDECORATION":            true,
	"SETDYNAMICCOLOR":          true,
	"SETFILLING":               true,
	"SETFIXED":                 true,
	"SETIMAGE":                 true,
	"SETLABELMODE":             true,
	"SETLAYER":                 true,
	"SETLEVELOFDETAIL":         true,
	"SETLINEOPACITY":           true,
	"SETLINESTYLE":             true,
	"SETLINETHICKNESS":         true,
	"SETPERSPECTIVE":           true,
	"SETPOINTSIZE":             true,
	"SETPOINTSTYLE":            true,
	"SETSEED":                  true,
	"SETSPINSPEED":             true,
	"SETTOOLTIPMODE":           true,
	"SETTRACE":                 true,
	"SETVALUE":                 true,
	"SETVIEWDIRECTION":         true,
	"SETVISIBLEINVIEW":         true,
	"SHOWAXES":                 true,
	"SHOWGRID":                 true,
	"SHOWLABEL":                true,
	"SHOWLAYER":                true,
	"SLIDER":                   true,
	"STARTANIMATION":           true,
	"STARTRECORD":              true,
	"TURTLE":                   true,
	"TURTLEBACK":               true,
	"TURTLEDOWN":               true,
	"TURTLEFORWARD":            true,
	"TURTLELEFT":               true,
	"TURTLERIGHT":              true,
	"TURTLEUP":                 true,
	"UPDATECONSTRUCTION":       true,
	"ZOOMIN":                   true,
	"ZOOMOUT":                  true,
}

// IsScriptingCommand reports whether cmd (any case) is an official GeoGebra
// Scripting command — one that returns no object.
func IsScriptingCommand(cmd string) bool {
	return scriptingCommands[strings.ToUpper(cmd)]
}

// kindForCmd maps a command name to the coarse Kind it produces, for commands
// whose result type is well-known and needed by later stages (kind checks,
// degeneracy). Commands outside the map produce KUnknown and are still checked
// for signature/args.
func kindForCmd(cmd string) Kind {
	switch strings.ToUpper(cmd) {
	case "POINT", "POINTIN", "MIDPOINT", "VERTEX", "INTERSECT":
		return KPoint
	case "LINE", "LINETHROUGH", "PERPENDICULARLINE", "PARALLELLINE",
		"TANGENT", "PERPENDICULARBISECTOR", "ANGLEBISECTOR":
		return KLine
	case "SEGMENT", "SIDE":
		return KSegment
	case "RAY":
		return KRay
	case "VECTOR":
		return KVector
	case "CIRCLE", "CIRCLEWITHCENTER", "CIRCLEBYRADIUSM", "SEMICIRCLE",
		"CIRCUMCIRCLE":
		return KCircle
	case "ELLIPSE", "PARABOLA", "HYPERBOLA", "CONIC":
		return KConic
	// Triangle center commands all yield a Point. Mapped so their results are
	// type-checked instead of falling through to KUnknown (which the lenient
	// fallback would silently accept anywhere).
	case "CIRCUMCENTER", "INCENTER", "CENTROID", "ORTHOCENTER", "TRIANGLECENTER":
		return KPoint
	case "POLYGON", "POLYLINE":
		return KPolygon
	case "DISTANCE", "LENGTH", "PERIMETER", "AREA", "ANGLE", "SLOPE",
		"RADIUS", "VOLUME", "CIRCUMFERENCE", "ABS", "SIGN",
		"FLOOR", "CEIL", "ROUND":
		return KNumber
	// Built-in scalar math functions (sqrt, trig, log/exp, roots) produce a
	// Number value. Matched case-insensitively so sqrt/sin/ln (and Sqrt/Sin)
	// all resolve consistently with the catalog. Mapped so nested
	// coordinate/argument uses keep the right coarse kind for signature matching.
	case "SQRT", "CBRT", "NROOT", "EXP", "LN", "LOG", "LOG10",
		"SIN", "COS", "TAN", "COT", "SEC", "CSC",
		"ARCSIN", "ARCCOS", "ARCTAN", "ARCCOT", "ARCSEC", "ARCCSC":
		return KNumber
	// 3D: map coarse result kinds so signature/geo checks stay consistent. A
	// surface/solid command yields a Quadric- or Solid-typed object, and a
	// Plane command yields a Plane.
	case "SPHERE", "CONE", "CYLINDER", "QUADRIC", "ELLIPSOID", "HYPERBOLOID",
		"SURFACE":
		return KQuadric
	case "PLANE", "ORTHOGONALPLANE", "PERPENDICULARPLANE", "PARALLELPLANE":
		return KPlane
	case "CUBE", "PRISM", "PYRAMID", "POLYHEDRON", "TETRAHEDRON", "OCTAHEDRON",
		"HEXAHEDRON", "ICOSAHEDRON", "DODECAHEDRON":
		// Polyhedra, not just "Solids": Net(<Polyhedron>, ...) and
		// Vertex(<Polyhedron>, ...) require the Polyhedron type, and a
		// Tetrahedron is a convex polyhedron. KSolid was also unreachable from
		// any other command, so KPolyhedron is the correct coarse kind.
		return KPolyhedron
	// Lists and sequences yield KList. Mapped so <List>-typed slots are checked
	// against the real kind instead of the KUnknown lenient fallback.
	case "LIST", "SEQUENCE":
		return KList
	// Sliders and GetTime are Scripting commands that nevertheless yield a
	// usable number: StartAnimation(<Point or Slider>, ...) consumes them.
	// Without a specific case they fall through to KScript, which a
	// <Point or Slider> slot rightly refuses.
	case "SLIDER", "GETTIME":
		return KNumber
	// Non-geometric object kinds. Each maps only commands that unambiguously
	// produce that kind, so the result is type-checked rather than falling
	// through to KUnknown.
	case "TEXT", "READTEXT":
		return KText
	case "MATRIX":
		return KMatrix
	case "POLYNOMIAL":
		return KPolynomial
	case "CURVE", "SPLINE":
		return KCurve
	case "LOCUS":
		return KLocus
	case "UNION", "DIFFERENCE":
		return KSet
	case "TURTLE":
		return KTurtle
	// An arc and an implicit curve are partial curves; GeoGebra's conic slots
	// accept them, and mapping them to KConic keeps <Arc>/<ImplicitCurve>
	// checkable instead of unmodeled.
	case "ARC", "CIRCULARARC", "CIRCUMCIRCULARARC", "IMPLICITCURVE":
		return KConic
	default:
		// A nested scripting call is a statement, not a value. Giving it a real
		// kind lets Repeat(<Number>, <Scripting Command>, ...) check that its
		// arguments really are scripting commands instead of accepting a Number
		// or a Point, and keeps them out of the "Any"-style wildcard slots,
		// matching the manual's rule that these cannot be nested. This is the
		// fallback: a specific case above wins first, because some scripting
		// commands (Turtle, Slider, Button, Checkbox, GetTime, ReadText) do
		// create a usable object that later commands consume.
		if IsScriptingCommand(cmd) {
			return KScript
		}
		return KUnknown
	}
}

// SetKindFromCmd assigns each object's Kind from its command name when the kind
// is still unknown (text input). For IR input Kind is present already.
func (g *Graph) SetKindFromCmd() {
	for _, id := range g.Order {
		o := g.Objects[id]
		if o.Kind != KUnknown {
			continue
		}
		o.Kind = kindForCmd(o.Cmd)
	}
}
