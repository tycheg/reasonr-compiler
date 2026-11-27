// Package record is the canonical binary encoding of values per the model
// schema: engines store entity and projection states, events and timer
// arguments with it. Encoding uses no reflection; the decoder writes values
// into the arena of a unit of work and does not trust its input: any input
// either decodes into a value matching the type or gives an error.
//
// The format (without headers: the caller sets the type):
//
//	T?                   byte 0 for none, 1 for a value of T
//	int, bigint, date,   varint (zigzag); int within 32 bits
//	duration, sequence
//	time                 uvarint nanoseconds since midnight
//	timestamp            varint Unix seconds, uvarint nanoseconds
//	float                8 bytes math.Float64bits, little-endian
//	decimal              16 bytes: Lo, Hi little-endian
//	bool                 byte 0 or 1
//	uuid, Timer.Id,      16 bytes of the uuid
//	uuid identifier
//	string, bytes        uvarint length, the bytes
//	enum                 uvarint value number
//	record               uvarint field count, the fields in schema order;
//	                     missing trailing fields get their default values
//	list, set            uvarint element count, the elements
//	map                  uvarint pair count, key and value in turn
//	page                 items (list), next (string?)
//	pageRequest          size (int), cursor (string?)
//	string identifier    a string; a composite one is a record of its value object
package record

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

// maxDepth is the deepest value nesting: it protects the decoder from a
// stack overflow on corrupted data.
const maxDepth = 64

// Codec encodes and decodes values per a schema. It is safe for concurrent
// use: between calls it keeps only the cache of default records.
type Codec struct {
	schema *model.Schema
	cache  sync.Map // TypeRef → *cached
}

// New returns a codec of schema s.
func New(s *model.Schema) *Codec { return &Codec{schema: s} }

// Schema returns the codec's schema.
func (c *Codec) Schema() *model.Schema { return c.schema }

// ErrCorrupt: the data does not match the type.
var ErrCorrupt = errors.New("record: data corrupted")

func corrupt(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCorrupt, fmt.Sprintf(format, args...))
}

// Type returns the record type t.
func Type(t model.TypeRef) model.Type { return model.Type{Kind: model.KindRecord, Ref: t} }

// AppendRecord appends to dst a record by the fields of its type.
func (c *Codec) AppendRecord(dst []byte, a *model.Arena, r model.Record) ([]byte, error) {
	return c.Append(dst, a, Type(r.Type), r.Value())
}

// DecodeRecord decodes a record of type t.
func (c *Codec) DecodeRecord(a *model.Arena, t model.TypeRef, data []byte) (model.Record, error) {
	v, err := c.Decode(a, Type(t), data)
	return v.Record(), err
}

// AppendFields appends a record with fields fields: method arguments (an
// event, a timer command).
func (c *Codec) AppendFields(dst []byte, a *model.Arena, fields []model.FieldInfo, r model.Record) ([]byte, error) {
	e := encoder{c: c, a: a, buf: dst}
	err := e.fields(fields, r.Value(), 0)
	return e.buf, err
}

// DecodeFields decodes a record with fields fields; t is the type of the
// result record (0 for method arguments).
func (c *Codec) DecodeFields(a *model.Arena, t model.TypeRef, fields []model.FieldInfo, data []byte) (model.Record, error) {
	d := decoder{c: c, a: a, data: data}
	v, err := d.fields(t, fields, 0)
	if err == nil && d.pos != len(d.data) {
		err = corrupt("%d extra bytes", len(d.data)-d.pos)
	}
	return v.Record(), err
}

// Append appends value v of type t to dst.
func (c *Codec) Append(dst []byte, a *model.Arena, t model.Type, v model.Value) ([]byte, error) {
	e := encoder{c: c, a: a, buf: dst}
	err := e.value(&t, v, 0)
	return e.buf, err
}

