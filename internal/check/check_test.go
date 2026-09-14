package check

import (
	"testing"

	"github.com/hycjack/geogebra-dsl-go/internal/diag"
)

func hasCode(rc *diag.Receipt, code diag.Code) bool {
	for _, p := range rc.Errors {
		if p.Code == code {
			return true
		}
	}
	return false
}

func TestTextOk(t *testing.T) {
	script := `A = Point(0, 2)
B = Point(4, 2)
C = Point(0, -2)
T = Point(0, 4)
l = Line(A, B)
c = Circle(C, T)
P1 = Intersect(c, l)`
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got errors: %v", rc.Errors)
	}
	if len(rc.Executable) != 7 {
		t.Fatalf("expected 7 executable objects, got %d: %v", len(rc.Executable), rc.Executable)
	}
	if rc.SourceIn != "text" {
		t.Fatalf("expected source text, got %s", rc.SourceIn)
	}
}

// TestDynamicStyledScriptOk verifies that dynamic controls (Slider) and
// statement-style style commands (SetColor / SetLineThickness / StartAnimation)
// pass the validator end to end and the modifiers are not part of the geometry.
func TestDynamicStyledScriptOk(t *testing.T) {
	script := `a = Slider(1, 5, 0.1)
A = (0, 0)
B = (4, 0)
c = Circle(A, B)
SetColor(c, "red")
SetLineThickness(c, 4)
StartAnimation(a)`
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected dynamic+styled script to be ok, got errors: %v", rc.Errors)
	}
	// Slider + 2 points + circle = 4 geometry objects; modifiers add none.
	if len(rc.Executable) != 4 {
		t.Fatalf("expected 4 executable objects, got %d: %v", len(rc.Executable), rc.Executable)
	}
}

// TestDynamicStyledScriptUndefinedTarget verifies a modifier referencing a
// missing object is caught.
func TestDynamicStyledScriptUndefinedTarget(t *testing.T) {
	rc := Check([]byte("SetColor(ghost, \"red\")\n"), Options{})
	if rc.OK {
		t.Fatal("expected failure for undefined modifier target")
	}
	if !hasCode(rc, diag.CodeDepUndefined) {
		t.Errorf("expected dep/undefined, got %v", rc.Errors)
	}
}

// TestCheckboxButtonScriptOk verifies Checkbox and Button controls validate
// (they are constructed with `=` so they are normal objects).
func TestCheckboxButtonScriptOk(t *testing.T) {
	script := `chk = Checkbox()
btn = Button("Show")
A = (0, 0)`
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected checkbox+button ok, got errors: %v", rc.Errors)
	}
}

func TestUndefinedRef(t *testing.T) {
	rc := Check([]byte("A = Point(0, 2)\nl = Line(A, X)\n"), Options{})
	if rc.OK {
		t.Fatal("expected failure")
	}
	if !hasCode(rc, diag.CodeDepUndefined) {
		t.Fatalf("expected dep/undefined, got %v", rc.Errors)
	}
}

func TestUnknownCommand(t *testing.T) {
	rc := Check([]byte("A = Point(0, 2)\nl = Foobar(A, A)\n"), Options{})
	if rc.OK {
		t.Fatal("expected failure")
	}
	if !hasCode(rc, diag.CodeCmdUnknown) {
		t.Fatalf("expected cmd/unknown, got %v", rc.Errors)
	}
}

func TestDegenerateLine(t *testing.T) {
	rc := Check([]byte("A = Point(1, 1)\nl = Line(A, A)\n"), Options{})
	if rc.OK {
		t.Fatal("expected failure")
	}
	if !hasCode(rc, diag.CodeGeoDegenerate) {
		t.Fatalf("expected geo/degenerate, got %v", rc.Errors)
	}
}

func TestCycle(t *testing.T) {
	script := `A = Point(0, 0)
B = Line(A, C)
C = Line(A, B)`
	rc := Check([]byte(script), Options{})
	if rc.OK {
		t.Fatal("expected failure")
	}
	if !hasCode(rc, diag.CodeDepCycle) {
		t.Fatalf("expected dep/cycle, got %v", rc.Errors)
	}
}

func TestIRJSONOk(t *testing.T) {
	ir := `{
  "objects": [
    {"id":"A","cmd":"Point","args":["0","2"],"kind":"Point"},
    {"id":"B","cmd":"Point","args":["4","2"],"kind":"Point"},
    {"id":"l","cmd":"Line","args":["A","B"],"refs":["A","B"],"kind":"Line"}
  ],
  "goals": ["l"]
}`
	rc := Check([]byte(ir), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
	if rc.SourceIn != "ir" {
		t.Fatalf("expected source ir, got %s", rc.SourceIn)
	}
	if len(rc.Executable) != 3 {
		t.Fatalf("expected 3 executable, got %v", rc.Executable)
	}
}

func TestIRGoalMissing(t *testing.T) {
	ir := `{
  "objects": [
    {"id":"A","cmd":"Point","args":["0","2"],"kind":"Point"}
  ],
  "goals": ["Zed"]
}`
	rc := Check([]byte(ir), Options{})
	if rc.OK {
		t.Fatal("expected failure")
	}
	if !hasCode(rc, diag.CodeGoalUnreachable) {
		t.Fatalf("expected goal/unreachable, got %v", rc.Errors)
	}
}

func TestIRUndefinedRef(t *testing.T) {
	// IR supplies refs verbatim; a ref to a non-existent object must be
	// reported as dep/undefined (the text path catches this during build).
	ir := `{
  "objects": [
    {"id":"A","cmd":"Point","args":["0","2"],"kind":"Point"},
    {"id":"l","cmd":"Line","args":["A","Missing"],"refs":["A","Missing"],"kind":"Line"}
  ],
  "goals": ["l"]
}`
	rc := Check([]byte(ir), Options{})
	if rc.OK {
		t.Fatal("expected failure")
	}
	if !hasCode(rc, diag.CodeDepUndefined) {
		t.Fatalf("expected dep/undefined, got %v", rc.Errors)
	}
}

func TestIRDuplicateIDRedefine(t *testing.T) {
	// Duplicate object ids in IR should be reported as dep/redefine, not
	// silently overwritten.
	ir := `{
  "objects": [
    {"id":"A","cmd":"Point","args":["0","2"],"kind":"Point"},
    {"id":"A","cmd":"Point","args":["4","2"],"kind":"Point"}
  ],
  "goals": ["A"]
}`
	rc := Check([]byte(ir), Options{})
	if rc.OK {
		t.Fatal("expected failure")
	}
	if !hasCode(rc, diag.CodeDepRedefine) {
		t.Fatalf("expected dep/redefine, got %v", rc.Errors)
	}
}

func TestSniff(t *testing.T) {
	if s := sniff([]byte("{  \"objects\": []}"), ""); s != "ir" {
		t.Fatalf("expected ir sniff, got %s", s)
	}
	if s := sniff([]byte("A = Point(0,2)"), ""); s != "text" {
		t.Fatalf("expected text sniff, got %s", s)
	}
	if s := sniff([]byte("{"), "text"); s != "text" {
		t.Fatalf("force text should win, got %s", s)
	}
}
