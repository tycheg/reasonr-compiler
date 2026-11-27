package record

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

// Default returns a record of type t whose fields all have default values
// (RFC §7.3): this is how Env.Load answers about a missing entity. The
// record is built once and then copied from the codec's cache.
func (c *Codec) Default(a *model.Arena, t model.TypeRef) (model.Record, error) {
	if d, ok := c.cache.Load(t); ok {
		def := d.(*cached)
		return a.Copy(def.a, def.v).Record(), nil
	}
	fields, err := c.fieldsOf(t)
	if err != nil {
		return model.Record{}, err
	}
	da := model.NewArena()
	v, err := c.defaults(da, t, fields, 0)
	if err != nil {
		return model.Record{}, err
	}
	c.cache.Store(t, &cached{da, v}) // the cache arena is only read from now on
	return a.Copy(da, v).Record(), nil
}

// cached is a default record in its own arena.
type cached struct {
	a *model.Arena
	v model.Value
}

func (c *Codec) defaults(a *model.Arena, t model.TypeRef, fields []model.FieldInfo, depth int) (model.Value, error) {
	m := a.Mark()
	for i := range fields {
		x, err := c.field(a, &fields[i], depth+1)
		if err != nil {
			a.Drop(m)
			return model.None, err
		}
		a.Push(x)
	}
	return a.End(model.KindRecord, t, m), nil
}

// FieldDefault returns the default value of a field: explicit
// (FieldInfo.Default) or the type's default value; decoders fill in
// missing fields and arguments this way.
func (c *Codec) FieldDefault(a *model.Arena, f *model.FieldInfo) (model.Value, error) {
	return c.field(a, f, 0)
}

func (c *Codec) field(a *model.Arena, f *model.FieldInfo, depth int) (model.Value, error) {
	if f.Default != nil {
		v, err := c.literal(a, f.Default, &f.Type, depth)
		if err != nil {
			return v, fmt.Errorf("%w (field %s)", err, f.Name)
		}
		return v, nil
	}
	v, err := c.zero(a, &f.Type, depth)
	if err != nil {
		return v, fmt.Errorf("%w (field %s)", err, f.Name)
	}
	return v, nil
}

// Zero returns the default value of type t (RFC §7.3). Identifiers have
// none: only T? has one, and it is none.
func (c *Codec) Zero(a *model.Arena, t model.Type) (model.Value, error) {
	return c.zero(a, &t, 0)
}

// zeroTimestamp — timestamp"0001-01-01T00:00:00Z".
var zeroTimestamp = model.Timestamp(time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC))

func (c *Codec) zero(a *model.Arena, t *model.Type, depth int) (model.Value, error) {
	if depth > maxDepth {
		return model.None, fmt.Errorf("record: nesting deeper than %d", maxDepth)
	}
	if t.Optional {
		return model.None, nil
	}
	switch t.Kind {
	case model.KindInt, model.KindBigint, model.KindFloat, model.KindDecimal, model.KindBool, model.KindUUID,
		model.KindDate, model.KindTime, model.KindDuration:
		return model.Value{Kind: t.Kind}, nil
	case model.KindTimestamp:
		return zeroTimestamp, nil
	case model.KindString:
		return a.String(""), nil
	case model.KindBytes:
		return a.Bytes(nil), nil
	case model.KindEnum:
		return model.Enum(t.Ref, 0), nil
	case model.KindRecord:
		fields, err := c.fieldsOf(t.Ref)
		if err != nil {
			return model.None, err
		}
		return c.defaults(a, t.Ref, fields, depth)
	case model.KindList, model.KindSet, model.KindMap:
		return a.End(t.Kind, 0, a.Mark()), nil
	case model.KindPageRequest:
		m := a.Mark()
		a.Push(model.Int(20))
		a.Push(model.None)
		return a.End(model.KindPageRequest, 0, m), nil
	}
	return model.None, fmt.Errorf("record: type %s has no default value", t.Kind)
}

// Literal returns the value of schema constant lit of type t.
func (c *Codec) Literal(a *model.Arena, lit *model.Literal, t model.Type) (model.Value, error) {
	return c.literal(a, lit, &t, 0)
}

