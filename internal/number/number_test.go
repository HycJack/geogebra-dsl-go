package number

import (
	"math/big"
	"testing"
)

func TestEvalConstants(t *testing.T) {
	pi, ok := Eval("pi")
	if !ok || pi.Sign() <= 0 {
		t.Fatalf("pi should be positive, got %v ok=%v", pi, ok)
	}
	f, _ := pi.Float64()
	if diff(f, 3.1415926535) > 1e-6 {
		t.Fatalf("pi approx wrong: %v", f)
	}
	e, ok := Eval("e")
	if !ok || e.Sign() <= 0 {
		t.Fatalf("e should be positive")
	}
	fe, _ := e.Float64()
	if diff(fe, 2.718281828) > 1e-6 {
		t.Fatalf("e approx wrong: %v", fe)
	}
}

func TestEvalNested(t *testing.T) {
	cases := map[string]string{ // expr -> float result
		"2*pi":       "6.2831853",
		"pi/2":       "1.5707963",
		"3 + pi":     "6.1415926",
		"(1+2)*3":    "9",
		"2^10":       "1024",
		"1/2 + 1/3":  "0.8333333",
		"-5":         "-5",
		"2*(pi+1)/3": "2.7610613",
	}
	for expr, want := range cases {
		v, ok := Eval(expr)
		if !ok {
			t.Errorf("expr %q should evaluate", expr)
			continue
		}
		f, _ := v.Float64()
		if diff(f, parseWant(t, want)) > 1e-4 {
			t.Errorf("expr %q = %v, want ~%v", expr, f, want)
		}
	}
}

func TestEvalZero(t *testing.T) {
	if !IsZero("0") {
		t.Error("0 should be zero")
	}
	if !IsZero("2 - 2") {
		t.Error("2-2 should be zero")
	}
	if IsZero("pi") {
		t.Error("pi should not be zero")
	}
	if !IsZero("1 - 1/1") {
		t.Error("1-1/1 should be zero")
	}
}

// TestEvalUnaryMinusPrecedence verifies that '^' binds tighter than a unary
// minus, so -2^2 == -(2^2) == -4 (as in standard math and GeoGebra) rather than
// (-2)^2 == 4. A negated base must be parenthesized: (-2)^2 == 4.
func TestEvalUnaryMinusPrecedence(t *testing.T) {
	cases := map[string]string{ // expr -> exact rational
		"-2^2":   "-4",
		"(-2)^2": "4",
		"-2^3":   "-8",
		"2^3^2":  "512", // right-associative: 2^(3^2)
		"-2*3^2": "-18",
		"-(2^2)": "-4",
		"1-2^2":  "-3",
		"4^0":    "1",
	}
	for expr, want := range cases {
		v, ok := Eval(expr)
		if !ok {
			t.Errorf("expr %q should evaluate", expr)
			continue
		}
		w, ok := new(big.Rat).SetString(want)
		if !ok {
			t.Fatalf("bad want %q", want)
		}
		if v.Cmp(w) != 0 {
			t.Errorf("expr %q = %v, want %s", expr, v, want)
		}
	}
}

// TestEvalHugeExponentBounded verifies an astronomically large exponent in
// untrusted input returns quickly (boundary cap) instead of looping unboundedly.
func TestEvalHugeExponentBounded(t *testing.T) {
	// 2^1000000000 would loop ~1e9 times if unbounded; the cap must short-circuit.
	if _, ok := Eval("2^1000000000"); !ok {
		t.Fatal("huge exponent should still be conservatively evaluable")
	}
	if _, ok := Eval("(-2)^1000000001"); !ok {
		t.Fatal("huge odd exponent of a negative base should be evaluable")
	}
}

func TestEvalUnsupported(t *testing.T) {
	for _, bad := range []string{"", "abc", "2+", "||||", "1/0", "i", "deg", "2^"} {
		if _, ok := Eval(bad); ok {
			t.Errorf("expr %q should NOT evaluate", bad)
		}
	}
	if _, ok := Eval("2^3+1"); !ok {
		t.Error("2^3+1 should evaluate")
	}
}

func parseWant(t *testing.T, s string) float64 {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		t.Fatalf("bad want %q", s)
	}
	f, _ := r.Float64()
	return f
}

func diff(a, b float64) float64 {
	if a < b {
		return b - a
	}
	return a - b
}

func TestKnownConstantSingleLetterCaseSensitive(t *testing.T) {
	// Lowercase single letters are the canonical constant spellings.
	for _, name := range []string{"e", "i"} {
		if !KnownConstant(name) {
			t.Errorf("KnownConstant(%q) = false, want true", name)
		}
	}
	// Uppercase single letters are object names, not constants: GeoGebra
	// auto-names points A, B, C, D, E, F…, so E and I are the everyday letters
	// for a fifth vertex and an incenter. Treating them as constants made an
	// undefined reference disappear silently — Polygon(A, B, C, E) validated as
	// if E were 2.718 instead of reporting that E is not defined.
	for _, name := range []string{"E", "I"} {
		if KnownConstant(name) {
			t.Errorf("KnownConstant(%q) = true, want false", name)
		}
	}
	// Multi-letter spellings stay case-insensitive: PI / Pi / Euler / Gamma are
	// unambiguous constant forms.
	for _, name := range []string{"pi", "PI", "Pi", "e", "Euler", "EULER", "Gamma", "GAMMA", "deg"} {
		if !KnownConstant(name) {
			t.Errorf("KnownConstant(%q) = false, want true", name)
		}
	}
}
