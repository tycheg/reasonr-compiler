// Package jsonv is the canonical JSON of ReasonR values per the model
// schema (RFC §31.3): one format in the
// HTTP API, in jsonb state, the outbox, exports and logs. Encoding uses no
// reflection; the decoder writes values into an arena and does not trust
// its input: any input either decodes into a value matching the type or
// gives an *Error with the path to the error.
//
//	int, float                 a number
//	bigint, sequence-id        a string with an integer (a number is accepted too)
//	decimal                    a string (a number is accepted too)
//	string, bool               a string, true / false
//	bytes                      a base64 string
//	uuid, uuid-id, Timer.Id    a string 8-4-4-4-12
//	date, time, timestamp      "2026-10-04", "15:04:05.5", RFC 3339 UTC
//	duration                   the largest exact unit: "90s", "2h", "500ms";
//	                           a composite "1h30m" is accepted too
//	enum                       the value name
//	value object, record,      an object with fields in schema order; on
//	composite identifier       input a missing field gets its default value
//	string-id                  a string
//	T? = none                  null
//	list, set                  an array
//	map                        an object if the keys are strings, enums or
//	                           string identifiers; otherwise an array of [k, v] pairs
//	page                       {"items": [...], "next": "cursor" | null}
//	PageRequest                {"size": 20, "cursor": null}
package jsonv

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
	"github.com/tycheg/reasonr-compiler/internal/runtime/record"
)

// maxDepth is the deepest value nesting.
const maxDepth = 64

// Codec encodes and decodes the values of a schema. It is safe for
// concurrent use.
type Codec struct {
	schema *model.Schema
	rec    *record.Codec // default values
	// IgnoreUnknown skips fields absent from the schema when decoding (state
	// written by an earlier model version); otherwise they are an error.
	IgnoreUnknown bool
	// SealedPersonal: personal fields and parameters may be none even for a
	// non-optional type: the store keeps a record without them and their
	// ciphertexts separately (RFC §20).
	SealedPersonal bool
}

// New returns a codec of schema s.
func New(s *model.Schema) *Codec { return &Codec{schema: s, rec: record.New(s)} }

// Schema returns the codec's schema.
func (c *Codec) Schema() *model.Schema { return c.schema }

// Error is an encoding or decoding error: Path is the path to the place
// ("route.from.lat", "items[2]"), "" for the root; Rule is the violated
// rule in invalidArgument terms (RFC §14): type, required, range, encoding,
// unknown.
type Error struct {
	Path string
	Msg  string
	Rule string
}

func (e *Error) Error() string {
	if e.Path == "" {
		return "jsonv: " + e.Msg
	}
	return "jsonv: " + e.Path + ": " + e.Msg
}

func errorf(format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...), Rule: "type"}
}

// ruleErr is an error with rule rule.
func ruleErr(rule, format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...), Rule: rule}
}

// at adds segment seg (a field name or [i]) to the path of error err.
func at(err error, seg string) error {
	e, ok := err.(*Error)
	if !ok {
		return err
	}
	switch {
	case e.Path == "":
		e.Path = seg
	case e.Path[0] == '[':
		e.Path = seg + e.Path
	default:
		e.Path = seg + "." + e.Path
	}
	return e
}

func index(i int) string { return "[" + strconv.Itoa(i) + "]" }

// AppendRecord appends a record by the fields of its type.
func (c *Codec) AppendRecord(dst []byte, a *model.Arena, r model.Record) ([]byte, error) {
	return c.Append(dst, a, model.Type{Kind: model.KindRecord, Ref: r.Type}, r.Value())
}

// AppendFields appends a record with fields fields (method arguments) as an object.
func (c *Codec) AppendFields(dst []byte, a *model.Arena, fields []model.FieldInfo, r model.Record) ([]byte, error) {
	e := encoder{c: c, a: a, buf: dst}
	err := e.fields(fields, r.Value(), 0)
	return e.buf, err
}

// Append appends value v of type t.
func (c *Codec) Append(dst []byte, a *model.Arena, t model.Type, v model.Value) ([]byte, error) {
	e := encoder{c: c, a: a, buf: dst}
	err := e.value(&t, v, 0)
	return e.buf, err
}

type encoder struct {
	c   *Codec
	a   *model.Arena
	buf []byte
}

func mismatch(t *model.Type, v model.Value) error {
	return errorf("a value of kind %s does not fit type %s", v.Kind, t.Kind)
}

