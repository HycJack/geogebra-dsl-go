package text

import (
	"testing"

	"github.com/you/geogebra-dsl-go/internal/ir"
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
	_, probs := Build(stmts)
	if len(probs) != 1 {
		t.Fatalf("expected redefinition, got %v", probs)
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
