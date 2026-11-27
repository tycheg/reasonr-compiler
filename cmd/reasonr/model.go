package main

import (
	"fmt"
	"io"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// loadModel loads one module by path and builds the model with an
// extension: the production one (without tests and fakes) or the test one.
// Diagnostics are printed to stderr. If the model cannot be built or has
// errors, it returns nil and the exit code.
func loadModel(path, ext string, production bool, stderr io.Writer) (*pipeline.Model, int) {
	dirs, err := module.Find(path)
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return nil, 2
	}
	switch len(dirs) {
	case 0:
		fmt.Fprintf(stderr, "reasonr: %s has no *.rr files\n", path)
		return nil, 2
	case 1:
	default:
		fmt.Fprintf(stderr, "reasonr: %s holds several modules, name one: %v\n", path, dirs)
		return nil, 2
	}
	fset := source.NewFileSet()
	mod, err := pipeline.Open(fset, dirs[0])
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return nil, 2
	}
	model := mod.Model
	if production {
		model = mod.ProductionModel
	}
	m, err := model(ext)
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return nil, 2
	}
	if len(m.Diagnostics) > 0 {
		if err := diag.WriteText(stderr, fset, m.Diagnostics); err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return nil, 2
		}
	}
	if m.HasErrors() {
		return nil, 1
	}
	return m, 0
}

// onePath returns the only path from the arguments ("." by default).
func onePath(args []string, cmd string, stderr io.Writer) (string, bool) {
	switch len(args) {
	case 0:
		return defaultPath(), true
	case 1:
		return args[0], true
	}
	fmt.Fprintf(stderr, "reasonr: %s works with one module\n", cmd)
	return "", false
}
