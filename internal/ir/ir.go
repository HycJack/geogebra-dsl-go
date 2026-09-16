// Package ir defines the single shared object graph produced by both input
// shapes (text script and IR JSON). Every later stage (deps, geo, reach) only
// reads an *ir.Graph; the seam the design isolates is this package.
package ir

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
	// Params carries the parameter names of a function definition
	// (`f(x, y) = ...`) for text input. It distinguishes a Function object
	// (params present) from a bare expression object (`y = x^2 + 1`, no params)
	// so later stages can reclassify numeric expressions without turning a
	// function body into a Number. Not part of the IR JSON shape.
	Params []string `json:"-"`
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

// KindFromToken is the inverse of Kind.String(): it maps a canonical kind name
// (as stored in the catalog's cmdmeta returns data, e.g. "Point" or "Number")
// back to a Kind. An unknown or empty token yields KUnknown.
func KindFromToken(token string) Kind {
	switch token {
	case "Point":
		return KPoint
	case "Line":
		return KLine
	case "Segment":
		return KSegment
	case "Ray":
		return KRay
	case "Vector":
		return KVector
	case "Circle":
		return KCircle
	case "Conic":
		return KConic
	case "Polygon":
		return KPolygon
	case "Number":
		return KNumber
	case "Boolean":
		return KBool
	case "Function":
		return KFunction
	case "Object":
		return KObject
	case "List":
		return KList
	case "Plane":
		return KPlane
	case "Quadric":
		return KQuadric
	case "Solid":
		return KSolid
	case "Polyhedron":
		return KPolyhedron
	case "Script":
		return KScript
	case "Text":
		return KText
	case "Matrix":
		return KMatrix
	case "Polynomial":
		return KPolynomial
	case "Curve":
		return KCurve
	case "Locus":
		return KLocus
	case "Set":
		return KSet
	case "Turtle":
		return KTurtle
	default:
		return KUnknown
	}
}
