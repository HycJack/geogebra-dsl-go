package ai

import "context"

// Role identifies a chat message participant.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ContentPart is one piece of a message payload: plain text or an inline image.
type ContentPart struct {
	Text      string `json:"text,omitempty"`       // used for text parts (and as the alt/guide for images)
	ImageB64  string `json:"image_b64,omitempty"`  // base64-encoded bytes
	ImageMIME string `json:"image_mime,omitempty"` // e.g. "image/png"
}

// Message is one turn in the conversation sent to the chat backend.
type Message struct {
	Role    Role
	Content []ContentPart
}

// CompleteOptions carries per-call settings that the service layer wants to
// pass through to the backend (temperature, max tokens, streaming, etc.).
type CompleteOptions struct {
	Temperature float64
	MaxTokens   int
	// Attempt is the 1-based index of the generation attempt this call belongs
	// to. It is used only to annotate trace steps and is ignored when Trace is
	// nil.
	Attempt int
	// Trace, if non-nil, receives a Step for the LLM call and for every
	// exponential-backoff HTTP retry within it. It is optional and nil-safe, so
	// a nil options.Trace leaves the client's behavior unchanged.
	Trace *StepTrace
}

// ChatClient abstracts the OpenAI-compatible backend so the repair loop can run
// against a real endpoint or against a test stub without touching the network.
type ChatClient interface {
	// Complete performs one chat/completions round-trip and returns the
	// assistant's full text reply. Streaming is an implementation detail of the
	// concrete client; this interface always yields the assembled final text.
	Complete(ctx context.Context, messages []Message, opts CompleteOptions) (string, error)
}
