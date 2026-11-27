// Package codec walks model values by their schema for serialization
// formats the compiler does not know (plan 0021, decision 6): encoding
// calls the methods of a Writer in walk order, decoding pulls tokens from a
// Reader. A format is a codec adapter of the engine; the canonical JSON
// (runtime/jsonv) is the built-in one and does not go through here.
package codec

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
	"github.com/tycheg/reasonr-compiler/internal/runtime/record"
)

// Writer receives a value in walk order. A record, a page and a page
// request are objects; a list and a set are lists; a map is a list of
// key and value pairs. A date is a Time at midnight UTC, a time of day a
// Duration, a bigint an Int, a timer identifier a UUID, an identifier its
// inner value.
type Writer interface {
	Object(fields int)
	Field(name string, num int) // num is the field's number from 1, stable while the field keeps its name
	List(n int)
	Map(n int)
	End()
	Null()
	Bool(v bool)
	Int(v int64)
	Float(v float64)
	Decimal(v string)
	String(v string)
	Bytes(v []byte)
	Time(v time.Time)
	Duration(v time.Duration)
	UUID(v [16]byte)
	Enum(name string, num int)
}

// Reader gives the tokens of a value in the order a Writer got them; the
// walker knows the type it expects and asks for it.
type Reader interface {
	// Null reports whether the next value is null and, if so, consumes it.
	Null() (bool, error)
	// Object starts an object; Field gives its next field, ok is false at
	// its end (consumed).
	Object() error
	Field() (name string, num int, ok bool, err error)
	// List and Map start a list or a map; More reports whether another
	// element (for a map: a key and a value) follows, false consumes the end.
	List() error
	Map() error
	More() (bool, error)
	// Skip skips the next value (a field the schema does not have).
	Skip() error
	Bool() (bool, error)
	Int() (int64, error)
	Float() (float64, error)
	Decimal() (string, error)
	String() (string, error)
	Bytes() ([]byte, error)
	Time() (time.Time, error)
	Duration() (time.Duration, error)
	UUID() ([16]byte, error)
	Enum() (name string, num int, err error)
}

// Error is a value that does not fit its schema type, with the path to it.
type Error struct {
	Path string // fields and indexes from the value: "stops[1].address"
	Msg  string
}

func (e *Error) Error() string {
	if e.Path == "" {
		return e.Msg
	}
	return e.Path + ": " + e.Msg
}

func errorf(format string, args ...any) error { return &Error{Msg: fmt.Sprintf(format, args...)} }

// at prefixes the path of err with seg.
func at(err error, seg string) error {
	var e *Error
	if err == nil || !errors.As(err, &e) {
		return err
	}
	out := *e
	switch {
	case out.Path == "":
		out.Path = seg
	case out.Path[0] == '[':
		out.Path = seg + out.Path
	default:
		out.Path = seg + "." + out.Path
	}
	return &out
}

func index(i int) string { return "[" + strconv.Itoa(i) + "]" }

const maxDepth = 64

// Options change how values are walked, as the canonical JSON options do.
type Options struct {
	// SealedPersonal: a personal field may be none even when its type is
	// not optional (its value is sealed or forgotten): it is written and
	// read as null.
	SealedPersonal bool
	// IgnoreUnknown: a record field the schema does not have is skipped
	// (a newer writer), not an error.
	IgnoreUnknown bool
}

// Encode writes value v of type t to w.
func Encode(s *model.Schema, t model.Type, v model.Value, a *model.Arena, w Writer, o Options) error {
	e := encoder{s: s, a: a, w: w, o: o}
	return e.value(&t, v, 0)
}

// EncodeFields writes a record with fields fields (method arguments) as an object.
func EncodeFields(s *model.Schema, fields []model.FieldInfo, r model.Record, a *model.Arena, w Writer, o Options) error {
	e := encoder{s: s, a: a, w: w, o: o}
	return e.fields(fields, r.Value(), 0)
}

