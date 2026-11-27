package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	reasonr "github.com/tycheg/reasonr-compiler"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"

	"github.com/tycheg/reasonr-compiler/internal/codegen"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/lower"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
	"github.com/tycheg/reasonr-compiler/internal/uigen"
)

// buildCmd is reasonr build [--extension t] [--dump-ir] [--dump-schema] [--dump-bytecode] [-o file [--test]] [path].
func buildCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("build", flag.ContinueOnError)
	fl.SetOutput(stderr)
	ext := fl.String("extension", "", "build the model with an extension: the base model and extensions/<name>/")
	dumpIR := fl.Bool("dump-ir", false, "print the IR")
	dumpSchema := fl.Bool("dump-schema", false, "print the model schema as JSON (for check --against)")
	dumpBytecode := fl.Bool("dump-bytecode", false, "print the disassembled bytecode")
	dumpUI := fl.Bool("dump-ui", false, "print the UI program as JSON")
	uiTypes := fl.Bool("ui-types", false, "print the interfaces of primitive implementations (TypeScript)")
	platform := fl.String("platform", "web", "the platform for --ui-types: web")
	dumpImpact := fl.Bool("dump-impact", false, "print the test impact analysis graph as JSON: nodes with hashes, the nodes of each test")
	out := fl.String("o", "", "write the model file the engine runs (reasonrd --model file)")
	test := fl.Bool("test", false, "-o: the test program, with tests, scenarios and fakes")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	path, ok := onePath(fl.Args(), "build", stderr)
	if !ok {
		return 2
	}
	if *dumpImpact {
		// The graph is built from the test model: tests are its functions.
		tm, code := loadModel(path, *ext, false, stderr)
		if tm == nil {
			return code
		}
		data, _ := json.MarshalIndent(pipeline.Graph(tm), "", "  ")
		_, _ = stdout.Write(append(data, '\n'))
		return 0
	}
	m, code := loadModel(path, *ext, true, stderr)
	if m == nil {
		return code
	}
	p, info := m.Program, m.Info

	prog := lower.Program(p, info, m.Checks)
	if err := ir.Verify(prog); err != nil {
		fmt.Fprintf(stderr, "reasonr: compiler failure: %v\n", err)
		return 2
	}
	if *dumpIR {
		if err := ir.Print(stdout, prog); err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
	}
	schema := schemagen.Generate(p, info)
	bc, err := codegen.Generate(prog, schema)
	if err == nil {
		err = bytecode.Verify(bc)
	}
	if err != nil {
		fmt.Fprintf(stderr, "reasonr: compiler failure: %v\n", err)
		return 2
	}
	if *dumpBytecode {
		if err := bytecode.Disassemble(stdout, bc); err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
	}
	if *dumpSchema {
		data, err := schema.MarshalIndent()
		if err == nil {
			_, err = stdout.Write(data)
		}
		if err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
	}
	if *dumpUI {
		data, err := json.MarshalIndent(uigen.Generate(info), "", "  ")
		if err == nil {
			_, err = stdout.Write(append(data, '\n'))
		}
		if err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
	}
	if *uiTypes {
		if *platform != "web" {
			fmt.Fprintf(stderr, "reasonr: --ui-types: platform %q is not supported, there is web\n", *platform)
			return 2
		}
		io.WriteString(stdout, uigen.TypeScript(uigen.Generate(info)))
	}
	if *out != "" {
		// The model file the engine runs without the sources (reasonrd --model file).
		model, err := reasonr.Build(path, reasonr.Options{Extension: *ext, Test: *test})
		var data []byte
		if err == nil {
			data, err = model.Encode()
		}
		if err == nil {
			err = os.WriteFile(*out, data, 0o644)
		}
		if err != nil {
			fmt.Fprintf(stderr, "reasonr: %v\n", err)
			return 2
		}
	}
	if !*dumpIR && !*dumpSchema && !*dumpBytecode && !*dumpUI && !*uiTypes {
		hash := ir.Hash(prog)
		fmt.Fprintf(stdout, "%s: %d functions, IR sha256 %s\n", m.Module.Dir, len(prog.Funcs), hex.EncodeToString(hash[:]))
	}
	return 0
}
