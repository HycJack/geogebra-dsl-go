package check

import (
	"os"
	"strings"
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
	// SetTrace and Rename both require their second argument per the official
	// manual: SetTrace(<Object>, <true|false>), Rename(<Object>, <Name>).
	script := "A=(0,0)\nB=(2,0)\ns=Segment(A,B)\nSetCoords(A, 1, 1)\nSetTrace(s, true)\nRename(B, \"P\")\n"
	rc := Check([]byte(script), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
}

func TestModifierSignatureRejected(t *testing.T) {
	// Modifiers were previously ref-checked only, so a well-defined target with
	// the wrong type passed silently. SetLineStyle takes
	// LineOrSegmentOrPolyline, and a Point must be rejected.
	rc := Check([]byte("A=(1,0)\nSetLineStyle(A, 2)\n"), Options{})
	if rc.OK {
		t.Fatal("expected SetLineStyle(<Point>) to be rejected")
	}
	found := false
	for _, p := range rc.Errors {
		if p.Code == diag.CodeCmdArg && p.Obj == "SetLineStyle" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cmd/arg for SetLineStyle, got %v", rc.Errors)
	}
}

func TestModifierMissingArgRejected(t *testing.T) {
	// Rename(<Object>, <Name>) needs both arguments; the old ref-only check
	// accepted Rename(B).
	rc := Check([]byte("B=(2,0)\nRename(B)\n"), Options{})
	if rc.OK {
		t.Fatal("expected Rename(<Object>) to be rejected for a missing name")
	}
}

func TestModifierArgTypeRejected(t *testing.T) {
	// TurtleLeft(<Turtle>, <Angle>) — passing a Point for the angle must fail.
	rc := Check([]byte("A=(1,0)\nT=Turtle()\nTurtleLeft(A, T)\n"), Options{})
	if rc.OK {
		t.Fatal("expected TurtleLeft(<Point>, <Turtle>) to be rejected")
	}
}

func TestModifierStillNotAGraphObject(t *testing.T) {
	// Validating a modifier must not turn it into a construction object: it is
	// absent from the executable order, and the modifier's target is still the
	// only thing reported for a dangling reference.
	rc := Check([]byte("A=(1,0)\nB=(2,0)\nSetColor(A, \"red\")\n"), Options{})
	if !rc.OK {
		t.Fatalf("expected ok, got %v", rc.Errors)
	}
	for _, id := range rc.Executable {
		if id == "SetColor" || id == "stmt1" {
			t.Errorf("modifier leaked into executable order: %q", id)
		}
	}
}

func TestModifierNestedCommandReported(t *testing.T) {
	// A nested command inside a modifier argument is materialized, so an unknown
	// nested command name is reported instead of being silently ignored.
	rc := Check([]byte("A=(1,0)\nSetColor(A, RBG(1, 0, 0))\n"), Options{})
	found := false
	for _, p := range rc.Errors {
		if p.Code == diag.CodeCmdUnknown {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cmd/unknown for the nested RBG call, got %v", rc.Errors)
	}
}

func TestScriptingCommandSlotRequiresScriptingCall(t *testing.T) {
	// Repeat(<Number>, <Scripting Command>, ...) must reject a Number or a Point
	// where it wants a scripting command. Before <Scripting Command> was modeled
	// as KScript it was a wildcard, so Repeat(8, 42) passed.
	for _, script := range []string{
		"T=Turtle()\nRepeat(8, 42)\n",
		"A=(1,0)\nT=Turtle()\nRepeat(8, A)\n",
	} {
		rc := Check([]byte(script), Options{})
		if rc.OK {
			t.Errorf("expected rejection for %q", script)
		}
	}
	// A real scripting call is accepted.
	rc := Check([]byte("T=Turtle()\nRepeat(8, TurtleForward(T, 1), TurtleRight(T, 45))\n"), Options{})
	if !rc.OK {
		t.Fatalf("expected Repeat with scripting calls to pass, got %v", rc.Errors)
	}
}

func TestStartAnimationAcceptsPointsAndSliders(t *testing.T) {
	// The manual says StartAnimation(<Point or Slider>, <Point or Slider>, ...):
	// one Point or Slider per argument, not a list. The catalog had
	// List<PointOrSlider> here, which rejected every call.
	for _, script := range []string{
		"A=(0,0)\nB=(1,1)\nStartAnimation(A, B)\n",
		"n=Slider(0, 10, 0.1)\nStartAnimation(n)\n",
		"A=(0,0)\nStartAnimation()\n",
	} {
		rc := Check([]byte(script), Options{})
		if !rc.OK {
			t.Errorf("expected ok for %q, got %v", script, rc.Errors)
		}
	}
}

func TestScriptingCommandResultIsNotUsableAsObject(t *testing.T) {
	// KScript results stay out of the Any/Object wildcard slots, matching the
	// manual's rule that scripting commands cannot be nested.
	rc := Check([]byte("A=(1,0)\nT=Turtle()\nSetColor(ShowAxes(true), \"red\")\n"), Options{})
	if rc.OK {
		t.Fatal("expected nesting a scripting call as an object to be rejected")
	}
}

func TestVarArgMinimumIsRequiredParamCount(t *testing.T) {
	// For a variadic overload the minimum argument count is the number of
	// REQUIRED params. Comparing against the total param count rejected valid
	// GeoGebra usages wherever trailing params are optional.
	for _, script := range []string{
		"a={1,2}\nb={3,4}\nj=Join(a,b)\n",                         // Join(<List>,<List>,…): 2 required of 3
		"x=1\ny=2\nf=Function(x^2)\nk=3\nl={1,2}\nz=Zip(f,k,l)\n", // Zip: first pair only
		"z=If(true, 1, true, 2)\n",                                // If: no Else
		"z=ExportImage()\n",                                       // all 16 params optional
		"A=(0,0)\nB=(1,0)\nC=(1,1)\na=Area(A,B,C)\n",              // Area(<Point>,…,<Point>): 3 required of 4
	} {
		rc := Check([]byte(script), Options{})
		if !rc.OK {
			t.Errorf("expected ok for %q, got %v", script, rc.Errors)
		}
	}
	// The maximum is still enforced: a non-vararg overload cannot grow.
	rc := Check([]byte("A=(0,0)\nB=(1,1)\nC=(2,0)\nD=(3,1)\nM=Midpoint(A,B,C)\n"), Options{})
	if rc.OK {
		t.Fatal("expected Midpoint with 3 args to be rejected")
	}
}

func TestNestedCommandKindIsChecked(t *testing.T) {
	// A nested call becomes a synthetic object (A.Midpoint1) whose id contains a
	// '.'. sig.isIdent must accept '.' so the synthetic object's real kind is
	// consulted instead of resolving to KUnknown and passing the lenient
	// fallback. Midpoint wants two Points, so a nested Line must be rejected.
	rc := Check([]byte("A=(0,0)\nB=(1,1)\nC=(2,0)\nM=Midpoint(Line(A,B), C)\n"), Options{})
	if rc.OK {
		t.Fatal("expected Midpoint(Line(A,B), C) to be rejected: a Line is not a Point")
	}
	// The same shape with a genuine Point result passes.
	rc = Check([]byte("A=(0,0)\nB=(1,1)\nC=(2,0)\nM=Midpoint(Midpoint(A,B), C)\n"), Options{})
	if !rc.OK {
		t.Fatalf("expected Midpoint(Midpoint(A,B), C) to pass, got %v", rc.Errors)
	}
	// A nested Number-producing call still fills a Number slot.
	rc = Check([]byte("A=(0,0)\nB=(4,0)\ns=Segment(A,B)\nR=Round(Length(s), 1)\n"), Options{})
	if !rc.OK {
		t.Fatalf("expected Round(Length(s), 1) to pass, got %v", rc.Errors)
	}
}

// TestExamSuite runs every 中考/高考-style problem in testdata/exam/ and
// asserts it validates clean. They are realistic exam-archetype constructions
// (三角形四心、圆的切线、半圆圆周角、圆内接四边形、角平分线、旋转、平移与
// 轴对称、抛物线面积、多项式拟合、椭圆焦点、动点轨迹、正多边形、勾股定理、
// 向量点积) covering both junior-high plane geometry and the gaokao conic and
// vector material. They live as fixture files rather than inline strings so
// they can be opened in GeoGebra itself and extended.
func TestExamSuite(t *testing.T) {
	for _, entry := range listFixture("../../testdata/exam") {
		rc := Check(mustRead(t, "../../testdata/exam/", entry), Options{})
		if !rc.OK {
			t.Errorf("%s: expected ok, got %v", entry, rc.Errors)
		}
	}
}

// TestExamBadSuite runs every deliberately-broken exam script in
// testdata/exam-bad/ and asserts the checker refuses it. These are the
// regression guard for the four gaps the exam sweep exposed:
//
//	b01/b03/b02/b06/b08/b11/b14  wrong argument type or arity
//	b04/b16                      coincident endpoints (geo degeneracy)
//	b05                          Polygon/Area need at least three vertices
//	b07                          Rotate's axis slot must be a line
//	b09                          an undefined reference is an undefined reference
//	b15                          a scripting command result is not an object
func TestExamBadSuite(t *testing.T) {
	for _, entry := range listFixture("../../testdata/exam-bad") {
		rc := Check(mustRead(t, "../../testdata/exam-bad/", entry), Options{})
		if rc.OK {
			t.Errorf("%s: expected the checker to reject it", entry)
		}
	}
}

func listFixture(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			out = append(out, e.Name())
		}
	}
	return out
}

