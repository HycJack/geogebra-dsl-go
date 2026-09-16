package catalog

import (
	"testing"

	"github.com/hycjack/geogebra-dsl-go/internal/ir"
)

// TestLoadMapShape covers a category file whose "commands" is a map keyed by name.
func TestLoadMapShape(t *testing.T) {
	const js = `{"category":"Geometry","count":1,"commands":{
  "Line": {"name":"Line","overloads":[
    {"syntax":"Line(<Point>,<Point>)","params":[{"name":"<Point>","role":"a","type":"Point"},{"name":"<Point>","role":"b","type":"Point"}]}
  ]}
}}`
	c, err := Load([]byte(js))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !c.Has("Line") {
		t.Fatal("Line should be present")
	}
}

// TestLoadArrayShape covers the CAS file whose "commands" is an array.
func TestLoadArrayShape(t *testing.T) {
	const js = `{"command_category":"CAS","total_commands":2,"commands":[
  {"name":"Assume","overloads":[{"syntax":"Assume(<Condition>)","params":[{"name":"<Condition>","role":"c","type":"GeoObject"}]}]},
  {"name":"NSolve","overloads":[]}
]}`
	c, err := Load([]byte(js))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !c.Has("Assume") || !c.Has("NSolve") {
		t.Fatal("array-shape commands not merged")
	}
}

// TestMergeOverloads verifies overloads accumulate across category files for a
// command that appears in several of them.
func TestMergeOverloads(t *testing.T) {
	mk := func(syntax string) []byte {
		return []byte(`{"commands":{"Line":{"name":"Line","overloads":[{"syntax":"` + syntax + `","params":[]}]}}}`)
	}
	var c *Catalog
	var err error
	if c, err = Load(mk("Line(<Point>,<Point>)")); err != nil {
		t.Fatal(err)
	}
	if err := c.mergeDoc(mk("Line(<Point>,<Parallel Line>)")); err != nil {
		t.Fatal(err)
	}
	cmd, ok := c.Lookup("Line")
	if !ok {
		t.Fatal("Line missing")
	}
	if len(cmd.Overloads) != 2 {
		t.Fatalf("expected 2 overloads after merge, got %d", len(cmd.Overloads))
	}
}

// TestDefaultLoadsAll verifies the full embedded catalog loads and covers the
// distinctive categories.
func TestDefaultLoadsAll(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if len(c.Names()) < 400 {
		t.Fatalf("expected >=400 merged commands, got %d", len(c.Names()))
	}
	// Commands from different category files must be present together.
	for _, want := range []string{"Sequence", "If", "Text", "Assume", "Circle", "Point", "Line"} {
		if !c.Has(want) {
			t.Errorf("missing merged command %q", want)
		}
	}
	// Built-in scalar math commands (used inside coordinates / expressions) must
	// be recognized so nested calls like Sqrt(3) validate instead of cmd/unknown.
	for _, want := range []string{"Sqrt", "Cbrt", "NRoot", "Abs", "Sin", "Cos", "Tan", "ln", "Log", "exp"} {
		if !c.Has(want) {
			t.Errorf("missing math command %q", want)
		}
	}
}

// TestVarArgEllipsisSpellings verifies both ellipsis spellings used by the
// catalog mark an overload as variadic. Only matching the ASCII "..." left the
// Unicode-ellipsis overloads fixed-arity, so Element(lst, 1, 2, 3) and
// Repeat(8, c1, c2) were rejected.
func TestVarArgEllipsisSpellings(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	for _, name := range []string{"Repeat", "Element", "Join", "Net", "Area", "If", "Zip", "SelectObjects", "TableText"} {
		cmd, ok := c.Lookup(name)
		if !ok {
			t.Errorf("missing command %q", name)
			continue
		}
		variadic := false
		for _, ov := range cmd.Overloads {
			if ov.IsVarArg {
				variadic = true
			}
		}
		if !variadic {
			t.Errorf("expected %s to have a variadic overload", name)
		}
	}
}

// TestHasEllipsis covers the two spellings directly.
func TestHasEllipsis(t *testing.T) {
	for _, in := range []string{"Join(<List>,<List>, ...)", "Zip(<Expression>,<Var1>,<List1>, …)", "A(<B>)"} {
		want := in != "A(<B>)"
		if hasEllipsis(in) != want {
			t.Errorf("hasEllipsis(%q) = %v, want %v", in, hasEllipsis(in), want)
		}
	}
}

// TestDefaultIsCached — regression: parsing ~20 embedded JSON category files
// on every Check call dominated validation cost; Default() must return the
// same process-wide catalog every time.
func TestDefaultIsCached(t *testing.T) {
	c1, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	c2, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if c1 != c2 {
		t.Fatal("Default() must return the cached catalog, not rebuild it")
	}
}