func (e *encoder) value(t *model.Type, v model.Value, depth int) error {
	if depth > maxDepth {
		return errorf("nesting deeper than %d", maxDepth)
	}
	if v.Kind == model.KindNone {
		if !t.Optional {
			return errorf("none for non-optional %s", t.Kind)
		}
		e.buf = append(e.buf, "null"...)
		return nil
	}
	if v.Kind != t.Kind {
		return mismatch(t, v)
	}
	switch t.Kind {
	case model.KindInt:
		e.buf = strconv.AppendInt(e.buf, v.Int(), 10)
	case model.KindBigint:
		e.buf = strconv.AppendInt(append(e.buf, '"'), v.Int(), 10)
		e.buf = append(e.buf, '"')
	case model.KindFloat:
		f := v.Float()
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return errorf("float %v cannot be written in JSON", f)
		}
		e.buf = strconv.AppendFloat(e.buf, f, 'g', -1, 64)
	case model.KindDecimal:
		d, ok := dec.FromBits(v.Lo, v.Hi)
		if !ok {
			return errorf("invalid decimal")
		}
		e.buf = append(d.Append(append(e.buf, '"')), '"')
	case model.KindString:
		return e.string(e.a.Data(v))
	case model.KindBytes:
		data := e.a.Data(v)
		e.buf = append(e.buf, '"')
		e.buf = base64.StdEncoding.AppendEncode(e.buf, data)
		e.buf = append(e.buf, '"')
	case model.KindBool:
		e.buf = strconv.AppendBool(e.buf, v.Bool())
	case model.KindUUID, model.KindTimerID:
		e.uuid(v)
	case model.KindDate:
		e.buf = append(v.Date().AppendFormat(append(e.buf, '"'), time.DateOnly), '"')
	case model.KindTime:
		if v.Lo >= uint64(24*time.Hour) {
			return errorf("time of day out of the day")
		}
		e.buf = append(time.Unix(0, int64(v.Lo)).UTC().AppendFormat(append(e.buf, '"'), timeLayout), '"')
	case model.KindTimestamp:
		e.buf = append(v.Timestamp().AppendFormat(append(e.buf, '"'), time.RFC3339Nano), '"')
	case model.KindDuration:
		e.buf = append(appendDuration(append(e.buf, '"'), int64(v.Lo)), '"')
	case model.KindEnum:
		ti := e.c.schema.Type(t.Ref)
		if ti == nil || v.Type != t.Ref || int(v.Lo) >= len(ti.Enum) {
			return errorf("invalid enum value %d", v.Lo)
		}
		e.buf = append(append(append(e.buf, '"'), ti.Enum[v.Lo]...), '"')
	case model.KindID:
		return e.id(t, v, depth)
	case model.KindRecord:
		ti := e.c.schema.Type(t.Ref)
		if ti == nil || v.Type != t.Ref {
			return errorf("a record of type %d instead of %d", v.Type, t.Ref)
		}
		return e.fields(ti.Fields, v, depth)
	case model.KindList, model.KindSet:
		if t.Elem == nil {
			return errorf("the collection has no element type")
		}
		e.buf = append(e.buf, '[')
		for i, x := range e.a.Elems(v) {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			if err := e.value(t.Elem, x, depth+1); err != nil {
				return at(err, index(i))
			}
		}
		e.buf = append(e.buf, ']')
	case model.KindMap:
		return e.mapValue(t, v, depth)
	case model.KindPage:
		s := e.a.Elems(v)
		if len(s) != 2 || t.Elem == nil {
			return errorf("invalid page")
		}
		e.buf = append(e.buf, `{"items":`...)
		if err := e.value(&model.Type{Kind: model.KindList, Elem: t.Elem}, s[0], depth+1); err != nil {
			return at(err, "items")
		}
		e.buf = append(e.buf, `,"next":`...)
		if err := e.value(&optString, s[1], depth+1); err != nil {
			return at(err, "next")
		}
		e.buf = append(e.buf, '}')
	case model.KindPageRequest:
		s := e.a.Elems(v)
		if len(s) != 2 || s[0].Kind != model.KindInt {
			return errorf("invalid PageRequest")
		}
		e.buf = strconv.AppendInt(append(e.buf, `{"size":`...), s[0].Int(), 10)
		e.buf = append(e.buf, `,"cursor":`...)
		if err := e.value(&optString, s[1], depth+1); err != nil {
			return at(err, "cursor")
		}
		e.buf = append(e.buf, '}')
	default:
		return errorf("kind %s is not encoded", t.Kind)
	}
	return nil
}

var optString = model.Type{Kind: model.KindString, Optional: true}

// timeLayout is a time of day: the fraction of seconds has significant digits only.
const timeLayout = "15:04:05.999999999"

func (e *encoder) fields(fields []model.FieldInfo, v model.Value, depth int) error {
	if v.Kind != model.KindRecord {
		return errorf("a value of kind %s instead of a record", v.Kind)
	}
	vals := e.a.Elems(v)
	if len(vals) != len(fields) {
		return errorf("the record has %d fields, the schema %d", len(vals), len(fields))
	}
	e.buf = append(e.buf, '{')
	for i := range fields {
		if i > 0 {
			e.buf = append(e.buf, ',')
		}
		e.buf = appendString(e.buf, fields[i].Name)
		e.buf = append(e.buf, ':')
		if vals[i].Kind == model.KindNone && e.c.SealedPersonal && fields[i].Personal != nil {
			e.buf = append(e.buf, "null"...)
			continue
		}
		if err := e.value(&fields[i].Type, vals[i], depth+1); err != nil {
			return at(err, fields[i].Name)
		}
	}
	e.buf = append(e.buf, '}')
	return nil
}