func mustRead(t *testing.T, dir, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(dir + name)
	if err != nil {
		t.Fatalf("read %s/%s: %v", dir, name, err)
	}
	return b
}

// TestUpperCaseSingleLetterIsNotAConstant pins the constant-name rule: a bare
// uppercase single letter is an object name, not a reserved constant. GeoGebra
// auto-names points A, B, C, D, E, F…, so E and I are the everyday letters for
// a fifth vertex and an incenter; treating them as constants made an undefined
// reference disappear silently, and Polygon(A, B, C, E) validated as if E were
// 2.718 instead of reporting that E is not defined.
func TestUpperCaseSingleLetterIsNotAConstant(t *testing.T) {
	for _, name := range []string{"E", "I"} {
		script := "A=(0,0)\nB=(1,0)\nC=(0,1)\nt=Polygon(A,B,C," + name + ")\n"
		rc := Check([]byte(script), Options{})
		if rc.OK {
			t.Errorf("expected %s to be reported as an undefined reference, got ok", name)
		}
	}
	// Lowercase stays a constant, and multi-letter spellings keep working in
	// any case.
	rc := Check([]byte("x=pi\ny=PI\nz=e\nw=Pi\nv=Euler\nu=Gamma\nt=Polygon((0,0),(1,0),(0,1),(1,1))\n"), Options{})
	if !rc.OK {
		t.Fatalf("constant spellings should validate, got %v", rc.Errors)
	}
}

