package ir

import (
	"math/big"
	"strconv"
	"strings"
)

// Type is the type of a slot or a constant.
type Type struct {
	Kind TypeKind
	Name string // Enum, Value, ID, Ref, Record: the name of the declaring type
	Elem *Type  // Optional, List, Set, Page, Map (the value)
	Key  *Type  // Map
}

// TypeKind is the kind of a type.
type TypeKind uint8

const (
	TInt TypeKind = iota
	TBigint
	TFloat
	TDecimal
	TString
	TBool
	TUUID
	TDate
	TTime
	TTimestamp
	TDuration
	TBytes
	TEnum
	TValue  // a value object
	TID     // an entity, actor or projection identifier
	TTimer  // Timer.Id
	TRef    // [x] in commands: a reference without field access
	TRecord // an entity or projection record
	TCaller
	TPageReq
	TList
	TSet
	TMap
	TPage
	TOptional
	tKindCount
)

var typeKindNames = [...]string{
	TInt: "int", TBigint: "bigint", TFloat: "float", TDecimal: "decimal", TString: "string", TBool: "bool",
	TUUID: "uuid", TDate: "date", TTime: "time", TTimestamp: "timestamp", TDuration: "duration", TBytes: "bytes",
	TEnum: "enum", TValue: "value", TID: "id", TTimer: "Timer.Id", TRef: "ref", TRecord: "record",
	TCaller: "Caller", TPageReq: "PageRequest", TList: "list", TSet: "set", TMap: "map", TPage: "page",
	TOptional: "optional",
}

// Basic returns a primitive type.
func Basic(k TypeKind) *Type { return &Type{Kind: k} }

// Optional returns T? (T if T is already optional).
func Optional(t *Type) *Type {
	if t.Kind == TOptional {
		return t
	}
	return &Type{Kind: TOptional, Elem: t}
}

// Unwrap returns T for T? and the type itself otherwise.
func (t *Type) Unwrap() *Type {
	if t.Kind == TOptional {
		return t.Elem
	}
	return t
}

func (t *Type) String() string {
	if t == nil {
		return "void"
	}
	switch t.Kind {
	case TEnum, TValue, TRecord:
		return t.Name
	case TID:
		return t.Name + ".Id"
	case TRef:
		return "[" + t.Name + "]"
	case TOptional:
		return t.Elem.String() + "?"
	case TList, TSet, TPage:
		return typeKindNames[t.Kind] + "<" + t.Elem.String() + ">"
	case TMap:
		return "map<" + t.Key.String() + ", " + t.Elem.String() + ">"
	}
	if int(t.Kind) < len(typeKindNames) {
		return typeKindNames[t.Kind]
	}
	return "?"
}

// Equal compares types.
func (t *Type) Equal(u *Type) bool {
	if t == nil || u == nil {
		return t == u
	}
	if t.Kind != u.Kind || t.Name != u.Name {
		return false
	}
	return t.Elem.Equal(u.Elem) && t.Key.Equal(u.Key)
}

// Const is a constant. Text is the canonical form of the value: an integer,
// an exact decimal fraction, a floating-point number (strconv 'g'), a
// string, true/false, a duration in nanoseconds, a date or a time in source
// form, an enum value name. For none Text is empty.
type Const struct {
	Type *Type
	None bool
	Text string
}

// Key returns the pool key of a constant.
func (c Const) Key() string {
	if c.None {
		return "none:" + c.Type.String()
	}
	return c.Type.String() + ":" + c.Text
}

func (c Const) String() string {
	if c.None {
		return "none"
	}
	switch c.Type.Kind {
	case TInt, TBool:
		return c.Text
	case TString:
		return strconv.Quote(c.Text)
	case TEnum:
		return c.Type.Name + "." + c.Text
	case TDuration:
		return formatDuration(c.Text)
	case TDate, TTime, TTimestamp:
		return c.Type.String() + "(" + strconv.Quote(c.Text) + ")"
	}
	return c.Type.String() + "(" + c.Text + ")"
}

// Durations in nanoseconds.
var durationUnits = []struct {
	name string
	ns   int64
}{{"d", 24 * 3600e9}, {"h", 3600e9}, {"m", 60e9}, {"s", 1e9}, {"ms", 1e6}}

// ParseDuration parses a duration literal (500ms, 30s, 5m, 2h, 1d) into
// nanoseconds.
func ParseDuration(lit string) (int64, bool) {
	lit = strings.ReplaceAll(lit, "_", "")
	for _, u := range []string{"ms", "s", "m", "h", "d"} {
		if !strings.HasSuffix(lit, u) {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSuffix(lit, u), 10, 64)
		if err != nil {
			continue
		}
		for _, du := range durationUnits {
			if du.name == u {
				if n > (1<<63-1)/du.ns || n < -(1<<63-1)/du.ns {
					return 0, false
				}
				return n * du.ns, true
			}
		}
	}
	return 0, false
}

// formatDuration prints nanoseconds in the largest exact unit: 1m, 90s.
func formatDuration(text string) string {
	ns, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return "duration(" + text + ")"
	}
	if ns == 0 {
		return "0s"
	}
	for _, u := range durationUnits {
		if ns%u.ns == 0 {
			return strconv.FormatInt(ns/u.ns, 10) + u.name
		}
	}
	return strconv.FormatInt(ns, 10) + "ns"
}

// RatText returns the canonical form of an exact number: an integer or a
// finite decimal fraction; ok == false if the fraction is infinite.
func RatText(r *big.Rat) (string, bool) {
	if r.IsInt() {
		return r.Num().String(), true
	}
	// A finite decimal fraction: the denominator is 2^a·5^b.
	d := new(big.Int).Set(r.Denom())
	digits := 0
	two, five, zero := big.NewInt(2), big.NewInt(5), new(big.Int)
	m := new(big.Int)
	for twos, fives := 0, 0; ; {
		if m.Mod(d, two).Cmp(zero) == 0 {
			d.Div(d, two)
			twos++
		} else if m.Mod(d, five).Cmp(zero) == 0 {
			d.Div(d, five)
			fives++
		} else {
			digits = max(twos, fives)
			break
		}
	}
	if d.Cmp(big.NewInt(1)) != 0 {
		return "", false
	}
	return r.FloatString(digits), true
}

func (k TypeKind) String() string {
	if int(k) < len(typeKindNames) {
		return typeKindNames[k]
	}
	return "?"
}