// Decode decodes a value of type t; data must be read entirely.
func (c *Codec) Decode(a *model.Arena, t model.Type, data []byte) (model.Value, error) {
	d := decoder{c: c, a: a, data: data}
	v, err := d.value(&t, 0)
	if err == nil && d.pos != len(d.data) {
		err = corrupt("%d extra bytes", len(d.data)-d.pos)
	}
	return v, err
}

// fieldsOf returns the fields of a record of type ref.
func (c *Codec) fieldsOf(ref model.TypeRef) ([]model.FieldInfo, error) {
	ti := c.schema.Type(ref)
	if ti == nil {
		return nil, fmt.Errorf("record: no type %d", ref)
	}
	return ti.Fields, nil
}

// idKind returns how an identifier of type ref is stored: the strategy and
// the value object of a composite identifier. A derived identifier is stored
// as the identifier of its entity.
func (c *Codec) idKind(ref model.TypeRef) (model.IDStrategy, model.TypeRef, error) {
	for range maxDepth {
		ti := c.schema.Type(ref)
		if ti == nil {
			return 0, 0, fmt.Errorf("record: no type %d", ref)
		}
		if ti.ID != model.IDDerived {
			return ti.ID, ti.IDType, nil
		}
		ref = ti.IDType
	}
	return 0, 0, fmt.Errorf("record: a cycle of derived identifiers at type %d", ref)
}

// ---------------------------------------------------------------------------

type encoder struct {
	c   *Codec
	a   *model.Arena
	buf []byte
}

func mismatch(t *model.Type, v model.Value) error {
	return fmt.Errorf("record: a value of kind %s does not fit type %s", v.Kind, t.Kind)
}

func (e *encoder) value(t *model.Type, v model.Value, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("record: nesting deeper than %d", maxDepth)
	}
	if t.Optional {
		if v.Kind == model.KindNone {
			e.buf = append(e.buf, 0)
			return nil
		}
		e.buf = append(e.buf, 1)
	}
	if v.Kind != t.Kind {
		return mismatch(t, v)
	}
	switch t.Kind {
	case model.KindInt:
		if x := int64(v.Lo); x != int64(int32(x)) {
			return fmt.Errorf("record: int %d outside 32 bits", x)
		}
		e.buf = binary.AppendVarint(e.buf, int64(v.Lo))
	case model.KindBigint, model.KindDate, model.KindDuration:
		e.buf = binary.AppendVarint(e.buf, int64(v.Lo))
	case model.KindTime:
		e.buf = binary.AppendUvarint(e.buf, v.Lo)
	case model.KindTimestamp:
		e.buf = binary.AppendVarint(e.buf, int64(v.Lo))
		e.buf = binary.AppendUvarint(e.buf, v.Hi)
	case model.KindFloat:
		e.buf = binary.LittleEndian.AppendUint64(e.buf, v.Lo)
	case model.KindDecimal:
		e.buf = binary.LittleEndian.AppendUint64(e.buf, v.Lo)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, v.Hi)
	case model.KindBool:
		e.buf = append(e.buf, byte(v.Lo&1))
	case model.KindUUID, model.KindTimerID:
		e.uuid(v)
	case model.KindString, model.KindBytes:
		b := e.a.Data(v)
		e.buf = binary.AppendUvarint(e.buf, uint64(len(b)))
		e.buf = append(e.buf, b...)
	case model.KindEnum:
		e.buf = binary.AppendUvarint(e.buf, v.Lo)
	case model.KindID:
		return e.id(t, v, depth)
	case model.KindRecord:
		fields, err := e.c.fieldsOf(t.Ref)
		if err != nil {
			return err
		}
		if v.Type != t.Ref {
			return fmt.Errorf("record: a record of type %d instead of %d", v.Type, t.Ref)
		}
		return e.fields(fields, v, depth)
	case model.KindList, model.KindSet, model.KindMap:
		if t.Elem == nil || t.Kind == model.KindMap && t.Key == nil {
			return fmt.Errorf("record: type %s has no element type", t.Kind)
		}
		e.buf = binary.AppendUvarint(e.buf, uint64(e.a.Len(v)))
		for i, x := range e.a.Elems(v) {
			et := t.Elem
			if t.Kind == model.KindMap && i%2 == 0 {
				et = t.Key
			}
			if err := e.value(et, x, depth+1); err != nil {
				return err
			}
		}
	case model.KindPage, model.KindPageRequest:
		s := e.a.Elems(v)
		if len(s) != 2 {
			return fmt.Errorf("record: %s has %d values instead of 2", t.Kind, len(s))
		}
		first := model.Type{Kind: model.KindInt}
		if t.Kind == model.KindPage {
			if t.Elem == nil {
				return fmt.Errorf("record: the page has no element type")
			}
			first = model.Type{Kind: model.KindList, Elem: t.Elem}
		}
		if err := e.value(&first, s[0], depth+1); err != nil {
			return err
		}
		return e.value(&optString, s[1], depth+1)
	default:
		return fmt.Errorf("record: kind %s is not encoded", t.Kind)
	}
	return nil
}