// TestPolygonNeedsThreeVertices — a polygon (and an area of points) needs at
// least three vertices. The catalog encoded the variadic form with a single
// repeated <Point> parameter, which made the minimum one.
func TestPolygonNeedsThreeVertices(t *testing.T) {
	for _, script := range []string{
		"A=(0,0)\np=Polygon(A)\n",
		"A=(0,0)\nB=(1,0)\np=Polygon(A,B)\n",
		"A=(0,0)\nB=(1,0)\na=Area(A,B)\n",
	} {
		if rc := Check([]byte(script), Options{}); rc.OK {
			t.Errorf("expected %q to be rejected, got ok", script)
		}
	}
	// Three and more vertices are fine, and the vertex-count overload still works.
	for _, script := range []string{
		"A=(0,0)\nB=(1,0)\nC=(0,1)\np=Polygon(A,B,C)\n",
		"A=(0,0)\nB=(1,0)\nC=(0,1)\nD=(1,1)\np=Polygon(A,B,C,D)\n",
		"A=(0,0)\nB=(1,0)\nC=(0,1)\na=Area(A,B,C)\n",
		"A=(0,0)\nB=(1,1)\np=Polygon(A,B,5)\n",
	} {
		if rc := Check([]byte(script), Options{}); !rc.OK {
			t.Errorf("expected %q to pass, got %v", script, rc.Errors)
		}
	}
}

