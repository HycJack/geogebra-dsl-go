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
	// lastFallback holds the most recent no-script reply that carried meaningful
	// text (a degraded answer). If the whole loop ends without a buildable
	// script, this text is returned so pure-calculation answers are shown rather
	// than reported as a bare "no <gg> script block" failure.
	lastFallback := ""
	var lastGate *GateResult // nil until the first script has been extracted
	producedAny := false
	attempts := 0
	trace := newStepTrace()

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

		as, err := generateOnce(ctx, client, cfg, msgs, attempt, trace)
		if err != nil {
			// Transient: Complete already retried with backoff and still failed,
			// or the reply was genuinely empty. Do NOT clobber lastScript/lastGate
			// — keep the previous real content so a subsequent repair round builds
			// on it.
			//
			// The raw HTTP/network failure was already recorded as an error step
			// by client.Complete. Only add one here when this attempt's failure
			// wasn't captured there (e.g. extraction failed after a successful
			// LLM call, or the client had no trace wired), to avoid duplicate
			// error rows in the UI.
			if !trace.hasError(attempt) {
				trace.add(Step{
					Stage:   "error",
					Attempt: attempt,
					Error:   err.Error(),
				})
			}
			cfg.logEvent("generate.attempt.error", map[string]any{
				"attempt": attempt,
				"error":   err.Error(),
			})
			if attempt == cfg.MaxRepair+1 {
				break
			}
			continue
		}

		if as.Degraded() {
			// No <gg> block but a text answer came back (e.g. a pure-calculation
			// problem with nothing to construct). Keep it as the fallback answer
			// and let the loop retry in case a later attempt produces a real
			// figure. Never record this as an error — the UI should present the
			// model's answer at the end instead of reporting a failure. The LLM
			// call itself is already in the trace via the llm.* steps.
			lastFallback = as.Fallback
			if attempt == cfg.MaxRepair+1 {
				break
			}
			continue
		}

		producedAny = true
		lastScript = as.Script
		lastNote = as.TeachingNote

		lastGate = runGate(as.Script)
		ok := lastGate.OK
		trace.add(Step{
			Stage:       "gate",
			Attempt:     attempt,
			Script:      as.Script,
			GateOK:      &ok,
			Executable:  lastGate.Executable,
			Diagnostics: lastGate.Diagnostics,
		})
		cfg.logEvent("generate.attempt.done", map[string]any{
			"attempt":       attempt,
			"gate_ok":       lastGate.OK,
			"script":        as.Script,
			"executables":   lastGate.Executable,
			"diagnostics":   lastGate.Diagnostics,
			"teaching_note": as.TeachingNote,
		})
		if lastGate.OK {
			return NewResult(lastScript, lastNote, lastGate, attempts, trace)
		}
	}

	// If nothing was ever produced (even the initial call failed), fall back to
	// an empty gate so the Result is well-formed and diagnostics reflect it.
	if lastGate == nil {
		lastGate = &GateResult{OK: false}
	}
	// End of loop with no buildable script: degrade to the last text answer if
	// the model produced one, so pure-calculation/explanation replies are shown
	// instead of a bare "no <gg> script block" failure.
	if lastScript == "" && lastFallback != "" {
		return &Result{
			OK:          false,
			Fallback:    lastFallback,
			Attempts:    attempts,
			Trace:       trace.Steps(),
			Diagnostics: []string{"模型未给出可构造的 <gg> 脚本，以下为其文字解答。"},
		}
	}
	return NewResult(lastScript, lastNote, lastGate, attempts, trace)
}
