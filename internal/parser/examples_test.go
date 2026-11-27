package parser

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

func exampleFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(rfcblocks.Examples(), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".rr") {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no examples")
	}
	return files
}

// TestExamples: all RFC examples parse without errors.
func TestExamples(t *testing.T) {
	for _, path := range exampleFiles(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := source.NewFileSet()
		file, diags := ParseFile(fset, path, src)
		if len(diags) > 0 {
			var b strings.Builder
			_ = diag.WriteText(&b, fset, diags)
			t.Errorf("%s:\n%s", path, b.String())
		}
		checkPositions(t, fset, file)
	}
}

// TestRFCBlocks: every ```rr block of the RFC parses without errors — as a
// file, as entity members or as a command body. "..." lines in blocks are omitted.
func TestRFCBlocks(t *testing.T) {
	blocks, err := rfcblocks.Load(rfcblocks.RFC())
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) == 0 {
		t.Fatal("the RFC has no rr blocks")
	}
	for _, b := range blocks {
		if b.IsIllustrative() {
			continue
		}
		variants := b.Variants()
		var firstErr string
		ok := false
		for _, v := range variants {
			fset := source.NewFileSet()
			name := "rfc.rr" // a file block has its name: *.perf.rr parses differently
			if first, _, _ := strings.Cut(v, "\n"); strings.HasPrefix(first, "# ") && strings.HasSuffix(first, ".rr") {
				name = path.Base(strings.TrimPrefix(first, "# "))
			}
			file, diags := ParseFile(fset, name, []byte(v))
			if len(diags) == 0 {
				checkPositions(t, fset, file)
				ok = true
				break
			}
			if firstErr == "" {
				firstErr = render(fset, diags)
			}
		}
		if !ok {
			t.Errorf("%s: the block does not parse:\n%s", b.Pos(), firstErr)
		}
	}
}
