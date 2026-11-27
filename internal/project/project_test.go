package project

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	f, err := Parse([]byte("src: src\nplatform:\n  image: x:1\nrequires:\n  example.test/a/b: v0.1.0\nreplace:\n  example.test/a/b: ../b\n"))
	if err != nil || len(f.Requires) != 1 || f.Requires[0].Version != "v0.1.0" || f.Replace["example.test/a/b"] != filepath.FromSlash("../b") {
		t.Fatalf("%+v %v", f, err)
	}
	for _, bad := range []string{
		"requires:\n  example.test/a/b: 0.1\n",
		"replace:\n  example.test/a/b: ../b\n",
		"requires:\n  example.test/a/b: v1\n  example.test/a/b: v2\n",
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("no error: %q", bad)
		}
	}
}

// TestDownload: a module in a repository subdirectory is downloaded by the
// tag "subdirectory/version" into a read-only cache, its sum goes to
// reasonr.lock; a modified cache fails the check.
func TestDownload(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "git", "example.test", "acme", "repo")
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, "auth", FileName), "module: example.test/acme/repo/auth\nsrc: src\n")
	write(filepath.Join(repo, "auth", "src", "contexts", "Auth", "user.rr"), "actor User\n  id uuid\n")
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
	git("tag", "auth/v0.1.0")
	t.Setenv("REASONR_GIT_BASE", "file://"+filepath.Join(tmp, "git")+"/")
	t.Setenv("REASONR_MODCACHE", filepath.Join(tmp, "cache"))
	t.Setenv("REASONR_MODULES", "")

	proj := filepath.Join(tmp, "proj")
	write(filepath.Join(proj, FileName), "src: src\nrequires:\n  example.test/acme/repo/auth: v0.1.0\n")
	f, err := Read(proj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Deps(proj, f); err == nil || !strings.Contains(err.Error(), "reasonr mod download") {
		t.Fatalf("without download: %v", err)
	}
	deps, err := Download(proj, f, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || filepath.Base(deps[0].Src) != "src" {
		t.Fatalf("%+v", deps)
	}
	lock, _ := os.ReadFile(filepath.Join(proj, LockName))
	if !strings.Contains(string(lock), "example.test/acme/repo/auth v0.1.0 h1:") {
		t.Errorf("reasonr.lock:\n%s", lock)
	}
	user := filepath.Join(deps[0].Src, "contexts", "Auth", "user.rr")
	if st, err := os.Stat(user); err != nil || st.Mode().Perm()&0o222 != 0 {
		t.Errorf("a module file in the cache is writable: %v %v", st.Mode(), err)
	}
	// A modified cache fails the check.
	_ = os.Chmod(filepath.Dir(user), 0o755)
	_ = os.Chmod(user, 0o644)
	write(user, "actor User\n  id string\n")
	if _, err := Deps(proj, f); err == nil || !strings.Contains(err.Error(), "has changed") {
		t.Errorf("modified module: %v", err)
	}
	t.Cleanup(func() { // the cache is read-only: TempDir cannot remove it without permissions
		_ = filepath.Walk(filepath.Join(tmp, "cache"), func(p string, _ os.FileInfo, _ error) error { return os.Chmod(p, 0o755) })
	})
}
