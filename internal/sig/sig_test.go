package sig

import (
	"strings"
	"testing"

	"github.com/hycjack/geogebra-dsl-go/internal/catalog"
	"github.com/hycjack/geogebra-dsl-go/internal/ir"
)

// testCatalog is a minimal catalog with a couple of commands, independent of
// the embedded rich catalog (so these tests stay hermetic).
func testCatalog() *catalog.Catalog {
	const js = `{"commands": {
  "Point": {"name":"Point","overloads":[
    {"syntax":"Point(<Object>)","params":[{"name":"<Object>","role":"o","type":"GeoObject","optional":false}]},
    {"syntax":"Point(<Point>, <Vector>)","params":[{"name":"<Point>","role":"s","type":"Point"},{"name":"<Vector>","role":"v","type":"Vector"}]}
  ]},
  "Line": {"name":"Line","overloads":[
    {"syntax":"Line(<Point>, <Point>)","params":[{"name":"<Point>","role":"a","type":"Point"},{"name":"<Point>","role":"b","type":"Point"}]},
    {"syntax":"Line(<Point>, <Parallel Line>)","params":[{"name":"<Point>","role":"p","type":"Point"},{"name":"<Line>","role":"l","type":"Line"}]}
  ]},
  "Circle": {"name":"Circle","overloads":[
    {"syntax":"Circle(<Point>, <Segment>)","params":[{"name":"<Point>","role":"c","type":"Point"},{"name":"<Segment>","role":"s","type":"Segment"}]},
    {"syntax":"Circle(<Point>, <Point>)","params":[{"name":"<Point>","role":"c","type":"Point"},{"name":"<Point>","role":"p","type":"Point"}]},
    {"syntax":"Circle(<Point>, <Number>)","params":[{"name":"<Point>","role":"c","type":"Point"},{"name":"<Number>","role":"r","type":"Number"}]}
  ]},
  "Polyline": {"name":"Polyline","overloads":[
    {"syntax":"Polyline(<Point>, <Point>, ...)","params":[{"name":"<Point>","role":"p","type":"Point"},{"name":"<Point>","role":"q","type":"Point"}]}
  ]},
  "Center": {"name":"Center","overloads":[
    {"syntax":"Center(<Conic>)","params":[{"name":"<Conic>","role":"k","type":"Conic"}]},
    {"syntax":"Center(<Quadric>)","params":[{"name":"<Quadric>","role":"q","type":"Quadric"}]}
  ]},
  "Union": {"name":"Union","overloads":[
    {"syntax":"Union(<Set>, <Set>)","params":[{"name":"<Set>","role":"A","type":"Set"},{"name":"<Set>","role":"B","type":"Set"}]}
  ]},
  "ShowAxes": {"name":"ShowAxes","overloads":[
    {"syntax":"ShowAxes(<Boolean>)","params":[{"name":"<Boolean>","role":"f","type":"Boolean"}]}
  ]},
  "Text": {"name":"Text","overloads":[
    {"syntax":"Text(<String>, <Point>)","params":[{"name":"<String>","role":"s","type":"String"},{"name":"<Point>","role":"p","type":"Point"}]}
  ]},
  "Matrix": {"name":"Matrix","overloads":[
    {"syntax":"Matrix(<Number>, <Number>, <Number>, <Number>)","params":[{"name":"<Number>","role":"a","type":"Number"},{"name":"<Number>","role":"b","type":"Number"},{"name":"<Number>","role":"c","type":"Number"},{"name":"<Number>","role":"d","type":"Number"}]}
  ]},
  "Dimension": {"name":"Dimension","overloads":[
    {"syntax":"Dimension(<VectorOrMatrix>)","params":[{"name":"<VectorOrMatrix>","role":"v","type":"VectorOrMatrix"}]}
  ]},
  "SetLineStyle": {"name":"SetLineStyle","overloads":[
    {"syntax":"SetLineStyle(<LineOrSegmentOrPolyline>)","params":[{"name":"<LineOrSegmentOrPolyline>","role":"l","type":"LineOrSegmentOrPolyline"}]}
  ]}
}}`
	c, err := catalog.Load([]byte(js))
	if err != nil {
		panic(err)
	}
	return c
}

