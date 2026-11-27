package lsp

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// document is a document open in the editor: its text overrides the file on disk.
type document struct {
	path    string
	version int
	text    []byte
}

// moduleState is a workspace module and the result of its last analysis.
type moduleState struct {
	dir     string
	fset    *source.FileSet
	cache   *module.Cache
	dirty   bool      // the text changed after the last analysis
	snap    *snapshot // nil: the module has not been analyzed yet
	publish map[string]bool
}

// snapshot is a checked module: the base model and the models of all extensions.
type snapshot struct {
	mod        *pipeline.Module
	base       *pipeline.Model
	extensions map[string]*pipeline.Model
	files      map[string]*module.File // by file path
	took       time.Duration
}

// model returns the model a file is checked in: the base one or one with an extension.
func (s *snapshot) model(path string) *pipeline.Model {
	if t := s.extensionOf(path); t != "" {
		if m := s.extensions[t]; m != nil {
			return m
		}
	}
	return s.base
}

// extensionOf returns the extension of a file: extensions/<name>/file.rr; "" for a base file.
func (s *snapshot) extensionOf(path string) string {
	return extensionOf(s.mod.Dir, path)
}

func extensionOf(dir, path string) string {
	if d, ext := module.FileModule(path); d == dir {
		return ext
	}
	return ""
}

// moduleDir returns the module directory a *.rr file belongs to
// (module.FileModule).
func moduleDir(path string) string {
	dir, _ := module.FileModule(path)
	return dir
}

// workspace holds the open documents and the modules they belong to.
type workspace struct {
	docs    map[string]*document // by path
	modules map[string]*moduleState
}

func newWorkspace() *workspace {
	return &workspace{docs: map[string]*document{}, modules: map[string]*moduleState{}}
}

// module returns the state of a file's module, creating it on first access.
func (w *workspace) module(path string) *moduleState {
	dir := moduleDir(path)
	m := w.modules[dir]
	if m == nil {
		m = &moduleState{dir: dir, dirty: true, publish: map[string]bool{}}
		w.modules[dir] = m
	}
	return m
}

// overlay returns the module file system in which open documents override
// the files on disk.
func (w *workspace) overlay(dir string, extra map[string][]byte) fs.FS {
	docs := map[string][]byte{}
	for p, d := range w.docs {
		if strings.HasPrefix(p, dir+string(filepath.Separator)) {
			docs[p] = d.text
		}
	}
	for p, text := range extra {
		docs[p] = text
	}
	return &overlayFS{root: dir, docs: docs}
}

// garbageLimit is how many times the file set may exceed the module's live
// text before it is recreated: every edit adds a new version of a file to
// the set.
const garbageLimit = 4

// analyze loads and checks a module. extra holds temporary file texts (for
// completion) that do not go into the module snapshot; only the model with
// extension ext is built with them.
func (w *workspace) analyze(m *moduleState, extra map[string][]byte, only string) (*snapshot, error) {
	start := time.Now()
	if m.fset == nil || total(m.fset) > garbageLimit*m.cache.Size()+1<<20 {
		m.fset = source.NewFileSet()
		m.cache = module.NewCache()
	}
	mod, err := pipeline.OpenCached(m.fset, w.overlay(m.dir, extra), m.dir, m.cache)
	if err != nil {
		return nil, err
	}
	s := &snapshot{mod: mod, extensions: map[string]*pipeline.Model{}, files: map[string]*module.File{}}
	for _, f := range mod.Base {
		s.files[filepath.FromSlash(f.Name)] = f
	}
	if extra == nil || only == "" {
		if s.base, err = mod.Model(""); err != nil {
			return nil, err
		}
	}
	for _, t := range mod.Extensions {
		for _, f := range t.Files {
			s.files[filepath.FromSlash(f.Name)] = f
		}
		if extra != nil && t.Name != only {
			continue
		}
		if s.extensions[t.Name], err = mod.Model(t.Name); err != nil {
			return nil, err
		}
	}
	if s.base == nil {
		s.base = s.extensions[only]
	}
	s.took = time.Since(start)
	return s, nil
}

func total(fset *source.FileSet) int {
	n := 0
	for _, f := range fset.Files() {
		n += f.Size()
	}
	return n
}

// diagnostics returns the snapshot's diagnostics by file: for base files
// from the base model, for extension files from their model. The keys are
// all module files, those without diagnostics included.
func (s *snapshot) diagnostics() map[string][]*diag.Diagnostic {
	out := map[string][]*diag.Diagnostic{}
	for p := range s.files {
		out[p] = nil
	}
	add := func(m *pipeline.Model, ext string) {
		for _, d := range m.Diagnostics {
			f := m.Module.FileSet.File(d.Range.Start)
			if f == nil {
				continue
			}
			p := filepath.FromSlash(f.Name())
			if _, ok := out[p]; ok && s.extensionOf(p) == ext {
				out[p] = append(out[p], d)
			}
		}
	}
	add(s.base, "")
	for name, m := range s.extensions {
		add(m, name)
	}
	return out
}

// overlayFS is a file system of directory root in which the texts of docs
// (by absolute path) override the files on disk and add to directories.
type overlayFS struct {
	root string
	docs map[string][]byte
}

func (o *overlayFS) path(name string) (string, error) {
	if !fs.ValidPath(name) {
		return "", &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	return filepath.Join(o.root, filepath.FromSlash(name)), nil
}

func (o *overlayFS) Open(name string) (fs.File, error) {
	return os.DirFS(o.root).Open(name)
}

func (o *overlayFS) ReadFile(name string) ([]byte, error) {
	p, err := o.path(name)
	if err != nil {
		return nil, err
	}
	if b, ok := o.docs[p]; ok {
		return b, nil
	}
	return os.ReadFile(p)
}

func (o *overlayFS) ReadDir(name string) ([]fs.DirEntry, error) {
	p, err := o.path(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	have := map[string]bool{}
	for _, e := range entries {
		have[e.Name()] = true
	}
	for doc := range o.docs {
		rel, err := filepath.Rel(p, doc)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		first, rest, _ := strings.Cut(filepath.ToSlash(rel), "/")
		if have[first] {
			continue
		}
		have[first] = true
		entries = append(entries, docEntry{name: first, dir: rest != ""})
	}
	if len(entries) == 0 && err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// docEntry is a directory entry for an open document absent from disk.
type docEntry struct {
	name string
	dir  bool
}

func (e docEntry) Name() string { return e.name }
func (e docEntry) IsDir() bool  { return e.dir }
func (e docEntry) Type() fs.FileMode {
	if e.dir {
		return fs.ModeDir
	}
	return 0
}
func (e docEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrNotExist }
