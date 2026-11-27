package genmodel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// TestGenerate: the generated model passes all checks without diagnostics,
// warnings included, and is deterministic by seed.
func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	st, err := Generate(dir, Options{Lines: 30_000, Seed: 7, UIShare: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if st.Lines < 30_000 || st.Apps == 0 || st.Tests == 0 {
		t.Fatalf("size: %+v", st)
	}
	fset := source.NewFileSet()
	m, err := pipeline.Load(fset, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Diagnostics) > 0 {
		for _, d := range m.Diagnostics[:min(5, len(m.Diagnostics))] {
			p := fset.Position(d.Range.Start)
			t.Errorf("%s:%d: %s %s", p.Filename, p.Line, d.Code(), d.Message())
		}
		t.Fatalf("%d diagnostics", len(m.Diagnostics))
	}
	again := t.TempDir()
	if _, err := Generate(again, Options{Lines: 30_000, Seed: 7, UIShare: 0.5}); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "contexts", "C0003", "item2.rr"))
	b, _ := os.ReadFile(filepath.Join(again, "contexts", "C0003", "item2.rr"))
	if len(a) == 0 || string(a) != string(b) {
		t.Error("generation with one seed differs")
	}
}
