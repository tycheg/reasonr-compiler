package model

import (
	"math"
	"testing"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

func TestScalars(t *testing.T) {
	if v := Int(-5); v.Kind != KindInt || v.Int() != -5 {
		t.Errorf("Int: %+v", v)
	}
	if v := Bigint(1 << 40); v.Kind != KindBigint || v.Int() != 1<<40 || NewArena().Format(v) != "1099511627776" {
		t.Errorf("Bigint: %+v", v)
	}
	if v := Float(-1.5); v.Float() != -1.5 {
		t.Errorf("Float: %+v", v)
	}
	d := dec.MustParse("-12.34")
	if v := Decimal(d); v.Decimal() != d {
		t.Errorf("Decimal: %+v", v)
	}
	if !Bool(true).Bool() || Bool(false).Bool() {
		t.Error("Bool")
	}
	u := [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32, 0x10}
	if UUID(u).UUID() != u || TimerID(u).UUID() != u {
		t.Error("UUID")
	}
	for _, tc := range []struct {
		y    int
		m    time.Month
		d    int
		days int64
	}{{1, 1, 1, 0}, {1970, 1, 1, epochDays}, {2026, 10, 3, 739891}, {9999, 12, 31, 3652058}} {
		v := Date(tc.y, tc.m, tc.d)
		if v.Days() != tc.days {
			t.Errorf("Date(%d-%d-%d).Days() = %d, want %d", tc.y, tc.m, tc.d, v.Days(), tc.days)
		}
		if got := v.Date(); got != time.Date(tc.y, tc.m, tc.d, 0, 0, 0, 0, time.UTC) {
			t.Errorf("Date(%d-%d-%d).Date() = %v", tc.y, tc.m, tc.d, got)
		}
	}
	for _, ts := range []time.Time{
		time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), // the default timestamp value
		time.Date(2026, 10, 3, 12, 30, 0, 123456789, time.UTC),
		time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC),
	} {
		if got := Timestamp(ts).Timestamp(); !got.Equal(ts) {
			t.Errorf("Timestamp(%v) = %v", ts, got)
		}
	}
	if Duration(-time.Minute).Duration() != -time.Minute || TimeOfDay(90*time.Minute).Duration() != 90*time.Minute {
		t.Error("Duration")
	}
	if v := Enum(3, 2); v.Type != 3 || v.Index() != 2 {
		t.Error("Enum")
	}
	if !None.IsNone() || Int(0).IsNone() {
		t.Error("None")
	}
}

func TestArena(t *testing.T) {
	a := NewArena()
	s := a.String("hello")
	b := a.Bytes([]byte{1, 2, 3})
	if a.Text(s) != "hello" || s.Kind != KindString || len(a.Data(b)) != 3 || b.Kind != KindBytes {
		t.Fatalf("strings: %q %v", a.Text(s), a.Data(b))
	}
	if a.Text(a.String("")) != "" {
		t.Error("empty string")
	}
	inner := a.Record(2, Int(1), s)
	l := a.List(inner.Value(), None)
	r := a.Record(1, l, Bool(true))
	if f := a.Fields(r); len(f) != 2 || !a.Equal(f[0], l) {
		t.Fatalf("Fields: %v", f)
	}
	if got := a.Format(r.Value()); got != `T1([T2(1, "hello"), none], true)` {
		t.Errorf("Format = %s", got)
	}

	// Building through the stack: a nested value is built inside the outer one.
	m := a.Mark()
	a.Push(a.String("k"))
	mi := a.Mark()
	a.Push(Int(1))
	a.Push(Int(2))
	a.Push(a.End(KindList, 0, mi))
	mp := a.End(KindMap, 0, m)
	if a.Len(mp) != 1 || a.Format(mp) != `["k": [1, 2]]` {
		t.Errorf("map: len %d, %s", a.Len(mp), a.Format(mp))
	}
	if a.Mark() != 0 {
		t.Errorf("the stack is not empty: %d", a.Mark())
	}

	id := a.ID(5, a.String("A-1"))
	if !id.Boxed() || a.Format(id) != `T5.Id("A-1")` || a.Unbox(id, IDGiven).Kind != KindString {
		t.Errorf("ID: %s", a.Format(id))
	}
	seq := ID(6, Int(42))
	if seq.Boxed() || a.Unbox(seq, IDSequence).Int() != 42 || a.Format(seq) != "T6.Id(42)" {
		t.Errorf("sequence ID: %s", a.Format(seq))
	}

	a.Reset()
	if n, v := a.Size(); n != 0 || v != 0 {
		t.Errorf("Reset: %d %d", n, v)
	}
}