// TestRotateAxisMustBeALine — Rotate(<Object>, <Angle>, <Axis of Rotation>) is
// the 3D rotation-about-a-line form. The axis slot was wildcarded, so a bare
// number validated; the <Point> overload correctly rejected the same script.
func TestRotateAxisMustBeALine(t *testing.T) {
	for _, script := range []string{
		"A=(0,0)\nB=(4,0)\nC=(4,4)\nD=(0,4)\nsq=Polygon(A,B,C,D)\nang=Slider(0,90,1)\nq=Rotate(sq,ang,5)\n",
		"A=(0,0)\nB=(4,0)\nC=(4,4)\nD=(0,4)\nsq=Polygon(A,B,C,D)\nang=Slider(0,90,1)\nP=(0,0)\nq=Rotate(sq,ang,P,P)\n",
		"A=(0,0)\nB=(4,0)\nC=(4,4)\nD=(0,4)\nsq=Polygon(A,B,C,D)\nang=Slider(0,90,1)\nP=(0,0)\nq=Rotate(sq,ang,P,5)\n",
	} {
		if rc := Check([]byte(script), Options{}); rc.OK {
			t.Errorf("expected a bad rotation axis to be rejected, got ok: %s", script)
		}
	}
	// Rotation about a point (2D) and about a line (3D) are both legitimate.
	for _, script := range []string{
		"A=(0,0)\nB=(4,0)\nC=(4,4)\nD=(0,4)\nsq=Polygon(A,B,C,D)\nang=Slider(0,90,1)\nq=Rotate(sq,ang,A)\n",
		"A=(0,0)\nB=(4,0)\nC=(4,4)\nD=(0,4)\nsq=Polygon(A,B,C,D)\nang=Slider(0,90,1)\nax=Line((0,0),(0,0,5))\nq=Rotate(sq,ang,ax)\n",
		"A=(0,0)\nB=(4,0)\nC=(4,4)\nD=(0,4)\nsq=Polygon(A,B,C,D)\nang=Slider(0,90,1)\nP=(0,0)\nax=Line((0,0),(0,0,5))\nq=Rotate(sq,ang,P,ax)\n",
	} {
		if rc := Check([]byte(script), Options{}); !rc.OK {
			t.Errorf("expected a legitimate rotation to pass, got %v", rc.Errors)
		}
	}
}

// TestArithmeticNumberAsRadiusOk — regression: `r = 2/3` is an arithmetic
// expression, hence a Number object (not KFunction), so Circle(O, r) must
// validate. Before the fix the expression RHS was always typed KFunction and
// the circle was falsely rejected with cmd/arg.
func TestArithmeticNumberAsRadiusOk(t *testing.T) {
	rc := Check([]byte("r = 2/3\nO = Point(0, 0)\nc1 = Circle(O, r)\n"), Options{})
	if !rc.OK {
		t.Fatalf("expected arithmetic number to satisfy a <Number> slot, got %v", rc.Errors)
	}
}

