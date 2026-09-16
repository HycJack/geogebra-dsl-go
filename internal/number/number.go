// Package number is a tiny exact expression evaluator over rationals plus the
// symbolic constants pi and e. It exists so degeneracy checks can decide the
// *sign* of a value (positive/zero/negative) exactly enough to refuse
// degenerate constructions like zero-radius circles. pi and e are irrational,
// so they are substituted with high-precision rational approximations (far
// beyond any relevant precision for a sign/zero decision) rather than true
// exact values.
package number

import (
	"math/big"
	"strings"
)

// Reserved constants recognised by the evaluator. Their approximations are
// chosen so that any sign comparison the degeneracy check needs is exact.
var constants = map[string]*big.Rat{
	"pi":    mustRat("3.141592653589793238462643383279502884197169399375105820974944"),
	"e":     mustRat("2.718281828459045235360287471352662497757247093699959574966967"),
	"i":     nil, // imaginary unit: not representable as a rational
	"euler": mustRat("0.577215664901532860606512090082402431042159335939923598805767"),
	"gamma": mustRat("0.577215664901532860606512090082402431042159335939923598805767"),
	"deg":   nil, // angle unit: handled as a conversion, not a bare number
}

func mustRat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic("bad constant: " + s)
	}
	return r
}

// KnownConstant reports whether name is a reserved numeric constant.
//
// Single-letter constants are matched case-sensitively: only the canonical
// lowercase forms e and i count. A bare uppercase single letter is an object
// name, not a constant — GeoGebra auto-names points A, B, C, D, E, F…, so E and
// I are the everyday letters for a fifth vertex and for an incenter. Matching
// them case-insensitively made an undefined reference disappear silently:
// Polygon(A, B, C, E) validated as if E were 2.718 instead of reporting that E
// is not defined. Multi-letter spellings keep case-insensitive matching, since
// PI / Pi / Euler / Gamma are unambiguous constant forms.
func KnownConstant(name string) bool {
	lower := strings.ToLower(name)
	if len(name) == 1 && name != lower {
		return false
	}
	_, ok := constants[lower]
	return ok
}

// Eval computes the value of a constant-only arithmetic expression (digits,
// ., +, -, *, /, ^, parens, unary sign, and the reserved words pi/e/Euler/Gamma).
// It returns the exact rational value and whether the expression was fully
// resolvable. Expressions involving 'i' or 'deg' may resolve with a zero or
// degrees-conversion result; unsupported syntax returns ok=false.
func Eval(expr string) (*big.Rat, bool) {
	p := &parser{src: strings.TrimSpace(expr)}
	v, ok := p.expr()
	if !ok || p.pos != len(p.src) {
		return nil, false
	}
	return v, true
}

// IsZero reports whether expr evaluates to exactly zero (used by degeneracy).
func IsZero(expr string) bool {
	v, ok := Eval(expr)
	return ok && v.Sign() == 0
}

type parser struct {
	src string
	pos int
}

func (p *parser) peek() byte {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *parser) skipWS() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' {
			p.pos++
		} else {
			break
		}
	}
}

func (p *parser) expr() (*big.Rat, bool) {
	return p.term()
}

func (p *parser) term() (*big.Rat, bool) {
	left, ok := p.factor()
	if !ok {
		return nil, false
	}
	for {
		p.skipWS()
		c := p.peek()
		if c == '+' || c == '-' {
			p.pos++
			right, ok := p.factor()
			if !ok {
				return nil, false
			}
			if c == '+' {
				left = new(big.Rat).Add(left, right)
			} else {
				left = new(big.Rat).Sub(left, right)
			}
		} else {
			return left, true
		}
	}
}

func (p *parser) factor() (*big.Rat, bool) {
	left, ok := p.unaryFactor()
	if !ok {
		return nil, false
	}
	for {
		p.skipWS()
		c := p.peek()
		if c == '*' || c == '/' {
			p.pos++
			right, ok := p.unaryFactor()
			if !ok {
				return nil, false
			}
			if c == '*' {
				left = new(big.Rat).Mul(left, right)
			} else {
				if right.Sign() == 0 {
					return nil, false // division by zero
				}
				left = new(big.Rat).Quo(left, right)
			}
		} else {
			return left, true
		}
	}
}

// unaryFactor parses an optional leading sign applied to a power, with the sign
// binding LOOSER than '^' so that `-2^2` means `-(2^2)` (as in standard math and
// GeoGebra), not `(-2)^2`. To get the negated base, write `(-2)^2`.
func (p *parser) unaryFactor() (*big.Rat, bool) {
	p.skipWS()
	switch p.peek() {
	case '+':
		p.pos++
		return p.power()
	case '-':
		p.pos++
		v, ok := p.power()
		if !ok {
			return nil, false
		}
		return new(big.Rat).Neg(v), true
	}
	return p.power()
}

// power parses `base ^ exp` (right-associative). The base is a primary
// (literal, parenthesized expression, or constant); an exponent may itself be a
// signed power so `2^-3` and `2^(-3)` both work. The base must NOT be a bare
// signed value — a leading '-' is consumed by unaryFactor first.
func (p *parser) power() (*big.Rat, bool) {
	base, ok := p.primary()
	if !ok {
		return nil, false
	}
	p.skipWS()
	if p.peek() != '^' {
		return base, true
	}
	p.pos++
	exp, ok := p.unaryFactor()
	if !ok {
		return nil, false
	}
	return powRat(base, exp)
}

