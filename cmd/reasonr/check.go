package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tycheg/reasonr-compiler/internal/compat"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/parser"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// checkCmd is reasonr check [--extension name] [--against schema.json] [--format text|json] [path ...].
func checkCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("check", flag.ContinueOnError)
	fl.SetOutput(stderr)
	format := fl.String("format", "text", "output format: text or json")
	ext := fl.String("extension", "", "check only the model with an extension (the base model and extensions/<name>/), or base for the base one only; all by default")
	againstPath := fl.String("against", "", "compare the model with the schema of a previous version (reasonr build --dump-schema)")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "reasonr: unknown format %q\n", *format)
		return 2
	}
	var against *model.Schema
	if *againstPath != "" {
		// The schema is of one model: without --extension the base one is compared.
		if *ext == "" {
			*ext = pipeline.Base
		}
		data, err := os.ReadFile(*againstPath)
		if err == nil {
			against, err = model.ParseSchema(data)
		}
		if err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
	}

	paths := fl.Args()
	if len(paths) == 0 {
		paths = []string{defaultPath()}
	}
	fset := source.NewFileSet()
	var all []*diag.Diagnostic
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
		if !info.IsDir() {
			src, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(stderr, "reasonr: %v\n", err)
				return 2
			}
			_, diags := parser.ParseFile(fset, filepath.ToSlash(path), src)
			all = append(all, diags...)
			continue
		}
		dirs, err := module.Find(path)
		if err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
		if len(dirs) == 0 {
			fmt.Fprintf(stderr, "reasonr: %s has no *.rr files\n", path)
			return 2
		}
		if against != nil && (len(dirs) > 1 || len(paths) > 1) {
			fmt.Fprintln(stderr, "reasonr: --against compares one module")
			return 2
		}
		for _, dir := range dirs {
			diags, err := checkModule(fset, dir, *ext, against)
			if err != nil {
				fmt.Fprintf(stderr, "reasonr: %v\n", err)
				return 2
			}
			all = append(all, diags...)
		}
	}
	diag.Sort(all)

	var err error
	if *format == "json" {
		err = diag.WriteJSON(stdout, fset, all)
	} else {
		err = diag.WriteText(stdout, fset, all)
	}
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	if diag.HasErrors(all) {
		return 1
	}
	return 0
}

// checkModule loads a module, builds the models ext (pipeline.Select) and
// checks them; against is the schema of a previous version for the
// compatibility check, or nil.
func checkModule(fset *source.FileSet, dir, ext string, against *model.Schema) ([]*diag.Diagnostic, error) {
	m, err := pipeline.Open(fset, dir)
	if err != nil {
		return nil, err
	}
	var diags []*diag.Diagnostic
	for _, t := range m.Select(ext) {
		model, err := m.Model(t)
		if err != nil {
			return nil, err
		}
		diags = append(diags, model.Diagnostics...)
		// Compatibility is compared only for a model without errors and only
		// for the production program: tests and fakes are not in the schema.
		if against != nil && !model.HasErrors() {
			prod, err := m.ProductionModel(t)
			if err != nil {
				return nil, err
			}
			diags = append(diags, compat.Check(against, prod.Program, prod.Info)...)
		}
	}
	return dedupe(diags), nil
}

// dedupe removes repeats: models with extensions include the base files, and
// errors in them are found when each model is checked.
func dedupe(list []*diag.Diagnostic) []*diag.Diagnostic {
	type key struct {
		code diag.Code
		r    source.Range
		msg  string
	}
	seen := map[key]bool{}
	out := list[:0]
	for _, d := range list {
		k := key{d.Code(), d.Range, d.Message()}
		if !seen[k] {
			seen[k] = true
			out = append(out, d)
		}
	}
	return out
}
