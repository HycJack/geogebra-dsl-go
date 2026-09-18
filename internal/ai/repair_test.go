package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubClient is a test ChatClient whose replies follow a script (pun intended):
// each configured reply is returned in order. It records the full message text
// sent for every call so tests can assert the repair loop feeds diagnostics back.
type stubClient struct {
	replies  []string
	calls    []string // concatenated messages for each call
	next     int
	callErrs []error
}

func (s *stubClient) Complete(_ context.Context, msgs []Message, opts CompleteOptions) (string, error) {
	var b strings.Builder
	for _, m := range msgs {
		for _, p := range m.Content {
			b.WriteString(p.Text)
		}
	}
	s.calls = append(s.calls, b.String())
	if s.next < len(s.callErrs) && s.callErrs[s.next] != nil {
		err := s.callErrs[s.next]
		s.next++
		// Mirror the real client: a trace (if configured) records the error step.
		opts.Trace.add(Step{Stage: "error", Attempt: opts.Attempt, Error: err.Error()})
		return "", err
	}
	reply := ""
	if s.next < len(s.replies) {
		reply = s.replies[s.next]
	}
	s.next++
	// Mirror the real client: record the LLM call step into the trace.
	opts.Trace.add(Step{Stage: "llm", Attempt: opts.Attempt, Reply: ellipsize(reply, 400), LatencyMS: 0})
	return reply, nil
}

func goodReply(script string) string {
	return "<gg>\n" + script + "\n</gg>\n<!-- 说明：先做定点后做线 -->"
}

func TestGenerateOneShot(t *testing.T) {
	// A fully-defined, non-degenerate teaching construction passes on the first
	// attempt with no repair round.
	script := "A = (0, 0)\nB = (4, 0)\nC = (2, 4)\nl = Line(A, B)\nc = Circle(C, A)"
	stub := &stubClient{replies: []string{goodReply(script)}}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 1}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s1", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("求过 C 的圆"),
	})
	if !res.OK {
		t.Fatalf("expected OK, got %+v", res)
	}
	if strings.Contains(res.Script, "c = Circle(C, A)") == false {
		t.Fatalf("script missing circle line: %q", res.Script)
	}
	if res.TeachingNote == "" {
		t.Error("expected teaching note to be parsed")
	}
	if res.Attempts != 1 {
		t.Fatalf("attempts=%d, want 1", res.Attempts)
	}
}

func TestGenerateRepairsOnce(t *testing.T) {
	bad := "l = Line(A, Missing)" // dep/undefined: Missing undefined
	good := "A = (0, 0)\nB = (4, 0)\nl = Line(A, B)"
	stub := &stubClient{replies: []string{goodReply(bad), goodReply(good)}}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 3}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s2", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("作线段 AB"),
	})
	if !res.OK {
		t.Fatalf("expected OK after repair, got %+v", res)
	}
	if res.Attempts != 2 {
		t.Fatalf("attempts=%d, want 2", res.Attempts)
	}
	// The second call must carry the repair scaffold with the diagnostic.
	if !strings.Contains(stub.calls[1], "dep/undefined") {
		t.Errorf("repair call missing diagnostic; call=%q", stub.calls[1])
	}
	if !strings.Contains(stub.calls[1], "引用了未定义对象") {
		t.Errorf("repair call missing hint; call=%q", stub.calls[1])
	}
}

func TestGenerateExhaustsRetries(t *testing.T) {
	// Every reply is bad, so the loop must exhaust the cap and report failure
	// without panicking or looping forever.
	bad := "l = Line(A, Missing)"
	stub := &stubClient{replies: []string{goodReply(bad), goodReply(bad), goodReply(bad), goodReply(bad)}}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 3}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s3", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("任意题"),
	})
	if res.OK {
		t.Fatal("expected failure when all attempts are bad")
	}
	if res.Attempts != 4 { // 1 initial + 3 repairs
		t.Fatalf("attempts=%d, want 4", res.Attempts)
	}
	if len(res.Diagnostics) == 0 {
		t.Error("expected unresolved diagnostics on exhausted retries")
	}
}