// EncodeArgs writes the arguments params as an object of the given ones:
// an argument that is none is left out (the interpreter puts its default),
// as DecodeArgs reads it.
func EncodeArgs(s *model.Schema, params []model.FieldInfo, r model.Record, a *model.Arena, w Writer) error {
	e := encoder{s: s, a: a, w: w}
	vals := a.Elems(r.Value())
	n := 0
	for i := range params {
		if i < len(vals) && vals[i].Kind != model.KindNone {
			n++
		}
	}
	w.Object(n)
	for i := range params {
		if i >= len(vals) || vals[i].Kind == model.KindNone {
			continue
		}
		w.Field(params[i].Name, i+1)
		t := params[i].Type
		t.Optional = true
		if err := e.value(&t, vals[i], 1); err != nil {
			return at(err, params[i].Name)
		}
	}
	w.End()
	return nil
}

type encoder struct {
	s *model.Schema
	a *model.Arena
	w Writer
	o Options
}

func (e *encoder) value(t *model.Type, v model.Value, depth int) error {
	if depth > maxDepth {
		return errorf("nesting deeper than %d", maxDepth)
	}
	if v.Kind == model.KindNone {
		if !t.Optional {
			return errorf("none for non-optional %s", t.Kind)
		}
		e.w.Null()
		return nil
	}
	if v.Kind != t.Kind {
		return errorf("a value of kind %s does not fit type %s", v.Kind, t.Kind)
	}
	switch t.Kind {
	case model.KindInt, model.KindBigint:
		e.w.Int(v.Int())
	case model.KindFloat:
		e.w.Float(v.Float())
	case model.KindDecimal:
		d, ok := dec.FromBits(v.Lo, v.Hi)
		if !ok {
			return errorf("invalid decimal")
		}
		e.w.Decimal(d.String())
	case model.KindString:
		e.w.String(string(e.a.Data(v)))
	case model.KindBytes:
		e.w.Bytes(e.a.Data(v))
	case model.KindBool:
		e.w.Bool(v.Bool())
	case model.KindUUID, model.KindTimerID:
		e.w.UUID(v.UUID())
	case model.KindDate:
		e.w.Time(v.Date())
	case model.KindTime:
		e.w.Duration(time.Duration(v.Lo))
	case model.KindTimestamp:
		e.w.Time(v.Timestamp())
	case model.KindDuration:
		e.w.Duration(time.Duration(int64(v.Lo)))
	case model.KindEnum:
		ti := e.s.Type(t.Ref)
		if ti == nil || v.Type != t.Ref || int(v.Lo) >= len(ti.Enum) {
			return errorf("invalid enum value %d", v.Lo)
		}
		e.w.Enum(ti.Enum[v.Lo], int(v.Lo)+1)
	case model.KindID:
		return e.id(t, v, depth)
	case model.KindRecord:
		ti := e.s.Type(t.Ref)
		if ti == nil || v.Type != t.Ref {
			return errorf("a record of type %d instead of %d", v.Type, t.Ref)
		}
		return e.fields(ti.Fields, v, depth)
	case model.KindList, model.KindSet:
		if t.Elem == nil {
			return errorf("the collection has no element type")
		}
		elems := e.a.Elems(v)
		e.w.List(len(elems))
		for i, x := range elems {
			if err := e.value(t.Elem, x, depth+1); err != nil {
				return at(err, index(i))
			}
		}
		e.w.End()
	case model.KindMap:
		if t.Key == nil || t.Elem == nil {
			return errorf("the map has no key or value type")
		}
		s := e.a.Elems(v)
		e.w.Map(len(s) / 2)
		for i := 0; i < len(s); i += 2 {
			if err := e.value(t.Key, s[i], depth+1); err != nil {
				return at(err, index(i/2))
			}
			if err := e.value(t.Elem, s[i+1], depth+1); err != nil {
				return at(err, index(i/2))
			}
		}
		e.w.End()
	case model.KindPage:
		s := e.a.Elems(v)
		if len(s) != 2 || t.Elem == nil {
			return errorf("invalid page")
		}
		e.w.Object(2)
		e.w.Field("items", 1)
		if err := e.value(&model.Type{Kind: model.KindList, Elem: t.Elem}, s[0], depth+1); err != nil {
			return at(err, "items")
		}
		e.w.Field("next", 2)
		if err := e.value(&optString, s[1], depth+1); err != nil {
			return at(err, "next")
		}
		e.w.End()
	case model.KindPageRequest:
		s := e.a.Elems(v)
		if len(s) != 2 || s[0].Kind != model.KindInt {
			return errorf("invalid PageRequest")
		}
		e.w.Object(2)
		e.w.Field("size", 1)
		e.w.Int(s[0].Int())
		e.w.Field("cursor", 2)
		if err := e.value(&optString, s[1], depth+1); err != nil {
			return at(err, "cursor")
		}
		e.w.End()
	default:
		return errorf("kind %s is not encoded", t.Kind)
	}
	return nil
}

