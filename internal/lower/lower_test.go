package lower

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// build builds the model of a module and lowers it to IR.
func build(t *testing.T, m *module.Module, ext string) *ir.Program {
	t.Helper()
	p, diags, err := m.Program(ext)
	if err != nil {
		t.Fatal(err)
	}
	info, d := sema.Check(p)
	diags = append(diags, d...)
	res := check.Analyze(p, info, check.All)
	diags = append(diags, res.Diagnostics...)
	if diag.HasErrors(diags) {
		t.Fatalf("the model has errors: %v", diags[0].Message())
	}
	out := Program(p, info, res)
	if err := ir.Verify(out); err != nil {
		t.Fatalf("the IR fails verification: %v", err)
	}
	return out
}

// TestLower: the IR of the examples and of module testdata/constructs (all
// language constructs) matches the golden files, is deterministic and
// survives serialization.
func TestLower(t *testing.T) {
	examples := rfcblocks.Examples()
	cases := []struct{ dir, ext, golden string }{
		{filepath.Join(examples, "taxi"), "", "taxi.ir"},
		{filepath.Join(examples, "taxi"), "acme", "taxi.acme.ir"},
		{filepath.Join(examples, "bank"), "", "bank.ir"},
		{filepath.Join("testdata", "constructs"), "", "constructs.ir"},
	}
	for _, c := range cases {
		t.Run(c.golden, func(t *testing.T) {
			load := func() *module.Module {
				m, diags, err := module.Load(source.NewFileSet(), c.dir)
				if err != nil || diag.HasErrors(diags) {
					t.Fatal(err, diags)
				}
				return m
			}
			p := build(t, load(), c.ext)
			golden.Check(t, filepath.Join("testdata", c.golden), []byte(p.String()))

			data := ir.Encode(p)
			again := ir.Encode(build(t, load(), c.ext))
			if !bytes.Equal(data, again) {
				t.Fatal("a repeated compilation gave a different IR")
			}
			decoded, err := ir.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(ir.Encode(decoded), data) {
				t.Fatal("encode(decode(x)) != x")
			}
			if decoded.String() != p.String() {
				t.Fatal("the dump differs after decode")
			}
		})
	}
}

// seeds are module sources as one file: the examples and testdata/constructs.
func seeds(t testing.TB) [][]byte {
	var out [][]byte
	for _, dir := range []string{
		filepath.Join(rfcblocks.Examples(), "taxi"),
		filepath.Join(rfcblocks.Examples(), "bank"),
		filepath.Join("testdata", "constructs"),
	} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.rr"))
		var src []byte
		for _, path := range files {
			// A seed is one file, and fakes live only in *.fake.rr: without
			// them the scenarios that set them up do not build.
			if strings.HasSuffix(path, ".fake.rr") || filepath.Base(path) == "ride.test.rr" || filepath.Base(path) == "antifraud.test.rr" {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			src = append(append(src, data...), '\n')
		}
		out = append(out, src)
	}
	return out
}

// FuzzLower: a model without errors lowers to IR that passes verification
// and serialization.
func FuzzLower(f *testing.F) {
	for _, src := range seeds(f) {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		fset := source.NewFileSet()
		m, diags, err := module.LoadFS(fset, fstest.MapFS{"m.rr": {Data: src}}, "fuzz")
		if err != nil || diag.HasErrors(diags) {
			return
		}
		p, d, err := m.Program("")
		if err != nil {
			t.Fatal(err)
		}
		info, d2 := sema.Check(p)
		res := check.Analyze(p, info, check.All)
		if diag.HasErrors(append(append(d, d2...), res.Diagnostics...)) {
			return
		}
		out := Program(p, info, res)
		if err := ir.Verify(out); err != nil {
			t.Fatalf("the IR fails verification: %v\n%s", err, out)
		}
		data := ir.Encode(out)
		back, err := ir.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(ir.Encode(back), data) {
			t.Fatal("encode(decode(x)) != x")
		}
	})
}

// TestSeeds: the sources for the fuzz test build without errors.
func TestSeeds(t *testing.T) {
	for i, src := range seeds(t) {
		fset := source.NewFileSet()
		m, diags, err := module.LoadFS(fset, fstest.MapFS{"m.rr": {Data: src}}, "seed")
		if err != nil {
			t.Fatal(err)
		}
		p, d, err := m.Program("")
		if err != nil {
			t.Fatal(err)
		}
		info, d2 := sema.Check(p)
		res := check.Analyze(p, info, check.All)
		if all := append(append(append(diags, d...), d2...), res.Diagnostics...); diag.HasErrors(all) {
			var b strings.Builder
			_ = diag.WriteText(&b, fset, all)
			t.Fatalf("seed %d:\n%s", i, b.String())
		}
	}
}