func (c *Codec) literal(a *model.Arena, lit *model.Literal, t *model.Type, depth int) (model.Value, error) {
	if depth > maxDepth {
		return model.None, fmt.Errorf("record: nesting deeper than %d", maxDepth)
	}
	if lit.Kind == model.KindNone {
		if !t.Optional {
			return model.None, fmt.Errorf("record: none for non-optional type %s", t.Kind)
		}
		return model.None, nil
	}
	if lit.Kind != t.Kind {
		return model.None, fmt.Errorf("record: a literal of kind %s for type %s", lit.Kind, t.Kind)
	}
	bad := func(err error) (model.Value, error) {
		return model.None, fmt.Errorf("record: literal %s %q: %v", lit.Kind, lit.Text, err)
	}
	switch t.Kind {
	case model.KindInt, model.KindBigint:
		bits := 64
		if t.Kind == model.KindInt {
			bits = 32
		}
		x, err := strconv.ParseInt(lit.Text, 10, bits)
		if err != nil {
			return bad(err)
		}
		return model.Value{Kind: t.Kind, Lo: uint64(x)}, nil
	case model.KindFloat:
		x, err := strconv.ParseFloat(lit.Text, 64)
		if err != nil {
			return bad(err)
		}
		return model.Float(x), nil
	case model.KindDecimal:
		x, err := dec.Parse(lit.Text)
		if err != nil {
			return bad(err)
		}
		return model.Decimal(x), nil
	case model.KindString:
		return a.String(lit.Text), nil
	case model.KindBytes:
		return a.Bytes([]byte(lit.Text)), nil
	case model.KindBool:
		x, err := strconv.ParseBool(lit.Text)
		if err != nil {
			return bad(err)
		}
		return model.Bool(x), nil
	case model.KindUUID:
		u, err := ParseUUID(lit.Text)
		if err != nil {
			return bad(err)
		}
		return model.UUID(u), nil
	case model.KindDate:
		x, err := time.Parse(time.DateOnly, lit.Text)
		if err != nil {
			return bad(err)
		}
		return model.Date(x.Date()), nil
	case model.KindTime:
		d, err := parseTimeOfDay(lit.Text)
		if err != nil {
			return bad(err)
		}
		return model.TimeOfDay(d), nil
	case model.KindTimestamp:
		x, err := time.Parse(time.RFC3339Nano, lit.Text)
		if err != nil {
			return bad(err)
		}
		return model.Timestamp(x), nil
	case model.KindDuration:
		x, err := strconv.ParseInt(lit.Text, 10, 64)
		if err != nil {
			return bad(err)
		}
		return model.Duration(time.Duration(x)), nil
	case model.KindEnum:
		if ti := c.schema.Type(t.Ref); ti != nil {
			for i, name := range ti.Enum {
				if name == lit.Text {
					return model.Enum(t.Ref, i), nil
				}
			}
		}
		return bad(fmt.Errorf("no such value in enum %d", t.Ref))
	case model.KindRecord:
		fields, err := c.fieldsOf(t.Ref)
		if err != nil {
			return model.None, err
		}
		if len(lit.Fields) > len(fields) {
			return bad(fmt.Errorf("%d fields, %d in the schema", len(lit.Fields), len(fields)))
		}
		m := a.Mark()
		for i := range fields {
			var x model.Value
			if i < len(lit.Fields) {
				x, err = c.literal(a, &lit.Fields[i], &fields[i].Type, depth+1)
			} else {
				x, err = c.field(a, &fields[i], depth+1)
			}
			if err != nil {
				a.Drop(m)
				return model.None, err
			}
			a.Push(x)
		}
		return a.End(model.KindRecord, t.Ref, m), nil
	case model.KindList, model.KindSet, model.KindMap:
		if t.Elem == nil || t.Kind == model.KindMap && (t.Key == nil || len(lit.Elems)%2 != 0) {
			return bad(fmt.Errorf("invalid elements"))
		}
		m := a.Mark()
		for i := range lit.Elems {
			et := t.Elem
			if t.Kind == model.KindMap && i%2 == 0 {
				et = t.Key
			}
			x, err := c.literal(a, &lit.Elems[i], et, depth+1)
			if err != nil {
				a.Drop(m)
				return model.None, err
			}
			a.Push(x)
		}
		return a.End(t.Kind, 0, m), nil
	}
	return bad(fmt.Errorf("unsupported kind"))
}

// parseTimeOfDay parses a time of day: 15:04, 15:04:05 or 15:04:05.999999999.
func parseTimeOfDay(s string) (time.Duration, error) {
	layout := "15:04"
	if strings.Count(s, ":") == 2 {
		layout = "15:04:05.999999999"
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return 0, err
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second + time.Duration(t.Nanosecond()), nil
}

// ParseUUID parses a uuid in the 8-4-4-4-12 form.
func ParseUUID(s string) ([16]byte, error) {
	var u [16]byte
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return u, fmt.Errorf("invalid uuid %q", s)
	}
	h := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:]
	if _, err := hex.Decode(u[:], []byte(h)); err != nil {
		return u, fmt.Errorf("invalid uuid %q", s)
	}
	return u, nil
}
