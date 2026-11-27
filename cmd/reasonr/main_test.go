package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	reasonr "github.com/tycheg/reasonr-compiler"

	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
)

func TestCheckExamples(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"check", rfcblocks.Examples()}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code %d, output:\n%s%s", code, out.String(), errOut.String())
	}
}

func TestCheckErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.rr")
	if err := os.WriteFile(path, []byte("aggregate A\n  command b()\n    status == x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"check", path}, &out, &errOut); code != 1 {
		t.Fatalf("code %d, want 1; stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "E0011") {
		t.Errorf("no E0011 in the output:\n%s", out.String())
	}

	out.Reset()
	if code := run([]string{"check", "--format", "json", path}, &out, &errOut); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	var res struct {
		Version     int
		Diagnostics []struct {
			Code    string
			Message string
		}
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if res.Version != 1 || len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != "E0011" {
		t.Errorf("unexpected result: %+v", res)
	}
	if !strings.HasPrefix(res.Diagnostics[0].Message, "expression is not a statement") {
		t.Errorf("unexpected message: %q", res.Diagnostics[0].Message)
	}
}

func TestUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"frobnicate"}, &out, &errOut); code != 2 {
		t.Errorf("code %d, want 2", code)
	}
}

func TestFmt(t *testing.T) {
	dir := t.TempDir()
	messy := filepath.Join(dir, "messy.rr")
	clean := filepath.Join(dir, "clean.rr")
	bad := filepath.Join(dir, "bad.rr")
	write := func(path, src string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(messy, "role   admin\n")
	write(clean, "role admin\n")

	var out, errOut bytes.Buffer
	if code := run([]string{"fmt", messy}, &out, &errOut); code != 0 || out.String() != "role admin\n" {
		t.Errorf("fmt: code %d, output %q, stderr %q", code, out.String(), errOut.String())
	}

	out.Reset()
	if code := run([]string{"fmt", "--check", dir}, &out, &errOut); code != 1 || out.String() != messy+"\n" {
		t.Errorf("fmt --check: code %d, output %q", code, out.String())
	}

	out.Reset()
	if code := run([]string{"fmt", "-w", dir}, &out, &errOut); code != 0 || out.Len() != 0 {
		t.Errorf("fmt -w: code %d, output %q", code, out.String())
	}
	if src, _ := os.ReadFile(messy); string(src) != "role admin\n" {
		t.Errorf("fmt -w did not write the file: %q", src)
	}
	if code := run([]string{"fmt", "--check", dir}, &out, &errOut); code != 0 {
		t.Errorf("fmt --check after -w: code %d", code)
	}

	write(bad, "role\n")
	errOut.Reset()
	if code := run([]string{"fmt", "-w", bad}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "E0012") {
		t.Errorf("fmt with an error: code %d, stderr %q", code, errOut.String())
	}
	if src, _ := os.ReadFile(bad); string(src) != "role\n" {
		t.Errorf("a file with an error changed: %q", src)
	}
}

func TestFmtExamples(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"fmt", "--check", rfcblocks.Examples()}, &out, &errOut); code != 0 {
		t.Fatalf("code %d, unformatted files:\n%s%s", code, out.String(), errOut.String())
	}
}

func TestCheckExtensions(t *testing.T) {
	var out, errOut bytes.Buffer
	taxi := filepath.Join(rfcblocks.Examples(), "taxi")
	if code := run([]string{"check", "--extension", "acme", taxi}, &out, &errOut); code != 0 {
		t.Errorf("--extension acme: code %d\n%s%s", code, out.String(), errOut.String())
	}
	if code := run([]string{"check", "--extension", "nope", taxi}, &out, &errOut); code != 2 {
		t.Errorf("a missing extension: code %d, want 2", code)
	}

	errorsDir := filepath.Join("..", "..", "internal", "module", "testdata", "errors")
	out.Reset()
	if code := run([]string{"check", "--extension", "base", errorsDir}, &out, &errOut); code != 1 {
		t.Fatalf("--extension base: code %d, want 1", code)
	}
	if strings.Contains(out.String(), "E1003") {
		t.Errorf("--extension base: extensions were checked:\n%s", out.String())
	}
	out.Reset()
	if code := run([]string{"check", errorsDir}, &out, &errOut); code != 1 {
		t.Fatalf("all extensions: code %d, want 1", code)
	}
	for _, c := range []string{"E1003", "E1007", "E1009"} {
		if !strings.Contains(out.String(), c) {
			t.Errorf("all extensions: no %s in the output", c)
		}
	}

	if code := run([]string{"check", t.TempDir()}, &out, &errOut); code != 2 {
		t.Errorf("empty directory: code %d, want 2", code)
	}
}

