package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hycjack/geogebra-dsl-go/internal/ai"
)

// doChat runs one /api/chat request against the handler via httptest and
// decodes the enveloped response (session_id + Result).
func doChat(t *testing.T, srv *server, body string) *chatResponse {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleChat(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var env chatResponse
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return &env
}

// captureClient is a test ChatClient that records the full text of every message
// bundle it receives and optionally injects scripted replies. Used to verify the
// HTTP layer's multi-turn append contract without a live LLM.
type captureClient struct {
	replies []string
	calls   []string
	next    int
}

func (c *captureClient) Complete(_ context.Context, msgs []ai.Message, _ ai.CompleteOptions) (string, error) {
	var b strings.Builder
	for _, m := range msgs {
		role := string(m.Role)
		for _, p := range m.Content {
			role = role + ":" + p.Text + "\n"
		}
		b.WriteString(role)
	}
	c.calls = append(c.calls, b.String())
	reply := ""
	if c.next < len(c.replies) {
		reply = c.replies[c.next]
	}
	c.next++
	return reply, nil
}

// goodGGBReply returns a reply whose extracted script builds a valid circle, so
// the ggcm gate passes on the first attempt.
func goodGGBReply(script string) string {
	return "<gg>\n" + script + "\n</gg>\n<!-- 说明：已构造 -->"
}

func TestHandleChatMultiTurnAppendContract(t *testing.T) {
	cfg := ai.LoadConfig()
	cfg.MaxRepair = 1
	stub := &captureClient{replies: []string{
		goodGGBReply("A = (0,0)\nB = (4,0)\nc = Circle(A, B)"),
		goodGGBReply("A = (0,0)\nB = (4,0)\nc = Circle(A, B)\nd = Circle(B, A)"),
	}}
	srv := &server{
		cfg:      cfg,
		client:   stub,
		sessions: newSessionStore(cfg.MaxHistory),
		logs:     nil,
	}

	// Turn 1: fresh generation of a text problem.
	res1 := doChat(t, srv, `{"input_type":"text","text":"作圆 c，圆心 (0,0) 过 (4,0)","stream":false}`)
	if !res1.OK {
		t.Fatalf("turn 1 expected OK, got %+v", res1)
	}
	if res1.SessionID == "" {
		t.Fatal("turn 1 should return a session id")
	}
	if len(res1.Trace) == 0 {
		t.Fatal("turn 1 should include a trace for the chat UI")
	}

	// Turn 2: append must carry session_id + input_type + append so the server
	// resumes the SAME session and instructs a modification of the prior script.
	res2 := doChat(t, srv, `{"session_id":"`+res1.SessionID+`","input_type":"text","append":"再作一个以 B 为圆心过 A 的圆","stream":false}`)
	if !res2.OK {
		t.Fatalf("turn 2 expected OK, got %+v", res2)
	}
	if res2.SessionID != res1.SessionID {
		t.Fatalf("append turn lost the session: got %q want %q", res2.SessionID, res1.SessionID)
	}
	// The second generation call must be built on the previous final script
	// (the "append" scaffold), NOT a blank regeneration.
	if len(stub.calls) < 2 {
		t.Fatalf("expected >=2 LLM calls, got %d", len(stub.calls))
	}
	if !strings.Contains(stub.calls[1], "追加修改") {
		t.Errorf("append turn did not carry the append instruction scaffold; call=%q", stub.calls[1])
	}
	if !strings.Contains(stub.calls[1], "c = Circle(A, B)") {
		t.Errorf("append turn lost the prior script from context; call=%q", stub.calls[1])
	}
}

// TestHandleChatModeSelectsPrompt verifies the "mode" field steers the system
// prompt so a 3D request gets the 3D guidance (and thus 3D construction).
func TestHandleChatModeSelectsPrompt(t *testing.T) {
	cfg := ai.LoadConfig()
	cfg.MaxRepair = 0
	stub := &captureClient{replies: []string{
		goodGGBReply("A = (0,0,0)\nB = (1,0,0)\nsp = Sphere(A, B)"),
	}}
	srv := &server{cfg: cfg, client: stub, sessions: newSessionStore(cfg.MaxHistory), logs: nil}

	doChat(t, srv, `{"input_type":"text","text":"作一个球","mode":"3d","stream":false}`)
	if len(stub.calls) == 0 {
		t.Fatal("expected an LLM call")
	}
	if !strings.Contains(stub.calls[0], "3D 模式") {
		t.Errorf("3D mode request should use the 3D system prompt; call=%q", stub.calls[0])
	}
	if !strings.Contains(stub.calls[0], "Sphere") {
		t.Errorf("3D system prompt should mention Sphere; call=%q", stub.calls[0])
	}
}

// TestHandleChatDefaultModeUses2DPrompt verifies that without a mode (or with a
// non-3D mode) the 2D prompt is used.
func TestHandleChatDefaultModeUses2DPrompt(t *testing.T) {
	cfg := ai.LoadConfig()
	cfg.MaxRepair = 0
	stub := &captureClient{replies: []string{goodGGBReply("A = (0,0)\nB = (4,0)\nc = Circle(A, B)")}}
	srv := &server{cfg: cfg, client: stub, sessions: newSessionStore(cfg.MaxHistory), logs: nil}

	doChat(t, srv, `{"input_type":"text","text":"作圆","stream":false}`)
	if strings.Contains(stub.calls[0], "3D 模式") {
		t.Errorf("default mode should use 2D prompt; call=%q", stub.calls[0])
	}
}
