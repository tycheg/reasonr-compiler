package model

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

// Value is a value without boxing (RFC-0001, §30.1). Scalars lie in Lo and
// Hi; strings, bytes, records and collections lie in the arena (Ref):
//
//	int, bigint          Lo: a two's complement integer (32 bits for int)
//	float                Lo: math.Float64bits
//	decimal              Lo, Hi: dec.Dec.Bits, an integer divided by 10¹⁸
//	bool                 Lo: 0 or 1
//	uuid, Timer.Id       Hi: bytes 0–7, Lo: bytes 8–15 (big-endian)
//	date                 Lo: days since 0001-01-01
//	time                 Lo: nanoseconds since midnight
//	timestamp            Lo: signed Unix seconds, Hi: nanoseconds 0–999 999 999
//	duration             Lo: signed nanoseconds
//	enum                 Type: the enum, Lo: the value number
//	string, bytes        Ref: the bytes in the arena
//	record               Type: the type, Ref: the fields in schema order
//	list, set            Ref: the elements; for a set in insertion order
//	map                  Ref: keys and values in turn, in insertion order
//	page                 Ref: two values, items (list) and next (string or none)
//	pageRequest          Ref: two values, size (int) and cursor (string or none)
//	<Entity>.Id          Type: the entity or projection; a uuid in Hi/Lo, a sequence in Lo,
//	                     string and composite: one value in the arena (Ref.Len == 1)
//
// A value that refers to an arena is valid while the arena lives.
type Value struct {
	Kind Kind
	Type TypeRef // the concrete type for Enum, ID, Record
	Lo   uint64
	Hi   uint64
	Ref  ArenaRef
}

// ArenaRef is an offset and a length in an arena: in bytes for strings and
// bytes, in values for records and collections. The high bit of Off marks
// the program's static arena (constants).
type ArenaRef struct{ Off, Len uint32 }

// None is the absence of a value of T?.
var None = Value{}

// IsNone reports whether the value is absent.
func (v Value) IsNone() bool { return v.Kind == KindNone }

// Int returns an int value (the caller is responsible for the 32-bit range).
func Int(x int64) Value { return Value{Kind: KindInt, Lo: uint64(x)} }

// Bigint returns a bigint value.
func Bigint(x int64) Value { return Value{Kind: KindBigint, Lo: uint64(x)} }

// Float returns a float value.
func Float(x float64) Value { return Value{Kind: KindFloat, Lo: math.Float64bits(x)} }

// Decimal returns a decimal value.
func Decimal(d dec.Dec) Value {
	lo, hi := d.Bits()
	return Value{Kind: KindDecimal, Lo: lo, Hi: hi}
}

// Bool returns a bool value.
func Bool(b bool) Value {
	v := Value{Kind: KindBool}
	if b {
		v.Lo = 1
	}
	return v
}

// UUID returns a uuid value.
func UUID(u [16]byte) Value {
	return Value{Kind: KindUUID, Hi: binary.BigEndian.Uint64(u[:8]), Lo: binary.BigEndian.Uint64(u[8:])}
}

// epochDays is the number of days from 0001-01-01 to 1970-01-01.
const epochDays = 719162

// Date returns a date.
func Date(year int, month time.Month, day int) Value {
	t := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	return DateDays(t.Unix()/86400 + epochDays)
}

// DateDays returns a date by the number of days since 0001-01-01.
func DateDays(days int64) Value { return Value{Kind: KindDate, Lo: uint64(days)} }

// TimeOfDay returns a time of day: d since midnight, 0 ≤ d < 24h.
func TimeOfDay(d time.Duration) Value { return Value{Kind: KindTime, Lo: uint64(d)} }

// Timestamp returns an instant.
func Timestamp(t time.Time) Value {
	return Value{Kind: KindTimestamp, Lo: uint64(t.Unix()), Hi: uint64(t.Nanosecond())}
}

// Duration returns a duration.
func Duration(d time.Duration) Value { return Value{Kind: KindDuration, Lo: uint64(d)} }

// Enum returns value number i of enum t.
func Enum(t TypeRef, i int) Value { return Value{Kind: KindEnum, Type: t, Lo: uint64(i)} }

// ID returns an identifier of entity or projection t over a uuid or a
// sequence (inner is KindUUID or KindBigint). Arena.ID creates string and
// composite identifiers.
func ID(t TypeRef, inner Value) Value {
	return Value{Kind: KindID, Type: t, Lo: inner.Lo, Hi: inner.Hi}
}

// TimerID returns a timer identifier.
func TimerID(u [16]byte) Value {
	v := UUID(u)
	v.Kind = KindTimerID
	return v
}

// Int returns an integer (int, bigint, a sequence identifier).
func (v Value) Int() int64 { return int64(v.Lo) }

// Float returns a float number.
func (v Value) Float() float64 { return math.Float64frombits(v.Lo) }

// Decimal returns a decimal number.
func (v Value) Decimal() dec.Dec {
	d, _ := dec.FromBits(v.Lo, v.Hi)
	return d
}

// Bool returns a bool value.
func (v Value) Bool() bool { return v.Lo != 0 }

// UUID returns the bytes of a uuid, a Timer.Id or a uuid identifier.
func (v Value) UUID() [16]byte {
	var u [16]byte
	binary.BigEndian.PutUint64(u[:8], v.Hi)
	binary.BigEndian.PutUint64(u[8:], v.Lo)
	return u
}

// Days returns a date as the number of days since 0001-01-01.
func (v Value) Days() int64 { return int64(v.Lo) }

// Date returns a date as the instant of midnight UTC.
func (v Value) Date() time.Time {
	return time.Unix((int64(v.Lo)-epochDays)*86400, 0).UTC()
}

// Timestamp returns an instant in UTC.
func (v Value) Timestamp() time.Time { return time.Unix(int64(v.Lo), int64(v.Hi)).UTC() }

// Duration returns a duration or a time of day.
func (v Value) Duration() time.Duration { return time.Duration(v.Lo) }

// Index returns the number of an enum value.
func (v Value) Index() int { return int(v.Lo) }

// Boxed reports whether the content of an identifier lies in the arena
// (string and composite identifiers).
func (v Value) Boxed() bool { return v.Kind == KindID && v.Ref.Len != 0 }

// Record is a record (a value object, an entity or projection state, the
// arguments): field values in schema order lie in the arena.
type Record struct {
	Type TypeRef
	Ref  ArenaRef
}

// Value returns the record as a value.
func (r Record) Value() Value { return Value{Kind: KindRecord, Type: r.Type, Ref: r.Ref} }

// Record returns a record value as a Record.
func (v Value) Record() Record { return Record{Type: v.Type, Ref: v.Ref} }

// FieldValue is the value of one field (a projection row record).
type FieldValue struct {
	Field FieldRef
	Value Value
}
