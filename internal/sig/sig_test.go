package sig

import (
	"testing"

	"github.com/you/geogebra-dsl-go/internal/catalog"
	"github.com/you/geogebra-dsl-go/internal/ir"
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
