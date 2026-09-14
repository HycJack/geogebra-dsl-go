package text

import (
	"testing"

	"github.com/hycjack/geogebra-dsl-go/internal/diag"
	"github.com/hycjack/geogebra-dsl-go/internal/ir"
)

func TestParseDispatch(t *testing.T) {
	src := `# comment
A = Point(0, 2)
l = Line(A, B)
M = (1, 2)
r = 3`
	stmts, probs := Parse(src)
	if len(probs) != 0 {
		t.Fatalf("unexpected parse problems: %v", probs)
	}
	if len(stmts) != 4 {
		t.Fatalf("expected 4 statements, got %d", len(stmts))
	}
	if stmts[0].cmd != "Point" {
		t.Errorf("stmt0 cmd=%q", stmts[0].cmd)
	}
	if !stmts[2].literalPoint {
		t.Errorf("stmt2 should be literal point")
	}
	if stmts[3].numberLiteral != "3" {
		t.Errorf("stmt3 numberLiteral=%q", stmts[3].numberLiteral)
	}
}

func TestBuildKindsAndRefs(t *testing.T) {
	src := "A = Point(0, 2)\nB = Point(4, 2)\nl = Line(A, B)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems: %v", probs)
	}
	// Kind resolution is centralized in ir.SetKindFromCmd (run later by check);
	// Build guarantees structure + refs, not kinds.
	g.SetKindFromCmd()
	a := g.Objects["A"]
	if a.Kind != ir.KPoint {
		t.Errorf("A kind=%v, want Point", a.Kind)
	}
	l := g.Objects["l"]
	if len(l.Refs) != 2 || l.Refs[0] != "A" || l.Refs[1] != "B" {
		t.Errorf("l refs=%v", l.Refs)
	}
}

func TestUndefinedRefReported(t *testing.T) {
	src := "A = Point(0,2)\nl = Line(A, Missing)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 1 {
		t.Fatalf("expected 1 problem, got %v", probs)
	}
	if probs[0].Obj != "l" {
		t.Errorf("problem obj=%q", probs[0].Obj)
	}
	// ref to defined A kept, Missing excluded
	l := g.Objects["l"]
	if len(l.Refs) != 1 {
		t.Errorf("refs=%v", l.Refs)
	}
}

func TestRedefinitionReported(t *testing.T) {
	src := "A = Point(0,0)\nA = Point(1,1)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 1 {
		t.Fatalf("expected 1 redefinition problem, got %v", probs)
	}
	if probs[0].Code != "dep/redefine" {
		t.Errorf("expected dep/redefine, got %s", probs[0].Code)
	}
	// The first definition is kept; the redefining line must not overwrite it.
	g.SetKindFromCmd()
	if got := g.Objects["A"].Args; len(got) != 2 || got[0] != "0" || got[1] != "0" {
		t.Errorf("redefinition overwrote first definition: args=%v (want [0 0])", got)
	}
}

func TestBoundVariableNotUndefined(t *testing.T) {
	// k is Sequence's iteration variable; must not be reported as undefined
	// nor become a ref targeting a nonexistent object.
	src := "n = 8\npts = Sequence(B + (k, 0), k, 1, n)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("expected no problems (k is bound), got %v", probs)
	}
	pts := g.Objects["pts"]
	for _, r := range pts.Refs {
		if r == "k" {
			t.Fatalf("k should not be a ref, got %v", pts.Refs)
		}
	}
}

func TestReservedConstantsNotUndefinedRefs(t *testing.T) {
	// pi / e are reserved constants, not object references; they must not be
	// reported as undefined and must not become dependency refs.
	src := "A = Point(0, 0)\nc1 = Circle(A, pi)\nc2 = Circle(A, 2*e)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("expected no problems for reserved constants, got %v", probs)
	}
	for _, id := range []string{"c1", "c2"} {
		for _, r := range g.Objects[id].Refs {
			if r == "pi" || r == "e" {
				t.Fatalf("reserved %q leaked into refs of %s: %v", r, id, g.Objects[id].Refs)
			}
		}
	}
}

