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

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal chat request: %w", err)
	}

	url := strings.TrimRight(c.cfg.Endpoint, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("chat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("chat backend %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode chat response: %w", err)
	}
	if parsed.Error != nil {
		return "", errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("chat backend returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
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
