package uigen_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/uigen"
)

// TestTaxi: the taxi UI program matches the golden file.
func TestTaxi(t *testing.T) {
	for _, ext := range []string{"", "acme"} {
		m, err := pipeline.Load(source.NewFileSet(), filepath.Join(rfcblocks.Examples(), "taxi"), ext)
		if err != nil {
			t.Fatal(err)
		}
		if m.HasErrors() {
			t.Fatal(m.Diagnostics)
		}
		data, err := json.MarshalIndent(uigen.Generate(m.Info), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		name := "taxi.ui.json"
		if ext != "" {
			name = "taxi." + ext + ".ui.json"
		}
		golden.Check(t, filepath.Join("testdata", name), append(data, '\n'))
		if ext == "" {
			golden.Check(t, filepath.Join("testdata", "taxi.ui.ts"), []byte(uigen.TypeScript(uigen.Generate(m.Info))))
		}
	}
}
