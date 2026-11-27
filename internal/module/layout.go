package module

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// Project part directories (RFC §23).
const (
	ContextsDir   = "contexts"   // contexts/<Context>/
	AppsDir       = "apps"       // apps/<App>/
	PrimitivesDir = "primitives" // primitives/
)

// contextName is the name of a bounded context directory: one capitalized
// name (docs/plans/0011-contexts-acl-aggregates.md, §3.6).
var contextName = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// structured reports whether the module is laid out in project parts: it
// has a contexts, apps or primitives directory. Otherwise the module is
// flat, with *.rr files at the root.
func structured(fsys fs.FS) (bool, error) {
	for _, d := range []string{ContextsDir, AppsDir, PrimitivesDir} {
		info, err := fs.Stat(fsys, d)
		if err == nil && info.IsDir() {
			return true, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

// subdirs returns the sorted names of nested directories except hidden
// ones; no directory gives an empty result.
func subdirs(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// rrFilesIn is like rrFiles, but the directory may be absent.
func rrFilesIn(fsys fs.FS, dir string) ([]string, error) {
	if _, err := fs.Stat(fsys, dir); os.IsNotExist(err) {
		return nil, nil
	}
	return rrFiles(fsys, dir)
}

// loadStructured loads the base files of a module laid out in project
// parts: bounded contexts, apps, primitives; *.rr files at the root are an error.
func (m *Module) loadStructured(fsys fs.FS, dir string, bag *diag.Bag) error {
	files, err := m.loadParts(fsys, dir, ".", bag)
	m.Base = files
	return err
}

// loadParts loads project parts from directory root (the source root or an
// extension directory): contexts/<Context>/, apps/<App>/, primitives/;
// files directly in root, contexts/ and apps/ are error E1401. File names
// are collected in order first, then the files are read and parsed in
// parallel (docs/plans/0015), then marked.
func (m *Module) loadParts(fsys fs.FS, dir, root string, bag *diag.Bag) ([]*File, error) {
	type part struct {
		name, context, app string
		prims, outside     bool
		first              bool // the first file of a bounded context: check the directory name
	}
	var parts []part
	contextsDir := path.Join(root, ContextsDir)
	contexts, err := subdirs(fsys, contextsDir)
	if err != nil {
		return nil, err
	}
	for _, c := range contexts {
		names, err := rrFiles(fsys, path.Join(contextsDir, c))
		if err != nil {
			return nil, err
		}
		for i, name := range names {
			parts = append(parts, part{name: name, context: c, first: i == 0})
		}
	}
	appsDir := path.Join(root, AppsDir)
	apps, err := subdirs(fsys, appsDir)
	if err != nil {
		return nil, err
	}
	for _, a := range apps {
		names, err := rrFiles(fsys, path.Join(appsDir, a))
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			parts = append(parts, part{name: name, app: a})
		}
	}
	prims, err := rrFilesIn(fsys, path.Join(root, PrimitivesDir))
	if err != nil {
		return nil, err
	}
	for _, name := range prims {
		parts = append(parts, part{name: name, prims: true})
	}
	// Files outside project parts: at the root, directly in contexts/ and apps/.
	for _, d := range []string{root, contextsDir, appsDir} {
		names, err := rrFilesIn(fsys, d)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			parts = append(parts, part{name: name, outside: true})
		}
	}

	names := make([]string, len(parts))
	for i, p := range parts {
		names[i] = p.name
	}
	files, err := m.cache.loadAll(m.fset, fsys, dir, names, bag)
	if err != nil {
		return nil, err
	}
	appFiles := map[string][]*File{}
	for i, p := range parts {
		f := files[i]
		switch {
		case p.context != "":
			f.Context = p.context
			if p.first && !contextName.MatchString(p.context) {
				bag.Add(diag.E1402, fileStart(f), p.context)
			}
		case p.app != "":
			f.App = p.app
			appFiles[p.app] = append(appFiles[p.app], f)
		case p.prims:
			f.Primitives = true
		case p.outside:
			f.Outside = true
			bag.Add(diag.E1401, fileStart(f), p.name)
		}
	}
	for _, a := range apps {
		if len(appFiles[a]) > 0 {
			checkApp(a, appFiles[a], bag)
		}
	}
	for _, f := range files {
		if !f.Outside {
			checkPlace(f, bag)
		}
	}
	return files, nil
}

// checkApp checks an app directory: it holds one app named like the directory.
func checkApp(dir string, files []*File, bag *diag.Bag) {
	var first *ast.AppDecl
	for _, f := range files {
		for _, d := range f.AST.Decls {
			app, ok := d.(*ast.AppDecl)
			if !ok || app.Name == nil {
				continue
			}
			switch {
			case first != nil:
				bag.Add(diag.E1403, rangeOf(app.Name), dir).Note(rangeOf(first.Name), noteHere)
			case app.Name.Name != dir:
				bag.Add(diag.E1403, rangeOf(app.Name), dir)
			}
			if first == nil {
				first = app
			}
		}
	}
	if first == nil && len(files) > 0 {
		bag.Add(diag.E1403, fileStart(files[0]), dir)
	}
}

// checkPlace checks that a file's declarations stand in their project part:
// an app in apps/, primitives and default for all apps in primitives/,
// everything else in bounded contexts; texts anywhere.
func checkPlace(f *File, bag *diag.Bag) {
	for _, d := range f.AST.Decls {
		var want, where string
		switch d.(type) {
		case *ast.TextsDecl, *ast.BadDecl:
			continue
		case *ast.AppDecl:
			want, where = AppsDir, AppsDir+"/"
		case *ast.PrimitiveDecl, *ast.DefaultDecl:
			want, where = PrimitivesDir, PrimitivesDir+"/"
		default:
			want, where = ContextsDir, ContextsDir+"/"
		}
		if f.part() == want {
			continue
		}
		end := d.End()
		if name := declName(d); name != nil {
			end = name.End()
		}
		bag.Add(diag.E1404, source.Range{Start: d.Pos(), End: end}, keyword(d), where)
	}
}

// part returns the project part directory that holds a file.
func (f *File) part() string {
	switch {
	case f.Context != "":
		return ContextsDir
	case f.App != "":
		return AppsDir
	case f.Primitives:
		return PrimitivesDir
	}
	return ""
}

// keyword returns the keyword of a declaration for messages.
func keyword(d ast.Decl) string {
	switch d := d.(type) {
	case *ast.RoleDecl:
		return "role"
	case *ast.FeatureDecl:
		return "feature"
	case *ast.EnumDecl:
		return "enum"
	case *ast.ValueDecl:
		return "value"
	case *ast.EntityDecl:
		switch {
		case d.IsActor:
			return "actor"
		case d.IsAggregate:
			return "aggregate"
		}
		return "entity"
	case *ast.ProjectionDecl:
		return "projection"
	case *ast.ExternalDecl:
		return "external"
	case *ast.FakeDecl:
		return "fake"
	case *ast.ACLDecl:
		return "acl"
	case *ast.MigrationDecl:
		return "migration"
	case *ast.TestDecl:
		return "test"
	case *ast.LetDecl:
		return "let"
	case *ast.AppDecl:
		return "app"
	case *ast.ScreenDecl:
		return "screen"
	case *ast.PrimitiveDecl:
		return "primitive"
	case *ast.DefaultDecl:
		return "default"
	}
	return "declaration"
}

// fileStart is the place of errors about a whole file: its start.
func fileStart(f *File) source.Range {
	p := f.Src.Pos(0)
	return source.Range{Start: p, End: p}
}
