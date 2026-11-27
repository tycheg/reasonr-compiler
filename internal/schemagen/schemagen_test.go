package schemagen

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

func schemaOf(t *testing.T, dir, ext string) *model.Schema {
	t.Helper()
	m, diags, err := module.Load(source.NewFileSet(), dir)
	if err != nil {
		t.Fatal(err)
	}
	p, d, err := m.Program(ext)
	if err != nil {
		t.Fatal(err)
	}
	diags = append(diags, d...)
	info, d := sema.Check(p)
	diags = append(diags, d...)
	diags = append(diags, check.Run(p, info, check.All)...)
	if diag.HasErrors(diags) {
		t.Fatalf("the model has errors: %s", diags[0].Message())
	}
	return Generate(p, info)
}

// TestExamples: the example schemas match the golden files, are
// deterministic and read back.
func TestExamples(t *testing.T) {
	examples := rfcblocks.Examples()
	cases := []struct{ dir, ext, golden string }{
		{filepath.Join(examples, "taxi"), "", "taxi.json"},
		{filepath.Join(examples, "taxi"), "acme", "taxi.acme.json"},
		{filepath.Join(examples, "bank"), "", "bank.json"},
		{filepath.Join("..", "lower", "testdata", "constructs"), "", "constructs.json"},
	}
	for _, c := range cases {
		t.Run(c.golden, func(t *testing.T) {
			s := schemaOf(t, c.dir, c.ext)
			data, err := s.MarshalIndent()
			if err != nil {
				t.Fatal(err)
			}
			golden.Check(t, filepath.Join("testdata", c.golden), data)
			again, _ := schemaOf(t, c.dir, c.ext).MarshalIndent()
			if !bytes.Equal(data, again) {
				t.Fatal("repeated generation gave a different schema")
			}
			back, err := model.ParseSchema(data)
			if err != nil {
				t.Fatal(err)
			}
			if out, _ := back.MarshalIndent(); !bytes.Equal(out, data) {
				t.Fatal("the schema changes after a read and a write")
			}
		})
	}
}
