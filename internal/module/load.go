// Package module loads a ReasonR module from a directory and builds the
// effective model with an extension: the base declarations plus the
// extension (RFC §28).
//
// The module directory is the project sources (RFC §23,
// decision 21):
//
//	contexts/<System>.<Context>/*.rr  a bounded context: model, tests (*.test.rr),
//	                                  external system fakes (*.fake.rr), texts
//	apps/<App>/*.rr                   an app and its texts
//	primitives/*.rr                   primitives and default for all apps
//	extensions/<name>/*.rr            an extension: customization for a customer (name is the directory)
//
// A module without contexts, apps and primitives is flat: the base model is
// in the *.rr files at the root, as one unnamed bounded context. Other
// nested directories are not part of the module.
package module

import (
	"bytes"
	"fmt"
	"github.com/tycheg/reasonr-compiler/internal/apicontract"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/parser"
	"github.com/tycheg/reasonr-compiler/internal/project"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/std"
)

// ExtensionsDir is the extensions directory inside a module.
const ExtensionsDir = "extensions"

// Module is a loaded module: base files and extensions.
type Module struct {
	Dir        string
	Base       []*File
	Extensions []*Extension // by name
	// Std is the standard UI catalog (std/ui.rr): it is included in every
	// module implicitly.
	Std *File
	// API is the module's external API contract (the api/ directory,
	// RFC §31.3.1); nil when there is no directory.
	API *apicontract.Source

	fset  *source.FileSet
	cache *Cache
}

// File is a parsed module file.
type File struct {
	Name string // path for diagnostics
	Rel  string // path relative to the module directory: "order.rr", "extensions/acme/order.rr"
	Test bool   // *.test.rr and *.demo.rr: tests and demo scenarios
	Demo bool   // *.demo.rr: demo scenarios (RFC §27.5)
	Perf bool   // *.perf.rr: performance tests (RFC §27.6)
	Fake bool   // *.fake.rr
	// The project part: a bounded context ("Taxi.Ordering"), an app,
	// primitives; Outside is a file outside the parts (error E1401). A flat
	// module and extensions have no parts.
	Context    string
	App        string
	Primitives bool
	Outside    bool
	// Dep is the dependency module the file comes from: the module path; "" for the project.
	Dep string
	Src *source.File
	AST *ast.File
}

// Extension is an extension: the files from extensions/<name>/.
type Extension struct {
	Name  string
	Files []*File
}