// idKind returns the identifier strategy of type ref and the value object
// of a composite one; a derived identifier is written as the identifier of
// its entity.
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
	strategy, composite, err := idKind(e.c.schema, t.Ref)
	if err != nil {
		return err
	}
	switch strategy {
	case model.IDUUID, model.IDSequence:
		if v.Boxed() {
			return errorf("identifier %d is in the arena, expected %s", t.Ref, strategy)
		}
		if strategy == model.IDUUID {
			e.uuid(v)
			return nil
		}
		e.buf = strconv.AppendInt(append(e.buf, '"'), int64(v.Lo), 10)
		e.buf = append(e.buf, '"')
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

// stringKeys reports whether a map with keys of type k is written as an object.
func (c *Codec) stringKeys(k *model.Type) bool {
	switch k.Kind {
	case model.KindString, model.KindEnum:
		return !k.Optional
	case model.KindID:
		strategy, composite, err := idKind(c.schema, k.Ref)
		return err == nil && strategy == model.IDGiven && composite == 0 && !k.Optional
	}
	return false
}

func (e *encoder) mapValue(t *model.Type, v model.Value, depth int) error {
	if t.Key == nil || t.Elem == nil {
		return errorf("the map has no key or value type")
	}
	s := e.a.Elems(v)
	if !e.c.stringKeys(t.Key) {
		e.buf = append(e.buf, '[')
		for i := 0; i < len(s); i += 2 {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			e.buf = append(e.buf, '[')
			if err := e.value(t.Key, s[i], depth+1); err != nil {
				return at(err, index(i/2))
			}
			e.buf = append(e.buf, ',')
			if err := e.value(t.Elem, s[i+1], depth+1); err != nil {
				return at(err, index(i/2))
			}
			e.buf = append(e.buf, ']')
		}
		e.buf = append(e.buf, ']')
		return nil
	}
	e.buf = append(e.buf, '{')
	for i := 0; i < len(s); i += 2 {
		if i > 0 {
			e.buf = append(e.buf, ',')
		}
		start := len(e.buf)
		if err := e.value(t.Key, s[i], depth+1); err != nil {
			return at(err, index(i/2))
		}
		if e.buf[start] != '"' {
			return at(errorf("the key is not a string"), index(i/2))
		}
		e.buf = append(e.buf, ':')
		if err := e.value(t.Elem, s[i+1], depth+1); err != nil {
			return at(err, index(i/2))
		}
	}
	e.buf = append(e.buf, '}')
	return nil
}

func (e *encoder) uuid(v model.Value) {
	u := v.UUID()
	var buf [38]byte
	buf[0], buf[37] = '"', '"'
	hex.Encode(buf[1:9], u[0:4])
	buf[9] = '-'
	hex.Encode(buf[10:14], u[4:6])
	buf[14] = '-'
	hex.Encode(buf[15:19], u[6:8])
	buf[19] = '-'
	hex.Encode(buf[20:24], u[8:10])
	buf[24] = '-'
	hex.Encode(buf[25:], u[10:])
	e.buf = append(e.buf, buf[:]...)
}

func (e *encoder) string(s []byte) error {
	if !utf8.Valid(s) {
		return ruleErr("encoding", "the string is not UTF-8")
	}
	e.buf = appendString(e.buf, string(s))
	return nil
}

const hexDigits = "0123456789abcdef"

// AppendString appends string s in quotes by the JSON rules.
func AppendString(dst []byte, s string) []byte { return appendString(dst, s) }

// appendString appends a string in quotes: the quote, the backslash and
// control characters are escaped.
func appendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		dst = append(dst, s[start:i]...)
		switch c {
		case '"', '\\':
			dst = append(dst, '\\', c)
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		}
		start = i + 1
	}
	return append(append(dst, s[start:]...), '"')
}

// durationUnits are duration units from large to small.
var durationUnits = []struct {
	name string
	ns   int64
}{{"d", 24 * 3600e9}, {"h", 3600e9}, {"m", 60e9}, {"s", 1e9}, {"ms", 1e6}, {"ns", 1}}

// appendDuration uses the largest unit in which the duration is whole (like
// string(x) in the language).
func appendDuration(b []byte, ns int64) []byte {
	if ns == 0 {
		return append(b, "0s"...)
	}
	for _, u := range durationUnits {
		if ns%u.ns == 0 {
			return append(strconv.AppendInt(b, ns/u.ns, 10), u.name...)
		}
	}
	return b
}
