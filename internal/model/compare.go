package model

import (
	"bytes"
	"cmp"
	"encoding/hex"
	"math"
	"strconv"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

// Equal compares values structurally (RFC §7.5): strings byte by byte,
// records and lists element by element, set and map regardless of order.
// Both values are read from arena a (and its static arena).
func (a *Arena) Equal(x, y Value) bool { return Equal(a, x, a, y) }

// Equal compares value x from arena ax with value y from arena ay.
func Equal(ax *Arena, x Value, ay *Arena, y Value) bool {
	if x.Kind != y.Kind {
		return false
	}
	switch x.Kind {
	case KindNone:
		return true
	case KindFloat:
		return x.Float() == y.Float()
	case KindString, KindBytes:
		return bytes.Equal(ax.Data(x), ay.Data(y))
	case KindEnum:
		return x.Type == y.Type && x.Lo == y.Lo
	case KindID:
		if x.Type != y.Type || x.Boxed() != y.Boxed() {
			return false
		}
		if x.Boxed() {
			return Equal(ax, ax.Elems(x)[0], ay, ay.Elems(y)[0])
		}
		return x.Lo == y.Lo && x.Hi == y.Hi
	case KindRecord, KindList, KindPage, KindPageRequest:
		if x.Type != y.Type || x.Ref.Len != y.Ref.Len {
			return false
		}
		ys := ay.Elems(y)
		for i, e := range ax.Elems(x) {
			if !Equal(ax, e, ay, ys[i]) {
				return false
			}
		}
		return true
	case KindSet:
		if x.Ref.Len != y.Ref.Len {
			return false
		}
		ys := ay.Elems(y)
		for _, e := range ax.Elems(x) {
			if indexOf(ax, e, ay, ys, 1) < 0 {
				return false
			}
		}
		return true
	case KindMap:
		if x.Ref.Len != y.Ref.Len {
			return false
		}
		xs, ys := ax.Elems(x), ay.Elems(y)
		for i := 0; i < len(xs); i += 2 {
			j := indexOf(ax, xs[i], ay, ys, 2)
			if j < 0 || !Equal(ax, xs[i+1], ay, ys[j+1]) {
				return false
			}
		}
		return true
	}
	return x.Lo == y.Lo && x.Hi == y.Hi
}

// indexOf looks for x among the elements of s with step step (2 for map keys).
func indexOf(ax *Arena, x Value, as *Arena, s []Value, step int) int {
	for i := 0; i < len(s); i += step {
		if Equal(ax, x, as, s[i]) {
			return i
		}
	}
	return -1
}

// Hash returns a hash of a value consistent with Equal: equal values have
// equal hashes. The hash is deterministic across runs.
func (a *Arena) Hash(v Value) uint64 {
	h := fnv(fnvOffset, uint64(v.Kind))
	switch v.Kind {
	case KindNone:
	case KindFloat:
		f := v.Float()
		if f == 0 {
			f = 0 // -0 == 0
		}
		h = fnv(h, math.Float64bits(f))
	case KindString, KindBytes:
		for _, b := range a.Data(v) {
			h = (h ^ uint64(b)) * fnvPrime
		}
	case KindEnum:
		h = fnv(fnv(h, uint64(v.Type)), v.Lo)
	case KindID:
		h = fnv(h, uint64(v.Type))
		if v.Boxed() {
			return fnv(h, a.Hash(a.Elems(v)[0]))
		}
		h = fnv(fnv(h, v.Lo), v.Hi)
	case KindRecord, KindList, KindPage, KindPageRequest:
		h = fnv(h, uint64(v.Type))
		for _, e := range a.Elems(v) {
			h = fnv(h, a.Hash(e))
		}
	case KindSet:
		var sum uint64 // independent of order
		for _, e := range a.Elems(v) {
			sum += mix(a.Hash(e))
		}
		h = fnv(h, sum)
	case KindMap:
		var sum uint64
		s := a.Elems(v)
		for i := 0; i < len(s); i += 2 {
			sum += mix(a.Hash(s[i]) ^ mix(a.Hash(s[i+1])))
		}
		h = fnv(h, sum)
	default:
		h = fnv(fnv(h, v.Lo), v.Hi)
	}
	return h
}

const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

// fnv adds the 8 bytes of x to the hash (FNV-1a).
func fnv(h, x uint64) uint64 {
	for range 8 {
		h = (h ^ x&0xff) * fnvPrime
		x >>= 8
	}
	return h
}

// mix mixes the bits (the splitmix64 finalizer).
func mix(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	return x ^ x>>31
}

// Compare orders values for index keys and sorting. none is less than any
// value; values of different kinds are ordered by kind. Numbers, dates and
// times by magnitude, strings and bytes byte by byte, uuids by bytes, enums
// by value number, records and lists lexicographically. set and map
// compare in insertion order, so for them Compare may disagree with Equal.
func (a *Arena) Compare(x, y Value) int { return Compare(a, x, a, y) }

// Compare orders value x from arena ax and value y from arena ay.
func Compare(ax *Arena, x Value, ay *Arena, y Value) int {
	if x.Kind != y.Kind {
		return cmp.Compare(x.Kind, y.Kind)
	}
	switch x.Kind {
	case KindNone:
		return 0
	case KindInt, KindBigint, KindDate, KindTime, KindDuration:
		return cmp.Compare(int64(x.Lo), int64(y.Lo))
	case KindFloat:
		return cmp.Compare(x.Float(), y.Float())
	case KindDecimal:
		return x.Decimal().Cmp(y.Decimal())
	case KindTimestamp:
		if c := cmp.Compare(int64(x.Lo), int64(y.Lo)); c != 0 {
			return c
		}
		return cmp.Compare(x.Hi, y.Hi)
	case KindString, KindBytes:
		return bytes.Compare(ax.Data(x), ay.Data(y))
	case KindEnum:
		if c := cmp.Compare(x.Type, y.Type); c != 0 {
			return c
		}
		return cmp.Compare(x.Lo, y.Lo)
	case KindID:
		if c := cmp.Compare(x.Type, y.Type); c != 0 {
			return c
		}
		switch {
		case x.Boxed() && y.Boxed():
			return Compare(ax, ax.Elems(x)[0], ay, ay.Elems(y)[0])
		case x.Boxed() != y.Boxed():
			if x.Boxed() {
				return 1
			}
			return -1
		}
		// uuid by bytes; for a sequence Hi == 0, and comparing Lo as unsigned
		// integers matches the order of positive numbers.
	case KindRecord, KindList, KindSet, KindMap, KindPage, KindPageRequest:
		if c := cmp.Compare(x.Type, y.Type); c != 0 {
			return c
		}
		xs, ys := ax.Elems(x), ay.Elems(y)
		for i := range min(len(xs), len(ys)) {
			if c := Compare(ax, xs[i], ay, ys[i]); c != 0 {
				return c
			}
		}
		return cmp.Compare(len(xs), len(ys))
	}
	if c := cmp.Compare(x.Hi, y.Hi); c != 0 {
		return c
	}
	return cmp.Compare(x.Lo, y.Lo)
}

// Format returns a debug form of a value without schema names: T3(…) is a
// record of type 3, T2#1 is value 1 of enum 2.
func (a *Arena) Format(v Value) string { return string(a.AppendFormat(nil, v)) }

// AppendFormat appends the debug form of a value to dst.
func (a *Arena) AppendFormat(dst []byte, v Value) []byte {
	typ := func(dst []byte) []byte { return strconv.AppendUint(append(dst, 'T'), uint64(v.Type), 10) }
	list := func(dst []byte, open, close byte, step int) []byte {
		dst = append(dst, open)
		s := a.Elems(v)
		for i, e := range s {
			if i > 0 {
				if step == 2 && i%2 == 1 {
					dst = append(dst, ": "...)
				} else {
					dst = append(dst, ", "...)
				}
			}
			dst = a.AppendFormat(dst, e)
		}
		if step == 2 && len(s) == 0 {
			dst = append(dst, ':')
		}
		return append(dst, close)
	}
	switch v.Kind {
	case KindNone:
		return append(dst, "none"...)
	case KindInt, KindBigint:
		return strconv.AppendInt(dst, v.Int(), 10)
	case KindFloat:
		return strconv.AppendFloat(dst, v.Float(), 'g', -1, 64)
	case KindDecimal:
		d, ok := dec.FromBits(v.Lo, v.Hi)
		if !ok {
			return append(dst, "decimal?"...)
		}
		return d.Append(dst)
	case KindString:
		return strconv.AppendQuote(dst, string(a.Data(v)))
	case KindBytes:
		dst = append(dst, `bytes"`...)
		return append(hex.AppendEncode(dst, a.Data(v)), '"')
	case KindBool:
		return strconv.AppendBool(dst, v.Bool())
	case KindUUID:
		return appendUUID(dst, v.UUID())
	case KindDate:
		return v.Date().AppendFormat(dst, time.DateOnly)
	case KindTime:
		return time.Unix(0, int64(v.Lo)).UTC().AppendFormat(dst, "15:04:05.999999999")
	case KindTimestamp:
		return v.Timestamp().AppendFormat(dst, time.RFC3339Nano)
	case KindDuration:
		return append(dst, v.Duration().String()...)
	case KindEnum:
		return strconv.AppendUint(append(typ(dst), '#'), v.Lo, 10)
	case KindID:
		dst = append(typ(dst), ".Id("...)
		switch {
		case v.Boxed():
			dst = a.AppendFormat(dst, a.Elems(v)[0])
		case v.Hi == 0:
			dst = strconv.AppendUint(dst, v.Lo, 10)
		default:
			dst = appendUUID(dst, v.UUID())
		}
		return append(dst, ')')
	case KindTimerID:
		return append(appendUUID(append(dst, "Timer.Id("...), v.UUID()), ')')
	case KindRecord:
		return list(typ(dst), '(', ')', 1)
	case KindList:
		return list(dst, '[', ']', 1)
	case KindSet:
		return list(append(dst, "set"...), '[', ']', 1)
	case KindMap:
		return list(dst, '[', ']', 2)
	case KindPage:
		return list(append(dst, "page"...), '(', ')', 1)
	case KindPageRequest:
		return list(append(dst, "PageRequest"...), '(', ')', 1)
	}
	return append(dst, '?')
}

func appendUUID(dst []byte, u [16]byte) []byte {
	var buf [36]byte
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:], u[10:])
	return append(dst, buf[:]...)
}