func TestCheckSema(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.rr"), []byte("aggregate A\n  id uuid\n  x Strng\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"check", dir}, &out, &errOut); code != 1 || !strings.Contains(out.String(), "E0107") {
		t.Errorf("code %d, output:\n%s%s", code, out.String(), errOut.String())
	}
}

func TestBuild(t *testing.T) {
	var out, errOut bytes.Buffer
	taxi := filepath.Join(rfcblocks.Examples(), "taxi")
	if code := run([]string{"build", "--extension", "acme", taxi}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "IR sha256") {
		t.Fatalf("build: code %d\n%s%s", code, out.String(), errOut.String())
	}
	out.Reset()
	file := filepath.Join(t.TempDir(), "taxi.rrm")
	if code := run([]string{"build", "--dump-ir", "--test", "-o", file, taxi}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "func command Ordering.Order.place(") {
		t.Fatalf("build --dump-ir: code %d\n%s", code, errOut.String())
	}
	m, err := reasonr.Open(file)
	if err != nil {
		t.Fatalf("the written model file does not open: %v", err)
	}
	if !m.Test() || len(m.Tests()) == 0 {
		t.Fatalf("build --test -o: the model file has no tests")
	}
	// Several modules and a module with errors.
	if code := run([]string{"build", rfcblocks.Examples()}, &out, &errOut); code != 2 {
		t.Fatalf("several modules: code %d", code)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.rr"), []byte("aggregate A\n  id uuid\n  x Nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if code := run([]string{"build", dir}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "E0107") {
		t.Fatalf("a module with errors: code %d\n%s", code, errOut.String())
	}
}

func TestDumpBytecode(t *testing.T) {
	var out, errOut bytes.Buffer
	bank := filepath.Join(rfcblocks.Examples(), "bank")
	if code := run([]string{"build", "--dump-bytecode", bank}, &out, &errOut); code != 0 ||
		!strings.Contains(out.String(), "func command Accounts.Account.Transfer(") {
		t.Fatalf("build --dump-bytecode: code %d\n%s", code, errOut.String())
	}
}

func TestCheckAgainst(t *testing.T) {
	var out, errOut bytes.Buffer
	bank := filepath.Join(rfcblocks.Examples(), "bank")
	if code := run([]string{"build", "--dump-schema", bank}, &out, &errOut); code != 0 {
		t.Fatalf("build --dump-schema: code %d\n%s", code, errOut.String())
	}
	schema := filepath.Join(t.TempDir(), "bank.json")
	if err := os.WriteFile(schema, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"check", "--against", schema, bank}, &out, &errOut); code != 0 || out.Len() != 0 {
		t.Fatalf("the model is incompatible with itself: code %d\n%s", code, out.String())
	}
	// A new version: the argument type of event withdrawn changes.
	src, err := os.ReadFile(filepath.Join(bank, "contexts", "Accounts", "bank.rr"))
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(src), "event withdrawn(amount decimal", "event withdrawn(amount int", 1)
	changed = strings.Replace(changed, "balance = balance - amount", "balance = balance - decimal(amount)", 1)
	changed = strings.Replace(changed, "withdrawn(amount: sum,", "withdrawn(amount: int(sum),", 1)
	dir := filepath.Join(t.TempDir(), "bank")
	if err := os.MkdirAll(filepath.Join(dir, "contexts", "Accounts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "contexts", "Accounts", "bank.rr"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"check", "--against", schema, dir}, &out, &errOut); code != 1 || !strings.Contains(out.String(), "E1102") {
		t.Fatalf("code %d\n%s%s", code, out.String(), errOut.String())
	}
}

