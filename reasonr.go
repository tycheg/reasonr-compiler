// Package reasonr is the compiler's contract with the engine (RFC §30):
// building a model and the model as an object with methods. It is the only
// package of the compiler other modules import; the rest is internal and
// changes in any release.
//
// The engine builds a model from the sources (Build, Load) or reads one
// built in advance (Open, reasonr build -o) and calls its methods: commands
// and queries (Execute, Query), migrations, acceptance tests and scenarios,
// screens, API views, the compatibility of stored data. The values the
// model exchanges with the engine are the boundary types of this package:
// Value in an Arena, Record, Schema and the call and result types.
package reasonr

import (
	"bytes"
	"fmt"
	"io/fs"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// Options select the model Build builds.
type Options struct {
	// Extension is the tenant extension (extensions/<name>/); "" is the
	// base model.
	Extension string
	// Test builds the test program: with acceptance tests, demo and
	// performance scenarios and the fakes of external systems. Otherwise
	// the production program.
	Test bool
}

// Errors are the errors of a model, already rendered: the engine only
// prints them. It is the error Build, Load and Module.Build return when the
// model has errors.
type Errors struct {
	// Text is how reasonr check prints them.
	Text []byte
	// JSON is how reasonr check --format json prints them (AGENTS.md).
	JSON []byte
}

func (e *Errors) Error() string {
	if line, _, ok := bytes.Cut(e.Text, []byte("\n")); ok || len(line) > 0 {
		return "the model has errors: " + string(line)
	}
	return "the model has errors"
}

// render returns the diagnostics as Errors.
func render(fset *source.FileSet, list []*diag.Diagnostic) *Errors {
	var text, js bytes.Buffer
	_ = diag.WriteText(&text, fset, list)
	_ = diag.WriteJSON(&js, fset, list)
	return &Errors{Text: text.Bytes(), JSON: js.Bytes()}
}

// Module is the loaded sources of a model module (a project with its
// dependency modules): the base model and its extensions build from it
// without reading the files again.
type Module struct {
	mod *pipeline.Module
	dir string
}

// Load loads the module in the directory dir with the modules of its
// project (reasonr.yaml, the module cache). Load errors are *Errors.
func Load(dir string) (*Module, error) {
	fset := source.NewFileSet()
	m, err := pipeline.Open(fset, dir)
	if err != nil {
		return nil, err
	}
	return &Module{mod: m, dir: dir}, nil
}

// LoadFS loads the module in the directory dir of the file system fsys,
// without the modules of a project.
func LoadFS(fsys fs.FS, dir string) (*Module, error) {
	m, err := pipeline.OpenFS(source.NewFileSet(), fsys, dir)
	if err != nil {
		return nil, err
	}
	return &Module{mod: m, dir: dir}, nil
}

// Dir is the module directory as given to Load.
func (m *Module) Dir() string { return m.dir }

// Extensions are the names of the module's extensions in name order.
func (m *Module) Extensions() []string {
	out := make([]string, 0, len(m.mod.Extensions))
	for _, e := range m.mod.Extensions {
		out = append(out, e.Name)
	}
	return out
}

// Build builds and checks a model of the module. Model errors are *Errors;
// another error is a compiler fault.
func (m *Module) Build(opts Options) (*Model, error) {
	if opts.Extension != "" && !m.has(opts.Extension) {
		return nil, fmt.Errorf("the module has no extension %q", opts.Extension)
	}
	var (
		pm  *pipeline.Model
		err error
	)
	if opts.Test {
		pm, err = m.mod.Model(opts.Extension)
	} else {
		pm, err = m.mod.ProductionModel(opts.Extension)
	}
	if err != nil {
		return nil, err
	}
	if pm.HasErrors() {
		return nil, render(m.mod.FileSet, pm.Diagnostics)
	}
	return newModel(pm)
}

func (m *Module) has(ext string) bool {
	for _, e := range m.mod.Extensions {
		if e.Name == ext {
			return true
		}
	}
	return false
}

// Build loads the module in dir and builds a model of it.
func Build(dir string, opts Options) (*Model, error) {
	m, err := Load(dir)
	if err != nil {
		return nil, err
	}
	return m.Build(opts)
}

// FindModules returns the module directories inside root: a directory with
// *.rr files or project parts is a module; nested directories are walked,
// except hidden ones and testdata.
func FindModules(root string) ([]string, error) { return module.Find(root) }

// FileModule returns the module directory of a *.rr file and its extension
// ("" for a base file).
func FileModule(file string) (dir, ext string) { return module.FileModule(file) }
