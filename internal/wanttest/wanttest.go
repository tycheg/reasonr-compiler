// Package wanttest checks diagnostics against expectations written right in
// the source as comments:
//
//	status = cancelled  # want "E0304"
//	x = 1               # want "E0103" "E0201"
//
// A diagnostic must be on the same line as the comment.
package wanttest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

var (
	wantRe = regexp.MustCompile(`#\s*want((?:\s+"[^"]*")+)`)
	codeRe = regexp.MustCompile(`"([^"]*)"`)
)

// Parse returns the expectations: line number (from 1) → codes.
func Parse(src []byte) map[int][]diag.Code {
	out := map[int][]diag.Code{}
	for i, line := range strings.Split(string(src), "\n") {
		m := wantRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, c := range codeRe.FindAllStringSubmatch(m[1], -1) {
			out[i+1] = append(out[i+1], diag.Code(c[1]))
		}
	}
	return out
}

// Check checks the diagnostics of file f against the expectations from its
// source. Diagnostics of other files are ignored.
func Check(t testing.TB, fset *source.FileSet, f *source.File, list []*diag.Diagnostic) {
	t.Helper()
	want := Parse(f.Source())
	for _, d := range list {
		if fset.File(d.Range.Start) != f {
			continue
		}
		pos := fset.Position(d.Range.Start)
		codes := want[pos.Line]
		i := indexOf(codes, d.Code())
		if i < 0 {
			t.Errorf("%s: unexpected diagnostic %s: %s", pos, d.Code(), d.Message())
			continue
		}
		want[pos.Line] = append(codes[:i:i], codes[i+1:]...)
	}
	lines := make([]int, 0, len(want))
	for line := range want {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	for _, line := range lines {
		for _, c := range want[line] {
			t.Errorf("%s:%d: expected diagnostic %s", f.Name(), line, c)
		}
	}
}

func indexOf(codes []diag.Code, c diag.Code) int {
	for i, x := range codes {
		if x == c {
			return i
		}
	}
	return -1
}

// ScanCodes walks the testdata directories inside root and returns the
// codes from `# want` expectations with the files they occur in.
func ScanCodes(root string) (map[diag.Code][]string, error) {
	out := map[diag.Code][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".rr") {
			return nil
		}
		if !strings.Contains(filepath.ToSlash(path), "/testdata/") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, codes := range Parse(src) {
			for _, c := range codes {
				out[c] = append(out[c], path)
			}
		}
		return nil
	})
	return out, err
}
