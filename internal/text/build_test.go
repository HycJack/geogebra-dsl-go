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

// TestHashInsideStringIsNotComment verifies a '#' inside a double-quoted string
// argument (e.g. SetCaption(c, "Answer #1")) is treated as part of the string,
// not as a comment terminator.
func TestHashInsideStringIsNotComment(t *testing.T) {
	src := "c = Circle((0,0), (4,0))\nSetCaption(c, \"Answer #1\")\n"
	stmts, parseProbs := Parse(src)
	if len(parseProbs) != 0 {
		t.Fatalf("unexpected parse errors: %v", parseProbs)
	}
	_, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("expected clean build, got %v", probs)
	}
	// The SetCaption line must still be a full modifier command with the # intact.
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements (circle + SetCaption), got %d", len(stmts))
	}
	mod := stmts[1]
	if !mod.modifier || len(mod.args) != 2 || mod.args[1] != "\"Answer #1\"" {
		t.Fatalf("SetCaption arg mangled by comment stripping: modifier=%v args=%v", mod.modifier, mod.args)
	}
}

// TestReservedNameShadowingKeepsRef verifies that if a script defines an object
// with a name that is also a reserved constant (pi/e/...), a later reference to
// that object is treated as a real dependency rather than silently dropped.
func TestReservedNameShadowingKeepsRef(t *testing.T) {
	src := "pi = Point(0, 0)\nc = Circle(pi, 3)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("expected clean build, got %v", probs)
	}
	refs := g.Objects["c"].Refs
	found := false
	for _, r := range refs {
		if r == "pi" {
			found = true
		}
	}
	if !found {
		t.Fatalf("object named 'pi' referenced by c must be a dependency ref; refs=%v", refs)
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

func TestParseListLiteral(t *testing.T) {
	src := "L = {1, 2, 3}\npts = {(0,0), (1,1), (2,4)}\n"
	stmts, probs := Parse(src)
	if len(probs) != 0 {
		t.Fatalf("unexpected parse problems: %v", probs)
	}
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems: %v", probs)
	}
	l := g.Objects["L"]
	if l.Kind != ir.KList {
		t.Errorf("L kind=%v, want List", l.Kind)
	}
	if len(l.Args) != 3 || l.Args[0] != "1" || l.Args[2] != "3" {
		t.Errorf("L args=%v", l.Args)
	}
	pts := g.Objects["pts"]
	if pts.Kind != ir.KList {
		t.Errorf("pts kind=%v, want List", pts.Kind)
	}
}

func TestParseExpressionRHS(t *testing.T) {
	// An expression RHS (implicit curve / algebraic) is accepted, not a syntax
	// error, and references to defined objects become deps.
	src := "k = 3\ny = x^2 + k\n"
	stmts, probs := Parse(src)
	if len(probs) != 0 {
		t.Fatalf("unexpected parse problems: %v", probs)
	}
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems (free var x must be tolerated): %v", probs)
	}
	y := g.Objects["y"]
	if y.Kind != ir.KFunction {
		t.Errorf("y kind=%v, want Function", y.Kind)
	}
	if !containsRef(y.Refs, "k") {
		t.Errorf("y should depend on k, refs=%v", y.Refs)
	}
}

func TestParseFunctionDef(t *testing.T) {
	// f(x) = ... registers object "f" (KFunction); the param x is local, the
	// body may reference defined objects.
	src := "f(x) = 2x + 1\nh = 2*f\n"
	stmts, probs := Parse(src)
	if len(probs) != 0 {
		t.Fatalf("unexpected parse problems: %v", probs)
	}
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems: %v", probs)
	}
	f := g.Objects["f"]
	if f.Kind != ir.KFunction {
		t.Errorf("f kind=%v, want Function", f.Kind)
	}
	h := g.Objects["h"]
	if !containsRef(h.Refs, "f") {
		t.Errorf("h should depend on f, refs=%v", h.Refs)
	}
}

