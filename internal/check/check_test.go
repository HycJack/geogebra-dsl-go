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

// TestVarArgCommandsOk verifies a vararg command (e.g. ANOVA, Polyline) with
// more args than its fixed params is accepted. This guards the regression
// where kindsMatch rejected any overload whose args outnumbered its params,
// falsely reporting valid vararg calls as cmd/arg.
func TestVarArgCommandsOk(t *testing.T) {
	for _, script := range []string{
		"l1 = {1, 2, 3}\nl2 = {3, 4, 5}\nl3 = {5, 6, 7}\na = ANOVA(l1, l2, l3)\n",
		"A = (0, 0)\nB = (1, 1)\nC = (2, 2)\npl = Polyline(A, B, C)\n",
	} {
		rc := Check([]byte(script), Options{})
		if !rc.OK {
			t.Fatalf("expected vararg script to validate, got errors: %v (script=%q)", rc.Errors, script)
		}
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

// TestNestedCommandsInPointsOk verifies that nested command calls inside point
// coordinates (B = (Sqrt(3),0,0), D = (Sqrt(3)/2, 3/2, 0)) and as number
// assignments (h = Sqrt(5)) are recognized and validated end to end.
func TestNestedCommandsInPointsOk(t *testing.T) {
	script := `A = (0, 0, 0)
B = (Sqrt(3), 0, 0)
C = (Sqrt(3), 1, 0)
D = (Sqrt(3)/2, 3/2, 0)
h = Sqrt(5)
L = {Sqrt(2), Sqrt(8), 3}`
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected nested commands to validate ok, got errors: %v", rc.Errors)
	}
}

// TestNestedCommandTypoCaught verifies a typo inside a point coordinate is
// reported as cmd/unknown rather than silently ignored.
func TestNestedCommandTypoCaught(t *testing.T) {
	rc := Check([]byte("B = (Sqrrt(3), 0, 0)\n"), Options{})
	if rc.OK {
		t.Fatal("expected failure for unknown nested command")
	}
	if !hasCode(rc, diag.CodeCmdUnknown) {
		t.Fatalf("expected cmd/unknown, got %v", rc.Errors)
	}
}

// TestSqrtNumberAssignmentOk verifies a scalar math function used as a command
// result (h = Sqrt(5)) is a valid known command.
func TestSqrtNumberAssignmentOk(t *testing.T) {
	rc := Check([]byte("h = Sqrt(5)\nr = Cos(0)\n"), Options{})
	if !rc.OK {
		t.Fatalf("expected Sqrt/Cos assignments ok, got errors: %v", rc.Errors)
	}
}

// TestNestedCommandInCurveBoundVar verifies a trig call inside Curve uses the
// parameter variable t (Curve's bound variable), so t is a local symbol, not an
// undefined reference.
func TestNestedCommandInCurveBoundVar(t *testing.T) {
	script := `c = Curve(cos(t), sin(t), t, 0, 2pi)`
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected Curve(cos(t), sin(t), ...) ok, got errors: %v", rc.Errors)
	}
}

// TestNestedCommandInSequenceBoundVar verifies a nested Sqrt call under a
// Sequence doesn't misreport the iteration variable k as undefined.
func TestNestedCommandInSequenceBoundVar(t *testing.T) {
	script := `S = Sequence((k*Sqrt(2), k), k, 1, 5)`
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected Sequence with nested Sqrt ok, got errors: %v", rc.Errors)
	}
}

// TestLowercaseMathCommandsOk verifies the built-in math functions written
// lowercase (sqrt, sin, cos, abs, ln, exp, tan) — GeoGebra's usual spelling —
// are recognized and validate, in number assignments and inside point coords.
func TestLowercaseMathCommandsOk(t *testing.T) {
	script := `h = sqrt(5)
r = cos(0)
s = sin(0)
q = abs(-3)
P = (sqrt(2), cos(0), 0)`
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected lowercase math commands ok, got errors: %v", rc.Errors)
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

// TestListLiteralAndElement verifies a { ... } list literal builds a List object
// that can feed Element / FitPoly.
func TestListLiteralAndElement(t *testing.T) {
	script := "L = {1, 2, 3}\ne = Element(L, 1)\n"
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
}

func TestPointListFitPoly(t *testing.T) {
	script := "pts = {(0,0), (1,1), (2,4)}\nl = FitPoly(pts, 2)\n"
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
}

func TestExpressionRHSOk(t *testing.T) {
	// Expression RHS / implicit curve no longer hard-fails parsing.
	script := "k = 3\ny = x^2 + k\n"
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
}

func TestFunctionDefBuiltinBody(t *testing.T) {
	// f(x) = sin(x) — the command-call-looking body must parse as a function
	// body expression, not an assignment, and pass validation.
	script := "f(x) = sin(x)\ng(t) = cos(t) + 1\n"
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
}

func TestRegularPolygonOk(t *testing.T) {
	script := "A=(0,0)\nB=(2,0)\np = RegularPolygon(A, B, 5)\n"
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
}

func TestPolygonVerticesNumberOk(t *testing.T) {
	script := "A=(0,0)\nB=(2,0)\np = Polygon(A, B, 5)\n"
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
}

func TestCircumcircleOk(t *testing.T) {
	script := "A=(0,0)\nB=(2,0)\nC=(1,2)\nc = Circumcircle(A, B, C)\n"
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
}

func TestTriangleCentersOk(t *testing.T) {
	script := "A=(0,0)\nB=(2,0)\nC=(1,2)\nI = Incenter(A, B, C)\no = Orthocenter(A,B,C)\n"
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
}

func TestNewModifierStatementsOk(t *testing.T) {
	// SetCoords / SetTrace / Rename as no-'=' statements are now accepted.
	script := "A=(0,0)\nB=(2,0)\ns=Segment(A,B)\nSetCoords(A, 1, 1)\nSetTrace(s)\nRename(B)\n"
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
}