// TestScientificNotationNumberOk — regression: GeoGebra accepts scientific
// notation (1e3); the number-literal detector must too, so `r = 1e3` becomes a
// Number object instead of an expression.
func TestScientificNotationNumberOk(t *testing.T) {
	rc := Check([]byte("r = 1e3\nO = Point(0, 0)\nc1 = Circle(O, r)\n"), Options{})
	if !rc.OK {
		t.Fatalf("expected scientific-notation literal to satisfy a <Number> slot, got %v", rc.Errors)
	}
}

// TestNumberExprOverCommandNumberOk — regression: `r = d + 1` where
// `d = Distance(A,B)` is a Number expression, so Circle(O, r) must validate.
// The first review pass only classified expressions over literals; this is the
// command-produced-number case it missed.
func TestNumberExprOverCommandNumberOk(t *testing.T) {
	rc := Check([]byte("A = (0, 0)\nB = (4, 0)\nd = Distance(A, B)\nr = d + 1\nO = (0, 0)\nc1 = Circle(O, r)\n"), Options{})
	if !rc.OK {
		t.Fatalf("expected r = d+1 to satisfy a <Number> slot, got %v", rc.Errors)
	}
}

// TestStringArgWithCommaOk — regression: splitArgs must not split inside a
// double-quoted string, so Text("hello, world", A) keeps two arguments. Before
// the fix the comma in the string produced three args and a false cmd/arg.
func TestStringArgWithCommaOk(t *testing.T) {
	rc := Check([]byte("A = Point(0, 0)\nt1 = Text(\"hello, world\", A)\n"), Options{})
	if !rc.OK {
		t.Fatalf("expected a string literal containing a comma to stay one arg, got %v", rc.Errors)
	}
}

// TestCycleAttributionPrecise — regression: the dep/cycle message must name the
// true cycle (B ↔ C) and report downstream dependents (D, E) separately as
// blocked, not lump them into the "环" list. The old implementation reported
// "环：B, C, D, E" and sent the AI repair loop after innocent objects.
func TestCycleAttributionPrecise(t *testing.T) {
	rc := Check([]byte("A = Point(0, 0)\nB = Midpoint(C, A)\nC = Midpoint(A, B)\nD = Circle(C, 2)\nE = Line(D, A)\n"), Options{})
	if rc.OK {
		t.Fatal("expected failure")
	}
	var cycleMsg, blockedMsg string
	for _, p := range rc.Errors {
		if p.Code == diag.CodeDepCycle {
			if strings.Contains(p.Msg, "成环节点") {
				blockedMsg = p.Msg
			} else {
				cycleMsg = p.Msg
			}
		}
	}
	if !strings.Contains(cycleMsg, "B") || !strings.Contains(cycleMsg, "C") {
		t.Fatalf("cycle message should name B and C, got %q", cycleMsg)
	}
	if strings.Contains(cycleMsg, "D") || strings.Contains(cycleMsg, "E") {
		t.Fatalf("cycle message must not include downstream dependents, got %q", cycleMsg)
	}
	if !strings.Contains(blockedMsg, "D") || !strings.Contains(blockedMsg, "E") {
		t.Fatalf("blocked message should name D and E, got %q", blockedMsg)
	}
}

// TestCmdArgDiagnosticCarriesSignatures — regression: a rejected command must
// tell the caller (and the LLM repair loop) the accepted overload syntaxes, so
// a bare rejection becomes a self-correctable instruction.
func TestCmdArgDiagnosticCarriesSignatures(t *testing.T) {
	rc := Check([]byte("O = Point(0, 0)\nc1 = Circle(O)\n"), Options{})
	if rc.OK {
		t.Fatal("expected failure")
	}
	var msg string
	for _, p := range rc.Errors {
		if p.Code == diag.CodeCmdArg {
			msg = p.Msg
		}
	}
	if msg == "" {
		t.Fatalf("expected a cmd/arg diagnostic, got %v", rc.Errors)
	}
	if !strings.Contains(msg, "Circle(<Point>") {
		t.Fatalf("cmd/arg diagnostic should carry the accepted signatures, got %q", msg)
	}
}