func TestParseFunctionDefCommandLikeBody(t *testing.T) {
	// f(x) = sin(x) has a command-call-looking body; it must be parsed as a
	// function body expression, not as `f = sin(x)` assignment (which would
	// flag x undefined / sin unknown).
	src := "f(x) = sin(x)\ng(t) = cos(t) + 1\n"
	stmts, probs := Parse(src)
	if len(probs) != 0 {
		t.Fatalf("unexpected parse problems: %v", probs)
	}
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems (body must be expression): %v", probs)
	}
	if g.Objects["f"].Kind != ir.KFunction {
		t.Errorf("f kind=%v, want Function", g.Objects["f"].Kind)
	}
	// The body is stored whole; neither sin nor x become refs.
	if len(g.Objects["f"].Refs) != 0 {
		t.Errorf("f should have no refs (sin/x are local), got %v", g.Objects["f"].Refs)
	}
}

func TestListElementsResolveRefs(t *testing.T) {
	// A list literal containing a defined object records that dependency and
	// an undefined standalone identifier is reported.
	src := "A = (0, 0)\nB = (2, 0)\nl = {A, B, Seven}\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	foundUndef := false
	for _, p := range probs {
		if p.Code == diag.CodeDepUndefined {
			foundUndef = true
		}
	}
	if !foundUndef {
		t.Errorf("expected undefined-ref for Seven, got %v", probs)
	}
	if len(g.Objects["l"].Refs) != 2 || !containsRef(g.Objects["l"].Refs, "A") || !containsRef(g.Objects["l"].Refs, "B") {
		t.Errorf("list refs=%v", g.Objects["l"].Refs)
	}
}

func TestNestedCommandInPointCoords(t *testing.T) {
	// A numeric function call inside a point's coordinate (B = (Sqrt(3),0,0))
	// is materialized as a synthetic object so it is validated, not silently
	// dropped.
	src := "B = (Sqrt(3), 0, 0)\n"
	stmts, probs := Parse(src)
	if len(probs) != 0 {
		t.Fatalf("unexpected parse problems: %v", probs)
	}
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems: %v", probs)
	}
	inner, ok := g.Get("B.Sqrt1")
	if !ok {
		t.Fatalf("synthetic Sqrt object missing; order=%v", g.Order)
	}
	if inner.Cmd != "Sqrt" {
		t.Fatalf("synthetic cmd=%q, want Sqrt", inner.Cmd)
	}
	if len(inner.Args) != 1 || inner.Args[0] != "3" {
		t.Fatalf("synthetic args=%v, want [3]", inner.Args)
	}
	// the point retains its coordinate text (with the call rewritten to the id)
	b := g.Objects["B"]
	if b.Kind != ir.KPoint {
		t.Fatalf("B kind=%v, want Point", b.Kind)
	}
	if len(b.Args) != 3 || b.Args[0] != "B.Sqrt1" {
		t.Fatalf("B args=%v, want leading B.Sqrt1", b.Args)
	}
}

func TestNestedCommandEmbeddedInPointCoord(t *testing.T) {
	// A call embedded inside arithmetic (D = (Sqrt(3)/2, 3/2, 0)) must be found
	// and materialized, not just whole-argument calls.
	src := "D = (Sqrt(3)/2, 3/2, 0)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems: %v", probs)
	}
	inner, ok := g.Get("D.Sqrt1")
	if !ok {
		t.Fatalf("synthetic Sqrt object missing; order=%v", g.Order)
	}
	if inner.Cmd != "Sqrt" || len(inner.Args) != 1 || inner.Args[0] != "3" {
		t.Fatalf("synthetic=%+v, want Sqrt(3)", inner)
	}
	d := g.Objects["D"]
	if len(d.Args) != 3 || d.Args[0] != "D.Sqrt1/2" {
		t.Fatalf("D args=%v, want leading D.Sqrt1/2", d.Args)
	}
}

