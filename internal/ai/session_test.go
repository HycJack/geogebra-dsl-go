package ai

import (
	"context"
	"strings"
	"testing"
)

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

// TestMultiTurnCarriesFinalScript verifies the full round-trip shape the HTTP
// layer relies on: a completed turn is stored as (user → assistant final script)
// and a subsequent Generate's prompt includes that previous final script, so the
// model can modify it rather than regenerate.
func TestMultiTurnCarriesFinalScript(t *testing.T) {
	sess := NewSession("s-mt", 20)
	sysMsg := SystemMessage()

	// Round 1: a valid script is produced and stored as the assistant turn.
	round1 := "A = (0, 2)\nB = (4, 2)\nl = Line(A, B)"
	stub := &stubClient{replies: []string{goodReply(round1)}}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 1, MaxHistory: 20}

	user1 := TextUserMessage("作线段 AB")
	res1 := Generate(context.Background(), stub, cfg, GenerateRequest{Session: sess, SystemMsg: sysMsg, UserMsg: user1})
	if !res1.OK {
		t.Fatalf("round1 not OK: %+v", res1)
	}

	// Simulate the HTTP layer persisting the completed turn.
	sess.Append(user1)
	sess.Append(TextAssistantMessage(res1.Script))

	// Round 2 (append): the prompt fed to the model must contain the previous
	// final script so the follow-up can modify it.
	user2 := TextUserMessage("把线段改为红色")
	stub2 := &stubClient{replies: []string{goodReply("A = (0, 2)\nB = (4, 2)")}}
	res2 := Generate(context.Background(), stub2, cfg, GenerateRequest{Session: sess, SystemMsg: sysMsg, UserMsg: user2})
	if !res2.OK {
		t.Fatalf("round2 not OK: %+v", res2)
	}
	// The first call in round 2 receives system + full history + user2.
	joined := strings.Join(stub2.calls, "\n")
	if !strings.Contains(joined, "Line(A, B)") {
		t.Errorf("round2 prompt missing previous final script; got: %q", joined)
	}
	// The previous final script must appear as an assistant message.
	foundAssistant := false
	for _, m := range sess.ContextMessages() {
		if m.Role == RoleAssistant && strings.Contains(m.Content[0].Text, "Line(A, B)") {
			foundAssistant = true
		}
	}
	if !foundAssistant {
		t.Error("session history missing the assistant final script")
	}
}

// TestFailureTurnStillStoresAssistant verifies that even when generation fails
// (no script produced), the HTTP layer's "always store user + assistant" rule
// keeps the pairing in history intact, so a later append turn sees a completed
// round rather than two user messages in a row.
func TestFailureTurnStillStoresAssistant(t *testing.T) {
	sess := NewSession("s-fail", 20)
	sysMsg := SystemMessage()
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 1, MaxHistory: 20}

	// A reply with no <gg> block yields ErrNoScript → res.Script == "".
	stub := &stubClient{replies: []string{"I cannot do that."}}
	user := TextUserMessage("画个圆")
	res := Generate(context.Background(), stub, cfg, GenerateRequest{Session: sess, SystemMsg: sysMsg, UserMsg: user})
	if res == nil || res.Script != "" {
		t.Fatalf("expected failed result with empty script, got %+v", res)
	}

	// Simulate the HTTP layer: always store user + assistant even on failure.
	sess.Append(user)
	sess.Append(TextAssistantMessage(res.Script))

	msgs := sess.ContextMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected exactly [user, assistant], got %d: %#v", len(msgs), msgs)
	}
	if msgs[0].Role != RoleUser || msgs[1].Role != RoleAssistant {
		t.Fatalf("expected user then assistant roles, got %s then %s", msgs[0].Role, msgs[1].Role)
	}
	if !strings.Contains(msgs[1].Content[0].Text, "<gg>") {
		t.Errorf("assistant turn should still be a well-formed block, got %q", msgs[1].Content[0].Text)
	}
}
