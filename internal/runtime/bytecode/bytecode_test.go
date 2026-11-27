package bytecode

import (
	"bytes"
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

func TestInstrEncoding(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for op := Op(1); int(op) < NumOps(); op++ {
		info := op.Info()
		for range 50 {
			var a, b, c uint32
			switch info.Format {
			case FormatABC:
				a, b, c = r.Uint32()&0xffff, r.Uint32()&0xffff, r.Uint32()&0xffff
				if r.Intn(2) == 0 {
					a, b, c = a&0xff, b&0xff, c&0xff
				}
			case FormatABx:
				a, b = r.Uint32()&0xffff, r.Uint32()
				if r.Intn(2) == 0 {
					a, b = a&0xff, b&0xffff
				}
			}
			code, ok := AppendInstr([]uint32{42}, op, a, b, c)
			if !ok {
				t.Fatalf("%s %d %d %d: not encoded", op, a, b, c)
			}
			in, ok := DecodeInstr(code, 1)
			if !ok || in.Op != op || in.A != a || in.B != b || in.C != c || in.Size != len(code)-1 {
				t.Fatalf("%s %d %d %d: decoded %+v (%v)", op, a, b, c, in, ok)
			}
			if in.Wide != (in.Size == 2) {
				t.Fatalf("%s: WIDE flag", op)
			}
		}
	}
	for _, tc := range []struct {
		op      Op
		a, b, c uint32
	}{
		{OpMove, 0x10000, 0, 0},
		{OpJmp, 0x10000, 0, 0},
		{OpJmp, 0, 0, 1},
		{OpRet, 1, 0, 0},
	} {
		if _, ok := AppendInstr(nil, tc.op, tc.a, tc.b, tc.c); ok {
			t.Errorf("%s %d %d %d encoded", tc.op, tc.a, tc.b, tc.c)
		}
	}
	for _, code := range [][]uint32{
		{uint32(OpWide)},                       // WIDE at the end
		{uint32(OpWide), uint32(OpWide)},       // WIDE after WIDE
		{0xff},                                 // unknown operation
		{uint32(OpRet) | 1<<8},                 // an operand of an operation without operands
		{uint32(OpWide) | 1<<8, uint32(OpRet)}, // WIDE before an operation without operands
	} {
		if in, ok := DecodeInstr(code, 0); ok {
			t.Errorf("%#x decoded: %+v", code, in)
		}
	}
	if _, ok := DecodeInstr(nil, 0); ok {
		t.Error("empty code decoded")
	}
}

func TestOps(t *testing.T) {
	if NumOps() > 256 || NumFixedOps() >= NumOps() {
		t.Fatalf("%d operations, %d untyped", NumOps(), NumFixedOps())
	}
	for _, name := range []string{"add.i32", "neg.dec", "eq.timer", "ge.dur", "diff.ts", "move", "expect.matches"} {
		op, ok := Lookup(name)
		if !ok || op.String() != name {
			t.Errorf("Lookup(%s) = %v, %v", name, op, ok)
		}
	}
	if Op(255).Valid() || Op(255).String() != "?" {
		t.Error("Op(255)")
	}
	seen := map[string]bool{}
	for i, name := range Intrinsics {
		if seen[name] || IntrinsicIndex(name) != int32(i) {
			t.Errorf("built-in function %s", name)
		}
		seen[name] = true
	}
	if IntrinsicIndex("nope") != NoFunc {
		t.Error("IntrinsicIndex(nope)")
	}
	if FuncKind(99).String() != "?" || FuncTest.String() != "test" {
		t.Error("FuncKind.String")
	}
}

// sample is a small verified program: Thing.Do(x int) adds x to the
// counter with event done.
func sample() *Program {
	intT := model.Type{Kind: model.KindInt}
	s := &model.Schema{Types: []model.TypeInfo{{
		Ref: 1, Name: "Thing", Kind: model.TypeEntity, ID: model.IDUUID,
		Fields: []model.FieldInfo{{Ref: 0, Name: "count", Type: intT}},
		Methods: []model.MethodInfo{
			{Ref: 0, Name: "Do", Kind: model.MethodCommand, Public: true, Params: []model.FieldInfo{{Name: "x", Type: intT}}},
			{Ref: 1, Name: "done", Kind: model.MethodEvent, Params: []model.FieldInfo{{Name: "x", Type: intT}}},
		},
	}}}
	p := &Program{Schema: s, Consts: []Const{{Type: intT, Lit: model.Literal{Kind: model.KindInt, Text: "0"}}}}
	do := Func{Name: "Thing.Do", Kind: FuncCommand, Owner: 1, Method: 0, Params: 1,
		Regs:     []model.Type{intT, {Kind: model.KindBool}},
		RegNames: []string{"x", ""}, Consts: []uint32{0},
		Sites: []Site{{Type: 1, Method: 1, Func: 1, Args: []uint32{0}}},
	}
	asm := func(f *Func, ins ...[4]uint32) {
		for _, in := range ins {
			var ok bool
			f.Code, ok = AppendInstr(f.Code, Op(in[0]), in[1], in[2], in[3])
			if !ok {
				panic("asm")
			}
		}
	}
	gt, _ := Lookup("gt.i32")
	asm(&do,
		[4]uint32{uint32(gt), 1, 0, 2},        // r1 = x > 0
		[4]uint32{uint32(OpBrFalse), 1, 3, 0}, // if not, to ret
		[4]uint32{uint32(OpEvent), 0, 0, 0},   // done(x: r0)
		[4]uint32{uint32(OpRet), 0, 0, 0},
	)
	apply := Func{Name: "Thing.done", Kind: FuncApply, Owner: 1, Method: 1, Params: 1,
		Regs: []model.Type{intT, intT}, RegNames: []string{"x", ""}}
	add, _ := Lookup("add.i32")
	asm(&apply,
		[4]uint32{uint32(OpField), 1, 0, 0},
		[4]uint32{uint32(add), 1, 1, 0},
		[4]uint32{uint32(OpSet), 0, 1, 0},
		[4]uint32{uint32(OpRet), 0, 0, 0},
	)
	p.Funcs = []Func{do, apply}
	return p
}

func TestVerify(t *testing.T) {
	if err := Verify(sample()); err != nil {
		t.Fatalf("Verify(sample): %v\n%s", err, sample())
	}
	want := "func command Thing.Do(x int)\n  regs: r0 x int, r1 bool\n  consts: r2 = k0\n" +
		"     0  gt.i32         r1, r0, r2\n     1  br.false       r1, 3\n" +
		"     2  event          Thing.done(x: r0) → Thing.done\n     3  ret\n"
	if got := sample().String(); !strings.Contains(got, want) {
		t.Errorf("disassembly:\n%s\nexpected:\n%s", got, want)
	}
	add, _ := Lookup("add.i32")
	for _, tc := range []struct {
		name   string
		mutate func(p *Program)
		want   string
	}{
		{"no schema", func(p *Program) { p.Schema = nil }, "no schema"},
		{"constant", func(p *Program) { p.Consts[0].Lit.Text = "x" }, "constant 0"},
		{"jump into the middle", func(p *Program) {
			p.Funcs[0].Code[1] = uint32(OpBrFalse) | 1<<8 | 7<<16
		}, "not the start of an instruction"},
		{"end without a return", func(p *Program) { p.Funcs[1].Code = p.Funcs[1].Code[:3] }, "ends"},
		{"register outside the frame", func(p *Program) { p.Funcs[0].Sites[0].Args[0] = 9 }, "no register r9"},
		{"result into a constant", func(p *Program) { p.Funcs[0].Code[0] = p.Funcs[0].Code[0]&^0xff00 | 2<<8 }, "result"},
		{"operand kind", func(p *Program) { p.Funcs[1].Regs[1] = model.Type{Kind: model.KindString} }, "expected int"},
		{"condition not bool", func(p *Program) { p.Funcs[0].Regs[1] = model.Type{Kind: model.KindInt} }, "expected bool"},
		{"method kind", func(p *Program) { p.Funcs[0].Sites[0].Method = 0 }, "expected event"},
		{"apply function", func(p *Program) { p.Funcs[0].Sites[0].Func = 0 }, "not apply"},
		{"argument count", func(p *Program) { p.Funcs[0].Sites[0].Args = nil }, "0 arguments, 1 parameters"},
		{"field", func(p *Program) { p.Funcs[1].Code[0] = uint32(OpField) | 1<<8 | 5<<16 }, "no field 5"},
		{"unknown operation", func(p *Program) { p.Funcs[1].Code[1] = 0xfe }, "invalid instruction"},
		{"no code", func(p *Program) { p.Funcs[1].Code = nil }, "no code"},
		{"loop.guard at the end", func(p *Program) { p.Funcs[1].Code[3] = uint32(OpLoopGuard) }, "ends"},
		{"add type", func(p *Program) {
			p.Funcs[1].Code[1] = uint32(add) | 1<<8 | 1<<16 | 0<<24
			p.Funcs[1].Regs[0] = model.Type{Kind: model.KindDecimal}
		}, "expected int"},
	} {
		p := sample()
		tc.mutate(p)
		err := Verify(p)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Verify = %v, expected %q", tc.name, err, tc.want)
		}
	}
}

