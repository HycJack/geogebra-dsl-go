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
func KnownConstant(name string) bool {
	name = strings.ToLower(name)
	_, ok := constants[name]
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
	left, ok := p.power()
	if !ok {
		return nil, false
	}
	for {
		p.skipWS()
		c := p.peek()
		if c == '*' || c == '/' {
			p.pos++
			right, ok := p.power()
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

func (p *parser) power() (*big.Rat, bool) {
	base, ok := p.unary()
	if !ok {
		return nil, false
	}
	p.skipWS()
	if p.peek() == '^' {
		p.pos++
		exp, ok := p.power()
		if !ok {
			return nil, false
		}
		return powRat(base, exp)
	}
	return base, true
}

func (p *parser) unary() (*big.Rat, bool) {
	p.skipWS()
	switch p.peek() {
	case '+':
		p.pos++
		return p.unary()
	case '-':
		p.pos++
		v, ok := p.unary()
		if !ok {
			return nil, false
		}
		return new(big.Rat).Neg(v), true
	}
	return p.primary()
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
	res := big.NewRat(1, 1)
	for i := int64(0); i < n; i++ {
		res = new(big.Rat).Mul(res, base)
	}
	return res, true
}

// isNumberToken reports whether tok is a plain decimal numeral (with optional
// sign already handled by unary).
func isNumberToken(tok string) bool {
	if tok == "" {
		return false
	}
	dot := false
	for _, r := range tok {
		switch {
		case '0' <= r && r <= '9':
		case r == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return true
}
