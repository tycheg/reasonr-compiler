package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	modules "github.com/tycheg/reasonr-compiler/internal/project"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// projectFile is the configuration of a model project
// (RFC §33): the source directory and the
// engine the model runs on.
//
//	src: src                    # the model sources; src by default
//	platform:
//	  image: taxi-engine:0.1.0  # the platform image (docker): local or from a registry
//	  build: ../platform        # the platform sources: build the image if it is not in a registry
//	  # or command: reasonrd    # the engine command; reasonrd from PATH by default
//	requires:                   # modules (compiler/internal/project): path: version
//	  github.com/tycheg/reasonr-auth: v0.1.0
//	replace:                    # a local module directory
//	  github.com/tycheg/reasonr-auth: ../reasonr-auth
const projectFile = "reasonr.yaml"

// project is a model project from reasonr.yaml.
type project struct {
	Dir     string // the reasonr.yaml directory (absolute)
	Src     string // the source directory (absolute)
	Image   string // the platform image or ""
	Build   string // the platform source directory (absolute) or ""
	Command string // the engine command if there is no image
}

// findProject looks for reasonr.yaml in directory from and above; nil if there is none.
func findProject(from string) (*project, error) {
	dir, err := filepath.Abs(from)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(dir); err == nil && !st.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, projectFile))
		if err == nil {
			p, err := parseProject(data)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", filepath.Join(dir, projectFile), err)
			}
			p.Dir, p.Src = dir, filepath.Join(dir, p.Src)
			if p.Build != "" {
				p.Build = filepath.Join(dir, p.Build)
			}
			return p, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

// parseProject parses reasonr.yaml: the src and platform (image or command)
// keys, as a block or on one line { … }, # comments.
func parseProject(data []byte) (*project, error) {
	p := &project{Src: "src"}
	section := ""
	sc := bufio.NewScanner(bytes.NewReader(data))
	for no := 1; sc.Scan(); no++ {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		nested := line[0] == ' ' || line[0] == '\t'
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key: value", no)
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch {
		case !nested && key == "src" && value != "":
			p.Src, section = filepath.FromSlash(value), ""
		case !nested && key == "platform" && value == "":
			section = "platform"
		case !nested && key == "platform" && strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}"):
			// platform: { image: … } or { command: … } on one line.
			section = ""
			for _, kv := range strings.Split(strings.Trim(value, "{}"), ",") {
				k, v, ok := strings.Cut(kv, ":")
				k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
				switch {
				case ok && k == "image" && v != "":
					p.Image = v
				case ok && k == "command" && v != "":
					p.Command = v
				case ok && k == "build" && v != "":
					p.Build = filepath.FromSlash(v)
				default:
					return nil, fmt.Errorf("line %d: platform: { image: …, build: … } or { command: … }", no)
				}
			}
		case nested && section == "platform" && key == "image" && value != "":
			p.Image = value
		case nested && section == "platform" && key == "command" && value != "":
			p.Command = value
		case nested && section == "platform" && key == "build" && value != "":
			p.Build = filepath.FromSlash(value)
		case !nested && (key == "module" || key == "requires" || key == "replace"):
			section = "deps" // modules: compiler/project
		case nested && section == "deps":
		default:
			return nil, fmt.Errorf("line %d: unknown key %q (src, platform: image, build | command, module, requires, replace)", no, key)
		}
	}
	if p.Image != "" && p.Command != "" {
		return nil, errors.New("platform: set image or command, not both")
	}
	if p.Build != "" && p.Image == "" {
		return nil, errors.New("platform.build builds the platform.image image: set image")
	}
	if p.Image == "" && p.Command == "" {
		p.Command = "reasonrd"
	}
	return p, nil
}

// defaultPath is the path of a command without arguments: the project
// sources if the current directory is in a project with reasonr.yaml, "." otherwise.
func defaultPath() string {
	if p, err := findProject("."); err == nil && p != nil {
		return relPath(p.Src)
	}
	return "."
}

// relPath is the path relative to the current directory if that is shorter.
func relPath(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

// boolFlags are the reasonrd test and reasonrd dev flags without a value:
// other flags without "=" take the next argument.
var boolFlags = []string{"v", "check-determinism", "coverage", "headed", "install", "affected", "h", "help"}

// positional returns the indexes of paths among the engine arguments.
func positional(args []string) []int {
	var out []int
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			for j := i + 1; j < len(args); j++ {
				out = append(out, j)
			}
			return out
		case strings.HasPrefix(a, "-"):
			name := strings.TrimLeft(a, "-")
			if !strings.Contains(name, "=") && !slices.Contains(boolFlags, name) {
				i++ // the flag value
			}
		default:
			out = append(out, i)
		}
	}
	return out
}

