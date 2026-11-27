package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/tools"
)

// featureCmd is reasonr feature list [--format text|json] [path] |
// reasonr feature remove [--off] <flag> [path] (RFC §23.4).
func featureCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "list":
			return featureListCmd(args[1:], stdout, stderr)
		case "remove":
			return featureRemoveCmd(args[1:], stdout, stderr)
		}
	}
	fmt.Fprintln(stderr, "reasonr: feature list lists source feature flags; feature remove [--off] <flag> removes a rolled-out flag from the code")
	return 2
}

// loadModels loads a module and checks its models: the base one and all
// extensions. Model errors are printed; nil means there are errors.
func loadModels(path string, stderr io.Writer) (*pipeline.Module, []*pipeline.Model, int) {
	dirs, err := module.Find(path)
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return nil, nil, 2
	}
	if len(dirs) != 1 {
		fmt.Fprintf(stderr, "reasonr: %s must hold one module, found %d\n", path, len(dirs))
		return nil, nil, 2
	}
	fset := source.NewFileSet()
	mod, err := pipeline.Open(fset, dirs[0])
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return nil, nil, 2
	}
	var models []*pipeline.Model
	var all []*diag.Diagnostic
	for _, ext := range mod.Select("") {
		m, err := mod.Model(ext)
		if err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return nil, nil, 2
		}
		all = append(all, m.Diagnostics...)
		models = append(models, m)
	}
	if diag.HasErrors(all) {
		_ = diag.WriteText(stderr, fset, all)
		return nil, nil, 1
	}
	return mod, models, 0
}

func featureListCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("feature list", flag.ContinueOnError)
	fl.SetOutput(stderr)
	format := fl.String("format", "text", "format: text or json")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "reasonr: unknown format %q\n", *format)
		return 2
	}
	path, ok := onePath(fl.Args(), "feature list", stderr)
	if !ok {
		return 2
	}
	mod, models, code := loadModels(path, stderr)
	if code != 0 {
		return code
	}
	if err := tools.WriteFeatures(stdout, tools.Features(models, time.Now()), *format, mod.Dir); err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	return 0
}

func featureRemoveCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("feature remove", flag.ContinueOnError)
	fl.SetOutput(stderr)
	off := fl.Bool("off", false, "keep the \"off\" branch (the feature is not needed) and remove the tests that turned the flag on")
	name, rest := "", args
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		name, rest = args[0], args[1:]
	}
	if err := fl.Parse(rest); err != nil {
		return 2
	}
	pathArgs := fl.Args()
	if name == "" && len(pathArgs) > 0 {
		name, pathArgs = pathArgs[0], pathArgs[1:]
	}
	if name == "" {
		fmt.Fprintln(stderr, "reasonr: name a flag: reasonr feature remove tips")
		return 2
	}
	path, ok := onePath(pathArgs, "feature remove", stderr)
	if !ok {
		return 2
	}
	_, models, code := loadModels(path, stderr)
	if code != 0 {
		return code
	}
	r, err := tools.RemoveFeature(models, name, *off)
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 1
	}
	for p, data := range r.Files {
		info, err := os.Stat(p)
		if err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
		if err := os.WriteFile(p, data, info.Mode()); err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, relPath(p))
	}
	for _, t := range r.Tests {
		fmt.Fprintf(stdout, "removed test %q\n", t)
	}
	branch := `"on"`
	if *off {
		branch = `"off"`
	}
	fmt.Fprintf(stderr, "reasonr: flag %s removed, the %s branch is kept; run reasonr check and reasonr test\n", r.Feature, branch)
	return 0
}
