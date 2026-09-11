package ai

import "testing"

func TestSessionTrimsToCap(t *testing.T) {
	s := NewSession("s1", 3)
	for i := 0; i < 10; i++ {
		s.Append(TextUserMessage("turn"))
	}
	if got := len(s.ContextMessages()); got != 3 {
		t.Fatalf("context length=%d, want cap 3", got)
	}
	if !s.HasMessages() {
		t.Fatal("session should have messages")
	}
}

func TestSessionEmpty(t *testing.T) {
	s := NewSession("s2", 20)
	if s.HasMessages() {
		t.Fatal("new session should be empty")
	}
	if got := len(s.ContextMessages()); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestSessionMinCap(t *testing.T) {
	s := NewSession("s3", 0) // degenerate cap clamps to 1
	s.Append(TextUserMessage("a"))
	s.Append(TextUserMessage("b"))
	if got := len(s.ContextMessages()); got != 1 {
		t.Fatalf("expected clamped cap 1, got %d", got)
	}
}
