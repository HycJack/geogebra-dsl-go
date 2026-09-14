package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// openAIClient is a concrete ChatClient speaking the OpenAI /chat/completions
// protocol. It uses only the standard library (net/http) and supports both text
// and inline base64 images.
type openAIClient struct {
	cfg    Config
	client *http.Client
}

// NewClient builds a ChatClient backed by an OpenAI-compatible endpoint.
func NewClient(cfg Config) ChatClient {
	return &openAIClient{
		cfg:    cfg,
		client: &http.Client{Timeout: time.Duration(cfg.HTTPTimeoutS) * time.Second},
	}
}

// chatRequest mirrors the subset of the chat/completions payload we send.
type chatRequest struct {
	Model       string       `json:"model"`
	Messages    []apiMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens"`
}

// apiMessage is a wire-level message: role plus a content array that may mix
// text and image_url parts, or a plain string for assistant history.
type apiMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// contentPart is a single wire-level content item.
type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

// chatResponse mirrors the chat/completions response we consume.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *openAIClient) Complete(ctx context.Context, messages []Message, opts CompleteOptions) (string, error) {
	req := chatRequest{
		Model:       c.cfg.Model,
		Messages:    make([]apiMessage, len(messages)),
		Temperature: opts.Temperature,
		MaxTokens:   opts.MaxTokens,
	}
	for i, m := range messages {
		req.Messages[i] = toAPIMessage(m)
	}

	// Build the wire body ONCE and reuse the exact same bytes on every retry, so
	// transient retries never alter or drop message content.
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal chat request: %w", err)
	}
	url := strings.TrimRight(c.cfg.Endpoint, "/") + "/chat/completions"

	n0 := time.Now()
	c.logRequest(messages, opts)

	maxAttempts := c.cfg.HTTPRetries + 1 // 1 initial + HTTPRetries retries
	for attempt := 1; ; attempt++ {
		reply, retryable, err := c.doComplete(ctx, url, body)
		if err == nil {
			c.logResponse(reply, n0)
			return reply, nil
		}
		// Non-transient failure (client error, malformed body, etc.) — stop now.
		if !retryable || attempt >= maxAttempts {
			return "", err
		}
		// Transient failure: exponential backoff (base, then doubles), still
		// within the caller's context.
		delay := time.Duration(c.cfg.HTTPRetryBase) * time.Millisecond * time.Duration(1<<uint(attempt-1))
		c.logRetry(attempt, delay, err)
		if err2 := sleepCtx(ctx, delay); err2 != nil {
			return "", err2
		}
	}
}

// doComplete performs a single raw attempt with the given (already-frozen)
// body. It returns the parsed reply, and retryable indicates whether the
// failure is transient (429/408/5xx or network error) and worth retrying.
// The body slice is intentionally re-serialized into a fresh reader each call.
func (c *openAIClient) doComplete(ctx context.Context, url string, body []byte) (string, bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", false, err // local build error: never retry
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		// Network errors (timeout, conn refused, TLS) are transient.
		return "", true, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := fmt.Errorf("chat backend %s: %s", resp.Status, strings.TrimSpace(string(msg)))
		return "", isRetryableStatus(resp.StatusCode), err
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", false, fmt.Errorf("decode chat response: %w", err) // not transient
	}
	if parsed.Error != nil {
		return "", false, errors.New(parsed.Error.Message) // backend business error
	}
	if len(parsed.Choices) == 0 {
		return "", false, errors.New("chat backend returned no choices")
	}
	return parsed.Choices[0].Message.Content, false, nil
}

// isRetryableStatus reports whether a non-200 status code is transient and
// worth an exponential-backoff retry: 408 (timeout), 409 (conflict), 429 (rate
// limit), and all 5xx (server failures). Other 4xx are permanent — retrying
// would not help.
func isRetryableStatus(code int) bool {
	switch {
	case code == http.StatusRequestTimeout, code == http.StatusTooManyRequests,
		code == http.StatusConflict:
		return true
	case code >= 500 && code <= 599:
		return true
	default:
		return false
	}
}

// sleepCtx waits for delay, aborting early if ctx is done.
func sleepCtx(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// logRequest emits a llm.request event with the outgoing parameters. Secrets
// (the API key) and large image payloads are never included.
func (c *openAIClient) logRequest(messages []Message, opts CompleteOptions) {
	c.logEntry("llm.request", map[string]any{
		"model":         c.cfg.Model,
		"endpoint":      strings.TrimRight(c.cfg.Endpoint, "/"),
		"temperature":   opts.Temperature,
		"max_tokens":    opts.MaxTokens,
		"messages":      summarizeMessages(messages),
		"message_count": len(messages),
	})
}

// logRetry emits a llm.retry event when a transient failure is about to be
// retried after a backoff delay.
func (c *openAIClient) logRetry(attempt int, delay time.Duration, err error) {
	c.logEntry("llm.retry", map[string]any{
		"attempt":  attempt, // 1-based number of the retry about to run
		"delay_ms": int(delay.Milliseconds()),
		"error":    err.Error(),
	})
}

// logResponse emits a llm.response event with the returned text (bounded preview).
func (c *openAIClient) logResponse(reply string, n0 time.Time) {
	took := time.Since(n0).Milliseconds()
	c.logEntry("llm.response", map[string]any{
		"latency_ms": took,
		"chars":      len(reply),
		"preview":    ellipsize(reply, 500),
	})
}

// logEntry is the single write point for this client's events.
func (c *openAIClient) logEntry(Event string, fields map[string]any) {
	c.cfg.logEvent(Event, fields)
}

// toAPIMessage converts a Message to its wire form. A single text-only message
// is emitted as a plain string (cheapest for history); image parts use the
// image_url array form.
func toAPIMessage(m Message) apiMessage {
	if len(m.Content) == 1 && m.Content[0].ImageB64 == "" {
		return apiMessage{Role: string(m.Role), Content: m.Content[0].Text}
	}
	parts := make([]contentPart, 0, len(m.Content))
	for _, p := range m.Content {
		switch {
		case p.ImageB64 != "":
			parts = append(parts, contentPart{
				Type: "image_url",
				ImageURL: &imageURL{
					URL: "data:" + mimeOr(p.ImageMIME) + ";base64," + p.ImageB64,
				},
			})
		default:
			parts = append(parts, contentPart{Type: "text", Text: p.Text})
		}
	}
	return apiMessage{Role: string(m.Role), Content: parts}
}

func mimeOr(m string) string {
	if m == "" {
		return "image/png"
	}
	return m
}
