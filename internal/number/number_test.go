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
