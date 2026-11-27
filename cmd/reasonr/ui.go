package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/format"
	"github.com/tycheg/reasonr-compiler/internal/parser"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// uiCmd is reasonr ui eject <App> [path]: the layout the automatic UI of
// an auto app builds (RFC §22.9).
func uiCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "eject" {
		fmt.Fprintln(stderr, "reasonr: usage: reasonr ui eject [--extension name] <App> [path]")
		return 2
	}
	fl := flag.NewFlagSet("ui eject", flag.ContinueOnError)
	fl.SetOutput(stderr)
	ext := fl.String("extension", "", "the model with an extension")
	if err := fl.Parse(args[1:]); err != nil {
		return 2
	}
	if fl.NArg() == 0 {
		fmt.Fprintln(stderr, "reasonr: ui eject: name an app")
		return 2
	}
	name := fl.Arg(0)
	path, ok := onePath(fl.Args()[1:], "ui eject", stderr)
	if !ok {
		return 2
	}
	m, code := loadModel(path, *ext, false, stderr)
	if m == nil {
		return code
	}
	a := m.Info.UI.App(name)
	switch {
	case a == nil:
		fmt.Fprintf(stderr, "reasonr: the model has no app %q\n", name)
		return 2
	case !a.Auto:
		fmt.Fprintf(stderr, "reasonr: the layout of app %s is already written: eject is for app … auto\n", name)
		return 2
	}
	out, _ := format.Source(source.NewFileSet(), "auto/"+name+".rr", a.AutoSource)
	if !structuredModel(m) {
		stdout.Write(out)
		return 0
	}
	// A laid-out project: the app goes to apps/<App>/, screens to the bounded
	// context of the type whose methods they bind to (RFC §23).
	for i, part := range ejectParts(m, a, out) {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "# %s\n\n%s", part.path, part.text)
	}
	return 0
}

type ejectPart struct{ path, text string }

// ejectParts spreads the ejected layout over the files of project parts.
func ejectParts(m *pipeline.Model, a *sema.App, src []byte) []ejectPart {
	file, _ := parser.ParseFile(source.NewFileSet(), "eject.rr", src)
	var parts []ejectPart
	index := map[string]int{}
	add := func(path string, d ast.Decl) {
		text := string(src[int(d.Pos())-1 : int(d.End())-1])
		text = strings.TrimRight(text, "\n") + "\n"
		i, ok := index[path]
		if !ok {
			index[path] = len(parts)
			parts = append(parts, ejectPart{path: path})
			i = len(parts) - 1
		} else {
			parts[i].text += "\n"
		}
		parts[i].text += text
	}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.AppDecl:
			add("apps/"+a.Name+"/app.rr", d)
		case *ast.ScreenDecl:
			ctx := screenContext(m.Info.UI.Screen(d.Name.Name))
			if ctx == "" {
				ctx = "<Context>"
			}
			add("contexts/"+ctx+"/"+strings.ToLower(a.Name)+".rr", d)
		}
	}
	return parts
}

// screenContext is the bounded context of the first model whose methods the screen binds to.
func screenContext(s *sema.Screen) string {
	if s == nil {
		return ""
	}
	var walk func([]*sema.Node) string
	walk = func(nodes []*sema.Node) string {
		for _, n := range nodes {
			if n.Owner != nil && n.Owner.Context != "" {
				return n.Owner.Context
			}
			if c := walk(n.Children); c != "" {
				return c
			}
		}
		return ""
	}
	return walk(s.Nodes)
}

// structuredModel reports whether the project is laid out in parts: its files have bounded contexts.
func structuredModel(m *pipeline.Model) bool {
	for _, f := range m.Program.Files {
		if f.Context != "" {
			return true
		}
	}
	return false
}
