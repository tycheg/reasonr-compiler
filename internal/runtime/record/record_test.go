package record

import (
	"bytes"
	"errors"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

func typ(k model.Kind) model.Type { return model.Type{Kind: k} }

func ptr(t model.Type) *model.Type { return &t }

func opt(t model.Type) model.Type { t.Optional = true; return t }

func field(name string, t model.Type) model.FieldInfo {
	return model.FieldInfo{Name: name, Type: t}
}

// Type numbers of schema allKinds.
const (
	tColor model.TypeRef = iota + 1
	tPoint
	tCode
	tDoc
	tSeq
	tNamed
	tComposite
	tMart
)

// allKinds is a schema with all value kinds and identifier strategies.
func allKinds() *model.Schema {
	ref := func(k model.Kind, r model.TypeRef) model.Type { return model.Type{Kind: k, Ref: r} }
	types := []model.TypeInfo{
		{Name: "Color", Kind: model.TypeEnum, Enum: []string{"red", "green", "blue"}},
		{Name: "Point", Kind: model.TypeValue, Fields: []model.FieldInfo{
			field("x", typ(model.KindDecimal)),
			{Name: "y", Type: typ(model.KindDecimal), Default: &model.Literal{Kind: model.KindDecimal, Text: "1.5"}},
		}},
		{Name: "Code", Kind: model.TypeValue, Fields: []model.FieldInfo{
			field("prefix", typ(model.KindString)), field("n", typ(model.KindInt)),
		}},
		{Name: "Doc", Kind: model.TypeEntity, ID: model.IDUUID, Fields: []model.FieldInfo{
			field("i", typ(model.KindInt)),
			field("bi", typ(model.KindBigint)),
			field("f", typ(model.KindFloat)),
			field("d", typ(model.KindDecimal)),
			field("s", typ(model.KindString)),
			field("b", typ(model.KindBool)),
			field("u", typ(model.KindUUID)),
			field("dt", typ(model.KindDate)),
			field("tm", typ(model.KindTime)),
			field("ts", typ(model.KindTimestamp)),
			field("du", typ(model.KindDuration)),
			field("by", typ(model.KindBytes)),
			field("e", ref(model.KindEnum, tColor)),
			field("p", ref(model.KindRecord, tPoint)),
			field("op", opt(ref(model.KindRecord, tPoint))),
			field("l", model.Type{Kind: model.KindList, Elem: ptr(opt(typ(model.KindInt)))}),
			field("st", model.Type{Kind: model.KindSet, Elem: ptr(typ(model.KindString))}),
			field("m", model.Type{Kind: model.KindMap, Key: ptr(typ(model.KindString)),
				Elem: ptr(model.Type{Kind: model.KindList, Elem: ptr(ref(model.KindEnum, tColor))})}),
			field("seq", opt(ref(model.KindID, tSeq))),
			field("named", opt(ref(model.KindID, tNamed))),
			field("comp", opt(ref(model.KindID, tComposite))),
			field("mart", opt(ref(model.KindID, tMart))),
			field("self", opt(ref(model.KindID, tDoc))),
			field("timer", opt(typ(model.KindTimerID))),
			field("pg", model.Type{Kind: model.KindPage, Elem: ptr(ref(model.KindRecord, tPoint))}),
			field("pr", typ(model.KindPageRequest)),
		}},
		{Name: "Seq", Kind: model.TypeEntity, ID: model.IDSequence},
		{Name: "Named", Kind: model.TypeEntity, ID: model.IDGiven},
		{Name: "Composite", Kind: model.TypeEntity, ID: model.IDGiven, IDType: tCode},
		{Name: "Mart", Kind: model.TypeProjection, ID: model.IDDerived, IDType: tDoc},
	}
	for i := range types {
		types[i].Ref = model.TypeRef(i + 1)
		for j := range types[i].Fields {
			types[i].Fields[j].Ref = model.FieldRef(j)
		}
	}
	return &model.Schema{Types: types}
}

// gen builds random values by type.
type gen struct {
	c *Codec
	a *model.Arena
	r *rand.Rand
}

func (g *gen) value(t *model.Type, depth int) model.Value {
	if t.Optional && g.r.Intn(3) == 0 {
		return model.None
	}
	r, a := g.r, g.a
	n := r.Intn(4)
	if depth > 3 {
		n = 0
	}
	switch t.Kind {
	case model.KindInt:
		return model.Int(int64(int32(r.Uint32())))
	case model.KindBigint:
		return model.Bigint(r.Int63() - r.Int63())
	case model.KindFloat:
		return model.Float([]float64{0, -1.5, math.Inf(1), math.MaxFloat64, r.NormFloat64()}[r.Intn(5)])
	case model.KindDecimal:
		d, _ := dec.FromFloat(r.NormFloat64() * 1e6)
		return model.Decimal(d)
	case model.KindString:
		return a.String([]string{"", "a", "δέλτα", "\x00\xff"}[r.Intn(4)])
	case model.KindBytes:
		return a.Bytes([]byte{byte(r.Intn(256)), 0})
	case model.KindBool:
		return model.Bool(r.Intn(2) == 1)
	case model.KindUUID:
		return model.UUID(g.uuid())
	case model.KindTimerID:
		return model.TimerID(g.uuid())
	case model.KindDate:
		return model.DateDays(r.Int63n(3652059))
	case model.KindTime:
		return model.TimeOfDay(time.Duration(r.Int63n(int64(24 * time.Hour))))
	case model.KindTimestamp:
		return model.Timestamp(time.Unix(r.Int63n(1e11)-5e10, r.Int63n(1e9)))
	case model.KindDuration:
		return model.Duration(time.Duration(r.Int63() - r.Int63()))
	case model.KindEnum:
		return model.Enum(t.Ref, r.Intn(len(g.c.schema.Type(t.Ref).Enum)))
	case model.KindID:
		strategy, composite, _ := g.c.idKind(t.Ref)
		switch strategy {
		case model.IDUUID:
			return model.ID(t.Ref, model.UUID(g.uuid()))
		case model.IDSequence:
			return model.ID(t.Ref, model.Int(r.Int63()))
		}
		if composite != 0 {
			return a.ID(t.Ref, g.value(ptr(Type(composite)), depth+1))
		}
		return a.ID(t.Ref, g.value(ptr(typ(model.KindString)), depth+1))
	case model.KindRecord:
		m := a.Mark()
		for i := range g.c.schema.Type(t.Ref).Fields {
			a.Push(g.value(&g.c.schema.Type(t.Ref).Fields[i].Type, depth+1))
		}
		return a.End(model.KindRecord, t.Ref, m)
	case model.KindList:
		m := a.Mark()
		for range n {
			a.Push(g.value(t.Elem, depth+1))
		}
		return a.End(t.Kind, 0, m)
	case model.KindSet, model.KindMap:
		m := a.Mark()
		var keys []model.Value
	next:
		for range n {
			kt := t.Elem
			if t.Kind == model.KindMap {
				kt = t.Key
			}
			k := g.value(kt, depth+1)
			for _, x := range keys {
				if a.Equal(x, k) {
					continue next
				}
			}
			keys = append(keys, k)
			a.Push(k)
			if t.Kind == model.KindMap {
				a.Push(g.value(t.Elem, depth+1))
			}
		}
		return a.End(t.Kind, 0, m)
	case model.KindPage:
		items := g.value(&model.Type{Kind: model.KindList, Elem: t.Elem}, depth+1)
		next := g.value(&optString, depth+1)
		m := a.Mark()
		a.Push(items)
		a.Push(next)
		return a.End(model.KindPage, 0, m)
	case model.KindPageRequest:
		cursor := g.value(&optString, depth+1)
		m := a.Mark()
		a.Push(model.Int(int64(r.Intn(100))))
		a.Push(cursor)
		return a.End(model.KindPageRequest, 0, m)
	}
	panic("gen: kind " + t.Kind.String())
}

func (g *gen) uuid() [16]byte {
	var u [16]byte
	g.r.Read(u[:])
	return u
}

// roundTrip encodes a value, decodes it into another arena and compares.
func roundTrip(t *testing.T, c *Codec, a *model.Arena, typ model.Type, v model.Value) {
	t.Helper()
	data, err := c.Append(nil, a, typ, v)
	if err != nil {
		t.Fatalf("Append(%s): %v", a.Format(v), err)
	}
	b := model.NewArena()
	b.String("shift")
	got, err := c.Decode(b, typ, data)
	if err != nil {
		t.Fatalf("Decode(%s): %v", a.Format(v), err)
	}
	if !model.Equal(a, v, b, got) {
		t.Fatalf("Decode(Append(%s)) = %s", a.Format(v), b.Format(got))
	}
	again, err := c.Append(nil, b, typ, got)
	if err != nil || !bytes.Equal(again, data) {
		t.Fatalf("encoding %s again differs: %v", a.Format(v), err)
	}
	// Any truncated input is an error, not a panic or a wrong value.
	for i := range data {
		if _, err := c.Decode(b, typ, data[:i]); err == nil {
			t.Fatalf("Decode(%s[:%d]) without an error", a.Format(v), i)
		}
	}
	if _, err := c.Decode(b, typ, append(data, 0)); err == nil {
		t.Fatalf("Decode(%s + an extra byte) without an error", a.Format(v))
	}
}

func TestRoundTrip(t *testing.T) {
	c := New(allKinds())
	a := model.NewArena()
	g := &gen{c: c, a: a, r: rand.New(rand.NewSource(1))}
	for range 300 {
		a.Reset()
		roundTrip(t, c, a, Type(tDoc), g.value(ptr(Type(tDoc)), 0))
	}
}

// exampleSchemas are the schemas of the RFC examples the compiler generated.
func exampleSchemas(t *testing.T) map[string]*model.Schema {
	files, err := filepath.Glob("../../schemagen/testdata/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no example schemas: %v", err)
	}
	out := map[string]*model.Schema{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s, err := model.ParseSchema(data)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[filepath.Base(f)] = s
	}
	return out
}