func TestNestedCommandInListLiteral(t *testing.T) {
	// A list literal element that is a command call is materialized and the list
	// depends on the synthetic object.
	src := "L = {Sqrt(2), 3}\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems: %v", probs)
	}
	inner, ok := g.Get("L.Sqrt1")
	if !ok {
		t.Fatalf("synthetic Sqrt object missing; order=%v", g.Order)
	}
	if inner.Cmd != "Sqrt" || len(inner.Args) != 1 || inner.Args[0] != "2" {
		t.Fatalf("synthetic=%+v, want Sqrt(2)", inner)
	}
	l := g.Objects["L"]
	if len(l.Args) != 2 || l.Args[0] != "L.Sqrt1" {
		t.Fatalf("L args=%v, want leading L.Sqrt1", l.Args)
	}
	if !containsRef(l.Refs, "L.Sqrt1") {
		t.Fatalf("list should depend on synthetic Sqrt, refs=%v", l.Refs)
	}
}

func TestNestedCommandUnknownInPoint(t *testing.T) {
	// A typo in a nested command inside a point coordinate is materialized and
	// reported (cmd/unknown via the sig stage) instead of being silently ignored.
	src := "B = (Sqrrt(3), 0, 0)\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("build should still succeed (sig stage flags cmd/unknown): %v", probs)
	}
	if _, ok := g.Get("B.Sqrrt1"); !ok {
		t.Fatalf("synthetic Sqrrt object missing")
	}
	if g.Objects["B.Sqrrt1"].Cmd != "Sqrrt" {
		t.Fatalf("synthetic cmd=%q", g.Objects["B.Sqrrt1"].Cmd)
	}
}

func TestParseBoolLiteral(t *testing.T) {
	// GeoGebra writes booleans bare (Slider's <Is Angle> defaults to false), so
	// `x = false` must be a literal, not an expression or a reference to an
	// undefined object named "false".
	for _, src := range []string{"x = false\n", "x = true\n", "x = FALSE\n", "x = True\n"} {
		stmts, probs := Parse(src)
		if len(probs) != 0 {
			t.Fatalf("%q: unexpected parse problems: %v", src, probs)
		}
		if len(stmts) != 1 || stmts[0].boolLiteral == "" {
			t.Fatalf("%q: expected a bool literal statement, got %+v", src, stmts)
		}
	}
}

func TestBuildBoolLiteralKind(t *testing.T) {
	// KBool was previously declared in ir.Kind but unreachable: no statement
	// could produce it, so no <Boolean> parameter slot was satisfiable.
	src := "flag = false\non = true\n"
	stmts, _ := Parse(src)
	g, probs := Build(stmts)
	if len(probs) != 0 {
		t.Fatalf("unexpected build problems: %v", probs)
	}
	for _, id := range []string{"flag", "on"} {
		obj, ok := g.Get(id)
		if !ok {
			t.Fatalf("missing object %q", id)
		}
		if obj.Kind != ir.KBool {
			t.Errorf("%s: expected KBool, got %s", id, obj.Kind)
		}
	}
}

func TestBoolLiteralArgumentNotUndefinedRef(t *testing.T) {
	// ShowAxes(false) used to report "undefined object: false" because false is
	// a valid identifier and got ref-resolved like any other name.
	src := "ShowAxes(false)\nShowGrid(true)\n"
	stmts, probs := Parse(src)
	if len(probs) != 0 {
		t.Fatalf("parse problems: %v", probs)
	}
	_, bprobs := Build(stmts)
	if len(bprobs) != 0 {
		t.Fatalf("build problems: %v", bprobs)
	}
}

