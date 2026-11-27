package lexer

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
	"github.com/tycheg/reasonr-compiler/internal/wanttest"
)

func scanFile(t *testing.T, path string) (*source.FileSet, *source.File, []token.Token, []token.Comment, []*diag.Diagnostic) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fset := source.NewFileSet()
	f := fset.AddFile(filepath.ToSlash(path), src)
	toks, comments, diags := Scan(f)
	return fset, f, toks, comments, diags
}

// dump prints tokens one per line: position, kind, text.
func dump(fset *source.FileSet, toks []token.Token, comments []token.Comment, diags []*diag.Diagnostic) string {
	var b strings.Builder
	var tok func(prefix string, t token.Token)
	tok = func(prefix string, t token.Token) {
		p := fset.Position(t.Pos)
		fmt.Fprintf(&b, "%s%d:%d %s", prefix, p.Line, p.Column, t.Kind)
		switch {
		case t.Kind == token.NEWLINE || t.Kind == token.INDENT || t.Kind == token.DEDENT || t.Kind == token.EOF:
		case t.Kind.IsKeyword() || t.Kind.IsOperator():
		default:
			fmt.Fprintf(&b, " %s", strconv.Quote(t.Text))
		}
		b.WriteByte('\n')
		for _, part := range t.Parts {
			if !part.IsExpr {
				fmt.Fprintf(&b, "%s  text %s\n", prefix, strconv.Quote(part.Text))
				continue
			}
			fmt.Fprintf(&b, "%s  expr\n", prefix)
			for _, it := range part.Tokens {
				tok(prefix+"    ", it)
			}
		}
	}
	for _, t := range toks {
		tok("", t)
	}
	for _, c := range comments {
		p := fset.Position(c.Pos)
		kind := "COMMENT"
		if c.Doc {
			kind = "DOC"
		}
		fmt.Fprintf(&b, "%d:%d %s %s\n", p.Line, p.Column, kind, strconv.Quote(c.Text))
	}
	for _, d := range diags {
		fmt.Fprintf(&b, "%s %s %s\n", fset.Position(d.Range.Start), d.Code(), d.Message())
	}
	return b.String()
}

func TestGolden(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "*.rr"))
	if len(files) == 0 {
		t.Fatal("no test files")
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fset, _, toks, comments, diags := scanFile(t, path)
			if len(diags) > 0 {
				t.Errorf("unexpected errors:\n%s", dump(fset, nil, nil, diags))
			}
			golden.Check(t, strings.TrimSuffix(path, ".rr")+".tokens", []byte(dump(fset, toks, comments, nil)))
		})
	}
}

func TestErrors(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "errors", "*.rr"))
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fset, f, _, _, diags := scanFile(t, path)
			wanttest.Check(t, fset, f, diags)
		})
	}
}

// TestExamples checks that the RFC examples are tokenized without errors.
func TestExamples(t *testing.T) {
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
	for _, path := range files {
		fset, _, toks, _, diags := scanFile(t, path)
		if len(diags) > 0 {
			t.Errorf("%s:\n%s", path, dump(fset, nil, nil, diags))
		}
		checkInvariants(t, fset, toks, nil, diags)
	}
}

// checkInvariants checks properties of the token stream that must hold on
// any input:
//   - the stream ends with EOF, INDENT and DEDENT are balanced;
//   - tokens and comments are ordered and do not overlap;
//   - without errors, every non-space byte is covered by a token or a comment.
func checkInvariants(t *testing.T, fset *source.FileSet, toks []token.Token, comments []token.Comment, diags []*diag.Diagnostic) {
	t.Helper()
	if len(toks) == 0 || toks[len(toks)-1].Kind != token.EOF {
		t.Fatalf("the stream does not end with EOF")
	}
	depth := 0
	for _, tk := range toks {
		switch tk.Kind {
		case token.INDENT:
			depth++
		case token.DEDENT:
			depth--
		}
		if depth < 0 {
			t.Fatalf("%s: DEDENT without INDENT", fset.Position(tk.Pos))
		}
		if tk.End < tk.Pos {
			t.Fatalf("%s: token ends before it starts", fset.Position(tk.Pos))
		}
	}
	if depth != 0 {
		t.Fatalf("unbalanced INDENT/DEDENT: %d", depth)
	}
	for i := 1; i < len(toks); i++ {
		if toks[i].Pos < toks[i-1].End {
			t.Fatalf("%s: tokens overlap", fset.Position(toks[i].Pos))
		}
	}
	if len(diags) > 0 || comments == nil {
		return
	}
	f := fset.File(toks[0].Pos)
	covered := make([]bool, f.Size()+1)
	mark := func(p, e source.Pos) {
		for o := f.Offset(p); o < f.Offset(e); o++ {
			covered[o] = true
		}
	}
	for _, tk := range toks {
		mark(tk.Pos, tk.End)
	}
	for _, c := range comments {
		mark(c.Pos, c.End)
	}
	src := f.Source()
	for o, c := range src {
		if !covered[o] && !strings.ContainsRune(" \t\r\n", rune(c)) && !(o < 3 && strings.HasPrefix(string(src), "\uFEFF")) {
			t.Fatalf("%s: byte %q is not covered by tokens", fset.Position(f.Pos(o)), c)
		}
	}
}

func FuzzLexer(f *testing.F) {
	seeds, _ := filepath.Glob(filepath.Join("testdata", "*.rr"))
	errs, _ := filepath.Glob(filepath.Join("testdata", "errors", "*.rr"))
	for _, p := range append(seeds, errs...) {
		src, err := os.ReadFile(p)
		if err == nil {
			f.Add(src)
		}
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		fset := source.NewFileSet()
		file := fset.AddFile("fuzz.rr", src)
		toks, comments, diags := Scan(file)
		if comments == nil {
			comments = []token.Comment{}
		}
		checkInvariants(t, fset, toks, comments, diags)
	})
}