func TestExampleSchemas(t *testing.T) {
	for name, s := range exampleSchemas(t) {
		c := New(s)
		a := model.NewArena()
		g := &gen{c: c, a: a, r: rand.New(rand.NewSource(2))}
		for _, ti := range s.Types {
			switch ti.Kind {
			case model.TypeEntity, model.TypeActor, model.TypeProjection, model.TypeValue:
			default:
				continue
			}
			a.Reset()
			// A value object with an identifier field has no default value.
			if r, err := c.Default(a, ti.Ref); err == nil {
				roundTrip(t, c, a, Type(ti.Ref), r.Value())
			} else if ti.Kind != model.TypeValue {
				t.Errorf("%s: Default(%s): %v", name, ti.Name, err)
			}
			for range 20 {
				roundTrip(t, c, a, Type(ti.Ref), g.value(ptr(Type(ti.Ref)), 0))
			}
			// Method arguments: events, commands, errors.
			for _, m := range ti.Methods {
				mt := m // a local copy for pointers
				for range 5 {
					m := a.Mark()
					for i := range mt.Params {
						a.Push(g.value(&mt.Params[i].Type, 1))
					}
					args := a.End(model.KindRecord, 0, m).Record()
					data, err := c.AppendFields(nil, a, mt.Params, args)
					if err != nil {
						t.Fatalf("%s: %s.%s: %v", name, ti.Name, mt.Name, err)
					}
					got, err := c.DecodeFields(a, 0, mt.Params, data)
					if err != nil || !a.Equal(got.Value(), args.Value()) {
						t.Fatalf("%s: %s.%s: %s ≠ %s (%v)", name, ti.Name, mt.Name, a.Format(got.Value()), a.Format(args.Value()), err)
					}
				}
			}
		}
	}
}

