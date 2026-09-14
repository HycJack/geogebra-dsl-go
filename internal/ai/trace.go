package ai

// Step describes one recorded stage of a generation pass. It is surfaced in the
// web UI so the chat can visualize each LLM call, every exponential-backoff HTTP
// retry, each ggcm gate run, and any transient failure. A Step is immutable
// after creation.
type Step struct {
	Stage       string   `json:"stage"`                 // "llm" | "retry" | "gate" | "error"
	Attempt     int      `json:"attempt"`               // 1-based generation attempt index
	Script      string   `json:"script,omitempty"`      // extracted script (gate stage)
	GateOK      *bool    `json:"gate_ok,omitempty"`     // nil unless a gate ran
	Executable  []string `json:"executable,omitempty"`  // objects built (gate stage)
	Diagnostics []string `json:"diagnostics,omitempty"` // unresolved issues (gate stage)
	Retry       int      `json:"retry,omitempty"`       // retry ordinal within one LLM call
	DelayMS     int      `json:"delay_ms,omitempty"`    // backoff delay of a retry (ms)
	LatencyMS   int      `json:"latency_ms,omitempty"`  // LLM round-trip duration (ms)
	Error       string   `json:"error,omitempty"`       // error text
	Reply       string   `json:"reply,omitempty"`       // LLM reply preview (llm stage)
}

// StepTrace collects Steps for one Generate call. It is single-goroutine by
// design: the repair loop runs synchronously, so no locking is needed.
type StepTrace struct {
	steps []Step
}

// newStepTrace returns an empty, ready-to-use trace.
func newStepTrace() *StepTrace { return &StepTrace{} }

// add appends a step. It is nil-safe (a nil trace silently ignores writes) so
// callers that are unsure whether a trace is configured can pass it through.
func (t *StepTrace) add(s Step) {
	if t == nil {
		return
	}
	t.steps = append(t.steps, s)
}

// hasError reports whether an error step was already recorded for the given
// generation attempt. It lets the repair loop avoid recording a duplicate error
// when the client (Complete) already captured the raw failure.
func (t *StepTrace) hasError(attempt int) bool {
	if t == nil {
		return false
	}
	for _, s := range t.steps {
		if s.Stage == "error" && s.Attempt == attempt {
			return true
		}
	}
	return false
}

// Steps returns a copy of the recorded steps. It is nil-safe.
func (t *StepTrace) Steps() []Step {
	if t == nil {
		return nil
	}
	out := make([]Step, len(t.steps))
	copy(out, t.steps)
	return out
}
