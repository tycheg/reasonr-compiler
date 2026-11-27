package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/diag"
)

// diagCmd is reasonr diag explain CODE… and reasonr diag list.
func diagCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "explain" && args[0] != "list") {
		fmt.Fprintln(stderr, "reasonr: diag explain CODE… explains a code; diag list lists all codes")
		return 2
	}
	fl := flag.NewFlagSet("diag "+args[0], flag.ContinueOnError)
	fl.SetOutput(stderr)
	if err := fl.Parse(args[1:]); err != nil {
		return 2
	}
	if args[0] == "list" {
		for _, s := range diag.All() {
			fmt.Fprintf(stdout, "%s  %-8s %s\n", s.Code, s.Severity, s.Title)
		}
		return 0
	}
	if fl.NArg() == 0 {
		fmt.Fprintln(stderr, "reasonr: name a code: reasonr diag explain E0504")
		return 2
	}
	for i, arg := range fl.Args() {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		code := diag.Code(strings.ToUpper(arg))
		if diag.Lookup(code) == nil {
			fmt.Fprintf(stderr, "reasonr: unknown code %s\n", arg)
			return 2
		}
		if err := diag.WriteExplanation(stdout, code); err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
	}
	return 0
}