func graphWith(t *testing.T, objs ...*ir.Object) *ir.Graph {
	t.Helper()
	g := ir.New()
	for _, o := range objs {
		g.Add(o)
	}
	return g
}

func TestLookupLineOk(t *testing.T) {
	g := graphWith(t,
		&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"0", "0"}},
		&ir.Object{ID: "B", Kind: ir.KPoint, Args: []string{"1", "1"}},
		&ir.Object{ID: "l", Cmd: "Line", Args: []string{"A", "B"}, Refs: []string{"A", "B"}},
	)
	m := Lookup(testCatalog(), g, g.Objects["l"])
	if !m.OK || !m.Known {
		t.Fatalf("expected ok match, got %+v", m)
	}
}

func TestLookupLineWrongType(t *testing.T) {
	// Line(A, A) where A is a Number -> no overload accepts two non-Points for
	// <Point>,<Point>; but Line(<Point>,<Line>) also no. Both refs must be Point.
	g := graphWith(t,
		&ir.Object{ID: "A", Kind: ir.KNumber, Args: []string{"0"}},
		&ir.Object{ID: "l", Cmd: "Line", Args: []string{"A", "A"}, Refs: []string{"A", "A"}},
	)
	m := Lookup(testCatalog(), g, g.Objects["l"])
	if m.OK {
		t.Fatalf("expected no match, got %+v", m)
	}
}

func TestLookupUnknownCmd(t *testing.T) {
	g := graphWith(t, &ir.Object{ID: "x", Cmd: "Nope", Args: []string{"1"}})
	m := Lookup(testCatalog(), g, g.Objects["x"])
	if m.Known {
		t.Fatal("expected unknown")
	}
	if !strings.Contains(m.Explain, "Nope") {
		t.Fatalf("explain should name the offending command, got %q", m.Explain)
	}
}

func TestLookupUnknownCmdSuggests(t *testing.T) {
	// A typo must name a plausible catalog command so the caller sees the
	// correct GeoGebra spelling instead of a bare rejection.
	g := graphWith(t, &ir.Object{ID: "x", Cmd: "Lne", Args: []string{"A", "B"}})
	m := Lookup(testCatalog(), g, g.Objects["x"])
	if m.Known {
		t.Fatal("expected unknown")
	}
	if !strings.Contains(m.Explain, "Line") {
		t.Fatalf("expected a Line suggestion in %q", m.Explain)
	}
}

func TestLookupUnknownCmdFarFromAllSuggestsNothing(t *testing.T) {
	g := graphWith(t, &ir.Object{ID: "x", Cmd: "Zqxwvplmnbkjh", Args: []string{"1"}})
	m := Lookup(testCatalog(), g, g.Objects["x"])
	if strings.Contains(m.Explain, "可能是") || strings.Contains(m.Explain, "相近命令") {
		t.Fatalf("expected no suggestion for an unrelated name, got %q", m.Explain)
	}
}

func TestLookupNumberLiteralNotPoint(t *testing.T) {
	// A bare number literal must not fill a Point slot: Circle(0, 3) has no
	// overload that accepts two Numbers, so it must not match.
	g := graphWith(t, &ir.Object{ID: "c", Cmd: "Circle", Args: []string{"0", "3"}})
	m := Lookup(testCatalog(), g, g.Objects["c"])
	if m.OK {
		t.Fatalf("expected no match for Circle(0,3), got OK; explain=%s", m.Explain)
	}
}

