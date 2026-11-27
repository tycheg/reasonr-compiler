package diag

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// Codes for render tests; they are not in the registry.
var (
	testError = &Spec{
		Code: "E0902", Severity: Error,
		Format: "personal data flows into an unprotected place",
	}
	testWarning = &Spec{
		Code: "W1002", Severity: Warning,
		Format: "unreachable code after %q",
	}
	noteText = "%q is personal (subject %s)"
	fixText  = "declare the parameter as %q"
)

func sample() (*source.FileSet, []*Diagnostic) {
	fset := source.NewFileSet()
	src := "actor Customer\n  command(self) Start(code string)\n    orderStarted(orderId: orderId, contact: phone)\n\treturn\n"
	f := fset.AddFile("taxi/customer.rr", []byte(src))
	at := func(sub string, n int) source.Range {
		i := strings.Index(src, sub)
		return source.Range{Start: f.Pos(i), End: f.Pos(i + n)}
	}
	var b Bag
	b.Add(testError, at("phone)", 5)).
		Note(at("phone)", 5), noteText, "phone", "Customer").
		Fix(Message{Format: fixText, Args: []any{"contact string? personal"}},
			Edit{Range: at("contact", 0), NewText: "x"})
	b.Add(testWarning, at("return", 6), "return").
		Hint("remove the line")
	return fset, b.List()
}

func TestWriteText(t *testing.T) {
	fset, list := sample()
	var buf bytes.Buffer
	if err := WriteText(&buf, fset, list); err != nil {
		t.Fatal(err)
	}
	golden.Check(t, filepath.Join("testdata", "render.txt"), buf.Bytes())
}

func TestWriteJSON(t *testing.T) {
	fset, list := sample()
	var buf bytes.Buffer
	if err := WriteJSON(&buf, fset, list); err != nil {
		t.Fatal(err)
	}
	golden.Check(t, filepath.Join("testdata", "render.json"), buf.Bytes())
}

func TestHasErrors(t *testing.T) {
	_, list := sample()
	if !HasErrors(list) {
		t.Error("HasErrors = false, want true")
	}
	if HasErrors(list[1:]) {
		t.Error("HasErrors(warnings) = true, want false")
	}
}

func TestMessageFormatsArgs(t *testing.T) {
	inner := Message{Format: "end of %s", Args: []any{"line"}}
	m := Message{Format: "found %s", Args: []any{inner}}
	if got := m.String(); got != "found end of line" {
		t.Errorf("String() = %q", got)
	}
}

func TestRegistry(t *testing.T) {
	for _, s := range All() {
		if s.Title == "" || s.Format == "" {
			t.Errorf("%s: no title or template", s.Code)
		}
	}
}