var optString = model.Type{Kind: model.KindString, Optional: true}

func (e *encoder) fields(fields []model.FieldInfo, v model.Value, depth int) error {
	if v.Kind != model.KindRecord {
		return errorf("a value of kind %s instead of a record", v.Kind)
	}
	vals := e.a.Elems(v)
	if len(vals) != len(fields) {
		return errorf("the record has %d fields, the schema %d", len(vals), len(fields))
	}
	e.w.Object(len(fields))
	for i := range fields {
		e.w.Field(fields[i].Name, i+1)
		if vals[i].Kind == model.KindNone && e.o.SealedPersonal && fields[i].Personal != nil {
			e.w.Null()
			continue
		}
		if err := e.value(&fields[i].Type, vals[i], depth+1); err != nil {
			return at(err, fields[i].Name)
		}
	}
	e.w.End()
	return nil
}

// idKind returns the identifier strategy of type ref and the value object
// of a composite one; a derived identifier is the identifier of its entity.
func idKind(s *model.Schema, ref model.TypeRef) (model.IDStrategy, model.TypeRef, error) {
	for range maxDepth {
		ti := s.Type(ref)
		if ti == nil {
			return 0, 0, errorf("no type %d", ref)
		}
		if ti.ID != model.IDDerived {
			return ti.ID, ti.IDType, nil
		}
		ref = ti.IDType
	}
	return 0, 0, errorf("a cycle of derived identifiers at type %d", ref)
}

func (e *encoder) id(t *model.Type, v model.Value, depth int) error {
	if v.Type != t.Ref {
		return errorf("an identifier of type %d instead of %d", v.Type, t.Ref)
	}
	strategy, composite, err := idKind(e.s, t.Ref)
	if err != nil {
		return err
	}
	switch strategy {
	case model.IDUUID:
		e.w.UUID(v.UUID())
		return nil
	case model.IDSequence:
		e.w.Int(int64(v.Lo))
		return nil
	case model.IDGiven:
		if !v.Boxed() {
			return errorf("identifier %d is not in the arena", t.Ref)
		}
		inner := model.Type{Kind: model.KindString}
		if composite != 0 {
			inner = model.Type{Kind: model.KindRecord, Ref: composite}
		}
		return e.value(&inner, e.a.Elems(v)[0], depth+1)
	}
	return errorf("type %d has no identifier", t.Ref)
}

// Decode reads a value of type t from r into the arena a.
func Decode(s *model.Schema, t model.Type, r Reader, a *model.Arena, o Options) (model.Value, error) {
	d := decoder{s: s, a: a, r: r, rec: record.New(s), o: o}
	return d.value(&t, 0)
}

// DecodeArgs reads the arguments params (an object) into a record; a
// missing argument is none (the interpreter puts the defaults).
func DecodeArgs(s *model.Schema, params []model.FieldInfo, r Reader, a *model.Arena) (model.Record, error) {
	d := decoder{s: s, a: a, r: r, rec: record.New(s)}
	if null, err := r.Null(); err != nil || null {
		return a.Record(0, make([]model.Value, len(params))...), err
	}
	vals := make([]model.Value, len(params))
	err := d.object(params, func(i int) error {
		null, err := r.Null()
		if err != nil || null {
			return err
		}
		t := params[i].Type
		t.Optional = false
		vals[i], err = d.value(&t, 1)
		return err
	}, false)
	return a.Record(0, vals...), err
}

type decoder struct {
	s   *model.Schema
	a   *model.Arena
	r   Reader
	rec *record.Codec
	o   Options
}

