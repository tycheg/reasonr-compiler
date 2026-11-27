package pipeline_test

import (
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tycheg/reasonr-compiler/internal/compat"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/format"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// TestExplanations: every code has an explanation with an example and a
// fix; the example reports exactly this code, the fix reports no errors
// (and, for warnings, not the warning itself).
func TestExplanations(t *testing.T) {
	for _, s := range diag.All() {
		code := s.Code
		t.Run(string(code), func(t *testing.T) {
			e := diag.Explain(code)
			switch {
			case e == nil:
				t.Fatal("no explanation in compiler/internal/diag/explain")
			case e.Text == "":
				t.Fatal("no text")
			case len(e.Example) == 0 || len(e.Fix) == 0:
				t.Fatal("no example or fix")
			}
			if e.Skip {
				return
			}
			got := run(t, e.Example)
			warning := s.Severity == diag.Warning
			errs := errorCodes(got)
			switch {
			case !warning && (len(errs) != 1 || errs[0] != code):
				t.Errorf("the example reports errors %v, expected only %s:\n%s", errs, code, render(got))
			case warning && (len(errs) > 0 || !has(got, code)):
				t.Errorf("the example must report %s without errors:\n%s", code, render(got))
			}
			got = run(t, e.Fix)
			if errs := errorCodes(got); len(errs) > 0 || has(got, code) {
				t.Errorf("the fix reports:\n%s", render(got))
			}
			for _, f := range e.Fix {
				out, diags := format.Source(source.NewFileSet(), f.Path, []byte(f.Src))
				if len(diags) == 0 && string(out) != f.Src {
					t.Errorf("fix %s is not formatted:\n%s", f.Path, out)
				}
			}
		})
	}
	for _, c := range diag.Explained() {
		if diag.Lookup(c) == nil {
			t.Errorf("%s: an explanation of a code that is not in the registry", c)
		}
	}
}

type result struct {
	fset  *source.FileSet
	diags []*diag.Diagnostic
}

// run builds the module of an example (the base model and all extensions)
// and returns its diagnostics. Files in old/ and new/ are two versions of a
// model: the new one is then checked for compatibility with the old one.
func run(t *testing.T, files []diag.ExampleFile) result {
	t.Helper()
	old, cur := fstest.MapFS{}, fstest.MapFS{}
	for _, f := range files {
		switch {
		case strings.HasPrefix(f.Path, "old/"):
			old[strings.TrimPrefix(f.Path, "old/")] = &fstest.MapFile{Data: []byte(f.Src)}
		case strings.HasPrefix(f.Path, "new/"):
			cur[strings.TrimPrefix(f.Path, "new/")] = &fstest.MapFile{Data: []byte(f.Src)}
		default:
			cur[f.Path] = &fstest.MapFile{Data: []byte(f.Src)}
		}
	}
	fset := source.NewFileSet()
	m, err := pipeline.OpenFS(fset, cur, "m")
	if err != nil {
		t.Fatal(err)
	}
	exts := []string{""}
	for _, tn := range m.Extensions {
		exts = append(exts, tn.Name)
	}
	var diags []*diag.Diagnostic
	seen := map[string]bool{}
	for _, tn := range exts {
		model, err := m.Model(tn)
		if err != nil {
			t.Fatal(err)
		}
		list := model.Diagnostics
		if len(old) > 0 && !model.HasErrors() {
			om, err := pipeline.OpenFS(source.NewFileSet(), old, "m")
			if err != nil {
				t.Fatal(err)
			}
			prev, err := om.Model("")
			if err != nil || prev.HasErrors() {
				t.Fatalf("the old version has errors: %v", err)
			}
			list = append(list, compat.Check(schemagen.Generate(prev.Program, prev.Info), model.Program, model.Info)...)
		}
		for _, d := range list {
			key := string(d.Code()) + d.Message()
			if !seen[key] {
				seen[key] = true
				diags = append(diags, d)
			}
		}
	}
	return result{fset: fset, diags: diags}
}

func errorCodes(r result) []diag.Code {
	set := map[diag.Code]bool{}
	for _, d := range r.diags {
		if d.Severity() == diag.Error {
			set[d.Code()] = true
		}
	}
	var out []diag.Code
	for c := range set {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func has(r result, c diag.Code) bool {
	for _, d := range r.diags {
		if d.Code() == c {
			return true
		}
	}
	return false
}

func render(r result) string {
	var b strings.Builder
	_ = diag.WriteText(&b, r.fset, r.diags)
	return b.String()
}
