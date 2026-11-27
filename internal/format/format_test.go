package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/parser"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

func format(t testing.TB, name string, src []byte) []byte {
	t.Helper()
	fset := source.NewFileSet()
	out, diags := Source(fset, name, src)
	if len(diags) > 0 {
		var b strings.Builder
		_ = diag.WriteText(&b, fset, diags)
		t.Fatalf("%s: syntax errors:\n%s", name, b.String())
	}
	return out
}

// TestGolden: testdata/x.rr formats into testdata/x.golden, and the golden
// file is already formatted.
func TestGolden(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "*.rr"))
	if len(files) == 0 {
		t.Fatal("no test files")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			src := readFile(t, path)
			out := format(t, path, src)
			goldenPath := strings.TrimSuffix(path, ".rr") + ".golden"
			golden.Check(t, goldenPath, out)
			checkProperties(t, path, src)
			if again := format(t, goldenPath, out); string(again) != string(out) {
				t.Errorf("the golden file changes when formatted again:\n%s", again)
			}
		})
	}
}

// TestExamples: the examples are already formatted.
func TestExamples(t *testing.T) {
	for _, path := range corpus(t, rfcblocks.Examples()) {
		src := readFile(t, path)
		if out := format(t, path, src); string(out) != string(src) {
			t.Errorf("%s is not formatted; reasonr fmt gives:\n%s", path, out)
		}
	}
}

// TestRFCBlocks: the RFC code blocks are already formatted.
func TestRFCBlocks(t *testing.T) {
	blocks, err := rfcblocks.Load(rfcblocks.RFC())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range blocks {
		if b.IsIllustrative() {
			continue
		}
		for _, v := range b.Variants() {
			fset := source.NewFileSet()
			if _, diags := parser.ParseFile(fset, "rfc.rr", []byte(v)); len(diags) > 0 {
				continue
			}
			if out := format(t, "rfc.rr", []byte(v)); string(out) != v {
				t.Errorf("%s: the block is not formatted; reasonr fmt gives:\n%s", b.Pos(), out)
			}
			break
		}
	}
}

// TestProperties checks the formatter properties on all error-free files
// of the parser tests and the examples.
func TestProperties(t *testing.T) {
	var files []string
	for _, dir := range []string{
		filepath.Join("..", "parser", "testdata"),
		rfcblocks.Examples(),
	} {
		files = append(files, corpus(t, dir)...)
	}
	for _, path := range files {
		if strings.Contains(filepath.ToSlash(path), "/errors/") {
			continue
		}
		checkProperties(t, path, readFile(t, path))
	}
}

// FuzzFormat: the formatter properties hold for any input without syntax
// errors.
func FuzzFormat(f *testing.F) {
	for _, dir := range []string{"testdata", filepath.Join("..", "parser", "testdata"), rfcblocks.Examples()} {
		for _, path := range corpus(f, dir) {
			f.Add(readFile(f, path))
		}
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		fset := source.NewFileSet()
		if _, diags := parser.ParseFile(fset, "fuzz.rr", src); len(diags) > 0 {
			return
		}
		checkProperties(t, "fuzz.rr", src)
	})
}

// checkProperties checks that formatting
//   - gives text without syntax errors;
//   - does not change the tree (ignoring positions) or the comments;
//   - is idempotent.
func checkProperties(t testing.TB, name string, src []byte) {
	t.Helper()
	fset := source.NewFileSet()
	before, diags := parser.ParseFile(fset, name, trimLines(src))
	if len(diags) > 0 {
		t.Fatalf("%s: the source does not parse", name)
	}
	out, diags := Source(fset, name, src)
	if len(diags) > 0 {
		var b strings.Builder
		_ = diag.WriteText(&b, fset, diags)
		t.Fatalf("%s: the formatted text does not parse:\n%s\n%s", name, b.String(), out)
	}
	after, _ := parser.ParseFile(fset, name, out)
	if a, b := ast.Dump(before), ast.Dump(after); a != b {
		t.Fatalf("%s: formatting changed the tree\nsource:\n%s\nresult:\n%s", name, src, out)
	}
	if a, b := commentTexts(before), commentTexts(after); a != b {
		t.Fatalf("%s: formatting changed the comments\nbefore:\n%s\nafter:\n%s", name, a, b)
	}
	if again, _ := Source(fset, name, out); string(again) != string(out) {
		t.Fatalf("%s: formatting is not idempotent\nfirst time:\n%s\nsecond time:\n%s", name, out, again)
	}
	for i, l := range strings.Split(string(out), "\n") {
		if l != strings.TrimRight(l, " \t") {
			t.Errorf("%s: trailing spaces in line %d of the result", name, i+1)
		}
	}
}

// trimLines removes trailing spaces: the formatter removes them too, and in
// the tree they show in the text of documentation comments.
func trimLines(src []byte) []byte {
	lines := strings.Split(string(src), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return []byte(strings.Join(lines, "\n"))
}

func commentTexts(f *ast.File) string {
	var b strings.Builder
	for _, c := range f.Comments {
		b.WriteString(strings.TrimRight(c.Text, " \t\r"))
		b.WriteByte('\n')
	}
	return b.String()
}

func readFile(t testing.TB, path string) []byte {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// corpus returns the *.rr files inside dir.
func corpus(t testing.TB, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".rr") {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
