package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/format"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// fmtCmd is reasonr fmt [-w] [-l] [--check] [path ...].
func fmtCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("fmt", flag.ContinueOnError)
	fl.SetOutput(stderr)
	write := fl.Bool("w", false, "write the result to the files")
	listOnly := fl.Bool("l", false, "list the files whose formatting differs")
	checkOnly := fl.Bool("check", false, "like -l, but exit with code 1 if there are such files (for CI)")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	paths := fl.Args()
	if len(paths) == 0 {
		paths = []string{defaultPath()}
	}
	files, err := collect(paths)
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}

	fset := source.NewFileSet()
	var all []*diag.Diagnostic
	unformatted := 0
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
		out, diags := format.Source(fset, filepath.ToSlash(name), src)
		if diag.HasErrors(diags) {
			all = append(all, diags...)
			continue
		}
		changed := !bytes.Equal(src, out)
		if changed {
			unformatted++
		}
		switch {
		case *listOnly || *checkOnly:
			if changed {
				fmt.Fprintln(stdout, name)
			}
		case *write:
			if changed {
				if err := writeFile(name, out); err != nil {
					fmt.Fprintf(stderr, "reasonr: %v\n", err)
					return 2
				}
			}
		default:
			_, _ = stdout.Write(out)
		}
	}
	if len(all) > 0 {
		diag.Sort(all)
		if err := diag.WriteText(stderr, fset, all); err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
		return 1
	}
	if *checkOnly && unformatted > 0 {
		return 1
	}
	return 0
}

// writeFile replaces the content of a file keeping its permissions.
func writeFile(name string, data []byte) error {
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	return os.WriteFile(name, data, info.Mode().Perm())
}
