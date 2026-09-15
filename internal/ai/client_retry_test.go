package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedRT returns queued (statusCode, body) responses in sequence. It also
// captures every request body it saw so tests can assert retries reuse content.
type scriptedRT struct {
	mu       sync.Mutex
	statuses []int
	bodies   []string
	seen     []string // raw request bodies, in order
}

func (s *scriptedRT) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	s.mu.Lock()
	s.seen = append(s.seen, string(body))
	var code int
	if len(s.statuses) > 0 {
		code = s.statuses[0]
		s.statuses = s.statuses[1:]
	} else {
		code = http.StatusOK
	}
	s.mu.Unlock()
	return &http.Response{
		StatusCode: code,
		Status:     http.StatusText(code),
		Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"OK"}}]}`)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func testClient(cfg Config, rt http.RoundTripper) *openAIClient {
	return &openAIClient{cfg: cfg, client: &http.Client{Transport: rt, Timeout: 5 * time.Second}}
}

func msgs() []Message {
	return []Message{
		{Role: RoleSystem, Content: []ContentPart{{Text: "sys-keep"}}},
		{Role: RoleUser, Content: []ContentPart{{Text: "user-keep"}}},
	}
}

func TestRetryTransientThenSucceeds(t *testing.T) {
	// 2 transient 500s, then success. Base backoff is tiny so the test is fast.
	rt := &scriptedRT{statuses: []int{500, 500}}
	cfg := Config{Endpoint: "http://x/v1", Model: "m", APIKey: "k", HTTPRetries: 5, HTTPRetryBase: 1}
	cl := testClient(cfg, rt)

	out, err := cl.Complete(context.Background(), msgs(), CompleteOptions{MaxTokens: 8})
	if err != nil {
		t.Fatalf("expected eventual success, got err: %v", err)
	}
	if out != "OK" {
		t.Fatalf("reply=%q, want OK", out)
	}
	// 1 initial + 2 retries = 3 requests; bodies must be byte-identical.
	if len(rt.seen) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(rt.seen))
	}
	for i := 1; i < len(rt.seen); i++ {
		if rt.seen[i] != rt.seen[0] {
			t.Errorf("retry %d body differs: got %q want %q", i, rt.seen[i], rt.seen[0])
		}
	}
	if !strings.Contains(rt.seen[0], "sys-keep") || !strings.Contains(rt.seen[0], "user-keep") {
		t.Errorf("message content lost in body: %q", rt.seen[0])
	}
}

func TestRetryExhaustsThenFails(t *testing.T) {
	// Every attempt returns 429 (retryable) until the retry budget is gone.
	rt := &scriptedRT{statuses: []int{429, 429, 429, 429, 429, 429}}
	cfg := Config{Endpoint: "http://x/v1", Model: "m", HTTPRetries: 5, HTTPRetryBase: 1}
	cl := testClient(cfg, rt)

	_, err := cl.Complete(context.Background(), msgs(), CompleteOptions{MaxTokens: 8})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !strings.Contains(err.Error(), "Too Many Requests") && !strings.Contains(err.Error(), "429") {
		t.Errorf("expected 429 in error, got: %v", err)
	}
	// 1 initial + 5 retries.
	if len(rt.seen) != 6 {
		t.Fatalf("expected 6 attempts (1+5), got %d", len(rt.seen))
	}
}

func TestRetryNonTransientStopsImmediately(t *testing.T) {
	// 404 (permanent) must NOT be retried.
	rt := &scriptedRT{statuses: []int{404}}
	cfg := Config{Endpoint: "http://x/v1", Model: "m", HTTPRetries: 5, HTTPRetryBase: 1}
	cl := testClient(cfg, rt)

	_, err := cl.Complete(context.Background(), msgs(), CompleteOptions{MaxTokens: 8})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(rt.seen) != 1 {
		t.Fatalf("expected no retry on 404, got %d attempts", len(rt.seen))
	}
}

func TestRetryZeroDisablesBackoff(t *testing.T) {
	// HTTPRetries=0 → still attempts once, no retry.
	rt := &scriptedRT{statuses: []int{500}}
	cfg := Config{Endpoint: "http://x/v1", Model: "m", HTTPRetries: 0, HTTPRetryBase: 1}
	cl := testClient(cfg, rt)
	_, err := cl.Complete(context.Background(), msgs(), CompleteOptions{MaxTokens: 8})
	if err == nil {
		t.Fatal("expected error with retries=0")
	}
	if len(rt.seen) != 1 {
		t.Fatalf("retries=0 should attempt once, got %d", len(rt.seen))
	}
}

// TestTraceRecordsClientRetries proves the explicit StepTrace attached via
// CompleteOptions captures each backoff retry and the final successful LLM call,
// so the chat UI can render the raw HTTP retry process.
func TestTraceRecordsClientRetries(t *testing.T) {
	rt := &scriptedRT{statuses: []int{503, 429, 200}}
	cfg := Config{Endpoint: "http://x/v1", Model: "m", HTTPRetries: 5, HTTPRetryBase: 1}
	cl := testClient(cfg, rt)
	trace := newStepTrace()

	if _, err := cl.Complete(context.Background(), msgs(), CompleteOptions{MaxTokens: 8, Attempt: 2, Trace: trace}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var retries []Step
	var llms []Step
	for _, s := range trace.Steps() {
		switch s.Stage {
		case "retry":
			retries = append(retries, s)
		case "llm":
			llms = append(llms, s)
		}
	}
	if len(retries) != 2 {
		t.Fatalf("expected 2 retry steps, got %d", len(retries))
	}
	if retries[0].Attempt != 2 || retries[0].Retry != 1 || retries[0].DelayMS < 1 {
		t.Errorf("bad first retry step: %+v", retries[0])
	}
	if len(llms) != 1 || llms[0].Attempt != 2 || llms[0].LatencyMS < 0 {
		t.Errorf("bad llm step: %+v", llms)
	}
	// Backoff doubles: second retry delay should be ~2x the first.
	if retries[1].DelayMS != 2*retries[0].DelayMS {
		t.Errorf("retry %d delay=%d, want 2x first=%d", retries[1].Retry, retries[1].DelayMS, retries[0].DelayMS)
	}
}

// TestTraceRecordsErrorOnFailure ensures a non-transient final failure emits an
// error step on the trace.
func TestTraceRecordsErrorOnFailure(t *testing.T) {
	rt := &scriptedRT{statuses: []int{404}}
	cfg := Config{Endpoint: "http://x/v1", Model: "m", HTTPRetries: 5, HTTPRetryBase: 1}
	cl := testClient(cfg, rt)
	trace := newStepTrace()
	if _, err := cl.Complete(context.Background(), msgs(), CompleteOptions{MaxTokens: 8, Attempt: 1, Trace: trace}); err == nil {
		t.Fatal("expected error on 404")
	}
	var got bool
	for _, s := range trace.Steps() {
		if s.Stage == "error" && s.Attempt == 1 && s.Error != "" {
			got = true
		}
	}
	if !got {
		t.Error("expected an error step for the 404 failure")
	}
}

// TestRetryBodyIsValidJSON proves the frozen body is still valid OpenAI JSON on
// every retry (messages preserved, not corrupted by re-wrapping).
func TestRetryBodyIsValidJSON(t *testing.T) {
	rt := &scriptedRT{statuses: []int{503, 503}}
	cfg := Config{Endpoint: "http://x/v1", Model: "m", HTTPRetries: 5, HTTPRetryBase: 1}
	cl := testClient(cfg, rt)
	if _, err := cl.Complete(context.Background(), msgs(), CompleteOptions{Temperature: 0.5, MaxTokens: 9}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, raw := range rt.seen {
		var p struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Temperature float64 `json:"temperature"`
			MaxTokens   int     `json:"max_tokens"`
		}
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("attempt %d body is invalid JSON: %v (raw=%q)", i, err, raw)
		}
		if len(p.Messages) != 2 || p.Messages[0].Role != "system" || p.Messages[1].Role != "user" {
			t.Fatalf("attempt %d messages not preserved: %+v", i, p.Messages)
		}
	}
}

func TestCappedMaxTokens(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{16384, 16384}, // reasonable value passes through
		{128000, maxTokensCeiling},
		{100000, maxTokensCeiling},
		{0, maxTokensCeiling},
		{-5, maxTokensCeiling},
	}
	for _, c := range cases {
		if got := cappedMaxTokens(c.in); got != c.want {
			t.Errorf("cappedMaxTokens(%d)=%d, want %d", c.in, got, c.want)
		}
	}
}
