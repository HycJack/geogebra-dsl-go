package ai

import (
	"context"
)

// GenerateRequest captures the input for one conversational generation pass.
type GenerateRequest struct {
	Session   *Session
	SystemMsg Message
	UserMsg   Message // the fresh user turn (text or image)
}

// Generate runs the bounded generate→gate→repair loop and returns a Result.
//
//   - First attempt: system + history + UserMsg.
//   - On failure, a RepairMessage carrying the last script + diagnostics is
//     appended and the loop retries up to cfg.MaxRepair times.
//   - On success the (last) valid script, executable order, and teaching note
//     are returned. If the cap is exhausted the best (final) script and its
//     unresolved diagnostics are returned with OK=false.
func Generate(ctx context.Context, client ChatClient, cfg Config, req GenerateRequest) *Result {
	prompt := append([]Message{req.SystemMsg}, req.Session.ContextMessages()...)

	lastScript := ""
	lastNote := ""
	var lastGate *GateResult
	attempts := 0

	for attempt := 1; attempt <= cfg.MaxRepair+1; attempt++ {
		attempts = attempt
		msgs := prompt
		if attempt > 1 {
			// Append the repair feedback for the previous failed script.
			msgs = append([]Message(nil), prompt...)
			msgs = append(msgs, RepairMessage(lastScript, lastGate.Diagnostics))
		} else {
			msgs = append([]Message(nil), prompt...)
			msgs = append(msgs, req.UserMsg)
		}

		as, err := generateOnce(ctx, client, cfg, msgs)
		if err != nil {
			// Transient/LLM errors are not script defects; treat as one failed
			// attempt and continue (so a flaky backend still gets retried).
			lastGate = &GateResult{OK: false}
			if attempt == cfg.MaxRepair+1 {
				break
			}
			continue
		}
		lastScript = as.Script
		lastNote = as.TeachingNote

		lastGate = runGate(as.Script)
		if lastGate.OK {
			return NewResult(lastScript, lastNote, lastGate, attempts)
		}
	}

	return NewResult(lastScript, lastNote, lastGate, attempts)
}