// launchCmd is reasonr test|dev [engine flags] [path ...]: it starts the
// project's engine (reasonr.yaml) with command cmd. Flags are passed to the
// engine as is; without paths, the project sources. The compiler does not
// run the engine itself: it only starts it with a command or a platform image.
func launchCmd(cmd string, args []string, stdout, stderr io.Writer) int {
	pos := positional(args)
	from := "."
	if len(pos) > 0 {
		from = args[pos[0]]
		if i := strings.LastIndex(from, ":"); i > 0 && !exists(from) {
			from = from[:i] // file:line
		}
	}
	p, err := findProject(from)
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	if p == nil {
		return execEngine("reasonrd", append([]string{cmd}, args...), nil, stdout, stderr)
	}
	if len(pos) == 0 {
		args = append(slices.Clone(args), relPath(p.Src))
		pos = []int{len(args) - 1}
	}
	if p.Image == "" {
		command := strings.Fields(p.Command) // for example: go run ./cmd/taxi-engine
		return execEngine(command[0], append(append(command[1:], cmd), args...), nil, stdout, stderr)
	}
	if code := ensureImage(p, stderr); code != 0 {
		return code
	}
	switch {
	case cmd == "dev":
		return devImage(p, args, pos, stdout, stderr)
	case slices.Contains(drivers(args), "web"):
		return webImage(p, args, stdout, stderr)
	}
	return testImage(p, args, pos, stdout, stderr)
}

// ensureImage finds the platform image: local, from a registry or built
// from the platform sources (platform.build) if there are any. Otherwise
// it is an error saying where to get the image.
func ensureImage(p *project, stderr io.Writer) int {
	if execEngine("docker", []string{"image", "inspect", p.Image}, nil, io.Discard, io.Discard) == 0 {
		return 0
	}
	var out bytes.Buffer
	if code := execEngine("docker", []string{"pull", p.Image}, nil, &out, &out); code == 0 {
		return 0
	}
	if st, err := os.Stat(p.Build); p.Build != "" && err == nil && st.IsDir() {
		fmt.Fprintf(stderr, "reasonr: image %s is neither local nor in a registry, building it from the platform sources %s\n", p.Image, p.Build)
		if code := buildImage(p, stderr); code != 0 {
			fmt.Fprintf(stderr, "reasonr: image %s did not build (see the docker build output above)\n", p.Image)
			return code
		}
		return 0
	}
	build := "no platform sources (platform.build is not set)"
	if p.Build != "" {
		build = "no platform sources: " + p.Build
	}
	fmt.Fprintf(stderr, `reasonr: no platform image %s (%s: platform.image): it is not local, it does not pull from a registry, %s.
  The platform developers release the image: get access to their registry. Without an image, use platform: { command: reasonrd }
  in %s: the standard engine, everything in memory.
%s`, p.Image, filepath.Join(p.Dir, projectFile), build, projectFile, indent(out.String()))
	return 2
}

// buildImage builds the platform image with docker build: the Dockerfile is
// src/Dockerfile or the Dockerfile of the platform directory, the context
// is the directory itself. In the ReasonR repository the build contexts
// compiler, engine, auth, postgres and web replace the published modules
// and packages.
func buildImage(p *project, stderr io.Writer) int {
	dockerfile := filepath.Join(p.Build, "src", "Dockerfile")
	if !exists(dockerfile) {
		dockerfile = filepath.Join(p.Build, "Dockerfile")
	}
	args := []string{"build"}
	if repo := reasonrRepo(p.Build); repo != "" {
		for _, part := range []string{"compiler", "engine", "auth", "postgres", "web"} {
			args = append(args, "--build-context", part+"="+filepath.Join(repo, part))
		}
	}
	args = append(args, "-f", dockerfile, "-t", p.Image, p.Build)
	return execEngine("docker", args, nil, stderr, stderr)
}