// Extension returns an extension or nil.
func (m *Module) Extension(name string) *Extension {
	for _, t := range m.Extensions {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// Load reads and parses a module from directory dir. An error is returned
// only on a read failure; syntax and heading errors are in the diagnostics.
func Load(fset *source.FileSet, dir string) (*Module, []*diag.Diagnostic, error) {
	deps, err := DepsOf(dir)
	if err != nil {
		return nil, nil, err
	}
	return LoadDeps(fset, os.DirFS(dir), dir, deps, nil)
}

// DepsOf resolves the dependency modules of the project whose sources are
// dir (reasonr.yaml above it, compiler/project); nil without a project or modules.
func DepsOf(dir string) ([]Dep, error) {
	pdir, f, err := project.ForSrc(dir)
	if err != nil {
		return nil, err
	}
	var ds []project.Dep
	if f == nil {
		ds, err = project.EnvDeps()
	} else {
		ds, err = project.Deps(pdir, f)
	}
	if err != nil {
		return nil, err
	}
	out := make([]Dep, len(ds))
	for i, d := range ds {
		out[i] = Dep{Path: d.Path, Src: d.Src}
	}
	return out, nil
}

// LoadFS is like Load but reads the module from fsys. File names in
// diagnostics start with dir.
func LoadFS(fset *source.FileSet, fsys fs.FS, dir string) (*Module, []*diag.Diagnostic, error) {
	return LoadCached(fset, fsys, dir, nil)
}

// Dep is a project dependency module: a path and a source directory (M5).
type Dep struct {
	Path string
	Src  string
}

// LoadCached is like LoadFS, but takes files whose text did not change since
// the previous load from the cache without parsing them again (LSP). All
// loads with one cache must use one file set fset. cache == nil means no cache.
func LoadCached(fset *source.FileSet, fsys fs.FS, dir string, cache *Cache) (*Module, []*diag.Diagnostic, error) {
	return LoadDeps(fset, fsys, dir, nil, cache)
}

// LoadDeps is like LoadCached, with dependency modules deps: their systems
// go into the project's base model (RFC §23); module tests do not.
func LoadDeps(fset *source.FileSet, fsys fs.FS, dir string, deps []Dep, cache *Cache) (*Module, []*diag.Diagnostic, error) {
	m := &Module{Dir: dir, fset: fset, cache: cache}
	var bag diag.Bag
	m.Std = cache.loadStd(fset, &bag)

	parts, err := structured(fsys)
	if err != nil {
		return nil, nil, err
	}
	if parts {
		err = m.loadStructured(fsys, dir, &bag)
	} else {
		err = m.loadFlat(fsys, dir, &bag)
	}
	if err != nil {
		return nil, nil, err
	}
	if m.API, err = apicontract.Load(fsys); err != nil {
		return nil, nil, err
	}
	for _, dep := range deps {
		files, err := m.loadParts(os.DirFS(dep.Src), dep.Src, ".", &bag)
		if err != nil {
			return nil, nil, fmt.Errorf("module %s: %w", dep.Path, err)
		}
		for _, f := range files {
			if !f.Test {
				f.Dep = dep.Path
				m.Base = append(m.Base, f)
			}
		}
	}
	for _, f := range m.Base {
		for _, d := range f.AST.Decls {
			switch d := d.(type) {
			case *ast.ExtendDecl:
				bag.Add(diag.E1009, source.Range{Start: d.Extend, End: d.Name.End()}, "extend")
			case *ast.ExcludeDecl:
				bag.Add(diag.E1009, source.Range{Start: d.Exclude, End: d.Test.End()}, "exclude")
			}
		}
		checkFakes(f, &bag)
		checkACLFile(f, &bag)
	}

	checkFakeSystems(m.Base, &bag)

	exts, err := fs.ReadDir(fsys, ExtensionsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	for _, e := range exts {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		t := &Extension{Name: e.Name()}
		if parts {
			// An extension of a laid-out project repeats its parts (M4).
			if t.Files, err = m.loadParts(fsys, dir, path.Join(ExtensionsDir, t.Name), &bag); err != nil {
				return nil, nil, err
			}
		} else {
			names, err := rrFiles(fsys, path.Join(ExtensionsDir, t.Name))
			if err != nil {
				return nil, nil, err
			}
			for _, name := range names {
				f, err := cache.load(fset, fsys, dir, name, &bag)
				if err != nil {
					return nil, nil, err
				}
				t.Files = append(t.Files, f)
			}
		}
		for _, f := range t.Files {
			checkFakes(f, &bag)
			checkACLFile(f, &bag)
		}
		m.Extensions = append(m.Extensions, t)
	}
	cache.prune(m)
	return m, bag.List(), nil
}

// loadFlat loads the base files of a flat module: *.rr at the root.
func (m *Module) loadFlat(fsys fs.FS, dir string, bag *diag.Bag) error {
	names, err := rrFiles(fsys, ".")
	if err != nil {
		return err
	}
	for _, name := range names {
		f, err := m.cache.load(m.fset, fsys, dir, name, bag)
		if err != nil {
			return err
		}
		m.Base = append(m.Base, f)
	}
	return nil
}

// rrFiles returns the sorted paths of *.rr files in a directory (not nested).
func rrFiles(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".rr") {
			out = append(out, path.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// Cache holds parsed module files by name for LoadCached.
type Cache struct {
	files map[string]*cached
	std   *cached
	gen   map[string]*cached // generated files by name and text
}

type cached struct {
	file  *File
	diags []*diag.Diagnostic
}

// NewCache creates an empty cache.
func NewCache() *Cache { return &Cache{files: map[string]*cached{}} }

// Size returns the total size of the files in the cache.
func (c *Cache) Size() int {
	n := 0
	for _, e := range c.files {
		n += e.file.Src.Size()
	}
	return n
}

// prune removes files that are no longer in the module from the cache.
func (c *Cache) prune(m *Module) {
	if c == nil {
		return
	}
	live := map[string]bool{}
	for _, f := range m.Base {
		live[f.Name] = true
	}
	for _, t := range m.Extensions {
		for _, f := range t.Files {
			live[f.Name] = true
		}
	}
	for name := range c.files {
		if !live[name] {
			delete(c.files, name)
		}
	}
}

// loadStd parses the standard UI catalog once per cache.
func (c *Cache) loadStd(fset *source.FileSet, bag *diag.Bag) *File {
	if c != nil && c.std != nil {
		bag.Append(c.std.diags...)
		return c.std.file
	}
	sf := fset.AddFile(std.UIName, std.UI)
	file, diags := parser.Parse(sf)
	bag.Append(diags...)
	f := &File{Name: std.UIName, Rel: std.UIName, Src: sf, AST: file}
	if c != nil {
		c.std = &cached{file: f, diags: diags}
	}
	return f
}

// Generate parses text built by the compiler (the layout of an auto app)
// as module file name. The same text is parsed once per module cache.
func (m *Module) Generate(name string, src []byte) (*File, []*diag.Diagnostic) {
	key := name + "\x00" + string(src)
	if c := m.cache; c != nil && c.gen[key] != nil {
		e := c.gen[key]
		return e.file, e.diags
	}
	sf := m.fset.AddFile(name, src)
	file, diags := parser.Parse(sf)
	f := &File{Name: name, Rel: name, Src: sf, AST: file}
	if c := m.cache; c != nil {
		if c.gen == nil || len(c.gen) > 64 { // LSP: old variants do not pile up
			c.gen = map[string]*cached{}
		}
		c.gen[key] = &cached{file: f, diags: diags}
	}
	return f, diags
}

// load reads and parses a file; a nil cache always parses.
func (c *Cache) load(fset *source.FileSet, fsys fs.FS, dir, name string, bag *diag.Bag) (*File, error) {
	files, err := c.loadAll(fset, fsys, dir, []string{name}, bag)
	if err != nil {
		return nil, err
	}
	return files[0], nil
}

// loadAll loads files names: it reads and parses them in parallel, while
// places in fset are assigned in name order, so positions do not depend on
// scheduling. Parse diagnostics go into bag in file order.
func (c *Cache) loadAll(fset *source.FileSet, fsys fs.FS, dir string, names []string, bag *diag.Bag) ([]*File, error) {
	type item struct {
		src     []byte
		err     error
		display string
		sf      *source.File
		file    *File
		diags   []*diag.Diagnostic
	}
	items := make([]item, len(names))
	parallel(len(names), func(i int) {
		it := &items[i]
		it.src, it.err = fs.ReadFile(fsys, names[i])
		it.display = filepath.ToSlash(filepath.Join(dir, filepath.FromSlash(names[i])))
	})
	for i := range items {
		it := &items[i]
		if it.err != nil {
			return nil, it.err
		}
		if c != nil {
			if e := c.files[it.display]; e != nil && bytes.Equal(e.file.Src.Source(), it.src) {
				it.file, it.diags = e.file, e.diags
				continue
			}
		}
		it.sf = fset.AddFile(it.display, it.src)
	}
	parallel(len(names), func(i int) {
		it := &items[i]
		if it.sf == nil {
			return
		}
		ast, diags := parser.Parse(it.sf)
		name := names[i]
		it.file = &File{Name: it.display, Rel: name, Test: strings.HasSuffix(name, ".test.rr") || strings.HasSuffix(name, ".demo.rr") || strings.HasSuffix(name, ".perf.rr"),
			Demo: strings.HasSuffix(name, ".demo.rr"), Perf: strings.HasSuffix(name, ".perf.rr"), Fake: strings.HasSuffix(name, ".fake.rr"), Src: it.sf, AST: ast}
		it.diags = diags
	})
	files := make([]*File, len(names))
	for i := range items {
		it := &items[i]
		bag.Append(it.diags...)
		if c != nil && it.sf != nil {
			c.files[it.display] = &cached{file: it.file, diags: it.diags}
		}
		files[i] = it.file
	}
	return files, nil
}

// parallel calls f(0..n-1) on GOMAXPROCS goroutines.
func parallel(n int, f func(int)) {
	workers := min(runtime.GOMAXPROCS(0), n)
	if workers <= 1 {
		for i := range n {
			f(i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for i := int(next.Add(1) - 1); i < n; i = int(next.Add(1) - 1) {
				f(i)
			}
		}()
	}
	wg.Wait()
}

// checkFakes checks where fakes stand: fake only in *.fake.rr of the base
// module, and only fake in those files. A *.fake.rr file in an extension
// directory is not a fakes file.
func checkFakes(f *File, bag *diag.Bag) {
	fakeFile := f.Fake && !strings.HasPrefix(f.Rel, ExtensionsDir+"/")
	for _, d := range f.AST.Decls {
		fake, ok := d.(*ast.FakeDecl)
		switch {
		case ok && !fakeFile:
			bag.Add(diag.E1204, source.Range{Start: fake.Fake, End: fake.Name.End()})
		case !ok && fakeFile:
			if _, bad := d.(*ast.BadDecl); !bad {
				end := d.End()
				if name := declName(d); name != nil {
					end = name.End()
				}
				bag.Add(diag.E1205, source.Range{Start: d.Pos(), End: end})
			}
		}
	}
}

// checkACLFile checks where ACLs stand: acl only in *.acl.rr, and only acl
// in those files (docs/plans/0011, §3.1).
func checkACLFile(f *File, bag *diag.Bag) {
	aclFile := strings.HasSuffix(f.Rel, ".acl.rr")
	for _, d := range f.AST.Decls {
		a, ok := d.(*ast.ACLDecl)
		switch {
		case ok && !aclFile:
			bag.Add(diag.E1412, source.Range{Start: a.ACL, End: a.Source.End()}, a.Source.Name, strings.ToLower(a.Source.Name))
		case !ok && aclFile:
			if _, bad := d.(*ast.BadDecl); !bad {
				end := d.End()
				if name := declName(d); name != nil {
					end = name.End()
				}
				bag.Add(diag.E1413, source.Range{Start: d.Pos(), End: end})
			}
		}
	}
}

// checkFakeSystems checks the fakes of the base module: a fake has an
// external system with a role (external(role)), and a system has one fake.
func checkFakeSystems(files []*File, bag *diag.Bag) {
	systems := map[string]*ast.ExternalDecl{}
	for _, f := range files {
		if f.Fake {
			continue
		}
		for _, d := range f.AST.Decls {
			if e, ok := d.(*ast.ExternalDecl); ok && systems[f.Context+"\x00"+e.Name.Name] == nil {
				systems[f.Context+"\x00"+e.Name.Name] = e
			}
		}
	}
	seen := map[string]*ast.FakeDecl{}
	for _, f := range files {
		if !f.Fake {
			continue
		}
		for _, d := range f.AST.Decls {
			fake, ok := d.(*ast.FakeDecl)
			if !ok {
				continue
			}
			name := fake.Name.Name
			key := f.Context + "\x00" + name
			if e := systems[key]; e == nil || e.Access == nil {
				bag.Add(diag.E1206, rangeOf(fake.Name), name, name)
				continue
			}
			if prev := seen[key]; prev != nil {
				bag.Add(diag.E0101, rangeOf(fake.Name), name, "an external system has one fake").
					Note(rangeOf(prev.Name), noteHere)
				continue
			}
			seen[key] = fake
		}
	}
}

// Find returns the module directories inside root: a directory with *.rr
// files or with a project part directory (contexts, apps, primitives,
// extensions) is a module (not descended into); otherwise nested
// directories are walked, except hidden ones and testdata.
func Find(root string) ([]string, error) {
	var out []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		var subdirs []string
		for _, e := range entries {
			if e.IsDir() {
				if isPartDir(e.Name()) {
					out = append(out, dir)
					return nil
				}
				if !strings.HasPrefix(e.Name(), ".") && e.Name() != "testdata" {
					subdirs = append(subdirs, filepath.Join(dir, e.Name()))
				}
				continue
			}
			if strings.HasSuffix(e.Name(), ".rr") {
				out = append(out, dir)
				return nil
			}
		}
		for _, d := range subdirs {
			if err := walk(d); err != nil {
				return err
			}
		}
		return nil
	}
	err := walk(root)
	return out, err
}

func isPartDir(name string) bool {
	switch name {
	case ContextsDir, AppsDir, PrimitivesDir, ExtensionsDir:
		return true
	}
	return false
}

// FileModule returns the module directory of a *.rr file and its extension
// ("" for a base file): the file's directory, or for files of project parts
// the directory above contexts/, apps/, primitives/ or extensions/.
func FileModule(file string) (dir, ext string) {
	dir = filepath.Dir(file)
	up := func(d string, n int) string {
		for range n {
			d = filepath.Dir(d)
		}
		return d
	}
	// A project part: a file in contexts/<C>/, apps/<A>/, primitives/ or directly in contexts/, apps/.
	switch {
	case filepath.Base(up(dir, 1)) == ContextsDir, filepath.Base(up(dir, 1)) == AppsDir:
		dir = up(dir, 2)
	case filepath.Base(dir) == PrimitivesDir, filepath.Base(dir) == ContextsDir, filepath.Base(dir) == AppsDir:
		dir = up(dir, 1)
	}
	// An extension: the directory above extensions/<name>/.
	if filepath.Base(up(dir, 1)) == ExtensionsDir {
		return up(dir, 2), filepath.Base(dir)
	}
	return dir, ""
}
