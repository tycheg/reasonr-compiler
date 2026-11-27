package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestParseProject(t *testing.T) {
	for _, c := range []struct {
		text         string
		src, image   string
		command, err string
	}{
		{"", "src", "", "reasonrd", ""},
		{"# project\nsrc: model  # sources\nplatform:\n  image: taxi-engine:0.1.0\n", "model", "taxi-engine:0.1.0", "", ""},
		{"platform:\n  command: \"go run ./cmd/taxi-engine\"\n", "src", "", "go run ./cmd/taxi-engine", ""},
		{"platform: { command: reasonrd }\n", "src", "", "reasonrd", ""},
		{"platform: { image: \"t:1\" }\n", "src", "t:1", "", ""},
		{"platform: { name: x }\n", "", "", "", "platform: {"},
		{"platform:\n  build: ../platform\n", "", "", "", "set image"},
		{"platform:\n  image: a\n  command: b\n", "", "", "", "image or command"},
		{"name: x\n", "", "", "", "unknown key"},
		{"src\n", "", "", "", "key: value"},
	} {
		p, err := parseProject([]byte(c.text))
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%q: error %v, expected %q", c.text, err, c.err)
			}
			continue
		}
		if err != nil || p.Src != c.src || p.Image != c.image || p.Command != c.command {
			t.Errorf("%q: %+v, %v", c.text, p, err)
		}
	}
}

func TestPositional(t *testing.T) {
	args := []string{"--driver", "api,web", "-v", "--run=x", "src/order.test.rr:5", "--coverage", "src", "--", "-odd"}
	if got := positional(args); !slices.Equal(got, []int{4, 6, 8}) {
		t.Fatalf("%v", got)
	}
}

// project creates a project with reasonr.yaml text and sources src/m.rr;
// the current directory is its subdirectory src.
func testProject(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{projectFile: text, "src/m.rr": "aggregate A\n  id uuid\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(filepath.Join(dir, "src")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	return dir
}

// script writes an executable sh script with body body into directory bin.
func script(t *testing.T, bin, name, body string) string {
	t.Helper()
	path := filepath.Join(bin, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDrivers(t *testing.T) {
	for args, want := range map[string]string{"--driver api,web x": "api,web", "-driver=web": "web", "-v x": "", "--run driver": ""} {
		if got := strings.Join(drivers(strings.Fields(args)), ","); got != want {
			t.Errorf("%q: %q", args, got)
		}
	}
}

func TestLaunchCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh scripts")
	}
	bin := t.TempDir()
	engine := script(t, bin, "engine", `echo "$@"`)
	dir := testProject(t, "platform:\n  command: "+engine+"\n")
	var out, errb bytes.Buffer
	if code := run([]string{"test", "--driver", "api"}, &out, &errb); code != 0 || out.String() != "test --driver api .\n" {
		t.Fatalf("code %d: %q %s", code, out.String(), errb.String())
	}
	out.Reset()
	if code := run([]string{"dev", "--integrations", "A=http://x", filepath.Join(dir, "src")}, &out, &errb); code != 0 ||
		out.String() != "dev --integrations A=http://x "+filepath.Join(dir, "src")+"\n" {
		t.Fatalf("code %d: %q %s", code, out.String(), errb.String())
	}
	// The default path of compiler commands is the project sources.
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if got := defaultPath(); got != "src" {
		t.Errorf("defaultPath: %q", got)
	}
	script(t, bin, "fail", "exit 3\n")
	if err := os.WriteFile(projectFile, []byte("platform:\n  command: "+filepath.Join(bin, "fail")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"test"}, &out, &errb); code != 3 {
		t.Errorf("engine code: %d", code)
	}
}

func TestLaunchImage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh scripts")
	}
	bin := t.TempDir()
	// docker: create prints an id, cp creates the debug environment, the rest
	// prints the arguments and the environment variables of the run.
	script(t, bin, "docker", `case "$1" in
image|pull) [ "$2" != inspect ] || [ "$3" = taxi-engine:0.1.0 ] || exit 1; [ "$1" != pull ] || exit 1 ;;
create) echo c1 ;;
cp) mkdir -p "$3/dev" && touch "$3/dev/compose.yaml" ;;
rm) ;;
*) echo "$@"; echo "image=$REASONR_IMAGE model=$REASONR_MODEL args=$REASONR_ARGS" ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := testProject(t, "platform:\n  image: taxi-engine:0.1.0\n")
	var miss bytes.Buffer
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte("platform:\n  image: nope:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"dev"}, io.Discard, &miss); code != 2 || !strings.Contains(miss.String(), "no platform image nope:1") {
		t.Fatalf("no image: code %d %s", code, miss.String())
	}
	if err := os.WriteFile(filepath.Join(dir, projectFile), []byte("platform:\n  image: taxi-engine:0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"test", "--driver", "api", "m.rr:2"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	want := "run --rm -i -v " + dir + ":/model -w /model/src taxi-engine:0.1.0 test --driver api m.rr:2\n"
	if !strings.HasPrefix(out.String(), want) {
		t.Fatalf("%q\nexpected %q", out.String(), want)
	}
	out.Reset()
	if code := run([]string{"dev", "--integrations", "A=http://x"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if lines := strings.Split(out.String(), "\n"); !strings.HasPrefix(lines[0], "compose -f ") || !strings.HasSuffix(lines[0], "/dev/compose.yaml up") ||
		lines[1] != "image=taxi-engine:0.1.0 model="+filepath.Join(dir, "src")+" args=--integrations A=http://x" {
		t.Fatalf("%q", out.String())
	}
}

// TestLaunchBuild: the image is neither local nor in the registry, so
// reasonr builds it from the platform sources (platform.build); in the
// ReasonR repository with the compiler and engine contexts. Without the
// sources it is an error.
func TestLaunchBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh scripts")
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "docker.log")
	script(t, bin, "docker", `echo "$@" >> `+log+`
case "$1" in
image|pull) exit 1 ;;
build) touch `+filepath.Join(bin, "built")+` ;;
*) echo "$@" ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := testProject(t, "platform:\n  image: x-engine:1\n  build: ../platform\n")
	repo := filepath.Dir(dir)
	for _, f := range []string{"compiler/go.mod", "engine/go.mod", "platform/src/Dockerfile"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	if code := run([]string{"test"}, &out, &errb); code != 0 || !strings.Contains(errb.String(), "building it from the platform sources") {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	got, _ := os.ReadFile(log)
	want := "build"
	for _, part := range []string{"compiler", "engine", "auth", "postgres", "web"} {
		want += " --build-context " + part + "=" + filepath.Join(repo, part)
	}
	want += " -f " + filepath.Join(repo, "platform", "src", "Dockerfile") + " -t x-engine:1 " + filepath.Join(repo, "platform")
	if !strings.Contains(string(got), want) || !strings.HasPrefix(out.String(), "run --rm") {
		t.Fatalf("docker:\n%s\nexpected %q\noutput %q", got, want, out.String())
	}
	// No platform sources: an error, docker build does not run.
	if err := os.RemoveAll(filepath.Join(repo, "platform")); err != nil {
		t.Fatal(err)
	}
	errb.Reset()
	if code := run([]string{"dev"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "no platform sources") {
		t.Fatalf("without sources: code %d %s", code, errb.String())
	}
}
