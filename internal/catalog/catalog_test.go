package catalog

import (
	"sort"
	"strings"
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
		{"Circumcircle", "", false}, // not a GeoGebra command; see the alias table
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
	//
	// Names that are NOT GeoGebra commands are deliberately absent from this
	// list AND from cmdmeta's returns map: LINETHROUGH, PARALLELLINE,
	// CIRCLEWITHCENTER, CIRCLEBYRADIUSM, CIRCUMCIRCLE and the ArcCot/ArcSec/
	// ArcCsc family. Mapping them to a kind was what let a script pass
	// validation and then fail in GeoGebra. TestAliasTableRetiresNonCommands
	// is the positive counterpart.
	for _, cmd := range []string{
		"POINTIN", "VERTEX", "INTERSECT", "TRIANGLECENTER",
		"PERPENDICULARLINE", "TANGENT", "PERPENDICULARBISECTOR",
		"ANGLEBISECTOR", "SIDE",
		"SEMICIRCLE", "PARABOLA", "HYPERBOLA", "ARC",
		"CIRCULARARC", "CIRCUMCIRCULARARC", "IMPLICITCURVE", "POLYLINE",
		"PERIMETER", "SLOPE", "RADIUS", "VOLUME", "CIRCUMFERENCE", "SIGN",
		"FLOOR", "CEIL", "ROUND", "CBRT", "NROOT", "EXP", "LN", "LOG", "LOG10",
		"COT", "SEC", "CSC", "ARCSIN", "ARCCOS", "ARCTAN",
		"SPHERE", "CONE", "CYLINDER", "QUADRIC", "ELLIPSOID",
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

// TestAliasTableRetiresNonCommands — the positive counterpart to the list
// above. Every name GeoGebra does not have must live in the alias table, must
// NOT resolve to a kind, and must NOT be reachable as a command — so a script
// using it is refused with a pointer to the real command instead of being
// accepted and then failing inside GeoGebra.
//
// This is the check that `alias` in supplement.json is live data. It was
// written to the JSON by f296d71 and read by nothing until now, which is how
// `Incenter(A,B,C)` could pass validation while GeoGebra has no such command.
func TestAliasTableRetiresNonCommands(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	cases := map[string]string{
		"Incenter":         "TriangleCenter",
		"Circumcenter":     "TriangleCenter",
		"Orthocenter":      "TriangleCenter",
		"Circumcircle":     "Circle",
		"RegularPolygon":   "Polygon",
		"TextBox":          "Textfield",
		"ParallelLine":     "Line",
		"LineThrough":      "Line",
		"CircleWithCenter": "Circle",
		"ArcCot":           "cot",
		"ArcSec":           "sec",
		"ArcCsc":           "csc",
	}
	for name, wantAlias := range cases {
		if got, ok := c.AliasOf(name); !ok || got != wantAlias {
			t.Errorf("AliasOf(%s) = %q,%v; want %q,true", name, got, ok, wantAlias)
		}
		// The whole point: it must not be a usable command any more.
		if _, ok := c.Lookup(name); ok {
			t.Errorf("%s is still a command; a script using it would pass validation and fail in GeoGebra", name)
		}
		// And it must not carry a result kind, or ApplyKinds would type it.
		if k := c.KindOf(name); k != "" {
			t.Errorf("KindOf(%s) = %q; a non-command must have no result kind", name, k)
		}
		// The alias target has to be a real command, or the hint is useless.
		if _, ok := c.Lookup(wantAlias); !ok {
			t.Errorf("alias target %q for %s is not a command", wantAlias, name)
		}
	}
	// Case-insensitive like every other lookup.
	if got, ok := c.AliasOf("incenter"); !ok || got != "TriangleCenter" {
		t.Errorf("AliasOf(incenter) = %q,%v; want TriangleCenter,true", got, ok)
	}
	if len(c.AliasNames()) < len(cases) {
		t.Errorf("AliasNames returned %d entries, want at least %d", len(c.AliasNames()), len(cases))
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

// ---------------------------------------------------------------------------
// Authoritative coverage check — bidirectional, replacing the >= 400 floor.
// ---------------------------------------------------------------------------

// TestCatalogCoversEveryKernelCommand — every command in GeoGebra's own
// Commands.java must be in the catalog. The old assertion was
// `len(Names()) >= 400`, a one-way floor: it caught the table collapsing but
// nothing else, so losing one real command (575 → 574) left it green. This is
// the direction that actually matters — a missing command is reported to the
// user as cmd/unknown on a perfectly valid script.
func TestCatalogCoversEveryKernelCommand(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	var missing []string
	for name := range kernelCommandNames {
		if _, ok := c.Lookup(name); !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d kernel command(s) missing from the catalog: %v", len(missing), missing)
	}
	if got := len(kernelCommandNames); got != 549 {
		t.Errorf("kernel-commands.txt holds %d names; the header says 549 — regenerate it", got)
	}
}

// TestCatalogHasNoUnexplainedNonKernelCommands — the other direction. A
// catalog command that is neither a kernel command nor a listed non-kernel
// command is a fabricated name, and the catalog would accept a script using it
// and then fail inside GeoGebra. Each allowed extra must carry its reason in
// non-kernel-commands.txt, so this cannot rot into a blanket allowlist.
func TestCatalogHasNoUnexplainedNonKernelCommands(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	var unexplained []string
	for _, name := range c.Names() {
		if kernelCommandNames[name] {
			continue
		}
		if !nonKernelCommandNames[name] {
			unexplained = append(unexplained, name)
		}
	}
	if len(unexplained) > 0 {
		t.Errorf("%d catalog command(s) are neither kernel commands nor listed "+
			"non-kernel commands — either a hallucination or a missing entry in "+
			"non-kernel-commands.txt: %v", len(unexplained), unexplained)
	}
	// The reverse: an entry excused in the file but no longer in the catalog is
	// a stale excuse, which hides a real removal.
	var stale []string
	for name := range nonKernelCommandNames {
		if _, ok := c.Lookup(name); !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("non-kernel-commands.txt excuses %d name(s) no longer in the catalog: %v", len(stale), stale)
	}
}

// TestNonKernelListDocumentsItsCategories — every excused entry must declare a
// known category and a non-empty reason, so the file stays reviewable data
// rather than a list of names.
func TestNonKernelListDocumentsItsCategories(t *testing.T) {
	known := map[string]bool{"parser-function": true, "reserved-word": true}
	seen := 0
	for _, raw := range strings.Split(string(nonKernelCommandsTXT), "\n") {
		if i := strings.IndexByte(raw, '#'); i >= 0 {
			raw = raw[:i]
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		seen++
		parts := strings.Split(raw, "\t")
		if len(parts) != 3 {
			t.Errorf("entry %q must be <NAME>\\t<category>\\t<reason>", raw)
			continue
		}
		if !known[parts[1]] {
			t.Errorf("entry %q has unknown category %q", parts[0], parts[1])
		}
		if len(parts[2]) < 20 {
			t.Errorf("entry %q needs a real reason, got %q", parts[0], parts[2])
		}
	}
	if seen == 0 {
		t.Fatal("non-kernel-commands.txt parsed as empty")
	}
}

// TestDefaultLoadsAllCountIsPinned — the totals are pinned so an accidental
// bulk edit to the JSON files cannot quietly reshape the table. A deliberate
// change must update these numbers in the same commit.
func TestDefaultLoadsAllCountIsPinned(t *testing.T) {
	c, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	const wantCommands, wantAliases = 575, 12
	if got := len(c.Names()); got != wantCommands {
		t.Errorf("catalog has %d commands, want %d", got, wantCommands)
	}
	if got := len(c.AliasNames()); got != wantAliases {
		t.Errorf("catalog has %d non-command aliases, want %d", got, wantAliases)
	}
}
