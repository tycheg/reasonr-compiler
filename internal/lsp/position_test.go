package lsp

import (
	"path/filepath"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/source"
)

func TestApplyChangeUTF16(t *testing.T) {
	// "😀" is two UTF-16 code units, a Greek letter is one.
	text := []byte("let x = \"😀έ\"\nδέλτα\n")
	got := applyChange(text, contentChange{
		Range: &Range{Start: Position{Line: 0, Character: 11}, End: Position{Line: 1, Character: 2}},
		Text:  "Ω",
	})
	if want := "let x = \"😀Ωλτα\n"; string(got) != want {
		t.Errorf("got %q, expected %q", got, want)
	}
	if got := applyChange(text, contentChange{Text: "όλα"}); string(got) != "όλα" {
		t.Errorf("whole replacement: %q", got)
	}
}

func TestPositions(t *testing.T) {
	src := []byte("a\n  😀b = 1\n")
	f := source.NewFileSet().AddFile("x.rr", src)
	b := f.Pos(len("a\n  😀"))
	p := lspPos(f, b)
	if p != (Position{Line: 1, Character: 4}) {
		t.Errorf("lspPos = %+v", p)
	}
	if got := filePos(f, p); got != b {
		t.Errorf("filePos = %d, expected %d", got, b)
	}
	// Past the end of a line or the file it is clamped.
	if got := filePos(f, Position{Line: 0, Character: 99}); got != f.Pos(1) {
		t.Errorf("past the end of a line: %d", got)
	}
	if got := filePos(f, Position{Line: 99}); got != f.Pos(len(src)) {
		t.Errorf("past the end of the file: %d", got)
	}
	if got := textPos(src, len(src)); got != (Position{Line: 2}) {
		t.Errorf("textPos of the end = %+v", got)
	}
}

func TestURI(t *testing.T) {
	path := filepath.FromSlash("/tmp/δέλτα module/order.rr")
	uri := uriFromPath(path)
	if uri != "file:///tmp/%CE%B4%CE%AD%CE%BB%CF%84%CE%B1%20module/order.rr" {
		t.Errorf("uri = %s", uri)
	}
	if got := pathFromURI(uri); got != path {
		t.Errorf("path = %s", got)
	}
	if got := pathFromURI("untitled:Untitled-1"); got != "" {
		t.Errorf("not a file: %q", got)
	}
}

func TestMinimalEdit(t *testing.T) {
	old := []byte("a\nb  c\nd\n")
	new := []byte("a\nb c\nd\n")
	e := minimalEdit(old, new)
	if e.Range != (Range{Start: Position{Line: 1}, End: Position{Line: 2}}) || e.NewText != "b c\n" {
		t.Errorf("edit %+v", e)
	}
	got := applyChange(old, contentChange{Range: &e.Range, Text: e.NewText})
	if string(got) != string(new) {
		t.Errorf("after the edit %q", got)
	}
	// A change in the last line without a line break.
	e = minimalEdit([]byte("a\nb"), []byte("a\nb\n"))
	if got := applyChange([]byte("a\nb"), contentChange{Range: &e.Range, Text: e.NewText}); string(got) != "a\nb\n" {
		t.Errorf("end of file: %q (%+v)", got, e)
	}
}

func TestModuleDir(t *testing.T) {
	dir := filepath.FromSlash("/m")
	tests := map[string]string{
		"/m/order.rr":                 "/m",
		"/m/extensions/acme/order.rr": "/m",
		"/m/contexts/Shop.Sales/a.rr": "/m",
		"/m/apps/Admin/app.rr":        "/m",
		"/m/primitives/p.rr":          "/m",
		"/m/sub/order.rr":             "/m/sub",
	}
	for p, want := range tests {
		if got := moduleDir(filepath.FromSlash(p)); got != filepath.FromSlash(want) {
			t.Errorf("moduleDir(%s) = %s", p, got)
		}
	}
	if got := extensionOf(dir, filepath.FromSlash("/m/extensions/acme/order.rr")); got != "acme" {
		t.Errorf("extensionOf = %q", got)
	}
	if got := extensionOf(dir, filepath.FromSlash("/m/order.rr")); got != "" {
		t.Errorf("extensionOf of a base file = %q", got)
	}
}
