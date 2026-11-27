package model

import (
	"fmt"
	"sync"
)

// Arena is the memory of a unit of work: the bytes of strings and bytes,
// the field values of records and the elements of collections. Values
// refer to it through ArenaRef. Buffers grow and are reused after Reset: in
// steady state there are no allocations.
//
// Composite values are built bottom-up: first the elements (strings, nested
// records), then the record itself — Push puts elements on the arena stack,
// End moves them into the arena in one piece. The slices Elems and Fields
// return are valid until the next allocation in the arena; values in the
// arena do not change.
//
// A reference with the high bit of Off set points into the program's static
// arena (constants): a regular arena reads it through Static. An arena is
// not meant for concurrent use from several goroutines.
type Arena struct {
	data   []byte
	vals   []Value
	stack  []Value
	flag   uint32 // staticBit for a static arena
	static *Arena
}

const (
	staticBit = 1 << 31
	maxOff    = staticBit - 1
)

// NewArena returns an empty arena.
func NewArena() *Arena { return &Arena{} }

// NewStaticArena returns an arena of program constants: its references are
// marked with the high bit and read from any arena that has it as static.
func NewStaticArena() *Arena { return &Arena{flag: staticBit} }

// SetStatic sets the program's static arena.
func (a *Arena) SetStatic(s *Arena) {
	if s != nil && s.flag == 0 {
		panic("reasonr: SetStatic: the arena is not static")
	}
	a.static = s
}

// Static returns the static arena or nil.
func (a *Arena) Static() *Arena { return a.static }

// Reset frees everything allocated; the buffers and the static arena remain.
func (a *Arena) Reset() {
	a.data = a.data[:0]
	a.vals = a.vals[:0]
	a.stack = a.stack[:0]
}

// Size returns the bytes and values in use.
func (a *Arena) Size() (bytes, values int) { return len(a.data), len(a.vals) }

// ---------------------------------------------------------------------------
// Pool.

var arenas = sync.Pool{New: func() any { return NewArena() }}

// maxPooled: arenas larger than this (in bytes) do not return to the pool,
// so a rare large unit of work does not hold memory.
const maxPooled = 1 << 20

// GetArena takes an empty arena from the pool.
func GetArena() *Arena { return arenas.Get().(*Arena) }

// PutArena returns an arena to the pool; its values become invalid.
func PutArena(a *Arena) {
	if a == nil || a.flag != 0 || cap(a.data)+cap(a.vals)*32 > maxPooled {
		return
	}
	a.Reset()
	a.static = nil
	arenas.Put(a)
}

// ---------------------------------------------------------------------------
// Allocation.

func (a *Arena) ref(off, n int) ArenaRef {
	if off+n > maxOff {
		panic(ErrArenaFull)
	}
	return ArenaRef{Off: uint32(off) | a.flag, Len: uint32(n)}
}

// ErrArenaFull: the arena ran out of address space (2³¹ bytes or values).
// The interpreter turns it into a runtime failure.
var ErrArenaFull = fmt.Errorf("reasonr: arena full")

// String returns a string copied into the arena.
func (a *Arena) String(s string) Value {
	off := len(a.data)
	a.data = append(a.data, s...)
	return Value{Kind: KindString, Ref: a.ref(off, len(s))}
}

// StringBytes returns a string of bytes b copied into the arena.
func (a *Arena) StringBytes(b []byte) Value {
	off := len(a.data)
	a.data = append(a.data, b...)
	return Value{Kind: KindString, Ref: a.ref(off, len(b))}
}

// StringStart starts a string of parts: StringAppend appends a part,
// StringEnd returns the string. No other bytes are allocated in the arena between them.
func (a *Arena) StringStart() int { return len(a.data) }

// StringAppend appends a part of a string; b may lie in the same arena.
func (a *Arena) StringAppend(b []byte) { a.data = append(a.data, b...) }

// StringEnd returns the string started with StringStart.
func (a *Arena) StringEnd(start int) Value {
	return Value{Kind: KindString, Ref: a.ref(start, len(a.data)-start)}
}

// Bytes returns a bytes value copied into the arena.
func (a *Arena) Bytes(b []byte) Value {
	v := a.StringBytes(b)
	v.Kind = KindBytes
	return v
}

// Data returns the bytes of a string or bytes value without copying.
func (a *Arena) Data(v Value) []byte {
	if v.Ref.Len == 0 {
		return nil
	}
	s, off := a.at(v.Ref)
	return s.data[off : off+v.Ref.Len : off+v.Ref.Len]
}

// Text returns a copy of a string.
func (a *Arena) Text(v Value) string { return string(a.Data(v)) }

// at returns the arena that holds the data of a reference and the offset in it.
func (a *Arena) at(r ArenaRef) (*Arena, uint32) {
	if r.Off&staticBit != 0 && a.flag == 0 {
		if a.static == nil {
			panic("reasonr: a static arena reference without a static arena")
		}
		return a.static, r.Off &^ staticBit
	}
	return a, r.Off &^ staticBit
}

