package ai

import (
	"strings"
	"testing"

	"github.com/hycjack/geogebra-dsl-go/internal/diag"
)

func TestGateOKScript(t *testing.T) {
	script := "A = (0, 2)\nB = (4, 2)\nl = Line(A, B)"
	r := runGate(script)
	if !r.OK {
		t.Fatalf("expected OK, diagnostics=%v", r.Diagnostics)
	}
	if len(r.Executable) != 3 {
		t.Fatalf("expected 3 executable, got %v", r.Executable)
	}
}

func TestGateDetectsUndefinedRef(t *testing.T) {
	r := runGate("l = Line(A, Missing)")
	if r.OK {
		t.Fatal("expected failure")
	}
	found := false
	for _, d := range r.Diagnostics {
		if strings.Contains(d, "dep/undefined") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dep/undefined diagnostic, got %v", r.Diagnostics)
	}
	// The repair hint for dep/undefined should be present.
	if !strings.Contains(r.Diagnostics[0], "先定义被引用对象") {
		t.Errorf("expected repair hint, got %q", r.Diagnostics[0])
	}
}

func TestFormatProblemEmptyObj(t *testing.T) {
	s := formatProblem(diag.Problem{Code: diag.CodeParseSyntax, Msg: "行无法解析", Obj: "l"})
	if !strings.Contains(s, "[parse/syntax]") {
		t.Errorf("missing code: %q", s)
	}
	if !strings.Contains(s, "（对象 l）") {
		t.Errorf("missing object: %q", s)
	}
}

func TestNormalizeScript(t *testing.T) {
	in := []string{"A = (0, 2)", "", "   ", "B = (4, 2)"}
	out := normalizeScript(in)
	if out != "A = (0, 2)\nB = (4, 2)" {
		t.Fatalf("normalize mismatch: %q", out)
	}
}
