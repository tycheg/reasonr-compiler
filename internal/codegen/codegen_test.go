package codegen

import (
	"bytes"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/lower"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// model builds the IR and schema of a model; ok == false means the model has errors.
func loadModel(m *module.Module, ext string) (*ir.Program, *model.Schema, bool, error) {
	p, diags, err := m.Program(ext)
	if err != nil {
		return nil, nil, false, err
	}
	info, d := sema.Check(p)
	diags = append(diags, d...)
	res := check.Analyze(p, info, check.All)
	diags = append(diags, res.Diagnostics...)
	if diag.HasErrors(diags) {
		return nil, nil, false, nil
	}
	return lower.Program(p, info, res), schemagen.Generate(p, info), true, nil
}

// generate builds and verifies bytecode; the encoding survives a round trip.
func generate(t *testing.T, p *ir.Program, s *model.Schema) *bytecode.Program {
	t.Helper()
	bc, err := Generate(p, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := bytecode.Verify(bc); err != nil {
		t.Fatalf("the bytecode fails verification: %v\n%s", err, bc)
	}
	data, err := bytecode.Encode(bc)
	if err != nil {
		t.Fatal(err)
	}
	back, err := bytecode.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	again, err := bytecode.Encode(back)
	if err != nil || !bytes.Equal(again, data) {
		t.Fatalf("Encode(Decode(x)) ≠ x: %v", err)
	}
	if back.String() != bc.String() {
		t.Fatal("the disassembly differs after an encoding round trip")
	}
	return bc
}

// TestExamples: the bytecode of the examples and testdata/constructs
// matches the golden files and is deterministic.
func TestExamples(t *testing.T) {
	examples := rfcblocks.Examples()
	cases := []struct{ dir, ext, golden string }{
		{filepath.Join(examples, "taxi"), "", "taxi.bc"},
		{filepath.Join(examples, "taxi"), "acme", "taxi.acme.bc"},
		{filepath.Join(examples, "bank"), "", "bank.bc"},
		{filepath.Join("..", "lower", "testdata", "constructs"), "", "constructs.bc"},
	}
	for _, c := range cases {
		t.Run(c.golden, func(t *testing.T) {
			var texts []string
			for range 2 {
				m, diags, err := module.Load(source.NewFileSet(), c.dir)
				if err != nil || diag.HasErrors(diags) {
					t.Fatal(err, diags)
				}
				p, s, ok, err := loadModel(m, c.ext)
				if err != nil || !ok {
					t.Fatalf("the model does not build: %v", err)
				}
				texts = append(texts, generate(t, p, s).String())
			}
			if texts[0] != texts[1] {
				t.Fatal("the bytecode is not deterministic")
			}
			golden.Check(t, filepath.Join("testdata", c.golden), []byte(texts[0]))
		})
	}
}

// FuzzCodegen: any model without errors whose IR lower builds gives bytecode
// that passes verification.
func FuzzCodegen(f *testing.F) {
	for _, dir := range []string{filepath.Join(rfcblocks.Examples(), "bank"), "../lower/testdata/constructs"} {
		m, _, err := module.Load(source.NewFileSet(), dir)
		if err != nil {
			f.Fatal(err)
		}
		for _, file := range m.Base {
			f.Add(file.Src.Source())
		}
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		m, diags, err := module.LoadFS(source.NewFileSet(), fstest.MapFS{"m.rr": {Data: src}}, "fuzz")
		if err != nil || diag.HasErrors(diags) {
			return
		}
		p, s, ok, err := loadModel(m, "")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			generate(t, p, s)
		}
	})
}