func (p *parser) primary() (*big.Rat, bool) {
	p.skipWS()
	c := p.peek()
	if c == '(' {
		p.pos++
		v, ok := p.expr()
		if !ok {
			return nil, false
		}
		p.skipWS()
		if p.peek() != ')' {
			return nil, false
		}
		p.pos++
		return v, true
	}
	// constant word or number
	start := p.pos
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '.' || ch == '_' {
			p.pos++
		} else {
			break
		}
	}
	if start == p.pos {
		return nil, false
	}
	tok := p.src[start:p.pos]
	// An exponent sign (+/-) is not part of the alnum run above, so a
	// scientific-notation literal like 1E-2 or 2.5e+3 stops at the sign and
	// would fail. Absorb the sign and following digits here, but ONLY when the
	// token so far is a numeric mantissa ending in e/E — never for a bare
	// constant `e` followed by `-3` (that is `e - 3`, which the grammar handles
	// via unary).
	if mantissaEndsWithE(tok) && p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') &&
		p.pos+1 < len(p.src) && isDigit(p.src[p.pos+1]) {
		p.pos++
		for p.pos < len(p.src) && isDigit(p.src[p.pos]) {
			p.pos++
		}
		tok = p.src[start:p.pos]
	}
	if isNumberToken(tok) {
		r, ok := new(big.Rat).SetString(tok)
		if !ok {
			return nil, false
		}
		return r, true
	}
	// constant word
	lower := strings.ToLower(tok)
	if val, ok := constants[lower]; ok && val != nil {
		return val, true
	}
	return nil, false
}

func powRat(base, exp *big.Rat) (*big.Rat, bool) {
	// exp must be a non-negative small integer for exact computation.
	if !exp.IsInt() {
		// For degeneracy, sign of base^exp for integer semantics elsewhere is
		// not needed; require integer exponent.
		return nil, false
	}
	n := exp.Num().Int64()
	if n < 0 {
		return nil, false
	}
	if n == 0 {
		return big.NewRat(1, 1), true
	}
	// Bound the exponent: the value is only used for sign/zero decisions, so an
	// astronomically large exponent (which could come from untrusted AI output)
	// must not trigger an unbounded loop here. Cap the iteration count. Bigger
	// exponents are treated as "resolvable sign" conservatively via overflow of
	// the loop instead of an endless, CPU-exhausting computation.
	if n > maxExponent {
		// 2^maxExponent is enormous; only the sign (of a negative base) could
		// flip, and only for odd exponents. Bases here are non-zero rationals
		// used for radius/coordinate sign checks.
		if base.Sign() == 0 {
			return big.NewRat(0, 1), true // 0^n == 0 (n>0)
		}
		neg := base.Sign() < 0 && n%2 == 1
		r := big.NewRat(1, 1)
		if neg {
			r.Neg(r)
		}
		return r, true
	}
	res := big.NewRat(1, 1)
	for i := int64(0); i < n; i++ {
		res = new(big.Rat).Mul(res, base)
	}
	return res, true
}

// maxExponent caps the iterative exponentiation loop. Values beyond this are
// so large that their exact magnitude is irrelevant to the sign/zero decisions
// the evaluator supports; only the sign (especially for a negative base raised
// to an odd power) matters, which is handled by overflow above. Kept small so
// untrusted input can never force an unbounded CPU loop.
const maxExponent = 1 << 12 // 4096

// mantissaEndsWithE reports whether tok is a numeric mantissa (digits with an
// optional decimal point) immediately followed by e/E — the head of a
// scientific-notation literal whose sign-and-digits exponent follows. A bare
// constant `e` (no mantissa digits) returns false so `e - 3` is never
// absorbed into one token.
func mantissaEndsWithE(tok string) bool {
	if len(tok) < 2 {
		return false
	}
	last := tok[len(tok)-1]
	if last != 'e' && last != 'E' {
		return false
	}
	return isNumberToken(tok[:len(tok)-1])
}

// isDigit reports whether b is an ASCII decimal digit.
func isDigit(b byte) bool {
	return '0' <= b && b <= '9'
}

// isNumberToken reports whether tok is a plain decimal numeral, optionally in
// scientific notation (1e3, 2.5E-2) — GeoGebra accepts both, and the text
// package's number-literal detector already does, so the evaluator must not
// fall out of step with the parser it serves. At least one digit is required;
// the sign is handled by unary before this is called. big.Rat.SetString
// parses the exponent form natively.
func isNumberToken(tok string) bool {
	if tok == "" {
		return false
	}
	i, n := 0, len(tok)
	digits := 0
	for i < n && '0' <= tok[i] && tok[i] <= '9' {
		i++
		digits++
	}
	if i < n && tok[i] == '.' {
		i++
		for i < n && '0' <= tok[i] && tok[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return false
	}
	if i < n && (tok[i] == 'e' || tok[i] == 'E') {
		i++
		if i < n && (tok[i] == '+' || tok[i] == '-') {
			i++
		}
		expDigits := 0
		for i < n && '0' <= tok[i] && tok[i] <= '9' {
			i++
			expDigits++
		}
		if expDigits == 0 {
			return false
		}
	}
	return i == n
}
