package main

import (
	"testing"

	"github.com/hycjack/geogebra-dsl-go/internal/diag"
)

// receiptWith returns a failed receipt carrying the given error codes.
func receiptWith(codes ...diag.Code) *diag.Receipt {
	rc := diag.NewReceipt("text")
	for _, c := range codes {
		rc.Fail(diag.Problem{Code: c, Msg: "diagnostic"})
	}
	return rc
}

func TestExitCodeOK(t *testing.T) {
	rc := diag.NewReceipt("text")
	rc.OK = true
	if got := exitCode(rc); got != 0 {
		t.Fatalf("exitCode(ok) = %d, want 0", got)
	}
}

func TestExitCodeConstructNotBuildable(t *testing.T) {
	// Construct-level diagnostics must exit 1.
	for _, c := range []diag.Code{
		diag.CodeCmdUnknown, diag.CodeCmdArg,
		diag.CodeDepCycle, diag.CodeDepRedefine, diag.CodeDepUndefined,
		diag.CodeGeoDegenerate, diag.CodeGoalUnreachable,
	} {
		rc := receiptWith(c)
		if got := exitCode(rc); got != 1 {
			t.Errorf("exitCode(%s) = %d, want 1", c, got)
		}
	}
}

func TestExitCodeUsageOrInputError(t *testing.T) {
	// Usage / structural input errors must exit 2.
	for _, c := range []diag.Code{
		diag.CodeUsage, diag.CodeParseSyntax, diag.CodeParseJSON,
	} {
		rc := receiptWith(c)
		if got := exitCode(rc); got != 2 {
			t.Errorf("exitCode(%s) = %d, want 2", c, got)
		}
	}
}

func TestExitCodeMixedPrefersInputError(t *testing.T) {
	// Mixed structural and construct errors: input/usage errors dominate → 2.
	rc := receiptWith(diag.CodeDepCycle, diag.CodeParseJSON)
	if got := exitCode(rc); got != 2 {
		t.Fatalf("exitCode(mixed) = %d, want 2", got)
	}
}
