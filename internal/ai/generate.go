package ai

import (
	"context"
	"errors"
	"strings"
)

// ErrNoScript signals that a model reply contained no extractable <gg> block
// AND carried no meaningful text to surface. A reply that is merely text (e.g.
// a pure calculation answer with no construction) does NOT raise this — it is
// returned as a degraded AssistantScript so the caller can show the answer.
var ErrNoScript = errors.New("model reply contained no <gg> script block")

// generateOnce performs a single LLM round-trip and extracts the script. The
// bundle of messages is the complete prompt for this attempt (system + history
// + the user/repair turn). It does NOT validate — the repair loop owns gating.
// The trace and attempt index are forwarded to the client so the UI can render
// the LLM call and any HTTP retries for this specific round.
//
// When the reply contains no <gg> block but does contain meaningful text, a
// degraded AssistantScript (Fallback set, Degraded()==true) is returned instead
// of an error, so non-construction answers degrade to readable text at the end
// of the repair loop rather than hard-failing every attempt.
func generateOnce(ctx context.Context, c ChatClient, cfg Config, msgs []Message, attempt int, tr *StepTrace) (*AssistantScript, error) {
	reply, err := c.Complete(ctx, msgs, CompleteOptions{
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxTokens,
		Attempt:     attempt,
		Trace:       tr,
	})
	if err != nil {
		return nil, err
	}
	as, ok := extractScript(reply)
	if !ok {
		if strings.TrimSpace(reply) == "" {
			return nil, ErrNoScript
		}
		// No <gg> block but there is an answer worth keeping: degrade to text.
		return &AssistantScript{Fallback: strings.TrimSpace(reply)}, nil
	}
	as.Script = normalizeScript(strings.Split(as.Script, "\n"))
	return &as, nil
}