func TestStatic(t *testing.T) {
	st := NewStaticArena()
	c := st.Record(1, st.String("const"), Int(7))
	if c.Ref.Off&staticBit == 0 {
		t.Fatal("a static arena reference is not marked")
	}
	if st.Text(st.Fields(c)[0]) != "const" {
		t.Error("a static arena reads itself")
	}
	a := NewArena()
	a.SetStatic(st)
	a.String("filler") // the offsets of a regular arena differ from the static one
	if a.Text(a.Fields(c)[0]) != "const" {
		t.Error("a regular arena does not read the static one")
	}
	// A copy into an arena with the same static arena does not copy constants.
	b := NewArena()
	b.SetStatic(st)
	if b.Copy(a, c.Value()) != c.Value() {
		t.Error("a constant was copied")
	}
	// Into an arena without a static one it is copied.
	other := NewArena()
	cp := other.Copy(a, c.Value())
	if cp.Ref.Off&staticBit != 0 || !Equal(other, cp, a, c.Value()) {
		t.Errorf("copy: %+v", cp)
	}
	defer func() {
		if recover() == nil {
			t.Error("no panic when reading a static reference without a static arena")
		}
	}()
	NewArena().Fields(c)
}

func TestCopy(t *testing.T) {
	src := NewArena()
	v := src.Record(1,
		src.String("text"),
		src.ID(2, src.Record(3, Int(1), src.String("x")).Value()),
		src.List(src.Bytes([]byte("b")), src.List()),
	).Value()
	dst := NewArena()
	dst.String("shift")
	cp := dst.Copy(src, v)
	if !Equal(dst, cp, src, v) || dst.Format(cp) != src.Format(v) {
		t.Errorf("copy %s ≠ %s", dst.Format(cp), src.Format(v))
	}
	src.Reset()
	src.String("garbage over the old data")
	if dst.Format(cp) != `T1("text", T2.Id(T3(1, "x")), [bytes"62", []])` {
		t.Errorf("the copy depends on the source: %s", dst.Format(cp))
	}
}

func TestEqualHashCompare(t *testing.T) {
	a := NewArena()
	set := func(xs ...Value) Value {
		m := a.Mark()
		for _, x := range xs {
			a.Push(x)
		}
		return a.End(KindSet, 0, m)
	}
	mp := func(xs ...Value) Value {
		m := a.Mark()
		for _, x := range xs {
			a.Push(x)
		}
		return a.End(KindMap, 0, m)
	}
	type pair struct {
		x, y  Value
		equal bool
	}
	nan := Float(math.NaN())
	pairs := []pair{
		{None, None, true},
		{None, Int(0), false},
		{Int(1), Int(1), true},
		{Int(1), Decimal(dec.FromInt(1)), false},
		{Float(0), Float(math.Copysign(0, -1)), true},
		{nan, nan, false},
		{a.String("a"), a.String("a"), true},
		{a.String("a"), a.Bytes([]byte("a")), false},
		{Enum(1, 0), Enum(2, 0), false},
		{ID(1, Int(5)), ID(1, Int(5)), true},
		{ID(1, Int(5)), ID(2, Int(5)), false},
		{a.ID(1, a.String("x")), a.ID(1, a.String("x")), true},
		{a.ID(1, a.String("x")), ID(1, Int(0)), false},
		{a.Record(1, Int(1)).Value(), a.Record(1, Int(1)).Value(), true},
		{a.Record(1, Int(1)).Value(), a.Record(2, Int(1)).Value(), false},
		{a.List(Int(1), Int(2)), a.List(Int(2), Int(1)), false},
		{set(Int(1), Int(2)), set(Int(2), Int(1)), true},
		{set(Int(1), Int(2)), set(Int(1), Int(3)), false},
		{set(Int(1)), set(Int(1), Int(2)), false},
		{mp(Int(1), a.String("a"), Int(2), a.String("b")), mp(Int(2), a.String("b"), Int(1), a.String("a")), true},
		{mp(Int(1), a.String("a")), mp(Int(1), a.String("b")), false},
		{Timestamp(time.Unix(1, 5)), Timestamp(time.Unix(1, 5)), true},
	}
	for _, p := range pairs {
		if got := a.Equal(p.x, p.y); got != p.equal {
			t.Errorf("Equal(%s, %s) = %v", a.Format(p.x), a.Format(p.y), got)
		}
		if p.equal && a.Hash(p.x) != a.Hash(p.y) {
			t.Errorf("Hash(%s) ≠ Hash(%s)", a.Format(p.x), a.Format(p.y))
		}
		if p.equal && p.x.Kind != KindSet && p.x.Kind != KindMap && a.Compare(p.x, p.y) != 0 {
			t.Errorf("Compare(%s, %s) ≠ 0", a.Format(p.x), a.Format(p.y))
		}
	}
	if a.Hash(Int(1)) == a.Hash(Int(2)) || a.Hash(a.String("ab")) == a.Hash(a.String("ba")) {
		t.Error("Hash does not tell values apart")
	}

	ordered := []Value{
		None,
		Int(-3), Int(0), Int(7),
		Decimal(dec.MustParse("-0.5")), Decimal(dec.MustParse("0.25")),
		a.String(""), a.String("a"), a.String("ab"), a.String("b"),
		UUID([16]byte{0x00, 0xff}), UUID([16]byte{0x01}),
		Date(2026, 1, 1), Date(2026, 1, 2),
		Timestamp(time.Unix(-10, 999)), Timestamp(time.Unix(-10, 1000)), Timestamp(time.Unix(0, 0)),
		Enum(1, 0), Enum(1, 1),
		ID(1, Int(1)), ID(1, Int(2)),
		a.Record(1, Int(1)).Value(), a.Record(1, Int(1), Int(0)).Value(), a.Record(1, Int(2)).Value(),
	}
	for i, x := range ordered {
		for j, y := range ordered {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := a.Compare(x, y); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", a.Format(x), a.Format(y), got, want)
			}
		}
	}
}