func TestDefaults(t *testing.T) {
	s := exampleSchemas(t)["bank.json"]
	c := New(s)
	a := model.NewArena()
	acc := s.Lookup("Account")
	r, err := c.Default(a, acc.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Format(r.Value()); got != "T"+itoa(acc.Ref)+"(none, 0, 0, false)" {
		t.Errorf("Default(Account) = %s", got)
	}

	c = New(allKinds())
	r, err = c.Default(a, tDoc)
	if err == nil {
		t.Errorf("Default(Doc) without an error: a page has no default value: %s", a.Format(r.Value()))
	}
	p, err := c.Default(a, tPoint)
	if err != nil || a.Format(p.Value()) != "T2(0, 1.5)" {
		t.Errorf("Default(Point) = %s, %v", a.Format(p.Value()), err)
	}
	for _, tc := range []struct {
		lit  model.Literal
		typ  model.Type
		want string
	}{
		{model.Literal{Kind: model.KindInt, Text: "-7"}, typ(model.KindInt), "-7"},
		{model.Literal{Kind: model.KindBigint, Text: "4294967296"}, typ(model.KindBigint), "4294967296"},
		{model.Literal{Kind: model.KindFloat, Text: "2.5"}, typ(model.KindFloat), "2.5"},
		{model.Literal{Kind: model.KindDecimal, Text: "0.333333333333333333333"}, typ(model.KindDecimal), "0.333333333333333333"},
		{model.Literal{Kind: model.KindString, Text: "x"}, typ(model.KindString), `"x"`},
		{model.Literal{Kind: model.KindBool, Text: "true"}, typ(model.KindBool), "true"},
		{model.Literal{Kind: model.KindUUID, Text: "00000000-0000-0000-0000-000000000001"}, typ(model.KindUUID), "00000000-0000-0000-0000-000000000001"},
		{model.Literal{Kind: model.KindDate, Text: "0001-01-01"}, typ(model.KindDate), "0001-01-01"},
		{model.Literal{Kind: model.KindTime, Text: "00:00"}, typ(model.KindTime), "00:00:00"},
		{model.Literal{Kind: model.KindTime, Text: "13:45:01.5"}, typ(model.KindTime), "13:45:01.5"},
		{model.Literal{Kind: model.KindTimestamp, Text: "0001-01-01T00:00:00Z"}, typ(model.KindTimestamp), "0001-01-01T00:00:00Z"},
		{model.Literal{Kind: model.KindDuration, Text: "60000000000"}, typ(model.KindDuration), "1m0s"},
		{model.Literal{Kind: model.KindEnum, Text: "green"}, model.Type{Kind: model.KindEnum, Ref: tColor}, "T1#1"},
		{model.Literal{Kind: model.KindNone}, opt(typ(model.KindInt)), "none"},
		{model.Literal{Kind: model.KindRecord, Fields: []model.Literal{{Kind: model.KindDecimal, Text: "2"}}},
			Type(tPoint), "T2(2, 1.5)"},
		{model.Literal{Kind: model.KindMap, Elems: []model.Literal{
			{Kind: model.KindString, Text: "k"}, {Kind: model.KindList, Elems: []model.Literal{{Kind: model.KindEnum, Text: "blue"}}},
		}}, allKinds().Type(tDoc).Field("m").Type, `["k": [T1#2]]`},
	} {
		v, err := c.Literal(a, &tc.lit, tc.typ)
		if err != nil || a.Format(v) != tc.want {
			t.Errorf("Literal(%+v) = %s, %v, want %s", tc.lit, a.Format(v), err, tc.want)
		}
	}
	for _, tc := range []struct {
		lit model.Literal
		typ model.Type
	}{
		{model.Literal{Kind: model.KindNone}, typ(model.KindInt)},
		{model.Literal{Kind: model.KindInt, Text: "x"}, typ(model.KindInt)},
		{model.Literal{Kind: model.KindInt, Text: "4294967296"}, typ(model.KindInt)},
		{model.Literal{Kind: model.KindInt, Text: "1"}, typ(model.KindString)},
		{model.Literal{Kind: model.KindEnum, Text: "purple"}, model.Type{Kind: model.KindEnum, Ref: tColor}},
		{model.Literal{Kind: model.KindUUID, Text: "nope"}, typ(model.KindUUID)},
		{model.Literal{Kind: model.KindTime, Text: "25:00"}, typ(model.KindTime)},
	} {
		if v, err := c.Literal(a, &tc.lit, tc.typ); err == nil {
			t.Errorf("Literal(%+v) = %s without an error", tc.lit, a.Format(v))
		}
	}
	if _, err := c.Zero(a, model.Type{Kind: model.KindID, Ref: tDoc}); err == nil {
		t.Error("an identifier has a default value")
	}
	if a.Mark() != 0 {
		t.Errorf("the arena stack is not empty: %d", a.Mark())
	}
}

func itoa(r model.TypeRef) string { return strconv.Itoa(int(r)) }

func TestOldVersion(t *testing.T) {
	// A record of an earlier schema version lacks the last field: it gets its default value.
	c := New(allKinds())
	a := model.NewArena()
	data := []byte{1}                        // one field of two
	data = append(data, make([]byte, 16)...) // x = 0
	p, err := c.DecodeRecord(a, tPoint, data)
	if err != nil || a.Format(p.Value()) != "T2(0, 1.5)" {
		t.Errorf("DecodeRecord(old version) = %s, %v", a.Format(p.Value()), err)
	}
	// The full record is encoded now.
	full, err := c.AppendRecord(nil, a, p)
	if err != nil || full[0] != 2 || c.Schema() == nil {
		t.Errorf("AppendRecord = %v, %v", full, err)
	}
}

func TestCorrupt(t *testing.T) {
	c := New(allKinds())
	a := model.NewArena()
	set := model.Type{Kind: model.KindSet, Elem: ptr(typ(model.KindInt))}
	for _, tc := range []struct {
		name string
		typ  model.Type
		data []byte
	}{
		{"bool 2", typ(model.KindBool), []byte{2}},
		{"int outside 32 bits", typ(model.KindInt), []byte{0x80, 0x80, 0x80, 0x80, 0x10}},
		{"optional flag", opt(typ(model.KindBool)), []byte{2, 1}},
		{"enum out of range", model.Type{Kind: model.KindEnum, Ref: tColor}, []byte{3}},
		{"decimal out of range", typ(model.KindDecimal), bytes.Repeat([]byte{0x7f}, 16)},
		{"nanoseconds", typ(model.KindTimestamp), []byte{0, 0x80, 0x94, 0xeb, 0xdc, 0x03}},
		{"time of day", typ(model.KindTime), []byte{0x80, 0x80, 0xa0, 0xbf, 0x9c, 0xa8, 0x2c}},
		{"length exceeds the data", typ(model.KindString), []byte{5, 'a'}},
		{"huge length", typ(model.KindString), []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}},
		{"more fields than the schema", Type(tPoint), []byte{3}},
		{"repeat in a set", set, []byte{2, 2, 2}},
		{"repeated map key", allKinds().Type(tDoc).Field("m").Type, []byte{2, 1, 'k', 0, 1, 'k', 0}},
	} {
		if v, err := c.Decode(a, tc.typ, tc.data); err == nil {
			t.Errorf("%s: Decode = %s without an error", tc.name, a.Format(v))
		} else if !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: error %v is not ErrCorrupt", tc.name, err)
		}
	}
	if a.Mark() != 0 {
		t.Errorf("the arena stack is not empty after errors: %d", a.Mark())
	}

	// A value of the wrong kind or type is not encoded.
	for _, tc := range []struct {
		typ model.Type
		v   model.Value
	}{
		{typ(model.KindInt), model.None},
		{typ(model.KindInt), model.Bool(true)},
		{typ(model.KindInt), model.Int(1 << 40)},
		{typ(model.KindBigint), model.Int(1)},
		{Type(tPoint), a.Record(tCode, a.String(""), model.Int(0)).Value()},
		{Type(tPoint), a.Record(tPoint, model.Decimal(dec.Dec{})).Value()},
		{model.Type{Kind: model.KindID, Ref: tSeq}, model.ID(tDoc, model.Int(1))},
		{model.Type{Kind: model.KindID, Ref: tNamed}, model.ID(tNamed, model.Int(1))},
		{model.Type{Kind: model.KindID, Ref: tSeq}, a.ID(tSeq, a.String("x"))},
	} {
		if _, err := c.Append(nil, a, tc.typ, tc.v); err == nil {
			t.Errorf("Append(%s as %s) without an error", a.Format(tc.v), tc.typ.Kind)
		}
	}
}