func TestLookupConstantExprIsNumber(t *testing.T) {
	// A constant arithmetic expression like "2*pi" is a numeric value and fills
	// a Number slot (Circle(C, 2*pi)) but not a Point slot.
	g := graphWith(t,
		&ir.Object{ID: "C", Kind: ir.KPoint, Args: []string{"1", "1"}},
		&ir.Object{ID: "c", Cmd: "Circle", Args: []string{"C", "2*pi"}},
	)
	m := Lookup(testCatalog(), g, g.Objects["c"])
	if !m.OK {
		t.Fatalf("expected Circle(C, 2*pi) to match Number slot, explain=%s", m.Explain)
	}
	// As a bare "coordinate-ish" arg it must not replace a Point on its own.
	g2 := graphWith(t, &ir.Object{ID: "c2", Cmd: "Circle", Args: []string{"2*pi", "0"}})
	m2 := Lookup(testCatalog(), g2, g2.Objects["c2"])
	if m2.OK {
		t.Fatalf("expected no match for Circle(2*pi,0), got OK")
	}
}

func TestLookupVarArgAcceptsExtraArgs(t *testing.T) {
	// A vararg command (Polyline) declared with N fixed params must accept a
	// call with MORE than N args — the extra args are the variable tail. This
	// guards against the regression where kindsMatch rejected any overload
	// whose args outnumbered its params, breaking valid calls like
	// ANOVA(l1,l2,l3) / Polyline(A,B,C) / PenStroke(...).
	g := graphWith(t,
		&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"0", "0"}},
		&ir.Object{ID: "B", Kind: ir.KPoint, Args: []string{"1", "1"}},
		&ir.Object{ID: "C", Kind: ir.KPoint, Args: []string{"2", "2"}},
		&ir.Object{ID: "pl", Cmd: "Polyline", Args: []string{"A", "B", "C"}, Refs: []string{"A", "B", "C"}},
	)
	m := Lookup(testCatalog(), g, g.Objects["pl"])
	if !m.OK {
		t.Fatalf("Polyline(A,B,C) with 3 args (2 fixed + vararg) should match, got %+v (explain=%s)", m, m.Explain)
	}
}

func TestLookupCircleNumberOk(t *testing.T) {
	g := graphWith(t,
		&ir.Object{ID: "C", Kind: ir.KPoint, Args: []string{"1", "1"}},
		&ir.Object{ID: "r", Kind: ir.KNumber, Args: []string{"3"}},
		&ir.Object{ID: "c", Cmd: "Circle", Args: []string{"C", "r"}, Refs: []string{"C", "r"}},
	)
	m := Lookup(testCatalog(), g, g.Objects["c"])
	if !m.OK {
		t.Fatalf("expected Circle(<Point>,<Number>) to match, got %+v (explain=%s)", m, m.Explain)
	}
}

func TestLookupConicAcceptsCircle(t *testing.T) {
	// A circle is a conic section, so Center(<Conic>) must accept a
	// Circle-typed object. Real GeoGebra accepts this; the flat Kind enum keeps
	// Circle and Conic distinct for granularity, so the subtype relation must
	// live in subkind rather than being an exact-equality miss.
	g := graphWith(t,
		&ir.Object{ID: "c", Kind: ir.KCircle, Args: []string{"A", "B", "C"}},
		&ir.Object{ID: "O", Cmd: "Center", Args: []string{"c"}, Refs: []string{"c"}},
	)
	m := Lookup(testCatalog(), g, g.Objects["O"])
	if !m.OK {
		t.Fatalf("expected Center(<Conic>) to accept a Circle, got %+v (explain=%s)", m, m.Explain)
	}
}

func TestLookupConicRejectsPoint(t *testing.T) {
	// The subtype relation must not widen arbitrarily: a Point is not a conic.
	g := graphWith(t,
		&ir.Object{ID: "P", Kind: ir.KPoint, Args: []string{"1", "1"}},
		&ir.Object{ID: "O", Cmd: "Center", Args: []string{"P"}, Refs: []string{"P"}},
	)
	m := Lookup(testCatalog(), g, g.Objects["O"])
	if m.OK {
		t.Fatalf("expected Center(<Conic>) to reject a Point, got OK")
	}
}

// fullCatalog is the embedded rich catalog (cached), including the cmdmeta
// command→result-kind data. Used by tests that exercise kind resolution, since
// kindForCmd now lives in catalog data rather than the minimal testCatalog.
func fullCatalog() *catalog.Catalog {
	c, err := catalog.Default()
	if err != nil {
		panic(err)
	}
	return c
}