var optString = model.Type{Kind: model.KindString, Optional: true}

func (e *encoder) uuid(v model.Value) {
	e.buf = binary.BigEndian.AppendUint64(e.buf, v.Hi)
	e.buf = binary.BigEndian.AppendUint64(e.buf, v.Lo)
}

func (e *encoder) fields(fields []model.FieldInfo, v model.Value, depth int) error {
	vals := e.a.Elems(v)
	if len(vals) != len(fields) {
		return fmt.Errorf("record: the record has %d fields, the schema %d", len(vals), len(fields))
	}
	e.buf = binary.AppendUvarint(e.buf, uint64(len(vals)))
	for i := range fields {
		if err := e.value(&fields[i].Type, vals[i], depth+1); err != nil {
			return fmt.Errorf("%w (field %s)", err, fields[i].Name)
		}
	}
	return nil
}

func (e *encoder) id(t *model.Type, v model.Value, depth int) error {
	if v.Type != t.Ref {
		return fmt.Errorf("record: an identifier of type %d instead of %d", v.Type, t.Ref)
	}
	strategy, composite, err := e.c.idKind(t.Ref)
	if err != nil {
		return err
	}
	switch strategy {
	case model.IDUUID, model.IDSequence:
		if v.Boxed() {
			return fmt.Errorf("record: identifier %d is in the arena, expected %s", t.Ref, strategy)
		}
		if strategy == model.IDSequence {
			e.buf = binary.AppendVarint(e.buf, int64(v.Lo))
		} else {
			e.uuid(v)
		}
		return nil
	case model.IDGiven:
		if !v.Boxed() {
			return fmt.Errorf("record: identifier %d is not in the arena", t.Ref)
		}
		inner := model.Type{Kind: model.KindString}
		if composite != 0 {
			inner = Type(composite)
		}
		return e.value(&inner, e.a.Elems(v)[0], depth+1)
	}
	return fmt.Errorf("record: type %d has no identifier", t.Ref)
}

// ---------------------------------------------------------------------------

type decoder struct {
	c    *Codec
	a    *model.Arena
	data []byte
	pos  int
}

func (d *decoder) byte() (byte, error) {
	if d.pos >= len(d.data) {
		return 0, corrupt("unexpected end of data")
	}
	b := d.data[d.pos]
	d.pos++
	return b, nil
}

func (d *decoder) uvarint() (uint64, error) {
	x, n := binary.Uvarint(d.data[d.pos:])
	if n <= 0 {
		return 0, corrupt("invalid number at position %d", d.pos)
	}
	d.pos += n
	return x, nil
}

func (d *decoder) varint() (int64, error) {
	x, n := binary.Varint(d.data[d.pos:])
	if n <= 0 {
		return 0, corrupt("invalid number at position %d", d.pos)
	}
	d.pos += n
	return x, nil
}