func FuzzDecode(f *testing.F) {
	c := New(allKinds())
	a := model.NewArena()
	g := &gen{c: c, a: a, r: rand.New(rand.NewSource(3))}
	for range 20 {
		data, err := c.Append(nil, a, Type(tDoc), g.value(ptr(Type(tDoc)), 0))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		a.Reset()
		v, err := c.Decode(a, Type(tDoc), data)
		if a.Mark() != 0 {
			t.Fatalf("the arena stack is not empty: %d", a.Mark())
		}
		if err != nil {
			return
		}
		again, err := c.Append(nil, a, Type(tDoc), v)
		if err != nil {
			t.Fatalf("Append(Decode): %v", err)
		}
		// Compare bytes: a float NaN is not Equal to itself.
		w, err := c.Decode(a, Type(tDoc), again)
		if err != nil {
			t.Fatalf("Decode(Append(Decode)): %v", err)
		}
		third, err := c.Append(nil, a, Type(tDoc), w)
		if err != nil || !bytes.Equal(third, again) {
			t.Fatalf("encoding is unstable: %v", err)
		}
	})
}

func TestAllocs(t *testing.T) {
	c := New(allKinds())
	a := model.NewArena()
	g := &gen{c: c, a: a, r: rand.New(rand.NewSource(4))}
	ct := Type(tCode)
	v := g.value(&ct, 0)
	data, _ := c.Append(nil, a, ct, v)
	buf := make([]byte, 0, 256)
	run := func() {
		a.Reset()
		x, err := c.Decode(a, ct, data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Append(buf[:0], a, ct, x); err != nil {
			t.Fatal(err)
		}
	}
	run()
	if n := testing.AllocsPerRun(100, run); n != 0 {
		t.Errorf("allocations per decode and encode: %v", n)
	}
}
