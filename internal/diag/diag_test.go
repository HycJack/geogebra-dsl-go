package diag

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestReceiptWarningsShape guards the documented receipt contract: the
// warnings field must serialize as a JSON array (never null) even when empty.
func TestReceiptWarningsShape(t *testing.T) {
	rc := NewReceipt("text")
	b, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"warnings":null`) {
		t.Fatalf("warnings must not serialize to null, got %s", b)
	}
	if !strings.Contains(string(b), `"warnings":[]`) {
		t.Fatalf("expected empty warnings array, got %s", b)
	}
}

// TestReceiptCollectionsAreAllArrays — errors was the one collection left nil,
// so a clean run serialized `"errors": null`. Hosts drive their repair loop by
// iterating the receipt's arrays, and a null breaks that (the WASM entry point
// documents the receipt as directly consumable). All four collections are part
// of the same contract, so they are asserted together.
func TestReceiptCollectionsAreAllArrays(t *testing.T) {
	b, err := json.Marshal(NewReceipt("ir"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{`"errors":[]`, `"warnings":[]`, `"executable":[]`, `"kinds":{}`} {
		if !strings.Contains(string(b), field) {
			t.Errorf("expected %s in receipt JSON, got %s", field, b)
		}
	}
	for _, field := range []string{`"errors":null`, `"warnings":null`, `"executable":null`, `"kinds":null`} {
		if strings.Contains(string(b), field) {
			t.Errorf("%s must not appear in receipt JSON, got %s", field, b)
		}
	}
}

func TestFailSetsOKFalse(t *testing.T) {
	rc := NewReceipt("text")
	rc.Fail(Problem{Code: CodeDepCycle, Msg: "cycle"})
	if rc.OK {
		t.Fatal("Fail must set OK=false")
	}
	if len(rc.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(rc.Errors))
	}
}
