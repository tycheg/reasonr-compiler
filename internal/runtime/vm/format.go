package vm

import (
	"encoding/hex"
	"strconv"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// text appends to a string started with StringStart the text of a value, as
// string(x) and interpolation print it: numbers, dates and times in
// canonical form, enums by value name, identifiers by content, value
// objects as Type(field: value, …).
func (r *run) text(v model.Value) {
	a := r.a
	var buf [64]byte
	b := buf[:0]
	switch v.Kind {
	case model.KindNone:
		b = append(b, "none"...)
	case model.KindInt, model.KindBigint:
		b = strconv.AppendInt(b, v.Int(), 10)
	case model.KindFloat:
		b = strconv.AppendFloat(b, v.Float(), 'g', -1, 64)
	case model.KindDecimal:
		b = v.Decimal().Append(b)
	case model.KindString:
		if r.quote {
			b = strconv.AppendQuote(b, a.Text(v))
			break
		}
		a.StringAppend(a.Data(v))
	case model.KindBytes:
		a.StringAppend(hex.AppendEncode(b, a.Data(v)))
		return
	case model.KindBool:
		b = strconv.AppendBool(b, v.Bool())
	case model.KindUUID, model.KindTimerID:
		b = appendUUID(b, v.UUID())
	case model.KindDate:
		b = v.Date().AppendFormat(b, time.DateOnly)
	case model.KindTime:
		layout := "15:04"
		if v.Lo%uint64(time.Minute) != 0 {
			layout = "15:04:05.999999999"
		}
		b = time.Unix(0, int64(v.Lo)).UTC().AppendFormat(b, layout)
	case model.KindTimestamp:
		b = v.Timestamp().AppendFormat(b, time.RFC3339Nano)
	case model.KindDuration:
		b = appendDuration(b, v.Int())
	case model.KindEnum:
		if ti := r.m.schema.Type(v.Type); ti != nil && int(v.Lo) < len(ti.Enum) {
			b = append(b, ti.Enum[v.Lo]...)
		}
	case model.KindID:
		name := ""
		if r.quote {
			name = r.varName(v)
		}
		switch {
		case name != "":
			b = append(b, name...)
		case v.Boxed():
			r.text(a.Elems(v)[0])
		case v.Hi == 0 && r.sequence(v.Type):
			b = strconv.AppendUint(b, v.Lo, 10)
		default:
			b = appendUUID(b, v.UUID())
		}
	case model.KindRecord:
		r.record(v)
	case model.KindList, model.KindSet, model.KindMap, model.KindPage, model.KindPageRequest:
		r.list(v)
	}
	if len(b) > 0 {
		a.StringAppend(b)
	}
}

// sequence reports whether an identifier of type t is stored as a sequence.
func (r *run) sequence(t model.TypeRef) bool {
	for range 8 {
		ti := r.m.schema.Type(t)
		if ti == nil {
			return false
		}
		if ti.ID != model.IDDerived {
			return ti.ID == model.IDSequence
		}
		t = ti.IDType
	}
	return false
}

func (r *run) record(v model.Value) {
	a := r.a
	ti := r.m.schema.Type(v.Type)
	if ti != nil {
		a.StringAppend([]byte(ti.Short())) // as in the model text: Route(…), not Ordering.Route(…)
	}
	a.StringAppend([]byte{'('})
	for i, f := range a.Elems(v) {
		if ti != nil && i >= len(ti.Fields) {
			break // the identifier of an entity record is not printed
		}
		if i > 0 {
			a.StringAppend([]byte(", "))
		}
		if ti != nil { // a record without a type (arguments) prints only the values
			a.StringAppend([]byte(ti.Fields[i].Name))
			a.StringAppend([]byte(": "))
		}
		r.text(f)
	}
	a.StringAppend([]byte{')'})
}

func (r *run) list(v model.Value) {
	a := r.a
	a.StringAppend([]byte{'['})
	for i, e := range a.Elems(v) {
		switch {
		case i > 0 && v.Kind == model.KindMap && i%2 == 1:
			a.StringAppend([]byte(": "))
		case i > 0:
			a.StringAppend([]byte(", "))
		}
		r.text(e)
	}
	if v.Kind == model.KindMap && v.Ref.Len == 0 {
		a.StringAppend([]byte{':'})
	}
	a.StringAppend([]byte{']'})
}

// appendDuration prints a duration in the largest exact unit, like a
// language literal: 90s, 2h, 1d, 500ms; otherwise in nanoseconds.
func appendDuration(b []byte, ns int64) []byte {
	if ns == 0 {
		return append(b, "0s"...)
	}
	for _, u := range []struct {
		name string
		ns   int64
	}{{"d", 24 * 3600e9}, {"h", 3600e9}, {"m", 60e9}, {"s", 1e9}, {"ms", 1e6}} {
		if ns%u.ns == 0 {
			return append(strconv.AppendInt(b, ns/u.ns, 10), u.name...)
		}
	}
	return append(strconv.AppendInt(b, ns, 10), "ns"...)
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
