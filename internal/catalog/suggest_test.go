package catalog

import "testing"

func suggestCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load([]byte(`{"commands": {
  "Segment": {"name":"Segment","overloads":[]},
  "Line":    {"name":"Line","overloads":[]},
  "Point":   {"name":"Point","overloads":[]},
  "Rotate":  {"name":"Rotate","overloads":[]}
}}`))
	if err != nil {
		panic(err)
	}
	return c
}

func TestSuggestTypo(t *testing.T) {
	c := suggestCatalog(t)
	got := c.Suggest("Segement", 3)
	if len(got) == 0 || got[0] != "Segment" {
		t.Fatalf("expected Segment first, got %v", got)
	}
}

// An exact case-insensitive match must not be suggested — Lookup would already
// have resolved the command.
func TestSuggestExactMatchNotSuggested(t *testing.T) {
	c := suggestCatalog(t)
	for _, in := range []string{"Rotate", "rotate", "ROTATE"} {
		if got := c.Suggest(in, 3); len(got) != 0 {
			t.Fatalf("Suggest(%q): exact match must not be suggested, got %v", in, got)
		}
	}
}

func TestSuggestSeparatorVariant(t *testing.T) {
	c := suggestCatalog(t)
	// Lookup compares raw uppercase names, so separator variants do not resolve
	// and must be surfaced as suggestions.
	for _, in := range []string{"Segment_", "seg-ment", "Seg.ment"} {
		got := c.Suggest(in, 3)
		if len(got) == 0 || got[0] != "Segment" {
			t.Errorf("Suggest(%q) = %v, want Segment first", in, got)
		}
	}
}

func TestSuggestNoPlausibleMatch(t *testing.T) {
	c := suggestCatalog(t)
	// Way too different to be a typo of anything.
	if got := c.Suggest("Zqxwvplmnbkjh", 3); len(got) != 0 {
		t.Fatalf("expected no suggestions, got %v", got)
	}
}

func TestSuggestHonorsLimit(t *testing.T) {
	c := suggestCatalog(t)
	if got := c.Suggest("Segement", 2); len(got) > 2 {
		t.Fatalf("expected at most 2, got %v", got)
	}
	if got := c.Suggest("Segement", 0); got != nil {
		t.Fatalf("expected nil for n=0, got %v", got)
	}
}