func TestNestedCommandMaterialized(t *testing.T) {
	// Circle(Midpoint(A,B), 3) creates a synthetic object for Midpoint(A,B)
	// that participates in the graph with correct dependencies.
	src := "A = Point(0, 0)\nB = Point(4, 0)\nc = Circle(Midpoint(A, B), 3)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("expected no problems, got %v", probs)
	}
	// synthetic nested object exists with cmd=Midpoint
	if _, ok := g.Get("c.Midpoint1"); !ok {
		t.Fatalf("synthetic Midpoint object missing; objects=%v", g.Order)
	}
	inner := g.Objects["c.Midpoint1"]
	if inner.Cmd != "Midpoint" {
		t.Fatalf("synthetic cmd=%q", inner.Cmd)
	}
	if len(inner.Refs) != 2 || inner.Refs[0] != "A" || inner.Refs[1] != "B" {
		t.Fatalf("synthetic refs=%v", inner.Refs)
	}
	// outer circle depends on the synthetic object
	if g.Objects["c"].Refs[0] != "c.Midpoint1" {
		t.Fatalf("circle should depend on synthetic midpoint, refs=%v", g.Objects["c"].Refs)
	}
}

func TestNestedCommandUnknown(t *testing.T) {
	// An unknown command nested in an arg is not silently dropped: build flags it.
	src := "A = Point(0, 0)\nB = Point(4, 0)\nc = Circle(Nope(A, B), 2)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	// build itself doesn't know the command table; it synthesizes the object.
	// The existence of "c.Nope1" is what the sig stage checks. No dep problems here.
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems: %v", probs)
	}
	if _, ok := g.Get("c.Nope1"); !ok {
		t.Fatalf("synthetic Nope object missing")
	}
	if g.Objects["c.Nope1"].Cmd != "Nope" {
		t.Fatalf("synthetic cmd=%q", g.Objects["c.Nope1"].Cmd)
	}
}

func TestParseModifierStatements(t *testing.T) {
	// Statement-style commands with no '=' parse as modifiers (modifier=true).
	src := "SetColor(c, \"red\")\nStartAnimation(a)\nSetLineThickness(l, 4)\n"
	stmts, probs := Parse(src)
	if len(probs) != 0 {
		t.Fatalf("unexpected parse problems: %v", probs)
	}
	if len(stmts) != 3 {
		t.Fatalf("expected 3 modifier statements, got %d", len(stmts))
	}
	for _, s := range stmts {
		if !s.modifier {
			t.Errorf("expected modifier, got %+v", s)
		}
		if s.id != "" {
			t.Errorf("modifier should have no id, got %q", s.id)
		}
	}
	if stmts[0].cmd != "SetColor" || len(stmts[0].args) != 2 {
		t.Errorf("SetColor parse wrong: %+v", stmts[0])
	}
}

func TestParseNonModifierNoEqualsRejected(t *testing.T) {
	// A construct command written without '=' is a syntax error, not silently ok.
	src := "Line(A, B)\n"
	stmts, probs := Parse(src)
	if len(stmts) != 0 {
		t.Fatalf("expected no statements, got %v", stmts)
	}
	if len(probs) == 0 {
		t.Fatal("expected a parse problem for bare non-modifier command")
	}
}

func TestBuildModifiersDoNotCreateObjects(t *testing.T) {
	src := "a = Slider(1, 5, 0.1)\nA = (0, 0)\nB = (4, 0)\nc = Circle(A, B)\nSetColor(c, \"red\")\nSetLineThickness(c, 4)\nStartAnimation(a)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems: %v", probs)
	}
	// Modifiers must NOT appear as graph objects.
	for _, id := range g.Order {
		if id == "SetColor" || id == "" {
			t.Fatalf("modifier leaked into graph: %q", id)
		}
	}
	for _, want := range []string{"a", "A", "B", "c"} {
		if _, ok := g.Get(want); !ok {
			t.Errorf("missing object %q; order=%v", want, g.Order)
		}
	}
	// Slider 'a' depends on A/B? No — it's a standalone number; a has no refs.
	if len(g.Objects["a"].Refs) != 0 {
		t.Errorf("slider a should have no refs, got %v", g.Objects["a"].Refs)
	}
}

func TestBuildModifierUndefinedTargetReports(t *testing.T) {
	// A modifier whose target isn't defined is an undefined-ref error.
	src := "SetColor(missing, \"red\")\n"
	stmts, _ := Parse(src)
	_, probs := Build(stmts)
	if len(probs) == 0 {
		t.Fatal("expected undefined-target error for modifier")
	}
	found := false
	for _, p := range probs {
		if p.Code == diag.CodeDepUndefined {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dep/undefined problem, got %v", probs)
	}
}