func TestKindForCmdConicsAndCenters(t *testing.T) {
	// Commands that used to fall through to KUnknown (and so slipped through the
	// lenient fallback everywhere) must now resolve to a real kind.
	want := map[string]ir.Kind{
		"Circumcircle":   ir.KCircle,
		"Ellipse":        ir.KConic,
		"Incenter":       ir.KPoint,
		"Circumcenter":   ir.KPoint,
		"TriangleCenter": ir.KPoint,
	}
	for cmd, k := range want {
		g := graphWith(t, &ir.Object{ID: "o", Cmd: cmd})
		fullCatalog().ApplyKinds(g)
		if got := g.Objects["o"].Kind; got != k {
			t.Errorf("%s: expected kind %v, got %v", cmd, k, got)
		}
	}
}

func TestCenterCircumcircle(t *testing.T) {
	// End-to-end: Circumcircle yields a Circle kind, and Center(<Conic>) then
	// accepts it through the subtype rule — no reliance on the KUnknown
	// lenient fallback.
	g := graphWith(t,
		&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"0", "0"}},
		&ir.Object{ID: "B", Kind: ir.KPoint, Args: []string{"1", "0"}},
		&ir.Object{ID: "C", Kind: ir.KPoint, Args: []string{"0", "1"}},
		&ir.Object{ID: "c", Cmd: "Circumcircle", Args: []string{"A", "B", "C"}},
		&ir.Object{ID: "O", Cmd: "Center", Args: []string{"c"}, Refs: []string{"c"}},
	)
	fullCatalog().ApplyKinds(g)
	m := Lookup(testCatalog(), g, g.Objects["O"])
	if !m.OK {
		t.Fatalf("expected Center(Circumcircle(...)) to match, got %+v (explain=%s)", m, m.Explain)
	}
}

func TestUnmodeledTokenStillStrict(t *testing.T) {
	// <Set> is modeled as KSet, so this is the modeled path: a Point must not be
	// accepted where a Set is expected. The checker stays strict against
	// GeoGebra's syntax whenever the actual kind is known. This closes the hole
	// that let TriangleCenter(Point, 4) and TriangleCenter(4, 4) validate, and
	// it stays closed once <Set> moved from unmodeled-reject to modeled-reject:
	// the acceptance rule is "actual kind ⊑ expected kind", not "expected token
	// unknown, so accept".
	g := graphWith(t,
		&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"0", "0"}},
		&ir.Object{ID: "B", Kind: ir.KPoint, Args: []string{"1", "0"}},
		&ir.Object{ID: "u", Cmd: "Union", Args: []string{"A", "B"}, Refs: []string{"A", "B"}},
	)
	if m := Lookup(testCatalog(), g, g.Objects["u"]); m.OK {
		t.Fatalf("expected Union(<Point>,<Point>) to be rejected, got OK")
	}
	// And a number literal in a <Set> slot is rejected too.
	g2 := graphWith(t, &ir.Object{ID: "u", Cmd: "Union", Args: []string{"1", "2"}})
	if m := Lookup(testCatalog(), g2, g2.Objects["u"]); m.OK {
		t.Fatalf("expected Union(1,2) to be rejected, got OK")
	}
}

func TestUnmodeledTokenLenientForUnknownActual(t *testing.T) {
	// Results of commands we have not mapped to a kind remain lenient: DESIGN.md
	// documents "字面量参数宽松通过", so an unmapped operand in an unmodeled slot
	// must not fail a well-formed command.
	g := graphWith(t,
		&ir.Object{ID: "l", Cmd: "SomeUnknownCmd", Args: []string{"1"}},
		&ir.Object{ID: "u", Cmd: "Union", Args: []string{"l", "l"}, Refs: []string{"l"}},
	)
	if m := Lookup(testCatalog(), g, g.Objects["u"]); !m.OK {
		t.Fatalf("expected lenient match for unmapped operands, got %+v (explain=%s)", m, m.Explain)
	}
}

