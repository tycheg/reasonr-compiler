package project

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// CacheDir is the module cache: REASONR_MODCACHE or <user cache>/reasonr/mod.
func CacheDir() (string, error) {
	if d := os.Getenv("REASONR_MODCACHE"); d != "" {
		return d, nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("module cache: %w (set REASONR_MODCACHE)", err)
	}
	return filepath.Join(d, "reasonr", "mod"), nil
}

// CachePath is the module directory in the cache: path@version.
func CachePath(modPath, version string) string {
	return filepath.FromSlash(modPath) + "@" + version
}

// Hash is the checksum of a module's sources: sha256 of the paths and
// contents of the files in path order, "h1:" and hex. Hidden files and
// directories are excluded.
func Hash(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && p != dir {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s %d\n", f, len(data))
		h.Write(data)
	}
	return "h1:" + hex.EncodeToString(h.Sum(nil)), nil
}

// ReadLock reads reasonr.lock in directory dir: "path version sum" per line;
// the key is "path version". No file means empty.
func ReadLock(dir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, LockName))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for no := 1; sc.Scan(); no++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			return nil, fmt.Errorf("%s:%d: expected path version sum", LockName, no)
		}
		out[f[0]+" "+f[1]] = f[2]
	}
	return out, nil
}

// WriteLock writes reasonr.lock in directory dir with the sums of modules deps from the cache.
func WriteLock(dir string, deps []Dep) error {
	var b strings.Builder
	b.WriteString("# Module versions and checksums (reasonr mod download). Do not edit by hand.\n")
	n := 0
	for _, d := range deps {
		if d.Replaced {
			continue
		}
		sum, err := Hash(d.Dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s %s %s\n", d.Path, d.Version, sum)
		n++
	}
	if n == 0 {
		err := os.Remove(filepath.Join(dir, LockName))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.WriteFile(filepath.Join(dir, LockName), []byte(b.String()), 0o644)
}

// Download downloads into the cache the project modules of directory dir
// that are missing there (except replace) and writes reasonr.lock. A module
// is downloaded with git: the repository is the first three parts of the
// path (github.com/owner/repository), the tag is "subdirectory/version" for
// a module in a repository subdirectory and the version otherwise.
// REASONR_GIT_BASE replaces "https://" in the repository address.
func Download(dir string, f *File, log io.Writer) ([]Dep, error) {
	cache, err := CacheDir()
	if err != nil {
		return nil, err
	}
	for _, r := range f.Requires {
		if f.Replace[r.Path] != "" {
			continue
		}
		target := filepath.Join(cache, CachePath(r.Path, r.Version))
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := fetch(r, target, log); err != nil {
			return nil, err
		}
	}
	lock, err := ReadLock(dir)
	if err != nil {
		return nil, err
	}
	// New modules go to the lock before Deps checks the sums.
	var fresh []Dep
	for _, r := range f.Requires {
		if f.Replace[r.Path] == "" && lock[r.Path+" "+r.Version] == "" {
			fresh = append(fresh, Dep{Path: r.Path, Version: r.Version, Dir: filepath.Join(cache, CachePath(r.Path, r.Version))})
		}
	}
	if len(fresh) > 0 {
		var all []Dep
		for _, r := range f.Requires {
			if f.Replace[r.Path] == "" {
				all = append(all, Dep{Path: r.Path, Version: r.Version, Dir: filepath.Join(cache, CachePath(r.Path, r.Version))})
			}
		}
		if err := WriteLock(dir, all); err != nil {
			return nil, err
		}
	}
	return Deps(dir, f)
}

// fetch clones the tag of module r and puts its directory into target, read-only.
func fetch(r Require, target string, log io.Writer) error {
	parts := strings.Split(r.Path, "/")
	if len(parts) < 3 {
		return fmt.Errorf("module %s: the path is host/owner/repository[/subdirectory]", r.Path)
	}
	repo, sub := strings.Join(parts[:3], "/"), strings.Join(parts[3:], "/")
	tag := r.Version
	if sub != "" {
		tag = sub + "/" + r.Version
	}
	base := os.Getenv("REASONR_GIT_BASE")
	if base == "" {
		base = "https://"
	}
	tmp, err := os.MkdirTemp("", "reasonr-mod")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	fmt.Fprintf(log, "reasonr: downloading %s@%s\n", r.Path, r.Version)
	cmd := exec.Command("git", "clone", "--quiet", "--depth", "1", "--branch", tag, base+repo, tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("module %s@%s: git clone %s%s (tag %s): %v\n%s", r.Path, r.Version, base, repo, tag, err, out)
	}
	src := filepath.Join(tmp, filepath.FromSlash(sub))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := copyTree(src, target); err != nil {
		return err
	}
	return readOnly(target)
}

// copyTree copies a directory without hidden files (.git).
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && p != src {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(to, data, 0o644)
	})
}

// readOnly makes a module directory in the cache read-only.
func readOnly(dir string) error {
	var dirs []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		return os.Chmod(p, 0o444)
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i], 0o555); err != nil {
			return err
		}
	}
	return nil
}
