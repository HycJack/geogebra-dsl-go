package sig

import (
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
