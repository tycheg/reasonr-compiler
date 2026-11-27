// Package dec is the ReasonR decimal number (RFC-0001, §7.1): a signed
// 128-bit integer divided by 10¹⁸. The magnitude is below 10³⁸: up to 20
// digits before the point and 18 after, like numeric(38, 18).
//
// Addition and subtraction are exact; multiplication and division round the
// result to 18 digits to the nearest even; Round rounds away from zero.
// Operations whose result is out of range return ok == false: that is an
// overflow, a runtime failure. The functions do not allocate, except
// String and Float64.
package dec

import (
	"errors"
	"math"
	"math/bits"
	"strconv"
)

// Scale is the number of digits after the point.
const Scale = 18

// Digits is the largest number of significant digits.
const Digits = 38

// Dec is a decimal number. The zero value is 0.
type Dec struct{ lo, hi uint64 } // two's complement: hi holds the signed high bits

var (
	errSyntax   = errors.New("dec: invalid number syntax")
	errOverflow = errors.New("dec: number out of the decimal range")
)

// FromBits returns a number by its 128-bit representation: the integer lo +
// hi·2⁶⁴ in two's complement divided by 10¹⁸. ok == false means out of range.
func FromBits(lo, hi uint64) (Dec, bool) {
	d := Dec{lo, hi}
	_, m := d.abs()
	return d, m.cmp(limit) < 0
}

// Bits returns the 128-bit representation: the low and the high bits.
func (d Dec) Bits() (lo, hi uint64) { return d.lo, d.hi }

// FromInt returns an integer number; it never overflows.
func FromInt(x int64) Dec {
	m := u128{0, uint64(x)}
	if x < 0 {
		m.lo = uint64(-x)
	}
	_, m = m.mul64(pow10[Scale].lo)
	return make128(x < 0, m)
}

// make128 builds a number from a sign and a magnitude below limit.
func make128(neg bool, m u128) Dec {
	d := Dec{m.lo, m.hi}
	if neg {
		return d.neg()
	}
	return d
}

// build is like make128 but checks the range.
func build(neg bool, m u128) (Dec, bool) {
	if m.cmp(limit) >= 0 {
		return Dec{}, false
	}
	return make128(neg, m), true
}

func (d Dec) negative() bool { return int64(d.hi) < 0 }

func (d Dec) neg() Dec {
	lo, c := bits.Sub64(0, d.lo, 0)
	hi, _ := bits.Sub64(0, d.hi, c)
	return Dec{lo, hi}
}

// abs returns the sign and the magnitude.
func (d Dec) abs() (bool, u128) {
	if d.negative() {
		n := d.neg()
		return true, u128{n.hi, n.lo}
	}
	return false, u128{d.hi, d.lo}
}

// IsZero reports whether the number is zero.
func (d Dec) IsZero() bool { return d.lo == 0 && d.hi == 0 }

// Sign returns -1, 0 or 1.
func (d Dec) Sign() int {
	switch {
	case d.negative():
		return -1
	case d.IsZero():
		return 0
	}
	return 1
}

// Cmp compares numbers: -1, 0 or 1.
func (d Dec) Cmp(e Dec) int {
	switch {
	case int64(d.hi) < int64(e.hi):
		return -1
	case int64(d.hi) > int64(e.hi):
		return 1
	case d.lo < e.lo:
		return -1
	case d.lo > e.lo:
		return 1
	}
	return 0
}

// Neg returns -d; it never overflows.
func (d Dec) Neg() Dec { return d.neg() }

// Abs returns the magnitude; it never overflows.
func (d Dec) Abs() Dec {
	if d.negative() {
		return d.neg()
	}
	return d
}

// Add returns d + e.
func (d Dec) Add(e Dec) (Dec, bool) {
	lo, c := bits.Add64(d.lo, e.lo, 0)
	hi, _ := bits.Add64(d.hi, e.hi, c)
	// Magnitudes are below 10³⁸, the sum is below 2¹²⁷: two's complement does not overflow.
	return FromBits(lo, hi)
}