func TestTools(t *testing.T) {
	taxi := filepath.Join(rfcblocks.Examples(), "taxi")
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"explain", "Order.Cancel", taxi}, 0, "Order.Cancel(reason string)"},
		{[]string{"explain", "Order.Cancel", taxi}, 0, "Actions in order"},
		{[]string{"explain", "--extension", "acme", "Order.placed", taxi}, 0, "Order.on placed"},
		{[]string{"explain", "Order.nope", taxi}, 2, ""},
		{[]string{"explain"}, 2, ""},
		{[]string{"graph", taxi}, 0, "flowchart LR"},
		{[]string{"graph", "--format", "dot", "--entity", "Order", taxi}, 0, "digraph model"},
		{[]string{"graph", "--entity", "Nope", taxi}, 2, ""},
		{[]string{"graph", "--app", "Admin", taxi}, 0, "Ordering_AdminOrders -- open --> Ordering_AdminOrder"},
		{[]string{"graph", "--contexts", taxi}, 0, "Ordering -- Order.placed, Order.edited"},
		{[]string{"extract", "Dispatch", taxi}, 0, "Ordering ← Dispatch.Trip.driverAssigned"},
		{[]string{"extract", "Nope", taxi}, 2, ""},
		{[]string{"ui", "eject", "Admin", taxi}, 2, ""},
		{[]string{"ui", "eject", "Nope", taxi}, 2, ""},
		{[]string{"ui"}, 2, ""},
		{[]string{"build", "--ui-types", taxi}, 0, "export interface EntityPickerProps<R, K>"},
		{[]string{"build", "--dump-ui", taxi}, 0, `"name": "Admin.AdminOrder:Order.Edit"`},
		{[]string{"build", "--ui-types", "--platform", "mobile", taxi}, 2, ""},
		{[]string{"graph", "--app", "Nope", taxi}, 2, ""},
		{[]string{"explain", "Admin.AdminOrder", taxi}, 0, "target ← data Order.Details.orderId"},
		{[]string{"graph", "--format", "svg", taxi}, 2, ""},
		{[]string{"pii", taxi}, 0, "Subject Ordering.Customer"},
		{[]string{"pii", "--format", "json", taxi}, 0, `"type": "Ordering.Customer"`},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		code := run(c.args, &out, &errOut)
		if code != c.code || !strings.Contains(out.String(), c.want) {
			t.Errorf("%v: code %d\n%s%s", c.args, code, out.String(), errOut.String())
		}
	}
}

func TestDiagExplain(t *testing.T) {
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"diag", "explain", "E0504"}, 0, "Fix:"},
		{[]string{"diag", "explain", "e0504", "W1001"}, 0, "W1001 — warning"},
		{[]string{"diag", "explain", "E9999"}, 2, ""},
		{[]string{"diag", "explain"}, 2, ""},
		{[]string{"diag", "list"}, 0, "E0001"},
		{[]string{"diag"}, 2, ""},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		code := run(c.args, &out, &errOut)
		if code != c.code || !strings.Contains(out.String(), c.want) {
			t.Errorf("%v: code %d\n%s%s", c.args, code, out.String(), errOut.String())
		}
	}
}

// TestUIDumpOnly: --ui-types and --dump-ui print only their own output: it
// is written to a file (types for tsc, the UI program), and the IR hash
// line would spoil it.
func TestUIDumpOnly(t *testing.T) {
	taxi := filepath.Join(rfcblocks.Examples(), "taxi")
	for _, flag := range []string{"--ui-types", "--dump-ui"} {
		var out, errOut bytes.Buffer
		if code := run([]string{"build", flag, taxi}, &out, &errOut); code != 0 || strings.Contains(out.String(), "IR sha256") {
			t.Errorf("%s: code %d\n%s", flag, code, out.String()[max(0, out.Len()-200):])
		}
	}
}

// TestUIEjectParts: in a laid-out project eject spreads the layout over the
// parts: the app goes to apps/<App>/app.rr, screens to the type's bounded context.
func TestUIEjectParts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "taxi")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(rfcblocks.Examples(), "taxi"))); err != nil {
		t.Fatal(err)
	}
	app := "app(Driver) DriverApp \"Driver\" auto\n  language en\n  platforms mobile\n"
	if err := os.WriteFile(filepath.Join(dir, "apps", "DriverApp", "app.rr"), []byte(app), 0o644); err != nil {
		t.Fatal(err)
	}
	ui := filepath.Join(dir, "contexts", "Dispatch", "ui.rr")
	src, err := os.ReadFile(ui)
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(string(src), "screen Shift"), strings.Index(string(src), "screen FreeDrivers")
	if err := os.WriteFile(ui, append(src[:i:i], src[j:]...), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"ui", "eject", "DriverApp", dir}, &out, &errOut); code != 0 {
		t.Fatalf("code %d\n%s", code, errOut.String())
	}
	for _, want := range []string{"# apps/DriverApp/app.rr\n\napp(Driver) DriverApp", "# contexts/Dispatch/driverapp.rr\n\nscreen DriverAppDriver"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q:\n%s", want, out.String())
		}
	}
}
