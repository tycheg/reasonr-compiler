package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/tycheg/reasonr-compiler/internal/tools"
)

// explainCmd is reasonr explain [--extension t] Type[.method] [path].
func explainCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("explain", flag.ContinueOnError)
	fl.SetOutput(stderr)
	ext := fl.String("extension", "", "the model with an extension")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if fl.NArg() == 0 {
		fmt.Fprintln(stderr, "reasonr: name a type or a method: reasonr explain Order.Cancel")
		return 2
	}
	path, ok := onePath(fl.Args()[1:], "explain", stderr)
	if !ok {
		return 2
	}
	m, code := loadModel(path, *ext, false, stderr)
	if m == nil {
		return code
	}
	out, err := tools.Explain(m, fl.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	fmt.Fprint(stdout, out)
	return 0
}

// graphCmd is reasonr graph [--extension t] [--entity Type | --app App | --contexts] [--format mermaid|dot] [path].
func graphCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("graph", flag.ContinueOnError)
	fl.SetOutput(stderr)
	ext := fl.String("extension", "", "the model with an extension")
	entity := fl.String("entity", "", "only the commands and reactions of this type")
	app := fl.String("app", "", "transitions between the screens of an app")
	contexts := fl.Bool("contexts", false, "the context map: ACLs from the source to the receiver")
	format := fl.String("format", "mermaid", "format: mermaid or dot")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if *format != "mermaid" && *format != "dot" {
		fmt.Fprintf(stderr, "reasonr: unknown format %q\n", *format)
		return 2
	}
	path, ok := onePath(fl.Args(), "graph", stderr)
	if !ok {
		return 2
	}
	m, code := loadModel(path, *ext, false, stderr)
	if m == nil {
		return code
	}
	if *entity != "" && m.Info.Lookup(*entity) == nil {
		fmt.Fprintf(stderr, "reasonr: the model has no type %q\n", *entity)
		return 2
	}
	if *app != "" && m.Info.UI.App(*app) == nil {
		fmt.Fprintf(stderr, "reasonr: the model has no app %q\n", *app)
		return 2
	}
	g := tools.BuildGraph(m.Info, *entity)
	switch {
	case *app != "":
		g = tools.BuildScreenGraph(m.Info, *app)
	case *contexts:
		g = tools.BuildContextMap(m.Info)
	}
	var err error
	if *format == "dot" {
		err = g.WriteDot(stdout)
	} else {
		err = g.WriteMermaid(stdout)
	}
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	return 0
}

// piiCmd is reasonr pii [--extension t] [--format text|json] [path].
func piiCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("pii", flag.ContinueOnError)
	fl.SetOutput(stderr)
	ext := fl.String("extension", "", "the model with an extension")
	format := fl.String("format", "text", "format: text or json")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "reasonr: unknown format %q\n", *format)
		return 2
	}
	path, ok := onePath(fl.Args(), "pii", stderr)
	if !ok {
		return 2
	}
	m, code := loadModel(path, *ext, false, stderr)
	if m == nil {
		return code
	}
	r := tools.PII(m)
	var err error
	if *format == "json" {
		err = r.WriteJSON(stdout)
	} else {
		err = r.WriteText(stdout)
	}
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	return 0
}

// extractCmd is reasonr extract Context [path]: the analysis of extracting
// a bounded context into a separate process (docs/plans/0011, B6).
func extractCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("extract", flag.ContinueOnError)
	fl.SetOutput(stderr)
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if fl.NArg() < 1 {
		fmt.Fprintln(stderr, "reasonr: extract: name a bounded context")
		return 2
	}
	ctx := fl.Arg(0)
	path, ok := onePath(fl.Args()[1:], "extract", stderr)
	if !ok {
		return 2
	}
	m, code := loadModel(path, "", false, stderr)
	if m == nil {
		return code
	}
	known := false
	for _, d := range m.Info.DeclList {
		known = known || d.Context == ctx
	}
	if !known {
		fmt.Fprintf(stderr, "reasonr: the model has no bounded context %q\n", ctx)
		return 2
	}
	if err := tools.WriteExtraction(stdout, tools.Extract(m.Info, ctx)); err != nil {
		fmt.Fprintf(stderr, "reasonr: %v\n", err)
		return 2
	}
	return 0
}