func TestPool(t *testing.T) {
	a := GetArena()
	a.String("x")
	o := &Outcome{Arena: a, Work: &UnitOfWork{}}
	o.Release()
	if o.Arena != nil || o.Work != nil {
		t.Error("Release did not reset the result")
	}
	if n, _ := a.Size(); n != 0 {
		t.Error("Release did not clear the arena")
	}
	PutArena(nil)
	PutArena(NewStaticArena()) // static arenas do not go into the pool
	if GetArena().flag != 0 {
		t.Error("a static arena came from the pool")
	}
}

func TestAllocs(t *testing.T) {
	a := NewArena()
	build := func() {
		a.Reset()
		s := a.String("text")
		m := a.Mark()
		a.Push(s)
		a.Push(Int(1))
		a.End(KindRecord, 1, m)
		_ = a.Hash(s)
	}
	build()
	if n := testing.AllocsPerRun(100, build); n != 0 {
		t.Errorf("allocations per loop: %v", n)
	}
}

func TestFormat(t *testing.T) {
	a := NewArena()
	u := [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 1, 2, 3, 4, 5, 6, 7, 8}
	m := a.Mark()
	a.Push(a.List(Int(1)))
	a.Push(a.String("c"))
	page := a.End(KindPage, 0, m)
	for _, tc := range []struct {
		v    Value
		want string
	}{
		{Float(2.5), "2.5"},
		{Decimal(dec.MustParse("-1.25")), "-1.25"},
		{Value{Kind: KindDecimal, Hi: math.MaxInt64}, "decimal?"},
		{a.Bytes([]byte{0xab}), `bytes"ab"`},
		{Bool(false), "false"},
		{UUID(u), "12345678-9abc-def0-0102-030405060708"},
		{Date(2026, 10, 3), "2026-10-03"},
		{TimeOfDay(13*time.Hour + 5*time.Minute), "13:05:00"},
		{Timestamp(time.Date(2026, 10, 3, 12, 0, 0, 5, time.UTC)), "2026-10-03T12:00:00.000000005Z"},
		{Duration(90 * time.Second), "1m30s"},
		{ID(2, UUID(u)), "T2.Id(12345678-9abc-def0-0102-030405060708)"},
		{TimerID(u), "Timer.Id(12345678-9abc-def0-0102-030405060708)"},
		{a.End(KindMap, 0, a.Mark()), "[:]"},
		{a.End(KindSet, 0, a.Mark()), "set[]"},
		{page, `page([1], "c")`},
		{Value{Kind: Kind(200)}, "?"},
	} {
		if got := a.Format(tc.v); got != tc.want {
			t.Errorf("Format = %s, want %s", got, tc.want)
		}
	}
	if a.Compare(Float(1), Float(2)) != -1 || a.Compare(TimeOfDay(2), TimeOfDay(1)) != 1 ||
		a.Compare(a.ID(1, a.String("a")), ID(1, Int(5))) != 1 || a.Compare(ID(1, Int(5)), a.ID(1, a.String("a"))) != -1 ||
		a.Compare(a.ID(1, a.String("a")), a.ID(1, a.String("b"))) != -1 || a.Compare(Enum(1, 5), Enum(2, 0)) != -1 ||
		a.Compare(TimerID(u), TimerID([16]byte{})) != 1 || a.Compare(ID(1, Int(5)), ID(2, Int(1))) != -1 {
		t.Error("Compare")
	}
	if a.Hash(Enum(1, 0)) == a.Hash(Enum(2, 0)) || a.Hash(page) == a.Hash(a.List()) || a.Hash(None) == a.Hash(Int(0)) {
		t.Error("Hash")
	}
}