func (d *decoder) fixed(n int) ([]byte, error) {
	if len(d.data)-d.pos < n {
		return nil, corrupt("unexpected end of data")
	}
	b := d.data[d.pos : d.pos+n]
	d.pos += n
	return b, nil
}

// count reads a number of elements: each takes at least a byte, so a count
// exceeding the rest of the data is corruption (not a reason to allocate).
func (d *decoder) count(per int) (int, error) {
	n, err := d.uvarint()
	if err != nil {
		return 0, err
	}
	if n > uint64(len(d.data)-d.pos)/uint64(per) {
		return 0, corrupt("element count %d exceeds the data", n)
	}
	return int(n), nil
}

func (d *decoder) value(t *model.Type, depth int) (model.Value, error) {
	if depth > maxDepth {
		return model.None, corrupt("nesting deeper than %d", maxDepth)
	}
	if t.Optional {
		tag, err := d.byte()
		switch {
		case err != nil:
			return model.None, err
		case tag == 0:
			return model.None, nil
		case tag != 1:
			return model.None, corrupt("invalid optional value flag %d", tag)
		}
	}
	v := model.Value{Kind: t.Kind}
	switch t.Kind {
	case model.KindInt, model.KindBigint, model.KindDate, model.KindDuration:
		x, err := d.varint()
		if err == nil && t.Kind == model.KindInt && x != int64(int32(x)) {
			err = corrupt("int %d outside 32 bits", x)
		}
		v.Lo = uint64(x)
		return v, err
	case model.KindTime:
		x, err := d.uvarint()
		if err == nil && x >= uint64(24*60*60*1e9) {
			err = corrupt("time of day %d ns", x)
		}
		v.Lo = x
		return v, err
	case model.KindTimestamp:
		sec, err := d.varint()
		if err != nil {
			return v, err
		}
		ns, err := d.uvarint()
		if err == nil && ns >= 1e9 {
			err = corrupt("nanoseconds %d", ns)
		}
		v.Lo, v.Hi = uint64(sec), ns
		return v, err
	case model.KindFloat:
		b, err := d.fixed(8)
		if err == nil {
			v.Lo = binary.LittleEndian.Uint64(b)
		}
		return v, err
	case model.KindDecimal:
		b, err := d.fixed(16)
		if err != nil {
			return v, err
		}
		v.Lo, v.Hi = binary.LittleEndian.Uint64(b), binary.LittleEndian.Uint64(b[8:])
		if _, ok := dec.FromBits(v.Lo, v.Hi); !ok {
			return v, corrupt("decimal out of range")
		}
		return v, nil
	case model.KindBool:
		b, err := d.byte()
		if err == nil && b > 1 {
			err = corrupt("bool %d", b)
		}
		v.Lo = uint64(b)
		return v, err
	case model.KindUUID, model.KindTimerID:
		return d.uuid(v)
	case model.KindString, model.KindBytes:
		n, err := d.count(1)
		if err != nil {
			return v, err
		}
		b, _ := d.fixed(n)
		s := d.a.StringBytes(b)
		s.Kind = t.Kind
		return s, nil
	case model.KindEnum:
		x, err := d.uvarint()
		if err != nil {
			return v, err
		}
		ti := d.c.schema.Type(t.Ref)
		if ti == nil || x >= uint64(len(ti.Enum)) {
			return v, corrupt("value %d of enum %d", x, t.Ref)
		}
		v.Type, v.Lo = t.Ref, x
		return v, nil
	case model.KindID:
		return d.id(t, depth)
	case model.KindRecord:
		fields, err := d.c.fieldsOf(t.Ref)
		if err != nil {
			return v, err
		}
		return d.fields(t.Ref, fields, depth)
	case model.KindList, model.KindSet, model.KindMap:
		return d.collection(t, depth)
	case model.KindPage, model.KindPageRequest:
		first := model.Type{Kind: model.KindInt}
		if t.Kind == model.KindPage {
			if t.Elem == nil {
				return v, fmt.Errorf("record: the page has no element type")
			}
			first = model.Type{Kind: model.KindList, Elem: t.Elem}
		}
		x, err := d.value(&first, depth+1)
		if err != nil {
			return v, err
		}
		next, err := d.value(&optString, depth+1)
		if err != nil {
			return v, err
		}
		m := d.a.Mark()
		d.a.Push(x)
		d.a.Push(next)
		return d.a.End(t.Kind, 0, m), nil
	}
	return v, fmt.Errorf("record: kind %s is not decoded", t.Kind)
}

