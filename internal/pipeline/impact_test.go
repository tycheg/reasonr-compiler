package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestImpact: changing one bank command selects only the tests that reach
// it; a change outside the sources means a full run; a flag together with a
// change of an old test is a violation (RFC §27.4).
func TestImpact(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../examples/bank")); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "base")
	src := filepath.Join(root, "src")
	model := filepath.Join(src, "contexts", "Accounts", "bank.rr")
	tests := filepath.Join(src, "contexts", "Accounts", "bank.test.rr")
	edit := func(path, old, new string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), old) {
			t.Fatalf("%s: no %q", path, old)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	impact := func(allowed map[string]bool) *ImpactReport {
		t.Helper()
		rep, err := Impact(src, "HEAD", allowed)
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}

	rep := impact(nil)
	if rep.Full || len(rep.Models) != 1 || len(rep.Models[0].Tests) != 0 {
		t.Fatalf("no changes: %+v %+v", rep, rep.Models)
	}

	// Comments and formatting affect no tests.
	edit(model, "  command(compliance) SetOverdraft", "  # limit\n  command(compliance) SetOverdraft")
	if rep := impact(nil); len(rep.Models[0].Tests) != 0 {
		t.Errorf("a comment affected tests: %v", rep.Models[0].Tests)
	}

	edit(model, "if limit < 0", "if limit < -1")
	rep = impact(nil)
	a := rep.Models[0]
	if rep.Full || len(a.Tests) == 0 || len(a.Tests) == a.Total {
		t.Fatalf("SetOverdraft change: run %d of %d", len(a.Tests), a.Total)
	}
	if _, ok := a.Tests["A transfer within the overdraft"]; !ok {
		t.Errorf("the overdraft test is not selected: %v", a.Tests)
	}
	if _, ok := a.Tests["Another client's account is not visible"]; ok {
		t.Errorf("a test that does not reach SetOverdraft is selected: %v", a.Tests["Another client's account is not visible"])
	}
	if sel := rep.Selected("", false); len(sel) != len(a.Tests) {
		t.Errorf("Selected: %v", sel)
	}

	// A project file outside the sources means a full run.
	edit(filepath.Join(root, "reasonr.yaml"), "src: src", "src: src\n# edit")
	if rep := impact(nil); !rep.Full || rep.Selected("", false) != nil {
		t.Errorf("reasonr.yaml: expected a full run: %+v", rep)
	}
	git("checkout", "-q", "--", "reasonr.yaml")
	if err := os.WriteFile(filepath.Join(root, "engine.yaml"), []byte("store: memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rep := impact(nil); !rep.Full {
		t.Errorf("new engine.yaml: expected a full run: %+v", rep)
	}
	_ = os.Remove(filepath.Join(root, "engine.yaml"))

	// A flag plus a change of an old test is a violation; an explicit exception lifts it.
	edit(model, "role compliance\n", "## New limits\nfeature limits \"2026-11-13\"\n\nrole compliance\n")
	edit(tests, `[a1].Transfer(target: a2, sum: -5, comment: "")`, `[a1].Transfer(target: a2, sum: -6, comment: "")`)
	edit(tests, "error invalidSum(sum: -5)", "error invalidSum(sum: -6)")
	const old = "All validation errors come at once"
	if rep := impact(nil); len(rep.Violations) != 1 || !strings.Contains(rep.Violations[0], old) {
		t.Errorf("flag and old test: %v", rep.Violations)
	}
	if rep := impact(map[string]bool{old: true}); len(rep.Violations) != 0 {
		t.Errorf("exception: %v", rep.Violations)
	}
}
