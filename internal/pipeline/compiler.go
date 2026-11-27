// Package pipeline is the common entry point for the CLI commands, the LSP
// and the tools: loading a module, building a model (base or with an
// extension) and all checks (RFC §28).
package pipeline

import (
	"io/fs"
	"os"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/compat"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// Module is a loaded module: models with extensions are built from it.
type Module struct {
	*module.Module
	FileSet *source.FileSet
	// Diagnostics are the errors of loading and parsing files.
	Diagnostics []*diag.Diagnostic
}

// Model is a checked model: the base one or one with an extension.
type Model struct {
	Module  *Module
	Program *module.Program
	Info    *sema.Info
	Checks  *check.Result
	// Diagnostics are all diagnostics of the model: module loading,
	// extension assembly, names and types, model checks. Sorted.
	Diagnostics []*diag.Diagnostic
}

// Open loads a module from a directory together with the project's
// dependency modules if the directory holds the sources of a project with
// reasonr.yaml (M5).
func Open(fset *source.FileSet, dir string) (*Module, error) {
	return OpenCached(fset, os.DirFS(dir), dir, nil)
}

// OpenFS loads a module from directory dir of file system fsys.
func OpenFS(fset *source.FileSet, fsys fs.FS, dir string) (*Module, error) {
	m, diags, err := module.LoadFS(fset, fsys, dir)
	if err != nil {
		return nil, err
	}
	return &Module{Module: m, FileSet: fset, Diagnostics: diags}, nil
}

// OpenCached is like OpenFS but takes unchanged files from the parse cache
// (module.LoadCached), with the dependency modules of the project of
// directory dir. All loads with one cache share one fset.
func OpenCached(fset *source.FileSet, fsys fs.FS, dir string, cache *module.Cache) (*Module, error) {
	ds, err := module.DepsOf(dir)
	if err != nil {
		return nil, err
	}
	m, diags, err := module.LoadDeps(fset, fsys, dir, ds, cache)
	if err != nil {
		return nil, err
	}
	return &Module{Module: m, FileSet: fset, Diagnostics: diags}, nil
}

// Base is the base model without an extension in the --extension flag.
const Base = "base"

// Select lists the module's models that commands with the --extension flag
// check: ext == "" gives the base model and the models of all extensions,
// Base only the base one, anything else the model of one extension. "" in
// the result is the base model.
func (m *Module) Select(ext string) []string {
	switch ext {
	case "":
		all := []string{""}
		for _, t := range m.Extensions {
			all = append(all, t.Name)
		}
		return all
	case Base:
		return []string{""}
	}
	return []string{ext}
}

// Model builds and checks a test model with an extension, with tests and
// fakes of external systems; ext == "" or Base gives the base model.
func (m *Module) Model(ext string) (*Model, error) {
	return m.model(m.Program(baseName(ext)))
}

// ProductionModel builds and checks a production model with an extension,
// without tests and fakes: the engine runs it outside test mode, and its
// schema is compared with the previous version.
func (m *Module) ProductionModel(ext string) (*Model, error) {
	return m.model(m.Production(baseName(ext)))
}

func baseName(ext string) string {
	if ext == Base {
		return ""
	}
	return ext
}

func (m *Module) model(p *module.Program, diags []*diag.Diagnostic, err error) (*Model, error) {
	if err != nil {
		return nil, err
	}
	all := append(append([]*diag.Diagnostic(nil), m.Diagnostics...), diags...)
	info, d := sema.Check(p)
	all = append(all, d...)
	res := check.Analyze(p, info, check.All)
	all = append(all, res.Diagnostics...)
	if src := p.API(); src != nil && !diag.HasErrors(all) {
		// External API contract: the model against released versions (RFC §31.3.1).
		all = append(all, compat.API(src, schemagen.Generate(p, info), info, time.Now().Format(time.DateOnly))...)
	}
	diag.Sort(all)
	return &Model{Module: m, Program: p, Info: info, Checks: res, Diagnostics: all}, nil
}

// Load loads a module and builds a model with an extension.
func Load(fset *source.FileSet, dir, ext string) (*Model, error) {
	m, err := Open(fset, dir)
	if err != nil {
		return nil, err
	}
	return m.Model(ext)
}

// HasErrors reports whether the model has errors.
func (m *Model) HasErrors() bool { return diag.HasErrors(m.Diagnostics) }

// Graph returns the command call graph.
func (m *Model) Graph() *check.Graph {
	g, _ := m.Checks.Of(check.CallGraph).(*check.Graph)
	return g
}

// Source returns the source text of a node, or "".
func (m *Model) Source(r source.Range) string {
	if f := m.Program.FileAt(r.Start); f != nil && f.Src.Contains(r.End) {
		src := f.Src.Source()
		return string(src[f.Src.Offset(r.Start):f.Src.Offset(r.End)])
	}
	return ""
}