func TestParseScriptingCategoryAcceptsBare(t *testing.T) {
	// The modifier allowlist is the official GeoGebra Scripting Commands category
	// (67 commands, Scripting_Commands page). Every one of these must parse bare.
	// All are written with an explicit argument list (possibly empty), because
	// parseCommandCall requires `Cmd(...)`: GeoGebra also accepts a paren-less
	// `ZoomIn`, but supporting that bare-name form is a separate grammar change.
	for _, line := range []string{
		"ShowAxes(false)", "ShowGrid(false)", "ShowLayer(1)", "HideLayer(1)",
		"CenterView()", "Pan(1, 2)", "ZoomIn()", "ZoomOut()",
		"Delete(A)", "Repeat(3)", "Execute(A)", "ReadText(f, \"t\")",
		"PlaySound(f, 1)", "ExportImage(\"a.png\")", "StartRecord()",
		"SelectObjects(A, B)", "Rename(A, \"P\")", "RunUpdateScript(A)",
		"CopyFreeObject(A)", "AttachCopyToView(A, 1)", "Slider(0, 10, 0.1)",
		"Button(\"Go\", f)", "Checkbox(\"flip\", x)", "InputBox(\"n=\", x)",
		"GetTime()", "ParseToFunction(\"x^2\")", "ParseToNumber(\"3.5\")",
		"SetLabelMode(A, 2)", "SetLineOpacity(c, 0.5)",
		"SetLevelOfDetail(A, 2)", "SetSpinSpeed(A, 1)",
		"SetViewDirection(0, 0, 1)", "SetImage(A, \"x.png\")",
		"SetConstructionStep(A, 3)", "Turtle(A)", "TurtleForward(A, 5)",
		"TurtleLeft(A, 90)", "TurtleRight(A, 90)",
		"TurtleUp(A)", "TurtleDown(A)", "TurtleBack(A, 5)",
	} {
		stmts, probs := Parse(line + "\n")
		if len(probs) != 0 {
			t.Errorf("%q: unexpected parse problems: %v", line, probs)
			continue
		}
		if len(stmts) != 1 || !stmts[0].modifier {
			t.Errorf("%q: expected one modifier statement, got %+v", line, stmts)
		}
	}
}

func TestParseNonScriptingCommandStillRejected(t *testing.T) {
	// A command that returns an object must not be accepted bare: that is how a
	// typo'd assignment looks, and rejecting it is what makes the error useful.
	for _, line := range []string{"Segment(A, B)", "Line(A, B)", "Circle(A, B)", "Polygon(A, B, C)", "Text(A, \"hi\")"} {
		stmts, probs := Parse(line + "\n")
		if len(probs) == 0 {
			t.Errorf("%q: expected a parse problem for a bare construct command", line)
		}
		if len(stmts) != 0 {
			t.Errorf("%q: expected no statements", line)
		}
	}
}

func TestDeadModifierAliasesRemoved(t *testing.T) {
	// SETVISIBLE and SETLABELVISIBLE were in the old allowlist but are absent
	// from the catalog entirely (GeoGebra has ShowLabel, not SetLabelVisible),
	// so they must no longer parse as modifiers.
	for _, line := range []string{"SetVisible(A, false)", "SetLabelVisible(A, false)"} {
		_, probs := Parse(line + "\n")
		if len(probs) == 0 {
			t.Errorf("%q: expected a parse problem for a non-existent command", line)
		}
	}
}

func containsRef(refs []string, want string) bool {
	for _, r := range refs {
		if r == want {
			return true
		}
	}
	return false
}

