package ai

import (
	"context"
	"errors"
	"strings"
)

// ErrNoScript signals that a model reply contained no extractable <gg> block.
var ErrNoScript = errors.New("model reply contained no <gg> script block")

// generateOnce performs a single LLM round-trip and extracts the script. The
// bundle of messages is the complete prompt for this attempt (system + history
// + the user/repair turn). It does NOT validate — the repair loop owns gating.
func generateOnce(ctx context.Context, c ChatClient, cfg Config, msgs []Message) (*AssistantScript, error) {
	reply, err := c.Complete(ctx, msgs, CompleteOptions{
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
	})
	if err != nil {
		return nil, err
	}
	as, ok := extractScript(reply)
	if !ok {
		return nil, ErrNoScript
	}
	as.Script = normalizeScript(strings.Split(as.Script, "\n"))
	return &as, nil
}