func TestEncode(t *testing.T) {
	p := sample()
	data, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Encode(back)
	if !bytes.Equal(again, data) || back.String() != p.String() {
		t.Fatal("Encode(Decode(x)) ≠ x")
	}
	h1, _ := Hash(p)
	p.Funcs[0].Name = "Thing.Other"
	h2, _ := Hash(p)
	if h1 == h2 {
		t.Error("the hash does not depend on the program")
	}
	for i := range data {
		if _, err := Decode(data[:i]); err == nil {
			t.Fatalf("Decode(data[:%d]) without an error", i)
		} else if !errors.Is(err, ErrFormat) {
			t.Fatalf("Decode(data[:%d]): %v is not ErrFormat", i, err)
		}
	}
	bad := append([]byte(nil), data...)
	bad[4] = 99 // the format version
	if _, err := Decode(bad); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("foreign version: %v", err)
	}
}

func FuzzDecode(f *testing.F) {
	data, err := Encode(sample())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Decode(data)
		if err != nil {
			return
		}
		_ = p.String() // the disassembler does not panic on an unverified program either
		if Verify(p) != nil {
			return
		}
		again, err := Encode(p)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Decode(again)
		if err != nil || back.String() != p.String() {
			t.Fatalf("encoding round trip: %v", err)
		}
	})
}