func TestStripCommentSlashes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"// 整行注释", ""},
		{"   // 前面有空白", "   "}, // stripComment 只截断不 trim，Parse 再做 TrimSpace
		{"A=(0,0) // 行尾注释", "A=(0,0) "},
		{"A=(0,0)\t// tab 分隔", "A=(0,0)\t"},
		{"# 老注释仍然支持", ""},
		{"A=(0,0) # 行尾老注释", "A=(0,0) "},
		// A "//" inside a string is not a comment.
		{"t=Text(\"http://example.com\")", "t=Text(\"http://example.com\")"},
		{"t=Text(\"http://x\") // 真注释", "t=Text(\"http://x\") "},
		// Division must survive: "/" is not followed by "/" in any of these.
		{"y=x/2", "y=x/2"},
		{"y=x/2/3", "y=x/2/3"},
		{"y=Area(A,B,C)/2", "y=Area(A,B,C)/2"},
		{"r=1/", "r=1/"},
		{"A=(0,0)", "A=(0,0)"},
	}
	for _, c := range cases {
		if got := stripComment(c.in); got != c.want {
			t.Errorf("stripComment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseSlashComments(t *testing.T) {
	src := `// 几何题：三角形的内心
# 两种注释可以混用
A = (0, 0)
B = (6, 0)      // 底边
C = (2, 5)
tri = Polygon(A, B, C)
I = Incenter(tri)
t = Text("http://example.com") // 字符串里的 // 不是注释
`
	stmts, probs := Parse(src)
	if len(probs) != 0 {
		t.Fatalf("unexpected parse problems: %v", probs)
	}
	if len(stmts) != 6 {
		t.Fatalf("got %d statements, want 6", len(stmts))
	}
}

func TestStripBlockComments(t *testing.T) {
	cases := []struct {
		in        string
		want      string
		wantUncop int // unclosed-comment line, 0 = well formed
	}{
		{"A=(0,0)\n", "A=(0,0)\n", 0},
		// A block comment on its own line disappears.
		{"/* 整行块注释 */\nA=(0,0)\n", " \nA=(0,0)\n", 0},
		// Multi-line block comment.
		{"/* 跨行\n   块注释 */\nA=(0,0)\n", " \nA=(0,0)\n", 0},
		// Inline block comment, and it must not glue the two tokens together.
		{"A = /* c */ B", "A =   B", 0},
		// Nested-looking content: the first "*/" closes the block.
		// The first "*/" closes the block (C/JS semantics, not pairing), so the
		// trailing "*/" is left behind — a genuine syntax error the user made.
		{"/* /* */ */\nA=(0,0)\n", "  */\nA=(0,0)\n", 0},
		// "/*" inside a string literal is not a comment start.
		{"t=Text(\"a/*b\")", "t=Text(\"a/*b\")", 0},
		// A string containing "*/" is inert once the real block has closed.
		{"/* c */ t=Text(\"x */ y\")", "  t=Text(\"x */ y\")", 0},
		// Adjacent string and block comment must not confuse the scanner.
		{"t=Text(\"/*\") /* real */", "t=Text(\"/*\")  ", 0},
		// Quotes inside a comment are inert: they do not reopen a string.
		{"/* \" /* c */ A=(0,0)\n", "  A=(0,0)\n", 0},
		// "*" separated from "/" by a space is ordinary multiplication, not a
		// block start. (Adjacent "x/*2" IS an unclosed block: see below.)
		{"y=x/ *2", "y=x/ *2", 0},
		{"y=x/*2", "", 1},
		// Unclosed blocks are reported with the line where they opened.
		{"A=(0,0)\n/* 没闭合", "", 2},
		{"/* 第一行就没闭合", "", 1},
		{"A=(0,0)\nB=(1,1)\n/*\n", "", 3},
	}
	for _, c := range cases {
		got, line := stripBlockComments(c.in)
		if got != c.want || line != c.wantUncop {
			t.Errorf("stripBlockComments(%q) = (%q, %d), want (%q, %d)", c.in, got, line, c.want, c.wantUncop)
		}
	}
}

func TestParseBlockComments(t *testing.T) {
	src := `/* 几何题：三角形的内心
   块注释跨行 */
A = (0, 0)
B = (6, 0)   // 底边
C = (2, 5)
tri = Polygon(A, B, C)
I = Incenter(tri)   /* 内心 */
`
	stmts, probs := Parse(src)
	if len(probs) != 0 {
		t.Fatalf("unexpected parse problems: %v", probs)
	}
	if len(stmts) != 5 {
		t.Fatalf("got %d statements, want 5", len(stmts))
	}
	if stmts[1].id != "B" {
		t.Errorf("second statement = %q, want B", stmts[1].id)
	}

	// An unclosed block comment is a parse error, fail-closed.
	if _, probs := Parse("A=(0,0)\n/* 没闭合\n"); len(probs) != 1 || probs[0].Line != 2 || probs[0].Code != diag.CodeParseSyntax {
		t.Fatalf("unclosed block comment should be a parse/syntax error on line 2, got %v", probs)
	}
}