// object reads an object with fields fields: field calls one with the
// field's index; unknown fields are an error, or skipped with skip.
func (d *decoder) object(fields []model.FieldInfo, field func(i int) error, skip bool) error {
	if err := d.r.Object(); err != nil {
		return err
	}
	seen := make([]bool, len(fields))
	for {
		name, num, ok, err := d.r.Field()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		i := fieldIndex(fields, name, num)
		if i < 0 {
			if skip {
				if err := d.r.Skip(); err != nil {
					return err
				}
				continue
			}
			return at(errorf("no such field"), cmpOr(name, strconv.Itoa(num)))
		}
		if seen[i] {
			return at(errorf("field repeated"), fields[i].Name)
		}
		seen[i] = true
		if err := field(i); err != nil {
			return at(err, fields[i].Name)
		}
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// fieldIndex looks up a field by name, its old name (was), then by number.
func fieldIndex(fields []model.FieldInfo, name string, num int) int {
	if name != "" {
		for i := range fields {
			if fields[i].Name == name {
				return i
			}
		}
		for i := range fields {
			if fields[i].WasName != "" && fields[i].WasName == name {
				return i
			}
		}
		return -1
	}
	if num >= 1 && num <= len(fields) {
		return num - 1
	}
	return -1
}

func (d *decoder) value(t *model.Type, depth int) (model.Value, error) {
	if depth > maxDepth {
		return model.None, errorf("nesting deeper than %d", maxDepth)
	}
	null, err := d.r.Null()
	if err != nil {
		return model.None, err
	}
	if null {
		if !t.Optional {
			return model.None, errorf("null for non-optional %s", t.Kind)
		}
		return model.None, nil
	}
	switch t.Kind {
	case model.KindInt:
		x, err := d.r.Int()
		if err == nil && x != int64(int32(x)) {
			err = errorf("%d out of range for int", x)
		}
		return model.Int(x), err
	case model.KindBigint:
		x, err := d.r.Int()
		return model.Bigint(x), err
	case model.KindFloat:
		f, err := d.r.Float()
		return model.Float(f), err
	case model.KindDecimal:
		text, err := d.r.Decimal()
		if err != nil {
			return model.None, err
		}
		x, err := dec.Parse(text)
		if err != nil {
			return model.None, errorf("invalid decimal %q", text)
		}
		return model.Decimal(x), nil
	case model.KindString:
		s, err := d.r.String()
		return d.a.String(s), err
	case model.KindBytes:
		b, err := d.r.Bytes()
		return d.a.Bytes(b), err
	case model.KindBool:
		b, err := d.r.Bool()
		return model.Bool(b), err
	case model.KindUUID:
		u, err := d.r.UUID()
		return model.UUID(u), err
	case model.KindTimerID:
		u, err := d.r.UUID()
		return model.TimerID(u), err
	case model.KindDate:
		tm, err := d.r.Time()
		return model.Date(tm.UTC().Date()), err
	case model.KindTime:
		x, err := d.r.Duration()
		if err == nil && (x < 0 || x >= 24*time.Hour) {
			err = errorf("time of day out of the day")
		}
		return model.TimeOfDay(x), err
	case model.KindTimestamp:
		tm, err := d.r.Time()
		return model.Timestamp(tm), err
	case model.KindDuration:
		x, err := d.r.Duration()
		return model.Duration(x), err
	case model.KindEnum:
		name, num, err := d.r.Enum()
		if err != nil {
			return model.None, err
		}
		return d.enum(t.Ref, name, num)
	case model.KindID:
		return d.id(t, depth)
	case model.KindRecord:
		ti := d.s.Type(t.Ref)
		if ti == nil {
			return model.None, errorf("no type %d", t.Ref)
		}
		return d.fields(t.Ref, ti.Fields, depth)
	case model.KindList, model.KindSet:
		return d.collection(t, depth)
	case model.KindMap:
		return d.mapValue(t, depth)
	case model.KindPage:
		return d.page(t, depth)
	case model.KindPageRequest:
		return d.pageRequest(depth)
	}
	return model.None, errorf("kind %s is not decoded", t.Kind)
}

func (d *decoder) enum(ref model.TypeRef, name string, num int) (model.Value, error) {
	ti := d.s.Type(ref)
	if ti == nil {
		return model.None, errorf("no enum %d", ref)
	}
	if name == "" {
		if num >= 1 && num <= len(ti.Enum) {
			return model.Enum(ref, num-1), nil
		}
		return model.None, errorf("no value number %d in %s", num, ti.Name)
	}
	for i, n := range ti.Enum {
		if n == name {
			return model.Enum(ref, i), nil
		}
	}
	return model.None, errorf("no value %q in %s", name, ti.Name)
}

func (d *decoder) fields(t model.TypeRef, fields []model.FieldInfo, depth int) (model.Value, error) {
	vals := make([]model.Value, len(fields))
	seen := make([]bool, len(fields))
	err := d.object(fields, func(i int) error {
		t := &fields[i].Type
		if d.o.SealedPersonal && fields[i].Personal != nil && !t.Optional {
			opt := *t
			opt.Optional = true
			t = &opt
		}
		v, err := d.value(t, depth+1)
		vals[i], seen[i] = v, true
		return err
	}, d.o.IgnoreUnknown)
	if err != nil {
		return model.None, err
	}
	for i := range fields {
		if seen[i] {
			continue
		}
		v, err := d.rec.FieldDefault(d.a, &fields[i])
		if err != nil {
			return model.None, at(errorf("no value: %v", err), fields[i].Name)
		}
		vals[i] = v
	}
	return d.a.Record(t, vals...).Value(), nil
}

func (d *decoder) id(t *model.Type, depth int) (model.Value, error) {
	strategy, composite, err := idKind(d.s, t.Ref)
	if err != nil {
		return model.None, err
	}
	switch strategy {
	case model.IDUUID:
		u, err := d.r.UUID()
		return model.ID(t.Ref, model.UUID(u)), err
	case model.IDSequence:
		x, err := d.r.Int()
		return model.Value{Kind: model.KindID, Type: t.Ref, Lo: uint64(x)}, err
	case model.IDGiven:
		inner := model.Type{Kind: model.KindString}
		if composite != 0 {
			inner = model.Type{Kind: model.KindRecord, Ref: composite}
		}
		x, err := d.value(&inner, depth+1)
		if err != nil {
			return model.None, err
		}
		return d.a.ID(t.Ref, x), nil
	}
	return model.None, errorf("type %d has no identifier", t.Ref)
}

func (d *decoder) collection(t *model.Type, depth int) (model.Value, error) {
	if t.Elem == nil {
		return model.None, errorf("the collection has no element type")
	}
	if err := d.r.List(); err != nil {
		return model.None, err
	}
	m := d.a.Mark()
	for i := 0; ; i++ {
		more, err := d.r.More()
		if err != nil {
			d.a.Drop(m)
			return model.None, at(err, index(i))
		}
		if !more {
			break
		}
		x, err := d.value(t.Elem, depth+1)
		if err == nil && t.Kind == model.KindSet && d.contains(m, x, 1) {
			err = errorf("set element repeated")
		}
		if err != nil {
			d.a.Drop(m)
			return model.None, at(err, index(i))
		}
		d.a.Push(x)
	}
	return d.a.End(t.Kind, 0, m), nil
}

// contains looks for x among the stack values from m with step step (2 for map keys).
func (d *decoder) contains(m int, x model.Value, step int) bool {
	for i := m; i < d.a.Mark(); i += step {
		if d.a.Equal(d.a.Stack(i), x) {
			return true
		}
	}
	return false
}

func (d *decoder) mapValue(t *model.Type, depth int) (model.Value, error) {
	if t.Key == nil || t.Elem == nil {
		return model.None, errorf("the map has no key or value type")
	}
	if err := d.r.Map(); err != nil {
		return model.None, err
	}
	m := d.a.Mark()
	for i := 0; ; i++ {
		more, err := d.r.More()
		if err != nil {
			d.a.Drop(m)
			return model.None, at(err, index(i))
		}
		if !more {
			break
		}
		k, err := d.value(t.Key, depth+1)
		if err == nil && d.contains(m, k, 2) {
			err = errorf("map key repeated")
		}
		var v model.Value
		if err == nil {
			v, err = d.value(t.Elem, depth+1)
		}
		if err != nil {
			d.a.Drop(m)
			return model.None, at(err, index(i))
		}
		d.a.Push(k)
		d.a.Push(v)
	}
	return d.a.End(model.KindMap, 0, m), nil
}

var (
	pageFields = []model.FieldInfo{{Name: "items"}, {Name: "next"}}
	pageReq    = []model.FieldInfo{{Name: "size"}, {Name: "cursor"}}
)

func (d *decoder) page(t *model.Type, depth int) (model.Value, error) {
	if t.Elem == nil {
		return model.None, errorf("the page has no element type")
	}
	vals := [2]model.Value{}
	seen := [2]bool{}
	err := d.object(pageFields, func(i int) error {
		tt := &optString
		if i == 0 {
			tt = &model.Type{Kind: model.KindList, Elem: t.Elem}
		}
		v, err := d.value(tt, depth+1)
		vals[i], seen[i] = v, true
		return err
	}, false)
	if err == nil && !seen[0] {
		err = errorf("no items in the page")
	}
	if err != nil {
		return model.None, err
	}
	m := d.a.Mark()
	d.a.Push(vals[0])
	d.a.Push(vals[1])
	return d.a.End(model.KindPage, 0, m), nil
}

func (d *decoder) pageRequest(depth int) (model.Value, error) {
	vals := [2]model.Value{}
	seen := [2]bool{}
	err := d.object(pageReq, func(i int) error {
		tt := &optString
		if i == 0 {
			tt = &model.Type{Kind: model.KindInt}
		}
		v, err := d.value(tt, depth+1)
		vals[i], seen[i] = v, true
		return err
	}, false)
	if err == nil && !seen[0] {
		err = errorf("no size in PageRequest")
	}
	if err != nil {
		return model.None, err
	}
	m := d.a.Mark()
	d.a.Push(vals[0])
	d.a.Push(vals[1])
	return d.a.End(model.KindPageRequest, 0, m), nil
}

// TypeDesc describes a type of the model for schema formats (Protobuf,
// Avro): its name, kind and fields with their numbers.
type TypeDesc struct {
	Name   string
	Kind   string // record, enum
	Fields []FieldDesc
	Values []string // enum values in number order from 1
}

// FieldDesc is a field of a described type.
type FieldDesc struct {
	Name     string
	Num      int
	Type     string // the type as the language writes it: int, text, list<Ordering.Stop>
	Optional bool
}

// Describe describes the record and enum types refs (all of them when refs
// is empty).
func Describe(s *model.Schema, refs []model.TypeRef) []TypeDesc {
	var out []TypeDesc
	add := func(ti *model.TypeInfo) {
		switch {
		case len(ti.Enum) > 0:
			out = append(out, TypeDesc{Name: ti.Name, Kind: "enum", Values: append([]string(nil), ti.Enum...)})
		case len(ti.Fields) > 0:
			d := TypeDesc{Name: ti.Name, Kind: "record"}
			for i, f := range ti.Fields {
				d.Fields = append(d.Fields, FieldDesc{Name: f.Name, Num: i + 1, Type: typeName(s, &f.Type), Optional: f.Type.Optional})
			}
			out = append(out, d)
		}
	}
	if len(refs) == 0 {
		for i := range s.Types {
			add(&s.Types[i])
		}
		return out
	}
	for _, r := range refs {
		if ti := s.Type(r); ti != nil {
			add(ti)
		}
	}
	return out
}

func typeName(s *model.Schema, t *model.Type) string {
	switch t.Kind {
	case model.KindList, model.KindSet:
		if t.Elem != nil {
			return t.Kind.String() + "<" + typeName(s, t.Elem) + ">"
		}
	case model.KindMap:
		if t.Key != nil && t.Elem != nil {
			return "map<" + typeName(s, t.Key) + ", " + typeName(s, t.Elem) + ">"
		}
	case model.KindRecord, model.KindEnum, model.KindID:
		if ti := s.Type(t.Ref); ti != nil {
			if t.Kind == model.KindID {
				return ti.Name + ".Id"
			}
			return ti.Name
		}
	}
	return t.Kind.String()
}