func TestGenerateNoScriptBlock(t *testing.T) {
	stub := &stubClient{replies: []string{"I cannot help with that."}}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 1}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s4", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("任意题"),
	})
	if res == nil {
		t.Fatal("Generate must always return a Result")
	}
}

// TestGenerateTransientMidRepairKeepsContent reproduces the mid-repair network
// failure case: a repair round's LLM call returns a transient error (simulating
// Complete exhausting its exponential-backoff retries). The NEXT repair round's
// prompt must still carry the previous real script AND its diagnostics — the
// content must not be lost to the hiccup.
func TestGenerateTransientMidRepairKeepsContent(t *testing.T) {
	bad := "l = Line(A, Missing)" // gate fails: dep/undefined Missing
	good := "A = (0, 0)\nB = (4, 0)\nl = Line(A, B)"

	// Call #0 (attempt 1): returns the bad script → gate fails.
	// Call #1 (attempt 2): transient error, like a backend that keeps failing.
	// Call #2 (attempt 3): returns a repaired script → gate passes.
	stub := &stubClient{
		replies:  []string{goodReply(bad), "", goodReply(good)},
		callErrs: []error{nil, errors.New("chat backend 503 transient"), nil},
	}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 3}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s-tran", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("作线段"),
	})
	if !res.OK {
		t.Fatalf("expected final OK, got %+v", res)
	}
	if res.Attempts != 3 {
		t.Fatalf("attempts=%d, want 3", res.Attempts)
	}
	// The repair round after the transient error (call #2) must have been built
	// on the previous real bad script + its diagnostics — not wiped.
	call := stub.calls[2]
	if !strings.Contains(call, bad) {
		t.Errorf("repair round after transient error lost previous script; call=%q", call)
	}
	if !strings.Contains(call, "dep/undefined") || !strings.Contains(call, "Missing") {
		t.Errorf("repair round after transient error lost diagnostics; call=%q", call)
	}
}

// TestGenerateFirstCallTransientStillSucceeds checks that if the very first LLM
// call transiently fails (no content was ever produced), the loop does not panic
// and still attempts a plain regeneration rather than an artificial repair.
func TestGenerateFirstCallTransientStillSucceeds(t *testing.T) {
	good := "A = (0, 0)\nB = (4, 0)\nl = Line(A, B)"
	stub := &stubClient{
		replies:  []string{"", goodReply(good)},
		callErrs: []error{errors.New("connection refused"), nil},
	}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 3}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s-first-tran", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("作线段"),
	})
	if !res.OK {
		t.Fatalf("expected OK after first-call transient, got %+v", res)
	}
}

// TestGenerateTraceRecordsSteps verifies the trace exposed to the chat UI
// captures each attempt: the LLM call, the ggbcheck gate outcome (driving repair),
// and any transient error (mid-repair network failure).
func TestGenerateTraceRecordsSteps(t *testing.T) {
	bad := "l = Line(A, Missing)"
	good := "A = (0, 0)\nB = (4, 0)\nl = Line(A, B)"
	stub := &stubClient{
		replies:  []string{goodReply(bad), "", goodReply(good)},
		callErrs: []error{nil, errors.New("chat backend 503 transient"), nil},
	}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 3}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s-trace", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("作线段"),
	})
	if !res.OK {
		t.Fatalf("expected final OK, got %+v", res)
	}
	if len(res.Trace) == 0 {
		t.Fatal("expected a non-empty trace")
	}

	// First generation attempt: failed gate on the bad script.
	var sawLLM1, sawGate1, sawErr, sawGateOK bool
	for _, s := range res.Trace {
		switch s.Stage {
		case "llm":
			if s.Attempt == 1 {
				sawLLM1 = true
			}
		case "gate":
			if s.Attempt == 1 && s.GateOK != nil && !*s.GateOK {
				sawGate1 = true
			}
			if s.GateOK != nil && *s.GateOK {
				sawGateOK = true
			}
		case "error":
			if s.Attempt == 2 {
				sawErr = true
			}
		}
	}
	if !sawLLM1 {
		t.Error("expected an llm step for attempt 1")
	}
	if !sawGate1 {
		t.Error("expected a failing gate step for attempt 1")
	}
	if !sawErr {
		t.Error("expected an error step for the transient attempt 2")
	}
	if !sawGateOK {
		t.Error("expected a passing gate step for the final OK attempt")
	}
	// The final successful gate on attempt 3 must record the repaired script.
	var lastGateScript string
	for _, s := range res.Trace {
		if s.Stage == "gate" && s.Script != "" {
			lastGateScript = s.Script
		}
	}
	if !strings.Contains(lastGateScript, "l = Line(A, B)") {
		t.Errorf("expected final gate script to be the repaired one; got %q", lastGateScript)
	}
}

