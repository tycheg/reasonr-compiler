package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

func model(t *testing.T, name, ext string) *pipeline.Model {
	t.Helper()
	m, err := pipeline.Load(source.NewFileSet(), filepath.Join(rfcblocks.Examples(), name), ext)
	if err != nil {
		t.Fatal(err)
	}
	if m.HasErrors() {
		t.Fatalf("the model has errors: %s", m.Diagnostics[0].Message())
	}
	return m
}

// TestExplain: the method descriptions of the examples match the golden file.
func TestExplain(t *testing.T) {
	cases := []struct{ example, ext, name string }{
		{"taxi", "", "Order"},
		{"taxi", "", "Order.Cancel"},
		{"taxi", "", "Trip.DriverFound"},
		{"taxi", "", "Trip.findDriver"},
		{"taxi", "", "Order.placed"},
		{"taxi", "", "Order.Card"},
		{"taxi", "", "Order.perKmRate"},
		{"taxi", "", "Order.cannotCancel"},
		{"taxi", "acme", "Order.placed"},
		{"taxi", "", "Admin"},
		{"taxi", "", "Admin.AdminOrder"},
		{"taxi", "", "AdminOrders"},
		{"taxi", "", "Taxi.MyOrders"},
		{"taxi", "acme", "MyOrder"},
		{"taxi", "", "EntityPicker"},
		{"bank", "", "Account.Transfer"},
		{"bank", "", "Client.Join"},
	}
	var b strings.Builder
	for _, c := range cases {
		m := model(t, c.example, c.ext)
		out, err := Explain(m, c.name)
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("=== " + c.example)
		if c.ext != "" {
			b.WriteString(" (" + c.ext + ")")
		}
		b.WriteString(" " + c.name + "\n" + out + "\n")
	}
	golden.Check(t, filepath.Join("testdata", "explain.golden"), []byte(b.String()))
	m := model(t, "taxi", "")
	for _, bad := range []string{"Nope", "Order.nope"} {
		if _, err := Explain(m, bad); err == nil {
			t.Errorf("Explain(%q): no error", bad)
		}
	}
}

// TestGraph: the example diagrams match the golden files.
func TestGraph(t *testing.T) {
	m := model(t, "taxi", "acme")
	var b strings.Builder
	if err := BuildGraph(m.Info, "").WriteMermaid(&b); err != nil {
		t.Fatal(err)
	}
	golden.Check(t, filepath.Join("testdata", "taxi.acme.mmd"), []byte(b.String()))
	b.Reset()
	if err := BuildGraph(m.Info, "Driver").WriteDot(&b); err != nil {
		t.Fatal(err)
	}
	golden.Check(t, filepath.Join("testdata", "taxi.driver.dot"), []byte(b.String()))
	b.Reset()
	if err := BuildScreenGraph(m.Info, "Admin").WriteMermaid(&b); err != nil {
		t.Fatal(err)
	}
	golden.Check(t, filepath.Join("testdata", "taxi.admin.mmd"), []byte(b.String()))
}

// TestPII: the personal data maps of the examples match the golden files.
func TestPII(t *testing.T) {
	var b strings.Builder
	for _, name := range []string{"taxi", "bank"} {
		r := PII(model(t, name, ""))
		if err := r.WriteText(&b); err != nil {
			t.Fatal(err)
		}
		if err := r.WriteJSON(&b); err != nil {
			t.Fatal(err)
		}
	}
	golden.Check(t, filepath.Join("testdata", "pii.golden"), []byte(b.String()))
}
