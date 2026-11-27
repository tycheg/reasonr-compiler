package jsonv

import (
	"errors"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
	"github.com/tycheg/reasonr-compiler/internal/runtime/record"
)

func typ(k model.Kind) model.Type { return model.Type{Kind: k} }

func ptr(t model.Type) *model.Type { return &t }

func opt(t model.Type) model.Type { t.Optional = true; return t }

func ref(k model.Kind, r model.TypeRef) model.Type { return model.Type{Kind: k, Ref: r} }

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

// allKinds is a schema with all value kinds and identifier strategies (as
// in runtime/record), plus a map with keys of every kind of record.
func allKinds() *model.Schema {
	types := []model.TypeInfo{
		{Name: "Color", Kind: model.TypeEnum, Enum: []string{"red", "green", "blue"}},
		{Name: "Point", Kind: model.TypeValue, Fields: []model.FieldInfo{
			field("x", typ(model.KindDecimal)),
			{Name: "y", Type: typ(model.KindDecimal), Default: &model.Literal{Kind: model.KindDecimal, Text: "1.5"}, WasName: "yy"},
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
			field("me", model.Type{Kind: model.KindMap, Key: ptr(ref(model.KindEnum, tColor)), Elem: ptr(typ(model.KindInt))}),
			field("mn", model.Type{Kind: model.KindMap, Key: ptr(ref(model.KindID, tNamed)), Elem: ptr(typ(model.KindBool))}),
			field("mi", model.Type{Kind: model.KindMap, Key: ptr(typ(model.KindInt)), Elem: ptr(opt(typ(model.KindString)))}),
			field("mc", model.Type{Kind: model.KindMap, Key: ptr(ref(model.KindID, tComposite)), Elem: ptr(typ(model.KindInt))}),
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

// gen builds random values representable in JSON: without infinities and
// with UTF-8 in strings.
type gen struct {
	s *model.Schema
	a *model.Arena
	r *rand.Rand
}

func (g *gen) uuid() [16]byte {
	var u [16]byte
	g.r.Read(u[:])
	return u
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
		return model.Float([]float64{0, -1.5, math.MaxFloat64, math.SmallestNonzeroFloat64, r.NormFloat64(), 1e21}[r.Intn(6)])
	case model.KindDecimal:
		d, _ := dec.FromFloat(r.NormFloat64() * 1e6)
		return model.Decimal(d)
	case model.KindString:
		return a.String([]string{"", "a", "δέλτα", "\x00\"\\\n\t ", "😀"}[r.Intn(5)])
	case model.KindBytes:
		return a.Bytes([]byte{byte(r.Intn(256)), 0, 0xff}[:r.Intn(4)])
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
		return model.Duration([]time.Duration{0, 90 * time.Second, -2 * time.Hour, 1500 * time.Millisecond,
			time.Duration(r.Int63() - r.Int63()), math.MinInt64, math.MaxInt64}[r.Intn(7)])
	case model.KindEnum:
		return model.Enum(t.Ref, r.Intn(len(g.s.Type(t.Ref).Enum)))
	case model.KindID:
		strategy, composite, _ := idKind(g.s, t.Ref)
		switch strategy {
		case model.IDUUID:
			return model.ID(t.Ref, model.UUID(g.uuid()))
		case model.IDSequence:
			return model.ID(t.Ref, model.Bigint(r.Int63()))
		}
		if composite != 0 {
			return a.ID(t.Ref, g.value(ptr(ref(model.KindRecord, composite)), depth+1))
		}
		return a.ID(t.Ref, g.value(ptr(typ(model.KindString)), depth+1))
	case model.KindRecord:
		fields := g.s.Type(t.Ref).Fields
		vals := make([]model.Value, len(fields))
		for i := range fields {
			vals[i] = g.value(&fields[i].Type, depth+1)
		}
		return a.Record(t.Ref, vals...).Value()
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
		a.Push(model.Int(int64(r.Intn(101))))
		a.Push(cursor)
		return a.End(model.KindPageRequest, 0, m)
	}
	panic("gen: kind " + t.Kind.String())
}

// roundTrip: value → JSON → value equals the original, encoding again gives
// the same bytes, a truncated input is an error.
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
		t.Fatalf("Decode(%s): %v", data, err)
	}
	if !model.Equal(a, v, b, got) {
		t.Fatalf("Decode(Append(%s)) = %s\n%s", a.Format(v), b.Format(got), data)
	}
	again, err := c.Append(nil, b, typ, got)
	if err != nil || string(again) != string(data) {
		t.Fatalf("encoding again differs: %v\n%s\n%s", err, data, again)
	}
	for _, i := range []int{0, len(data) / 2, len(data) - 1} {
		if _, err := c.Decode(b, typ, data[:i]); err == nil {
			t.Fatalf("Decode(%s) without an error", data[:i])
		}
	}
}

func TestRoundTrip(t *testing.T) {
	s := allKinds()
	c := New(s)
	a := model.NewArena()
	g := &gen{s: s, a: a, r: rand.New(rand.NewSource(1))}
	for range 500 {
		a.Reset()
		roundTrip(t, c, a, ref(model.KindRecord, tDoc), g.value(ptr(ref(model.KindRecord, tDoc)), 0))
	}
}

// FuzzRoundTrip is a round trip of random values (plan, stage E1).
func FuzzRoundTrip(f *testing.F) {
	for _, seed := range []int64{1, 2, 3, 42} {
		f.Add(seed)
	}
	s := allKinds()
	c := New(s)
	f.Fuzz(func(t *testing.T, seed int64) {
		a := model.NewArena()
		g := &gen{s: s, a: a, r: rand.New(rand.NewSource(seed))}
		roundTrip(t, c, a, ref(model.KindRecord, tDoc), g.value(ptr(ref(model.KindRecord, tDoc)), 0))
	})
}

// FuzzDecode: any input either gives an error or a value that encodes and
// decodes again into an equal one.
func FuzzDecode(f *testing.F) {
	s := allKinds()
	c := New(s)
	a := model.NewArena()
	g := &gen{s: s, a: a, r: rand.New(rand.NewSource(7))}
	for range 5 {
		data, err := c.Append(nil, a, ref(model.KindRecord, tDoc), g.value(ptr(ref(model.KindRecord, tDoc)), 0))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte(`{"p":{"x":"1"},"l":[1,null],"du":"1h30m","mi":[[1,"a"]]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		a := model.NewArena()
		typ := ref(model.KindRecord, tDoc)
		v, err := c.Decode(a, typ, data)
		if err != nil {
			var e *Error
			if !errors.As(err, &e) {
				t.Fatalf("the error is not *Error: %v", err)
			}
			return
		}
		out, err := c.Append(nil, a, typ, v)
		if err != nil {
			t.Fatalf("the decoded value does not encode: %v", err)
		}
		b := model.NewArena()
		w, err := c.Decode(b, typ, out)
		if err != nil || !model.Equal(a, v, b, w) {
			t.Fatalf("decoding %s again: %v", out, err)
		}
	})
}

// TestCanonical: the exact form of values of every kind.
func TestCanonical(t *testing.T) {
	s := allKinds()
	c := New(s)
	a := model.NewArena()
	d, _ := dec.Parse("190.50")
	u := [16]byte{0x01, 0x92, 0xb6, 0xc1, 15: 0xff}
	page := func() model.Value {
		m := a.Mark()
		a.Push(a.List(a.Record(tPoint, model.Decimal(d), model.Decimal(dec.FromInt(1))).Value()))
		a.Push(a.String("c1"))
		return a.End(model.KindPage, 0, m)
	}
	pageReq := func() model.Value {
		m := a.Mark()
		a.Push(model.Int(20))
		a.Push(model.None)
		return a.End(model.KindPageRequest, 0, m)
	}
	mapOf := func(vals ...model.Value) model.Value {
		m := a.Mark()
		for _, v := range vals {
			a.Push(v)
		}
		return a.End(model.KindMap, 0, m)
	}
	for _, c2 := range []struct {
		t    model.Type
		v    model.Value
		want string
	}{
		{typ(model.KindInt), model.Int(-42), `-42`},
		{typ(model.KindBigint), model.Bigint(9007199254740993), `"9007199254740993"`},
		{typ(model.KindFloat), model.Float(55.75), `55.75`},
		{typ(model.KindFloat), model.Float(1e21), `1e+21`},
		{typ(model.KindDecimal), model.Decimal(d), `"190.5"`},
		{typ(model.KindString), a.String("é\"\\\n\x01"), `"é\"\\\n\u0001"`},
		{typ(model.KindBytes), a.Bytes([]byte("hi!")), `"aGkh"`},
		{typ(model.KindBool), model.Bool(true), `true`},
		{typ(model.KindUUID), model.UUID(u), `"0192b6c1-0000-0000-0000-0000000000ff"`},
		{typ(model.KindTimerID), model.TimerID(u), `"0192b6c1-0000-0000-0000-0000000000ff"`},
		{typ(model.KindDate), model.Date(2026, 10, 4), `"2026-10-04"`},
		{typ(model.KindTime), model.TimeOfDay(15*time.Hour + 4*time.Minute + 5*time.Second + 500*time.Millisecond), `"15:04:05.5"`},
		{typ(model.KindTimestamp), model.Timestamp(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)), `"2026-01-01T10:00:00Z"`},
		{typ(model.KindDuration), model.Duration(90 * time.Second), `"90s"`},
		{typ(model.KindDuration), model.Duration(90 * time.Minute), `"90m"`},
		{typ(model.KindDuration), model.Duration(-48 * time.Hour), `"-2d"`},
		{typ(model.KindDuration), model.Duration(1500 * time.Microsecond), `"1500000ns"`},
		{typ(model.KindDuration), model.Duration(0), `"0s"`},
		{ref(model.KindEnum, tColor), model.Enum(tColor, 1), `"green"`},
		{opt(ref(model.KindEnum, tColor)), model.None, `null`},
		{ref(model.KindRecord, tPoint), a.Record(tPoint, model.Decimal(d), model.Decimal(dec.FromInt(1))).Value(), `{"x":"190.5","y":"1"}`},
		{ref(model.KindID, tDoc), model.ID(tDoc, model.UUID(u)), `"0192b6c1-0000-0000-0000-0000000000ff"`},
		{ref(model.KindID, tMart), model.ID(tMart, model.UUID(u)), `"0192b6c1-0000-0000-0000-0000000000ff"`},
		{ref(model.KindID, tSeq), model.ID(tSeq, model.Bigint(7)), `"7"`},
		{ref(model.KindID, tNamed), a.ID(tNamed, a.String("A1")), `"A1"`},
		{ref(model.KindID, tComposite), a.ID(tComposite, a.Record(tCode, a.String("X"), model.Int(2)).Value()), `{"prefix":"X","n":2}`},
		{model.Type{Kind: model.KindList, Elem: ptr(opt(typ(model.KindInt)))}, a.List(model.Int(1), model.None), `[1,null]`},
		{s.Types[tDoc-1].Fields[18].Type, mapOf(model.Enum(tColor, 2), model.Int(1)), `{"blue":1}`},
		{s.Types[tDoc-1].Fields[19].Type, mapOf(a.ID(tNamed, a.String("k")), model.Bool(false)), `{"k":false}`},
		{s.Types[tDoc-1].Fields[20].Type, mapOf(model.Int(3), model.None), `[[3,null]]`},
		{model.Type{Kind: model.KindPage, Elem: ptr(ref(model.KindRecord, tPoint))}, page(), `{"items":[{"x":"190.5","y":"1"}],"next":"c1"}`},
		{typ(model.KindPageRequest), pageReq(), `{"size":20,"cursor":null}`},
	} {
		got, err := c.Append(nil, a, c2.t, c2.v)
		if err != nil || string(got) != c2.want {
			t.Errorf("%s: %s, %v; expected %s", a.Format(c2.v), got, err, c2.want)
			continue
		}
		back, err := c.Decode(a, c2.t, got)
		if err != nil || !a.Equal(back, c2.v) {
			t.Errorf("%s: back %s, %v", c2.want, a.Format(back), err)
		}
	}
}

// TestDecodeInputs: what is accepted on input beyond the canonical form,
// default values, was and errors with a path.
func TestDecodeInputs(t *testing.T) {
	s := allKinds()
	c := New(s)
	a := model.NewArena()
	point := ref(model.KindRecord, tPoint)
	for _, c2 := range []struct {
		t    model.Type
		in   string
		want string // Format
	}{
		{typ(model.KindBigint), `12`, `12`},
		{typ(model.KindDecimal), `1.25`, `1.25`},
		{typ(model.KindDuration), `"1h30m"`, `1h30m0s`},
		{typ(model.KindDuration), `"-1m30s"`, `-1m30s`},
		{typ(model.KindTimestamp), `"2026-01-01T13:00:00+03:00"`, `2026-01-01T10:00:00Z`},
		{ref(model.KindID, tSeq), `5`, `T5.Id(5)`},
		{point, `{"x": "2"}`, `T2(2, 1.5)`},
		{point, ` { "yy" : "3" , "x" : "2" } `, `T2(2, 3)`},
		{opt(point), `null`, `none`},
		{typ(model.KindString), `"A😀"`, `"A😀"`},
	} {
		v, err := c.Decode(a, c2.t, []byte(c2.in))
		if err != nil || a.Format(v) != c2.want {
			t.Errorf("%s: %s, %v; expected %s", c2.in, a.Format(v), err, c2.want)
		}
	}
	doc := ref(model.KindRecord, tDoc)
	for _, c2 := range []struct {
		t        model.Type
		in, path string
		msg      string
	}{
		{point, `{"x": "a"}`, "x", "invalid decimal"},
		{doc, `{"op": {"x": 1, "z": 2}}`, "op.z", "no such field"},
		{doc, `{"l": [1, "2"]}`, "l[1]", "expected a number"},
		{doc, `{"st": ["a", "a"]}`, "st[1]", "repeated"},
		{doc, `{"m": {"k": ["red", "pink"]}}`, "m[0][1]", "no value"},
		{doc, `{"mi": [[1, "a"], [1, "b"]]}`, "mi[1]", "repeated"},
		{doc, `{"i": 2147483648}`, "i", "out of range"},
		{doc, `{"e": null}`, "e", "null for non-optional"},
		{doc, `{"i": 1, "i": 2}`, "i", "repeated"},
		{doc, `{"pr": {"cursor": null}}`, "pr", "no size"},
		{doc, `{"du": "5w"}`, "du", "invalid duration"},
		{doc, `{"du": "9223372036854775808ns"}`, "du", "invalid duration"},
		{typ(model.KindString), `"a" "b"`, "", "extra data"},
		{typ(model.KindString), "\"\xff\"", "", "UTF-8"},
		{typ(model.KindInt), `01`, "", "expected a number"},
	} {
		_, err := c.Decode(a, c2.t, []byte(c2.in))
		var e *Error
		if !errors.As(err, &e) || e.Path != c2.path || !strings.Contains(e.Msg, c2.msg) {
			t.Errorf("%s: error %v; expected path %q and %q", c2.in, err, c2.path, c2.msg)
		}
	}
	// Fields of an earlier model version are skipped if so configured.
	lenient := New(s)
	lenient.IgnoreUnknown = true
	if v, err := lenient.Decode(a, point, []byte(`{"x":"1","gone":{"a":[1,{"b":null}]}}`)); err != nil || a.Format(v) != `T2(1, 1.5)` {
		t.Errorf("skipping unknown: %s, %v", a.Format(v), err)
	}
	// Missing record fields get default values, as in record.
	for _, tr := range []model.TypeRef{tPoint, tCode} {
		v, err := c.Decode(a, ref(model.KindRecord, tr), []byte(`{}`))
		def, derr := record.New(s).Default(a, tr)
		if err != nil || derr != nil || !a.Equal(v, def.Value()) {
			t.Errorf("empty object %d: %s, %v", tr, a.Format(v), err)
		}
	}
	if _, err := c.Decode(a, doc, []byte(`{}`)); err == nil {
		t.Error("a page has no default value: no error")
	}
	// Personal values of the store are none even for a non-optional type:
	// the value lies separately as a ciphertext.
	sealedFields := append([]model.FieldInfo(nil), s.Types[tPoint-1].Fields...)
	sealedFields[0].Personal = &model.Personal{}
	sealedRec := a.Record(0, model.None, model.Decimal(dec.FromInt(2)))
	if _, err := c.AppendFields(nil, a, sealedFields, sealedRec); err == nil {
		t.Error("none for a non-optional personal value without SealedPersonal: no error")
	}
	sealedCodec := New(s)
	sealedCodec.SealedPersonal = true
	if b, err := sealedCodec.AppendFields(nil, a, sealedFields, sealedRec); err != nil || string(b) != `{"x":null,"y":"2"}` {
		t.Errorf("SealedPersonal: %s, %v", b, err)
	} else if v, err := sealedCodec.DecodeFields(a, 0, sealedFields, b); err != nil || a.Elems(v.Value())[0].Kind != model.KindNone {
		t.Errorf("SealedPersonal decode: %v", err)
	}
	// Method arguments.
	args, err := c.DecodeFields(a, 0, s.Types[tPoint-1].Fields, []byte(`{"x":"4"}`))
	if err != nil || a.Format(args.Value()) != `T0(4, 1.5)` {
		t.Errorf("DecodeFields: %s, %v", a.Format(args.Value()), err)
	}
	out, err := c.AppendFields(nil, a, s.Types[tPoint-1].Fields, args)
	if err != nil || string(out) != `{"x":"4","y":"1.5"}` {
		t.Errorf("AppendFields: %s, %v", out, err)
	}
}

// TestEncodeErrors: values that do not exist in JSON or do not fit the type
// are an error with a path.
func TestEncodeErrors(t *testing.T) {
	s := allKinds()
	c := New(s)
	a := model.NewArena()
	point := a.Record(tPoint, model.Decimal(dec.FromInt(1)), model.Float(1)).Value()
	for _, c2 := range []struct {
		t    model.Type
		v    model.Value
		path string
	}{
		{typ(model.KindFloat), model.Float(math.Inf(1)), ""},
		{typ(model.KindFloat), model.Float(math.NaN()), ""},
		{typ(model.KindString), a.String("\xff"), ""},
		{typ(model.KindInt), model.None, ""},
		{ref(model.KindRecord, tPoint), point, "y"},
		{model.Type{Kind: model.KindList, Elem: ptr(typ(model.KindInt))}, a.List(model.Int(1), model.Bool(true)), "[1]"},
		{ref(model.KindEnum, tColor), model.Enum(tColor, 9), ""},
	} {
		_, err := c.Append(nil, a, c2.t, c2.v)
		var e *Error
		if !errors.As(err, &e) || e.Path != c2.path {
			t.Errorf("%s: %v; expected path %q", a.Format(c2.v), err, c2.path)
		}
	}
}

// TestDecodeArgs: a missing argument and null are none (the interpreter
// substitutes default values), errors carry the rule.
func TestDecodeArgs(t *testing.T) {
	s := allKinds()
	c := New(s)
	a := model.NewArena()
	params := s.Types[tPoint-1].Fields
	for in, want := range map[string]string{``: `T0(none, none)`, ` {} `: `T0(none, none)`, `{"y":null,"x":"2"}`: `T0(2, none)`, `{"yy":"3"}`: `T0(none, 3)`} {
		r, err := c.DecodeArgs(a, params, []byte(in))
		if err != nil || a.Format(r.Value()) != want {
			t.Errorf("%q: %s, %v; expected %s", in, a.Format(r.Value()), err, want)
		}
	}
	doc := s.Types[tDoc-1].Fields
	for in, want := range map[string][2]string{
		`{"z":1}`:                {"z", "unknown"},
		`{"i":2147483648}`:       {"i", "range"},
		`{"i":"1"}`:              {"i", "type"},
		`{"by":"@@"}`:            {"by", "encoding"},
		`{"p":{"x":null}}`:       {"p.x", "required"},
		`{"l":[1,"a"]}`:          {"l[1]", "type"},
		`[]`:                     {"", "type"},
		`{"op":{"x":"1","q":2}}`: {"op.q", "unknown"},
	} {
		_, err := c.DecodeArgs(a, doc, []byte(in))
		var e *Error
		if !errors.As(err, &e) || e.Path != want[0] || e.Rule != want[1] {
			t.Errorf("%s: %v; expected path %q and rule %q", in, err, want[0], want[1])
		}
	}
}

func TestDecodeIDText(t *testing.T) {
	c := New(allKinds())
	a := model.NewArena()
	for _, c2 := range []struct {
		t    model.TypeRef
		text string
		want string
	}{
		{tDoc, "0192b6c1-0000-0000-0000-0000000000ff", "T4.Id(0192b6c1-0000-0000-0000-0000000000ff)"},
		{tSeq, "42", "T5.Id(42)"},
		{tNamed, `A "1"`, `T6.Id("A \"1\"")`},
		{tComposite, `{"prefix":"X","n":2}`, `T7.Id(T3("X", 2))`},
	} {
		v, err := c.DecodeIDText(a, c2.t, c2.text)
		if err != nil || a.Format(v) != c2.want {
			t.Errorf("%s: %s, %v", c2.text, a.Format(v), err)
		}
	}
	if _, err := c.DecodeIDText(a, tSeq, "x"); err == nil {
		t.Error("invalid number: no error")
	}
}