// TestGenerateDegradesToTextAnswer verifies that a pure-calculation / explanation
// reply with no <gg> block is surfaced as a degraded textual answer at the end of
// the loop rather than hard-failing with an empty result.
func TestGenerateDegradesToTextAnswer(t *testing.T) {
	textReply := "圆心 (0,-2) 到直线 y=√3 x+2 的距离为 2，因此距离为 1 的点恰有两个需要 1 < r < 3，故选 B."
	stub := &stubClient{replies: []string{textReply, textReply, textReply}}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 2, MaxHistory: 20}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s-degrade", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("求 r 的取值范围"),
	})
	if res == nil {
		t.Fatal("expected a result, got nil")
	}
	if res.OK {
		t.Fatal("degraded text answer should not claim gate-passed OK")
	}
	if res.Script != "" {
		t.Fatalf("expected empty script, got %q", res.Script)
	}
	if res.Fallback != textReply {
		t.Fatalf("expected fallback text, got %q", res.Fallback)
	}
	if res.Attempts != cfg.MaxRepair+1 {
		t.Errorf("expected %d attempts, got %d", cfg.MaxRepair+1, res.Attempts)
	}
}

// TestGenerateNoScriptEmptyReplyStillFails verifies that a genuinely empty reply
// (no script AND no text) still fails cleanly with an empty script, not a
// misleading fallback.
func TestGenerateNoScriptEmptyReplyStillFails(t *testing.T) {
	stub := &stubClient{replies: []string{"", "", ""}}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 2, MaxHistory: 20}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s-empty", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("画个圆"),
	})
	if res == nil {
		t.Fatal("expected a result, got nil")
	}
	if res.Fallback != "" {
		t.Fatalf("expected no fallback for an empty reply, got %q", res.Fallback)
	}
	if res.Script != "" {
		t.Fatalf("expected empty script, got %q", res.Script)
	}
}

// TestGenerateRepairDirectsToGgEvenWhenFirstIsText verifies that a no-script
// text reply on the first attempt does not prevent a later attempt from
// producing a real <gg> script (e.g. the user did ask for a figure).
func TestGenerateRepairCanStillProduceScriptAfterText(t *testing.T) {
	textReply := "下面是解题思路，无构造物。"
	script := "A = (0, 0)\nB = (4, 0)\nc = Circle(A, B)"
	stub := &stubClient{replies: []string{textReply, goodReply(script)}}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 2, MaxHistory: 20}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s-text-then-gg", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("作圆并画出配图"),
	})
	if !res.OK {
		t.Fatalf("expected a real script on a later attempt, got %+v", res)
	}
	if res.Fallback != "" {
		t.Fatalf("expected no fallback when a script was produced, got %q", res.Fallback)
	}
	if !strings.Contains(res.Script, "c = Circle(A, B)") {
		t.Fatalf("script missing circle: %q", res.Script)
	}
}
