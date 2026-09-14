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
//   - On a validation failure, a RepairMessage carrying the last script +
//     diagnostics is appended and the loop retries up to cfg.MaxRepair times.
//   - On success the (last) valid script, executable order, and teaching note
//     are returned. If the cap is exhausted the best (final) script and its
//     unresolved diagnostics are returned with OK=false.
//
// A transient error from client.Complete (the HTTP layer already did its
// exponential-backoff retries and still failed) must NOT clobber lastScript or
// lastGate: a later repair round still builds on the previous real script and
// its diagnostics, so no content is lost to a network hiccup mid-repair.
func Generate(ctx context.Context, client ChatClient, cfg Config, req GenerateRequest) *Result {
	prompt := append([]Message{req.SystemMsg}, req.Session.ContextMessages()...)

	// lastScript/lastGate reflect the most recent script that was actually
	// extracted and validated. They are updated only on a successful
	// generateOnce, never on a transient error.
	lastScript := ""
	lastNote := ""
	var lastGate *GateResult // nil until the first script has been extracted
	producedAny := false
	attempts := 0

	for attempt := 1; attempt <= cfg.MaxRepair+1; attempt++ {
		attempts = attempt

		var msgs []Message
		if attempt == 1 || !producedAny {
			// First attempt, or the prior attempt never produced usable content
			// (e.g. a network failure with no script to repair): send the plain
			// user turn again rather than a misleading "repair the empty script".
			msgs = append([]Message(nil), prompt...)
			msgs = append(msgs, req.UserMsg)
		} else {
			// Repair round: build on the previous real script + its diagnostics.
			msgs = append([]Message(nil), prompt...)
			msgs = append(msgs, RepairMessage(lastScript, lastGate.Diagnostics))
		}

		cfg.logEvent("generate.attempt.start", map[string]any{
			"attempt": attempt,
			"retry":   producedAny,
		})

		as, err := generateOnce(ctx, client, cfg, msgs)
		if err != nil {
			// Transient: Complete already retried with backoff and still failed.
			// Do not clobber lastScript/lastGate — keep the previous real content
			// so a subsequent repair round builds on it.
			cfg.logEvent("generate.attempt.error", map[string]any{
				"attempt": attempt,
				"error":   err.Error(),
			})
			if attempt == cfg.MaxRepair+1 {
				break
			}
			continue
		}
		producedAny = true
		lastScript = as.Script
		lastNote = as.TeachingNote

		lastGate = runGate(as.Script)
		cfg.logEvent("generate.attempt.done", map[string]any{
			"attempt":       attempt,
			"gate_ok":       lastGate.OK,
			"script":        as.Script,
			"executables":   lastGate.Executable,
			"diagnostics":   lastGate.Diagnostics,
			"teaching_note": as.TeachingNote,
		})
		if lastGate.OK {
			return NewResult(lastScript, lastNote, lastGate, attempts)
		}
	}

	// If nothing was ever produced (even the initial call failed), fall back to
	// an empty gate so the Result is well-formed and diagnostics reflect it.
	if lastGate == nil {
		lastGate = &GateResult{OK: false}
	}
	return NewResult(lastScript, lastNote, lastGate, attempts)
}
