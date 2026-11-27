package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	modules "github.com/tycheg/reasonr-compiler/internal/project"
)

// TestProjectWorkflow: a model project the way its team works on it in its
// own repository — the taxi model requires the authorization module by
// version (a git tag, no replace), the platform engine is a command. Before
// reasonr mod download the project does not compile; then reasonr.lock
// pins the module, mod verify passes, check, fmt --check, the UI types for
// the primitive implementations, the API description and the feature flags
// work on the project with the module from the cache, and reasonr test
// starts the platform engine on the project sources.
func TestProjectWorkflow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh scripts")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	// The module the taxi example requires (reasonr mod download in
	// examples/taxi).
	taxi := filepath.Join("..", "..", "examples", "taxi")
	pf, err := modules.Read(taxi)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := modules.Deps(taxi, pf)
	if err != nil || len(deps) != 1 {
		t.Fatalf("the taxi modules: %v %v", deps, err)
	}
	auth := deps[0].Dir
	// The authorization module published as a git repository with a tag.
	repo := filepath.Join(tmp, "git", "github.com", "tycheg", "reasonr-auth")
	if err := os.CopyFS(repo, os.DirFS(auth)); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "auth")
	git("tag", "v0.1.0")
	t.Setenv("REASONR_GIT_BASE", "file://"+filepath.Join(tmp, "git")+"/")
	t.Setenv("REASONR_MODCACHE", filepath.Join(tmp, "cache"))
	t.Setenv("REASONR_MODULES", "")
	t.Cleanup(func() { // the cache is read-only: TempDir cannot remove it without permissions
		_ = filepath.Walk(filepath.Join(tmp, "cache"), func(p string, _ os.FileInfo, _ error) error { return os.Chmod(p, 0o755) })
	})

	// The project: the taxi sources, the module by version, the engine command.
	proj := filepath.Join(tmp, "taxi")
	if err := os.CopyFS(filepath.Join(proj, "src"), os.DirFS(filepath.Join("..", "..", "examples", "taxi", "src"))); err != nil {
		t.Fatal(err)
	}
	engine := script(t, tmp, "engine", `echo "$@"`)
	if err := os.WriteFile(filepath.Join(proj, projectFile), []byte("src: src\nplatform:\n  command: "+engine+
		"\nrequires:\n  github.com/tycheg/reasonr-auth: v0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	reasonr := func(args ...string) (int, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := run(args, &out, &errb)
		return code, out.String() + errb.String()
	}
	must := func(code int, out string) string {
		t.Helper()
		if code != 0 {
			t.Fatalf("code %d:\n%s", code, out)
		}
		return out
	}

	if code, out := reasonr("check"); code == 0 || !strings.Contains(out, "reasonr mod download") {
		t.Fatalf("check before download: code %d\n%s", code, out)
	}
	must(reasonr("mod", "download"))
	if lock, _ := os.ReadFile(filepath.Join(proj, "reasonr.lock")); !strings.Contains(string(lock), "github.com/tycheg/reasonr-auth v0.1.0 h1:") {
		t.Fatalf("reasonr.lock:\n%s", lock)
	}
	must(reasonr("mod", "verify"))
	must(reasonr("check"))
	must(reasonr("fmt", "--check", "src"))
	if out := must(reasonr("build", "--ui-types")); !strings.Contains(out, "export interface TagProps<E>") {
		t.Fatalf("--ui-types: no TagProps:\n%.400s", out)
	}
	if out := must(reasonr("api", "openapi")); !strings.Contains(out, `"openapi": "3.1`) {
		t.Fatalf("api openapi:\n%.400s", out)
	}
	if out := must(reasonr("feature", "list")); !strings.Contains(out, "Ordering.tips") {
		t.Fatalf("feature list:\n%s", out)
	}
	if out := must(reasonr("test", "--driver", "api")); out != "test --driver api src\n" {
		t.Fatalf("the engine command got %q", out)
	}
}