// Sub returns d - e.
func (d Dec) Sub(e Dec) (Dec, bool) {
	lo, c := bits.Sub64(d.lo, e.lo, 0)
	hi, _ := bits.Sub64(d.hi, e.hi, c)
	return FromBits(lo, hi)
}

// Mul returns d·e rounded to 18 digits to the nearest even.
func (d Dec) Mul(e Dec) (Dec, bool) {
	dn, dm := d.abs()
	en, em := e.abs()
	p := dm.mul(em)
	r := divWords(p[:], pow10[Scale].lo)
	if p[0] != 0 || p[1] != 0 {
		return Dec{}, false
	}
	q := u128{p[2], p[3]}
	if roundHalf(q, u128{0, r}, pow10[Scale], true) {
		q = q.add64(1)
	}
	return build(dn != en && !q.isZero(), q)
}

// Quo returns d / e rounded to 18 digits to the nearest even. Division by
// zero gives ok == false.
func (d Dec) Quo(e Dec) (Dec, bool) {
	if e.IsZero() {
		return Dec{}, false
	}
	dn, dm := d.abs()
	en, em := e.abs()
	top, n := dm.mul64(pow10[Scale].lo)
	q, r, ok := div192(top, n, em)
	if !ok {
		return Dec{}, false
	}
	if roundHalf(q, r, em, true) {
		q = q.add64(1)
	}
	return build(dn != en && !q.isZero(), q)
}

// Rem returns the remainder d - trunc(d/e)·e: the sign is that of d,
// division by zero gives ok == false. The remainder is exact.
func (d Dec) Rem(e Dec) (Dec, bool) {
	if e.IsZero() {
		return Dec{}, false
	}
	dn, dm := d.abs()
	_, em := e.abs()
	_, r := dm.divmod(em)
	return build(dn && !r.isZero(), r)
}

// Round rounds to n digits after the point away from zero: round(2.345, 2)
// = 2.35, round(-2.5, 0) = -3. A negative n rounds to tens, hundreds and so on.
func (d Dec) Round(n int) (Dec, bool) {
	return d.quantize(n, func(neg bool, q, r, div u128) bool { return roundHalf(q, r, div, false) })
}

// Floor rounds down to an integer.
func (d Dec) Floor() (Dec, bool) {
	return d.quantize(0, func(neg bool, q, r, div u128) bool { return neg && !r.isZero() })
}

// Ceil rounds up to an integer.
func (d Dec) Ceil() (Dec, bool) {
	return d.quantize(0, func(neg bool, q, r, div u128) bool { return !neg && !r.isZero() })
}

// Trunc drops the fractional part; it never overflows.
func (d Dec) Trunc() Dec {
	t, _ := d.quantize(0, func(bool, u128, u128, u128) bool { return false })
	return t
}

// quantize rounds the magnitude to a multiple of 10^(18-n): up decides
// whether to increase the magnitude of the quotient.
func (d Dec) quantize(n int, up func(neg bool, q, r, div u128) bool) (Dec, bool) {
	k := Scale - n
	if k <= 0 {
		return d, true
	}
	neg, m := d.abs()
	if k > Digits {
		// The magnitude is below half the divisor: the result is 0.
		return Dec{}, true
	}
	div := pow10[k]
	q, r := m.divmod(div)
	if up(neg, q, r, div) {
		q = q.add64(1)
	}
	if k == Digits {
		if q.isZero() {
			return Dec{}, true
		}
		return Dec{}, false // 10³⁸ or more
	}
	p := q.mul(div)
	if p[0] != 0 || p[1] != 0 {
		return Dec{}, false
	}
	res := u128{p[2], p[3]}
	return build(neg && !res.isZero(), res)
}

// IsInt reports whether the number is an integer.
func (d Dec) IsInt() bool {
	_, m := d.abs()
	_, r := m.divmod(pow10[Scale])
	return r.isZero()
}

