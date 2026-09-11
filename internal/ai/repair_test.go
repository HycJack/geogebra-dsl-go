package ai

import (
	"context"
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

func (s *stubClient) Complete(_ context.Context, msgs []Message, _ CompleteOptions) (string, error) {
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
		return "", err
	}
	reply := ""
	if s.next < len(s.replies) {
		reply = s.replies[s.next]
	}
	s.next++
	return reply, nil
}

func goodReply(script string) string {
	return "<gg>\n" + script + "\n</gg>\n<!-- 说明：先做定点后做线 -->"
}

func TestGenerateOneShot(t *testing.T) {
	// A fully-defined, non-degenerate teaching construction passes on the first
	// attempt with no repair round.
	script := "A = Point(0, 0)\nB = Point(4, 0)\nC = Point(2, 4)\nl = Line(A, B)\nc = Circle(C, A)"
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
	good := "A = Point(0, 0)\nB = Point(4, 0)\nl = Line(A, B)"
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
