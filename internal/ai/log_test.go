package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// captureLogFunc collects emitted events for assertions.
type captureLogFunc struct {
	events map[string][]map[string]any
	order  []string
}

func (c *captureLogFunc) emit(Event string, fields map[string]any) {
	if c.events == nil {
		c.events = map[string][]map[string]any{}
	}
	c.events[Event] = append(c.events[Event], fields)
	c.order = append(c.order, Event)
}

func TestCompleteEmitsRequestAndResponse(t *testing.T) {
	stub := &stubClient{replies: []string{goodReply("A = (0, 2)")}}
	cap := &captureLogFunc{}
	cfg := Config{
		Temperature: 0.2, MaxTokens: 2048, MaxRepair: 1,
		Endpoint: "https://example.test/v1", Model: "glm-5.2",
		MaxHistory: 20,
		Log:        cap.emit,
	}

	// Generate path must emit attempt events regardless of the client.
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s-log-1", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("画点"),
	})
	if !res.OK {
		t.Fatalf("expected OK, got %+v", res)
	}
	if len(cap.events["generate.attempt.start"]) != 1 {
		t.Errorf("expected 1 generate.attempt.start, got %d", len(cap.events["generate.attempt.start"]))
	}
	if len(cap.events["generate.attempt.done"]) != 1 {
		t.Errorf("expected 1 generate.attempt.done, got %d", len(cap.events["generate.attempt.done"]))
	}
	done := cap.events["generate.attempt.done"][0]
	if done["gate_ok"] != true {
		t.Errorf("expected gate_ok=true in attempt.done, got %#v", done["gate_ok"])
	}
	if _, ok := done["script"]; !ok {
		t.Error("attempt.done missing script field")
	}
}

func TestLogFuncNilIsNoop(t *testing.T) {
	// Config without Log must not panic through the whole flow.
	script := "A = Point(0, 0)\nB = Point(4, 0)\nl = Line(A, B)"
	stub := &stubClient{replies: []string{goodReply(script)}}
	cfg := Config{Temperature: 0.2, MaxTokens: 2048, MaxRepair: 1, MaxHistory: 20}
	res := Generate(context.Background(), stub, cfg, GenerateRequest{
		Session:   NewSession("s-log-nil", 20),
		SystemMsg: SystemMessage(),
		UserMsg:   TextUserMessage("作线段"),
	})
	if !res.OK {
		t.Fatalf("expected OK, got %+v", res)
	}
}

func TestSummarizeMessagesOmitsImagePayload(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: []ContentPart{{Text: "sys"}}},
		{Role: RoleUser, Content: []ContentPart{{ImageB64: "UU1WRw==", Text: "看图"}}},
	}
	sum := summarizeMessages(msgs)
	if len(sum) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(sum))
	}
	if sum[1]["images"] != 1 {
		t.Errorf("expected images=1, got %#v", sum[1])
	}
	if imgs := sum[0]["images"]; imgs != nil {
		t.Errorf("expected no images on text-only message, got %#v", imgs)
	}
	// Must never include the raw base64 payload.
	b, _ := json.Marshal(sum)
	if strings.Contains(string(b), "UU1WRw==") {
		t.Error("image base64 payload leaked into summary")
	}
}