func TestBooleanLiteralSatisfiesBooleanSlot(t *testing.T) {
	// ShowAxes(false) is the canonical case: false used to be ref-resolved as an
	// undefined object, and KBool was unreachable from the parser.
	g := graphWith(t, &ir.Object{ID: "s", Cmd: "ShowAxes", Args: []string{"false"}})
	m := Lookup(testCatalog(), g, g.Objects["s"])
	if !m.Known {
		t.Fatalf("ShowAxes should be a known command")
	}
	if !m.OK {
		t.Fatalf("expected ShowAxes(false) to match, got %+v (explain=%s)", m, m.Explain)
	}
	// A KBool object (x = false) fills the slot through its kind, not as a literal.
	g2 := graphWith(t,
		&ir.Object{ID: "x", Kind: ir.KBool, Args: []string{"false"}},
		&ir.Object{ID: "s", Cmd: "ShowAxes", Args: []string{"x"}, Refs: []string{"x"}},
	)
	if m := Lookup(testCatalog(), g2, g2.Objects["s"]); !m.OK {
		t.Fatalf("expected ShowAxes(<KBool>) to match, got %+v (explain=%s)", m, m.Explain)
	}
}

func TestBooleanSlotRejectsNumber(t *testing.T) {
	// Strictness: a number is not a boolean. GeoGebra accepts `ShowAxes(0)` in
	// some contexts, but our coarse model must not silently let 0 fill a <Boolean>
	// slot, the same way it must not let a Point fill a <Set> slot.
	g := graphWith(t, &ir.Object{ID: "s", Cmd: "ShowAxes", Args: []string{"0"}})
	m := Lookup(testCatalog(), g, g.Objects["s"])
	if !m.Known || m.OK {
		t.Fatalf("expected ShowAxes(0) to be rejected, got %+v", m)
	}
}

func TestTokenAlternatives(t *testing.T) {
	// tokenAlternatives is what makes <List<Number>> (18 spellings), <VectorOrList>
	// (union) and <Expression f(x,y)> (labelled) resolve instead of falling to the
	// strict-reject path for an unknown token.
	cases := []struct {
		in   string
		want []string
	}{
		{"Point", []string{"Point"}},
		{"List<Number>", []string{"List"}},
		{"List of Numbers", []string{"List"}},
		{"ListOfText", []string{"List"}},
		{"List<PointOrSlider>", []string{"List"}}, // must NOT split on Or
		{"VectorOrList", []string{"Vector", "List"}},
		{"LineOrSegmentOrPolyline", []string{"Line", "Segment", "Polyline"}},
		{"StringOrNumber", []string{"String", "Number"}},
		{"Expression f(x,y)", []string{"Expression"}},
		{"Expression y'(t)", []string{"Expression"}},
		{"Function f(x)", []string{"Function"}},
		{"FunctionName", []string{"FunctionName"}}, // exact hit beats the prefix rule
		{"Equation in A,B,C", []string{"Equation"}},
		{"Axis Direction or Plane", []string{"Axis Direction or Plane"}}, // exact hit, no split
		{"Geometric Object", []string{"Geometric Object"}},
		{"Spline", []string{"Spline"}},
		{"SomeTotallyUnmodeledToken", []string{"SomeTotallyUnmodeledToken"}},
	}
	for _, c := range cases {
		got := tokenAlternatives(c.in)
		if !equalStrs(got, c.want) {
			t.Errorf("tokenAlternatives(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestModeledObjectKindsAccepted(t *testing.T) {
	// The new coarse kinds must be ACCEPTABLE in their slots, not merely
	// non-rejecting. Before KText/KMatrix/KSet existed, these slots were
	// unmodeled and could reject a well-typed argument.
	g := graphWith(t,
		&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"0", "0"}},
		&ir.Object{ID: "t", Kind: ir.KText, Args: []string{"hi"}},
		&ir.Object{ID: "m", Kind: ir.KMatrix, Args: []string{"1", "2", "3", "4"}},
		&ir.Object{ID: "u", Kind: ir.KSet, Args: []string{"a", "b"}},
		&ir.Object{ID: "x", Cmd: "Text", Args: []string{"t", "A"}, Refs: []string{"t", "A"}},
		&ir.Object{ID: "y", Cmd: "Matrix", Args: []string{"1", "2", "3", "4"}},
		&ir.Object{ID: "z", Cmd: "Union", Args: []string{"u", "u"}, Refs: []string{"u"}},
	)
	for _, id := range []string{"x", "y", "z"} {
		if m := Lookup(testCatalog(), g, g.Objects[id]); !m.OK {
			t.Errorf("%s: expected match, got %+v (explain=%s)", id, m, m.Explain)
		}
	}
}

func TestModeledObjectKindsRejectWrongKind(t *testing.T) {
	// Modeling a kind must not weaken strictness: a Point is still not a Matrix,
	// and a Matrix is still not a Set.
	g := graphWith(t,
		&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"0", "0"}},
		&ir.Object{ID: "m", Kind: ir.KMatrix, Args: []string{"1", "2", "3", "4"}},
		&ir.Object{ID: "y", Cmd: "Matrix", Args: []string{"A", "A", "A", "A"}, Refs: []string{"A"}},
		&ir.Object{ID: "z", Cmd: "Union", Args: []string{"m", "m"}, Refs: []string{"m"}},
	)
	for _, id := range []string{"y", "z"} {
		if m := Lookup(testCatalog(), g, g.Objects[id]); m.OK {
			t.Errorf("%s: expected rejection, got OK", id)
		}
	}
}

