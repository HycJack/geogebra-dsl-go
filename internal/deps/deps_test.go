package deps

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hycjack/geogebra-dsl-go/internal/diag"
	"github.com/hycjack/geogebra-dsl-go/internal/ir"
)

func TestOrderDeterministicAndRespectsRefs(t *testing.T) {
	// Deterministic output seeded from g.Order (the old map-seeded queue made
	// the executable order nondeterministic).
	g := ir.New()
	g.Add(&ir.Object{ID: "c", Cmd: "Circle", Refs: []string{"O"}})
	g.Add(&ir.Object{ID: "O", Kind: ir.KPoint})
	g.Add(&ir.Object{ID: "l", Cmd: "Line", Refs: []string{"O"}})
	order, probs := Order(g)
	if len(probs) != 0 {
		t.Fatalf("unexpected problems: %v", probs)
	}
	if order[0] != "O" {
		t.Fatalf("order[0]=%q, want O (the only root)", order[0])
	}
	// Determinism across runs.
	order2, _ := Order(g)
	if !reflect.DeepEqual(order, order2) {
		t.Fatalf("order not deterministic: %v vs %v", order, order2)
	}
}

// TestCycleAttribution — regression: only the true cycle members (B, C) are
// reported as the cycle; downstream dependents (D, E) are reported separately
// as blocked and must NOT appear in the cycle message.
func TestCycleAttribution(t *testing.T) {
	g := ir.New()
	// A independent; B↔C true cycle; D refs C; E refs D (both downstream).
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint})
	g.Add(&ir.Object{ID: "B", Cmd: "Midpoint", Refs: []string{"C", "A"}})
	g.Add(&ir.Object{ID: "C", Cmd: "Midpoint", Refs: []string{"A", "B"}})
	g.Add(&ir.Object{ID: "D", Cmd: "Circle", Refs: []string{"C"}})
	g.Add(&ir.Object{ID: "E", Cmd: "Line", Refs: []string{"D", "A"}})

	_, probs := Order(g)
	var cycle, blocked []diag.Problem
	for _, p := range probs {
		if p.Code != diag.CodeDepCycle {
			t.Fatalf("expected only dep/cycle problems, got %+v", p)
		}
		if strings.Contains(p.Msg, "成环节点") {
			blocked = append(blocked, p)
		} else {
			cycle = append(cycle, p)
		}
	}
	if len(cycle) != 1 {
		t.Fatalf("expected exactly one true-cycle problem, got %d: %+v", len(cycle), probs)
	}
	if !strings.Contains(cycle[0].Msg, "B") || !strings.Contains(cycle[0].Msg, "C") {
		t.Fatalf("cycle message should name B and C, got %q", cycle[0].Msg)
	}
	if strings.Contains(cycle[0].Msg, "D") || strings.Contains(cycle[0].Msg, "E") {
		t.Fatalf("cycle message must not include downstream dependents, got %q", cycle[0].Msg)
	}
	if len(blocked) != 1 || !strings.Contains(blocked[0].Msg, "D") || !strings.Contains(blocked[0].Msg, "E") {
		t.Fatalf("blocked problem should name D and E, got %+v", blocked)
	}
	// The partial order must contain only what is buildable without the cycle:
	// A is independent; B depends on C (a cycle member), so B/C/D/E are all
	// excluded from the partial order.
	order, _ := Order(g)
	if !reflect.DeepEqual(order, []string{"A"}) {
		t.Fatalf("partial order=%v, want [A]", order)
	}
}

func TestSelfLoopReportedAsCycle(t *testing.T) {
	g := ir.New()
	g.Add(&ir.Object{ID: "X", Cmd: "Foo", Refs: []string{"X"}})
	_, probs := Order(g)
	if len(probs) != 1 || !strings.Contains(probs[0].Msg, "依赖自身") {
		t.Fatalf("expected self-loop cycle, got %+v", probs)
	}
}

func TestDiamondOrder(t *testing.T) {
	// A → B, A → C, B → D, C → D.
	g := ir.New()
	g.Add(&ir.Object{ID: "A", Kind: ir.KPoint})
	g.Add(&ir.Object{ID: "B", Cmd: "Line", Refs: []string{"A"}})
	g.Add(&ir.Object{ID: "C", Cmd: "Line", Refs: []string{"A"}})
	g.Add(&ir.Object{ID: "D", Cmd: "Circle", Refs: []string{"B", "C"}})
	order, probs := Order(g)
	if len(probs) != 0 {
		t.Fatalf("unexpected problems: %v", probs)
	}
	if order[0] != "A" || order[len(order)-1] != "D" {
		t.Fatalf("order=%v, want A first and D last", order)
	}
}
