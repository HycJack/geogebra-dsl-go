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

	n0 := time.Now()
	c.logRequest(messages, opts)

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal chat request: %w", err)
	}

	url := strings.TrimRight(c.cfg.Endpoint, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		c.logError("llm.error", "build_request", n0, err)
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		c.logError("llm.error", "http", n0, err)
		return "", fmt.Errorf("chat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		c.logError("llm.error", "status", n0, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(msg))))
		return "", fmt.Errorf("chat backend %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		c.logError("llm.error", "decode", n0, err)
		return "", fmt.Errorf("decode chat response: %w", err)
	}
	if parsed.Error != nil {
		c.logError("llm.error", "backend", n0, errors.New(parsed.Error.Message))
		return "", errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		c.logError("llm.error", "no_choices", n0, errors.New("backend returned no choices"))
		return "", errors.New("chat backend returned no choices")
	}

	reply := parsed.Choices[0].Message.Content
	c.logResponse(reply, n0)
	return reply, nil
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

// logResponse emits a llm.response event with the returned text (bounded preview).
func (c *openAIClient) logResponse(reply string, n0 time.Time) {
	took := time.Since(n0).Milliseconds()
	c.logEntry("llm.response", map[string]any{
		"latency_ms": took,
		"chars":      len(reply),
		"preview":    ellipsize(reply, 500),
	})
}

// logError emits a llm.error event with the failure and latency.
func (c *openAIClient) logError(Event, stage string, n0 time.Time, err error) {
	c.logEntry(Event, map[string]any{
		"stage":      stage,
		"latency_ms": time.Since(n0).Milliseconds(),
		"error":      err.Error(),
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
