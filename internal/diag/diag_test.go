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