// Alloc allocates n values and returns a reference and a slice to fill. The
// slice must be filled before the next allocation.
func (a *Arena) Alloc(n int) (ArenaRef, []Value) {
	off := len(a.vals)
	r := a.ref(off, n)
	a.vals = append(a.vals, make([]Value, n)...)
	return r, a.vals[off : off+n : off+n]
}

// Elems returns the values of a composite value without copying: record
// fields, list and set elements, map keys and values in turn, items and
// next of a page, size and cursor of a pageRequest, the content of a
// composite identifier.
func (a *Arena) Elems(v Value) []Value {
	if v.Ref.Len == 0 {
		return nil
	}
	s, off := a.at(v.Ref)
	return s.vals[off : off+v.Ref.Len : off+v.Ref.Len]
}

// Fields returns the fields of a record without copying.
func (a *Arena) Fields(r Record) []Value { return a.Elems(r.Value()) }

// Len returns the number of elements of a collection (pairs for a map), the
// length of a string in bytes or the number of record fields.
func (a *Arena) Len(v Value) int {
	if v.Kind == KindMap {
		return int(v.Ref.Len / 2)
	}
	return int(v.Ref.Len)
}

// ID returns a string or composite identifier of t: inner is a string or a
// record and goes into the arena.
func (a *Arena) ID(t TypeRef, inner Value) Value {
	r, s := a.Alloc(1)
	s[0] = inner
	return Value{Kind: KindID, Type: t, Ref: r}
}

// Unbox returns the content of an identifier: a uuid, a sequence integer, a
// string or a record.
func (a *Arena) Unbox(v Value, strategy IDStrategy) Value {
	if v.Boxed() {
		return a.Elems(v)[0]
	}
	if strategy == IDSequence {
		return Value{Kind: KindBigint, Lo: v.Lo}
	}
	return Value{Kind: KindUUID, Lo: v.Lo, Hi: v.Hi}
}

// ---------------------------------------------------------------------------
// Building composite values.

// Mark returns the top of the stack: the elements of the next End start there.
func (a *Arena) Mark() int { return len(a.stack) }

// Push puts an element on the stack.
func (a *Arena) Push(v Value) { a.stack = append(a.stack, v) }

// Stack returns stack element number i (from Mark below the top).
func (a *Arena) Stack(i int) Value { return a.stack[i] }

// SetStack replaces stack element number i.
func (a *Arena) SetStack(i int, v Value) { a.stack[i] = v }

// Drop pops the stack elements above mark (building was aborted).
func (a *Arena) Drop(mark int) { a.stack = a.stack[:mark] }

// End moves the stack elements from mark into the arena and returns a
// composite value of kind kind (Record, List, Set, Map, Page, PageRequest)
// of type t. The caller ensures uniqueness of set elements and map keys.
func (a *Arena) End(kind Kind, t TypeRef, mark int) Value {
	items := a.stack[mark:]
	off := len(a.vals)
	r := a.ref(off, len(items))
	a.vals = append(a.vals, items...)
	a.stack = a.stack[:mark]
	return Value{Kind: kind, Type: t, Ref: r}
}

// Record returns a record of type t from field values.
func (a *Arena) Record(t TypeRef, fields ...Value) Record {
	r, s := a.Alloc(len(fields))
	copy(s, fields)
	return Record{Type: t, Ref: r}
}

// List returns a list of elements.
func (a *Arena) List(items ...Value) Value {
	r, s := a.Alloc(len(items))
	copy(s, items)
	return Value{Kind: KindList, Ref: r}
}

// ---------------------------------------------------------------------------
// Copying between arenas.

// Copy copies a value from arena src into a with all nested data. References
// into the shared static arena are not copied.
func (a *Arena) Copy(src *Arena, v Value) Value {
	if v.Ref.Len == 0 {
		v.Ref = ArenaRef{}
		return v
	}
	if src == a {
		return v
	}
	if v.Ref.Off&staticBit != 0 {
		if s, _ := src.at(v.Ref); s == a.static {
			return v
		}
	}
	switch v.Kind {
	case KindString, KindBytes:
		off := len(a.data)
		a.data = append(a.data, src.Data(v)...)
		v.Ref = a.ref(off, int(v.Ref.Len))
		return v
	case KindRecord, KindList, KindSet, KindMap, KindPage, KindPageRequest, KindID:
		// The elements are copied into the arena whole, then nested data is
		// replaced with copies: src ≠ a, so allocations in a do not touch src.
		elems := src.Elems(v)
		off := len(a.vals)
		ref := a.ref(off, len(elems))
		a.vals = append(a.vals, elems...)
		for i, e := range elems {
			switch {
			case e.Ref.Len != 0:
				c := a.Copy(src, e)
				a.vals[off+i] = c
			case e.Ref.Off != 0:
				a.vals[off+i].Ref = ArenaRef{}
			}
		}
		v.Ref = ref
		return v
	}
	return v
}
