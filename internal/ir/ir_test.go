package ir_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/lower"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// example lowers an example to IR.
func example(t testing.TB, name string) *ir.Program {
	m, _, err := module.Load(source.NewFileSet(), filepath.Join(rfcblocks.Examples(), name))
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := m.Program("")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := sema.Check(p)
	res := check.Analyze(p, info, check.All)
	return lower.Program(p, info, res)
}

func TestHashStable(t *testing.T) {
	first, second := ir.Hash(example(t, "taxi")), ir.Hash(example(t, "taxi"))
	if first != second {
		t.Fatal("the IR hash changes between compilations")
	}
	if ir.Hash(example(t, "taxi")) == ir.Hash(example(t, "bank")) {
		t.Fatal("different programs have the same hash")
	}
}

func TestDecodeErrors(t *testing.T) {
	data := ir.Encode(example(t, "bank"))
	for i := 0; i < len(data); i++ {
		// Any truncated input is an error, not a panic.
		if _, err := ir.Decode(data[:i]); err == nil {
			t.Fatalf("input truncated to %d bytes is accepted", i)
		}
	}
	if _, err := ir.Decode(append(data, 0)); err == nil {
		t.Fatal("an extra trailing byte is accepted")
	}
}

func TestOps(t *testing.T) {
	for _, name := range []string{"add.i64", "eq.enum", "lt.ts", "diff.ts", "neg.dur", "move", "find"} {
		op, ok := ir.Lookup(name)
		if !ok || op.String() != name {
			t.Errorf("operation %s: %v %v", name, op, ok)
		}
	}
	if _, ok := ir.Lookup("lt.enum"); ok {
		t.Error("enums are not ordered")
	}
}

// FuzzDecode: decoding arbitrary bytes does not crash, and an accepted
// program serializes back unchanged.
func FuzzDecode(f *testing.F) {
	f.Add(ir.Encode(example(f, "bank")))
	f.Add(ir.Encode(example(f, "taxi")))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := ir.Decode(data)
		if err != nil {
			return
		}
		enc := ir.Encode(p)
		q, err := ir.Decode(enc)
		if err != nil {
			t.Fatalf("repeated decode: %v", err)
		}
		if !bytes.Equal(ir.Encode(q), enc) {
			t.Fatal("encode(decode(x)) != x")
		}
		_ = p.String()
	})
}
