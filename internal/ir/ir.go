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

// Graph is the shared object graph. Insertion order is preserved.
type Graph struct {
	Objects map[string]*Object
	Order   []string
	Goals   []string
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

// kindForCmd maps a command name to the coarse Kind it produces, for commands
// whose result type is well-known and needed by later stages (kind checks,
// degeneracy). Commands outside the map produce KUnknown and are still checked
// for signature/args.
func kindForCmd(cmd string) Kind {
	switch cmd {
	case "Point", "PointIn", "Midpoint", "Vertex", "Intersect":
		return KPoint
	case "Line", "LineThrough", "PerpendicularLine", "ParallelLine",
		"Tangent", "PerpendicularBisector", "AngleBisector":
		return KLine
	case "Segment", "Side":
		return KSegment
	case "Ray":
		return KRay
	case "Vector":
		return KVector
	case "Circle", "CircleWithCenter", "CircleByRadiusM", "Semicircle":
		return KCircle
	case "Polygon", "Polyline":
		return KPolygon
	case "Distance", "Length", "Perimeter", "Area", "Angle", "Slope",
		"Radius", "Volume", "Circumference", "abs", "Abs":
		return KNumber
	// 3D: map coarse result kinds so signature/geo checks stay consistent. A
	// surface/solid command yields a Quadric- or Solid-typed object, and a
	// Plane command yields a Plane.
	case "Sphere", "Cone", "Cylinder", "Quadric", "Ellipsoid", "Hyperboloid",
		"Surface":
		return KQuadric
	case "Plane", "OrthogonalPlane", "PerpendicularPlane", "ParallelPlane":
		return KPlane
	case "Cube", "Prism", "Pyramid", "Polyhedron", "Tetrahedron", "Octahedron",
		"Hexahedron", "Icosahedron", "Dodecahedron":
		return KSolid
	default:
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