func TestUnionTypeToken(t *testing.T) {
	// <VectorOrMatrix> is satisfied by either member and rejected by a third kind.
	g := graphWith(t,
		&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"0", "0"}},
		&ir.Object{ID: "v", Kind: ir.KVector, Args: []string{"A", "A"}},
		&ir.Object{ID: "m", Kind: ir.KMatrix, Args: []string{"1", "2", "3", "4"}},
		&ir.Object{ID: "d1", Cmd: "Dimension", Args: []string{"v"}, Refs: []string{"v"}},
		&ir.Object{ID: "d2", Cmd: "Dimension", Args: []string{"m"}, Refs: []string{"m"}},
		&ir.Object{ID: "d3", Cmd: "Dimension", Args: []string{"A"}, Refs: []string{"A"}},
	)
	if m := Lookup(testCatalog(), g, g.Objects["d1"]); !m.OK {
		t.Fatalf("Dimension(<Vector>) should match, got %+v", m)
	}
	if m := Lookup(testCatalog(), g, g.Objects["d2"]); !m.OK {
		t.Fatalf("Dimension(<Matrix>) should match, got %+v", m)
	}
	if m := Lookup(testCatalog(), g, g.Objects["d3"]); m.OK {
		t.Fatalf("Dimension(<Point>) should be rejected, got OK")
	}
}

func TestUnionTypeTokenPolylineMember(t *testing.T) {
	// LineOrSegmentOrPolyline: all three members accepted, KPoint rejected.
	g := graphWith(t,
		&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"0", "0"}},
		&ir.Object{ID: "l", Kind: ir.KLine, Args: []string{"A", "A"}},
		&ir.Object{ID: "s", Kind: ir.KSegment, Args: []string{"A", "A"}},
		&ir.Object{ID: "p", Kind: ir.KPolygon, Args: []string{"A", "A", "A"}},
		&ir.Object{ID: "o1", Cmd: "SetLineStyle", Args: []string{"l"}, Refs: []string{"l"}},
		&ir.Object{ID: "o2", Cmd: "SetLineStyle", Args: []string{"s"}, Refs: []string{"s"}},
		&ir.Object{ID: "o3", Cmd: "SetLineStyle", Args: []string{"p"}, Refs: []string{"p"}},
		&ir.Object{ID: "o4", Cmd: "SetLineStyle", Args: []string{"A"}, Refs: []string{"A"}},
	)
	for _, id := range []string{"o1", "o2", "o3"} {
		if m := Lookup(testCatalog(), g, g.Objects[id]); !m.OK {
			t.Errorf("%s: expected match, got %+v (explain=%s)", id, m, m.Explain)
		}
	}
	if m := Lookup(testCatalog(), g, g.Objects["o4"]); m.OK {
		t.Fatalf("SetLineStyle(<Point>) should be rejected, got OK")
	}
}

