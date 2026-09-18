// Package diag defines the error codes, per-problem diagnostics, and the
// check receipt produced by the validator. It is the single vocabulary used
// across every stage of the pipeline.
package diag

// Code is a stable, machine-readable error code. All codes are unique; the
// exit code mapping lives in the CLI.
type Code string

const (
	// Parsing / structure errors.
	CodeParseSyntax Code = "parse/syntax"
	CodeParseJSON   Code = "parse/json"
	CodeUsage       Code = "usage"

	// Command/signature errors (catalog + sig).
	CodeCmdUnknown Code = "cmd/unknown"
	CodeCmdArg     Code = "cmd/arg"

	// Dependency graph errors (build + deps).
	CodeDepCycle     Code = "dep/cycle"
	CodeDepRedefine  Code = "dep/redefine"
	CodeDepUndefined Code = "dep/undefined"

	// Geometric degeneracy (geo).
	CodeGeoDegenerate Code = "geo/degenerate"

	// Goals / reachability (reach).
	CodeGoalUnreachable Code = "goal/unreachable"
)

// Problem is one diagnostic: a stable code, a human-readable message, and the
// object id it refers to (empty when not object-specific).
type Problem struct {
	Code Code   `json:"code"`
	Msg  string `json:"msg"`
	Obj  string `json:"obj,omitempty"`
	Line int    `json:"line,omitempty"` // 1-based source line, 0 when unknown
}

// Error codes specs for locating a Problem in text output / tests.
func (c Code) String() string { return string(c) }

// Receipt is the full result of a check run.
type Receipt struct {
	OK         bool              `json:"ok"`         // true iff all checks passed
	Errors     []Problem         `json:"errors"`     // blocking problems (any makes OK=false)
	Warnings   []Problem         `json:"warnings"`   // non-blocking notes
	Executable []string          `json:"executable"` // object ids in topological (build) order
	Kinds      map[string]string `json:"kinds"`      // object id -> resolved coarse kind
	SourceIn   string            `json:"source_in"`  // "text" or "ir"
}

// NewReceipt builds an empty receipt. Warnings is initialized to an empty
// slice (rather than nil) so the documented `warnings[]` field always
// serializes as `[]`, not `null`; it is reserved for non-blocking notes and
// may remain empty when the validator has nothing to warn about. Kinds is
// likewise always present so hosts can read it without a nil check; it is
// empty when the input could not be turned into a graph.
func NewReceipt(source string) *Receipt {
	return &Receipt{SourceIn: source, Warnings: []Problem{}, Executable: []string{}, Kinds: map[string]string{}}
}

// Fail marks the receipt as failed (OK=false). Used by fail-closed stages.
func (r *Receipt) Fail(p Problem) {
	r.OK = false
	r.Errors = append(r.Errors, p)
}
