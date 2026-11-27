// Package project reads the dependencies of a ReasonR project: modules
// from reasonr.yaml, their versions and directories
// (RFC §23).
//
//	module: github.com/tycheg/reasonr-auth   # a module has its path
//	src: src
//	requires:
//	  github.com/tycheg/reasonr-auth: v0.1.0
//	replace:                                          # local development
//	  github.com/tycheg/reasonr-auth: ../reasonr-auth
//
// A module is a directory with reasonr.yaml (the module key) and sources
// src in project parts: the bounded contexts of its systems, apps,
// primitives. Module sources of a version live in the module cache,
// read-only, as in Go; reasonr.lock pins versions and checksums. replace
// substitutes a local directory: no cache and no sum.
package project

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileName is the project and module file.
const FileName = "reasonr.yaml"

// LockName holds the versions and checksums of the project's modules.
const LockName = "reasonr.lock"

// File holds the reasonr.yaml keys needed for dependencies. The reasonr
// command reads the others (platform).
type File struct {
	Module   string            // module path; "" for a project that is not a module
	Src      string            // source directory relative to the file; src by default
	Requires []Require         // by module path
	Replace  map[string]string // module path → local directory relative to the file
}

// Require is a dependency: a module path and a version.
type Require struct {
	Path    string
	Version string
}

// Parse parses the module, src, requires and replace keys; it skips other
// top-level keys and their nested lines.
func Parse(data []byte) (*File, error) {
	f := &File{Src: "src", Replace: map[string]string{}}
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
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch {
		case !nested:
			section = ""
			switch key {
			case "module":
				f.Module = value
			case "src":
				if value != "" {
					f.Src = filepath.FromSlash(value)
				}
			case "requires", "replace":
				section = key
				if value != "" {
					return nil, fmt.Errorf("line %d: %s is a block: module path: value", no, key)
				}
			}
		case !ok:
			if section != "" {
				return nil, fmt.Errorf("line %d: expected module path: value", no)
			}
		case section == "requires":
			if !strings.HasPrefix(value, "v") {
				return nil, fmt.Errorf("line %d: the version of module %s is a tag like v1.2.3", no, key)
			}
			f.Requires = append(f.Requires, Require{Path: key, Version: value})
		case section == "replace":
			f.Replace[key] = filepath.FromSlash(value)
		}
	}
	sort.Slice(f.Requires, func(i, j int) bool { return f.Requires[i].Path < f.Requires[j].Path })
	for i := 1; i < len(f.Requires); i++ {
		if f.Requires[i].Path == f.Requires[i-1].Path {
			return nil, fmt.Errorf("module %s is listed in requires twice", f.Requires[i].Path)
		}
	}
	for path := range f.Replace {
		if !f.requires(path) {
			return nil, fmt.Errorf("replace %s: the module is not in requires", path)
		}
	}
	return f, nil
}

func (f *File) requires(path string) bool {
	for _, r := range f.Requires {
		if r.Path == path {
			return true
		}
	}
	return false
}

// Read reads reasonr.yaml from directory dir.
func Read(dir string) (*File, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	f, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, FileName), err)
	}
	return f, nil
}

// ForSrc finds the project whose sources are directory src: reasonr.yaml
// in the directory above it (or in it). nil means a module without a project.
func ForSrc(src string) (dir string, f *File, err error) {
	abs, err := filepath.Abs(src)
	if err != nil {
		return "", nil, err
	}
	for _, d := range []string{filepath.Dir(abs), abs} {
		f, err := Read(d)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		if filepath.Join(d, f.Src) == abs {
			return d, f, nil
		}
	}
	return "", nil, nil
}

// Dep is a resolved dependency: a module and its source directory.
type Dep struct {
	Path    string
	Version string
	Dir     string // module directory (with reasonr.yaml)
	Src     string // module source directory
	// Replaced is the local directory from replace: no cache and no sum.
	Replaced bool
}

// Deps resolves the dependencies of the project in directory dir with file
// f: replace gives a local directory, otherwise the module cache
// (CacheDir). Module directories from REASONR_MODULES (path=directory
// separated by ";") take precedence: that is how reasonr passes them into
// the platform container. A module from the cache is checked against
// reasonr.lock.
func Deps(dir string, f *File) ([]Dep, error) {
	if f == nil || len(f.Requires) == 0 {
		return nil, nil
	}
	env := envModules()
	lock, err := ReadLock(dir)
	if err != nil {
		return nil, err
	}
	var out []Dep
	for _, r := range f.Requires {
		d := Dep{Path: r.Path, Version: r.Version}
		switch {
		case env[r.Path] != "":
			d.Dir = env[r.Path]
			d.Replaced = f.Replace[r.Path] != ""
		case f.Replace[r.Path] != "":
			d.Dir, d.Replaced = f.Replace[r.Path], true
			if !filepath.IsAbs(d.Dir) {
				d.Dir = filepath.Join(dir, d.Dir)
			}
		default:
			cache, err := CacheDir()
			if err != nil {
				return nil, err
			}
			d.Dir = filepath.Join(cache, CachePath(r.Path, r.Version))
			if _, err := os.Stat(d.Dir); err != nil {
				return nil, fmt.Errorf("module %s@%s is not in the module cache: run reasonr mod download", r.Path, r.Version)
			}
			sum, err := Hash(d.Dir)
			if err != nil {
				return nil, err
			}
			if want := lock[r.Path+" "+r.Version]; want == "" {
				return nil, fmt.Errorf("module %s@%s is not in %s: run reasonr mod download", r.Path, r.Version, LockName)
			} else if want != sum {
				return nil, fmt.Errorf("module %s@%s in the cache has changed: sum %s, in %s — %s", r.Path, r.Version, sum, LockName, want)
			}
		}
		mf, err := Read(d.Dir)
		if err != nil {
			return nil, fmt.Errorf("module %s: %w", r.Path, err)
		}
		if mf.Module != r.Path {
			return nil, fmt.Errorf("module %s: %s declares module %q", r.Path, filepath.Join(d.Dir, FileName), mf.Module)
		}
		if len(mf.Requires) > 0 {
			return nil, fmt.Errorf("module %s: module dependencies are not supported yet", r.Path)
		}
		d.Src = filepath.Join(d.Dir, mf.Src)
		out = append(out, d)
	}
	return out, nil
}

// EnvDeps are the dependencies from REASONR_MODULES without a project: this
// is how the engine gets them in the platform container when only the
// sources are mounted.
func EnvDeps() ([]Dep, error) {
	env := envModules()
	paths := make([]string, 0, len(env))
	for p := range env {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []Dep
	for _, p := range paths {
		mf, err := Read(env[p])
		if err != nil {
			return nil, fmt.Errorf("module %s: %w", p, err)
		}
		out = append(out, Dep{Path: p, Dir: env[p], Src: filepath.Join(env[p], mf.Src), Replaced: true})
	}
	return out, nil
}

// envModules reads REASONR_MODULES: path=directory separated by ";".
func envModules() map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(os.Getenv("REASONR_MODULES"), ";") {
		if p, d, ok := strings.Cut(kv, "="); ok && p != "" && d != "" {
			out[p] = d
		}
	}
	return out
}

// EnvModules is the REASONR_MODULES value for dependencies with directories dirs.
func EnvModules(deps []Dep, dirs []string) string {
	var parts []string
	for i, d := range deps {
		parts = append(parts, d.Path+"="+dirs[i])
	}
	return strings.Join(parts, ";")
}
