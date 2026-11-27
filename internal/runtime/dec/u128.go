package dec

import "math/bits"

// u128 is an unsigned 128-bit integer: the magnitude of a Dec value.
type u128 struct{ hi, lo uint64 }

func (a u128) isZero() bool { return a.hi == 0 && a.lo == 0 }

func (a u128) cmp(b u128) int {
	switch {
	case a.hi < b.hi:
		return -1
	case a.hi > b.hi:
		return 1
	case a.lo < b.lo:
		return -1
	case a.lo > b.lo:
		return 1
	}
	return 0
}

func (a u128) add64(b uint64) u128 {
	lo, c := bits.Add64(a.lo, b, 0)
	return u128{a.hi + c, lo}
}

func (a u128) sub(b u128) u128 {
	lo, c := bits.Sub64(a.lo, b.lo, 0)
	hi, _ := bits.Sub64(a.hi, b.hi, c)
	return u128{hi, lo}
}

// mul64 returns a·b as a 192-bit number (the high word, then 128 bits).
func (a u128) mul64(b uint64) (uint64, u128) {
	h0, l0 := bits.Mul64(a.lo, b)
	h1, l1 := bits.Mul64(a.hi, b)
	mid, c := bits.Add64(h0, l1, 0)
	return h1 + c, u128{mid, l0}
}

// mul returns a·b as a 256-bit number: words from high to low.
func (a u128) mul(b u128) [4]uint64 {
	var r [4]uint64 // r[3] is the low word
	h, l := bits.Mul64(a.lo, b.lo)
	r[3] = l
	r[2] = h
	h, l = bits.Mul64(a.lo, b.hi)
	var c uint64
	r[2], c = bits.Add64(r[2], l, 0)
	r[1], c = bits.Add64(h, 0, c)
	r[0] = c
	h, l = bits.Mul64(a.hi, b.lo)
	r[2], c = bits.Add64(r[2], l, 0)
	r[1], c = bits.Add64(r[1], h, c)
	r[0] += c
	h, l = bits.Mul64(a.hi, b.hi)
	r[1], c = bits.Add64(r[1], l, 0)
	r[0], _ = bits.Add64(r[0], h, c)
	return r
}

// divWords divides a number of words (from high to low) by d in place and
// returns the remainder.
func divWords(w []uint64, d uint64) uint64 {
	var r uint64
	for i := range w {
		w[i], r = bits.Div64(r, w[i], d)
	}
	return r
}

// divmod returns the quotient and remainder of a / d, d ≠ 0.
func (a u128) divmod(d u128) (u128, u128) {
	if d.hi == 0 {
		w := [2]uint64{a.hi, a.lo}
		r := divWords(w[:], d.lo)
		return u128{w[0], w[1]}, u128{0, r}
	}
	if a.cmp(d) < 0 {
		return u128{}, a
	}
	// The divisor exceeds 2^64: the quotient is below 2^64, shift and subtract.
	var q uint64
	var r u128
	n := 128 - bits.LeadingZeros64(a.hi)
	for i := n - 1; i >= 0; i-- {
		r = u128{r.hi<<1 | r.lo>>63, r.lo<<1 | a.bit(i)}
		q <<= 1
		if r.cmp(d) >= 0 {
			r = r.sub(d)
			q |= 1
		}
	}
	return u128{0, q}, r
}

func (a u128) bit(i int) uint64 {
	if i >= 64 {
		return a.hi >> (i - 64) & 1
	}
	return a.lo >> i & 1
}

// div192 divides a 192-bit number (hi, mid:lo) by d < 2^127, d ≠ 0. The
// quotient is returned only if it fits into 128 bits (ok).
func div192(top uint64, a u128, d u128) (q u128, r u128, ok bool) {
	if d.hi == 0 {
		w := [3]uint64{top, a.hi, a.lo}
		rem := divWords(w[:], d.lo)
		return u128{w[1], w[2]}, u128{0, rem}, w[0] == 0
	}
	// Shift and subtract: the remainder is below d < 2^127, the shift does not overflow.
	n := 192 - bits.LeadingZeros64(top)
	if top == 0 {
		n = 128 - bits.LeadingZeros64(a.hi)
		if a.hi == 0 {
			n = 64 - bits.LeadingZeros64(a.lo)
		}
	}
	for i := n - 1; i >= 0; i-- {
		var b uint64
		if i >= 128 {
			b = top >> (i - 128) & 1
		} else {
			b = a.bit(i)
		}
		r = u128{r.hi<<1 | r.lo>>63, r.lo<<1 | b}
		if q.hi>>63 != 0 {
			return q, r, false
		}
		q = u128{q.hi<<1 | q.lo>>63, q.lo << 1}
		if r.cmp(d) >= 0 {
			r = r.sub(d)
			q.lo |= 1
		}
	}
	return q, r, true
}

// roundHalf reports whether to increase quotient q given remainder r of a
// division by d: half-even to the nearest even, otherwise away from zero.
func roundHalf(q, r, d u128, even bool) bool {
	if r.isZero() {
		return false
	}
	switch r.cmp(d.sub(r)) { // 2r ? d without overflow
	case 1:
		return true
	case 0:
		return !even || q.lo&1 == 1
	}
	return false
}

// pow10[i] = 10^i, i ≤ 38; limit = 10³⁸ — the magnitude of a Dec is strictly below it.
var (
	pow10 = powers()
	limit = pow10[Digits]
)

func powers() [Digits + 1]u128 {
	var p [Digits + 1]u128
	p[0] = u128{0, 1}
	for i := 1; i < len(p); i++ {
		_, p[i] = p[i-1].mul64(10)
	}
	return p
}
