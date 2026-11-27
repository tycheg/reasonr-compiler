package main

import (
	"fmt"
	"io"

	modules "github.com/tycheg/reasonr-compiler/internal/project"
)

// modCmd is reasonr mod download | verify: the project's modules
// (RFC §23). download downloads the modules from requires into
// the module cache and writes reasonr.lock; verify checks the modules in
// the cache against reasonr.lock.
func modCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "download" && args[0] != "verify" {
		fmt.Fprintln(stderr, "reasonr: usage: reasonr mod download | verify")
		return 2
	}
	p, err := findProject(".")
	if err != nil || p == nil {
		fmt.Fprintf(stderr, "reasonr: mod: no %s in the current directory or above %v\n", projectFile, errText(err))
		return 2
	}
	f, err := modules.Read(p.Dir)
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	var deps []modules.Dep
	if args[0] == "download" {
		deps, err = modules.Download(p.Dir, f, stderr)
	} else {
		deps, err = modules.Deps(p.Dir, f)
	}
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 1
	}
	for _, d := range deps {
		where := "cache"
		if d.Replaced {
			where = "replace"
		}
		fmt.Fprintf(stdout, "%s %s %s (%s)\n", d.Path, d.Version, relPath(d.Dir), where)
	}
	return 0
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return ": " + err.Error()
}
