package ir

import "testing"

// TestKindStringRoundTrip — Kind.String() and KindFromToken are the two halves
// of one bidirectional mapping (the catalog stores tokens, the graph uses
// kinds). A round trip must be the identity for every declared kind, so a new
// Kind can never silently desync the two tables.
func TestKindStringRoundTrip(t *testing.T) {
	for k := KUnknown; k <= KTurtle; k++ {
		tok := k.String()
		if k != KUnknown && tok == "Unknown" {
			t.Fatalf("kind %d has no distinct String() name", k)
		}
		back := KindFromToken(tok)
		if back != k {
			t.Errorf("KindFromToken(%q) = %v, want %v", tok, back, k)
		}
	}
	// The Unknown sentinel round-trips through "Unknown".
	if got := KindFromToken("Unknown"); got != KUnknown {
		t.Errorf("KindFromToken(Unknown) = %v, want KUnknown", got)
	}
	// Unknown/garbage tokens yield KUnknown.
	if got := KindFromToken(""); got != KUnknown {
		t.Errorf("KindFromToken(\"\") = %v, want KUnknown", got)
	}
	if got := KindFromToken("NoSuchKind"); got != KUnknown {
		t.Errorf("KindFromToken(NoSuchKind) = %v, want KUnknown", got)
	}
}
