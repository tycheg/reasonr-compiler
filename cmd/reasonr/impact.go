package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
)

// impactCmd is reasonr impact [--base ref] [--format text|json]
// [--allow-test-changes test]... [path]: which acceptance tests a change
// relative to ref may affect (RFC §27.4). Exit code 1: the change
// introduces a feature flag and changes an old test.
func impactCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("impact", flag.ContinueOnError)
	fl.SetOutput(stderr)
	base := fl.String("base", "origin/main", "the base version in git: a branch, a tag or a commit")
	format := fl.String("format", "text", "format: text or json")
	var allow multiFlag
	fl.Var(&allow, "allow-test-changes", "a test that may change when a flag is introduced (can be repeated)")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	path, ok := onePath(fl.Args(), "impact", stderr)
	if !ok {
		return 2
	}
	dirs, err := module.Find(path)
	if err != nil || len(dirs) != 1 {
		fmt.Fprintf(stderr, "reasonr: impact: %s must hold one module\n", path)
		return 2
	}
	allowed := map[string]bool{}
	for _, a := range allow {
		allowed[a] = true
	}
	rep, err := pipeline.Impact(dirs[0], *base, allowed)
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: impact: %v\n", err)
		return 2
	}
	if *format == "json" {
		data, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Fprintln(stdout, string(data))
	} else {
		writeImpact(stdout, rep)
	}
	if len(rep.Violations) > 0 {
		for _, v := range rep.Violations {
			fmt.Fprintf(stderr, "error: %s\n", v)
		}
		return 1
	}
	return 0
}

// writeImpact prints the report as text.
func writeImpact(w io.Writer, rep *pipeline.ImpactReport) {
	if rep.Full {
		fmt.Fprintf(w, "full run: %s\n", strings.Join(rep.Reasons, "; "))
	}
	for _, a := range rep.Models {
		name := "base"
		if a.Model != "" {
			name = a.Model
		}
		fmt.Fprintf(w, "%s: run %d of %d tests\n", name, len(a.Tests), a.Total)
		if a.UI {
			fmt.Fprintln(w, "  the UI changed: with --driver web, all tests")
		}
		var tests []string
		for t := range a.Tests {
			tests = append(tests, t)
		}
		sort.Strings(tests)
		for _, t := range tests {
			why := a.Tests[t]
			if len(why) > 3 {
				why = append(why[:3:3], fmt.Sprintf("%d more", len(a.Tests[t])-3))
			}
			fmt.Fprintf(w, "  %s — %s\n", t, strings.Join(why, ", "))
		}
	}
}

// multiFlag is a flag that can be repeated.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
