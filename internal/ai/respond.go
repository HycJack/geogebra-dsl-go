package ai

// Result is the complete outcome of one conversational generation attempt, ready
// to be serialized for the HTTP layer (both streaming lines and final JSON).
type Result struct {
	OK           bool     `json:"ok"`
	Script       string   `json:"script"`
	Executable   []string `json:"executable,omitempty"`
	TeachingNote string   `json:"teaching_note,omitempty"`
	Diagnostics  []string `json:"diagnostics,omitempty"`
	Attempts     int      `json:"attempts"` // number of generation round-trips, including the first
}

// NewResult assembles a Result from a gated, verified script and its gate
// outcome plus the attempt counter and teaching note.
func NewResult(script, note string, attrs *GateResult, attempts int) *Result {
	r := &Result{
		Script:       script,
		TeachingNote: note,
		Executable:   attrs.Executable,
		Attempts:     attempts,
	}
	if attrs.OK {
		r.OK = true
	} else {
		r.OK = false
		r.Diagnostics = attrs.Diagnostics
	}
	return r
}
