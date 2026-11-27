package model

import (
	"encoding/binary"
	"errors"
)

// The binary form of a value with its kind and type, without a schema:
// logs and protocols of engines, snapshots of the in-memory store. Type
// numbers are those of the program that wrote the value.

// ErrShortValue: a value record is truncated.
var ErrShortValue = errors.New("reasonr: value record truncated")

// AppendValue appends value v with all nested data of arena a to dst.
func AppendValue(dst []byte, a *Arena, v Value) []byte {
	dst = append(dst, byte(v.Kind))
	dst = binary.AppendUvarint(dst, uint64(v.Type))
	dst = binary.AppendUvarint(dst, v.Lo)
	dst = binary.AppendUvarint(dst, v.Hi)
	switch {
	case v.Ref.Len == 0:
		dst = binary.AppendUvarint(dst, 0)
	case v.Kind == KindString || v.Kind == KindBytes:
		d := a.Data(v)
		dst = binary.AppendUvarint(dst, uint64(len(d))+1)
		dst = append(dst, d...)
	default:
		elems := a.Elems(v)
		dst = binary.AppendUvarint(dst, uint64(len(elems))+1)
		for _, x := range elems {
			dst = AppendValue(dst, a, x)
		}
	}
	return dst
}

// ReadValue reads a value from the start of src into arena a and returns the rest.
func ReadValue(src []byte, a *Arena) (Value, []byte, error) {
	r := valueReader{b: src}
	v := r.value(a)
	return v, r.b, r.err
}

type valueReader struct {
	b   []byte
	err error
}

func (r *valueReader) u() uint64 {
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.err, r.b = ErrShortValue, nil
		return 0
	}
	r.b = r.b[n:]
	return v
}

func (r *valueReader) value(a *Arena) Value {
	if len(r.b) == 0 {
		r.err = ErrShortValue
		return None
	}
	kind := Kind(r.b[0])
	r.b = r.b[1:]
	typ := TypeRef(r.u())
	lo, hi := r.u(), r.u()
	n := r.u()
	if r.err != nil {
		return None
	}
	var v Value
	switch {
	case n == 0:
		v = Value{Kind: kind}
	case kind == KindString || kind == KindBytes:
		if uint64(len(r.b)) < n-1 {
			r.err, r.b = ErrShortValue, nil
			return None
		}
		v = a.Bytes(r.b[:n-1])
		v.Kind = kind
		r.b = r.b[n-1:]
	default:
		if n-1 > uint64(len(r.b)) {
			r.err, r.b = ErrShortValue, nil
			return None
		}
		mark := a.Mark()
		for range n - 1 {
			a.Push(r.value(a))
			if r.err != nil {
				a.Drop(mark)
				return None
			}
		}
		v = a.End(kind, typ, mark)
	}
	v.Type, v.Lo, v.Hi = typ, lo, hi
	return v
}