// reasonrRepo is the ReasonR repository (compiler/go.mod and engine/go.mod)
// above directory dir, or "".
func reasonrRepo(dir string) string {
	for {
		if exists(filepath.Join(dir, "compiler", "go.mod")) && exists(filepath.Join(dir, "engine", "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func indent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return "  docker pull: " + strings.ReplaceAll(s, "\n", "\n  ") + "\n"
}

// drivers is the --driver list from the engine arguments.
func drivers(args []string) []string {
	for i, a := range args {
		name, value, ok := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if name != "driver" || !strings.HasPrefix(a, "-") {
			continue
		}
		if !ok && i+1 < len(args) {
			value = args[i+1]
		}
		return strings.Split(value, ",")
	}
	return nil
}

// webImage is reasonr test --driver …,web… with a platform image: the image
// has no browser, so the test-mode server with the platform UI build (dev
// --ui /ui) runs in a container, and reasonrd (the standard distribution,
// which links the web driver) runs the tests on the host against it (--url).
func webImage(p *project, args []string, stdout, stderr io.Writer) int {
	src, ok := containerPath(p, p.Src)
	if !ok {
		fmt.Fprintf(stderr, "reasonr: sources %s are outside the project\n", p.Src)
		return 2
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	var id bytes.Buffer
	mounts, env, err := moduleMounts(p)
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	run := append([]string{"run", "-d", "--rm", "-p", fmt.Sprintf("127.0.0.1:%d:8080", port), "-v", p.Dir + ":/model:ro"}, mounts...)
	if env != "" {
		run = append(run, "-e", "REASONR_MODULES="+env)
	}
	run = append(run, p.Image, "dev", "--ui", "/ui", "--watch", "0", src)
	if code := execEngine("docker", run, nil, &id, stderr); code != 0 {
		return code
	}
	cid := strings.TrimSpace(id.String())
	defer execEngine("docker", []string{"stop", cid}, nil, io.Discard, io.Discard)
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !ready(url) {
		fmt.Fprintf(stderr, "reasonr: the server in image %s did not start: docker logs %s\n", p.Image, cid)
		return 2
	}
	return execEngine("reasonrd", append([]string{"test", "--url", url}, args...), nil, stdout, stderr)
}

// ready waits until server url answers GET /openapi.json.
func ready(url string) bool {
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if resp, err := http.Get(url + "/openapi.json"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
	}
	return false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// containerPath is a host path in the project directory as a container path
// (the project is the /model volume); ok == false: the path is outside the project.
func containerPath(p *project, path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(p.Dir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(filepath.Join("/model", rel)), true
}

// testImage is reasonr test in the platform image: the project is the
// /model volume, the working directory is the current one if it is in the
// project. Relative paths stay as is: the report uses host paths; the rest
// are container paths.
func testImage(p *project, args []string, pos []int, stdout, stderr io.Writer) int {
	args = slices.Clone(args)
	wd, inside := containerPath(p, ".")
	if !inside {
		wd = "/model"
	}
	for _, i := range pos {
		path, line := args[i], ""
		if j := strings.LastIndex(path, ":"); j > 0 && !exists(path) {
			path, line = path[:j], path[j:]
		}
		cp, ok := containerPath(p, path)
		if !ok {
			fmt.Fprintf(stderr, "reasonr: %s is outside project %s\n", args[i], p.Dir)
			return 2
		}
		if !inside || filepath.IsAbs(path) {
			args[i] = cp + line
		}
	}
	mounts, env, err := moduleMounts(p)
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	// --affected: the image has no git, so test selection is computed here
	// and passed to the engine as a file (--impact).
	if rest, base, ok := affectedArgs(args); ok {
		file, err := impactFile(p.Src, base)
		if err != nil {
			fmt.Fprintf(stderr, "reasonr: --affected: %v\n", err)
			return 2
		}
		defer os.Remove(file)
		mounts = append(mounts, "-v", file+":/tmp/reasonr-impact.json:ro")
		args = append([]string{"--impact", "/tmp/reasonr-impact.json"}, rest...)
	}
	docker := append([]string{"run", "--rm", "-i", "-v", p.Dir + ":/model", "-w", wd}, mounts...)
	if env != "" {
		docker = append(docker, "-e", "REASONR_MODULES="+env)
	}
	docker = append(docker, p.Image, "test")
	return execEngine("docker", append(docker, args...), nil, stdout, stderr)
}

// devImage is reasonr dev in the platform image: docker compose brings up
// the platform's debug environment (/etc/reasonr/dev/compose.yaml in the
// image: databases and the engine). REASONR_IMAGE is the image,
// REASONR_MODEL the model sources, REASONR_ARGS the engine flags.
func devImage(p *project, args []string, pos []int, stdout, stderr io.Writer) int {
	model, err := filepath.Abs(args[pos[0]])
	if err != nil || len(pos) > 1 {
		fmt.Fprintln(stderr, "reasonr: dev works with one module")
		return 2
	}
	var flags []string
	for i, a := range args {
		if !slices.Contains(pos, i) {
			flags = append(flags, a)
		}
	}
	dir, err := os.MkdirTemp("", "reasonr-dev")
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	defer os.RemoveAll(dir)
	// The image may have no shell: the environment is copied out of the container.
	var id bytes.Buffer
	if code := execEngine("docker", []string{"create", p.Image}, nil, &id, stderr); code != 0 {
		return code
	}
	cid := strings.TrimSpace(id.String())
	code := execEngine("docker", []string{"cp", cid + ":/etc/reasonr/dev", dir}, nil, io.Discard, stderr)
	_ = execEngine("docker", []string{"rm", cid}, nil, io.Discard, io.Discard)
	if code != 0 {
		fmt.Fprintf(stderr, "reasonr: image %s has no debug environment /etc/reasonr/dev (compose.yaml)\n", p.Image)
		return code
	}
	// The project's modules as a copy in the environment directory: the /modules volume (compose.yaml).
	mods, menv, err := copyModules(p, filepath.Join(dir, "modules"))
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	env := []string{"REASONR_IMAGE=" + p.Image, "REASONR_MODEL=" + model, "REASONR_ARGS=" + strings.Join(flags, " "),
		"REASONR_MODULES_DIR=" + mods, "REASONR_MODULES=" + menv}
	return execEngine("docker", []string{"compose", "-f", filepath.Join(dir, "dev", "compose.yaml"), "up"}, env, stdout, stderr)
}

// execEngine runs the engine command and returns its exit code. SIGINT and
// SIGTERM are passed to it: reasonr waits until it stops (docker compose
// together with the containers).
func execEngine(name string, args, env []string, stdout, stderr io.Writer) int {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	err := cmd.Start()
	if err == nil {
		done := make(chan struct{})
		go func() {
			for {
				select {
				case s := <-sig:
					_ = cmd.Process.Signal(s)
				case <-done:
					return
				}
			}
		}()
		err = cmd.Wait()
		close(done)
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		fmt.Fprintf(stderr, "reasonr: %s: %v\n", name, err)
		return 2
	}
	return 0
}

// projectDeps are the project's modules (compiler/internal/project).
func projectDeps(p *project) ([]modules.Dep, error) {
	f, err := modules.Read(p.Dir)
	if err != nil {
		return nil, err
	}
	return modules.Deps(p.Dir, f)
}

// moduleMounts are the volumes and REASONR_MODULES for the project's modules
// in a container: module i is /modules/i, read-only.
func moduleMounts(p *project) (mounts []string, env string, err error) {
	deps, err := projectDeps(p)
	if err != nil {
		return nil, "", err
	}
	var dirs []string
	for i, d := range deps {
		dir := fmt.Sprintf("/modules/%d", i)
		mounts = append(mounts, "-v", d.Dir+":"+dir+":ro")
		dirs = append(dirs, dir)
	}
	return mounts, modules.EnvModules(deps, dirs), nil
}

// copyModules copies the project's modules into directory to (module i is
// to/i) for the /modules volume of the debug environment and returns to and
// REASONR_MODULES.
func copyModules(p *project, to string) (string, string, error) {
	deps, err := projectDeps(p)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return "", "", err
	}
	var dirs []string
	for i, d := range deps {
		if err := os.CopyFS(filepath.Join(to, fmt.Sprint(i)), os.DirFS(d.Dir)); err != nil {
			return "", "", err
		}
		dirs = append(dirs, fmt.Sprintf("/modules/%d", i))
	}
	return to, modules.EnvModules(deps, dirs), nil
}

// affectedArgs removes --affected and --base from the engine arguments; ok:
// --affected was there, base is the base version (origin/main by default).
func affectedArgs(args []string) (rest []string, base string, ok bool) {
	base = "origin/main"
	for i := 0; i < len(args); i++ {
		name, value, eq := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		switch {
		case !strings.HasPrefix(args[i], "-") || args[i] == "--":
			rest = append(rest, args[i])
		case name == "affected":
			ok = true
		case name == "base":
			if !eq && i+1 < len(args) {
				i++
				value = args[i]
			}
			base = value
		default:
			rest = append(rest, args[i])
		}
	}
	return rest, base, ok
}

// impactFile writes the reasonr impact report of sources src relative to
// base into a temporary file and returns its path.
func impactFile(src, base string) (string, error) {
	rep, err := pipeline.Impact(src, base, nil)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(rep)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "reasonr-impact-*.json")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return f.Name(), nil
}