// TestKindOfAndScripting — the command→kind mapping and the scripting-command
// set are now data (cmdmeta.json). Spot-check the interesting boundary cases:
// a value-returning command, a scripting command that also returns a value, a
// plain scripting command (Script), and an unmodeled command ("").
func TestKindOfAndScripting(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	cases := []struct {
		cmd        string
		wantKind   string
		wantScript bool
	}{
		{"Point", "Point", false},
		{"circle", "Circle", false}, // case-insensitive
		{"Circumcircle", "Circle", false},
		{"Sqrt", "Number", false},
		{"Slider", "Number", true}, // scripting command that yields a usable number
		{"SetColor", "Script", true},
		{"StartAnimation", "Script", true},
		{"Turtle", "Turtle", true},
		{"TotallyUnknownCmd", "", false},
	}
	for _, tc := range cases {
		if got := c.KindOf(tc.cmd); got != tc.wantKind {
			t.Errorf("KindOf(%s) = %q, want %q", tc.cmd, got, tc.wantKind)
		}
		if got := c.IsScriptingCommand(tc.cmd); got != tc.wantScript {
			t.Errorf("IsScriptingCommand(%s) = %v, want %v", tc.cmd, got, tc.wantScript)
		}
	}
}

// TestKindOfCoversAllKindForCmdCases — the data-driven returns map must cover
// every command the old code switch mapped. This guards against losing a
// mapping when the table moves from code to data.
func TestKindOfCoversAllKindForCmdCases(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	// Every command in the catalog that the old kindForCmd knew must still
	// resolve to a non-empty kind. Commands outside the old switch resolve to
	// "" and stay KUnknown — that is expected, so only spot-check known ones.
	for _, cmd := range []string{
		"POINTIN", "VERTEX", "INTERSECT", "TRIANGLECENTER", "LINETHROUGH",
		"PERPENDICULARLINE", "PARALLELLINE", "TANGENT", "PERPENDICULARBISECTOR",
		"ANGLEBISECTOR", "SIDE", "CIRCLEWITHCENTER", "CIRCLEBYRADIUSM",
		"SEMICIRCLE", "CIRCUMCIRCLE", "PARABOLA", "HYPERBOLA", "ARC",
		"CIRCULARARC", "CIRCUMCIRCULARARC", "IMPLICITCURVE", "POLYLINE",
		"PERIMETER", "SLOPE", "RADIUS", "VOLUME", "CIRCUMFERENCE", "SIGN",
		"FLOOR", "CEIL", "ROUND", "CBRT", "NROOT", "EXP", "LN", "LOG", "LOG10",
		"COT", "SEC", "CSC", "ARCSIN", "ARCCOS", "ARCTAN", "ARCCOT", "ARCSEC",
		"ARCCSC", "SPHERE", "CONE", "CYLINDER", "QUADRIC", "ELLIPSOID",
		"HYPERBOLOID", "SURFACE", "ORTHOGONALPLANE", "PERPENDICULARPLANE",
		"PARALLELPLANE", "PRISM", "PYRAMID", "POLYHEDRON", "TETRAHEDRON",
		"OCTAHEDRON", "HEXAHEDRON", "ICOSAHEDRON", "DODECAHEDRON", "LIST",
		"SEQUENCE", "READTEXT", "MATRIX", "POLYNOMIAL", "CURVE", "SPLINE",
		"LOCUS", "UNION", "DIFFERENCE", "GETTIME", "CUBE",
	} {
		if c.KindOf(cmd) == "" {
			t.Errorf("KindOf(%s) is empty; the old kindForCmd mapped this command", cmd)
		}
	}
}

// TestReturnsTokensAllResolve — every kind token the cmdmeta data emits must
// map back to a real ir.Kind (not KUnknown). This is the guard at the
// data↔code seam: a typo in cmdmeta.json or a missing KindFromToken branch
// shows up here, not as a silent KUnknown deep in a check.
func TestReturnsTokensAllResolve(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for cmd, tok := range c.returns {
		if got := ir.KindFromToken(tok); got == ir.KUnknown {
			t.Errorf("returns[%s] = %q does not resolve to a real ir.Kind", cmd, tok)
		}
	}
	// And every scripting command must at least resolve through the "Script"
	// fallback (scripting-with-value commands resolve via their returns entry).
	for cmd := range c.scripting {
		if c.KindOf(cmd) == "" {
			t.Errorf("KindOf(%s) empty; scripting command must at least resolve to Script", cmd)
		}
	}
}