// Int64 returns the integer part (truncating the fraction); ok == false
// means it does not fit into int64.
func (d Dec) Int64() (int64, bool) {
	neg, m := d.abs()
	q, _ := m.divmod(pow10[Scale])
	if q.hi != 0 {
		return 0, false
	}
	if neg {
		if q.lo > 1<<63 {
			return 0, false
		}
		return -int64(q.lo), true
	}
	if q.lo >= 1<<63 {
		return 0, false
	}
	return int64(q.lo), true
}

// FromFloat returns the number with 18 digits nearest to x (the shortest
// decimal form of x rounded to the nearest even). NaN, infinity and
// |x| ≥ 10²⁰ give ok == false.
func FromFloat(x float64) (Dec, bool) {
	if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) >= 1e20 {
		return Dec{}, false
	}
	var buf [64]byte
	d, err := parse(strconv.AppendFloat(buf[:0], x, 'f', -1, 64))
	return d, err == nil
}

// Float64 returns the nearest float64 number.
func (d Dec) Float64() float64 {
	f, _ := strconv.ParseFloat(d.String(), 64)
	return f
}

// Parse parses [+-]digits[.digits]. Digits after the 18th are rounded to
// the nearest even.
func Parse(s string) (Dec, error) {
	return parse([]byte(s))
}

// MustParse is like Parse but panics on an error; for constants and tests.
func MustParse(s string) Dec {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

func parse(s []byte) (Dec, error) {
	neg := false
	if len(s) > 0 && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	var m u128
	digits, frac := 0, -1 // frac is the digits after the point; -1 when there was no point
	round, sticky := byte(0), false
	for i, c := range s {
		switch {
		case c == '.' && frac < 0 && digits > 0 && i < len(s)-1:
			frac = 0
			continue
		case c < '0' || c > '9':
			return Dec{}, errSyntax
		}
		digits++
		if frac >= Scale {
			// Extra digits: the first decides the rounding, the rest only whether there is a tail.
			if frac == Scale {
				round = c - '0'
			} else if c != '0' {
				sticky = true
			}
			frac++
			continue
		}
		if m.cmp(pow10[Digits-1]) >= 0 {
			return Dec{}, errOverflow
		}
		_, m = m.mul64(10)
		m = m.add64(uint64(c - '0'))
		if frac >= 0 {
			frac++
		}
	}
	if digits == 0 {
		return Dec{}, errSyntax
	}
	if frac < 0 {
		frac = 0
	}
	if frac < Scale {
		top, scaled := m.mul64(pow10[Scale-frac].lo)
		if top != 0 {
			return Dec{}, errOverflow
		}
		m = scaled
	}
	if round > 5 || round == 5 && (sticky || m.lo&1 == 1) {
		m = m.add64(1)
	}
	d, ok := build(neg && !m.isZero(), m)
	if !ok {
		return Dec{}, errOverflow
	}
	return d, nil
}

// String returns the canonical form: without extra zeros, "0", "-1.5".
func (d Dec) String() string {
	var buf [48]byte
	return string(d.Append(buf[:0]))
}

// Append appends the canonical form to dst.
func (d Dec) Append(dst []byte) []byte {
	neg, m := d.abs()
	// 38 digits of the magnitude with leading zeros: two halves of 19 digits.
	var digits [Digits]byte
	hi, lo := m.divmod(pow10[19])
	fill(digits[19:], lo.lo)
	fill(digits[:19], hi.lo)
	ip, fp := digits[:Digits-Scale], digits[Digits-Scale:]
	for len(ip) > 1 && ip[0] == '0' {
		ip = ip[1:]
	}
	for len(fp) > 0 && fp[len(fp)-1] == '0' {
		fp = fp[:len(fp)-1]
	}
	if neg {
		dst = append(dst, '-')
	}
	dst = append(dst, ip...)
	if len(fp) > 0 {
		dst = append(dst, '.')
		dst = append(dst, fp...)
	}
	return dst
}

// fill writes x in decimal digits into b with leading zeros.
func fill(b []byte, x uint64) {
	for i := len(b) - 1; i >= 0; i-- {
		b[i] = byte('0' + x%10)
		x /= 10
	}
}