func (d *decoder) uuid(v model.Value) (model.Value, error) {
	b, err := d.fixed(16)
	if err == nil {
		v.Hi, v.Lo = binary.BigEndian.Uint64(b), binary.BigEndian.Uint64(b[8:])
	}
	return v, err
}

// fields decodes a record: missing trailing fields (a record of an older
// schema version) get their default values.
func (d *decoder) fields(t model.TypeRef, fields []model.FieldInfo, depth int) (model.Value, error) {
	n, err := d.count(1)
	if err != nil {
		return model.None, err
	}
	if n > len(fields) {
		return model.None, corrupt("the record has %d fields, the schema %d", n, len(fields))
	}
	m := d.a.Mark()
	for i := range fields {
		var x model.Value
		if i < n {
			x, err = d.value(&fields[i].Type, depth+1)
		} else {
			x, err = d.c.field(d.a, &fields[i], depth+1)
		}
		if err != nil {
			d.a.Drop(m)
			return model.None, err
		}
		d.a.Push(x)
	}
	return d.a.End(model.KindRecord, t, m), nil
}

func (d *decoder) collection(t *model.Type, depth int) (model.Value, error) {
	if t.Elem == nil || t.Kind == model.KindMap && t.Key == nil {
		return model.None, fmt.Errorf("record: type %s has no element type", t.Kind)
	}
	per := 1
	if t.Kind == model.KindMap {
		per = 2
	}
	n, err := d.count(per)
	if err != nil {
		return model.None, err
	}
	m := d.a.Mark()
	fail := func(err error) (model.Value, error) {
		d.a.Drop(m)
		return model.None, err
	}
	for range n {
		if t.Kind == model.KindMap {
			k, err := d.value(t.Key, depth+1)
			if err != nil {
				return fail(err)
			}
			d.a.Push(k)
		}
		x, err := d.value(t.Elem, depth+1)
		if err != nil {
			return fail(err)
		}
		d.a.Push(x)
	}
	v := d.a.End(t.Kind, 0, m)
	if t.Kind != model.KindList && duplicates(d.a, d.a.Elems(v), per) {
		return model.None, corrupt("repeated element of %s", t.Kind)
	}
	return v, nil
}

// duplicates reports whether set elements or map keys (step per) are equal.
func duplicates(a *model.Arena, s []model.Value, per int) bool {
	for i := 0; i < len(s); i += per {
		for j := i + per; j < len(s); j += per {
			if a.Equal(s[i], s[j]) {
				return true
			}
		}
	}
	return false
}

func (d *decoder) id(t *model.Type, depth int) (model.Value, error) {
	strategy, composite, err := d.c.idKind(t.Ref)
	if err != nil {
		return model.None, err
	}
	v := model.Value{Kind: model.KindID, Type: t.Ref}
	switch strategy {
	case model.IDUUID:
		return d.uuid(v)
	case model.IDSequence:
		x, err := d.varint()
		v.Lo = uint64(x)
		return v, err
	case model.IDGiven:
		inner := model.Type{Kind: model.KindString}
		if composite != 0 {
			inner = Type(composite)
		}
		x, err := d.value(&inner, depth+1)
		if err != nil {
			return model.None, err
		}
		return d.a.ID(t.Ref, x), nil
	}
	return model.None, fmt.Errorf("record: type %d has no identifier", t.Ref)
}
