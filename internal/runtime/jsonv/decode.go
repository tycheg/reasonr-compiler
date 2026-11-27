package jsonv

import (
	"encoding/base64"
	"math"
	"strconv"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
	"github.com/tycheg/reasonr-compiler/internal/runtime/record"
)

// DecodeRecord decodes a record of type t.
func (c *Codec) DecodeRecord(a *model.Arena, t model.TypeRef, data []byte) (model.Record, error) {
	v, err := c.Decode(a, model.Type{Kind: model.KindRecord, Ref: t}, data)
	return v.Record(), err
}

// DecodeFields decodes an object with fields fields (method arguments) into
// a record of type t (0 for arguments): missing fields get their default
// values, none for T?.
func (c *Codec) DecodeFields(a *model.Arena, t model.TypeRef, fields []model.FieldInfo, data []byte) (model.Record, error) {
	d := decoder{c: c, a: a, s: scanner{data: data}}
	v, err := d.fields(t, fields, 0)
	if err == nil {
		err = d.end()
	}
	return v.Record(), err
}

// DecodeArgs decodes the arguments of an external call (a command body,
// query parameters) into a record of parameters params: a missing argument
// and null are none; the interpreter checks default values and required
// ones (invalidArgument "required"). An empty input is an empty object.
func (c *Codec) DecodeArgs(a *model.Arena, params []model.FieldInfo, data []byte) (model.Record, error) {
	d := decoder{c: c, a: a, s: scanner{data: data}}
	if d.s.peek() == 0 {
		return a.Record(0, make([]model.Value, len(params))...), nil
	}
	vals := make([]model.Value, len(params))
	seen := make([]bool, len(params))
	err := d.s.object(func(key string) error {
		i := fieldIndex(params, key)
		if i < 0 {
			return at(ruleErr("unknown", "no such parameter"), key)
		}
		if seen[i] {
			return at(errorf("parameter repeated"), params[i].Name)
		}
		seen[i] = true
		if d.s.null() {
			return nil
		}
		t := params[i].Type
		t.Optional = false
		v, err := d.value(&t, 1)
		if err != nil {
			return at(err, params[i].Name)
		}
		vals[i] = v
		return nil
	})
	if err == nil {
		err = d.end()
	}
	return a.Record(0, vals...), err
}

// Decode decodes a value of type t; data must be read entirely.
func (c *Codec) Decode(a *model.Arena, t model.Type, data []byte) (model.Value, error) {
	d := decoder{c: c, a: a, s: scanner{data: data}}
	v, err := d.value(&t, 0)
	if err == nil {
		err = d.end()
	}
	return v, err
}

type decoder struct {
	c *Codec
	a *model.Arena
	s scanner
}

func (d *decoder) end() error {
	if d.s.peek() != 0 {
		return d.s.unexpected("extra data after the value")
	}
	return nil
}