func TestKindForCmdNewObjectKinds(t *testing.T) {
	// The new kinds must be reachable from the parser path (cmd name → kind),
	// otherwise they can never be produced and their slots stay effectively
	// unmodeled.
	g := ir.New()
	for id, cmd := range map[string]string{
		"t": "Text", "m": "Matrix", "q": "Polynomial", "c": "Curve",
		"s": "Locus", "u": "Union", "tt": "Turtle", "ls": "List", "sq": "Sequence",
	} {
		g.Add(&ir.Object{ID: id, Cmd: cmd})
	}
	fullCatalog().ApplyKinds(g)
	want := map[string]ir.Kind{
		"t": ir.KText, "m": ir.KMatrix, "q": ir.KPolynomial, "c": ir.KCurve,
		"s": ir.KLocus, "u": ir.KSet, "tt": ir.KTurtle, "ls": ir.KList, "sq": ir.KList,
	}
	for id, k := range want {
		if got := g.Objects[id].Kind; got != k {
			t.Errorf("%s: kind=%s, want %s", id, got, k)
		}
	}
}

func TestKindForCmdPolyhedra(t *testing.T) {
	// Convex-polyhedron commands must map to KPolyhedron, not KSolid: Net and
	// Vertex both declare <Polyhedron>, so Net(Tetrahedron(A,B,C), 0.5) had to
	// be rejected before this fix.
	g := ir.New()
	for id, cmd := range map[string]string{
		"tet": "Tetrahedron", "cube": "Cube", "pr": "Prism", "py": "Pyramid",
		"ph": "Polyhedron", "oct": "Octahedron", "do": "Dodecahedron",
	} {
		g.Add(&ir.Object{ID: id, Cmd: cmd})
	}
	fullCatalog().ApplyKinds(g)
	for id := range map[string]string{
		"tet": "Tetrahedron", "cube": "Cube", "pr": "Prism", "py": "Pyramid",
		"ph": "Polyhedron", "oct": "Octahedron", "do": "Dodecahedron",
	} {
		if got := g.Objects[id].Kind; got != ir.KPolyhedron {
			t.Errorf("%s: kind=%s, want Polyhedron", id, got)
		}
	}
}

func TestSubkindPolyhedronIsSolid(t *testing.T) {
	// A polyhedron is a solid, so a <Solid> slot accepts one; the reverse does
	// not hold.
	if !subkind(ir.KSolid, ir.KPolyhedron) {
		t.Error("expected KPolyhedron to satisfy a KSolid slot")
	}
	if subkind(ir.KPolyhedron, ir.KSolid) {
		t.Error("a KSolid must not satisfy a KPolyhedron slot")
	}
	if !subkind(ir.KConic, ir.KCircle) {
		t.Error("existing KCircle -> KConic relation broken")
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestArgMismatchExplainCarriesSignatures — regression: a rejected overload
// must name the accepted syntaxes (from the catalog) so the AI repair loop (and
// humans) can self-correct instead of guessing. ParamCount used to be computed
// and discarded; the syntaxes replace it.
func TestArgMismatchExplainCarriesSignatures(t *testing.T) {
	g := graphWith(t, &ir.Object{ID: "O", Kind: ir.KPoint, Args: []string{"0", "0"}})
	g.Add(&ir.Object{ID: "c1", Cmd: "Circle", Args: []string{"O"}, Refs: []string{"O"}})
	m := Lookup(testCatalog(), g, g.Objects["c1"])
	if m.OK {
		t.Fatal("expected Circle(O) to be rejected (needs 2 args)")
	}
	if !strings.Contains(m.Explain, "Circle(<Point>, <Segment>)") {
		t.Fatalf("explain should carry the accepted overload syntaxes, got %q", m.Explain)
	}
	if strings.Contains(m.Explain, "（命令表未提供签名）") {
		t.Fatalf("explain should not be the empty-syntax fallback, got %q", m.Explain)
	}
}
