package geo

import (
	"testing"

	"github.com/hycjack/geogebra-dsl-go/internal/ir"
)

func TestLineIdenticalPoints(t *testing.T) {
	g := ir.New()
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"1", "1"}})
	g.Add(&ir.Object{ID: "l", Cmd: "Line", Kind: ir.KLine, Args: []string{"A", "A"}, Refs: []string{"A", "A"}})
	probs := Check(g, []string{"A", "l"})
	if len(probs) == 0 {
		t.Fatal("expected degeneracy problem")
	}
}

func TestLineDistinctPointsOK(t *testing.T) {
	g := ir.New()
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"1", "1"}})
	g.Add(&ir.Object{ID: "B", Kind: ir.KPoint, Args: []string{"4", "2"}})
	g.Add(&ir.Object{ID: "l", Cmd: "Line", Kind: ir.KLine, Args: []string{"A", "B"}, Refs: []string{"A", "B"}})
	probs := Check(g, []string{"A", "B", "l"})
	if len(probs) != 0 {
		t.Fatalf("expected no degeneracy, got %v", probs)
	}
}

func TestZeroRadiusCircle(t *testing.T) {
	g := ir.New()
	g.Add(&ir.Object{ID: "c", Cmd: "Circle", Kind: ir.KCircle, Args: []string{"C", "0"}})
	probs := Check(g, []string{"c"})
	if len(probs) == 0 {
		t.Fatal("expected zero-radius degeneracy")
	}
}

func TestParseRat(t *testing.T) {
	for in, wantOk := range map[string]bool{"3": true, "3.5": true, "-2.25": true, "0": true, "abc": false, "": false} {
		_, ok := parseRat(in)
		if ok != wantOk {
			t.Errorf("parseRat(%q) ok=%v, want %v", in, ok, wantOk)
		}
	}
}

func TestCircleRadiusPiOK(t *testing.T) {
	// π is a positive constant, so Circle(A, pi) must NOT be flagged.
	g := ir.New()
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"0", "0"}})
	g.Add(&ir.Object{ID: "c", Cmd: "Circle", Kind: ir.KCircle, Args: []string{"A", "pi"}})
	if probs := Check(g, []string{"A", "c"}); len(probs) != 0 {
		t.Fatalf("Circle(A, pi) should be valid, got %v", probs)
	}
}

func TestCircleRadiusNestedZeroDegenerate(t *testing.T) {
	// (2*pi) - (2*pi) == 0 → zero radius → degenerate.
	g := ir.New()
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"1", "1"}})
	g.Add(&ir.Object{ID: "c", Cmd: "Circle", Kind: ir.KCircle, Args: []string{"A", "2*pi - 2*pi"}})
	if probs := Check(g, []string{"A", "c"}); len(probs) == 0 {
		t.Fatal("Circle(A, 2*pi-2*pi) should be degenerate (zero radius)")
	}
}

func TestLineReservedPointsDegenerate(t *testing.T) {
	// A and B are both literal points at (pi, 0) → coincident → degenerate line.
	g := ir.New()
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"pi", "0"}})
	g.Add(&ir.Object{ID: "B", Kind: ir.KPoint, Args: []string{"pi", "0"}})
	g.Add(&ir.Object{ID: "l", Cmd: "Line", Kind: ir.KLine, Args: []string{"A", "B"}, Refs: []string{"A", "B"}})
	if probs := Check(g, []string{"A", "B", "l"}); len(probs) == 0 {
		t.Fatal("Line(A,B) with both at (pi,0) should be degenerate")
	}
}

func TestCircleRadiusNegativeDegenerate(t *testing.T) {
	g := ir.New()
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"0", "0"}})
	g.Add(&ir.Object{ID: "c", Cmd: "Circle", Kind: ir.KCircle, Args: []string{"A", "0 - pi"}})
	if probs := Check(g, []string{"A", "c"}); len(probs) == 0 {
		t.Fatal("Circle(A, -pi) should be degenerate (negative radius)")
	}
}

func TestSemicircleCoincidentEndpointsDegenerate(t *testing.T) {
	g := ir.New()
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"-4", "0"}})
	g.Add(&ir.Object{ID: "s", Cmd: "Semicircle", Kind: ir.KConic, Args: []string{"A", "A"}, Refs: []string{"A", "A"}})
	if probs := Check(g, []string{"A", "s"}); len(probs) == 0 {
		t.Fatal("Semicircle(A, A) should be degenerate: both diameter endpoints coincide")
	}
}

func TestSemicircleDistinctEndpointsOk(t *testing.T) {
	g := ir.New()
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"-4", "0"}})
	g.Add(&ir.Object{ID: "B", Kind: ir.KPoint, Args: []string{"4", "0"}})
	g.Add(&ir.Object{ID: "s", Cmd: "Semicircle", Kind: ir.KConic, Args: []string{"A", "B"}, Refs: []string{"A", "B"}})
	if probs := Check(g, []string{"A", "B", "s"}); len(probs) != 0 {
		t.Fatalf("Semicircle(A, B) with distinct endpoints should validate, got %v", probs)
	}
}

func TestEllipseCoincidentFociDegenerate(t *testing.T) {
	g := ir.New()
	g.Add(&ir.Object{ID: "f", Kind: ir.KPoint, Args: []string{"3", "0"}})
	g.Add(&ir.Object{ID: "e", Cmd: "Ellipse", Kind: ir.KConic, Args: []string{"f", "f", "10"}, Refs: []string{"f", "f"}})
	if probs := Check(g, []string{"f", "e"}); len(probs) == 0 {
		t.Fatal("Ellipse(f, f, 10) should be degenerate: both foci coincide")
	}
}

func TestSegmentCoincidentEndpointsDegenerate(t *testing.T) {
	g := ir.New()
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint, Args: []string{"1", "2"}})
	g.Add(&ir.Object{ID: "s", Cmd: "Segment", Kind: ir.KSegment, Args: []string{"A", "A"}, Refs: []string{"A", "A"}})
	if probs := Check(g, []string{"A", "s"}); len(probs) == 0 {
		t.Fatal("Segment(A, A) should be degenerate")
	}
}
