package geo

import (
	"testing"

	"github.com/you/geogebra-dsl-go/internal/ir"
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
