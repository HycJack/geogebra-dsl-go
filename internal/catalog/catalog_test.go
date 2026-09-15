package catalog

import "testing"

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