func (d *decoder) value(t *model.Type, depth int) (model.Value, error) {
	if depth > maxDepth {
		return model.None, errorf("nesting deeper than %d", maxDepth)
	}
	if d.s.null() {
		if !t.Optional {
			return model.None, ruleErr("required", "null for non-optional %s", t.Kind)
		}
		return model.None, nil
	}
	switch t.Kind {
	case model.KindInt:
		x, err := d.integer(false)
		if err == nil && x != int64(int32(x)) {
			err = ruleErr("range", "%d out of range for int", x)
		}
		return model.Int(x), err
	case model.KindBigint:
		x, err := d.integer(true)
		return model.Bigint(x), err
	case model.KindFloat:
		num, err := d.s.number()
		if err != nil {
			return model.None, err
		}
		f, err := strconv.ParseFloat(string(num), 64)
		if err != nil || math.IsInf(f, 0) {
			return model.None, errorf("invalid number %s", num)
		}
		return model.Float(f), nil
	case model.KindDecimal:
		var text []byte
		var err error
		if d.s.peek() == '"' {
			text, err = d.s.string()
		} else {
			text, err = d.s.number()
		}
		if err != nil {
			return model.None, err
		}
		x, err := dec.Parse(string(text))
		if err != nil {
			return model.None, errorf("invalid decimal %q", text)
		}
		return model.Decimal(x), nil
	case model.KindString:
		s, err := d.s.string()
		if err != nil {
			return model.None, err
		}
		return d.a.StringBytes(s), nil
	case model.KindBytes:
		s, err := d.s.string()
		if err != nil {
			return model.None, err
		}
		b, err := base64.StdEncoding.Strict().DecodeString(string(s))
		if err != nil {
			return model.None, ruleErr("encoding", "invalid base64")
		}
		return d.a.Bytes(b), nil
	case model.KindBool:
		switch {
		case d.s.literal("true"):
			return model.Bool(true), nil
		case d.s.literal("false"):
			return model.Bool(false), nil
		}
		return model.None, d.s.unexpected("expected true or false")
	case model.KindUUID, model.KindTimerID:
		u, err := d.uuid()
		if t.Kind == model.KindTimerID {
			return model.TimerID(u), err
		}
		return model.UUID(u), err
	case model.KindDate:
		s, err := d.s.string()
		if err != nil {
			return model.None, err
		}
		tm, err := time.Parse(time.DateOnly, string(s))
		if err != nil {
			return model.None, errorf("invalid date %q", s)
		}
		return model.Date(tm.Date()), nil
	case model.KindTime:
		s, err := d.s.string()
		if err != nil {
			return model.None, err
		}
		tm, err := time.Parse(timeLayout, string(s))
		if err != nil {
			return model.None, errorf("invalid time %q", s)
		}
		h, m, sec := tm.Clock()
		return model.TimeOfDay(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute +
			time.Duration(sec)*time.Second + time.Duration(tm.Nanosecond())), nil
	case model.KindTimestamp:
		s, err := d.s.string()
		if err != nil {
			return model.None, err
		}
		tm, err := time.Parse(time.RFC3339Nano, string(s))
		if err != nil {
			return model.None, errorf("invalid timestamp %q", s)
		}
		return model.Timestamp(tm), nil
	case model.KindDuration:
		s, err := d.s.string()
		if err != nil {
			return model.None, err
		}
		ns, ok := parseDuration(s)
		if !ok {
			return model.None, errorf("invalid duration %q", s)
		}
		return model.Duration(time.Duration(ns)), nil
	case model.KindEnum:
		s, err := d.s.string()
		if err != nil {
			return model.None, err
		}
		return d.enum(t.Ref, s)
	case model.KindID:
		return d.id(t, depth)
	case model.KindRecord:
		ti := d.c.schema.Type(t.Ref)
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

// integer reads an integer: a number or a (quoted) string with an integer.
func (d *decoder) integer(quoted bool) (int64, error) {
	var text []byte
	var err error
	if quoted && d.s.peek() == '"' {
		text, err = d.s.string()
	} else {
		text, err = d.s.number()
	}
	if err != nil {
		return 0, err
	}
	x, err := strconv.ParseInt(string(text), 10, 64)
	if err != nil {
		return 0, errorf("invalid integer %q", text)
	}
	return x, nil
}

func (d *decoder) uuid() ([16]byte, error) {
	s, err := d.s.string()
	if err != nil {
		return [16]byte{}, err
	}
	if len(s) != 36 {
		return [16]byte{}, errorf("invalid uuid %q", s)
	}
	u, err := record.ParseUUID(string(s))
	if err != nil {
		return u, errorf("invalid uuid %q", s)
	}
	return u, nil
}

func (d *decoder) enum(ref model.TypeRef, name []byte) (model.Value, error) {
	ti := d.c.schema.Type(ref)
	if ti == nil {
		return model.None, errorf("no enum %d", ref)
	}
	for i, n := range ti.Enum {
		if n == string(name) {
			return model.Enum(ref, i), nil
		}
	}
	return model.None, errorf("no value %q in %s", name, ti.Name)
}

// fields reads an object with fields fields into a record of type t.
func (d *decoder) fields(t model.TypeRef, fields []model.FieldInfo, depth int) (model.Value, error) {
	vals := make([]model.Value, len(fields))
	seen := make([]bool, len(fields))
	err := d.s.object(func(key string) error {
		i := fieldIndex(fields, key)
		if i < 0 {
			if d.c.IgnoreUnknown {
				return d.s.skip(depth + 1)
			}
			return at(ruleErr("unknown", "no such field"), key)
		}
		if seen[i] {
			return at(errorf("field repeated"), fields[i].Name)
		}
		t := &fields[i].Type
		if d.c.SealedPersonal && fields[i].Personal != nil && !t.Optional {
			opt := *t
			opt.Optional = true
			t = &opt
		}
		v, err := d.value(t, depth+1)
		if err != nil {
			return at(err, fields[i].Name)
		}
		vals[i], seen[i] = v, true
		return nil
	})
	if err != nil {
		return model.None, err
	}
	for i := range fields {
		if seen[i] {
			continue
		}
		v, err := d.c.rec.FieldDefault(d.a, &fields[i])
		if err != nil {
			return model.None, at(errorf("no value: %v", err), fields[i].Name)
		}
		vals[i] = v
	}
	return d.a.Record(t, vals...).Value(), nil
}

// fieldIndex looks up a field by name, then by its old name (was).
func fieldIndex(fields []model.FieldInfo, name string) int {
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

func (d *decoder) id(t *model.Type, depth int) (model.Value, error) {
	strategy, composite, err := idKind(d.c.schema, t.Ref)
	if err != nil {
		return model.None, err
	}
	v := model.Value{Kind: model.KindID, Type: t.Ref}
	switch strategy {
	case model.IDUUID:
		u, err := d.uuid()
		return model.ID(t.Ref, model.UUID(u)), err
	case model.IDSequence:
		x, err := d.integer(true)
		v.Lo = uint64(x)
		return v, err
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
	m := d.a.Mark()
	err := d.s.array(func(i int) error {
		x, err := d.value(t.Elem, depth+1)
		if err != nil {
			return at(err, index(i))
		}
		if t.Kind == model.KindSet && d.contains(m, x, 1) {
			return at(errorf("set element repeated"), index(i))
		}
		d.a.Push(x)
		return nil
	})
	if err != nil {
		d.a.Drop(m)
		return model.None, err
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
	m := d.a.Mark()
	put := func(i int, k model.Value) error {
		if d.contains(m, k, 2) {
			return at(errorf("map key repeated"), index(i))
		}
		v, err := d.value(t.Elem, depth+1)
		if err != nil {
			return at(err, index(i))
		}
		d.a.Push(k)
		d.a.Push(v)
		return nil
	}
	var err error
	if d.c.stringKeys(t.Key) {
		i := 0
		err = d.s.object(func(key string) error {
			k, err := d.key(t.Key, key)
			if err != nil {
				return at(err, index(i))
			}
			i++
			return put(i-1, k)
		})
	} else {
		err = d.s.array(func(i int) error {
			if err := d.s.expect('['); err != nil {
				return at(err, index(i))
			}
			k, err := d.value(t.Key, depth+1)
			if err != nil {
				return at(err, index(i))
			}
			if err := d.s.expect(','); err != nil {
				return at(err, index(i))
			}
			if err := put(i, k); err != nil {
				return err
			}
			return at(d.s.expect(']'), index(i))
		})
	}
	if err != nil {
		d.a.Drop(m)
		return model.None, err
	}
	return d.a.End(model.KindMap, 0, m), nil
}

// key returns a map key from an object string: a string, an enum or a
// string identifier.
func (d *decoder) key(t *model.Type, s string) (model.Value, error) {
	switch t.Kind {
	case model.KindEnum:
		return d.enum(t.Ref, []byte(s))
	case model.KindID:
		return d.a.ID(t.Ref, d.a.String(s)), nil
	}
	return d.a.String(s), nil
}

func (d *decoder) page(t *model.Type, depth int) (model.Value, error) {
	if t.Elem == nil {
		return model.None, errorf("the page has no element type")
	}
	items, next := model.None, model.None
	seenItems, seenNext := false, false
	err := d.s.object(func(key string) error {
		var err error
		switch {
		case key == "items" && !seenItems:
			items, err = d.value(&model.Type{Kind: model.KindList, Elem: t.Elem}, depth+1)
			seenItems = true
		case key == "next" && !seenNext:
			next, err = d.value(&optString, depth+1)
			seenNext = true
		default:
			return errorf("extra page field %q", key)
		}
		return at(err, key)
	})
	if err == nil && !seenItems {
		err = errorf("no items in the page")
	}
	if err != nil {
		return model.None, err
	}
	m := d.a.Mark()
	d.a.Push(items)
	d.a.Push(next)
	return d.a.End(model.KindPage, 0, m), nil
}

func (d *decoder) pageRequest(depth int) (model.Value, error) {
	size, cursor := model.None, model.None
	seenSize, seenCursor := false, false
	err := d.s.object(func(key string) error {
		var err error
		switch {
		case key == "size" && !seenSize:
			size, err = d.value(&model.Type{Kind: model.KindInt}, depth+1)
			seenSize = true
		case key == "cursor" && !seenCursor:
			cursor, err = d.value(&optString, depth+1)
			seenCursor = true
		default:
			return errorf("extra PageRequest field %q", key)
		}
		return at(err, key)
	})
	if err == nil && !seenSize {
		err = errorf("no size in PageRequest")
	}
	if err != nil {
		return model.None, err
	}
	m := d.a.Mark()
	d.a.Push(size)
	d.a.Push(cursor)
	return d.a.End(model.KindPageRequest, 0, m), nil
}

// parseDuration parses a duration: an optional minus, then one or more
// "integer + unit" parts (d, h, m, s, ms, us, ns).
func parseDuration(s []byte) (int64, bool) {
	neg := len(s) > 0 && s[0] == '-'
	if neg {
		s = s[1:]
	}
	limit := uint64(math.MaxInt64)
	if neg {
		limit++
	}
	var total uint64
	for first := true; first || len(s) > 0; first = false {
		i := 0
		for i < len(s) && '0' <= s[i] && s[i] <= '9' {
			i++
		}
		j := i
		for j < len(s) && 'a' <= s[j] && s[j] <= 'z' {
			j++
		}
		unit, ok := durationUnit[string(s[i:j])]
		if i == 0 || !ok {
			return 0, false
		}
		n, err := strconv.ParseUint(string(s[:i]), 10, 64)
		if err != nil || n > limit/unit || total > limit-n*unit {
			return 0, false
		}
		total += n * unit
		s = s[j:]
	}
	if neg {
		return -int64(total), true // -(1<<63) stays MinInt64
	}
	return int64(total), true
}

var durationUnit = map[string]uint64{"d": 24 * 3600e9, "h": 3600e9, "m": 60e9, "s": 1e9, "ms": 1e6, "us": 1e3, "ns": 1}

// DecodeIDText reads an identifier of type t from text (a URL path, a token
// claim): a uuid, a sequence number, a string; a composite one is a JSON object.
func (c *Codec) DecodeIDText(a *model.Arena, t model.TypeRef, text string) (model.Value, error) {
	_, composite, err := idKind(c.schema, t)
	if err != nil {
		return model.None, err
	}
	data := []byte(text)
	if composite == 0 {
		data = appendString(nil, text)
	}
	return c.Decode(a, model.Type{Kind: model.KindID, Ref: t}, data)
}
